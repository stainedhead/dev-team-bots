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
//
// call.ThreadID asserts the P1.1 fix: it must be the NIP-10 thread root
// (rootEventID's fallback -- evt.ID, since evt carries no root-marked `e`
// tag), NOT channelUUID ("chan-1"). Before P1.1, this assertion read
// call.ThreadID != "chan-1", encoding the bug FR-208 fixes -- two
// concurrent threads in the same channel sharing one
// ChatTaskManager.pendingMap entry.
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
	if call.BotName != "test-bot" || call.EventID != "evt-1" || call.ThreadID != "evt-1" || call.Instruction != "please review the PR" {
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

// TestMonitor_Dispatch_ConcurrentThreadsSameChannel_IndependentThreadIDs is
// P1.1's regression test (FR-208): two @mentions in the same channel, each
// starting its own thread (a distinct root-marked `e` tag), must produce
// two Dispatch calls with two DISTINCT ThreadID values -- both derived from
// each event's own NIP-10 thread root, never collapsed onto the shared
// channelUUID. Before the fix, both calls would have carried the same
// ThreadID ("chan-shared"), causing ChatTaskManager.pendingMap to treat
// unrelated threads as one scheduling-confirmation conversation.
func TestMonitor_Dispatch_ConcurrentThreadsSameChannel_IndependentThreadIDs(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "t", AwaitResult: false}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evtA := domain.Event{
		ID: "evt-a", PubKey: "someone", Kind: 9,
		Tags:    [][]string{{"h", "chan-shared"}, {"p", "self-pk"}, {"e", "thread-a-root", "", "root"}},
		Content: "question in thread A",
	}
	evtB := domain.Event{
		ID: "evt-b", PubKey: "someone-else", Kind: 9,
		Tags:    [][]string{{"h", "chan-shared"}, {"p", "self-pk"}, {"e", "thread-b-root", "", "root"}},
		Content: "question in thread B",
	}
	m.handleChannelEvent(context.Background(), "chan-shared", evtA)
	m.handleChannelEvent(context.Background(), "chan-shared", evtB)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 2 })
	calls := td.GetCalls()
	threadIDs := map[string]bool{calls[0].ThreadID: true, calls[1].ThreadID: true}
	if len(threadIDs) != 2 {
		t.Fatalf("expected two distinct ThreadIDs for two concurrent threads in the same channel, got %+v", calls)
	}
	if !threadIDs["thread-a-root"] || !threadIDs["thread-b-root"] {
		t.Fatalf("expected ThreadIDs {thread-a-root, thread-b-root}, got %+v", calls)
	}
	if threadIDs["chan-shared"] {
		t.Fatal("expected ThreadID never to be the shared channelUUID")
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

// --- P1.2: triggerThreadReply --------------------------------------------

// TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_KnownThread_Dispatches
// is P1.2's core acceptance criterion (FR-205): a reply event that carries
// no #p mention of self, but IS a root-marked NIP-10 reply within a thread
// KnownThread confirms this persona previously dispatched in, must still
// dispatch -- before the fix this was silently dropped as triggerNone.
func TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_KnownThread_Dispatches(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "thread-task-1"}, nil
		},
	}
	td.MarkKnownThread("test-bot", "known-root")
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	// No #p tag naming self-pk -- classifyTrigger alone would return
	// triggerNone. Carries a root-marked e tag pointing at a thread this
	// persona already dispatched in.
	evt := domain.Event{
		ID:      "reply-evt",
		PubKey:  "someone",
		Kind:    9,
		Tags:    [][]string{{"h", "chan-1"}, {"e", "known-root", "", "root"}},
		Content: "following up, no mention this time",
	}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
	call := td.GetCalls()[0]
	if call.ThreadID != "known-root" || call.Instruction != "following up, no mention this time" {
		t.Fatalf("unexpected bridge Dispatch call: %+v", call)
	}
}

// TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_UnknownThread_Dropped
// verifies the negative case (spec.md's "Thread reply to a root this
// persona never dispatched in" edge case): a reply-shaped event whose
// referenced root is NOT a KnownThread for this persona must still be
// dropped, exactly like the pre-P1.2 behaviour.
func TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_UnknownThread_Dropped(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	td := &mocks.BuzzTaskDispatcher{}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td))

	evt := domain.Event{
		ID:      "reply-evt",
		PubKey:  "someone",
		Kind:    9,
		Tags:    [][]string{{"h", "chan-1"}, {"e", "unknown-root", "", "root"}},
		Content: "reply in a thread we never touched",
	}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	time.Sleep(50 * time.Millisecond)
	if len(td.GetCalls()) != 0 {
		t.Fatalf("expected no dispatch for a reply in an unknown thread, got %+v", td.GetCalls())
	}
}

// TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_NoBridge_Dropped
// verifies that without a BuzzTaskDispatcher wired (the pre-existing direct
// queue.Send fallback), thread-reply classification never runs -- there is
// no KnownThread state to consult, and the pre-existing no-mention-dropped
// behaviour is preserved exactly.
func TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_NoBridge_Dropped(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := newTestMonitor(fr, q, nil) // no WithTaskDispatcher

	evt := domain.Event{
		ID:      "reply-evt",
		PubKey:  "someone",
		Kind:    9,
		Tags:    [][]string{{"h", "chan-1"}, {"e", "some-root", "", "root"}},
		Content: "reply without mention, no bridge wired",
	}
	m.handleChannelEvent(context.Background(), "chan-1", evt)

	time.Sleep(50 * time.Millisecond)
	if len(q.GetSendCalls()) != 0 {
		t.Fatal("expected no dispatch when no BuzzTaskDispatcher is wired")
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
