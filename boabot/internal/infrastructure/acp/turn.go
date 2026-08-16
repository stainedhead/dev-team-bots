package acp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// boardItemTitleMaxLen bounds titleFromInstruction's output -- mirrors
// internal/application/orchestrator/buzz_task_bridge.go's identical
// boardItemTitleMaxLen constant for native mode's Buzz-dispatched board
// items.
const boardItemTitleMaxLen = 80

// Prompt implements sdk.Agent's session/prompt handling: builds a
// domain.Task from the prompt content, executes it against the existing
// Worker, and maps the result back to an ACP response -- architecture.md's
// Data Flow section 4-6.
//
// specs/260816-acp-native-shared-state/spec.md added three optional stages,
// each independently gated on its own Agent field so a persona with none of
// them wired behaves exactly as before this feature (NFR-Correctness):
//
//   - FR-503: the raw inbound message is recorded to chatStore (if set) and
//     the eventual Worker instruction is built by replaying recent thread
//     history, mirroring BuzzTaskBridge.buildInstructionWithHistory.
//   - FR-504: if chatTaskManager is set, ChatTaskManager.DetectAndHandle runs
//     as a synchronous pre-check on the raw instruction -- a scheduling
//     confirmation short-circuits the turn without ever calling
//     worker.Execute, exactly mirroring BuzzTaskBridge.Dispatch's identical
//     use of the same ChatTaskManager. See noImmediateDispatchQueue (dispatch.go)
//     for why this pre-check cannot resolve to an ASAP/immediate schedule.
//   - FR-504a: if taskStore and board are both set, every turn that reaches
//     worker.Execute records a real DirectTask and Kanban board item,
//     updated to its final status/output when the turn completes.
func (a *Agent) Prompt(ctx context.Context, params sdk.PromptRequest) (sdk.PromptResponse, error) {
	a.mu.Lock()
	s, ok := a.sessions[params.SessionId]
	a.mu.Unlock()
	if !ok {
		return sdk.PromptResponse{}, fmt.Errorf("acp: unknown session %q", params.SessionId)
	}

	// Serializes turn execution across every session on this Agent -- see
	// the turnMu field doc comment on Agent for why this is required, not
	// just a throughput choice.
	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	turnCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.mu.Lock()
	s.cancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		s.cancel = nil
		a.mu.Unlock()
	}()

	rawInstruction := extractText(params.Prompt)
	threadID := a.resolveThreadID(rawInstruction, params.SessionId)
	a.recordChatMessage(turnCtx, threadID, domain.ChatDirectionOutbound, rawInstruction)

	// FR-504: scheduling-intent pre-check. A "handled" result ends the turn
	// here, whether it's a confirmation prompt, a cancellation ack, or an
	// actually-confirmed dispatch -- worker.Execute is never reached for any
	// of these (NFR-Correctness's flip side: this must NOT slow down or
	// alter the non-scheduling path below, which it doesn't touch at all).
	if a.chatTaskManager != nil {
		resp, handled, scheduled, detectErr := a.chatTaskManager.DetectAndHandle(turnCtx, threadID, rawInstruction, domain.DirectTaskSourceACP)
		if handled {
			reply := resp
			if detectErr != nil {
				reply = schedulingFailureMessage(detectErr)
			}
			if scheduled != nil {
				a.recordScheduledTask(turnCtx, *scheduled, rawInstruction)
			}
			a.recordChatMessage(turnCtx, threadID, domain.ChatDirectionInbound, reply)
			a.emit(context.Background(), params.SessionId, sdk.UpdateAgentMessageText(reply))
			slog.Info("acp turn finished", "session_id", params.SessionId,
				"stop_reason", sdk.StopReasonEndTurn, "scheduling_handled", true)
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
	}

	task := domain.Task{
		ID:          randomID(),
		Instruction: a.buildInstructionWithHistory(turnCtx, threadID, rawInstruction),
		Source:      "acp",
		WorkDir:     a.workDir,
	}

	slog.Info("acp turn started", "session_id", params.SessionId, "task_id", task.ID)

	directTaskID, boardItemID := a.startDirectTask(turnCtx, threadID, rawInstruction)

	// Keep-alive: a progress-driven signal when the Worker offers one, plus
	// a ticker fallback for a single long silent tool call with no
	// intermediate progress events -- architecture.md AD-3. This is a
	// correctness requirement: buzz-acp's --idle-timeout kills a turn after
	// N seconds of stdout silence.
	progress := make(chan string, 16)
	if pr, ok := a.worker.(progressReporter); ok {
		pr.WithProgressHandler(func(_, line string) {
			select {
			case progress <- line:
			default: // never block the worker on a full channel
			}
		})
	}
	keepAliveDone := make(chan struct{})
	var keepAliveWG sync.WaitGroup
	keepAliveWG.Add(1)
	go func() {
		defer keepAliveWG.Done()
		a.keepAlive(turnCtx, params.SessionId, progress, keepAliveDone)
	}()

	result, execErr := a.runWorkerSafely(turnCtx, task)
	close(keepAliveDone)
	// Wait for the keep-alive goroutine to fully exit before emitting the
	// final update or returning, so no session/update can arrive after this
	// call has already responded (deterministic ordering, and avoids a
	// dangling background write racing with whatever the caller does next).
	keepAliveWG.Wait()

	// Recorded regardless of outcome (including cancellation, handled
	// below) -- an operator watching the board must see a stuck/failed/
	// cancelled ACP task, not just successful ones. Uses a fresh background
	// context, matching this function's existing convention (see the
	// fallback-publish and emit calls below) of not depending on turnCtx
	// once the worker call itself has returned.
	a.finishDirectTask(context.Background(), directTaskID, boardItemID, result, execErr)

	if turnCtx.Err() != nil {
		// Either session/cancel fired, or the parent context was cancelled.
		slog.Warn("acp turn cancelled", "session_id", params.SessionId, "task_id", task.ID)
		return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
	}
	if execErr != nil || !result.Success {
		// A domain-level failure (including a recovered panic) ends the
		// turn without a protocol-level error -- FR-008: never surface a
		// raw crash to the host, always a well-formed ACP response. It must
		// not be silent, though: without an emitted message, a human
		// watching the channel sees the keep-alive "thinking" indicator and
		// then nothing at all, with no way to tell anything went wrong.
		slog.Info("acp turn finished", "session_id", params.SessionId, "task_id", task.ID,
			"stop_reason", sdk.StopReasonRefusal, "err", execErr)
		failureMsg := turnFailureMessage(execErr, result)
		a.recordChatMessage(context.Background(), threadID, domain.ChatDirectionInbound, failureMsg)
		a.emit(context.Background(), params.SessionId, sdk.UpdateAgentMessageText(failureMsg))
		return sdk.PromptResponse{StopReason: sdk.StopReasonRefusal}, nil
	}

	// Fallback publish: a turn can produce a perfectly good reply while the
	// model itself never calls the buzz CLI to actually publish it --
	// function-calling models only invoke a tool when they judge it
	// necessary, and a casual-seeming reply doesn't always clear that bar
	// even though the system prompt says it must. Without this, that reply
	// is only ever visible as an ephemeral ACP session/update, never a real
	// channel message. Best-effort: a failure here is logged, not fatal --
	// the turn still finishes normally and the text still reaches the
	// human via the emit below either way.
	//
	// Uses rawInstruction, not task.Instruction: the latter may now be
	// history-augmented (FR-503), and a channel UUID from replayed prior
	// conversation text must never be mistaken for this turn's own channel.
	if result.Output != "" && !calledPublish(result.ToolCalls) {
		if channelID, ok := extractChannelID(rawInstruction); ok {
			if pubErr := a.publisher.Publish(context.Background(), channelID, result.Output); pubErr != nil {
				slog.Warn("acp fallback publish failed", "session_id", params.SessionId, "task_id", task.ID, "err", pubErr)
			} else {
				slog.Info("acp fallback publish succeeded", "session_id", params.SessionId, "task_id", task.ID)
			}
		}
	}

	a.recordChatMessage(context.Background(), threadID, domain.ChatDirectionInbound, result.Output)
	a.emit(context.Background(), params.SessionId, sdk.UpdateAgentMessageText(result.Output))

	slog.Info("acp turn finished", "session_id", params.SessionId, "task_id", task.ID,
		"stop_reason", sdk.StopReasonEndTurn)
	return sdk.PromptResponse{
		StopReason: sdk.StopReasonEndTurn,
		// Usage intentionally left nil: no cost-enforcement is wired into
		// the live task path in either mode to source it from -- corrected
		// FR-005, see specs/archive/260813-boabot-acp-stdio-harness-support/spec.md.
	}, nil
}

// turnFailureMessage builds a human-readable explanation for a failed turn,
// preferring the returned error, then domain.TaskResult.Err, then a generic
// fallback -- there is always something to show, never a bare silence.
func turnFailureMessage(execErr error, result domain.TaskResult) string {
	if execErr != nil {
		return fmt.Sprintf("Sorry, I couldn't complete that: %v", execErr)
	}
	if result.Err != nil {
		return fmt.Sprintf("Sorry, I couldn't complete that: %v", result.Err)
	}
	return "Sorry, I couldn't complete that task."
}

// schedulingFailureMessage builds a human-readable explanation for a failed
// ChatTaskManager.DetectAndHandle dispatch (turn.go's scheduling pre-check),
// special-casing errImmediateDispatchUnsupported (dispatch.go) with a clear
// explanation instead of a raw wrapped error string.
func schedulingFailureMessage(err error) string {
	if errors.Is(err, errImmediateDispatchUnsupported) {
		return "I can only schedule recurring or future tasks here -- I can't immediately hand this off to another bot in this mode."
	}
	return fmt.Sprintf("Sorry, I couldn't create that task: %v", err)
}

// runWorkerSafely recovers a panicking Worker so it surfaces as an error
// (mapped to StopReasonRefusal by the caller) instead of crashing the
// long-lived ACP process -- FR-008.
func (a *Agent) runWorkerSafely(ctx context.Context, task domain.Task) (result domain.TaskResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("acp worker panic recovered", "task_id", task.ID, "panic", r)
			err = fmt.Errorf("acp: worker panic: %v", r)
		}
	}()
	return a.worker.Execute(ctx, task)
}

// keepAlive emits acp::thought session/update notifications until done is
// closed, sourced from progress (when the Worker supports it) or a plain
// ticker otherwise.
func (a *Agent) keepAlive(ctx context.Context, sid sdk.SessionId, progress <-chan string, done <-chan struct{}) {
	interval := a.keepAliveInterval
	if interval <= 0 {
		interval = defaultKeepAliveInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case line := <-progress:
			a.emit(ctx, sid, sdk.UpdateAgentThoughtText(line))
		case <-ticker.C:
			a.emit(ctx, sid, sdk.UpdateAgentThoughtText("still working"))
		}
	}
}

// emit sends a session/update, silently doing nothing if no connection is
// wired (e.g. a unit test that never called setUpdater/SetConnection).
func (a *Agent) emit(ctx context.Context, sid sdk.SessionId, update sdk.SessionUpdate) {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: sid, Update: update})
}

// extractText joins every text content block in a prompt into one
// instruction string. buzz-acp assembles the full prompt (platform context,
// team instructions, memory, persona system prompt, user message) before
// sending it -- BaoBot treats it as opaque text, per research.md.
func extractText(blocks []sdk.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Text != nil {
			parts = append(parts, b.Text.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// resolveThreadID picks the conversation identity FR-503/504/504a key all
// their per-thread state on: the buzz-acp [Context] channel UUID
// (extractChannelID) when present, since that identifies the same Buzz
// channel/DM across every turn and every ACP worker process sharing this
// persona's ChatStore; the ACP session ID otherwise, so a channel-less
// session (e.g. a bare stdio ACP client with no buzz-acp [Context] block)
// still gets consistent per-session history/scheduling state instead of
// none at all.
func (a *Agent) resolveThreadID(rawInstruction string, sid sdk.SessionId) string {
	if channelID, ok := extractChannelID(rawInstruction); ok {
		return channelID
	}
	return string(sid)
}

// recordChatMessage appends msg to chatStore (FR-503), a no-op if chatStore
// is unset, threadID is empty, or content is empty. A write failure is
// logged, not fatal -- history recording must never block a turn.
func (a *Agent) recordChatMessage(ctx context.Context, threadID string, direction domain.ChatDirection, content string) {
	if a.chatStore == nil || threadID == "" || content == "" {
		return
	}
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   a.botName,
		Direction: direction,
		Content:   content,
	}
	if err := a.chatStore.Append(ctx, msg); err != nil {
		slog.Warn("acp mode: failed to append chat message", "bot", a.botName, "thread_id", threadID, "err", err)
	}
}

// buildInstructionWithHistory returns instruction unchanged when chatStore
// is nil, threadID is empty, the history read fails, or there is no prior
// history; otherwise it prepends a "Prior conversation context" block built
// from up to the last 10 prior messages in threadID -- an exact mirror of
// internal/application/orchestrator/buzz_task_bridge.go's
// buildInstructionWithHistory (FR-503, reusing the identical pattern rather
// than inventing a new one). Must be called after recordChatMessage has
// already appended this turn's own inbound message, so history[0] is that
// just-appended message and the window below correctly starts at index 1.
func (a *Agent) buildInstructionWithHistory(ctx context.Context, threadID, instruction string) string {
	if a.chatStore == nil || threadID == "" {
		return instruction
	}
	history, err := a.chatStore.List(ctx, threadID)
	if err != nil || len(history) <= 1 {
		return instruction
	}
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

// recordScheduledTask corrects a ChatTaskManager-confirmed task's BotName to
// this Agent's own identity -- ACP mode is single-persona, so the NL
// heuristic parser's guess at a bot name (which may be empty, or name a
// different bot entirely -- native mode's multi-bot delegation concept that
// does not apply here) must never override it, unlike native mode's
// BuzzTaskBridge, which legitimately trusts the parsed name for cross-bot
// delegation. Then records the FR-504a board item for the scheduled task
// (status Backlog, since it has not run yet).
func (a *Agent) recordScheduledTask(ctx context.Context, task domain.DirectTask, rawInstruction string) {
	if a.taskStore != nil && task.BotName != a.botName {
		task.BotName = a.botName
		if updated, err := a.taskStore.Update(ctx, task); err != nil {
			slog.Warn("acp mode: failed to correct scheduled task bot_name", "bot", a.botName, "task_id", task.ID, "err", err)
		} else {
			task = updated
		}
	}
	a.createBoardItemForTask(ctx, task, rawInstruction, domain.WorkItemStatusBacklog)
}

// startDirectTask records a Running DirectTask and an in-progress board
// item for this turn (FR-504a), if both a.taskStore and a.board are
// configured. Returns empty IDs (and finishDirectTask/no board update
// follows) when recording is disabled, or if either write fails -- a
// write failure here is logged, not fatal, mirroring
// BuzzTaskBridge.createBoardItem's identical non-fatal treatment.
func (a *Agent) startDirectTask(ctx context.Context, threadID, rawInstruction string) (directTaskID, boardItemID string) {
	if a.taskStore == nil || a.board == nil {
		return "", ""
	}
	now := time.Now().UTC()
	created, err := a.taskStore.Create(ctx, domain.DirectTask{
		BotName:      a.botName,
		Source:       domain.DirectTaskSourceACP,
		ThreadID:     threadID,
		Instruction:  rawInstruction,
		Status:       domain.DirectTaskStatusRunning,
		DispatchedAt: &now,
	})
	if err != nil {
		slog.Warn("acp mode: failed to record direct task", "bot", a.botName, "err", err)
		return "", ""
	}
	itemID := a.createBoardItemForTask(ctx, created, rawInstruction, domain.WorkItemStatusInProgress)
	return created.ID, itemID
}

// finishDirectTask updates directTaskID's status/output to reflect the
// turn's outcome and moves boardItemID to a terminal board status
// (FR-504a). A no-op if directTaskID is empty (recording was never
// activated, or its initial Create already failed). Failures here are
// logged, not fatal -- the turn's own response to the human is already
// decided independently of this bookkeeping.
func (a *Agent) finishDirectTask(ctx context.Context, directTaskID, boardItemID string, result domain.TaskResult, execErr error) {
	if a.taskStore == nil || directTaskID == "" {
		return
	}
	task, err := a.taskStore.Get(ctx, directTaskID)
	if err != nil {
		slog.Warn("acp mode: failed to load direct task for completion update", "task_id", directTaskID, "err", err)
		return
	}
	now := time.Now().UTC()
	task.CompletedAt = &now
	boardStatus := domain.WorkItemStatusDone
	if execErr != nil || !result.Success {
		task.Status = domain.DirectTaskStatusFailed
		boardStatus = domain.WorkItemStatusErrored
		switch {
		case execErr != nil:
			task.Output = execErr.Error()
		case result.Err != nil:
			task.Output = result.Err.Error()
		default:
			task.Output = result.Output
		}
	} else {
		task.Status = domain.DirectTaskStatusSucceeded
		task.Output = result.Output
	}

	if _, err := a.taskStore.Update(ctx, task); err != nil {
		slog.Warn("acp mode: failed to update direct task on completion", "task_id", directTaskID, "err", err)
	}
	a.finishBoardItem(ctx, boardItemID, boardStatus)
}

// createBoardItemForTask creates the Kanban board item FR-504a requires
// alongside a DirectTask, returning its ID (or "" if a.board is nil or the
// write fails -- logged, not fatal, mirroring
// BuzzTaskBridge.createBoardItem's identical non-fatal treatment).
func (a *Agent) createBoardItemForTask(ctx context.Context, task domain.DirectTask, rawInstruction string, status domain.WorkItemStatus) string {
	if a.board == nil {
		return ""
	}
	item := domain.WorkItem{
		Title:        titleFromInstruction(rawInstruction),
		Description:  rawInstruction,
		AssignedTo:   a.botName,
		Status:       status,
		ActiveTaskID: task.ID,
		CreatedBy:    "acp",
	}
	created, err := a.board.Create(ctx, item)
	if err != nil {
		slog.Warn("acp mode: failed to create board item for task", "bot", a.botName, "task_id", task.ID, "err", err)
		return ""
	}
	return created.ID
}

// finishBoardItem moves boardItemID to status, a no-op if a.board is nil or
// boardItemID is empty. A failure here is logged, not fatal.
func (a *Agent) finishBoardItem(ctx context.Context, boardItemID string, status domain.WorkItemStatus) {
	if a.board == nil || boardItemID == "" {
		return
	}
	item, err := a.board.Get(ctx, boardItemID)
	if err != nil {
		slog.Warn("acp mode: failed to load board item for completion update", "item_id", boardItemID, "err", err)
		return
	}
	item.Status = status
	if _, err := a.board.Update(ctx, item); err != nil {
		slog.Warn("acp mode: failed to update board item on completion", "item_id", boardItemID, "err", err)
	}
}

// titleFromInstruction derives a short board-item title from an ACP
// instruction -- an exact mirror of
// internal/application/orchestrator/buzz_task_bridge.go's buzzBoardTitle.
// Truncation is rune-safe: slicing by byte index can split a multi-byte
// UTF-8 rune in half, corrupting the persisted WorkItem.Title.
func titleFromInstruction(instruction string) string {
	title := strings.TrimSpace(instruction)
	if title == "" {
		return "ACP task"
	}
	if utf8.RuneCountInString(title) > boardItemTitleMaxLen {
		runes := []rune(title)
		title = strings.TrimSpace(string(runes[:boardItemTitleMaxLen])) + "…"
	}
	return title
}
