package orchestrator_test

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

func TestInMemoryControlPlane_Register(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	entry := domain.BotEntry{
		Name:    "dev-1",
		BotType: "developer",
	}
	if err := cp.Register(ctx, entry); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := cp.Get(ctx, "dev-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.BotStatusActive {
		t.Errorf("expected Status=active, got %q", got.Status)
	}
	if got.RegisteredAt.IsZero() {
		t.Error("expected RegisteredAt to be set after Register")
	}
}

func TestInMemoryControlPlane_Register_SetsStatus(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	entry := domain.BotEntry{Name: "bot", BotType: "worker"}
	if err := cp.Register(ctx, entry); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, _ := cp.Get(ctx, "bot")
	if got.Status != domain.BotStatusActive {
		t.Errorf("expected BotStatusActive, got %q", got.Status)
	}
}

func TestInMemoryControlPlane_Deregister(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	entry := domain.BotEntry{Name: "dev-2", BotType: "developer"}
	_ = cp.Register(ctx, entry)

	if err := cp.Deregister(ctx, "dev-2"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	got, err := cp.Get(ctx, "dev-2")
	if err != nil {
		t.Fatalf("Get after Deregister: %v", err)
	}
	if got.Status != domain.BotStatusInactive {
		t.Errorf("expected Status=inactive after Deregister, got %q", got.Status)
	}
}

func TestInMemoryControlPlane_Deregister_DoesNotDelete(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "bot", BotType: "worker"})
	_ = cp.Deregister(ctx, "bot")

	// Bot should still be retrievable (not deleted)
	_, err := cp.Get(ctx, "bot")
	if err != nil {
		t.Errorf("expected bot to still exist after Deregister, got error: %v", err)
	}
}

func TestInMemoryControlPlane_UpdateHeartbeat(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "bot", BotType: "worker"})

	if err := cp.UpdateHeartbeat(ctx, "bot"); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	after, _ := cp.Get(ctx, "bot")
	if after.LastHeartbeat.IsZero() {
		t.Error("expected LastHeartbeat to be set after UpdateHeartbeat")
	}
}

func TestInMemoryControlPlane_UpdateHeartbeat_NotFound(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	err := cp.UpdateHeartbeat(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent bot, got nil")
	}
}

func TestInMemoryControlPlane_Get_NotFound(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_, err := cp.Get(ctx, "ghost")
	if err == nil {
		t.Error("expected error for nonexistent bot, got nil")
	}
}

func TestInMemoryControlPlane_List(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "a", BotType: "worker"})
	_ = cp.Register(ctx, domain.BotEntry{Name: "b", BotType: "developer"})

	bots, err := cp.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(bots) != 2 {
		t.Errorf("expected 2 bots, got %d", len(bots))
	}
}

func TestInMemoryControlPlane_List_Empty(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	bots, err := cp.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if bots == nil {
		t.Error("expected non-nil slice from empty List")
	}
}

func TestInMemoryControlPlane_IsTypeActive_True(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "dev-1", BotType: "developer"})

	active, err := cp.IsTypeActive(ctx, "developer")
	if err != nil {
		t.Fatalf("IsTypeActive: %v", err)
	}
	if !active {
		t.Error("expected IsTypeActive=true for registered active developer")
	}
}

func TestInMemoryControlPlane_IsTypeActive_False_AfterDeregister(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "dev-1", BotType: "developer"})
	_ = cp.Deregister(ctx, "dev-1")

	active, err := cp.IsTypeActive(ctx, "developer")
	if err != nil {
		t.Fatalf("IsTypeActive: %v", err)
	}
	if active {
		t.Error("expected IsTypeActive=false after Deregister")
	}
}

func TestInMemoryControlPlane_IsTypeActive_False_NotFound(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	active, err := cp.IsTypeActive(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("IsTypeActive: %v", err)
	}
	if active {
		t.Error("expected IsTypeActive=false for nonexistent type")
	}
}

func TestInMemoryControlPlane_IsTypeActive_True_WhenOneActive(t *testing.T) {
	t.Parallel()
	cp := orchestrator.NewInMemoryControlPlane()
	ctx := context.Background()

	_ = cp.Register(ctx, domain.BotEntry{Name: "dev-1", BotType: "developer"})
	_ = cp.Register(ctx, domain.BotEntry{Name: "dev-2", BotType: "developer"})
	_ = cp.Deregister(ctx, "dev-1")

	active, err := cp.IsTypeActive(ctx, "developer")
	if err != nil {
		t.Fatalf("IsTypeActive: %v", err)
	}
	if !active {
		t.Error("expected IsTypeActive=true when at least one active developer exists")
	}
}
