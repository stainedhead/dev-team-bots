package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	if eventID != "" && b.markSeenIfDuplicate(eventID) {
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

// markSeenIfDuplicate returns true (and does not mark) if eventID was
// already seen within the dedup TTL; otherwise it marks eventID seen now
// and returns false. Also lazily evicts expired entries so the map does not
// grow unbounded across a long-running process.
func (b *BuzzTaskBridge) markSeenIfDuplicate(eventID string) bool {
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

// buzzBoardTitle derives a short board-item title from the (already
// screened) Buzz instruction text.
func buzzBoardTitle(instruction string) string {
	title := strings.TrimSpace(instruction)
	if title == "" {
		return "Buzz task"
	}
	if len(title) > boardItemTitleMaxLen {
		title = strings.TrimSpace(title[:boardItemTitleMaxLen]) + "…"
	}
	return title
}
