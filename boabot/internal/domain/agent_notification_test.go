package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/mocks"
)

// ---- AgentNotificationFilter logic via MockAgentNotificationStore ----

func TestAgentNotificationStore_List_FilterByStatus(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	n1 := domain.AgentNotification{
		ID:      "1",
		BotName: "dev-1",
		Message: "hello",
		Status:  domain.AgentNotificationStatusUnread,
	}
	n2 := domain.AgentNotification{
		ID:      "2",
		BotName: "dev-1",
		Message: "world",
		Status:  domain.AgentNotificationStatusRead,
	}
	_ = store.Save(ctx, n1)
	_ = store.Save(ctx, n2)

	results, err := store.List(ctx, domain.AgentNotificationFilter{Status: domain.AgentNotificationStatusUnread})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 || results[0].ID != "1" {
		t.Errorf("expected 1 unread result with ID=1, got %v", results)
	}
}

func TestAgentNotificationStore_List_FilterByBotName(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", BotName: "dev-1", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", BotName: "qa-1", Status: domain.AgentNotificationStatusUnread})

	results, err := store.List(ctx, domain.AgentNotificationFilter{BotName: "dev-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 || results[0].ID != "1" {
		t.Errorf("expected 1 result for dev-1, got %v", results)
	}
}

func TestAgentNotificationStore_List_FilterBySearch(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", BotName: "dev-1", Message: "build failed", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", BotName: "dev-1", Message: "tests passed", Status: domain.AgentNotificationStatusUnread})

	results, err := store.List(ctx, domain.AgentNotificationFilter{Search: "failed"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 || results[0].ID != "1" {
		t.Errorf("expected 1 result matching 'failed', got %v", results)
	}
}

func TestAgentNotificationStore_List_EmptyFilter_ReturnsAll(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", BotName: "dev-1", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", BotName: "qa-1", Status: domain.AgentNotificationStatusRead})

	results, err := store.List(ctx, domain.AgentNotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with empty filter, got %d", len(results))
	}
}

func TestAgentNotificationStore_Get_ReturnsNotification(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	n := domain.AgentNotification{ID: "abc", BotName: "dev-1", Message: "hi", Status: domain.AgentNotificationStatusUnread}
	_ = store.Save(ctx, n)

	got, err := store.Get(ctx, "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "abc" || got.Message != "hi" {
		t.Errorf("Get returned unexpected notification: %+v", got)
	}
}

func TestAgentNotificationStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nope")
	if err == nil {
		t.Error("expected error for missing notification, got nil")
	}
}

func TestAgentNotificationStore_UnreadCount(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "3", Status: domain.AgentNotificationStatusRead})

	count, err := store.UnreadCount(ctx)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 unread, got %d", count)
	}
}

func TestAgentNotificationStore_AppendDiscuss(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", Status: domain.AgentNotificationStatusUnread})

	entry := domain.DiscussEntry{Author: "user", Message: "please clarify", Timestamp: time.Now()}
	if err := store.AppendDiscuss(ctx, "1", entry); err != nil {
		t.Fatalf("AppendDiscuss: %v", err)
	}

	got, _ := store.Get(ctx, "1")
	if len(got.DiscussThread) != 1 || got.DiscussThread[0].Message != "please clarify" {
		t.Errorf("expected discuss thread with one entry, got %v", got.DiscussThread)
	}
}

func TestAgentNotificationStore_MarkActioned(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", Status: domain.AgentNotificationStatusUnread})

	if err := store.MarkActioned(ctx, "1"); err != nil {
		t.Fatalf("MarkActioned: %v", err)
	}

	got, _ := store.Get(ctx, "1")
	if got.Status != domain.AgentNotificationStatusActioned {
		t.Errorf("expected status=actioned, got %q", got.Status)
	}
	if got.ActionedAt == nil {
		t.Error("expected ActionedAt to be set after MarkActioned")
	}
}

func TestAgentNotificationStore_Delete(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", Status: domain.AgentNotificationStatusRead})

	if err := store.Delete(ctx, []string{"1"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	all, _ := store.List(ctx, domain.AgentNotificationFilter{})
	if len(all) != 1 || all[0].ID != "2" {
		t.Errorf("expected only ID=2 remaining after delete, got %v", all)
	}
}

func TestAgentNotificationStore_Delete_NotFound_NoError(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	if err := store.Delete(ctx, []string{"nonexistent"}); err != nil {
		t.Errorf("Delete of nonexistent IDs should not error, got: %v", err)
	}
}

func TestAgentNotificationStore_List_FilterByWorkDir(t *testing.T) {
	t.Parallel()
	store := mocks.NewInMemoryAgentNotificationStore()
	ctx := context.Background()

	_ = store.Save(ctx, domain.AgentNotification{ID: "1", BotName: "dev-1", WorkDir: "/home/user/projects/alpha", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "2", BotName: "dev-1", WorkDir: "/home/user/projects/beta", Status: domain.AgentNotificationStatusUnread})
	_ = store.Save(ctx, domain.AgentNotification{ID: "3", BotName: "dev-1", WorkDir: "", Status: domain.AgentNotificationStatusUnread})

	results, err := store.List(ctx, domain.AgentNotificationFilter{WorkDir: "alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 || results[0].ID != "1" {
		t.Errorf("expected 1 result for dir=alpha, got %v", results)
	}
}
