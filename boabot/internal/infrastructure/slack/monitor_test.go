package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- fakes -------------------------------------------------------------------

// fakePoster records PostMessageContext calls.
type fakePoster struct {
	mu    sync.Mutex
	calls []postCall
}

type postCall struct {
	channelID string
	opts      []slackgo.MsgOption
}

func (f *fakePoster) PostMessageContext(_ context.Context, channelID string, opts ...slackgo.MsgOption) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, postCall{channelID: channelID, opts: opts})
	return "ts", "channel", nil
}

func (f *fakePoster) getCalls() []postCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]postCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeSocketClient is an in-memory socket client for testing.
type fakeSocketClient struct {
	events chan socketmode.Event
	acks   []socketmode.Request
	mu     sync.Mutex
}

func newFakeSocket() *fakeSocketClient {
	return &fakeSocketClient{
		events: make(chan socketmode.Event, 10),
	}
}

func (f *fakeSocketClient) RunContext(_ context.Context) error {
	return nil
}

func (f *fakeSocketClient) EventsChan() <-chan socketmode.Event {
	return f.events
}

func (f *fakeSocketClient) Ack(req socketmode.Request, _ ...interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks = append(f.acks, req)
	return nil
}

// sendEvent pushes a socketmode.Event into the fake event channel.
func (f *fakeSocketClient) sendEvent(evt socketmode.Event) {
	f.events <- evt
}

// close closes the events channel so the run loop terminates.
func (f *fakeSocketClient) close() {
	close(f.events)
}

// --- helpers -----------------------------------------------------------------

func makeMonitor(t *testing.T, q *mocks.MessageQueue, poster *fakePoster, socket *fakeSocketClient) *Monitor {
	t.Helper()
	cfg := Config{BotToken: "xoxb-test", AppToken: "xapp-test", BotName: "bao"}
	return newMonitor(cfg, q, poster, socket)
}

// makeDMEvent builds a socketmode.Event representing an im message.
func makeDMEvent(text, channel, ts, botID, subType string) socketmode.Event {
	msgEvt := &slackevents.MessageEvent{
		Type:        string(slackevents.Message),
		Text:        text,
		Channel:     channel,
		TimeStamp:   ts,
		ChannelType: slackevents.ChannelTypeIM,
		BotID:       botID,
		SubType:     subType,
	}
	inner := slackevents.EventsAPIInnerEvent{
		Type: string(slackevents.Message),
		Data: msgEvt,
	}
	outer := slackevents.EventsAPIEvent{
		Type:       slackevents.CallbackEvent,
		InnerEvent: inner,
	}
	req := &socketmode.Request{EnvelopeID: "env-dm"}
	return socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    outer,
		Request: req,
	}
}

// makeAppMentionEvent builds a socketmode.Event for an app_mention.
func makeAppMentionEvent(text, channel, ts, threadTS string) socketmode.Event {
	ev := &slackevents.AppMentionEvent{
		Type:            string(slackevents.AppMention),
		Text:            text,
		Channel:         channel,
		TimeStamp:       ts,
		ThreadTimeStamp: threadTS,
	}
	inner := slackevents.EventsAPIInnerEvent{
		Type: string(slackevents.AppMention),
		Data: ev,
	}
	outer := slackevents.EventsAPIEvent{
		Type:       slackevents.CallbackEvent,
		InnerEvent: inner,
	}
	req := &socketmode.Request{EnvelopeID: "env-mention"}
	return socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    outer,
		Request: req,
	}
}

// waitForSend blocks until queue.Send is called or the timeout elapses.
func waitForSend(q *mocks.MessageQueue, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(q.GetSendCalls()) > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// --- tests -------------------------------------------------------------------

func TestMonitor_DM_Dispatch(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	socket.sendEvent(makeDMEvent("hello bot", "C123", "111.000", "", ""))

	if !waitForSend(q, 2*time.Second) {
		t.Fatal("queue.Send not called within timeout")
	}

	calls := q.GetSendCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(calls))
	}
	call := calls[0]
	if call.QueueURL != "bao" {
		t.Errorf("expected QueueURL=bao, got %q", call.QueueURL)
	}
	if call.Message.Type != domain.MessageTypeTask {
		t.Errorf("expected message type task, got %q", call.Message.Type)
	}
	if call.Message.From != "slack" {
		t.Errorf("expected From=slack, got %q", call.Message.From)
	}

	var tp domain.TaskPayload
	if err := json.Unmarshal(call.Message.Payload, &tp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if tp.Instruction != "hello bot" {
		t.Errorf("expected instruction=%q, got %q", "hello bot", tp.Instruction)
	}
	if tp.TaskID == "" {
		t.Error("expected non-empty TaskID")
	}
}

func TestMonitor_BotMessageIgnored(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// BotID is set → should be ignored.
	socket.sendEvent(makeDMEvent("bot speaking", "C123", "111.000", "B999", ""))

	// Give a short time to ensure nothing is sent.
	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for bot message, got %d", len(calls))
	}
}

func TestMonitor_AppMention_Dispatch(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Simulate "<@U123> do something".
	socket.sendEvent(makeAppMentionEvent("<@U123> do something", "C456", "222.000", ""))

	if !waitForSend(q, 2*time.Second) {
		t.Fatal("queue.Send not called within timeout")
	}

	calls := q.GetSendCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(calls))
	}

	var tp domain.TaskPayload
	if err := json.Unmarshal(calls[0].Message.Payload, &tp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if tp.Instruction != "do something" {
		t.Errorf("expected stripped instruction=%q, got %q", "do something", tp.Instruction)
	}
}

func TestMonitor_AppMention_EmptyAfterStrip(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Mention with no trailing text — stripping leaves empty string.
	socket.sendEvent(makeAppMentionEvent("<@U123>", "C456", "222.000", ""))

	// Nothing should be dispatched.
	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for empty mention, got %d", len(calls))
	}
}

func TestMonitor_HandleResult_PostsReply(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	// Inject a pending entry directly.
	taskID := "task-abc"
	mon.mu.Lock()
	mon.pending[taskID] = replyTarget{channelID: "C789", threadTS: ""}
	mon.mu.Unlock()

	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  taskID,
		Output:  "done!",
		Success: true,
	})

	calls := poster.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(calls))
	}
	if calls[0].channelID != "C789" {
		t.Errorf("expected channelID=C789, got %q", calls[0].channelID)
	}
	if len(calls[0].opts) != 1 {
		t.Errorf("expected 1 msg option (no threadTS), got %d", len(calls[0].opts))
	}
}

func TestMonitor_HandleResult_UnknownTask(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	// Call HandleResult with an unknown task ID.
	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  "unknown-task",
		Output:  "result",
		Success: true,
	})

	calls := poster.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 post calls for unknown task, got %d", len(calls))
	}
}

func TestMonitor_HandleResult_ThreadedReply(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	taskID := "task-threaded"
	mon.mu.Lock()
	mon.pending[taskID] = replyTarget{channelID: "C999", threadTS: "111.111"}
	mon.mu.Unlock()

	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  taskID,
		Output:  "threaded result",
		Success: true,
	})

	calls := poster.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(calls))
	}
	// With a non-empty threadTS, two options should be present:
	// MsgOptionText + MsgOptionTS.
	if len(calls[0].opts) < 2 {
		t.Errorf("expected at least 2 msg options (text + thread ts), got %d", len(calls[0].opts))
	}
}

func TestMonitor_Stop(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx := context.Background()
	if err := mon.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

func TestMonitor_ContextCancellation_ExitsLoop(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		mon.run(ctx)
	}()

	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

func TestMonitor_ChannelClose_ExitsLoop(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mon.run(ctx)
	}()

	socket.close()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after channel close")
	}
}

func TestMonitor_NonEventsAPI_EventSkipped(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Send a non-EventsAPI event — should be ignored.
	socket.sendEvent(socketmode.Event{
		Type: socketmode.EventType("hello"),
	})

	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for non-events-api event, got %d", len(calls))
	}
}

func TestMonitor_EventsAPI_WrongDataType_Skipped(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Send an EventTypeEventsAPI event with non-EventsAPIEvent data.
	socket.sendEvent(socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: "not an EventsAPIEvent",
	})

	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for wrong data type, got %d", len(calls))
	}
}

func TestMonitor_DM_NonIM_Ignored(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// MessageEvent with channel type "channel" (not IM).
	msgEvt := &slackevents.MessageEvent{
		Type:        string(slackevents.Message),
		Text:        "hello everyone",
		Channel:     "C123",
		TimeStamp:   "111.000",
		ChannelType: slackevents.ChannelTypeChannel,
	}
	inner := slackevents.EventsAPIInnerEvent{
		Type: string(slackevents.Message),
		Data: msgEvt,
	}
	outer := slackevents.EventsAPIEvent{
		Type:       slackevents.CallbackEvent,
		InnerEvent: inner,
	}
	req := &socketmode.Request{EnvelopeID: "env-chan"}
	socket.sendEvent(socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    outer,
		Request: req,
	})

	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for channel message, got %d", len(calls))
	}
}

func TestMonitor_DM_BotSubTypeIgnored(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// SubType = "bot_message" should be ignored even with empty BotID.
	socket.sendEvent(makeDMEvent("bot output", "C123", "111.000", "", "bot_message"))

	time.Sleep(100 * time.Millisecond)

	calls := q.GetSendCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 send calls for bot_message subtype, got %d", len(calls))
	}
}

func TestMonitor_QueueSendError_NoPending(t *testing.T) {
	q := &mocks.MessageQueue{
		SendFn: func(_ context.Context, _ string, _ domain.Message) error {
			return errQueueFull
		},
	}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	socket.sendEvent(makeDMEvent("hello", "C123", "111.000", "", ""))

	// Wait for send attempt.
	if !waitForSend(q, 2*time.Second) {
		t.Fatal("queue.Send not called")
	}

	// After a failed send, no entry should be in pending.
	mon.mu.Lock()
	pendingCount := len(mon.pending)
	mon.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected 0 pending entries after queue error, got %d", pendingCount)
	}
}

func TestMonitor_HandleResult_FailedTask_UsesError(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	taskID := "task-fail"
	mon.mu.Lock()
	mon.pending[taskID] = replyTarget{channelID: "C999", threadTS: ""}
	mon.mu.Unlock()

	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  taskID,
		Output:  "",
		Error:   "something went wrong",
		Success: false,
	})

	calls := poster.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(calls))
	}
	if calls[0].channelID != "C999" {
		t.Errorf("expected channelID=C999, got %q", calls[0].channelID)
	}
}

func TestMonitor_AppMention_WithThreadTS(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Mention inside a thread — threadTS should be used.
	socket.sendEvent(makeAppMentionEvent("<@U123> review this", "C456", "222.000", "111.000"))

	if !waitForSend(q, 2*time.Second) {
		t.Fatal("queue.Send not called within timeout")
	}

	calls := q.GetSendCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(calls))
	}

	// Find the pending entry — threadTS should be "111.000".
	mon.mu.Lock()
	var target replyTarget
	for _, v := range mon.pending {
		target = v
	}
	mon.mu.Unlock()

	if target.threadTS != "111.000" {
		t.Errorf("expected threadTS=111.000, got %q", target.threadTS)
	}
}

func TestMonitor_NilRequest_Handled(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Event with nil Request — should not panic.
	outer := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				Type:        string(slackevents.Message),
				Text:        "hi",
				Channel:     "C123",
				TimeStamp:   "1.0",
				ChannelType: slackevents.ChannelTypeIM,
			},
		},
	}
	socket.sendEvent(socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    outer,
		Request: nil,
	})

	if !waitForSend(q, 2*time.Second) {
		t.Fatal("queue.Send not called within timeout")
	}
}

// errQueueFull is a sentinel error for queue-full conditions in tests.
var errQueueFull = fmt.Errorf("queue full")

// TestSocketmodeWrapper_Implements_socketClient is a compile-time check.
func TestSocketmodeWrapper_Implements_socketClient(t *testing.T) {
	var _ socketClient = (*socketmodeWrapper)(nil)
}

// TestSocketmodeWrapper_EventsChan verifies EventsChan returns the underlying
// Client.Events channel without nil-dereferencing.
func TestSocketmodeWrapper_EventsChan(t *testing.T) {
	// socketmode.New requires a non-nil *slack.Client, but we only need the
	// type — creating one with a dummy token is harmless for this path.
	api := slackgo.New("xoxb-dummy")
	raw := socketmode.New(api)
	w := &socketmodeWrapper{raw}

	ch := w.EventsChan()
	if ch == nil {
		t.Error("EventsChan should not return nil")
	}
}

func TestMonitor_HandleResult_EmptyOutput_UsesPlaceholder(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &fakePoster{}
	socket := newFakeSocket()
	mon := makeMonitor(t, q, poster, socket)

	taskID := "task-empty"
	mon.mu.Lock()
	mon.pending[taskID] = replyTarget{channelID: "C111", threadTS: ""}
	mon.mu.Unlock()

	// Both Output and Error are empty.
	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  taskID,
		Output:  "",
		Error:   "",
		Success: true,
	})

	calls := poster.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(calls))
	}
}

// errorPoster always returns an error from PostMessageContext.
type errorPoster struct{}

func (e *errorPoster) PostMessageContext(_ context.Context, _ string, _ ...slackgo.MsgOption) (string, string, error) {
	return "", "", fmt.Errorf("slack api error")
}

func TestMonitor_HandleResult_PostError_Logged(t *testing.T) {
	q := &mocks.MessageQueue{}
	poster := &errorPoster{}
	socket := newFakeSocket()
	cfg := Config{BotToken: "xoxb-test", AppToken: "xapp-test", BotName: "bao"}
	mon := newMonitor(cfg, q, poster, socket)

	taskID := "task-post-err"
	mon.mu.Lock()
	mon.pending[taskID] = replyTarget{channelID: "C222", threadTS: ""}
	mon.mu.Unlock()

	// Should not panic despite poster error.
	mon.HandleResult(context.Background(), domain.TaskResultPayload{
		TaskID:  taskID,
		Output:  "result",
		Success: true,
	})
}
