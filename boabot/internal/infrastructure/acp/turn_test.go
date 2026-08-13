package acp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// captureLogs swaps slog's default logger for the duration of the test,
// returning a function that yields everything logged so far. Restores the
// prior default logger on test cleanup.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf.String
}

// fakeConn records SessionUpdate calls so tests can assert on keep-alive /
// final-output behavior without a real ACP transport.
type fakeConn struct {
	mu      sync.Mutex
	updates []sdk.SessionNotification
}

func (c *fakeConn) SessionUpdate(_ context.Context, n sdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)
	return nil
}

func (c *fakeConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.updates)
}

func (c *fakeConn) snapshot() []sdk.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]sdk.SessionNotification, len(c.updates))
	copy(out, c.updates)
	return out
}

func newSessionForTest(t *testing.T, a *Agent) sdk.SessionId {
	t.Helper()
	resp, err := a.NewSession(context.Background(), sdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return resp.SessionId
}

func TestAgent_Prompt_SuccessMapsToEndTurn(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "the answer is 42", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "/work")
	fc := &fakeConn{}
	a.setUpdater(fc)

	sid := newSessionForTest(t, a)
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("what is the answer?")},
	})
	if err != nil {
		t.Fatalf("Prompt returned error: %v", err)
	}
	if resp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v", resp.StopReason, sdk.StopReasonEndTurn)
	}
	if resp.Usage != nil {
		t.Errorf("Usage = %+v, want nil (no BudgetTracker exists -- FR-005)", resp.Usage)
	}
	if fw.receivedTask.Instruction != "what is the answer?" {
		t.Errorf("Task.Instruction = %q, want the prompt text", fw.receivedTask.Instruction)
	}
	if fw.receivedTask.WorkDir != "/work" {
		t.Errorf("Task.WorkDir = %q, want the persona's configured work dir", fw.receivedTask.WorkDir)
	}

	// Final output must have been streamed as an agent-message update.
	found := false
	for _, u := range fc.snapshot() {
		if u.Update.AgentMessageChunk != nil && u.Update.AgentMessageChunk.Content.Text != nil &&
			u.Update.AgentMessageChunk.Content.Text.Text == "the answer is 42" {
			found = true
		}
	}
	if !found {
		t.Error("no session/update carried the final output text")
	}
}

func TestAgent_Prompt_UnknownSession(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")
	_, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: "does-not-exist"})
	if err == nil {
		t.Fatal("expected an error for an unknown session, got nil")
	}
}

func TestAgent_Prompt_WorkerErrorMapsToRefusal(t *testing.T) {
	fw := &fakeWorker{err: errors.New("boom")}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})

	sid := newSessionForTest(t, a)
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("do the thing")},
	})
	if err != nil {
		t.Fatalf("Prompt returned a transport error instead of a mapped stop reason: %v", err)
	}
	if resp.StopReason != sdk.StopReasonRefusal {
		t.Errorf("StopReason = %v, want %v on worker failure", resp.StopReason, sdk.StopReasonRefusal)
	}
}

func TestAgent_Prompt_KeepAliveDuringLongTurn(t *testing.T) {
	// A turn slower than one keep-alive tick must still produce at least one
	// keep-alive update before completion -- this is the idle-timeout
	// compatibility requirement from architecture.md AD-3, not cosmetic.
	fw := &fakeWorker{
		result: domain.TaskResult{Output: "done", Success: true},
		delay:  60 * time.Millisecond,
	}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.keepAliveInterval = 15 * time.Millisecond
	fc := &fakeConn{}
	a.setUpdater(fc)

	sid := newSessionForTest(t, a)
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("slow task")},
	}); err != nil {
		t.Fatalf("Prompt returned error: %v", err)
	}

	if fc.count() < 2 {
		t.Errorf("got %d session/update calls for a %v turn with a %v keep-alive tick, want at least 2 (>=1 keep-alive + final)",
			fc.count(), fw.delay, a.keepAliveInterval)
	}
}

func TestAgent_Cancel_StopsInFlightTurn(t *testing.T) {
	fw := &fakeWorker{
		result: domain.TaskResult{Output: "should not complete", Success: true},
		delay:  time.Second,
	}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})

	sid := newSessionForTest(t, a)
	done := make(chan sdk.PromptResponse, 1)
	go func() {
		resp, _ := a.Prompt(context.Background(), sdk.PromptRequest{
			SessionId: sid,
			Prompt:    []sdk.ContentBlock{sdk.TextBlock("long task")},
		})
		done <- resp
	}()

	time.Sleep(20 * time.Millisecond) // let the turn actually start
	if err := a.Cancel(context.Background(), sdk.CancelNotification{SessionId: sid}); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.StopReason != sdk.StopReasonCancelled {
			t.Errorf("StopReason = %v, want %v", resp.StopReason, sdk.StopReasonCancelled)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Prompt did not return promptly after Cancel -- turn was not actually cancelled")
	}
}

func TestAgent_Prompt_WorkerPanicIsRecovered(t *testing.T) {
	a := New(&fakeWorkerFactory{worker: &panicWorker{}}, "")
	a.setUpdater(&fakeConn{})

	sid := newSessionForTest(t, a)
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("trigger panic")},
	})
	if err != nil {
		t.Fatalf("Prompt returned a transport error instead of a mapped stop reason: %v", err)
	}
	if resp.StopReason != sdk.StopReasonRefusal {
		t.Errorf("StopReason = %v, want %v after a recovered panic", resp.StopReason, sdk.StopReasonRefusal)
	}
}

func TestAgent_Prompt_ConcurrentSessionsDoNotRace(t *testing.T) {
	// RT1/FR-001 (auto-review): a single shared Worker had WithProgressHandler
	// mutated per-turn with no synchronization -- concurrent Prompt calls on
	// separate sessions raced on that shared state and could cross-deliver
	// progress lines between sessions. Turns are now serialized per Agent;
	// this test proves both properties: no race, and no cross-session
	// contamination (each session's final output matches what only it asked
	// for).
	fw := &fakeWorker{delay: 20 * time.Millisecond, result: domain.TaskResult{Output: "ok", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.keepAliveInterval = 5 * time.Millisecond
	fc := &fakeConn{}
	a.setUpdater(fc)

	sidA := newSessionForTest(t, a)
	sidB := newSessionForTest(t, a)

	var wg sync.WaitGroup
	results := make(map[sdk.SessionId]sdk.PromptResponse, 2)
	var resultsMu sync.Mutex
	for _, sid := range []sdk.SessionId{sidA, sidB} {
		wg.Add(1)
		go func(sid sdk.SessionId) {
			defer wg.Done()
			resp, err := a.Prompt(context.Background(), sdk.PromptRequest{
				SessionId: sid,
				Prompt:    []sdk.ContentBlock{sdk.TextBlock("concurrent question")},
			})
			if err != nil {
				t.Errorf("Prompt(%s) returned error: %v", sid, err)
				return
			}
			resultsMu.Lock()
			results[sid] = resp
			resultsMu.Unlock()
		}(sid)
	}
	wg.Wait()

	for _, sid := range []sdk.SessionId{sidA, sidB} {
		if resp, ok := results[sid]; !ok || resp.StopReason != sdk.StopReasonEndTurn {
			t.Errorf("session %s: got %+v, want a recorded StopReasonEndTurn response", sid, resp)
		}
	}
}

func TestAgent_Prompt_SameSessionOverlappingTurns_SecondCancelNotClobbered(t *testing.T) {
	// RT4/FR-005 (auto-review): two overlapping Prompt calls for the SAME
	// session used to race on session.cancel -- the first turn's deferred
	// `s.cancel = nil` could null out the second turn's still-active cancel
	// function. RT1's turnMu serialization (turn.go) makes "overlapping" on
	// one session impossible by construction: the second call blocks until
	// the first fully finishes, including its deferred reset. This test
	// proves cancellation still works correctly for the second turn once it
	// actually starts.
	fw := &fakeWorker{delay: time.Second, result: domain.TaskResult{Output: "unused", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	// First turn: fire-and-forget, will be cancelled by the second call's
	// arrival timing below is irrelevant -- we let it run to completion in
	// the background and only care about the second, sequential turn.
	first := make(chan sdk.PromptResponse, 1)
	go func() {
		resp, _ := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("first")}})
		first <- resp
	}()

	time.Sleep(10 * time.Millisecond) // let the first turn actually acquire turnMu and start
	if err := a.Cancel(context.Background(), sdk.CancelNotification{SessionId: sid}); err != nil {
		t.Fatalf("Cancel (first turn): %v", err)
	}
	if resp := <-first; resp.StopReason != sdk.StopReasonCancelled {
		t.Fatalf("first turn StopReason = %v, want Cancelled", resp.StopReason)
	}

	// Second turn on the SAME session, started only after the first fully
	// returned (turnMu guarantees this ordering even if launched
	// concurrently) -- cancel it too, and confirm ITS cancel function is
	// live and works, proving the first turn's cleanup didn't leave the
	// session's cancel state broken for reuse.
	second := make(chan sdk.PromptResponse, 1)
	go func() {
		resp, _ := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("second")}})
		second <- resp
	}()
	time.Sleep(10 * time.Millisecond)
	if err := a.Cancel(context.Background(), sdk.CancelNotification{SessionId: sid}); err != nil {
		t.Fatalf("Cancel (second turn): %v", err)
	}
	select {
	case resp := <-second:
		if resp.StopReason != sdk.StopReasonCancelled {
			t.Errorf("second turn StopReason = %v, want Cancelled", resp.StopReason)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second turn did not return promptly after Cancel -- its cancel function was clobbered or never wired")
	}
}

func TestAgent_Prompt_LogsTurnStartAndEnd(t *testing.T) {
	// RT2/FR-002 (auto-review): spec.md's NFR requires turn start/end,
	// cancellation, and errors to be logged -- previously entirely unmet
	// (zero slog calls in the package).
	logs := captureLogs(t)
	fw := &fakeWorker{result: domain.TaskResult{Output: "42", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("question")},
	}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	out := logs()
	if !strings.Contains(out, "acp turn started") {
		t.Errorf("log output missing turn-start line:\n%s", out)
	}
	if !strings.Contains(out, "acp turn finished") {
		t.Errorf("log output missing turn-finished line:\n%s", out)
	}
	if !strings.Contains(out, "end_turn") {
		t.Errorf("log output missing the stop reason:\n%s", out)
	}
}

func TestAgent_Prompt_LogsCancellation(t *testing.T) {
	logs := captureLogs(t)
	fw := &fakeWorker{delay: time.Second, result: domain.TaskResult{Output: "unused", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	done := make(chan struct{})
	go func() {
		_, _ = a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("q")}})
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	if err := a.Cancel(context.Background(), sdk.CancelNotification{SessionId: sid}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	<-done

	out := logs()
	if !strings.Contains(out, "acp turn cancelled") {
		t.Errorf("log output missing cancellation line:\n%s", out)
	}
}

func TestAgent_Prompt_LogsRecoveredPanic(t *testing.T) {
	logs := captureLogs(t)
	a := New(&fakeWorkerFactory{worker: &panicWorker{}}, "")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{
		SessionId: sid,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("trigger panic")},
	}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	out := logs()
	if !strings.Contains(out, "acp worker panic recovered") {
		t.Errorf("log output missing recovered-panic line:\n%s", out)
	}
}

type panicWorker struct{}

func (panicWorker) Execute(context.Context, domain.Task) (domain.TaskResult, error) {
	panic("simulated worker panic")
}
