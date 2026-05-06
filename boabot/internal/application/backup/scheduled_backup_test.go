package backup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── mockBackup ────────────────────────────────────────────────────────────────

// mockBackup implements domain.MemoryBackup for testing.
type mockBackup struct {
	mu          sync.Mutex
	backupCalls int
	backupErr   error
}

func (m *mockBackup) Backup(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backupCalls++
	return m.backupErr
}

func (m *mockBackup) Restore(_ context.Context) error {
	return nil
}

func (m *mockBackup) Status(_ context.Context) (domain.BackupStatus, error) {
	return domain.BackupStatus{}, nil
}

func (m *mockBackup) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backupCalls
}

// ── fakeCron ──────────────────────────────────────────────────────────────────

// fakeCron satisfies cronRunner and immediately invokes the registered function
// before Start() returns, giving tests synchronous control.
type fakeCron struct {
	fn     func()
	addErr error
	stopCh chan struct{}
}

func newFakeCron() *fakeCron {
	ch := make(chan struct{})
	close(ch) // Stop() returns an already-done channel.
	return &fakeCron{stopCh: ch}
}

func (f *fakeCron) AddFunc(_ string, cmd func()) (cron.EntryID, error) {
	if f.addErr != nil {
		return 0, f.addErr
	}
	f.fn = cmd
	return 1, nil
}

func (f *fakeCron) Start() {
	if f.fn != nil {
		f.fn() // run immediately in the calling goroutine
	}
}

func (f *fakeCron) Stop() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// ── helpers ───────────────────────────────────────────────────────────────────

// buildUC creates a ScheduledBackupUseCase with an injected cron factory.
func buildUC(mb domain.MemoryBackup, schedule string, fc *fakeCron) *ScheduledBackupUseCase {
	uc := New(mb, schedule)
	uc.newCron = func() cronRunner { return fc }
	return uc
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_DefaultSchedule(t *testing.T) {
	uc := New(&mockBackup{}, "")
	if uc.schedule != defaultSchedule {
		t.Errorf("expected default schedule %q, got %q", defaultSchedule, uc.schedule)
	}
}

func TestNew_CustomSchedule(t *testing.T) {
	uc := New(&mockBackup{}, "0 * * * *")
	if uc.schedule != "0 * * * *" {
		t.Errorf("expected custom schedule, got %q", uc.schedule)
	}
}

// ── Run ───────────────────────────────────────────────────────────────────────

func TestRun_InvalidCronExpression_ReturnsError(t *testing.T) {
	fc := newFakeCron()
	fc.addErr = errors.New("invalid expression")
	uc := buildUC(&mockBackup{}, "bad expression", fc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := uc.Run(ctx); err == nil {
		t.Fatal("expected error for invalid cron expression (simulated by fakeCron)")
	}
}

func TestRun_BackupCalledOnTick(t *testing.T) {
	mb := &mockBackup{}
	fc := newFakeCron()
	uc := buildUC(mb, defaultSchedule, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so Run exits after the tick

	err := uc.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if mb.calls() != 1 {
		t.Errorf("expected 1 backup call, got %d", mb.calls())
	}
}

func TestRun_BackupError_IsNonFatal(t *testing.T) {
	mb := &mockBackup{backupErr: errors.New("transient error")}
	fc := newFakeCron()
	uc := buildUC(mb, defaultSchedule, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := uc.Run(ctx)
	// Run must return the context error, not the backup error.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if mb.calls() != 1 {
		t.Errorf("expected 1 backup call (error is non-fatal), got %d", mb.calls())
	}
}

func TestRun_CancelledContext_ReturnsContextError(t *testing.T) {
	mb := &mockBackup{}
	// Use a real cron with a schedule that never fires so we just test cancellation.
	uc := New(mb, "59 23 31 12 0")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- uc.Run(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRun_InvalidSchedule_WithRealCron_ReturnsError(t *testing.T) {
	mb := &mockBackup{}
	uc := New(mb, "not a valid cron expression!!!!")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := uc.Run(ctx); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

// ── interface compliance ──────────────────────────────────────────────────────

var _ domain.MemoryBackup = (*mockBackup)(nil)
var _ cronRunner = (*fakeCron)(nil)
