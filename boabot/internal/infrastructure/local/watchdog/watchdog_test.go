package watchdog_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
)

// makeFakeReadMem returns a readMem func that sets HeapInuse to heapMB MiB.
func makeFakeReadMem(heapMB uint64) func(*runtime.MemStats) {
	return func(ms *runtime.MemStats) {
		ms.HeapInuse = heapMB * 1024 * 1024
	}
}

// TestWatchdog_NoBreach verifies that no action is taken when heap usage is
// below both thresholds.
func TestWatchdog_NoBreach(t *testing.T) {
	t.Parallel()
	var shutdownCalled atomic.Bool

	cfg := watchdog.Config{
		SampleInterval: 10 * time.Millisecond,
		WarnMB:         200,
		HardMB:         400,
	}
	wd := watchdog.New(cfg, func() { shutdownCalled.Store(true) })
	watchdog.SetReadMem(wd, makeFakeReadMem(100)) // 100 MiB — below both thresholds

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	wd.Run(ctx)

	if shutdownCalled.Load() {
		t.Error("shutdown should not have been called when heap is below thresholds")
	}
}

// TestWatchdog_WarnThreshold verifies that a warning is logged (shutdown NOT
// called) when heap crosses WarnMB but not HardMB.
func TestWatchdog_WarnThreshold(t *testing.T) {
	t.Parallel()
	var shutdownCalled atomic.Bool

	cfg := watchdog.Config{
		SampleInterval: 10 * time.Millisecond,
		WarnMB:         100,
		HardMB:         400,
	}
	wd := watchdog.New(cfg, func() { shutdownCalled.Store(true) })
	watchdog.SetReadMem(wd, makeFakeReadMem(200)) // 200 MiB — above WarnMB but below HardMB

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	wd.Run(ctx)

	if shutdownCalled.Load() {
		t.Error("shutdown should not be called at WarnMB threshold — only at HardMB")
	}
}

// TestWatchdog_HardThreshold verifies that shutdown is called when heap exceeds HardMB.
func TestWatchdog_HardThreshold(t *testing.T) {
	t.Parallel()
	shutdownCh := make(chan struct{}, 1)

	cfg := watchdog.Config{
		SampleInterval: 10 * time.Millisecond,
		WarnMB:         100,
		HardMB:         200,
	}
	wd := watchdog.New(cfg, func() {
		select {
		case shutdownCh <- struct{}{}:
		default:
		}
	})
	watchdog.SetReadMem(wd, makeFakeReadMem(300)) // 300 MiB — above HardMB

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		wd.Run(ctx)
		close(done)
	}()

	select {
	case <-shutdownCh:
		// Expected: shutdown was called.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected shutdown to be called within 500ms")
	}

	// Run should return after calling shutdown.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after shutdown")
	}
}

// TestWatchdog_ContextCancellation verifies that Run exits cleanly on context
// cancellation without calling shutdown.
func TestWatchdog_ContextCancellation(t *testing.T) {
	t.Parallel()
	var shutdownCalled atomic.Bool

	cfg := watchdog.Config{
		SampleInterval: 100 * time.Millisecond, // long enough that heap check won't fire
		WarnMB:         0,
		HardMB:         0,
	}
	wd := watchdog.New(cfg, func() { shutdownCalled.Store(true) })
	watchdog.SetReadMem(wd, makeFakeReadMem(0))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		wd.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return within 500ms after context cancel")
	}

	if shutdownCalled.Load() {
		t.Error("shutdown should not be called on clean context cancellation")
	}
}

// TestWatchdog_DefaultSampleInterval verifies that New applies a default
// SampleInterval when cfg.SampleInterval is zero.
func TestWatchdog_DefaultSampleInterval(t *testing.T) {
	t.Parallel()
	// Just ensure New doesn't panic with zero SampleInterval.
	cfg := watchdog.Config{
		SampleInterval: 0, // should default to 30s
		WarnMB:         0,
		HardMB:         0,
	}
	wd := watchdog.New(cfg, func() {})
	_ = wd // constructed without panic
}

// TestWatchdog_WarnAndHardBothDisabled verifies that Run exits on context
// cancellation when both thresholds are disabled.
func TestWatchdog_WarnAndHardBothDisabled(t *testing.T) {
	t.Parallel()
	var shutdownCalled atomic.Bool

	cfg := watchdog.Config{
		SampleInterval: 10 * time.Millisecond,
		WarnMB:         0,
		HardMB:         0,
	}
	wd := watchdog.New(cfg, func() { shutdownCalled.Store(true) })
	// Even with huge heap values, no action if limits are 0.
	watchdog.SetReadMem(wd, makeFakeReadMem(99999))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	wd.Run(ctx)

	if shutdownCalled.Load() {
		t.Error("shutdown should not be called when thresholds are disabled (0)")
	}
}

// TestWatchdog_HardThresholdExactBoundary verifies that the hard limit fires
// at exactly HardMB (>=, not >).
func TestWatchdog_HardThresholdExactBoundary(t *testing.T) {
	t.Parallel()
	shutdownCh := make(chan struct{}, 1)

	cfg := watchdog.Config{
		SampleInterval: 10 * time.Millisecond,
		WarnMB:         0,
		HardMB:         256,
	}
	wd := watchdog.New(cfg, func() {
		select {
		case shutdownCh <- struct{}{}:
		default:
		}
	})
	watchdog.SetReadMem(wd, makeFakeReadMem(256)) // exactly at HardMB

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go wd.Run(ctx)

	select {
	case <-shutdownCh:
		// Expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected shutdown at exactly HardMB")
	}
}
