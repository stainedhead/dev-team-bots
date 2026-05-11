package httpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/notifications"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── fake notification store ───────────────────────────────────────────────────

type fakeNotificationStore struct {
	saveFn          func(ctx context.Context, n domain.AgentNotification) error
	getFn           func(ctx context.Context, id string) (domain.AgentNotification, error)
	listFn          func(ctx context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error)
	unreadCountFn   func(ctx context.Context) (int, error)
	appendDiscussFn func(ctx context.Context, id string, entry domain.DiscussEntry) error
	markActionedFn  func(ctx context.Context, id string) error
	deleteFn        func(ctx context.Context, ids []string) error
}

func (f *fakeNotificationStore) Save(ctx context.Context, n domain.AgentNotification) error {
	if f.saveFn != nil {
		return f.saveFn(ctx, n)
	}
	return nil
}

func (f *fakeNotificationStore) Get(ctx context.Context, id string) (domain.AgentNotification, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.AgentNotification{
		ID:      id,
		BotName: "dev-1",
		Message: "blocked on review",
		Status:  domain.AgentNotificationStatusUnread,
		TaskID:  "task-1",
	}, nil
}

func (f *fakeNotificationStore) List(ctx context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}
	return []domain.AgentNotification{
		{ID: "n1", BotName: "dev-1", Message: "hello", Status: domain.AgentNotificationStatusUnread},
	}, nil
}

func (f *fakeNotificationStore) UnreadCount(ctx context.Context) (int, error) {
	if f.unreadCountFn != nil {
		return f.unreadCountFn(ctx)
	}
	return 3, nil
}

func (f *fakeNotificationStore) AppendDiscuss(ctx context.Context, id string, entry domain.DiscussEntry) error {
	if f.appendDiscussFn != nil {
		return f.appendDiscussFn(ctx, id, entry)
	}
	return nil
}

func (f *fakeNotificationStore) MarkActioned(ctx context.Context, id string) error {
	if f.markActionedFn != nil {
		return f.markActionedFn(ctx, id)
	}
	return nil
}

func (f *fakeNotificationStore) Delete(ctx context.Context, ids []string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, ids)
	}
	return nil
}

// ── fake task store for requeue ───────────────────────────────────────────────

type fakeNotifTaskStore struct {
	getFn    func(ctx context.Context, id string) (domain.DirectTask, error)
	updateFn func(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error)
}

func (f *fakeNotifTaskStore) Create(_ context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	return t, nil
}
func (f *fakeNotifTaskStore) Update(ctx context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, t)
	}
	return t, nil
}
func (f *fakeNotifTaskStore) Get(ctx context.Context, id string) (domain.DirectTask, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.DirectTask{ID: id, BotName: "dev-1", Status: domain.DirectTaskStatusBlocked, Instruction: "do work"}, nil
}
func (f *fakeNotifTaskStore) List(_ context.Context, _ string) ([]domain.DirectTask, error) {
	return []domain.DirectTask{}, nil
}
func (f *fakeNotifTaskStore) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	return []domain.DirectTask{}, nil
}
func (f *fakeNotifTaskStore) ListBySource(_ context.Context, _ domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return []domain.DirectTask{}, nil
}
func (f *fakeNotifTaskStore) Delete(_ context.Context, _ string) error { return nil }
func (f *fakeNotifTaskStore) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	return []domain.DirectTask{}, nil
}
func (f *fakeNotifTaskStore) ClaimDue(_ context.Context, _ string) (bool, error) { return false, nil }

// ── helpers ───────────────────────────────────────────────────────────────────

func newNotifSvc(store domain.AgentNotificationStore, taskStore domain.DirectTaskStore) *notifications.NotificationService {
	return notifications.NewNotificationService(store, taskStore)
}

func newNotifTestServer(svc *notifications.NotificationService) *httptest.Server {
	s := httpserver.New(httpserver.Config{
		Auth:          &fakeAuth{},
		Board:         &fakeBoardStore{},
		Team:          &fakeControlPlane{},
		Users:         &fakeUserStore{},
		Skills:        &fakeSkillRegistry{},
		DLQ:           &fakeDLQStore{},
		Tasks:         &fakeDirectTaskStore{},
		Dispatcher:    &fakeTaskDispatcher{},
		Notifications: svc,
	})
	return httptest.NewServer(s.Handler())
}

// ── Notifications tests ───────────────────────────────────────────────────────

func TestNotificationList_ReturnsList(t *testing.T) {
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "notifications", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var notifs []domain.AgentNotification
	if err := json.NewDecoder(resp.Body).Decode(&notifs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(notifs) < 1 {
		t.Fatal("expected at least one notification")
	}
}

func TestNotificationList_AppliesBotFilter(t *testing.T) {
	var capturedFilter domain.AgentNotificationFilter
	store := &fakeNotificationStore{
		listFn: func(_ context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
			capturedFilter = filter
			return []domain.AgentNotification{}, nil
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/notifications?bot=dev-1&status=unread&q=blocked", nil)
	req.Header.Set("Authorization", authHeader())
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedFilter.BotName != "dev-1" {
		t.Errorf("expected BotName=dev-1, got %q", capturedFilter.BotName)
	}
	if capturedFilter.Status != domain.AgentNotificationStatusUnread {
		t.Errorf("expected Status=unread, got %q", capturedFilter.Status)
	}
	if capturedFilter.Search != "blocked" {
		t.Errorf("expected Search=blocked, got %q", capturedFilter.Search)
	}
}

func TestNotificationList_AppliesDirFilter(t *testing.T) {
	var capturedFilter domain.AgentNotificationFilter
	store := &fakeNotificationStore{
		listFn: func(_ context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
			capturedFilter = filter
			return []domain.AgentNotification{}, nil
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/notifications?dir=myrepo", nil)
	req.Header.Set("Authorization", authHeader())
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedFilter.WorkDir != "myrepo" {
		t.Errorf("expected WorkDir=myrepo, got %q", capturedFilter.WorkDir)
	}
}

func TestNotificationList_NilService_Returns501(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		// Notifications intentionally nil
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "notifications", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}

func TestNotificationList_RequiresAuth(t *testing.T) {
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/notifications", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestNotificationCount_ReturnsUnreadCount(t *testing.T) {
	store := &fakeNotificationStore{
		unreadCountFn: func(_ context.Context) (int, error) {
			return 7, nil
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "notifications/count", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Unread int `json:"unread"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Unread != 7 {
		t.Errorf("expected unread=7, got %d", out.Unread)
	}
}

func TestNotificationCount_NilService_Returns501(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "notifications/count", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}

func TestNotificationDiscuss_AppendsEntry(t *testing.T) {
	var savedNotif domain.AgentNotification
	store := &fakeNotificationStore{
		saveFn: func(_ context.Context, n domain.AgentNotification) error {
			savedNotif = n
			return nil
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	body := `{"message":"please check the logs"}`
	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/discuss", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	// Verify the entry was saved with author from JWT claims (Subject="admin" from fakeAuth).
	if len(savedNotif.DiscussThread) != 1 {
		t.Fatalf("expected 1 discuss entry, got %d", len(savedNotif.DiscussThread))
	}
	// fakeAuth returns Subject="admin"; FR-011 uses claimsFromContext(r).Subject as author.
	if savedNotif.DiscussThread[0].Author != "admin" {
		t.Errorf("expected author=admin (from JWT claims), got %q", savedNotif.DiscussThread[0].Author)
	}
	if savedNotif.DiscussThread[0].Message != "please check the logs" {
		t.Errorf("unexpected message: %q", savedNotif.DiscussThread[0].Message)
	}
}

func TestNotificationDiscuss_EmptyMessage_Returns400(t *testing.T) {
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	body := `{"message":""}`
	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/discuss", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestNotificationDiscuss_NilService_Returns501(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/discuss", `{"message":"hi"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}

func TestNotificationRequeue_ReturnsOK(t *testing.T) {
	var updatedTask domain.DirectTask
	taskStore := &fakeNotifTaskStore{
		updateFn: func(_ context.Context, t domain.DirectTask) (domain.DirectTask, error) {
			updatedTask = t
			return t, nil
		},
	}
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, taskStore)
	srv := newNotifTestServer(svc)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/requeue", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	// Task should have been reset to pending with ASAP schedule.
	if updatedTask.Status != domain.DirectTaskStatusPending {
		t.Errorf("expected status=pending, got %q", updatedTask.Status)
	}
	if updatedTask.Schedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("expected schedule mode=asap, got %q", updatedTask.Schedule.Mode)
	}
}

func TestNotificationRequeue_RunningTask_Returns409(t *testing.T) {
	taskStore := &fakeNotifTaskStore{
		getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
			return domain.DirectTask{ID: id, BotName: "dev-1", Status: domain.DirectTaskStatusRunning, Instruction: "do work"}, nil
		},
	}
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, taskStore)
	srv := newNotifTestServer(svc)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/requeue", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestNotificationRequeue_NilService_Returns501(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "notifications/notif-abc/requeue", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}

func TestNotificationDelete_DeletesByIDs(t *testing.T) {
	var deletedIDs []string
	store := &fakeNotificationStore{
		deleteFn: func(_ context.Context, ids []string) error {
			deletedIDs = ids
			return nil
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	body := `{"ids":["n1","n2"]}`
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/notifications", strings.NewReader(body))
	req.Header.Set("Authorization", authHeader())
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	if len(deletedIDs) != 2 || deletedIDs[0] != "n1" || deletedIDs[1] != "n2" {
		t.Errorf("unexpected deleted IDs: %v", deletedIDs)
	}
}

func TestNotificationDelete_NilService_Returns501(t *testing.T) {
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/notifications", strings.NewReader(`{"ids":["n1"]}`))
	req.Header.Set("Authorization", authHeader())
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}

// ── Schedule field on task create ─────────────────────────────────────────────

func TestBotTaskCreate_WithScheduleASAP(t *testing.T) {
	var dispatchedSchedule domain.Schedule
	dispatcher := &fakeScheduledDispatcher{
		dispatchWithScheduleFn: func(_ context.Context, _, _ string, schedule domain.Schedule, _ domain.DirectTaskSource, _, _, _ string) (domain.DirectTask, error) {
			dispatchedSchedule = schedule
			return domain.DirectTask{ID: "task-sched", Status: domain.DirectTaskStatusRunning}, nil
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

	body := `{"instruction":"do it","schedule":{"mode":"asap"}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if dispatchedSchedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("expected mode=asap, got %q", dispatchedSchedule.Mode)
	}
}

func TestBotTaskCreate_WithScheduleFuture(t *testing.T) {
	var dispatchedSchedule domain.Schedule
	dispatcher := &fakeScheduledDispatcher{
		dispatchWithScheduleFn: func(_ context.Context, _, _ string, schedule domain.Schedule, _ domain.DirectTaskSource, _, _, _ string) (domain.DirectTask, error) {
			dispatchedSchedule = schedule
			return domain.DirectTask{ID: "task-fut", Status: domain.DirectTaskStatusPending}, nil
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

	future := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	body := `{"instruction":"later job","schedule":{"mode":"future","run_at":"` + future.Format(time.RFC3339) + `"}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if dispatchedSchedule.Mode != domain.ScheduleModeFuture {
		t.Errorf("expected mode=future, got %q", dispatchedSchedule.Mode)
	}
	if dispatchedSchedule.RunAt == nil {
		t.Fatal("expected RunAt to be set")
	}
}

func TestBotTaskCreate_WithScheduleRecurring(t *testing.T) {
	var dispatchedSchedule domain.Schedule
	dispatcher := &fakeScheduledDispatcher{
		dispatchWithScheduleFn: func(_ context.Context, _, _ string, schedule domain.Schedule, _ domain.DirectTaskSource, _, _, _ string) (domain.DirectTask, error) {
			dispatchedSchedule = schedule
			return domain.DirectTask{ID: "task-recur", Status: domain.DirectTaskStatusPending}, nil
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

	body := `{"instruction":"daily standup","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"09:00"}}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if dispatchedSchedule.Mode != domain.ScheduleModeRecurring {
		t.Errorf("expected mode=recurring, got %q", dispatchedSchedule.Mode)
	}
	if dispatchedSchedule.Rule == nil {
		t.Fatal("expected Rule to be set")
	}
	if dispatchedSchedule.Rule.Frequency != domain.RecurrenceFrequencyDaily {
		t.Errorf("expected frequency=daily, got %q", dispatchedSchedule.Rule.Frequency)
	}
}

func TestBotTaskCreate_WithScheduleInvalidMode_Returns400(t *testing.T) {
	dispatcher := &fakeScheduledDispatcher{}
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

	body := `{"instruction":"bad","schedule":{"mode":"hourly"}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid mode, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_WithScheduleFuture_MissingRunAt_Returns400(t *testing.T) {
	dispatcher := &fakeScheduledDispatcher{}
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

	body := `{"instruction":"later","schedule":{"mode":"future"}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for future without run_at, got %d", resp.StatusCode)
	}
}

// fakeScheduledDispatcher implements both TaskDispatcher and ScheduledTaskDispatcher.
type fakeScheduledDispatcher struct {
	dispatchWithScheduleFn func(ctx context.Context, botName, instruction string, schedule domain.Schedule, source domain.DirectTaskSource, threadID, workDir, title string) (domain.DirectTask, error)
}

func (f *fakeScheduledDispatcher) Dispatch(_ context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource, threadID, workDir string) (domain.DirectTask, error) {
	return domain.DirectTask{
		ID:          "task-fallback",
		BotName:     botName,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusRunning,
	}, nil
}

func (f *fakeScheduledDispatcher) RunNow(_ context.Context, _ string) (domain.DirectTask, error) {
	return domain.DirectTask{Status: domain.DirectTaskStatusRunning}, nil
}

func (f *fakeScheduledDispatcher) DispatchWithSchedule(ctx context.Context, botName, instruction string, schedule domain.Schedule, source domain.DirectTaskSource, threadID, workDir, title string) (domain.DirectTask, error) {
	if f.dispatchWithScheduleFn != nil {
		return f.dispatchWithScheduleFn(ctx, botName, instruction, schedule, source, threadID, workDir, title)
	}
	return domain.DirectTask{
		ID:          "task-sched",
		BotName:     botName,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusPending,
		Schedule:    schedule,
	}, nil
}

// FR-004: HH:MM range validation.
// These tests exercise parseRecurrenceRequest (via the HTTP handler) to verify
// that invalid hour/minute values are rejected with 400.

func TestBotTaskCreate_WithScheduleRecurring_InvalidHour_Returns400(t *testing.T) {
	dispatcher := &fakeScheduledDispatcher{}
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

	body := `{"instruction":"bad hour","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"25:00"}}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid hour 25, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_WithScheduleRecurring_InvalidMinute_Returns400(t *testing.T) {
	dispatcher := &fakeScheduledDispatcher{}
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

	body := `{"instruction":"bad minute","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"9:99"}}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid minute 99, got %d", resp.StatusCode)
	}
}

func TestBotTaskCreate_WithScheduleRecurring_ValidTime2359_Returns201(t *testing.T) {
	dispatcher := &fakeScheduledDispatcher{}
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

	body := `{"instruction":"valid time","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"23:59"}}}`
	resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for valid time 23:59, got %d", resp.StatusCode)
	}
}

// FR-015: Table-driven schedule parse tests via HTTP handler.
func TestBotTaskCreate_ScheduleParsing_TableDriven(t *testing.T) {
	futureTime := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	tests := []struct {
		name           string
		body           string
		wantStatusCode int
	}{
		{
			name:           "nil schedule (no schedule field) — ASAP",
			body:           `{"instruction":"do it"}`,
			wantStatusCode: http.StatusCreated,
		},
		{
			name:           "explicit asap mode",
			body:           `{"instruction":"do it","schedule":{"mode":"asap"}}`,
			wantStatusCode: http.StatusCreated,
		},
		{
			name:           "unknown mode → error",
			body:           `{"instruction":"bad","schedule":{"mode":"hourly"}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "future mode with nil run_at → error",
			body:           `{"instruction":"later","schedule":{"mode":"future"}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "recurring with nil recurrence → error",
			body:           `{"instruction":"daily","schedule":{"mode":"recurring"}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "recurring with invalid frequency → error",
			body:           `{"instruction":"hourly","schedule":{"mode":"recurring","recurrence":{"frequency":"hourly","time":"09:00"}}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "recurring with invalid time (hour>23) → error",
			body:           `{"instruction":"bad hour","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"25:00"}}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "recurring with invalid time (minute>59) → error",
			body:           `{"instruction":"bad min","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"09:99"}}}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "valid future schedule",
			body:           `{"instruction":"later","schedule":{"mode":"future","run_at":"` + futureTime.Format(time.RFC3339) + `"}}`,
			wantStatusCode: http.StatusCreated,
		},
		{
			name:           "valid daily recurring schedule",
			body:           `{"instruction":"daily","schedule":{"mode":"recurring","recurrence":{"frequency":"daily","time":"09:00"}}}`,
			wantStatusCode: http.StatusCreated,
		},
		{
			name:           "valid weekly recurring schedule",
			body:           `{"instruction":"weekly","schedule":{"mode":"recurring","recurrence":{"frequency":"weekly","days":["monday"],"time":"09:00"}}}`,
			wantStatusCode: http.StatusCreated,
		},
		{
			name:           "valid monthly recurring schedule",
			body:           `{"instruction":"monthly","schedule":{"mode":"recurring","recurrence":{"frequency":"monthly","month_day":15,"time":"09:00"}}}`,
			wantStatusCode: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher := &fakeScheduledDispatcher{}
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

			resp := doJSON(t, srv, http.MethodPost, "bots/dev-1/tasks", tc.body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatusCode {
				t.Errorf("expected %d, got %d", tc.wantStatusCode, resp.StatusCode)
			}
		})
	}
}

// FR-016: Empty IDs → 400.
func TestHandleNotificationDelete_EmptyIDs_Returns400(t *testing.T) {
	store := &fakeNotificationStore{}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/notifications", strings.NewReader(`{"ids":[]}`))
	req.Header.Set("Authorization", authHeader())
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ids, got %d", resp.StatusCode)
	}
}

func TestNotificationList_StoreError_Returns500(t *testing.T) {
	store := &fakeNotificationStore{
		listFn: func(_ context.Context, _ domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
			return nil, errors.New("store failure")
		},
	}
	svc := newNotifSvc(store, &fakeNotifTaskStore{})
	srv := newNotifTestServer(svc)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "notifications", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}
