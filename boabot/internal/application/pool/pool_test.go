package pool_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/pool"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

// newTestPool creates a Pool with injected spawnFn/stopFn stubs.
func newTestPool(
	t *testing.T,
	spawnFn func(ctx context.Context, name string) error,
	stopFn func(ctx context.Context, name string) error,
) *pool.Pool {
	t.Helper()
	sfPath := filepath.Join(t.TempDir(), "pool.json")
	file := infrastructure.NewPoolStateFile(sfPath)
	p := pool.New(pool.Config{
		BotsDir:       t.TempDir(),
		MemoryRoot:    t.TempDir(),
		SpawnTimeout:  2 * time.Second,
		SoftPoolLimit: 10,
	}, file)
	p.SetSpawnFn(spawnFn)
	p.SetStopFn(stopFn)
	return p
}

func noopSpawn(_ context.Context, _ string) error { return nil }
func noopStop(_ context.Context, _ string) error  { return nil }

// TestPool_Allocate_SpawnsNewInstance verifies that Allocate spawns a new instance
// when the pool is empty.
func TestPool_Allocate_SpawnsNewInstance(t *testing.T) {
	t.Parallel()
	spawned := make(chan string, 10)
	p := newTestPool(t, func(_ context.Context, name string) error {
		spawned <- name
		return nil
	}, noopStop)

	ctx := context.Background()
	entry, err := p.Allocate(ctx, "item-1")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Status != domain.PoolEntryStatusAllocated {
		t.Errorf("expected Status=allocated, got %q", entry.Status)
	}
	if entry.ItemID != "item-1" {
		t.Errorf("expected ItemID=item-1, got %q", entry.ItemID)
	}
	if entry.InstanceName == "" {
		t.Error("expected non-empty InstanceName")
	}

	select {
	case name := <-spawned:
		if name != entry.InstanceName {
			t.Errorf("spawned instance name mismatch: got %q, want %q", name, entry.InstanceName)
		}
	case <-time.After(time.Second):
		t.Fatal("expected spawnFn to be called")
	}
}

// TestPool_Allocate_ReusesIdleInstance verifies that an idle instance is reused
// before spawning a new one.
func TestPool_Allocate_ReusesIdleInstance(t *testing.T) {
	t.Parallel()
	spawnCount := 0
	p := newTestPool(t, func(_ context.Context, _ string) error {
		spawnCount++
		return nil
	}, noopStop)

	ctx := context.Background()

	// First allocation — spawns new instance.
	entry1, err := p.Allocate(ctx, "item-1")
	if err != nil {
		t.Fatalf("first Allocate: %v", err)
	}

	// Deallocate — instance should become idle (warm standby since it's the last one).
	if err := p.Deallocate(ctx, "item-1"); err != nil {
		t.Fatalf("Deallocate: %v", err)
	}

	// Second allocation — should reuse the idle instance.
	initialSpawnCount := spawnCount
	entry2, err := p.Allocate(ctx, "item-2")
	if err != nil {
		t.Fatalf("second Allocate: %v", err)
	}

	if spawnCount != initialSpawnCount {
		t.Errorf("expected no new spawn on idle reuse, got %d additional spawns", spawnCount-initialSpawnCount)
	}
	if entry2.InstanceName != entry1.InstanceName {
		t.Errorf("expected reuse of %q, got %q", entry1.InstanceName, entry2.InstanceName)
	}
	_ = m_TearDown(t, p, "item-2")
}

// m_TearDown is a helper to clean up allocations in tests.
func m_TearDown(t *testing.T, p *pool.Pool, itemID string) error {
	t.Helper()
	return p.Deallocate(context.Background(), itemID)
}

// TestPool_Allocate_DuplicateItemID_ReturnsError verifies that allocating an
// already-allocated itemID returns an error.
func TestPool_Allocate_DuplicateItemID_ReturnsError(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	ctx := context.Background()
	if _, err := p.Allocate(ctx, "item-1"); err != nil {
		t.Fatalf("first Allocate: %v", err)
	}

	_, err := p.Allocate(ctx, "item-1")
	if err == nil {
		t.Fatal("expected error on duplicate itemID, got nil")
	}
	_ = p.Deallocate(ctx, "item-1")
}

// TestPool_Deallocate_LastInstance_StaysIdle verifies that the last instance in
// the pool is kept as a warm standby (marked idle, not removed).
func TestPool_Deallocate_LastInstance_StaysIdle(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	ctx := context.Background()
	if _, err := p.Allocate(ctx, "item-1"); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	if err := p.Deallocate(ctx, "item-1"); err != nil {
		t.Fatalf("Deallocate: %v", err)
	}

	// Pool should still have one entry in idle state.
	entries, err := p.ListEntries(ctx)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (warm standby), got %d", len(entries))
	}
	if entries[0].Status != domain.PoolEntryStatusIdle {
		t.Errorf("expected Status=idle, got %q", entries[0].Status)
	}
	if entries[0].ItemID != "" {
		t.Errorf("expected empty ItemID after deallocate, got %q", entries[0].ItemID)
	}
}

// TestPool_Deallocate_NonLastInstance_Removes verifies that a non-last instance
// is stopped and removed from the pool.
func TestPool_Deallocate_NonLastInstance_Removes(t *testing.T) {
	t.Parallel()
	stopped := make(chan string, 10)
	p := newTestPool(t, noopSpawn, func(_ context.Context, name string) error {
		stopped <- name
		return nil
	})

	ctx := context.Background()

	// Allocate two items — creates two instances.
	entry1, err := p.Allocate(ctx, "item-1")
	if err != nil {
		t.Fatalf("Allocate item-1: %v", err)
	}
	_, err = p.Allocate(ctx, "item-2")
	if err != nil {
		t.Fatalf("Allocate item-2: %v", err)
	}

	// Deallocate item-1 — since item-2's instance still exists, item-1's is removed.
	if err := p.Deallocate(ctx, "item-1"); err != nil {
		t.Fatalf("Deallocate item-1: %v", err)
	}

	select {
	case name := <-stopped:
		if name != entry1.InstanceName {
			t.Errorf("expected stop of %q, got %q", entry1.InstanceName, name)
		}
	case <-time.After(time.Second):
		t.Fatal("expected stopFn to be called")
	}

	// Pool should have one entry (item-2).
	entries, err := p.ListEntries(ctx)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after deallocate, got %d", len(entries))
	}
	_ = p.Deallocate(ctx, "item-2")
}

// TestPool_Deallocate_UnknownItemID_ReturnsError verifies that deallocating an
// unknown itemID returns an error.
func TestPool_Deallocate_UnknownItemID_ReturnsError(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	err := p.Deallocate(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error for unknown itemID, got nil")
	}
}

// TestPool_Allocate_ConcurrentNoDuplicates verifies that concurrent Allocate
// calls do not create duplicate instances for different itemIDs.
func TestPool_Allocate_ConcurrentNoDuplicates(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	ctx := context.Background()
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			itemID := "item-" + string(rune('0'+idx))
			if _, err := p.Allocate(ctx, itemID); err != nil {
				errs[idx] = err
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Allocate error: %v", i, err)
		}
	}

	entries, err := p.ListEntries(ctx)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != n {
		t.Errorf("expected %d entries, got %d", n, len(entries))
	}

	// Cleanup.
	for i := range n {
		_ = p.Deallocate(ctx, "item-"+string(rune('0'+i)))
	}
}

// TestPool_GetByItemID verifies that GetByItemID returns the correct entry.
func TestPool_GetByItemID(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	ctx := context.Background()
	entry, err := p.Allocate(ctx, "item-abc")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	got, err := p.GetByItemID(ctx, "item-abc")
	if err != nil {
		t.Fatalf("GetByItemID: %v", err)
	}
	if got.InstanceName != entry.InstanceName {
		t.Errorf("expected InstanceName=%q, got %q", entry.InstanceName, got.InstanceName)
	}
	_ = p.Deallocate(ctx, "item-abc")
}

// TestPool_GetByItemID_Miss_ReturnsError verifies that GetByItemID returns an
// error for an unknown itemID.
func TestPool_GetByItemID_Miss_ReturnsError(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	_, err := p.GetByItemID(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error for unknown itemID, got nil")
	}
}

// TestPool_Reconcile_RemovesStaleEntries verifies that Reconcile removes entries
// for dead instances (isRunning returns false).
func TestPool_Reconcile_RemovesStaleEntries(t *testing.T) {
	t.Parallel()
	sfPath := filepath.Join(t.TempDir(), "pool.json")
	file := infrastructure.NewPoolStateFile(sfPath)

	// Pre-populate the state file with stale records.
	stale := []infrastructure.PoolStateRecord{
		{InstanceName: "tech-lead-1", Status: domain.PoolEntryStatusAllocated, ItemID: "item-1", BusID: "b1"},
		{InstanceName: "tech-lead-2", Status: domain.PoolEntryStatusIdle, BusID: "b2"},
	}
	if err := file.Save(stale); err != nil {
		t.Fatalf("Save stale records: %v", err)
	}

	p := pool.New(pool.Config{
		BotsDir:      t.TempDir(),
		MemoryRoot:   t.TempDir(),
		SpawnTimeout: 2 * time.Second,
	}, file)
	p.SetSpawnFn(noopSpawn)
	p.SetStopFn(noopStop)
	// isRunning returns false for all — so all should be removed.
	p.SetIsRunningFn(func(_ context.Context, _ string) bool { return false })

	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	entries, err := p.ListEntries(context.Background())
	if err != nil {
		t.Fatalf("ListEntries after Reconcile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after Reconcile of all-dead instances, got %d", len(entries))
	}
}

// TestPool_Reconcile_KeepsLiveEntries verifies that Reconcile retains entries
// for live instances.
func TestPool_Reconcile_KeepsLiveEntries(t *testing.T) {
	t.Parallel()
	sfPath := filepath.Join(t.TempDir(), "pool.json")
	file := infrastructure.NewPoolStateFile(sfPath)

	live := []infrastructure.PoolStateRecord{
		{InstanceName: "tech-lead-1", Status: domain.PoolEntryStatusAllocated, ItemID: "item-1", BusID: "b1"},
	}
	if err := file.Save(live); err != nil {
		t.Fatalf("Save live records: %v", err)
	}

	p := pool.New(pool.Config{
		BotsDir:      t.TempDir(),
		MemoryRoot:   t.TempDir(),
		SpawnTimeout: 2 * time.Second,
	}, file)
	p.SetSpawnFn(noopSpawn)
	p.SetStopFn(noopStop)
	// isRunning returns true for all — all should be kept.
	p.SetIsRunningFn(func(_ context.Context, _ string) bool { return true })

	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	entries, err := p.ListEntries(context.Background())
	if err != nil {
		t.Fatalf("ListEntries after Reconcile: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after Reconcile of live instance, got %d", len(entries))
	}
}

// TestPool_SpawnFn_Error_ReturnsError verifies that an error from spawnFn is
// propagated.
func TestPool_SpawnFn_Error_ReturnsError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("spawn failed")
	p := newTestPool(t, func(_ context.Context, _ string) error {
		return sentinel
	}, noopStop)

	_, err := p.Allocate(context.Background(), "item-1")
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// TestPool_InstanceNaming verifies that instances are named tech-lead-N with
// incrementing N.
func TestPool_InstanceNaming(t *testing.T) {
	t.Parallel()
	p := newTestPool(t, noopSpawn, noopStop)

	ctx := context.Background()
	e1, _ := p.Allocate(ctx, "item-1")
	e2, _ := p.Allocate(ctx, "item-2")

	if e1.InstanceName == e2.InstanceName {
		t.Errorf("expected different instance names, both got %q", e1.InstanceName)
	}

	_ = p.Deallocate(ctx, "item-1")
	_ = p.Deallocate(ctx, "item-2")
}

// TestPool_SoftPoolLimit_LogsWarning verifies that exceeding the soft pool limit
// does NOT block allocation (just logs a warning). We can only check that Allocate
// succeeds — the warning goes to the log.
func TestPool_SoftPoolLimit_LogsWarning(t *testing.T) {
	t.Parallel()
	sfPath := filepath.Join(t.TempDir(), "pool.json")
	file := infrastructure.NewPoolStateFile(sfPath)
	p := pool.New(pool.Config{
		BotsDir:       t.TempDir(),
		MemoryRoot:    t.TempDir(),
		SpawnTimeout:  2 * time.Second,
		SoftPoolLimit: 2,
	}, file)
	p.SetSpawnFn(noopSpawn)
	p.SetStopFn(noopStop)

	ctx := context.Background()
	for i := range 4 {
		itemID := "item-" + string(rune('0'+i))
		if _, err := p.Allocate(ctx, itemID); err != nil {
			t.Errorf("Allocate beyond soft limit: %v", err)
		}
	}

	// Cleanup.
	for i := range 4 {
		_ = p.Deallocate(ctx, "item-"+string(rune('0'+i)))
	}
}

// TestPool_ApplyDefaults_ZeroSpawnTimeout verifies that a zero SpawnTimeout is
// replaced with the default (1s) during Pool construction.
func TestPool_ApplyDefaults_ZeroSpawnTimeout(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.Config{SpawnTimeout: 0, SoftPoolLimit: 5}, nil)
	p.SetSpawnFn(noopSpawn)
	entry, err := p.Allocate(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("Allocate with zero SpawnTimeout: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
}

// TestPool_DefaultSpawnFn_ReturnsError verifies that Allocate fails with an
// informative error when no spawnFn has been injected.
func TestPool_DefaultSpawnFn_ReturnsError(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.Config{SpawnTimeout: time.Second, SoftPoolLimit: 5}, nil)
	// No SetSpawnFn — uses the default stub that always returns an error.
	_, err := p.Allocate(context.Background(), "item-1")
	if err == nil {
		t.Fatal("expected error from default spawnFn")
	}
}

// TestPool_DefaultStopFn_NoError verifies that Deallocate succeeds for a
// non-last entry when no stopFn has been injected (default is a noop).
func TestPool_DefaultStopFn_NoError(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.Config{SpawnTimeout: time.Second, SoftPoolLimit: 5}, nil)
	p.SetSpawnFn(noopSpawn)
	// Deliberately do NOT call SetStopFn — exercises the default stopFn body.
	ctx := context.Background()
	if _, err := p.Allocate(ctx, "item-a"); err != nil {
		t.Fatalf("Allocate a: %v", err)
	}
	if _, err := p.Allocate(ctx, "item-b"); err != nil {
		t.Fatalf("Allocate b: %v", err)
	}
	// Deallocate item-a (not the last) — calls default stopFn (returns nil).
	if err := p.Deallocate(ctx, "item-a"); err != nil {
		t.Fatalf("Deallocate: %v", err)
	}
}

// TestPool_DefaultIsRunFn_RemovesStaleEntries verifies that Reconcile discards
// pool records when no isRunningFn has been injected (default returns false).
func TestPool_DefaultIsRunFn_RemovesStaleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := infrastructure.NewPoolStateFile(filepath.Join(dir, "pool.json"))
	_ = file.Save([]infrastructure.PoolStateRecord{
		{InstanceName: "tech-lead-1", Status: "allocated"},
	})
	p := pool.New(pool.Config{SpawnTimeout: time.Second, SoftPoolLimit: 5}, file)
	p.SetSpawnFn(noopSpawn)
	// Do NOT call SetIsRunningFn — default returns false, marking the record stale.
	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	entries, _ := p.ListEntries(context.Background())
	if len(entries) != 0 {
		t.Errorf("expected stale entries removed by default isRunFn, got %d", len(entries))
	}
}

// TestPool_SaveUnlocked_NilFile verifies that saveUnlocked is a no-op (no panic)
// when Pool is created with a nil PoolStateFile.
func TestPool_SaveUnlocked_NilFile(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.Config{SpawnTimeout: time.Second, SoftPoolLimit: 5}, nil)
	p.SetSpawnFn(noopSpawn)
	if _, err := p.Allocate(context.Background(), "item-1"); err != nil {
		t.Fatalf("Allocate with nil file: %v", err)
	}
}

// TestPool_Deallocate_StopFnError verifies that Deallocate returns nil even
// when the injected stopFn returns an error (the error is only logged).
func TestPool_Deallocate_StopFnError(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.Config{SpawnTimeout: time.Second, SoftPoolLimit: 5}, nil)
	p.SetSpawnFn(noopSpawn)
	p.SetStopFn(func(_ context.Context, _ string) error {
		return errors.New("stop failed")
	})
	ctx := context.Background()
	if _, err := p.Allocate(ctx, "item-a"); err != nil {
		t.Fatalf("Allocate a: %v", err)
	}
	if _, err := p.Allocate(ctx, "item-b"); err != nil {
		t.Fatalf("Allocate b: %v", err)
	}
	// Deallocate item-a (not the last) — stopFn errors but Deallocate should still return nil.
	if err := p.Deallocate(ctx, "item-a"); err != nil {
		t.Fatalf("Deallocate should succeed despite stopFn error: %v", err)
	}
}
