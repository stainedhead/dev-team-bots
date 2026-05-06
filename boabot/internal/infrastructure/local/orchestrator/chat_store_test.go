package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

func TestInMemoryChatStore_Append_AssignsID(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	msg := domain.ChatMessage{
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
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	before := time.Now()
	msg := domain.ChatMessage{
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
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	msg := domain.ChatMessage{
		ID:        "explicit-id",
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

func TestInMemoryChatStore_List_FiltersByBotName(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "a"})
	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "b"})
	_ = store.Append(ctx, domain.ChatMessage{BotName: "qa-1", Direction: domain.ChatDirectionOutbound, Content: "c"})

	msgs, err := store.List(ctx, "dev-1")
	if err != nil {
		t.Fatalf("List: %v", err)
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

func TestInMemoryChatStore_List_EmptyBotName_ReturnsAll(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "a"})
	_ = store.Append(ctx, domain.ChatMessage{BotName: "qa-1", Direction: domain.ChatDirectionOutbound, Content: "b"})

	msgs, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages with empty botName, got %d", len(msgs))
	}
}

func TestInMemoryChatStore_List_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	msgs, err := store.List(ctx, "dev-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if msgs == nil {
		t.Error("expected non-nil slice from List on empty store")
	}
}

func TestInMemoryChatStore_ListAll_ReturnsNewestFirst(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	base := time.Now().UTC()
	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "first", CreatedAt: base.Add(-2 * time.Second)})
	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionInbound, Content: "second", CreatedAt: base.Add(-1 * time.Second)})
	_ = store.Append(ctx, domain.ChatMessage{BotName: "dev-1", Direction: domain.ChatDirectionOutbound, Content: "third", CreatedAt: base})

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
	store := orchestrator.NewInMemoryChatStore()
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
	store := orchestrator.NewInMemoryChatStore()
	ctx := context.Background()

	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msg := domain.ChatMessage{
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
