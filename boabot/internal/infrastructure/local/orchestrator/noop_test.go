package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// ── NoopSkillRegistry ─────────────────────────────────────────────────────────

func TestNoopSkillRegistry_List_ReturnsEmptyNonNilSlice(t *testing.T) {
	t.Parallel()
	r := orchestrator.NoopSkillRegistry{}
	skills, err := r.List(context.Background(), "any", "staged")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if skills == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(skills) != 0 {
		t.Errorf("expected empty slice, got len=%d", len(skills))
	}
}

func TestNoopSkillRegistry_Get_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	r := orchestrator.NoopSkillRegistry{}
	_, err := r.Get(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Get, got nil")
	}
	if !errors.Is(err, orchestrator.ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got: %v", err)
	}
}

func TestNoopSkillRegistry_Approve_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	r := orchestrator.NoopSkillRegistry{}
	err := r.Approve(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Approve, got nil")
	}
	if !errors.Is(err, orchestrator.ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got: %v", err)
	}
}

func TestNoopSkillRegistry_Reject_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	r := orchestrator.NoopSkillRegistry{}
	err := r.Reject(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Reject, got nil")
	}
	if !errors.Is(err, orchestrator.ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got: %v", err)
	}
}

func TestNoopSkillRegistry_Revoke_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	r := orchestrator.NoopSkillRegistry{}
	err := r.Revoke(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Revoke, got nil")
	}
	if !errors.Is(err, orchestrator.ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got: %v", err)
	}
}

// ── NoopDLQStore ──────────────────────────────────────────────────────────────

func TestNoopDLQStore_List_ReturnsEmptyNonNilSlice(t *testing.T) {
	t.Parallel()
	d := orchestrator.NoopDLQStore{}
	items, err := d.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if items == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(items) != 0 {
		t.Errorf("expected empty slice, got len=%d", len(items))
	}
}

func TestNoopDLQStore_Retry_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	d := orchestrator.NoopDLQStore{}
	err := d.Retry(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Retry, got nil")
	}
	if !errors.Is(err, orchestrator.ErrDLQItemNotFound) {
		t.Errorf("expected ErrDLQItemNotFound, got: %v", err)
	}
}

func TestNoopDLQStore_Discard_ReturnsSentinelError(t *testing.T) {
	t.Parallel()
	d := orchestrator.NoopDLQStore{}
	err := d.Discard(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected sentinel error from Discard, got nil")
	}
	if !errors.Is(err, orchestrator.ErrDLQItemNotFound) {
		t.Errorf("expected ErrDLQItemNotFound, got: %v", err)
	}
}
