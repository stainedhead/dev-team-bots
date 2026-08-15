package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// defaultEventDedupTTL bounds how long a Nostr event ID is remembered for
// relay-replay dedup (spec.md's "Relay reconnect / message replay" edge
// case) -- matches ChatTaskManager's own defaultPendingTTL so both TTLs in
// the Buzz dispatch path behave consistently.
const defaultEventDedupTTL = defaultPendingTTL

const boardItemTitleMaxLen = 80

// BuzzTaskBridge implements domain.BuzzTaskDispatcher: it is the Buzz
// ChannelMonitor's seam into the orchestrator's Dispatcher/DirectTaskStore/
// BoardStore pipeline (FR-005), replacing the direct Worker.Execute call
// P1.0 found at internal/infrastructure/buzz/monitor.go's old dispatch().
//
// It reuses ChatTaskManager's existing NL-scheduling/confirm-cancel flow
// (P3.1) rather than building a second heuristic parser, tagging every
// resulting DirectTask domain.DirectTaskSourceBuzz. Each Buzz-enabled
// persona gets its own BuzzTaskBridge instance (own event-ID dedup state,
// own ChatTaskManager pending-intent map) -- see
// specs/260814-boabot-native-daemon-mode/implementation-notes.md's
// "Per-persona isolation" decision for why a shared instance would allow
// cross-persona confirmation cross-talk (FR-004).
type BuzzTaskBridge struct {
	dispatcher domain.ScheduledTaskDispatcher
	board      domain.BoardStore
	chatMgr    *ChatTaskManager

	// chatStore, when set (NewBuzzTaskBridgeWithChatStore), backs FR-206's
	// conversation continuation: Dispatch records the inbound message
	// against threadID and builds each dispatched instruction by replaying
	// that thread's recent history, mirroring
	// internal/infrastructure/http/server.go's handleChatSend "Prior
	// conversation" pattern (architecture.md's RQ1 resolution -- reuse
	// ChatStore, no new session machinery). nil is safe -- every use
	// degrades to the pre-existing no-history-replay behaviour.
	chatStore domain.ChatStore

	mu       sync.Mutex
	seenEvts map[string]time.Time
	dedupTTL time.Duration

	// dispatchedThreads tracks, per persona (keyed "botName|threadID"),
	// that this bridge has dispatched (or attempted to dispatch) within a
	// thread/conversation -- backs KnownThread (P1.3/FR-205), which
	// Monitor's triggerThreadReply classification consults to recognize an
	// in-thread reply without a fresh @mention. Sibling to seenEvts
	// (architecture.md: "keeps all per-persona dispatch-tracking state in
	// one place"). Deliberately has no TTL/eviction, unlike seenEvts:
	// architecture.md's RQ1 resolution explicitly accepts that a
	// conversation "naturally" fades as ChatStore history scrolls past the
	// 10-message replay window, rather than needing a separate
	// dormancy/timeout mechanism here.
	dispatchedThreads map[string]time.Time
}

var _ domain.BuzzTaskDispatcher = (*BuzzTaskBridge)(nil)

// NewBuzzTaskBridge constructs a BuzzTaskBridge with the default event-ID
// dedup TTL and no ChatStore (history replay disabled -- see
// NewBuzzTaskBridgeWithChatStore).
func NewBuzzTaskBridge(dispatcher domain.ScheduledTaskDispatcher, board domain.BoardStore, chatMgr *ChatTaskManager) *BuzzTaskBridge {
	return NewBuzzTaskBridgeWithDedupTTL(dispatcher, board, chatMgr, defaultEventDedupTTL)
}

// NewBuzzTaskBridgeWithDedupTTL constructs a BuzzTaskBridge with a custom
// event-ID dedup TTL. Use this in tests to exercise dedup-window expiry with
// a short duration.
func NewBuzzTaskBridgeWithDedupTTL(dispatcher domain.ScheduledTaskDispatcher, board domain.BoardStore, chatMgr *ChatTaskManager, dedupTTL time.Duration) *BuzzTaskBridge {
	return &BuzzTaskBridge{
		dispatcher:        dispatcher,
		board:             board,
		chatMgr:           chatMgr,
		seenEvts:          make(map[string]time.Time),
		dedupTTL:          dedupTTL,
		dispatchedThreads: make(map[string]time.Time),
	}
}

// NewBuzzTaskBridgeWithChatStore constructs a BuzzTaskBridge exactly like
// NewBuzzTaskBridge, additionally wiring chatStore for FR-206's ChatStore
// history-replay conversation continuation (P1.5). Kept as a separate
// constructor (rather than changing NewBuzzTaskBridge's signature) so
// existing callers/tests are unaffected.
func NewBuzzTaskBridgeWithChatStore(dispatcher domain.ScheduledTaskDispatcher, board domain.BoardStore, chatMgr *ChatTaskManager, chatStore domain.ChatStore) *BuzzTaskBridge {
	b := NewBuzzTaskBridge(dispatcher, board, chatMgr)
	b.chatStore = chatStore
	return b
}

// Dispatch implements domain.BuzzTaskDispatcher.
func (b *BuzzTaskBridge) Dispatch(ctx context.Context, botName, eventID, threadID, instruction string) (domain.BuzzDispatchResult, error) {
	// checkAndMarkSeen is an atomic check-and-set: it marks eventID seen as
	// part of the same locked operation that checks it, so two concurrent
	// Dispatch calls for the identical event ID cannot both proceed (FR-101).
	// If the dispatch that follows fails, unmarkEvent below undoes the mark
	// so a later relay reconnect/replay of the same event ID is retried, not
	// misreported as a harmless duplicate and silently dropped.
	if eventID != "" && b.checkAndMarkSeen(eventID) {
		return domain.BuzzDispatchResult{Duplicate: true}, nil
	}

	// P1.3: mark this persona as having dispatched (or attempted to) in
	// threadID, for a later triggerThreadReply classification to find via
	// KnownThread. P1.5: append the inbound message to ChatStore before
	// building/dispatching anything, so a later history replay for this
	// threadID (here or in a future turn) includes it.
	b.markDispatchedThread(botName, threadID)
	b.recordInbound(ctx, botName, threadID, instruction)

	// Reuse ChatTaskManager's existing NL-scheduling/confirm-cancel flow
	// (P3.1) instead of building a second heuristic parser. A "handled"
	// result means either a confirmation prompt / cancellation ack (task
	// nil, just a Reply to publish) or an actually-confirmed dispatch (task
	// non-nil). Scheduling-intent detection runs on the raw instruction,
	// not the history-augmented one below -- prepending prior-conversation
	// text ahead of it would risk defeating the heuristic parser.
	if b.chatMgr != nil {
		resp, handled, task, err := b.chatMgr.DetectAndHandle(ctx, threadID, instruction, domain.DirectTaskSourceBuzz)
		if err != nil {
			b.unmarkEvent(eventID)
			return domain.BuzzDispatchResult{}, fmt.Errorf("buzz task bridge: schedule detection: %w", err)
		}
		if handled {
			result := domain.BuzzDispatchResult{Reply: resp}
			if task != nil {
				result.TaskID = task.ID
				result.AwaitResult = task.Status == domain.DirectTaskStatusRunning
				b.createBoardItem(ctx, botName, instruction, *task)
			}
			return result, nil
		}
	}

	// P1.5/FR-206: build the dispatched instruction by replaying threadID's
	// recent ChatStore history, mirroring handleChatSend's "Prior
	// conversation" pattern. The board item below still uses the raw
	// (un-augmented) instruction -- board titles/descriptions should stay
	// concise, not carry the replayed history block.
	dispatchInstruction := b.buildInstructionWithHistory(ctx, threadID, instruction)

	// Not a scheduling request (or no ChatTaskManager wired): plain
	// immediate dispatch, tagged DirectTaskSourceBuzz.
	task, err := b.dispatcher.DispatchWithSchedule(ctx, botName, dispatchInstruction,
		domain.Schedule{Mode: domain.ScheduleModeASAP}, domain.DirectTaskSourceBuzz, threadID, "", "")
	if err != nil {
		// See the FR-101 comment above: a failed dispatch must un-mark
		// eventID, or a later relay replay of the identical event would be
		// misreported as a harmless duplicate and the instruction
		// permanently lost.
		b.unmarkEvent(eventID)
		return domain.BuzzDispatchResult{}, fmt.Errorf("buzz task bridge: dispatch: %w", err)
	}
	b.createBoardItem(ctx, botName, instruction, task)

	return domain.BuzzDispatchResult{
		TaskID:      task.ID,
		AwaitResult: task.Status == domain.DirectTaskStatusRunning,
	}, nil
}

// KnownThread implements domain.BuzzTaskDispatcher (P1.3/FR-205). Strictly
// per-persona (keyed "botName|rootID") -- see dispatchedThreads' doc
// comment.
func (b *BuzzTaskBridge) KnownThread(botName, rootID string) bool {
	if rootID == "" {
		return false
	}
	key := botName + "|" + rootID
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.dispatchedThreads[key]
	return ok
}

// markDispatchedThread records botName as having dispatched within
// threadID, under the same lock dispatchedThreads is otherwise guarded by.
// A no-op for an empty threadID.
func (b *BuzzTaskBridge) markDispatchedThread(botName, threadID string) {
	if threadID == "" {
		return
	}
	key := botName + "|" + threadID
	b.mu.Lock()
	b.dispatchedThreads[key] = time.Now()
	b.mu.Unlock()
}

// recordInbound appends the inbound Buzz message to chatStore, keyed by
// threadID, before any instruction augmentation -- mirrors
// handleChatSend's Append-then-List ordering, so buildInstructionWithHistory
// (called after this, within the same Dispatch call) sees this message as
// the newest (index 0) entry in threadID's history and skips it when
// building the "prior conversation" block. A no-op when chatStore is nil or
// threadID is empty; a ChatStore write failure is logged, not fatal --
// history recording must never block dispatch.
func (b *BuzzTaskBridge) recordInbound(ctx context.Context, botName, threadID, instruction string) {
	if b.chatStore == nil || threadID == "" {
		return
	}
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   botName,
		Direction: domain.ChatDirectionOutbound,
		Content:   instruction,
	}
	if err := b.chatStore.Append(ctx, msg); err != nil {
		slog.Warn("buzz task bridge: failed to append inbound chat message", "bot", botName, "thread_id", threadID, "err", err)
	}
}

// buildInstructionWithHistory returns instruction unchanged when chatStore
// is nil, threadID is empty, the history read fails, or there is no prior
// history; otherwise it prepends a "Prior conversation context" block built
// from up to the last 10 prior messages in threadID -- an exact mirror of
// internal/infrastructure/http/server.go's handleChatSend (FR-206/RQ1's
// resolution: reuse ChatStore history replay, not new session/continuation
// machinery). A ChatStore read failure fails open (returns instruction
// unaugmented) -- a history-replay degradation must never block dispatch.
//
// Deviation from a literal line-for-line mirror of handleChatSend
// (documented in implementation-notes.md): handleChatSend's own windowing
// loop (`for i := len(history)-1; i >= 1 && len(prior) < 10; i--`) selects
// the 10 OLDEST prior messages once a thread exceeds 11 total messages, not
// the 10 most recent -- it stops capping only because in practice web-UI
// chat threads rarely exceed that length. Buzz threads/DMs are expected to
// run longer, so this instead explicitly windows to the 10 MOST RECENT
// prior messages (still assembled oldest-first for the replayed block),
// matching every FR/spec.md description of the intended behaviour ("last
// 10 messages", "recent ChatStore history").
func (b *BuzzTaskBridge) buildInstructionWithHistory(ctx context.Context, threadID, instruction string) string {
	if b.chatStore == nil || threadID == "" {
		return instruction
	}
	history, err := b.chatStore.List(ctx, threadID)
	if err != nil || len(history) <= 1 {
		return instruction
	}
	// history is newest-first; index 0 is the message recordInbound just
	// appended for this call, so the 10 most recent PRIOR messages are at
	// indices 1..min(10, len(history)-1). Walk that window from its oldest
	// end (highest index) down to index 1 to assemble prior in chronological
	// (oldest-first) order.
	end := len(history)
	if end > 11 {
		end = 11
	}
	var prior []domain.ChatMessage
	for i := end - 1; i >= 1; i-- {
		prior = append(prior, history[i])
	}
	if len(prior) == 0 {
		return instruction
	}
	var sb strings.Builder
	sb.WriteString("Prior conversation context (oldest first):\n")
	for _, m := range prior {
		who := "User"
		if m.Direction == domain.ChatDirectionInbound {
			who = m.BotName
		}
		fmt.Fprintf(&sb, "%s: %s\n", who, m.Content)
	}
	sb.WriteString("\nUser: ")
	sb.WriteString(instruction)
	return sb.String()
}

// createBoardItem creates the Kanban board item FR-005 requires alongside
// the DirectTask. A failure here is logged and non-fatal -- the task is
// already dispatched (or already visible in the Tasks UI via
// DirectTaskStore), and a board-item write failure must not roll that back
// or surface as a Dispatch error.
func (b *BuzzTaskBridge) createBoardItem(ctx context.Context, botName, instruction string, task domain.DirectTask) {
	if b.board == nil {
		return
	}
	status := domain.WorkItemStatusBacklog
	if task.Status == domain.DirectTaskStatusRunning {
		status = domain.WorkItemStatusInProgress
	}
	item := domain.WorkItem{
		Title:        buzzBoardTitle(instruction),
		Description:  instruction,
		AssignedTo:   botName,
		Status:       status,
		ActiveTaskID: task.ID,
		CreatedBy:    "buzz",
	}
	if _, err := b.board.Create(ctx, item); err != nil {
		slog.Warn("buzz task bridge: failed to create board item for dispatched task",
			"bot", botName, "task_id", task.ID, "err", err)
	}
}

// checkAndMarkSeen reports whether eventID was already seen within the
// dedup TTL; if not, it marks eventID seen now, in the same locked
// operation, and returns false. Keeping the check and the mark atomic under
// one lock acquisition (rather than two separate calls) is what makes this
// dedup safe against two concurrent Dispatch calls for the identical event
// ID -- only one of them can ever observe "not seen" and proceed. Callers
// that go on to fail their dispatch must call unmarkEvent (FR-101), so a
// later relay reconnect/replay of the same event ID is retried rather than
// misreported as a harmless duplicate. Also lazily evicts expired entries on
// every call so the map does not grow unbounded across a long-running
// process.
//
// FR-110: this eviction sweep scans the entire seenEvts map under b.mu on
// every single Dispatch call, so per-dispatch lock hold time grows linearly
// with the number of distinct events seen within the last dedupTTL window.
// Accepted as-is deliberately: at the default TTL (defaultEventDedupTTL) and
// realistic Buzz @mention volume per persona, that bound stays small (tens,
// not thousands, of entries), and a per-dispatch sweep is simpler to reason
// about than a periodic ticker-driven one. Revisit with a periodic sweep
// (every N calls, or a ticker) only if a persona's mention volume grows
// enough to make this measurably show up in profiling.
func (b *BuzzTaskBridge) checkAndMarkSeen(eventID string) bool {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, seenAt := range b.seenEvts {
		if b.dedupTTL > 0 && now.Sub(seenAt) > b.dedupTTL {
			delete(b.seenEvts, id)
		}
	}

	if seenAt, ok := b.seenEvts[eventID]; ok {
		if b.dedupTTL <= 0 || now.Sub(seenAt) <= b.dedupTTL {
			return true
		}
	}
	b.seenEvts[eventID] = now
	return false
}

// unmarkEvent removes eventID from the seen set. Called only when a dispatch
// checkAndMarkSeen guarded goes on to fail (FR-101) -- see checkAndMarkSeen's
// doc comment. A no-op for an empty eventID.
func (b *BuzzTaskBridge) unmarkEvent(eventID string) {
	if eventID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.seenEvts, eventID)
}

// buzzBoardTitle derives a short board-item title from the (already
// screened) Buzz instruction text. Truncation is rune-safe (FR-102): slicing
// by byte index can split a multi-byte UTF-8 rune in half, corrupting the
// persisted WorkItem.Title.
func buzzBoardTitle(instruction string) string {
	title := strings.TrimSpace(instruction)
	if title == "" {
		return "Buzz task"
	}
	if utf8.RuneCountInString(title) > boardItemTitleMaxLen {
		runes := []rune(title)
		title = strings.TrimSpace(string(runes[:boardItemTitleMaxLen])) + "…"
	}
	return title
}
