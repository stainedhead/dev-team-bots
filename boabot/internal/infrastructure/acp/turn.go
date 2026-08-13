package acp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Prompt implements sdk.Agent's session/prompt handling: builds a
// domain.Task from the prompt content, executes it against the existing
// Worker, and maps the result back to an ACP response -- architecture.md's
// Data Flow section 4-6.
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

	task := domain.Task{
		ID:          randomID(),
		Instruction: extractText(params.Prompt),
		Source:      "acp",
		WorkDir:     a.workDir,
	}

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

	if turnCtx.Err() != nil {
		// Either session/cancel fired, or the parent context was cancelled.
		return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
	}
	if execErr != nil || !result.Success {
		// A domain-level failure (including a recovered panic) ends the
		// turn without a protocol-level error -- FR-008: never surface a
		// raw crash to the host, always a well-formed ACP response.
		return sdk.PromptResponse{StopReason: sdk.StopReasonRefusal}, nil
	}

	a.emit(context.Background(), params.SessionId, sdk.UpdateAgentMessageText(result.Output))

	return sdk.PromptResponse{
		StopReason: sdk.StopReasonEndTurn,
		// Usage intentionally left nil: no domain.BudgetTracker exists in
		// this codebase to source it from -- corrected FR-005, see
		// specs/260813-boabot-acp-stdio-harness-support/spec.md.
	}, nil
}

// runWorkerSafely recovers a panicking Worker so it surfaces as an error
// (mapped to StopReasonRefusal by the caller) instead of crashing the
// long-lived ACP process -- FR-008.
func (a *Agent) runWorkerSafely(ctx context.Context, task domain.Task) (result domain.TaskResult, err error) {
	defer func() {
		if r := recover(); r != nil {
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
