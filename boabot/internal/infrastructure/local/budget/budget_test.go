package budget_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/budget"
)

// newTracker is a helper that creates a BudgetTracker with the given config.
func newTracker(t *testing.T, cfg config.BudgetConfig) (*budget.BudgetTracker, string) {
	t.Helper()
	dir := t.TempDir()
	bt, err := budget.New(cfg, dir)
	if err != nil {
		t.Fatalf("budget.New: %v", err)
	}
	return bt, dir
}

// TestBudgetTracker_TokensWithinLimit verifies tokens are recorded when under limit.
func TestBudgetTracker_TokensWithinLimit(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}
	bt, _ := newTracker(t, cfg)

	if err := bt.CheckAndRecordTokens(context.Background(), 500); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}
	// Second call should also succeed (500+400 = 900 < 1000).
	if err := bt.CheckAndRecordTokens(context.Background(), 400); err != nil {
		t.Fatalf("second CheckAndRecordTokens: %v", err)
	}
}

// TestBudgetTracker_TokensExceedsLimit verifies error when tokens would exceed the cap.
func TestBudgetTracker_TokensExceedsLimit(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 100, ToolCallsHourly: 10}
	bt, _ := newTracker(t, cfg)

	// Record 90 tokens.
	if err := bt.CheckAndRecordTokens(context.Background(), 90); err != nil {
		t.Fatalf("first CheckAndRecordTokens: %v", err)
	}
	// Trying to record 20 more would exceed the 100 cap.
	err := bt.CheckAndRecordTokens(context.Background(), 20)
	if err == nil {
		t.Fatal("expected error when exceeding token limit, got nil")
	}
}

// TestBudgetTracker_TokensNotRecordedOnExceed verifies that a failed check leaves counter unchanged.
func TestBudgetTracker_TokensNotRecordedOnExceed(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 100, ToolCallsHourly: 10}
	bt, _ := newTracker(t, cfg)

	// Use 80 tokens.
	if err := bt.CheckAndRecordTokens(context.Background(), 80); err != nil {
		t.Fatalf("first CheckAndRecordTokens: %v", err)
	}
	// Attempt to use 30 more (would exceed 100) — should fail.
	if err := bt.CheckAndRecordTokens(context.Background(), 30); err == nil {
		t.Fatal("expected error, got nil")
	}
	// After the failed attempt, using 15 should succeed (80+15=95 ≤ 100).
	if err := bt.CheckAndRecordTokens(context.Background(), 15); err != nil {
		t.Fatalf("CheckAndRecordTokens after failed attempt: %v", err)
	}
}

// TestBudgetTracker_ZeroLimit verifies that a zero limit means unlimited.
func TestBudgetTracker_ZeroLimit(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 0, ToolCallsHourly: 0}
	bt, _ := newTracker(t, cfg)

	ctx := context.Background()
	for range 100 {
		if err := bt.CheckAndRecordTokens(ctx, 1000); err != nil {
			t.Fatalf("CheckAndRecordTokens with 0 limit: %v", err)
		}
		if err := bt.CheckAndRecordToolCall(ctx); err != nil {
			t.Fatalf("CheckAndRecordToolCall with 0 limit: %v", err)
		}
	}
}

// TestBudgetTracker_ToolCallsWithinLimit verifies tool calls recorded within limit.
func TestBudgetTracker_ToolCallsWithinLimit(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 0, ToolCallsHourly: 5}
	bt, _ := newTracker(t, cfg)

	ctx := context.Background()
	for range 5 {
		if err := bt.CheckAndRecordToolCall(ctx); err != nil {
			t.Fatalf("CheckAndRecordToolCall: %v", err)
		}
	}
}

// TestBudgetTracker_ToolCallsExceedsLimit verifies error on tool call cap breach.
func TestBudgetTracker_ToolCallsExceedsLimit(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 0, ToolCallsHourly: 3}
	bt, _ := newTracker(t, cfg)

	ctx := context.Background()
	for range 3 {
		if err := bt.CheckAndRecordToolCall(ctx); err != nil {
			t.Fatalf("CheckAndRecordToolCall: %v", err)
		}
	}
	err := bt.CheckAndRecordToolCall(ctx)
	if err == nil {
		t.Fatal("expected error when exceeding tool call limit, got nil")
	}
}

// TestBudgetTracker_Flush verifies that Flush writes budget.json.
func TestBudgetTracker_Flush(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}
	bt, dir := newTracker(t, cfg)

	ctx := context.Background()
	if err := bt.CheckAndRecordTokens(ctx, 42); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}
	if err := bt.CheckAndRecordToolCall(ctx); err != nil {
		t.Fatalf("CheckAndRecordToolCall: %v", err)
	}

	if err := bt.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "budget.json"))
	if err != nil {
		t.Fatalf("read budget.json: %v", err)
	}

	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal budget.json: %v", err)
	}

	// token_spend should be 42.
	tokenSpend, ok := state["token_spend"].(float64)
	if !ok || tokenSpend != 42 {
		t.Errorf("expected token_spend=42, got %v", state["token_spend"])
	}
	// tool_call_count should be 1.
	toolCallCount, ok := state["tool_call_count"].(float64)
	if !ok || toolCallCount != 1 {
		t.Errorf("expected tool_call_count=1, got %v", state["tool_call_count"])
	}
}

// TestBudgetTracker_LoadFromFlushFile verifies that New loads state from an
// existing budget.json, making counters survive restarts.
func TestBudgetTracker_LoadFromFlushFile(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}

	// First instance: record some usage.
	bt1, dir := newTracker(t, cfg)
	ctx := context.Background()
	if err := bt1.CheckAndRecordTokens(ctx, 200); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}
	if err := bt1.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Second instance: should load 200 from budget.json.
	bt2, err := budget.New(cfg, dir)
	if err != nil {
		t.Fatalf("budget.New (second): %v", err)
	}

	// Only 800 remaining — trying 900 should fail.
	if err := bt2.CheckAndRecordTokens(ctx, 900); err == nil {
		t.Fatal("expected error after restart with loaded state, got nil")
	}
	// But 800 exactly should succeed.
	if err := bt2.CheckAndRecordTokens(ctx, 800); err != nil {
		t.Fatalf("CheckAndRecordTokens 800 after reload: %v", err)
	}
}

// TestBudgetTracker_DailyReset verifies that the token counter resets when the
// date changes.
func TestBudgetTracker_DailyReset(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 100, ToolCallsHourly: 10}
	bt, _ := newTracker(t, cfg)

	ctx := context.Background()
	// Use the full daily budget.
	if err := bt.CheckAndRecordTokens(ctx, 100); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}
	// Next call should fail (over budget).
	if err := bt.CheckAndRecordTokens(ctx, 1); err == nil {
		t.Fatal("expected budget exceeded error before day change")
	}

	// Simulate a day change by advancing the tracker's clock.
	bt.SetNow(func() time.Time {
		return time.Now().AddDate(0, 0, 1)
	})

	// Counter should have reset; this call should succeed.
	if err := bt.CheckAndRecordTokens(ctx, 50); err != nil {
		t.Fatalf("CheckAndRecordTokens after day change: %v", err)
	}
}

// TestBudgetTracker_HourlyReset verifies that the tool call counter resets when the
// hour changes.
func TestBudgetTracker_HourlyReset(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 0, ToolCallsHourly: 3}
	bt, _ := newTracker(t, cfg)

	ctx := context.Background()
	// Use the full hourly budget.
	for range 3 {
		if err := bt.CheckAndRecordToolCall(ctx); err != nil {
			t.Fatalf("CheckAndRecordToolCall: %v", err)
		}
	}
	// Next call should fail.
	if err := bt.CheckAndRecordToolCall(ctx); err == nil {
		t.Fatal("expected error before hour change")
	}

	// Advance clock by one hour.
	bt.SetNow(func() time.Time {
		return time.Now().Add(time.Hour)
	})

	// Should succeed after hourly reset.
	if err := bt.CheckAndRecordToolCall(ctx); err != nil {
		t.Fatalf("CheckAndRecordToolCall after hour change: %v", err)
	}
}

// TestBudgetTracker_RunFlushesAndStops verifies the background flush goroutine.
func TestBudgetTracker_RunFlushesAndStops(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}
	bt, dir := newTracker(t, cfg)

	ctx := context.Background()
	if err := bt.CheckAndRecordTokens(ctx, 77); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}

	cancelCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	// Run returns when ctx is cancelled.
	done := make(chan error, 1)
	go func() {
		done <- bt.Run(cancelCtx)
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}

	// budget.json should have been written during the Run.
	data, err := os.ReadFile(filepath.Join(dir, "budget.json"))
	if err != nil {
		t.Fatalf("budget.json not written by Run: %v", err)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal budget.json: %v", err)
	}
	tokenSpend, _ := state["token_spend"].(float64)
	if tokenSpend != 77 {
		t.Errorf("expected token_spend=77 in flushed file, got %v", tokenSpend)
	}
}

// TestBudgetTracker_NewBadPath verifies constructor fails if flush path is invalid.
func TestBudgetTracker_NewBadPath(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{}
	_, err := budget.New(cfg, "/dev/null/impossible/path")
	if err == nil {
		t.Fatal("expected error for bad flush path, got nil")
	}
}

// TestBudgetTracker_FlushReadOnlyDir verifies that Flush returns an error
// when the flush directory is read-only (CreateTemp fails).
func TestBudgetTracker_FlushReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}
	bt, dir := newTracker(t, cfg)

	// Flush once successfully first to get a budget.json.
	if err := bt.Flush(context.Background()); err != nil {
		t.Fatalf("initial Flush: %v", err)
	}

	// Make the flush directory read-only so CreateTemp fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	err := bt.Flush(context.Background())
	if err == nil {
		t.Fatal("expected error flushing to read-only directory, got nil")
	}
}

// TestBudgetTracker_RunWithTickerFlush verifies that Run flushes on the ticker
// interval, not just on initial start and shutdown.
func TestBudgetTracker_RunWithTickerFlush(t *testing.T) {
	t.Parallel()
	cfg := config.BudgetConfig{TokenSpendDaily: 1000, ToolCallsHourly: 10}
	bt, dir := newTracker(t, cfg)

	// Use a very short flush interval so the ticker fires quickly.
	bt.SetFlushInterval(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Record some tokens after starting Run so they get flushed by ticker.
	done := make(chan error, 1)
	go func() { done <- bt.Run(ctx) }()

	// Record tokens and wait for Run to complete.
	if err := bt.CheckAndRecordTokens(context.Background(), 55); err != nil {
		t.Fatalf("CheckAndRecordTokens: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	// Read the budget.json — token_spend should be 55.
	data, err := os.ReadFile(filepath.Join(dir, "budget.json"))
	if err != nil {
		t.Fatalf("read budget.json: %v", err)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tokenSpend, _ := state["token_spend"].(float64)
	if tokenSpend != 55 {
		t.Errorf("expected token_spend=55, got %v", tokenSpend)
	}
}

// TestBudgetTracker_LoadCorruptedJSON verifies that New returns an error when
// budget.json exists but contains invalid JSON.
func TestBudgetTracker_LoadCorruptedJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write invalid JSON to budget.json.
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte("not-json"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := config.BudgetConfig{}
	_, err := budget.New(cfg, dir)
	if err == nil {
		t.Fatal("expected error loading corrupted budget.json, got nil")
	}
}
