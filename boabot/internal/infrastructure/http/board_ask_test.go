package httpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeAskRouter is a test double for domain.AskRouter.
type fakeAskRouter struct {
	enqueueFn func(botName string, req domain.AskRequest) bool
}

func (f *fakeAskRouter) Enqueue(botName string, req domain.AskRequest) bool {
	if f.enqueueFn != nil {
		return f.enqueueFn(botName, req)
	}
	return true
}

// ── buildBoardAskInstruction tests (via handleBoardAsk) ───────────────────────

// TestBoardAsk_IdleItem_IncludesQAFraming verifies that when a board item is not
// in-progress the dispatched instruction tells the bot it is in a Q&A context
// and should not take active actions.
func TestBoardAsk_IdleItem_IncludesQAFraming(t *testing.T) {
	item := domain.WorkItem{
		ID:         "item-1",
		Title:      "Fix login bug",
		Status:     domain.WorkItemStatusDone,
		AssignedTo: "dev-1",
	}
	board := &fakeBoardStore{
		getFn: func(_ context.Context, _ string) (domain.WorkItem, error) { return item, nil },
	}
	var capturedInstruction string
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
			capturedInstruction = instruction
			return domain.DirectTask{ID: "t1"}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		Chat:       &fakeChatStore{listFn: func(_ context.Context, _ string) ([]domain.ChatMessage, error) { return nil, nil }},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/item-1/ask", `{"content":"what happened?"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if !strings.Contains(capturedInstruction, "answer questions") &&
		!strings.Contains(capturedInstruction, "Q&A") {
		t.Errorf("expected Q&A framing in instruction, got:\n%s", capturedInstruction)
	}
	if strings.Contains(capturedInstruction, "Regarding board item") {
		t.Error("idle-item instruction must not use the mid-task 'Regarding board item' format")
	}
}

// TestBoardAsk_IdleItem_IncludesItemDescription verifies that the dispatched
// instruction contains the item's title, description, and status.
func TestBoardAsk_IdleItem_IncludesItemDescription(t *testing.T) {
	item := domain.WorkItem{
		ID:          "item-2",
		Title:       "Deploy pipeline",
		Description: "Set up CI/CD for the new service",
		Status:      domain.WorkItemStatusBacklog,
		AssignedTo:  "ops-1",
	}
	board := &fakeBoardStore{
		getFn: func(_ context.Context, _ string) (domain.WorkItem, error) { return item, nil },
	}
	var capturedInstruction string
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
			capturedInstruction = instruction
			return domain.DirectTask{ID: "t2"}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		Chat:       &fakeChatStore{listFn: func(_ context.Context, _ string) ([]domain.ChatMessage, error) { return nil, nil }},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/item-2/ask", `{"content":"is it ready?"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	for _, want := range []string{"Deploy pipeline", "Set up CI/CD", "backlog"} {
		if !strings.Contains(capturedInstruction, want) {
			t.Errorf("expected %q in instruction, got:\n%s", want, capturedInstruction)
		}
	}
}

// TestBoardAsk_IdleItem_IncludesLastResult verifies that a completed item's
// last output is included in the dispatched instruction.
func TestBoardAsk_IdleItem_IncludesLastResult(t *testing.T) {
	item := domain.WorkItem{
		ID:         "item-3",
		Title:      "Run tests",
		Status:     domain.WorkItemStatusDone,
		AssignedTo: "dev-1",
		LastResult: "All 42 tests passed. Coverage: 94%.",
	}
	board := &fakeBoardStore{
		getFn: func(_ context.Context, _ string) (domain.WorkItem, error) { return item, nil },
	}
	var capturedInstruction string
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
			capturedInstruction = instruction
			return domain.DirectTask{ID: "t3"}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		Chat:       &fakeChatStore{listFn: func(_ context.Context, _ string) ([]domain.ChatMessage, error) { return nil, nil }},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/item-3/ask", `{"content":"how many tests?"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if !strings.Contains(capturedInstruction, "All 42 tests passed") {
		t.Errorf("expected last result in instruction, got:\n%s", capturedInstruction)
	}
}

// TestBoardAsk_IdleItem_IncludesConversationHistory verifies that prior chat
// messages for the item thread are included in the dispatched instruction.
func TestBoardAsk_IdleItem_IncludesConversationHistory(t *testing.T) {
	item := domain.WorkItem{
		ID:         "item-4",
		Title:      "Write docs",
		Status:     domain.WorkItemStatusDone,
		AssignedTo: "dev-1",
	}
	board := &fakeBoardStore{
		getFn: func(_ context.Context, _ string) (domain.WorkItem, error) { return item, nil },
	}
	// Simulate a thread with a prior exchange. newest-first order.
	history := []domain.ChatMessage{
		// index 0 = the just-appended user question (skipped by buildBoardAskInstruction)
		{ThreadID: "board-item-4", BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "new question"},
		// index 1 = prior bot reply
		{ThreadID: "board-item-4", BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "I wrote the README."},
		// index 2 = prior user message
		{ThreadID: "board-item-4", BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "what did you write?"},
	}
	var capturedInstruction string
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
			capturedInstruction = instruction
			return domain.DirectTask{ID: "t4"}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		Chat: &fakeChatStore{
			listFn: func(_ context.Context, _ string) ([]domain.ChatMessage, error) {
				return history, nil
			},
		},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/item-4/ask", `{"content":"new question"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if !strings.Contains(capturedInstruction, "I wrote the README") {
		t.Errorf("expected prior bot reply in instruction, got:\n%s", capturedInstruction)
	}
	if !strings.Contains(capturedInstruction, "what did you write") {
		t.Errorf("expected prior user message in instruction, got:\n%s", capturedInstruction)
	}
}

// TestBoardAsk_InProgressItem_RoutesToAskRouter verifies that when a board item
// is in-progress, the question is routed to the AskRouter (not the dispatcher)
// using the concise mid-task format.
func TestBoardAsk_InProgressItem_RoutesToAskRouter(t *testing.T) {
	item := domain.WorkItem{
		ID:         "item-5",
		Title:      "Build feature X",
		Status:     domain.WorkItemStatusInProgress,
		AssignedTo: "dev-1",
	}
	board := &fakeBoardStore{
		getFn: func(_ context.Context, _ string) (domain.WorkItem, error) { return item, nil },
	}
	var routedBot, routedQuestion string
	router := &fakeAskRouter{
		enqueueFn: func(botName string, req domain.AskRequest) bool {
			routedBot = botName
			routedQuestion = req.Question
			return true
		},
	}
	dispatchCalled := false
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
			dispatchCalled = true
			return domain.DirectTask{}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
		AskRouter:  router,
		Chat:       &fakeChatStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/item-5/ask", `{"content":"how far along?"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if dispatchCalled {
		t.Error("in-progress item must route to AskRouter, not Dispatcher")
	}
	if routedBot != "dev-1" {
		t.Errorf("expected routed bot=dev-1, got %q", routedBot)
	}
	if !strings.Contains(routedQuestion, "Build feature X") {
		t.Errorf("expected item title in routed question, got: %s", routedQuestion)
	}
	if !strings.Contains(routedQuestion, "how far along") {
		t.Errorf("expected user question in routed question, got: %s", routedQuestion)
	}
}
