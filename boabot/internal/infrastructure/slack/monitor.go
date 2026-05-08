// Package slack provides a Slack Socket Mode ChannelMonitor adapter.
// It listens for direct messages and @mentions, dispatches them as task
// messages to the configured bot queue, and posts results back to Slack.
package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/google/uuid"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// poster abstracts the Slack PostMessageContext call.
type poster interface {
	PostMessageContext(ctx context.Context, channelID string, options ...slackgo.MsgOption) (string, string, error)
}

// socketClient abstracts the Socket Mode connection.
type socketClient interface {
	RunContext(ctx context.Context) error
	EventsChan() <-chan socketmode.Event
	Ack(req socketmode.Request, payload ...interface{}) error
}

// replyTarget holds the Slack coordinates needed to post a reply.
type replyTarget struct {
	channelID string
	threadTS  string
}

// Config holds the Slack connection parameters.
type Config struct {
	BotToken string
	AppToken string
	BotName  string // target bot queue URL (its name in local mode)
}

// Monitor implements domain.ChannelMonitor for Slack Socket Mode.
type Monitor struct {
	cfg     Config
	queue   domain.MessageQueue
	api     poster
	socket  socketClient
	mu      sync.Mutex
	pending map[string]replyTarget // taskID → {channelID, threadTS}
}

// socketmodeWrapper wraps *socketmode.Client to implement socketClient.
type socketmodeWrapper struct{ *socketmode.Client }

func (w *socketmodeWrapper) EventsChan() <-chan socketmode.Event {
	return w.Events
}

func (w *socketmodeWrapper) Ack(req socketmode.Request, payload ...interface{}) error {
	return w.Client.Ack(req, payload...)
}

// New constructs a production Monitor backed by real Slack connections.
func New(cfg Config, queue domain.MessageQueue) *Monitor {
	api := slackgo.New(cfg.BotToken, slackgo.OptionAppLevelToken(cfg.AppToken))
	raw := socketmode.New(api)
	return newMonitor(cfg, queue, api, &socketmodeWrapper{raw})
}

// newMonitor constructs a Monitor with injected dependencies (used in tests).
func newMonitor(cfg Config, queue domain.MessageQueue, api poster, socket socketClient) *Monitor {
	return &Monitor{
		cfg:     cfg,
		queue:   queue,
		api:     api,
		socket:  socket,
		pending: make(map[string]replyTarget),
	}
}

// Start implements domain.ChannelMonitor. It launches the socket event loop
// in a goroutine and returns nil immediately.
func (m *Monitor) Start(ctx context.Context) error {
	go m.run(ctx)
	return nil
}

// Stop implements domain.ChannelMonitor. Context cancellation terminates the loop.
func (m *Monitor) Stop(_ context.Context) error {
	return nil
}

// run drives the Socket Mode event loop. It starts the underlying connection
// in its own goroutine and drains the event channel.
func (m *Monitor) run(ctx context.Context) {
	go func() {
		if err := m.socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			slog.Error("slack socket mode run error", "err", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-m.socket.EventsChan():
			if !ok {
				return
			}
			if evt.Type != socketmode.EventTypeEventsAPI {
				continue
			}
			req := evt.Request
			if req == nil {
				// Best effort — some events may not carry a Request.
				req = &socketmode.Request{}
			}
			_ = m.socket.Ack(*req)

			apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				continue
			}
			if apiEvent.Type == slackevents.CallbackEvent {
				m.handleCallbackEvent(ctx, apiEvent)
			}
		}
	}
}

// handleCallbackEvent dispatches MessageEvent (DMs) and AppMentionEvent.
func (m *Monitor) handleCallbackEvent(ctx context.Context, outer slackevents.EventsAPIEvent) {
	switch ev := outer.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		if ev.ChannelType != slackevents.ChannelTypeIM {
			return
		}
		// Skip bot messages to avoid loops.
		if ev.BotID != "" || ev.SubType == "bot_message" {
			return
		}
		m.dispatch(ctx, ev.Text, ev.Channel, ev.TimeStamp)

	case *slackevents.AppMentionEvent:
		text := ev.Text
		// Strip the leading "<@UXXXXX> " mention.
		if idx := strings.Index(text, "> "); idx != -1 {
			text = text[idx+2:]
		} else if strings.HasPrefix(text, "<@") {
			// Mention only, no trailing text.
			text = ""
		}
		text = strings.TrimSpace(text)

		threadTS := ev.ThreadTimeStamp
		if threadTS == "" {
			threadTS = ev.TimeStamp
		}
		m.dispatch(ctx, text, ev.Channel, threadTS)
	}
}

// dispatch enqueues a task message to the target bot queue.
func (m *Monitor) dispatch(ctx context.Context, text, channelID, threadTS string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	taskID := uuid.New().String()
	payload := domain.TaskPayload{
		TaskID:      taskID,
		Instruction: text,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("slack monitor: marshal task payload", "err", err)
		return
	}

	msg := domain.Message{
		ID:        uuid.New().String(),
		Type:      domain.MessageTypeTask,
		From:      "slack",
		To:        m.cfg.BotName,
		Payload:   payloadBytes,
		Timestamp: time.Now().UTC(),
	}

	if err := m.queue.Send(ctx, m.cfg.BotName, msg); err != nil {
		slog.Error("slack monitor: send to queue", "err", err)
		return
	}

	m.mu.Lock()
	m.pending[taskID] = replyTarget{channelID: channelID, threadTS: threadTS}
	m.mu.Unlock()

	slog.Info("slack monitor: dispatched task", "task_id", taskID, "bot", m.cfg.BotName, "channel", channelID)
}

// HandleResult posts the task output back to the originating Slack channel.
// It is called by TeamManager's result handler closure.
func (m *Monitor) HandleResult(ctx context.Context, p domain.TaskResultPayload) {
	m.mu.Lock()
	target, ok := m.pending[p.TaskID]
	if ok {
		delete(m.pending, p.TaskID)
	}
	m.mu.Unlock()

	if !ok {
		return // not a Slack-originated task
	}

	output := p.Output
	if !p.Success && p.Error != "" {
		output = p.Error
	}
	if output == "" {
		output = "(no output)"
	}

	opts := []slackgo.MsgOption{slackgo.MsgOptionText(output, false)}
	if target.threadTS != "" {
		opts = append(opts, slackgo.MsgOptionTS(target.threadTS))
	}

	if _, _, err := m.api.PostMessageContext(ctx, target.channelID, opts...); err != nil {
		slog.Warn("slack monitor: post message failed", "channel", target.channelID, "err", err)
	}
}
