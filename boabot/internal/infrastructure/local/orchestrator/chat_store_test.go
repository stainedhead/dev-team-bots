package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// helper: create a store with no persistence and a default thread.
func newStoreWithThread(t *testing.T) (*orchestrator.InMemoryChatStore, string) {
	t.Helper()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()
	thread, err := store.CreateThread(ctx, "test thread", []string{"dev-1"})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	return store, thread.ID
}

func TestInMemoryChatStore_Append_AssignsID(t *testing.T) {
	t.Parallel()
	store, threadID := newStoreWithThread(t)
	ctx := context.Background()

	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   "dev-1",
		Direction: domain.ChatDirectionOutbound,
		Content:   "hello",
	}
	if err := store.Append(ctx, msg); err != nil {
		t.Fatalf("Append returned unexpected error: %v", err)
	}

	all, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 message, got %d", len(all))
	}
	if all[0].ID == "" {
		t.Error("expected non-empty ID after Append")
	}
}

func TestInMemoryChatStore_Append_SetsCreatedAt(t *testing.T) {
	t.Parallel()
	store, threadID := newStoreWithThread(t)
	ctx := context.Background()

	before := time.Now()
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   "dev-1",
		Direction: domain.ChatDirectionOutbound,
		Content:   "hello",
	}
	if err := store.Append(ctx, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}
	after := time.Now()

	all, _ := store.ListAll(ctx)
	ts := all[0].CreatedAt
	if ts.Before(before) || ts.After(after) {
		t.Errorf("CreatedAt %v not in expected range [%v, %v]", ts, before, after)
	}
}

func TestInMemoryChatStore_Append_PreservesProvidedID(t *testing.T) {
	t.Parallel()
	store, threadID := newStoreWithThread(t)
	ctx := context.Background()

	msg := domain.ChatMessage{
		ID:        "explicit-id",
		ThreadID:  threadID,
		BotName:   "dev-1",
		Direction: domain.ChatDirectionOutbound,
		Content:   "hello",
	}
	if err := store.Append(ctx, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, _ := store.ListAll(ctx)
	if all[0].ID != "explicit-id" {
		t.Errorf("expected ID=explicit-id, got %q", all[0].ID)
	}
}

func TestInMemoryChatStore_List_FiltersByThreadID(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	t1, _ := store.CreateThread(ctx, "thread-1", []string{"dev-1"})
	t2, _ := store.CreateThread(ctx, "thread-2", []string{"qa-1"})

	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "a"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "b"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t2.ID, BotName: "qa-1", Direction: domain.ChatDirectionOutbound, Content: "c"})

	msgs, err := store.List(ctx, t1.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages for thread-1, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.ThreadID != t1.ID {
			t.Errorf("unexpected ThreadID: %q", m.ThreadID)
		}
	}
}

func TestInMemoryChatStore_List_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	msgs, err := store.List(ctx, "nonexistent-thread")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if msgs == nil {
		t.Error("expected non-nil slice from List on empty store")
	}
}

func TestInMemoryChatStore_ListAll_ReturnsNewestFirst(t *testing.T) {
	t.Parallel()
	store, threadID := newStoreWithThread(t)
	ctx := context.Background()

	base := time.Now().UTC()
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: threadID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "first", CreatedAt: base.Add(-2 * time.Second)})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: threadID, BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "second", CreatedAt: base.Add(-1 * time.Second)})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: threadID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "third", CreatedAt: base})

	msgs, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Newest first — last appended should appear first.
	if msgs[0].Content != "third" {
		t.Errorf("expected newest message first (third), got %q", msgs[0].Content)
	}
	if msgs[2].Content != "first" {
		t.Errorf("expected oldest message last (first), got %q", msgs[2].Content)
	}
}

func TestInMemoryChatStore_ListAll_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	msgs, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if msgs == nil {
		t.Error("expected non-nil slice from ListAll on empty store")
	}
}

func TestInMemoryChatStore_Append_PreservesProvidedCreatedAt(t *testing.T) {
	t.Parallel()
	store, threadID := newStoreWithThread(t)
	ctx := context.Background()

	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   "dev-1",
		Direction: domain.ChatDirectionOutbound,
		Content:   "hello",
		CreatedAt: fixedTime,
	}
	if err := store.Append(ctx, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, _ := store.ListAll(ctx)
	if !all[0].CreatedAt.Equal(fixedTime) {
		t.Errorf("expected CreatedAt=%v, got %v", fixedTime, all[0].CreatedAt)
	}
}

func TestInMemoryChatStore_CreateThread_AssignsID(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	thread, err := store.CreateThread(ctx, "my thread", []string{"dev-1", "qa-1"})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if thread.ID == "" {
		t.Error("expected non-empty thread ID")
	}
	if thread.Title != "my thread" {
		t.Errorf("expected title=my thread, got %q", thread.Title)
	}
}

func TestInMemoryChatStore_ListThreads_ReturnsSortedByUpdatedAt(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	t1, _ := store.CreateThread(ctx, "older", []string{"dev-1"})
	t2, _ := store.CreateThread(ctx, "newer", []string{"qa-1"})

	// Append a message to t2 to advance its UpdatedAt.
	_ = store.Append(ctx, domain.ChatMessage{
		ThreadID:  t2.ID,
		BotName:   "qa-1",
		Direction: domain.ChatDirectionOutbound,
		Content:   "ping",
	})

	threads, err := store.ListThreads(ctx)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	// t2 was updated by the message append so it should appear first.
	if threads[0].ID != t2.ID {
		t.Errorf("expected newest thread first (%s), got %s", t2.ID, threads[0].ID)
	}
	_ = t1
}

func TestInMemoryChatStore_DeleteThread_RemovesThreadAndMessages(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	t1, _ := store.CreateThread(ctx, "to-delete", []string{"dev-1"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "bye"})

	if err := store.DeleteThread(ctx, t1.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}

	threads, _ := store.ListThreads(ctx)
	for _, th := range threads {
		if th.ID == t1.ID {
			t.Error("thread still present after delete")
		}
	}

	msgs, _ := store.List(ctx, t1.ID)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after thread delete, got %d", len(msgs))
	}
}

func TestInMemoryChatStore_ListByBot_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore("")
	ctx := context.Background()

	t1, _ := store.CreateThread(ctx, "chat", []string{"dev-1", "qa-1"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "a"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "qa-1", Direction: domain.ChatDirectionOutbound, Content: "b"})
	_ = store.Append(ctx, domain.ChatMessage{ThreadID: t1.ID, BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "c"})

	msgs, err := store.ListByBot(ctx, "dev-1")
	if err != nil {
		t.Fatalf("ListByBot: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages for dev-1, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.BotName != "dev-1" {
			t.Errorf("unexpected BotName: %q", m.BotName)
		}
	}
}
