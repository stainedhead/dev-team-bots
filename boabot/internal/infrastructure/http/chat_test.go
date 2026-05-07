package httpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── fake chat store ───────────────────────────────────────────────────────────

type fakeChatStore struct {
	appendFn       func(ctx context.Context, msg domain.ChatMessage) error
	listFn         func(ctx context.Context, threadID string) ([]domain.ChatMessage, error)
	listAllFn      func(ctx context.Context) ([]domain.ChatMessage, error)
	listByBotFn    func(ctx context.Context, botName string) ([]domain.ChatMessage, error)
	createThreadFn func(ctx context.Context, title string, participants []string) (domain.ChatThread, error)
	listThreadsFn  func(ctx context.Context) ([]domain.ChatThread, error)
	deleteThreadFn func(ctx context.Context, threadID string) error
}

func (f *fakeChatStore) CreateThread(ctx context.Context, title string, participants []string) (domain.ChatThread, error) {
	if f.createThreadFn != nil {
		return f.createThreadFn(ctx, title, participants)
	}
	return domain.ChatThread{ID: "thread-1", Title: title, Participants: participants}, nil
}

func (f *fakeChatStore) ListThreads(ctx context.Context) ([]domain.ChatThread, error) {
	if f.listThreadsFn != nil {
		return f.listThreadsFn(ctx)
	}
	return []domain.ChatThread{{ID: "thread-1", Title: "Test Thread"}}, nil
}

func (f *fakeChatStore) DeleteThread(ctx context.Context, threadID string) error {
	if f.deleteThreadFn != nil {
		return f.deleteThreadFn(ctx, threadID)
	}
	return nil
}

func (f *fakeChatStore) Append(ctx context.Context, msg domain.ChatMessage) error {
	if f.appendFn != nil {
		return f.appendFn(ctx, msg)
	}
	return nil
}

func (f *fakeChatStore) List(ctx context.Context, threadID string) ([]domain.ChatMessage, error) {
	if f.listFn != nil {
		return f.listFn(ctx, threadID)
	}
	return []domain.ChatMessage{
		{ID: "m1", ThreadID: threadID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "hello", CreatedAt: time.Now()},
	}, nil
}

func (f *fakeChatStore) ListAll(ctx context.Context) ([]domain.ChatMessage, error) {
	if f.listAllFn != nil {
		return f.listAllFn(ctx)
	}
	return []domain.ChatMessage{
		{ID: "m1", BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "hello", CreatedAt: time.Now()},
	}, nil
}

func (f *fakeChatStore) ListByBot(ctx context.Context, botName string) ([]domain.ChatMessage, error) {
	if f.listByBotFn != nil {
		return f.listByBotFn(ctx, botName)
	}
	return []domain.ChatMessage{
		{ID: "m1", BotName: botName, Direction: domain.ChatDirectionOutbound, Content: "hello", CreatedAt: time.Now()},
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newChatTestServer() *httptest.Server {
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: &fakeTaskDispatcher{},
		Chat:       &fakeChatStore{},
	})
	return httptest.NewServer(s.Handler())
}

// ── Chat handler tests ────────────────────────────────────────────────────────

func TestChatList_ReturnsAllMessages(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "chat", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var msgs []domain.ChatMessage
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) < 1 {
		t.Fatal("expected at least one message")
	}
}

func TestChatList_RequiresAuth(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/chat", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestChatBotList_ReturnsMessagesForBot(t *testing.T) {
	var capturedBotName string
	chat := &fakeChatStore{
		listByBotFn: func(_ context.Context, botName string) ([]domain.ChatMessage, error) {
			capturedBotName = botName
			return []domain.ChatMessage{
				{ID: "m1", BotName: botName, Direction: domain.ChatDirectionOutbound, Content: "ping"},
			}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: &fakeTaskDispatcher{},
		Chat:       chat,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "chat/dev-1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedBotName != "dev-1" {
		t.Errorf("expected bot name dev-1, got %q", capturedBotName)
	}
	var msgs []domain.ChatMessage
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestChatBotList_RequiresAuth(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/chat/dev-1", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestChatSend_Returns201WithMessage(t *testing.T) {
	var appendedMsg domain.ChatMessage
	chat := &fakeChatStore{
		appendFn: func(_ context.Context, msg domain.ChatMessage) error {
			appendedMsg = msg
			return nil
		},
	}
	var dispatchedBot, dispatchedInstruction string
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, botName, instruction string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			dispatchedBot = botName
			dispatchedInstruction = instruction
			return domain.DirectTask{ID: "task-99", BotName: botName, Instruction: instruction, Status: domain.DirectTaskStatusRunning}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		Chat:       chat,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"content":"please run tests"}`
	resp := doJSON(t, srv, http.MethodPost, "chat/dev-1", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var out domain.ChatMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BotName != "dev-1" {
		t.Errorf("expected BotName=dev-1, got %q", out.BotName)
	}
	if out.Direction != domain.ChatDirectionOutbound {
		t.Errorf("expected direction=outbound, got %q", out.Direction)
	}
	if out.Content != "please run tests" {
		t.Errorf("expected content='please run tests', got %q", out.Content)
	}
	if out.TaskID != "task-99" {
		t.Errorf("expected TaskID=task-99, got %q", out.TaskID)
	}
	if dispatchedBot != "dev-1" {
		t.Errorf("expected dispatcher called with dev-1, got %q", dispatchedBot)
	}
	if dispatchedInstruction != "please run tests" {
		t.Errorf("expected instruction='please run tests', got %q", dispatchedInstruction)
	}
	_ = appendedMsg
}

func TestChatSend_RequiresAuth(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/chat/dev-1",
		strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestChatSend_NilChat_Returns503(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: &fakeTaskDispatcher{},
		// Chat intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "chat/dev-1", `{"content":"hi"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Chat is nil, got %d", resp.StatusCode)
	}
}

func TestChatSend_NilDispatcher_Returns503(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		Tasks:  &fakeDirectTaskStore{},
		Chat:   &fakeChatStore{},
		// Dispatcher intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "chat/dev-1", `{"content":"hi"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Dispatcher is nil, got %d", resp.StatusCode)
	}
}

func TestChatSend_EmptyContent_Returns400(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "chat/dev-1", `{"content":""}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestChatList_NilChat_Returns503(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		// Chat intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "chat", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Chat is nil, got %d", resp.StatusCode)
	}
}

func TestKanbanUI_HasChatEndpoint(t *testing.T) {
	srv := newChatTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf strings.Builder
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "/api/v1/chat") {
		t.Error("kanban UI must reference /api/v1/chat")
	}
}
