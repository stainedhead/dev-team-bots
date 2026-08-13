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
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// defaultKeepAliveInterval is well under buzz-acp's --idle-timeout so a
// long-running turn is never killed for stdout silence (architecture.md
// AD-3). Mirrors the Slack ChannelMonitor's typing-indicator refresh cadence
// documented in Buzz-Adoption-Config.md.
const defaultKeepAliveInterval = 10 * time.Second

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

	mu       sync.Mutex
	sessions map[sdk.SessionId]*session
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

// New constructs an Agent backed by the Worker workerFactory.New() returns.
// workDir is applied to every domain.Task built from a session/prompt call,
// matching the persona's configured work directory.
func New(workerFactory domain.WorkerFactory, workDir string, opts ...Option) *Agent {
	a := &Agent{
		worker:            workerFactory.New(),
		workDir:           workDir,
		keepAliveInterval: defaultKeepAliveInterval,
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
// state (session.go).
func (a *Agent) NewSession(_ context.Context, _ sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	sid := sdk.SessionId(randomID())
	a.mu.Lock()
	a.sessions[sid] = &session{}
	a.mu.Unlock()
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

// Logout, CloseSession, ListSessions, and ResumeSession are optional
// capabilities Initialize does not advertise support for -- returning
// MethodNotFound matches coder/acp-go-sdk's own reference example/agent's
// treatment of unsupported optional methods.

func (a *Agent) Logout(_ context.Context, _ sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

func (a *Agent) CloseSession(_ context.Context, _ sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionClose)
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
