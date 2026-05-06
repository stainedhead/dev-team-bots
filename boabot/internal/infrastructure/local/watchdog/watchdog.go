// Package watchdog provides a heap memory watchdog that samples runtime heap
// usage and triggers warnings or a graceful shutdown when configurable thresholds
// are exceeded.
package watchdog

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

const defaultSampleInterval = 30 * time.Second

// Config holds thresholds for the heap watchdog.
type Config struct {
	// SampleInterval is how often to sample heap usage. Defaults to 30s.
	SampleInterval time.Duration
	// WarnMB is the heap usage in MiB at which a warning is logged. 0 = disabled.
	WarnMB int
	// HardMB is the heap usage in MiB at which shutdown is called. 0 = disabled.
	HardMB int
}

// Watchdog samples runtime.MemStats.HeapInuse and calls shutdown when limits
// are exceeded.
type Watchdog struct {
	cfg      Config
	shutdown func()
	readMem  func(*runtime.MemStats)
}

// New creates a Watchdog with the given config and shutdown callback.
// shutdown is called when the HardMB threshold is exceeded.
func New(cfg Config, shutdown func()) *Watchdog {
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = defaultSampleInterval
	}
	return &Watchdog{
		cfg:      cfg,
		shutdown: shutdown,
		readMem:  runtime.ReadMemStats,
	}
}

// Run samples heap usage on cfg.SampleInterval. On WarnMB: logs a warning.
// On HardMB: logs an error, calls shutdown(), and returns.
// Run blocks until ctx is cancelled or HardMB is exceeded.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.SampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var ms runtime.MemStats
			w.readMem(&ms)
			heapMB := int(ms.HeapInuse / (1024 * 1024))

			if w.cfg.HardMB > 0 && heapMB >= w.cfg.HardMB {
				slog.Error("heap hard limit exceeded; shutting down",
					"heap_mb", heapMB,
					"hard_mb", w.cfg.HardMB,
				)
				w.shutdown()
				return
			}

			if w.cfg.WarnMB > 0 && heapMB >= w.cfg.WarnMB {
				slog.Warn("heap usage approaching limit",
					"heap_mb", heapMB,
					"warn_mb", w.cfg.WarnMB,
				)
			}
		}
	}
}
