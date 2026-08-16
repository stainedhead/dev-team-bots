// Package acp implements a thin Agent Client Protocol (ACP) adapter over
// BaoBot's existing domain.Worker execution engine, so a single BaoBot
// persona can be registered as a buzz-acp custom harness. See
// specs/260813-boabot-acp-stdio-harness-support/architecture.md.
package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	apporchestrator "github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// defaultKeepAliveInterval is well under buzz-acp's --idle-timeout so a
// long-running turn is never killed for stdout silence (architecture.md
// AD-3). Mirrors the Slack ChannelMonitor's typing-indicator refresh cadence
// documented in Buzz-Adoption-Config.md.
const defaultKeepAliveInterval = 10 * time.Second

// defaultMaxSessions bounds Agent.sessions for a long-lived, pooled process
// (auto-review RT5/FR-006) -- a safety net for hosts that never call
// session/close, on top of CloseSession now being a real removal path.
// Generous: this is a leak backstop, not a real per-deployment limit.
const defaultMaxSessions = 10_000

// updater is the minimal surface of *sdk.AgentSideConnection this package
// needs, kept as an interface so tests can substitute a fake instead of
// standing up a real stdio transport.
type updater interface {
	SessionUpdate(ctx context.Context, params sdk.SessionNotification) error
}

// progressReporter is satisfied by *application.ExecuteTaskUseCase (and any
// other domain.Worker that offers it). Detected via type assertion rather
// than added to domain.Worker itself, so the domain layer stays unchanged
// per architecture.md AD-1.
type progressReporter interface {
	WithProgressHandler(func(taskID, line string))
}

// Agent implements sdk.Agent as a thin adapter over an existing
// domain.Worker -- no new domain interfaces, per architecture.md AD-1.
type Agent struct {
	worker  domain.Worker
	workDir string
	conn    updater

	keepAliveInterval time.Duration

	// turnMu serializes turn execution across every session on this Agent.
	// The underlying Worker is a single shared instance (see New), and
	// Worker.WithProgressHandler mutates unsynchronized state on it per
	// turn (application.ExecuteTaskUseCase.progressFn has no lock of its
	// own -- safe in native mode only because WithProgressHandler is called
	// once at startup, never per-task). Serializing here is the fix for a
	// real, reproduced data race (auto-review FR-001) and, as a side
	// effect, also closes FR-005's same-session-ID cancellation
	// re-entrancy bug: a second overlapping Prompt call for any session
	// simply waits for the first to fully finish (including its deferred
	// session.cancel reset) before starting, so there is never a live
	// cancel func to clobber. Residual limitation: session/cancel for a
	// session whose turn hasn't started yet (still queued behind another
	// session's in-flight turn) is a no-op, since that session's
	// session.cancel isn't set until its turn actually begins -- not
	// addressed here; no finding required it.
	turnMu sync.Mutex

	maxSessions int

	// publisher is the fallback-publish safety net (turn.go) used when a
	// turn produces real output but the model never called the buzz CLI
	// itself to publish it.
	publisher Publisher

	// botName tags every DirectTask this Agent records (FR-504a) and is
	// used for FR-503's ChatMessage.BotName field. Empty is safe -- both
	// simply record an empty botName, matching pre-feature behavior for a
	// persona that hasn't opted in.
	botName string

	// chatStore, when set, backs FR-503's conversation continuity: every
	// turn's inbound/outbound message is appended to it, and each new
	// turn's instruction is built by replaying recent history for the same
	// thread -- mirroring BuzzTaskBridge's identical ChatStore usage in
	// native mode. nil disables history replay entirely (pre-feature
	// behavior).
	chatStore domain.ChatStore

	// taskStore and board, when both set, back FR-504a: every ACP-dispatched
	// task is recorded as a real DirectTask and Kanban board item, updated
	// to its final status/output when the turn completes. Either being nil
	// disables this recording entirely (pre-feature behavior) -- a board
	// item with no backing DirectTask, or vice versa, would be a broken
	// half-write, so both must be present together.
	taskStore domain.DirectTaskStore
	board     domain.BoardStore

	// chatTaskManager, when set, backs FR-504's scheduling-intent pre-check:
	// DetectAndHandle runs on every turn's raw instruction before the
	// existing synchronous worker.Execute path, exactly mirroring
	// BuzzTaskBridge.Dispatch's identical use in native mode. nil disables
	// scheduling detection entirely -- every message falls through to
	// worker.Execute unchanged (pre-feature behavior).
	chatTaskManager *apporchestrator.ChatTaskManager

	mu           sync.Mutex
	sessions     map[sdk.SessionId]*session
	sessionOrder []sdk.SessionId // insertion order, for FIFO eviction once maxSessions is exceeded
}

var _ sdk.Agent = (*Agent)(nil)

// Option configures an Agent constructed by New.
type Option func(*Agent)

// WithKeepAliveInterval overrides the default keep-alive session/update
// cadence (defaultKeepAliveInterval) -- mainly useful for tests, but also a
// legitimate operator tuning knob if buzz-acp's --idle-timeout is
// configured tighter than the default in a given deployment.
func WithKeepAliveInterval(d time.Duration) Option {
	return func(a *Agent) { a.keepAliveInterval = d }
}

// WithMaxSessions overrides defaultMaxSessions -- mainly useful for tests,
// but also a legitimate knob for a deployment that wants a tighter bound.
func WithMaxSessions(n int) Option {
	return func(a *Agent) { a.maxSessions = n }
}

// WithPublisher overrides the default cliPublisher -- mainly useful for
// tests, but also lets a deployment substitute a different publish
// mechanism if it ever needs to.
func WithPublisher(p Publisher) Option {
	return func(a *Agent) { a.publisher = p }
}

// WithBotName sets the persona identity tagged onto every DirectTask/
// ChatMessage this Agent records (FR-503/FR-504a). Unset (empty) is safe --
// records are simply tagged with an empty name.
func WithBotName(name string) Option {
	return func(a *Agent) { a.botName = name }
}

// WithChatStore enables FR-503's conversation-continuity history replay.
// Unset (nil, the default) disables it entirely -- pre-feature behavior.
func WithChatStore(cs domain.ChatStore) Option {
	return func(a *Agent) { a.chatStore = cs }
}

// WithDirectTaskStore and WithBoardStore together enable FR-504a's
// automatic per-turn DirectTask/board-item recording. Both must be set for
// recording to activate -- see the Agent.taskStore/board field doc comment.
func WithDirectTaskStore(ts domain.DirectTaskStore) Option {
	return func(a *Agent) { a.taskStore = ts }
}

func WithBoardStore(b domain.BoardStore) Option {
	return func(a *Agent) { a.board = b }
}

// WithChatTaskManager enables FR-504's scheduling-intent pre-check. Unset
// (nil, the default) disables it entirely -- pre-feature behavior.
func WithChatTaskManager(m *apporchestrator.ChatTaskManager) Option {
	return func(a *Agent) { a.chatTaskManager = m }
}

// New constructs an Agent backed by the Worker workerFactory.New() returns.
// workDir is applied to every domain.Task built from a session/prompt call,
// matching the persona's configured work directory.
func New(workerFactory domain.WorkerFactory, workDir string, opts ...Option) *Agent {
	a := &Agent{
		worker:            workerFactory.New(),
		workDir:           workDir,
		keepAliveInterval: defaultKeepAliveInterval,
		maxSessions:       defaultMaxSessions,
		publisher:         cliPublisher{},
		sessions:          make(map[sdk.SessionId]*session),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// SetConnection wires the live ACP connection after
// sdk.NewAgentSideConnection constructs it -- the connection needs the Agent
// to exist first, so this can't happen in New. Mirrors coder/acp-go-sdk's
// own example/agent SetAgentConnection convention.
func (a *Agent) SetConnection(conn *sdk.AgentSideConnection) {
	a.setUpdater(conn)
}

// setUpdater is the test seam behind SetConnection (see updater above).
func (a *Agent) setUpdater(u updater) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conn = u
}

// Initialize implements sdk.Agent. ACP mode advertises no auth methods --
// buzz-acp already owns relay authentication; there is nothing for BaoBot's
// side of the connection to separately authenticate.
func (a *Agent) Initialize(_ context.Context, _ sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	return sdk.InitializeResponse{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		AuthMethods:     []sdk.AuthMethod{},
		AgentCapabilities: sdk.AgentCapabilities{
			SessionCapabilities: sdk.SessionCapabilities{
				// CloseSession is a real removal path, not a MethodNotFound
				// stub -- RT5/FR-006, auto-review.
				Close: &sdk.SessionCloseCapabilities{},
			},
		},
	}, nil
}

// Authenticate implements sdk.Agent as a no-op success: Initialize
// advertises no auth methods, so a well-behaved client should never call
// this, but accepting it gracefully (rather than erroring) matches
// coder/acp-go-sdk's own reference example/agent.
func (a *Agent) Authenticate(_ context.Context, _ sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, nil
}

// NewSession implements sdk.Agent, allocating per-session turn-cancellation
// state (session.go). Evicts the oldest session(s) first if maxSessions
// would otherwise be exceeded (RT5/FR-006, auto-review).
func (a *Agent) NewSession(_ context.Context, _ sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	sid := sdk.SessionId(randomID())
	a.mu.Lock()
	a.sessions[sid] = &session{}
	a.sessionOrder = append(a.sessionOrder, sid)
	var evicted []context.CancelFunc
	for len(a.sessionOrder) > a.maxSessions {
		oldest := a.sessionOrder[0]
		a.sessionOrder = a.sessionOrder[1:]
		if s, ok := a.sessions[oldest]; ok {
			if s.cancel != nil {
				evicted = append(evicted, s.cancel)
			}
			delete(a.sessions, oldest)
		}
	}
	a.mu.Unlock()
	for _, cancel := range evicted {
		cancel()
	}
	return sdk.NewSessionResponse{SessionId: sid}, nil
}

// Cancel implements sdk.Agent, cancelling the named session's in-flight
// turn context (architecture.md Data Flow step 7).
func (a *Agent) Cancel(_ context.Context, params sdk.CancelNotification) error {
	a.mu.Lock()
	var cancel context.CancelFunc
	if s, ok := a.sessions[params.SessionId]; ok {
		cancel = s.cancel
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// SetSessionConfigOption implements sdk.Agent as a no-op success.
// buzz-acp calls this for its `mode` (permission-mode) config option on
// every session by default (research.md) -- BaoBot's ACP mode has no
// permission-gating of its own to apply the option to, so it simply
// acknowledges the call rather than erroring.
func (a *Agent) SetSessionConfigOption(_ context.Context, _ sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	return sdk.SetSessionConfigOptionResponse{}, nil
}

// SetSessionMode implements sdk.Agent as a no-op success -- BaoBot's ACP
// mode does not support multiple session modes in v1.
func (a *Agent) SetSessionMode(_ context.Context, _ sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, nil
}

// Logout, ListSessions, and ResumeSession are optional capabilities
// Initialize does not advertise support for -- returning MethodNotFound
// matches coder/acp-go-sdk's own reference example/agent's treatment of
// unsupported optional methods. CloseSession, below, IS supported for real.

func (a *Agent) Logout(_ context.Context, _ sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

// CloseSession implements sdk.Agent for real (RT5/FR-006, auto-review) --
// advertised via AgentCapabilities.SessionCapabilities.Close in Initialize.
// Per the ACP spec, closing must first cancel any ongoing work for the
// session (treated as if session/cancel was called) and then free its
// resources.
func (a *Agent) CloseSession(_ context.Context, params sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	a.mu.Lock()
	var cancel context.CancelFunc
	if s, ok := a.sessions[params.SessionId]; ok {
		cancel = s.cancel
	}
	delete(a.sessions, params.SessionId)
	for i, sid := range a.sessionOrder {
		if sid == params.SessionId {
			a.sessionOrder = append(a.sessionOrder[:i], a.sessionOrder[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return sdk.CloseSessionResponse{}, nil
}

func (a *Agent) ListSessions(_ context.Context, _ sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionList)
}

func (a *Agent) ResumeSession(_ context.Context, _ sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "sess_" + hex.EncodeToString([]byte(time.Now().String()))
	}
	return "sess_" + hex.EncodeToString(b[:])
}
