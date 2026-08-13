package acp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

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

type panicWorker struct{}

func (panicWorker) Execute(context.Context, domain.Task) (domain.TaskResult, error) {
	panic("simulated worker panic")
}
