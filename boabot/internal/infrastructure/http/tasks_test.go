package httpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── fake stores for tasks ─────────────────────────────────────────────────────

type fakeDirectTaskStore struct {
	createFn  func(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error)
	updateFn  func(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error)
	getFn     func(ctx context.Context, id string) (domain.DirectTask, error)
	listFn    func(ctx context.Context, botName string) ([]domain.DirectTask, error)
	listAllFn func(ctx context.Context) ([]domain.DirectTask, error)
}

func (f *fakeDirectTaskStore) Create(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	if f.createFn != nil {
		return f.createFn(ctx, task)
	}
	task.ID = "task-1"
	return task, nil
}

func (f *fakeDirectTaskStore) Update(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, task)
	}
	return task, nil
}

func (f *fakeDirectTaskStore) Get(ctx context.Context, id string) (domain.DirectTask, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.DirectTask{ID: id, BotName: "dev-1"}, nil
}

func (f *fakeDirectTaskStore) List(ctx context.Context, botName string) ([]domain.DirectTask, error) {
	if f.listFn != nil {
		return f.listFn(ctx, botName)
	}
	return []domain.DirectTask{{ID: "task-1", BotName: botName, Status: domain.DirectTaskStatusPending}}, nil
}

func (f *fakeDirectTaskStore) ListAll(ctx context.Context) ([]domain.DirectTask, error) {
	if f.listAllFn != nil {
		return f.listAllFn(ctx)
	}
	return []domain.DirectTask{
		{ID: "task-1", BotName: "dev-1", Status: domain.DirectTaskStatusPending},
		{ID: "task-2", BotName: "qa-1", Status: domain.DirectTaskStatusDispatched},
	}, nil
}

func (f *fakeDirectTaskStore) ListBySource(_ context.Context, _ domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return []domain.DirectTask{}, nil
}

type fakeTaskDispatcher struct {
	dispatchFn func(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource) (domain.DirectTask, error)
}

func (f *fakeTaskDispatcher) Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource) (domain.DirectTask, error) {
	if f.dispatchFn != nil {
		return f.dispatchFn(ctx, botName, instruction, scheduledAt, source)
	}
	return domain.DirectTask{
		ID:          "task-new",
		BotName:     botName,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusDispatched,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newTaskTestServer() *httptest.Server {
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: &fakeTaskDispatcher{},
	})
	return httptest.NewServer(s.Handler())
}

// ── Task handler tests ────────────────────────────────────────────────────────

func TestTaskList_ReturnsAllTasks(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var tasks []domain.DirectTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) < 1 {
		t.Fatal("expected at least one task")
	}
}

func TestTaskList_RequiresAuth(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/tasks", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_Immediate(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	body := `{"instruction":"deploy to staging"}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var task domain.DirectTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
}

func TestBotTaskCreate_WithScheduledAt(t *testing.T) {
	var capturedScheduledAt *time.Time
	var capturedBotName, capturedInstruction string

	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, botName, instruction string, scheduledAt *time.Time, _ domain.DirectTaskSource) (domain.DirectTask, error) {
			capturedBotName = botName
			capturedInstruction = instruction
			capturedScheduledAt = scheduledAt
			return domain.DirectTask{
				ID:          "task-sched",
				BotName:     botName,
				Instruction: instruction,
				Status:      domain.DirectTaskStatusPending,
				ScheduledAt: scheduledAt,
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
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	schedTime := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	body := `{"instruction":"nightly job","scheduled_at":"` + schedTime.Format(time.RFC3339) + `"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/bots/dev-1/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if capturedBotName != "dev-1" {
		t.Errorf("expected botName=dev-1, got %q", capturedBotName)
	}
	if capturedInstruction != "nightly job" {
		t.Errorf("expected instruction=nightly job, got %q", capturedInstruction)
	}
	if capturedScheduledAt == nil {
		t.Fatal("expected non-nil scheduled_at to be forwarded")
	}
}

func TestBotTaskCreate_EmptyInstruction_Returns400(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	body := `{"instruction":""}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_RequiresAuth(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/bots/dev-1/tasks",
		strings.NewReader(`{"instruction":"do something"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_DispatcherError_Returns500(t *testing.T) {
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource) (domain.DirectTask, error) {
			return domain.DirectTask{}, errors.New("queue full")
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
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", `{"instruction":"fail me"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBotTaskList_ReturnsTasksForBot(t *testing.T) {
	var capturedBotName string
	store := &fakeDirectTaskStore{
		listFn: func(_ context.Context, botName string) ([]domain.DirectTask, error) {
			capturedBotName = botName
			return []domain.DirectTask{
				{ID: "t1", BotName: botName, Status: domain.DirectTaskStatusPending},
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
		Tasks:      store,
		Dispatcher: &fakeTaskDispatcher{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "bots/dev-1/tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedBotName != "dev-1" {
		t.Errorf("expected bot name dev-1, got %q", capturedBotName)
	}
	var tasks []domain.DirectTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
}

func TestBotTaskList_RequiresAuth(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bots/dev-1/tasks", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTaskList_NilStore_Returns503(t *testing.T) {
	// No Tasks or Dispatcher set.
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		// Tasks and Dispatcher intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Tasks is nil, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_NilDispatcher_Returns503(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		// Dispatcher intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", `{"instruction":"do something"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Dispatcher is nil, got %d", resp.StatusCode)
	}
}

func TestBotTaskList_StoreError_Returns500(t *testing.T) {
	store := &fakeDirectTaskStore{
		listFn: func(_ context.Context, _ string) ([]domain.DirectTask, error) {
			return nil, errors.New("store failure")
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      store,
		Dispatcher: &fakeTaskDispatcher{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "bots/dev-1/tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestTaskList_StoreError_Returns500(t *testing.T) {
	store := &fakeDirectTaskStore{
		listAllFn: func(_ context.Context) ([]domain.DirectTask, error) {
			return nil, errors.New("store failure")
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      store,
		Dispatcher: &fakeTaskDispatcher{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBotTaskList_NilStore_Returns503(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		// Tasks and Dispatcher intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "bots/dev-1/tasks", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Tasks is nil, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_InvalidBody_Returns400(t *testing.T) {
	srv := newTaskTestServer()
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", "not-json")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestKanbanUI_HasTasksEndpoint(t *testing.T) {
	srv := newTaskTestServer()
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
	if !strings.Contains(body, "/api/v1/tasks") {
		t.Error("kanban UI must reference /api/v1/tasks")
	}
}
