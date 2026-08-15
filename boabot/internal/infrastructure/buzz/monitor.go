// Package buzz (see translate.go's package doc) also implements Monitor, a
// domain.ChannelMonitor adapter for Buzz (Phase F), mirroring
// internal/infrastructure/slack.Monitor's shape: discover/subscribe to
// channels, dispatch qualifying inbound events as tasks onto the existing
// domain.MessageQueue, and correlate results back to a kind:9 reply via a
// pending map keyed by task ID.
package buzz

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"github.com/google/uuid"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Nostr/Buzz event kinds this file cares about (data-dictionary.md's
// "Enumerations" section).
const (
	kindChannelMetadata = 39000 // NIP-29 relay-signed group metadata (F1)
	kindChannelMembers  = 39002 // NIP-29 relay-signed group member list (F1)
	kindChannelMessage  = 9     // NIP-29 group message (F2/F5)
	kindMemberAdded     = 44100 // Buzz membership-added (F3, p-gated)
	kindMemberRemoved   = 44101 // Buzz membership-removed (F3, p-gated)
	kindPresence        = 20001 // Buzz presence (F14/F15)
	kindTyping          = 20002 // Buzz typing indicator (F16)
)

// shutdownCommand is FR-026's exact stop-signal string.
const shutdownCommand = "!shutdown"

// maxContentLen bounds an inbound kind:9 event's Content before it is
// allowed to become a TaskPayload.Instruction (review PRD FR-005). Nothing
// in this codebase caps evt.Content upstream (translate.go's
// FromLibraryEvent/ToLibraryEvent impose no limit, nor does
// fiatjaf.com/nostr's WebSocket transport, verified but not relied upon) --
// once BaoBot is authenticated into a workspace, any other member can
// publish an arbitrarily large kind:9 event that, if it passes the
// trigger/author gate, would otherwise become an uncapped worker-harness
// instruction with no demonstrated exploit but real uncontrolled token/cost
// spend on a single oversized message.
//
// 64 KiB is chosen as generous for any legitimate multi-paragraph chat
// message (Nostr relays and clients commonly cap event/content sizes in a
// similar tens-of-KB range) while still closing off the unbounded-spend
// concern; it does not need to be operator-tunable to satisfy that goal.
//
// Package constant, not a Monitor.Config field (architecture.md AD-4):
// consistent with FR-007/OQ-R2's resolution for reconnect backoff, this
// avoids adding operator-tunable surface without a demonstrated need. No
// concrete reason surfaced during WS-D's implementation (no existing test
// fixture needed a non-default bound) to promote it to Config -- if an
// operator need for a different bound is identified later, add a
// MaxContentLen field to Config then, defaulting to this constant when
// zero, mirroring PresenceInterval's existing zero-value-defaults pattern.
const maxContentLen = 64 * 1024

// relayClient is the seam Monitor needs from a relay connection. It is
// satisfied by this package's own concrete *RelayClient in production, and
// by a fake in tests -- so Monitor's discovery/dispatch/reply logic is
// unit-testable without a live relay, per the brief's "unit-testable via
// Phase D's existing mock/fake seams wherever possible." SetConnStateFunc
// is the F14 prerequisite added to RelayClient in reconnect.go/
// relay_client.go this phase.
type relayClient interface {
	Connect(ctx context.Context) error
	Authenticate(ctx context.Context) error
	Publish(ctx context.Context, evt domain.Event) error
	// PublishRaw sends evt verbatim, without signing/overwriting its
	// PubKey -- see domain.RelayClient.PublishRaw's doc comment. Required
	// for the DM reply path (dm.go), whose gift-wrapped events are already
	// signed with a throwaway ephemeral key by nip59.GiftWrap.
	PublishRaw(ctx context.Context, evt domain.Event) error
	Subscribe(ctx context.Context, f domain.Filter) (<-chan domain.Event, error)
	Close() error
	SetConnStateFunc(fn func(connected bool))
}

var _ relayClient = (*RelayClient)(nil)

// contentScreener is the seam Monitor uses for FR-028: every inbound
// message body, channel name, and channel topic is routed through it
// before being used for anything beyond gate/trigger/control-command
// matching (which operates on raw content -- see handleChannelEvent).
// Satisfied by *internal/application/screening.ScreenContentUseCase in
// production, whose Screen method already wraps output in
// <untrusted-content> delimiters -- the same path MCP tool output goes
// through, per FR-028's literal wording.
type contentScreener interface {
	Screen(content string) (string, error)
}

// ShutdownFunc is F17's hook into "the existing graceful shutdown path"
// (application.RunAgentUseCase.Shutdown in production). Monitor never
// reimplements shutdown logic itself -- it only calls this when a
// !shutdown command passes the FR-026 gate.
type ShutdownFunc func(ctx context.Context) error

// Config holds Monitor's non-secret parameters. It deliberately mirrors
// slack.Config's shape and is a plain, dependency-free struct -- not
// *config.BuzzConfig, which is Phase H's wiring concern (architecture.md
// Phase H note).
type Config struct {
	// RelayURL is used only for structured logging (FR-032); the actual
	// connection is owned by the relayClient passed to NewMonitor.
	RelayURL string

	// BotName is the target bot queue name, mirroring slack.Config.BotName.
	BotName string

	// AgentPubKeyHex is this bot's own authenticated pubkey, hex-encoded.
	AgentPubKeyHex string

	// OwnerPubkeyHex is consulted only by F17's wider !shutdown gate
	// (FR-026); it plays no role in F8's ordinary dispatch gate.
	OwnerPubkeyHex string

	// RespondTo and RespondToAllowlist implement F8 (FR-029). See
	// authorGate's doc comment for the nil-vs-empty-slice semantics --
	// callers MUST pass RespondToAllowlist through unmodified.
	RespondTo          string
	RespondToAllowlist []string

	// PresenceInterval is F14's publish interval; defaults to
	// defaultPresenceInterval when zero. MUST stay under the 180s
	// staleness bound (FR-023) -- not enforced here, since NewMonitor
	// takes plain typed parameters per architecture.md's Phase H note;
	// validating operator-supplied durations against that bound is a
	// config-loading concern (H1), not Monitor's.
	PresenceInterval time.Duration

	// LockDir is the directory Start uses to acquire FR-031/OQ-1's
	// process-singleton lock (see lock.go): the file itself is named
	// buzz-<shortpubkey>.lock (lockFileName), so it is already scoped to
	// AgentPubKeyHex -- never the raw nsec, which this package never
	// writes to disk. LockDir MUST therefore be the *shared* memory root
	// (internal/application/team.ManagerConfig.MemoryRoot), not a
	// per-bot subdirectory: the whole point of keying on the derived
	// pubkey rather than bot name is to catch two boabot processes with
	// *different* bot configs pointed at the same nsec, which a per-bot
	// directory would defeat. Phase H's cmd/boabot/main.go wiring is
	// expected to set it accordingly.
	//
	// Empty (the zero value) disables the lock entirely -- Start behaves
	// exactly as it did before Phase G (and logs a warning, since this
	// leaves FR-031's protection inactive). This keeps Config
	// dependency-free per architecture.md's Phase H note (NewMonitor
	// takes plain typed parameters, not *config.BuzzConfig) and leaves
	// every pre-Phase-G test unaffected; production activation of the
	// lock is Phase H's wiring concern, not this struct's -- H2 MUST set
	// both LockDir and AgentPubKeyHex for the lock to actually protect
	// anything.
	LockDir string
}

// replyTarget is where HandleResult (F12) publishes a channel-dispatched
// task's kind:9 reply, carrying everything publishReply needs for complete
// NIP-10 tagging (FR-207): the root event (thread root, per P1.1's fix),
// the immediate parent event this reply is directed at (parentEventID --
// the triggering mention/thread-reply's own event ID), and that event's
// author (authorPubKey), so the eventual reply can carry a `p` tag back to
// them.
type replyTarget struct {
	channelUUID   string
	rootEventID   string
	parentEventID string
	authorPubKey  string
}

// pendingEntry is one in-flight dispatched task. Exactly one of target
// (channel-dispatched) or dmTarget (DM-dispatched, P2.3) is meaningful --
// dmTarget is non-nil only for a task dispatched via the DM path, in which
// case target is the zero value and ignored.
type pendingEntry struct {
	target   replyTarget
	dmTarget *dmReplyTarget

	// typingDone is closed by HandleResult, under the same lock that pops
	// this entry from Monitor.pending, to stop F16's typing-indicator loop
	// for this task. nil for a DM-dispatched entry -- F16's typing
	// indicator is a channel (#h-tagged) concept that has no DM analogue,
	// so no typing loop is ever started for one; HandleResult and Stop
	// nil-check before closing it.
	typingDone chan struct{}
}

// Monitor implements domain.ChannelMonitor for Buzz.
type Monitor struct {
	cfg      Config
	relay    relayClient
	queue    domain.MessageQueue
	screener contentScreener
	gate     authorGate
	shutdown ShutdownFunc
	logger   *slog.Logger

	// taskDispatcher, when set via WithTaskDispatcher, routes dispatch()
	// through the orchestrator's Dispatcher/DirectTaskStore/BoardStore
	// bridge (domain.BuzzTaskDispatcher, backed by
	// internal/application/orchestrator.BuzzTaskBridge in production)
	// instead of building/sending the task message directly. nil preserves
	// the pre-existing direct queue.Send behaviour unchanged -- production
	// wiring (cmd/boabot/main.go) always sets this, so the direct-queue
	// path only remains live for tests that predate the bridge and any
	// caller that intentionally omits it.
	taskDispatcher domain.BuzzTaskDispatcher

	// chatStore, when set via WithChatStore, records every inbound/outbound
	// Buzz message (channel thread-reply and DM) so a later dispatch in the
	// same thread/conversation can replay it as context (FR-206). nil is
	// safe -- every call site nil-checks before using it, degrading to the
	// pre-existing no-history-replay behaviour.
	chatStore domain.ChatStore

	// dmKeyer, when set via WithDMKeyer, is this persona's nostr.Keyer
	// adapter (dm_keyer.go) over its own key material, required for every
	// DM operation (P2.1). nil disables DM support entirely -- no
	// subscription is opened and no DM reply can be prepared -- consistent
	// with spec.md's non-goal "DM support for personas without a
	// provisioned key."
	dmKeyer nostr.Keyer

	newTicker func(time.Duration) ticker

	mu      sync.Mutex
	pending map[string]*pendingEntry

	channelsMu sync.Mutex
	channels   map[string]context.CancelFunc // channel UUID -> cancel for its kind:9 subscription (F2/F3)

	presenceMu     sync.Mutex
	presenceCancel context.CancelFunc

	// lockMu guards lock, which Start acquires (FR-031/OQ-1) and Stop
	// releases (presence.go), alongside the offline-presence publish, as
	// part of the same shutdown sequence.
	lockMu sync.Mutex
	lock   *Lock
}

var _ domain.ChannelMonitor = (*Monitor)(nil)

// MonitorOption configures a Monitor.
type MonitorOption func(*Monitor)

// WithShutdownFunc registers F17's hook into the existing graceful
// shutdown path. Without one, an allowed !shutdown is logged as an error
// (no hook wired) rather than silently doing nothing.
func WithShutdownFunc(fn ShutdownFunc) MonitorOption {
	return func(m *Monitor) { m.shutdown = fn }
}

// WithMonitorLogger overrides Monitor's logger (defaults to slog.Default()).
func WithMonitorLogger(l *slog.Logger) MonitorOption {
	return func(m *Monitor) { m.logger = l }
}

// WithTaskDispatcher wires dispatch() through a domain.BuzzTaskDispatcher
// bridge (P2.2's replacement of the direct queue.Send/Worker.Execute path)
// instead of building/sending the task message directly. See the
// taskDispatcher field's doc comment for why this is an option rather than
// a required constructor parameter.
func WithTaskDispatcher(td domain.BuzzTaskDispatcher) MonitorOption {
	return func(m *Monitor) { m.taskDispatcher = td }
}

// WithChatStore wires ChatStore-backed history replay (FR-206/P1.5) for
// both the channel thread-continuation path and DM conversations. See the
// chatStore field's doc comment.
func WithChatStore(cs domain.ChatStore) MonitorOption {
	return func(m *Monitor) { m.chatStore = cs }
}

// WithDMKeyer wires DM support (P2.1-P2.4): a nostr.Keyer over this
// persona's own key material, required for the DM subscription and reply
// paths. See the dmKeyer field's doc comment for what omitting it means.
func WithDMKeyer(kr nostr.Keyer) MonitorOption {
	return func(m *Monitor) { m.dmKeyer = kr }
}

// withTicker overrides the ticker constructor used by the presence (F14)
// and typing-indicator (F16) loops. Test-only (unexported): production
// callers get newRealTicker.
func withTicker(fn func(time.Duration) ticker) MonitorOption {
	return func(m *Monitor) { m.newTicker = fn }
}

// NewMonitor constructs a Monitor. rc's SetConnStateFunc is wired to the
// new Monitor's presence-suspend/resume hook (F14) as part of
// construction, so callers never need to remember to do it themselves.
func NewMonitor(rc relayClient, cfg Config, queue domain.MessageQueue, screener contentScreener, opts ...MonitorOption) *Monitor {
	m := &Monitor{
		cfg:       cfg,
		relay:     rc,
		queue:     queue,
		screener:  screener,
		gate:      newAuthorGate(cfg),
		logger:    slog.Default(),
		newTicker: newRealTicker,
		pending:   make(map[string]*pendingEntry),
		channels:  make(map[string]context.CancelFunc),
	}
	for _, opt := range opts {
		opt(m)
	}
	rc.SetConnStateFunc(m.onConnStateChange)
	return m
}

// Start implements domain.ChannelMonitor. It first acquires FR-031/OQ-1's
// process-singleton lock (when Config.LockDir is set): a second boabot
// process started against the same identity (nsec) -- an operator-error
// double-start, or a botched upgrade leaving two copies running -- finds
// the lock already held, logs why clearly, and returns nil without
// launching connect/authenticate/discovery, without returning an error,
// and without touching m.relay at all. This mirrors
// internal/infrastructure/credentials.Load's fail-fast-without-crashing
// precedent: the Buzz monitor declines to attach, but every other channel
// monitor and every other bot in this process starts normally (FR-003).
//
// Once the lock (if any) is held, connect/authenticate/discovery run in a
// goroutine and Start returns immediately -- mirroring
// slack.Monitor.Start -- so a Buzz outage at startup never blocks Slack,
// SQS, or scheduled-task processing (NFR Reliability).
func (m *Monitor) Start(ctx context.Context) error {
	switch {
	case m.cfg.LockDir == "":
		// Locking is opt-in (Config.LockDir's zero value): Phase H's
		// cmd/boabot/main.go wiring is responsible for setting it.
		// Warn loudly rather than silently leaving FR-031 unenforced, so
		// a missed H2 wiring step is greppable in the running process's
		// own logs, not just discoverable by reading this file.
		m.logger.Warn("buzz monitor: singleton lock not configured (Config.LockDir empty); FR-031/OQ-1 multi-instance protection is INACTIVE for this monitor",
			"agent_pubkey", m.cfg.AgentPubKeyHex)
	case m.cfg.AgentPubKeyHex == "":
		// Without a pubkey every identity collapses onto the same
		// buzz-.lock file, which would make unrelated bots/identities
		// falsely contend with each other. Refuse outright rather than
		// acquiring a meaningless shared lock.
		m.logger.Error("buzz monitor: refusing to start -- LockDir is configured but AgentPubKeyHex is empty; " +
			"cannot key the FR-031/OQ-1 singleton lock to an identity")
		return nil
	default:
		lock, err := AcquireLock(LockPath(m.cfg.LockDir, m.cfg.AgentPubKeyHex))
		if err != nil {
			m.logger.Error("buzz monitor: refusing to start -- singleton lock for this identity is already held (FR-031/OQ-1); "+
				"another boabot process is likely already running against this nsec (operator double-start or a botched upgrade); "+
				"the Buzz monitor will not attach, but all other channels and bots in this process continue normally",
				"agent_pubkey", m.cfg.AgentPubKeyHex, "lock_dir", m.cfg.LockDir, "err", err)
			return nil
		}
		m.lockMu.Lock()
		m.lock = lock
		m.lockMu.Unlock()
	}

	go m.run(ctx)
	return nil
}

func (m *Monitor) run(ctx context.Context) {
	if err := m.relay.Connect(ctx); err != nil {
		m.logger.Error("buzz monitor: connect failed", "relay_url", m.cfg.RelayURL, "err", err)
		return
	}
	if err := m.relay.Authenticate(ctx); err != nil {
		m.logger.Error("buzz monitor: authenticate failed", "relay_url", m.cfg.RelayURL, "err", err)
		return
	}
	if err := m.startDiscovery(ctx); err != nil {
		m.logger.Error("buzz monitor: start discovery failed", "err", err)
		return
	}
	if err := m.startMembershipWatch(ctx); err != nil {
		m.logger.Error("buzz monitor: start membership watch failed", "err", err)
		return
	}
	// P2.2: DM support is additive and opt-in (dmKeyer wired via
	// WithDMKeyer). A DM subscription failure is isolated to DM handling
	// only -- it must never block or tear down channel discovery/
	// membership watch above, which have already started successfully by
	// this point (NFR Reliability).
	if m.dmKeyer != nil {
		m.startDMSubscription(ctx)
	}
}

// screen implements FR-028: content is routed through the configured
// contentScreener before it is used for anything beyond gate/trigger/
// control-command matching. On a screener error, the original content is
// used (logged, not fatal) -- a screening-tool failure must not block an
// otherwise-legitimate dispatch outright; this is a deliberate fail-open
// choice on the screener's own availability, not a weakening of the
// untrusted-content wrapping it normally applies. See
// implementation-notes.md for the full rationale.
func (m *Monitor) screen(field, content string) string {
	if m.screener == nil {
		return content
	}
	screened, err := m.screener.Screen(content)
	if err != nil {
		m.logger.Warn("buzz monitor: content screening failed", "field", field, "err", err)
		return content
	}
	return screened
}

// handleChannelEvent implements F9 (self-filter) -> F10 (trigger
// classification, extended by P1.2's triggerThreadReply fallback) -> the
// !shutdown branch (F17, checked on RAW content with FR-026's wider gate)
// OR the ordinary dispatch path (F11, which applies F8's ordinary gate
// internally). Screening (F7) happens only once content is actually used to
// build a task instruction -- never before gate/trigger/control-command
// matching, so a prompt-injection payload can never be crafted to also
// dodge the author gate or masquerade as a control command via
// screener-side rewriting.
func (m *Monitor) handleChannelEvent(ctx context.Context, channelUUID string, evt domain.Event) {
	if evt.PubKey == m.cfg.AgentPubKeyHex {
		return // F9: self-authored, ignore (loop prevention)
	}

	root := rootEventID(evt)
	kind := classifyTrigger(evt, m.cfg.AgentPubKeyHex)
	if kind != triggerMention {
		// FR-205/P1.2: not an explicit @mention -- still dispatch if this is
		// a reply/root-tagged event within a thread this persona previously
		// dispatched in (KnownThread). Only meaningful with a bridge wired
		// (KnownThread is BuzzTaskBridge-backed state); the pre-bridge
		// direct-dispatch fallback never supported thread continuation and
		// still doesn't.
		if m.taskDispatcher == nil {
			return
		}
		matchedRoot, ok := m.matchKnownThread(evt)
		if !ok {
			return
		}
		kind = triggerThreadReply
		root = matchedRoot
	}

	text := strings.TrimSpace(evt.Content)
	if text == shutdownCommand {
		m.handleShutdownCommand(ctx, evt)
		return
	}

	if kind == triggerThreadReply {
		m.logger.Info("buzz monitor: dispatching thread-reply without re-mention",
			"channel", channelUUID, "event_id", evt.ID, "thread_root", root)
	}

	m.dispatch(ctx, channelUUID, root, evt)
}

// matchKnownThread implements P1.2's NIP-10 reply/root-tag detection for an
// event that did not qualify via classifyTrigger's explicit-@mention check.
// It only considers kind:9 channel messages (mirroring classifyTrigger's own
// kind check) and tries each of threadReplyCandidates(evt), in order,
// against m.taskDispatcher.KnownThread, returning the first match.
func (m *Monitor) matchKnownThread(evt domain.Event) (string, bool) {
	if evt.Kind != kindChannelMessage {
		return "", false
	}
	for _, candidate := range threadReplyCandidates(evt) {
		if m.taskDispatcher.KnownThread(m.cfg.BotName, candidate) {
			return candidate, true
		}
	}
	return "", false
}

// handleShutdownCommand implements F17/FR-026.
func (m *Monitor) handleShutdownCommand(ctx context.Context, evt domain.Event) {
	if !m.gate.allowsShutdown(evt.PubKey, m.cfg.OwnerPubkeyHex) {
		m.logger.Warn("buzz monitor: !shutdown rejected: sender outside author gate",
			"pubkey", evt.PubKey, "event_id", evt.ID)
		return
	}

	m.logger.Warn("buzz monitor: !shutdown received from an allowed pubkey; initiating graceful shutdown",
		"pubkey", evt.PubKey, "event_id", evt.ID)

	if m.shutdown == nil {
		m.logger.Error("buzz monitor: !shutdown allowed but no shutdown hook is wired; ignoring")
		return
	}
	go func() {
		if err := m.shutdown(ctx); err != nil {
			m.logger.Error("buzz monitor: shutdown hook failed", "err", err)
		}
	}()
}

// dispatch implements F11 (mint task ID, enqueue) with F8's ordinary
// author gate, F7's screening on the instruction text, F13's structured
// logging, and F16's typing-indicator kick-off. root is the thread root
// this dispatch's reply/continuation state keys off (computed by the
// caller -- handleChannelEvent -- either via F6's rootEventID for an
// explicit @mention, or via the matched KnownThread candidate for a P1.2
// thread-reply). When a BuzzTaskDispatcher is wired (WithTaskDispatcher),
// it routes through that bridge (P2.2) instead of building/sending the
// task message directly; otherwise it falls back to the pre-existing
// direct queue.Send behaviour.
func (m *Monitor) dispatch(ctx context.Context, channelUUID, root string, evt domain.Event) {
	text := strings.TrimSpace(evt.Content)
	if text == "" {
		return
	}

	// FR-005: reject oversized content before it is ever used to build a
	// task instruction. Checked ahead of the author gate deliberately -- an
	// oversized-content attempt is worth logging regardless of who sent it,
	// and there is no reason to spend a gate check on content that will be
	// rejected anyway.
	if n := len(evt.Content); n > maxContentLen {
		m.logger.Warn("buzz monitor: event content exceeds max size, rejecting",
			"event_id", evt.ID, "content_len", n, "max_content_len", maxContentLen,
			"channel", channelUUID)
		return
	}

	if !m.gate.allows(evt.PubKey) {
		m.logger.Warn("buzz monitor: mention rejected by author gate",
			"pubkey", evt.PubKey, "channel", channelUUID, "event_id", evt.ID)
		return
	}

	instruction := m.screen("message_body", text)

	if m.taskDispatcher != nil {
		m.dispatchViaBridge(ctx, channelUUID, root, evt, instruction)
		return
	}
	m.dispatchDirect(ctx, channelUUID, root, evt, instruction)
}

// dispatchDirect is the pre-existing direct queue.Send/Worker.Execute
// dispatch path (P1.0's trace target), preserved as the fallback for when
// no BuzzTaskDispatcher is wired.
func (m *Monitor) dispatchDirect(ctx context.Context, channelUUID, root string, evt domain.Event, instruction string) {
	taskID := uuid.New().String()
	payload := domain.TaskPayload{TaskID: taskID, Instruction: instruction}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		m.logger.Error("buzz monitor: marshal task payload", "err", err)
		return
	}

	msg := domain.Message{
		ID:        uuid.New().String(),
		Type:      domain.MessageTypeTask,
		From:      "buzz",
		To:        m.cfg.BotName,
		Payload:   payloadBytes,
		Timestamp: time.Now().UTC(),
	}

	if err := m.queue.Send(ctx, m.cfg.BotName, msg); err != nil {
		m.logger.Error("buzz monitor: send to queue", "err", err)
		return
	}

	m.awaitResult(ctx, taskID, channelUUID, root, evt.ID, evt.PubKey)

	m.logger.Info("buzz monitor: dispatched task",
		"task_id", taskID, "agent_pubkey", m.cfg.AgentPubKeyHex, "relay_url", m.cfg.RelayURL,
		"channel", channelUUID, "event_id", evt.ID)
}

// dispatchViaBridge routes an inbound mention/thread-reply through the
// domain.BuzzTaskDispatcher bridge (P2.2), replacing the direct
// queue.Send/Worker.Execute call dispatchDirect makes. The bridge decides
// immediate-vs-scheduled dispatch (reusing ChatTaskManager's NL-scheduling
// flow), creates the DirectTask/board item, and reports back what the
// monitor should do next: publish an immediate text reply (a confirmation
// prompt, cancellation ack, or scheduled-task ack), await the dispatched
// task's own result, both, or neither (a relay-replay duplicate). root
// (not channelUUID -- P1.1's fix) is passed as threadID: the stable
// per-conversation key the bridge uses for both NL-scheduling state and
// ChatStore history replay (FR-208).
func (m *Monitor) dispatchViaBridge(ctx context.Context, channelUUID, root string, evt domain.Event, instruction string) {
	result, err := m.taskDispatcher.Dispatch(ctx, m.cfg.BotName, evt.ID, root, instruction)
	if err != nil {
		m.logger.Error("buzz monitor: task dispatcher bridge failed",
			"err", err, "channel", channelUUID, "event_id", evt.ID)
		return
	}
	if result.Duplicate {
		m.logger.Info("buzz monitor: duplicate event, skipping re-dispatch",
			"channel", channelUUID, "event_id", evt.ID)
		return
	}

	if result.Reply != "" {
		// FR-301: this immediate reply (a scheduling confirmation prompt or
		// ack) has no DirectTask/TaskResultPayload, so it is never seen by
		// TeamManager.handleSharedTaskResult -- recordOutbound must run here,
		// not inside publishReply, so FR-206's ChatStore history replay still
		// sees it on a later turn. Only recorded on a successful publish,
		// matching publishReply's own pre-FR-301 behaviour for this path.
		if err := m.publishReply(ctx, channelUUID, root, evt.ID, evt.PubKey, result.Reply); err == nil {
			m.recordOutbound(ctx, root, m.cfg.BotName, result.Reply)
		} // publishReply logs its own failure internally
	}

	if result.TaskID != "" && result.AwaitResult {
		m.awaitResult(ctx, result.TaskID, channelUUID, root, evt.ID, evt.PubKey)
	}

	m.logger.Info("buzz monitor: dispatched task via bridge",
		"task_id", result.TaskID, "agent_pubkey", m.cfg.AgentPubKeyHex, "relay_url", m.cfg.RelayURL,
		"channel", channelUUID, "event_id", evt.ID, "await_result", result.AwaitResult)
}

// awaitResult registers taskID in the pending map and starts F16's
// typing-indicator loop, so a later HandleResult call for this taskID
// publishes a threaded reply -- shared by both the direct and bridge
// dispatch paths. parentEventID/authorPubKey carry FR-207's complete NIP-10
// tagging through to the eventual publishReply call.
func (m *Monitor) awaitResult(ctx context.Context, taskID, channelUUID, root, parentEventID, authorPubKey string) {
	typingDone := make(chan struct{})
	m.mu.Lock()
	m.pending[taskID] = &pendingEntry{
		target: replyTarget{
			channelUUID:   channelUUID,
			rootEventID:   root,
			parentEventID: parentEventID,
			authorPubKey:  authorPubKey,
		},
		typingDone: typingDone,
	}
	m.mu.Unlock()

	go m.typingLoop(ctx, channelUUID, typingDone)
}

// publishReply publishes content as a threaded kind:9 reply into
// channelUUID, and returns any publish error (logged by the caller with its
// own context -- e.g. HandleResult's FR-032 task_id-tagged success log).
// Shared by HandleResult (F12, an eventual task result) and
// dispatchViaBridge (an immediate bridge-produced Reply, e.g. a scheduling
// confirmation prompt) so both use identical event tagging.
//
// FR-301: unlike before, this method does NOT call recordOutbound itself.
// HandleResult's task-completion replies are now recorded exactly once by
// internal/application/team.TeamManager.handleSharedTaskResult (via
// chatMessageThreadID passing the real Buzz ThreadID through) -- a second,
// unconditional recordOutbound write here would reintroduce the duplicate
// chat-store row that finding fixed. dispatchViaBridge's immediate-Reply
// branch (a scheduling confirmation prompt or ack) has no corresponding
// DirectTask/TaskResultPayload for that handler to ever see, so it calls
// recordOutbound itself, at its own call site, after a successful publish.
//
// FR-207: the event carries complete NIP-10 threading metadata -- a
// root-marked `e` tag (root), a reply-marked `e` tag for the immediate
// parent (parentEventID, the triggering mention/thread-reply's own event),
// and a `p` tag back to that event's author (authorPubKey). The reply tag
// is omitted when parentEventID equals root (replying directly to the
// thread's own root needs only the one root-marked `e` tag per NIP-10 --
// a duplicate reply tag pointing at the same ID adds nothing). The p tag
// is omitted when authorPubKey is empty (defensive; every real call site
// supplies it).
func (m *Monitor) publishReply(ctx context.Context, channelUUID, root, parentEventID, authorPubKey, content string) error {
	tags := [][]string{
		{"h", channelUUID},
		{"e", root, "", "root"},
	}
	if parentEventID != "" && parentEventID != root {
		tags = append(tags, []string{"e", parentEventID, "", "reply"})
	}
	if authorPubKey != "" {
		tags = append(tags, []string{"p", authorPubKey})
	}
	reply := domain.Event{
		Kind:    kindChannelMessage,
		Tags:    tags,
		Content: content,
	}
	if err := m.relay.Publish(ctx, reply); err != nil {
		m.logger.Warn("buzz monitor: publish reply failed",
			"channel", channelUUID, "err", err)
		return err
	}
	return nil
}

// recordOutbound appends a bot-originated reply to ChatStore (FR-206), when
// one is wired (WithChatStore) and threadID is non-empty. A ChatStore
// append failure is logged and non-fatal -- the reply has already been
// published; a history-recording failure must not be reported as a publish
// failure.
//
// FR-301: called ONLY from the immediate-bridge-reply call sites
// (dispatchViaBridge and dm.go's handleDMEvent, both after a successful
// publish) -- never from publishReply/publishDMReply themselves, and never
// from HandleResult's task-completion path. A task's own completion message
// is recorded exactly once, by internal/application/team.TeamManager.
// handleSharedTaskResult, using the task's real Buzz ThreadID (see
// team_manager.go's chatMessageThreadID). Calling recordOutbound again here
// for that path would reintroduce the duplicate-chat-row bug this finding
// fixed -- see HandleResult's doc comment.
func (m *Monitor) recordOutbound(ctx context.Context, threadID, botName, content string) {
	if m.chatStore == nil || threadID == "" {
		return
	}
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   botName,
		Direction: domain.ChatDirectionInbound,
		Content:   content,
	}
	if err := m.chatStore.Append(ctx, msg); err != nil {
		m.logger.Warn("buzz monitor: failed to append outbound chat message", "thread_id", threadID, "err", err)
	}
}

// HandleResult implements domain.ChannelMonitor / F12. Unmatched task IDs
// (results from another channel) are ignored silently. A reply-publish
// failure is logged and does NOT re-enqueue the task -- the worker already
// ran; re-running would duplicate side effects (architecture.md "FR-022
// reply-publish failure"). The pending-map entry is popped regardless of
// publish outcome, matching the Slack adapter's fire-and-forget reply
// semantics.
//
// FR-301: this method publishes the task's reply via publishReply/
// publishDMReply but deliberately does not record it to ChatStore itself
// (neither method calls recordOutbound for this path anymore) --
// TeamManager.handleSharedTaskResult, which runs before this method is ever
// reached (it triggers forwardResultToMonitors), already recorded the same
// content under the task's real Buzz ThreadID. A second write here would
// duplicate that row in GET /api/v1/chat, which is exactly the bug this
// finding fixed.
func (m *Monitor) HandleResult(ctx context.Context, p domain.TaskResultPayload) {
	m.mu.Lock()
	entry, ok := m.pending[p.TaskID]
	if ok {
		delete(m.pending, p.TaskID)
		if entry.typingDone != nil {
			close(entry.typingDone) // F16: stop typing, under the same lock that pops the entry
		}
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	output := p.Output
	if !p.Success && p.Error != "" {
		output = p.Error
	}
	if output == "" {
		output = "(no output)"
	}

	// P2.4: a DM-dispatched task (registered via awaitDMResult) publishes
	// through the gift-wrap DM reply path, not the channel-shaped
	// publishReply -- FR-209.
	if entry.dmTarget != nil {
		if err := m.publishDMReply(ctx, entry.dmTarget.recipientPubKey, entry.dmTarget.threadID, output); err != nil {
			m.logger.Warn("buzz monitor: dm task result publish failed", "task_id", p.TaskID)
			return
		}
		m.logger.Info("buzz monitor: published dm reply",
			"task_id", p.TaskID, "agent_pubkey", m.cfg.AgentPubKeyHex, "relay_url", m.cfg.RelayURL)
		return
	}

	if err := m.publishReply(ctx, entry.target.channelUUID, entry.target.rootEventID, entry.target.parentEventID, entry.target.authorPubKey, output); err != nil {
		// publishReply already logged the failure; add the task_id this
		// call site alone knows, for FR-032 traceability.
		m.logger.Warn("buzz monitor: task result publish failed", "task_id", p.TaskID)
		return
	}

	// FR-032: agent pubkey, relay URL, channel UUID, event ID for every
	// published reply. Publish returns only an error (it signs internally
	// and doesn't hand back the new event's own ID), so event_id here is
	// the triggering mention's root event -- the ID that reconciles with
	// relay-side audit records for this conversation, per FR-032's intent.
	m.logger.Info("buzz monitor: published reply",
		"task_id", p.TaskID, "agent_pubkey", m.cfg.AgentPubKeyHex, "relay_url", m.cfg.RelayURL,
		"channel", entry.target.channelUUID, "event_id", entry.target.rootEventID)
}

// typingLoop implements F16: publishes a kind:20002 typing indicator
// immediately at dispatch time, then re-publishes on an interval until
// done is closed (by HandleResult), ctx is done (e.g. the owning channel
// subscription was canceled by F3's unsubscribe), or maxTypingDuration
// elapses -- a safety bound against a runaway indicator if a task result
// is somehow never delivered.
func (m *Monitor) typingLoop(ctx context.Context, channelUUID string, done <-chan struct{}) {
	ctx, cancel := context.WithTimeout(ctx, maxTypingDuration)
	defer cancel()

	m.publishTyping(ctx, channelUUID)

	t := m.newTicker(typingInterval)
	defer t.stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.c():
			m.publishTyping(ctx, channelUUID)
		}
	}
}

func (m *Monitor) publishTyping(ctx context.Context, channelUUID string) {
	evt := domain.Event{Kind: kindTyping, Tags: [][]string{{"h", channelUUID}}, Content: "typing"}
	if err := m.relay.Publish(ctx, evt); err != nil {
		m.logger.Warn("buzz monitor: publish typing indicator failed", "channel", channelUUID, "err", err)
	}
}

const (
	typingInterval    = 15 * time.Second
	maxTypingDuration = 30 * time.Minute
)
