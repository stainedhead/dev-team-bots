package buzz

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// This file covers the P2.2/P2.3 replacement of Monitor.dispatch()'s direct
// queue.Send call with a domain.BuzzTaskDispatcher bridge
// (WithTaskDispatcher). See monitor_test.go's
// TestMonitor_HandleChannelEvent_Mention_Dispatches et al for the pre-
// existing no-bridge-configured fallback path, which these tests leave
// untouched by construction (a Monitor without WithTaskDispatcher keeps
// calling queue.Send directly, unaffected by this file).

// TestMonitor_Dispatch_WithTaskDispatcher_UsesBridgeNotQueue verifies that
// once a BuzzTaskDispatcher is wired, a qualifying mention goes through the
// bridge instead of the direct queue.Send path, and the bridge's returned
// TaskID is tracked in the pending map (so HandleResult can still publish
// the eventual reply) when AwaitResult is true.
func TestMonitor_Dispatch_WithTaskDispatcher_UsesBridgeNotQueue(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, botName, eventID, threadID, instruction string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "bridge-task-1", AwaitResult: true}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{
		ID:      "evt-1",
		PubKey:  "someone",
		Kind:    9,
		Tags:    [][]string{{"h", "chan-1"}, {"p", "self-pk"}},
		Content: "please review the PR",
	}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
	call := td.GetCalls()[0]
	if call.BotName != "test-bot" || call.EventID != "evt-1" || call.ThreadID != "chan-1" || call.Instruction != "please review the PR" {
		t.Fatalf("unexpected bridge Dispatch call: %+v", call)
	}

	if len(q.GetSendCalls()) != 0 {
		t.Fatalf("expected no direct queue.Send calls when a bridge is wired, got %d", len(q.GetSendCalls()))
	}

	m.mu.Lock()
	_, pending := m.pending["bridge-task-1"]
	m.mu.Unlock()
	if !pending {
		t.Fatal("expected the bridge-returned TaskID to be tracked in the pending map (AwaitResult=true)")
	}
}

// TestMonitor_Dispatch_WithTaskDispatcher_ReplyOnly_PublishesImmediately
// verifies that a bridge result carrying only a Reply (a scheduling
// confirmation prompt) is published immediately as a threaded kind:9 reply,
// with no pending-map entry created (nothing to await).
func TestMonitor_Dispatch_WithTaskDispatcher_ReplyOnly_PublishesImmediately(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{Reply: "I'll create a task... Confirm?"}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{ID: "root-evt", PubKey: "someone", Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "schedule a review every Monday at 9am"}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool {
		_, ok := lastEventOfKind(fr.publishedSnapshot(), kindChannelMessage)
		return ok
	})
	reply, _ := lastEventOfKind(fr.publishedSnapshot(), kindChannelMessage)
	if reply.Content != "I'll create a task... Confirm?" {
		t.Fatalf("unexpected reply content: %q", reply.Content)
	}
	if got := firstTagValue(reply.Tags, "h"); got != "chan-1" {
		t.Fatalf("expected #h chan-1, got %s", got)
	}
	if got := firstTagValue(reply.Tags, "e"); got != "root-evt" {
		t.Fatalf("expected #e root-evt, got %s", got)
	}

	if len(q.GetSendCalls()) != 0 {
		t.Fatal("expected no queue.Send calls for a reply-only bridge result")
	}
	m.mu.Lock()
	pendingCount := len(m.pending)
	m.mu.Unlock()
	if pendingCount != 0 {
		t.Fatal("expected no pending-map entry for a reply-only bridge result")
	}
}

// TestMonitor_Dispatch_WithTaskDispatcher_ScheduledAck_NoPendingEntry
// verifies that a bridge result with both a TaskID and a Reply, but
// AwaitResult=false (a confirmed future/recurring dispatch), publishes the
// ack immediately and does NOT register a pending-map entry -- there is no
// imminent task result to wait for.
func TestMonitor_Dispatch_WithTaskDispatcher_ScheduledAck_NoPendingEntry(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "sched-task-1", Reply: "Task created — next run: Monday 9am.", AwaitResult: false}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{ID: "root-evt", PubKey: "someone", Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "yes"}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool {
		_, ok := lastEventOfKind(fr.publishedSnapshot(), kindChannelMessage)
		return ok
	})
	reply, _ := lastEventOfKind(fr.publishedSnapshot(), kindChannelMessage)
	if reply.Content != "Task created — next run: Monday 9am." {
		t.Fatalf("unexpected reply content: %q", reply.Content)
	}

	m.mu.Lock()
	_, pending := m.pending["sched-task-1"]
	m.mu.Unlock()
	if pending {
		t.Fatal("expected no pending-map entry when AwaitResult=false")
	}
}

// TestMonitor_Dispatch_WithTaskDispatcher_Duplicate_NoPublishNoPending
// verifies that a Duplicate bridge result (relay-replayed event) produces
// no publish and no pending-map entry.
func TestMonitor_Dispatch_WithTaskDispatcher_Duplicate_NoPublishNoPending(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{Duplicate: true}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{ID: "evt-dup", PubKey: "someone", Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "hi"}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
	time.Sleep(50 * time.Millisecond)

	if len(fr.publishedSnapshot()) != 0 {
		t.Fatal("expected no publish for a duplicate bridge result")
	}
	m.mu.Lock()
	pendingCount := len(m.pending)
	m.mu.Unlock()
	if pendingCount != 0 {
		t.Fatal("expected no pending-map entry for a duplicate bridge result")
	}
}

// TestMonitor_Dispatch_WithTaskDispatcher_Error_LoggedNotPanic verifies that
// a bridge error is logged, not propagated as a panic, and produces no
// publish/pending side effects.
func TestMonitor_Dispatch_WithTaskDispatcher_Error_LoggedNotPanic(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{}, context.DeadlineExceeded
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{ID: "evt-err", PubKey: "someone", Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "hi"}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected a bridge error to be logged, not panic: %v", r)
		}
	}()
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
	time.Sleep(50 * time.Millisecond)

	if len(fr.publishedSnapshot()) != 0 {
		t.Fatal("expected no publish when the bridge returns an error")
	}
}

// TestMonitor_HandleResult_StillWorksForBridgeDispatchedTask verifies that
// HandleResult's existing publish-and-pop-pending behaviour is unaffected
// for a task that was dispatched through the bridge rather than the direct
// queue.Send path.
func TestMonitor_HandleResult_StillWorksForBridgeDispatchedTask(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "bridge-task-2", AwaitResult: true}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{ID: "root-evt-2", PubKey: "someone", Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "hi"}
	m.handleChannelEvent(context.Background(), "chan-1", evt)
	waitFor(t, time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.pending["bridge-task-2"]
		return ok
	})

	m.HandleResult(context.Background(), domain.TaskResultPayload{TaskID: "bridge-task-2", Output: "the answer", Success: true})

	reply, ok := lastEventOfKind(fr.publishedSnapshot(), kindChannelMessage)
	if !ok || reply.Content != "the answer" {
		t.Fatalf("expected the bridge-dispatched task's result to be published, got %+v", fr.publishedSnapshot())
	}

	m.mu.Lock()
	_, stillPending := m.pending["bridge-task-2"]
	m.mu.Unlock()
	if stillPending {
		t.Fatal("expected pending entry to be popped after HandleResult")
	}
}
