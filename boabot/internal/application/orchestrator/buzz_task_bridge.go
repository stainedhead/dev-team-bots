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

	mu       sync.Mutex
	seenEvts map[string]time.Time
	dedupTTL time.Duration
}

var _ domain.BuzzTaskDispatcher = (*BuzzTaskBridge)(nil)

// NewBuzzTaskBridge constructs a BuzzTaskBridge with the default event-ID
// dedup TTL.
func NewBuzzTaskBridge(dispatcher domain.ScheduledTaskDispatcher, board domain.BoardStore, chatMgr *ChatTaskManager) *BuzzTaskBridge {
	return NewBuzzTaskBridgeWithDedupTTL(dispatcher, board, chatMgr, defaultEventDedupTTL)
}

// NewBuzzTaskBridgeWithDedupTTL constructs a BuzzTaskBridge with a custom
// event-ID dedup TTL. Use this in tests to exercise dedup-window expiry with
// a short duration.
func NewBuzzTaskBridgeWithDedupTTL(dispatcher domain.ScheduledTaskDispatcher, board domain.BoardStore, chatMgr *ChatTaskManager, dedupTTL time.Duration) *BuzzTaskBridge {
	return &BuzzTaskBridge{
		dispatcher: dispatcher,
		board:      board,
		chatMgr:    chatMgr,
		seenEvts:   make(map[string]time.Time),
		dedupTTL:   dedupTTL,
	}
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

	// Reuse ChatTaskManager's existing NL-scheduling/confirm-cancel flow
	// (P3.1) instead of building a second heuristic parser. A "handled"
	// result means either a confirmation prompt / cancellation ack (task
	// nil, just a Reply to publish) or an actually-confirmed dispatch (task
	// non-nil).
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

	// Not a scheduling request (or no ChatTaskManager wired): plain
	// immediate dispatch, tagged DirectTaskSourceBuzz.
	task, err := b.dispatcher.DispatchWithSchedule(ctx, botName, instruction,
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
