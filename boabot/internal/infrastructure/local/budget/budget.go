// Package budget provides a local in-process implementation of domain.BudgetTracker.
// It is intended for single-binary operation without any cloud infrastructure.
//
// Counters are held in memory using sync/atomic for thread-safety. They are
// flushed to <flushPath>/budget.json periodically by Run and on explicit Flush
// calls. On startup, existing state is loaded from the file so budget caps
// survive process restarts within the same calendar period.
//
// Daily token counters reset at midnight UTC. Hourly tool call counters reset
// at the top of each UTC hour.
package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

const (
	defaultFlushInterval = 10 * time.Second
	budgetFile           = "budget.json"
)

// flushState is the JSON representation persisted to disk.
type flushState struct {
	TokenSpend     int64     `json:"token_spend"`
	ToolCallCount  int64     `json:"tool_call_count"`
	TokenResetDate string    `json:"token_reset_date"`
	ToolResetHour  string    `json:"tool_reset_hour"`
	LastUpdated    time.Time `json:"last_updated"`
}

// BudgetTracker implements domain.BudgetTracker with local atomic counters.
type BudgetTracker struct {
	cfg           config.BudgetConfig
	flushPath     string
	flushInterval time.Duration

	tokenSpend    atomic.Int64
	toolCallCount atomic.Int64

	mu             sync.Mutex
	tokenResetDate string // "2006-01-02"
	toolResetHour  string // "2006-01-02T15"

	nowFn func() time.Time // injectable for tests
}

// New constructs a BudgetTracker. The flushPath directory is created if it does
// not exist. If budget.json exists it is loaded to seed the counters.
// Returns an error if the directory cannot be created.
func New(cfg config.BudgetConfig, flushPath string) (*BudgetTracker, error) {
	if err := os.MkdirAll(flushPath, 0700); err != nil {
		return nil, fmt.Errorf("local/budget: create flush path %q: %w", flushPath, err)
	}

	bt := &BudgetTracker{
		cfg:           cfg,
		flushPath:     flushPath,
		flushInterval: defaultFlushInterval,
		nowFn:         time.Now,
	}
	now := bt.now()
	bt.tokenResetDate = now.UTC().Format("2006-01-02")
	bt.toolResetHour = now.UTC().Format("2006-01-02T15")

	if err := bt.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("local/budget: load state: %w", err)
	}
	return bt, nil
}

// SetNow overrides the internal clock — for testing only.
func (bt *BudgetTracker) SetNow(fn func() time.Time) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.nowFn = fn
}

func (bt *BudgetTracker) now() time.Time {
	bt.mu.Lock()
	fn := bt.nowFn
	bt.mu.Unlock()
	return fn()
}

// CheckAndRecordTokens checks whether adding count tokens would exceed the
// daily cap. If within limit, the tokens are recorded. Returns an error if
// the cap would be exceeded (without recording).
func (bt *BudgetTracker) CheckAndRecordTokens(_ context.Context, count int64) error {
	bt.maybeResetTokens()

	if bt.cfg.TokenSpendDaily <= 0 {
		bt.tokenSpend.Add(count)
		return nil
	}

	for {
		current := bt.tokenSpend.Load()
		if current+count > bt.cfg.TokenSpendDaily {
			return fmt.Errorf("local/budget: daily token spend cap exceeded (%d+%d > %d)",
				current, count, bt.cfg.TokenSpendDaily)
		}
		if bt.tokenSpend.CompareAndSwap(current, current+count) {
			return nil
		}
	}
}

// CheckAndRecordToolCall checks whether recording one tool call would exceed
// the hourly cap. If within limit, the call is recorded. Returns an error if
// the cap would be exceeded (without recording).
func (bt *BudgetTracker) CheckAndRecordToolCall(_ context.Context) error {
	bt.maybeResetToolCalls()

	if bt.cfg.ToolCallsHourly <= 0 {
		bt.toolCallCount.Add(1)
		return nil
	}

	for {
		current := bt.toolCallCount.Load()
		if current+1 > int64(bt.cfg.ToolCallsHourly) {
			return fmt.Errorf("local/budget: hourly tool call cap exceeded (%d+1 > %d)",
				current, bt.cfg.ToolCallsHourly)
		}
		if bt.toolCallCount.CompareAndSwap(current, current+1) {
			return nil
		}
	}
}

// Flush writes the current in-memory state to <flushPath>/budget.json.
// The write is atomic (temp file + rename). Returns an error if the write fails.
func (bt *BudgetTracker) Flush(_ context.Context) error {
	bt.mu.Lock()
	state := flushState{
		TokenSpend:     bt.tokenSpend.Load(),
		ToolCallCount:  bt.toolCallCount.Load(),
		TokenResetDate: bt.tokenResetDate,
		ToolResetHour:  bt.toolResetHour,
		LastUpdated:    bt.nowFn().UTC(),
	}
	bt.mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("local/budget: marshal state: %w", err)
	}

	dest := filepath.Join(bt.flushPath, budgetFile)
	tmp, err := os.CreateTemp(bt.flushPath, ".tmp-budget-*")
	if err != nil {
		return fmt.Errorf("local/budget: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close() // close before WriteFile re-opens

	if err := os.WriteFile(tmpName, data, 0600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("local/budget: write temp file: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("local/budget: rename temp to budget.json: %w", err)
	}
	return nil
}

// SetFlushInterval overrides the flush interval used by Run — for testing only.
func (bt *BudgetTracker) SetFlushInterval(d time.Duration) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.flushInterval = d
}

// Run starts a background goroutine that flushes state every flushInterval
// until ctx is cancelled. Returns nil when ctx is done.
// TeamManager should call this in a goroutine.
func (bt *BudgetTracker) Run(ctx context.Context) error {
	// Flush once immediately on start.
	if err := bt.Flush(ctx); err != nil {
		log.Printf("local/budget: initial flush error: %v", err)
	}

	ticker := time.NewTicker(bt.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := bt.Flush(ctx); err != nil {
				log.Printf("local/budget: flush error: %v", err)
			}
		case <-ctx.Done():
			// Final flush on shutdown.
			if err := bt.Flush(context.Background()); err != nil {
				log.Printf("local/budget: final flush error: %v", err)
			}
			return nil
		}
	}
}

// --- internal helpers --------------------------------------------------------

// load reads budget.json from flushPath and seeds the counters.
// Returns os.ErrNotExist if the file does not exist (caller ignores this).
func (bt *BudgetTracker) load() error {
	data, err := os.ReadFile(filepath.Join(bt.flushPath, budgetFile))
	if err != nil {
		return err
	}
	var state flushState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	now := bt.nowFn().UTC()
	today := now.Format("2006-01-02")
	thisHour := now.Format("2006-01-02T15")

	bt.mu.Lock()
	defer bt.mu.Unlock()

	// Restore token counter only if we're in the same day.
	if state.TokenResetDate == today {
		bt.tokenSpend.Store(state.TokenSpend)
		bt.tokenResetDate = state.TokenResetDate
	}
	// Restore tool call counter only if we're in the same hour.
	if state.ToolResetHour == thisHour {
		bt.toolCallCount.Store(state.ToolCallCount)
		bt.toolResetHour = state.ToolResetHour
	}
	return nil
}

// maybeResetTokens checks if the date has changed and resets the token counter.
func (bt *BudgetTracker) maybeResetTokens() {
	today := bt.now().UTC().Format("2006-01-02")
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.tokenResetDate != today {
		bt.tokenSpend.Store(0)
		bt.tokenResetDate = today
	}
}

// maybeResetToolCalls checks if the hour has changed and resets the tool call counter.
func (bt *BudgetTracker) maybeResetToolCalls() {
	thisHour := bt.now().UTC().Format("2006-01-02T15")
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.toolResetHour != thisHour {
		bt.toolCallCount.Store(0)
		bt.toolResetHour = thisHour
	}
}
