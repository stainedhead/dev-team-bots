package notifications_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/notifications"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/mocks"
)

// ---------------------------------------------------------------------------
// Fake DirectTaskStore for unit tests
// ---------------------------------------------------------------------------

type fakeDirectTaskStore struct {
	mu      sync.RWMutex
	tasks   map[string]domain.DirectTask
	updates []domain.DirectTask
}

func newFakeDirectTaskStore() *fakeDirectTaskStore {
	return &fakeDirectTaskStore{tasks: make(map[string]domain.DirectTask)}
}

func (f *fakeDirectTaskStore) Create(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task.ID = "fake-id"
	f.tasks[task.ID] = task
	return task, nil
}

func (f *fakeDirectTaskStore) Update(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[task.ID]; !ok {
		return domain.DirectTask{}, errors.New("fake: task not found")
	}
	f.tasks[task.ID] = task
	f.updates = append(f.updates, task)
	return task, nil
}

func (f *fakeDirectTaskStore) Get(_ context.Context, id string) (domain.DirectTask, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.tasks[id]
	if !ok {
		return domain.DirectTask{}, errors.New("fake: task not found")
	}
	return t, nil
}

func (f *fakeDirectTaskStore) List(_ context.Context, _ string) ([]domain.DirectTask, error) {
	return nil, nil
}

func (f *fakeDirectTaskStore) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	return nil, nil
}

func (f *fakeDirectTaskStore) ListBySource(_ context.Context, _ domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return nil, nil
}

func (f *fakeDirectTaskStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *fakeDirectTaskStore) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	return nil, nil
}

func (f *fakeDirectTaskStore) ClaimDue(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// addTask seeds the store with the given task (using its existing ID).
func (f *fakeDirectTaskStore) addTask(task domain.DirectTask) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = task
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newSvc(t *testing.T) (*notifications.NotificationService, *mocks.InMemoryAgentNotificationStore, *fakeDirectTaskStore) {
	t.Helper()
	ns := mocks.NewInMemoryAgentNotificationStore()
	ts := newFakeDirectTaskStore()
	svc := notifications.NewNotificationService(ns, ts)
	return svc, ns, ts
}

// ---------------------------------------------------------------------------
// RaiseNotification tests
// ---------------------------------------------------------------------------

func TestRaiseNotification_PersistsWithCorrectFields(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	before := time.Now().UTC()
	n, err := svc.RaiseNotification(ctx, "bot-a", "task-1", "work-1", "hello", "ctx-sum")
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ID == "" {
		t.Error("expected non-empty ID")
	}
	if n.BotName != "bot-a" {
		t.Errorf("BotName: got %q, want %q", n.BotName, "bot-a")
	}
	if n.TaskID != "task-1" {
		t.Errorf("TaskID: got %q, want %q", n.TaskID, "task-1")
	}
	if n.WorkItemID != "work-1" {
		t.Errorf("WorkItemID: got %q, want %q", n.WorkItemID, "work-1")
	}
	if n.Message != "hello" {
		t.Errorf("Message: got %q, want %q", n.Message, "hello")
	}
	if n.ContextSummary != "ctx-sum" {
		t.Errorf("ContextSummary: got %q, want %q", n.ContextSummary, "ctx-sum")
	}
	if n.Status != domain.AgentNotificationStatusUnread {
		t.Errorf("Status: got %q, want unread", n.Status)
	}
	if n.CreatedAt.Before(before) || n.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not within [%v, %v]", n.CreatedAt, before, after)
	}

	// Must be persisted in the store.
	saved, err := store.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("notification not persisted: %v", err)
	}
	if saved.ID != n.ID {
		t.Errorf("stored ID mismatch: got %q, want %q", saved.ID, n.ID)
	}
}

func TestRaiseNotification_EmptyOptionalFields(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	n, err := svc.RaiseNotification(ctx, "bot-b", "", "", "msg", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.TaskID != "" {
		t.Errorf("expected empty TaskID, got %q", n.TaskID)
	}
	if n.WorkItemID != "" {
		t.Errorf("expected empty WorkItemID, got %q", n.WorkItemID)
	}
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

func TestList_DelegatesToStoreWithFilter(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	// Seed two notifications.
	n1, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "first", "")
	n2, _ := svc.RaiseNotification(ctx, "bot-b", "", "", "second", "")
	_ = n1
	_ = n2

	// List all.
	all, err := svc.List(ctx, domain.AgentNotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List all: got %d, want 2", len(all))
	}

	// List with bot filter — use store directly to confirm delegation.
	filtered, err := svc.List(ctx, domain.AgentNotificationFilter{BotName: "bot-a"})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("List bot-a: got %d, want 1", len(filtered))
	}

	// Confirm store SaveCalls length.
	if len(store.SaveCalls) != 2 {
		t.Errorf("SaveCalls: got %d, want 2", len(store.SaveCalls))
	}
}

// ---------------------------------------------------------------------------
// UnreadCount tests
// ---------------------------------------------------------------------------

func TestUnreadCount_DelegatesToStore(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	// Raise two notifications (both unread).
	_, _ = svc.RaiseNotification(ctx, "bot-a", "", "", "one", "")
	_, _ = svc.RaiseNotification(ctx, "bot-a", "", "", "two", "")

	count, err := svc.UnreadCount(ctx)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("UnreadCount: got %d, want 2", count)
	}
}

// ---------------------------------------------------------------------------
// AppendDiscuss tests
// ---------------------------------------------------------------------------

func TestAppendDiscuss_EntryAppended(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "msg", "")

	err := svc.AppendDiscuss(ctx, n.ID, "operator", "reply here")
	if err != nil {
		t.Fatalf("AppendDiscuss: %v", err)
	}

	updated, err := store.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if len(updated.DiscussThread) != 1 {
		t.Fatalf("DiscussThread len: got %d, want 1", len(updated.DiscussThread))
	}
	if updated.DiscussThread[0].Author != "operator" {
		t.Errorf("Author: got %q, want %q", updated.DiscussThread[0].Author, "operator")
	}
	if updated.DiscussThread[0].Message != "reply here" {
		t.Errorf("Message: got %q, want %q", updated.DiscussThread[0].Message, "reply here")
	}
}

func TestAppendDiscuss_StatusTransitionsUnreadToRead(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "msg", "")
	if n.Status != domain.AgentNotificationStatusUnread {
		t.Fatalf("precondition: status=%q, want unread", n.Status)
	}

	_ = svc.AppendDiscuss(ctx, n.ID, "operator", "hello")

	updated, _ := store.Get(ctx, n.ID)
	if updated.Status != domain.AgentNotificationStatusRead {
		t.Errorf("Status after AppendDiscuss: got %q, want read", updated.Status)
	}
}

func TestAppendDiscuss_NoStatusTransitionIfAlreadyRead(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "msg", "")
	// First AppendDiscuss — transitions to read.
	_ = svc.AppendDiscuss(ctx, n.ID, "operator", "first")
	// Second AppendDiscuss — must stay read, not revert.
	_ = svc.AppendDiscuss(ctx, n.ID, "bot-a", "second")

	updated, _ := store.Get(ctx, n.ID)
	if updated.Status != domain.AgentNotificationStatusRead {
		t.Errorf("Status after second AppendDiscuss: got %q, want read", updated.Status)
	}
	if len(updated.DiscussThread) != 2 {
		t.Errorf("DiscussThread len: got %d, want 2", len(updated.DiscussThread))
	}
}

func TestAppendDiscuss_HundredEntryCap(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "msg", "")

	// Append 100 entries.
	for i := range 100 {
		author := "author"
		msg := "entry"
		if i == 0 {
			msg = "oldest" // track the oldest entry
			author = "oldest-author"
		}
		err := svc.AppendDiscuss(ctx, n.ID, author, msg)
		if err != nil {
			t.Fatalf("AppendDiscuss[%d]: %v", i, err)
		}
	}

	// Confirm 100 entries.
	got, _ := store.Get(ctx, n.ID)
	if len(got.DiscussThread) != 100 {
		t.Fatalf("before 101st: got %d entries, want 100", len(got.DiscussThread))
	}
	if got.DiscussThread[0].Message != "oldest" {
		t.Errorf("first entry should be 'oldest', got %q", got.DiscussThread[0].Message)
	}

	// Append the 101st — oldest must be removed.
	err := svc.AppendDiscuss(ctx, n.ID, "new-author", "newest")
	if err != nil {
		t.Fatalf("101st AppendDiscuss: %v", err)
	}

	got, _ = store.Get(ctx, n.ID)
	if len(got.DiscussThread) != 100 {
		t.Errorf("after 101st: got %d entries, want 100", len(got.DiscussThread))
	}
	// Oldest (index 0) must be gone; newest must be last.
	if got.DiscussThread[0].Message == "oldest" {
		t.Error("oldest entry was not removed")
	}
	if got.DiscussThread[99].Message != "newest" {
		t.Errorf("last entry: got %q, want 'newest'", got.DiscussThread[99].Message)
	}
}

// ---------------------------------------------------------------------------
// ActionNotification tests
// ---------------------------------------------------------------------------

func TestActionNotification_SetsStatusActioned(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "msg", "")

	err := svc.ActionNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("ActionNotification: %v", err)
	}

	updated, _ := store.Get(ctx, n.ID)
	if updated.Status != domain.AgentNotificationStatusActioned {
		t.Errorf("Status: got %q, want actioned", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// RequeueTask tests
// ---------------------------------------------------------------------------

func TestRequeueTask_PrependContextAndSetsPending(t *testing.T) {
	svc, _, ts := newSvc(t)
	ctx := context.Background()

	task := domain.DirectTask{
		ID:          "task-abc",
		BotName:     "bot-a",
		Instruction: "original instruction",
		Status:      domain.DirectTaskStatusSucceeded,
	}
	ts.addTask(task)

	n, _ := svc.RaiseNotification(ctx, "bot-a", "task-abc", "", "blocked", "")
	_ = svc.AppendDiscuss(ctx, n.ID, "operator", "please retry with X")
	_ = svc.AppendDiscuss(ctx, n.ID, "bot-a", "understood")

	err := svc.RequeueTask(ctx, n.ID)
	if err != nil {
		t.Fatalf("RequeueTask: %v", err)
	}

	if len(ts.updates) == 0 {
		t.Fatal("expected taskStore.Update to be called")
	}
	updated := ts.updates[len(ts.updates)-1]

	if updated.Status != domain.DirectTaskStatusPending {
		t.Errorf("task status: got %q, want pending", updated.Status)
	}
	if updated.NextRunAt != nil {
		t.Errorf("NextRunAt: expected nil, got %v", updated.NextRunAt)
	}
	if updated.Schedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("Schedule.Mode: got %q, want asap", updated.Schedule.Mode)
	}

	// Verify discuss context is prepended with separator before original instruction.
	if !strings.Contains(updated.Instruction, "operator: please retry with X") {
		t.Errorf("discuss context not prepended: %q", updated.Instruction)
	}
	if !strings.Contains(updated.Instruction, "bot-a: understood") {
		t.Errorf("discuss context not prepended: %q", updated.Instruction)
	}
	if !strings.Contains(updated.Instruction, "original instruction") {
		t.Errorf("original instruction missing: %q", updated.Instruction)
	}
	if !strings.Contains(updated.Instruction, "---") {
		t.Errorf("separator missing in instruction: %q", updated.Instruction)
	}
	if !strings.Contains(updated.Instruction, n.ID) {
		t.Errorf("notification ID missing in header: %q", updated.Instruction)
	}
}

func TestRequeueTask_NoTaskID_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	// Notification with no TaskID.
	n, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "blocked", "")

	err := svc.RequeueTask(ctx, n.ID)
	if err == nil {
		t.Fatal("expected error for notification with no TaskID")
	}
}

func TestRequeueTask_TaskNotFound_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	// Notification references a task that doesn't exist in the store.
	n, _ := svc.RaiseNotification(ctx, "bot-a", "nonexistent-task", "", "blocked", "")

	err := svc.RequeueTask(ctx, n.ID)
	if err == nil {
		t.Fatal("expected error when task not found")
	}
}

func TestRequeueTask_TaskRunning_ReturnsRequeueConflictErr(t *testing.T) {
	svc, _, ts := newSvc(t)
	ctx := context.Background()

	task := domain.DirectTask{
		ID:          "task-running",
		BotName:     "bot-a",
		Instruction: "do something",
		Status:      domain.DirectTaskStatusRunning,
	}
	ts.addTask(task)

	n, _ := svc.RaiseNotification(ctx, "bot-a", "task-running", "", "blocked", "")

	err := svc.RequeueTask(ctx, n.ID)
	if !errors.Is(err, notifications.ErrRequeueConflict) {
		t.Errorf("expected RequeueConflictErr, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestDelete_DelegatesToStore(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()

	n1, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "one", "")
	n2, _ := svc.RaiseNotification(ctx, "bot-a", "", "", "two", "")

	err := svc.Delete(ctx, []string{n1.ID, n2.ID})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	all, _ := store.List(ctx, domain.AgentNotificationFilter{})
	if len(all) != 0 {
		t.Errorf("after Delete: got %d notifications, want 0", len(all))
	}
}
