package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// helpers

func makeNotification(botName, message string, status domain.AgentNotificationStatus) domain.AgentNotification {
	return domain.AgentNotification{
		BotName: botName,
		Message: message,
		Status:  status,
	}
}

func newStore(t *testing.T) *orchestrator.InMemoryAgentNotificationStore {
	t.Helper()
	return orchestrator.NewInMemoryAgentNotificationStore("")
}

func newPersistStore(t *testing.T) (*orchestrator.InMemoryAgentNotificationStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.json")
	return orchestrator.NewInMemoryAgentNotificationStore(path), path
}

// --- Save ---

func TestAgentNotificationStore_Save_GeneratesID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "hello", domain.AgentNotificationStatusUnread)
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}

	list, err := s.List(ctx, domain.AgentNotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(list))
	}
	if list[0].ID == "" {
		t.Error("expected non-empty ID after Save")
	}
}

func TestAgentNotificationStore_Save_SetsCreatedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	before := time.Now().UTC()
	n := makeNotification("bot-a", "hi", domain.AgentNotificationStatusUnread)
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}
	after := time.Now().UTC()

	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	got := list[0].CreatedAt
	if got.Before(before) || got.After(after) {
		t.Errorf("CreatedAt %v out of range [%v, %v]", got, before, after)
	}
}

func TestAgentNotificationStore_Save_PreservesExistingIDAndCreatedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n := domain.AgentNotification{
		ID:        "fixed-id",
		BotName:   "bot-a",
		Message:   "existing",
		Status:    domain.AgentNotificationStatusUnread,
		CreatedAt: fixedTime,
	}
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "fixed-id")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "fixed-id" {
		t.Errorf("ID: got %q, want fixed-id", got.ID)
	}
	if !got.CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, fixedTime)
	}
}

func TestAgentNotificationStore_Save_UpdatesExisting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "first", domain.AgentNotificationStatusUnread)
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	saved := list[0]
	saved.Message = "updated"

	if err := s.Save(ctx, saved); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := s.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Message != "updated" {
		t.Errorf("Message: got %q, want updated", got.Message)
	}
}

// --- Get ---

func TestAgentNotificationStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_, err := s.Get(ctx, "missing-id")
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestAgentNotificationStore_Get_ReturnsCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "original", domain.AgentNotificationStatusUnread)
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	id := list[0].ID

	got, _ := s.Get(ctx, id)
	got.Message = "mutated"

	got2, _ := s.Get(ctx, id)
	if got2.Message == "mutated" {
		t.Error("Get should return a copy, not a reference")
	}
}

// --- List ---

func TestAgentNotificationStore_List_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	list, err := s.List(ctx, domain.AgentNotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Error("List should return non-nil slice when empty")
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}
}

func TestAgentNotificationStore_List_NewestFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	early := domain.AgentNotification{
		BotName:   "bot-a",
		Message:   "early",
		Status:    domain.AgentNotificationStatusUnread,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	late := domain.AgentNotification{
		BotName:   "bot-a",
		Message:   "late",
		Status:    domain.AgentNotificationStatusUnread,
		CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	_ = s.Save(ctx, early)
	_ = s.Save(ctx, late)

	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(list))
	}
	if list[0].Message != "late" {
		t.Errorf("expected newest first, got %q first", list[0].Message)
	}
}

func TestAgentNotificationStore_List_FilterByBotName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "from a", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-b", "from b", domain.AgentNotificationStatusUnread))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{BotName: "bot-a"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].BotName != "bot-a" {
		t.Errorf("expected bot-a, got %q", list[0].BotName)
	}
}

func TestAgentNotificationStore_List_FilterByStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "unread msg", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "read msg", domain.AgentNotificationStatusRead))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{Status: domain.AgentNotificationStatusRead})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Status != domain.AgentNotificationStatusRead {
		t.Errorf("expected read, got %q", list[0].Status)
	}
}

func TestAgentNotificationStore_List_FilterBySearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "deploy failed on prod", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "all tests passing", domain.AgentNotificationStatusUnread))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{Search: "deploy"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if !strings.Contains(list[0].Message, "deploy") {
		t.Errorf("unexpected message: %q", list[0].Message)
	}
}

func TestAgentNotificationStore_List_FilterByWorkDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, domain.AgentNotification{BotName: "bot-a", Message: "msg1", WorkDir: "/home/user/alpha", Status: domain.AgentNotificationStatusUnread})
	_ = s.Save(ctx, domain.AgentNotification{BotName: "bot-a", Message: "msg2", WorkDir: "/home/user/beta", Status: domain.AgentNotificationStatusUnread})
	_ = s.Save(ctx, domain.AgentNotification{BotName: "bot-a", Message: "msg3", WorkDir: "", Status: domain.AgentNotificationStatusUnread})

	list, err := s.List(ctx, domain.AgentNotificationFilter{WorkDir: "alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification for dir=alpha, got %d", len(list))
	}
	if list[0].Message != "msg1" {
		t.Errorf("unexpected notification: %q", list[0].Message)
	}
}

func TestAgentNotificationStore_List_FilterCombined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "deploy failed", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "deploy warning", domain.AgentNotificationStatusRead))
	_ = s.Save(ctx, makeNotification("bot-b", "deploy issue", domain.AgentNotificationStatusUnread))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{
		BotName: "bot-a",
		Status:  domain.AgentNotificationStatusUnread,
		Search:  "deploy",
	})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].BotName != "bot-a" || list[0].Status != domain.AgentNotificationStatusUnread {
		t.Error("combined filter returned wrong notification")
	}
}

// --- UnreadCount ---

func TestAgentNotificationStore_UnreadCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "msg1", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "msg2", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "msg3", domain.AgentNotificationStatusRead))

	count, err := s.UnreadCount(ctx)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestAgentNotificationStore_UnreadCount_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	count, err := s.UnreadCount(ctx)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

// --- AppendDiscuss ---

func TestAgentNotificationStore_AppendDiscuss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "issue", domain.AgentNotificationStatusUnread)
	_ = s.Save(ctx, n)
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	id := list[0].ID

	entry := domain.DiscussEntry{
		Author:    "operator",
		Message:   "looking into it",
		Timestamp: time.Now().UTC(),
	}
	if err := s.AppendDiscuss(ctx, id, entry); err != nil {
		t.Fatalf("AppendDiscuss: %v", err)
	}

	got, _ := s.Get(ctx, id)
	if len(got.DiscussThread) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.DiscussThread))
	}
	if got.DiscussThread[0].Author != "operator" {
		t.Errorf("Author: got %q, want operator", got.DiscussThread[0].Author)
	}
}

func TestAgentNotificationStore_AppendDiscuss_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	entry := domain.DiscussEntry{Author: "op", Message: "hi", Timestamp: time.Now()}
	err := s.AppendDiscuss(ctx, "nonexistent", entry)
	if err == nil {
		t.Fatal("expected error for missing notification, got nil")
	}
}

func TestAgentNotificationStore_AppendDiscuss_MultipleEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "problem", domain.AgentNotificationStatusUnread)
	_ = s.Save(ctx, n)
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	id := list[0].ID

	for i := 0; i < 5; i++ {
		entry := domain.DiscussEntry{Author: "op", Message: "msg", Timestamp: time.Now().UTC()}
		_ = s.AppendDiscuss(ctx, id, entry)
	}

	got, _ := s.Get(ctx, id)
	if len(got.DiscussThread) != 5 {
		t.Errorf("expected 5 entries, got %d", len(got.DiscussThread))
	}
}

// --- MarkActioned ---

func TestAgentNotificationStore_MarkActioned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	n := makeNotification("bot-a", "needs action", domain.AgentNotificationStatusUnread)
	_ = s.Save(ctx, n)
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	id := list[0].ID

	before := time.Now().UTC()
	if err := s.MarkActioned(ctx, id); err != nil {
		t.Fatalf("MarkActioned: %v", err)
	}
	after := time.Now().UTC()

	got, _ := s.Get(ctx, id)
	if got.Status != domain.AgentNotificationStatusActioned {
		t.Errorf("Status: got %q, want actioned", got.Status)
	}
	if got.ActionedAt == nil {
		t.Fatal("ActionedAt is nil after MarkActioned")
	}
	if got.ActionedAt.Before(before) || got.ActionedAt.After(after) {
		t.Errorf("ActionedAt %v out of range [%v, %v]", got.ActionedAt, before, after)
	}
}

func TestAgentNotificationStore_MarkActioned_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	err := s.MarkActioned(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// --- Delete ---

func TestAgentNotificationStore_Delete_RemovesNotifications(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "first", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "second", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "third", domain.AgentNotificationStatusUnread))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	ids := []string{list[0].ID, list[1].ID}

	if err := s.Delete(ctx, ids); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	remaining, _ := s.List(ctx, domain.AgentNotificationFilter{})
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(remaining))
	}
}

func TestAgentNotificationStore_Delete_MissingIDsIgnored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "msg", domain.AgentNotificationStatusUnread))

	// Deleting a mix of existing and non-existing IDs should succeed.
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	err := s.Delete(ctx, []string{list[0].ID, "nonexistent-id"})
	if err != nil {
		t.Fatalf("Delete with missing ids: %v", err)
	}

	remaining, _ := s.List(ctx, domain.AgentNotificationFilter{})
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(remaining))
	}
}

func TestAgentNotificationStore_Delete_EmptySlice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "msg", domain.AgentNotificationStatusUnread))

	if err := s.Delete(ctx, []string{}); err != nil {
		t.Fatalf("Delete with empty ids: %v", err)
	}

	remaining, _ := s.List(ctx, domain.AgentNotificationFilter{})
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(remaining))
	}
}

// --- Persistence ---

func TestAgentNotificationStore_Persists_AndReloads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, path := newPersistStore(t)

	n := makeNotification("bot-a", "important", domain.AgentNotificationStatusUnread)
	if err := s.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was written.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persist file not created: %v", err)
	}

	// Load a new store from the same path.
	s2 := orchestrator.NewInMemoryAgentNotificationStore(path)
	list, err := s2.List(ctx, domain.AgentNotificationFilter{})
	if err != nil {
		t.Fatalf("List on reloaded store: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification after reload, got %d", len(list))
	}
	if list[0].Message != "important" {
		t.Errorf("Message: got %q, want important", list[0].Message)
	}
}

func TestAgentNotificationStore_Persist_AfterDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, path := newPersistStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "first", domain.AgentNotificationStatusUnread))
	_ = s.Save(ctx, makeNotification("bot-a", "second", domain.AgentNotificationStatusUnread))

	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	_ = s.Delete(ctx, []string{list[0].ID})

	s2 := orchestrator.NewInMemoryAgentNotificationStore(path)
	list2, _ := s2.List(ctx, domain.AgentNotificationFilter{})
	if len(list2) != 1 {
		t.Errorf("expected 1 after reload post-delete, got %d", len(list2))
	}
}

func TestAgentNotificationStore_Persist_AfterMarkActioned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, path := newPersistStore(t)

	_ = s.Save(ctx, makeNotification("bot-a", "msg", domain.AgentNotificationStatusUnread))
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	id := list[0].ID
	_ = s.MarkActioned(ctx, id)

	s2 := orchestrator.NewInMemoryAgentNotificationStore(path)
	got, err := s2.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get on reloaded store: %v", err)
	}
	if got.Status != domain.AgentNotificationStatusActioned {
		t.Errorf("Status after reload: got %q, want actioned", got.Status)
	}
}

func TestAgentNotificationStore_NoPath_DoesNotPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := orchestrator.NewInMemoryAgentNotificationStore("")

	// All operations should work without a persist path.
	_ = s.Save(ctx, makeNotification("bot-a", "msg", domain.AgentNotificationStatusUnread))
	list, _ := s.List(ctx, domain.AgentNotificationFilter{})
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}
