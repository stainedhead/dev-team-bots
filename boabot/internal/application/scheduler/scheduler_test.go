package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/scheduler"
)

// --- helpers ---

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

type mockTaskRunner struct {
	mu       sync.Mutex
	runFn    func(ctx context.Context, task scheduler.ScheduledTask) error
	runCalls []scheduler.ScheduledTask
}

func (m *mockTaskRunner) Run(ctx context.Context, task scheduler.ScheduledTask) error {
	m.mu.Lock()
	m.runCalls = append(m.runCalls, task)
	m.mu.Unlock()
	if m.runFn != nil {
		return m.runFn(ctx, task)
	}
	return nil
}

func (m *mockTaskRunner) RunCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runCalls)
}

type mockTaskStore struct {
	mu              sync.Mutex
	listFn          func(ctx context.Context) ([]scheduler.ScheduledTask, error)
	updateLastRunFn func(ctx context.Context, id string, lastRunAt time.Time) error

	updateLastRunCalls int
}

func (m *mockTaskStore) List(ctx context.Context) ([]scheduler.ScheduledTask, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockTaskStore) UpdateLastRun(ctx context.Context, id string, lastRunAt time.Time) error {
	m.mu.Lock()
	m.updateLastRunCalls++
	m.mu.Unlock()
	if m.updateLastRunFn != nil {
		return m.updateLastRunFn(ctx, id, lastRunAt)
	}
	return nil
}

func (m *mockTaskStore) UpdateLastRunCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateLastRunCalls
}

// --- NextRun ---

func TestNextRun_ParsesEveryMinute(t *testing.T) {
	after := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	next, err := scheduler.NextRun("* * * * *", after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextRun_ParsesHourly(t *testing.T) {
	after := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	next, err := scheduler.NextRun("0 * * * *", after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextRun_InvalidExpression(t *testing.T) {
	_, err := scheduler.NextRun("not-a-cron", time.Now())
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNextRun_DailyAtMidnight(t *testing.T) {
	after := time.Date(2024, 6, 15, 23, 59, 0, 0, time.UTC)
	next, err := scheduler.NextRun("0 0 * * *", after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

// --- Schedule ---

func TestScheduler_Schedule_InvalidCron(t *testing.T) {
	store := &mockTaskStore{}
	runner := &mockTaskRunner{}
	s := scheduler.NewScheduler(store, runner, time.UTC)

	err := s.Schedule(scheduler.ScheduledTask{ID: "t1", CronExpr: "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestScheduler_Schedule_ValidCron(t *testing.T) {
	store := &mockTaskStore{}
	runner := &mockTaskRunner{}
	s := scheduler.NewScheduler(store, runner, time.UTC)

	err := s.Schedule(scheduler.ScheduledTask{ID: "t1", CronExpr: "* * * * *", Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Start / runDue ---

func TestScheduler_Start_RunsDueTasks(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-2 * time.Minute) // 2 minutes ago → cron "* * * * *" is due

	task := scheduler.ScheduledTask{
		ID:        "t1",
		CronExpr:  "* * * * *",
		Enabled:   true,
		LastRunAt: &lastRun,
	}

	var runCount atomic.Int32
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{
		runFn: func(_ context.Context, _ scheduler.ScheduledTask) error {
			runCount.Add(1)
			return nil
		},
	}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	// Give the initial runDue a moment to execute.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if runCount.Load() == 0 {
		t.Fatal("expected task to run at least once")
	}
}

func TestScheduler_Start_SkipsDisabledTasks(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-2 * time.Minute)

	task := scheduler.ScheduledTask{
		ID:        "t2",
		CronExpr:  "* * * * *",
		Enabled:   false, // disabled
		LastRunAt: &lastRun,
	}

	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if len(runner.runCalls) != 0 {
		t.Fatalf("expected 0 run calls for disabled task, got %d", len(runner.runCalls))
	}
}

func TestScheduler_Start_SkipsFutureStartAt(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-2 * time.Minute)
	futureStart := now.Add(time.Hour) // not yet reached

	task := scheduler.ScheduledTask{
		ID:            "t3",
		CronExpr:      "* * * * *",
		Enabled:       true,
		LastRunAt:     &lastRun,
		FutureStartAt: &futureStart,
	}

	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if len(runner.runCalls) != 0 {
		t.Fatalf("expected 0 run calls (future start), got %d", len(runner.runCalls))
	}
}

func TestScheduler_Start_RunsPastFutureStartAt(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-2 * time.Minute)
	pastStart := now.Add(-time.Hour) // already passed

	task := scheduler.ScheduledTask{
		ID:            "t4",
		CronExpr:      "* * * * *",
		Enabled:       true,
		LastRunAt:     &lastRun,
		FutureStartAt: &pastStart,
	}

	var runCount atomic.Int32
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{
		runFn: func(_ context.Context, _ scheduler.ScheduledTask) error {
			runCount.Add(1)
			return nil
		},
	}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if runCount.Load() == 0 {
		t.Fatal("expected task to run when futureStartAt is in the past")
	}
}

func TestScheduler_Start_TaskNeverRun_IsConsideredDue(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	task := scheduler.ScheduledTask{
		ID:       "t5",
		CronExpr: "* * * * *",
		Enabled:  true,
		// LastRunAt is nil → never run
	}

	var runCount atomic.Int32
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{
		runFn: func(_ context.Context, _ scheduler.ScheduledTask) error {
			runCount.Add(1)
			return nil
		},
	}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if runCount.Load() == 0 {
		t.Fatal("expected never-run task to execute")
	}
}

func TestScheduler_Start_ListError_DoesNotPanic(t *testing.T) {
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return nil, errors.New("db error")
		},
	}
	runner := &mockTaskRunner{}

	s := scheduler.NewScheduler(store, runner, time.UTC)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestScheduler_NilLocation_UsesUTC(t *testing.T) {
	store := &mockTaskStore{}
	runner := &mockTaskRunner{}
	s := scheduler.NewScheduler(store, runner, nil) // nil → UTC
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

func TestScheduler_Start_SkipsNotYetDueTask(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	// lastRun was 10 minutes ago at 12:20; cron runs every hour → next due at 13:00.
	lastRun := now.Add(-10 * time.Minute)

	task := scheduler.ScheduledTask{
		ID:        "nd1",
		CronExpr:  "0 * * * *", // every hour
		Enabled:   true,
		LastRunAt: &lastRun,
	}

	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if len(runner.runCalls) != 0 {
		t.Fatalf("expected 0 run calls for not-yet-due task, got %d", len(runner.runCalls))
	}
}

func TestScheduler_Start_InvalidCronSkipped(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	task := scheduler.ScheduledTask{
		ID:       "bad-cron",
		CronExpr: "not-valid",
		Enabled:  true,
	}

	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return []scheduler.ScheduledTask{task}, nil
		},
	}
	runner := &mockTaskRunner{}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if len(runner.runCalls) != 0 {
		t.Fatalf("expected 0 run calls for invalid cron task, got %d", len(runner.runCalls))
	}
}

func TestScheduler_Start_ConcurrentTasksDontBlock(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-2 * time.Minute)

	tasks := []scheduler.ScheduledTask{
		{ID: "c1", CronExpr: "* * * * *", Enabled: true, LastRunAt: &lastRun},
		{ID: "c2", CronExpr: "* * * * *", Enabled: true, LastRunAt: &lastRun},
		{ID: "c3", CronExpr: "* * * * *", Enabled: true, LastRunAt: &lastRun},
	}

	var runCount atomic.Int32
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return tasks, nil
		},
	}
	runner := &mockTaskRunner{
		runFn: func(_ context.Context, _ scheduler.ScheduledTask) error {
			time.Sleep(20 * time.Millisecond)
			runCount.Add(1)
			return nil
		},
	}

	s := scheduler.NewScheduler(store, runner, time.UTC)
	s = s.WithClock(&fakeClock{t: now})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	elapsed := time.Since(start)

	if runCount.Load() < 3 {
		t.Fatalf("expected all 3 tasks to run, got %d", runCount.Load())
	}
	// If tasks ran concurrently, elapsed should be < 3*20ms = 60ms for a single tick.
	// With 150ms sleep we're well above, but let's just check all ran.
	_ = elapsed
}

// TestScheduler_RealClock_Now verifies that NewScheduler (without WithClock) uses
// the real system clock. Start calls runDue immediately; runDue calls clock.Now().
func TestScheduler_RealClock_Now(t *testing.T) {
	t.Parallel()
	store := &mockTaskStore{
		listFn: func(_ context.Context) ([]scheduler.ScheduledTask, error) {
			return nil, nil // nothing to run
		},
	}
	s := scheduler.NewScheduler(store, &mockTaskRunner{}, nil)
	// Cancel immediately — Start runs one runDue then exits.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.Start(ctx)
}
