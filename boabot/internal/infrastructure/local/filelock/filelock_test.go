package filelock_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/filelock"
)

func TestAcquireWait_AcquiresAndReleases(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	lock, err := filelock.AcquireWait(path, time.Second)
	if err != nil {
		t.Fatalf("AcquireWait: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected lock file to exist after acquire: %v", statErr)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected lock file to be removed after Release, stat err=%v", statErr)
	}
}

func TestAcquireWait_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	lock, err := filelock.AcquireWait(path, time.Second)
	if err != nil {
		t.Fatalf("AcquireWait: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release should be a no-op, got: %v", err)
	}
}

func TestAcquireWait_NilLockReleaseIsSafe(t *testing.T) {
	t.Parallel()
	var lock *filelock.Lock
	if err := lock.Release(); err != nil {
		t.Fatalf("Release on nil *Lock should be a no-op, got: %v", err)
	}
}

func TestAcquireWait_SecondCallerWaitsUntilFirstReleases(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := filelock.AcquireWait(path, time.Second)
	if err != nil {
		t.Fatalf("first AcquireWait: %v", err)
	}

	type result struct {
		lock *filelock.Lock
		err  error
		at   time.Time
	}
	resCh := make(chan result, 1)
	go func() {
		l, aerr := filelock.AcquireWait(path, 2*time.Second)
		resCh <- result{l, aerr, time.Now()}
	}()

	// Give the second caller a real chance to observe contention and enter
	// its retry/backoff loop before we release.
	time.Sleep(30 * time.Millisecond)
	releasedAt := time.Now()
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("second AcquireWait: %v", res.err)
		}
		if res.at.Before(releasedAt) {
			t.Fatalf("second AcquireWait succeeded at %v, before first released at %v -- lock did not serialize", res.at, releasedAt)
		}
		t.Cleanup(func() { _ = res.lock.Release() })
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second AcquireWait to succeed after release")
	}
}

func TestAcquireWait_TimesOutWhenLockHeld(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	held, err := filelock.AcquireWait(path, time.Second)
	if err != nil {
		t.Fatalf("AcquireWait: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	_, err = filelock.AcquireWait(path, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected AcquireWait to time out while the lock is held, got nil error")
	}
}

func TestAcquireWait_ReclaimsStaleLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	// A PID that is exceedingly unlikely to be alive, simulating a lock
	// file left behind by a process that crashed without cleanup.
	const deadPID = 999999
	if err := os.WriteFile(path, []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatalf("seeding stale lock file: %v", err)
	}

	lock, err := filelock.AcquireWait(path, time.Second)
	if err != nil {
		t.Fatalf("AcquireWait should reclaim a stale lock, got: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading reclaimed lock file: %v", err)
	}
	if string(data) == strconv.Itoa(deadPID) {
		t.Fatal("expected lock file content to be overwritten with the new holder's PID")
	}
}
