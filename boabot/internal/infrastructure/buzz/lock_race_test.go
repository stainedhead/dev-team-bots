package buzz

import (
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestAcquireLock_ConcurrentRace_OnlyOneWinner is FR-004's (WS-C1) red
// test: it races two goroutines' AcquireLock calls against the same path,
// deliberately stalling the first goroutine (via acquireLockAfterCreateHook)
// between the moment it creates the not-yet-populated carrier for the lock
// file's content and the moment that content is actually written. The
// second goroutine is released to run AcquireLock while the first is
// stalled in exactly that window.
//
// Before FR-004's fix, the first goroutine's O_CREATE|O_EXCL directly
// creates the lock file itself under its final name, empty, before writing
// the PID -- so the stalled window is directly observable to the second
// goroutine under the lock's real path. The second goroutine hits EEXIST,
// reads the (still-empty) file, and -- per readLockPID's deliberate
// empty-file-is-pid-0 stale heuristic -- concludes the lock is abandoned,
// reclaims it, and succeeds. The first goroutine, unaware, then finishes
// its own write against its now-detached descriptor and also returns a
// non-error *Lock. Both believe they hold the FR-031/OQ-1 singleton lock
// simultaneously: at most one may ever do so.
func TestAcquireLock_ConcurrentRace_OnlyOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buzz-race.lock")

	started := make(chan struct{})
	proceed := make(chan struct{})
	var stalled atomic.Bool

	prevHook := acquireLockAfterCreateHook
	acquireLockAfterCreateHook = func() {
		// Only the goroutine that reaches this hook FIRST is the
		// "deliberately slowed" one the finding describes; any later
		// call (e.g. a retry, or the second goroutine's own create)
		// must not also block, or the test would deadlock. A plain
		// CAS -- rather than sync.Once -- is essential here: Once.Do
		// blocks a *concurrent* second caller on its internal mutex
		// until the first call's function returns, so if the first
		// caller is itself stalled inside that function (as it is
		// here, waiting on <-proceed), a second caller reaching
		// Once.Do would deadlock rather than passing through.
		if stalled.CompareAndSwap(false, true) {
			close(started)
			<-proceed
		}
	}
	t.Cleanup(func() { acquireLockAfterCreateHook = prevHook })

	type result struct {
		lock *Lock
		err  error
	}

	aCh := make(chan result, 1)
	go func() {
		l, err := AcquireLock(path)
		aCh <- result{l, err}
	}()

	<-started // goroutine A is now stalled inside its create-then-write window

	bLock, bErr := AcquireLock(path)

	close(proceed) // release goroutine A to finish its write

	aRes := <-aCh

	successes := 0
	if aRes.err == nil {
		successes++
		t.Cleanup(func() { _ = aRes.lock.Release() })
	}
	if bErr == nil {
		successes++
		t.Cleanup(func() { _ = bLock.Release() })
	}

	if successes > 1 {
		t.Fatalf("both racing AcquireLock calls against %q succeeded (A err=%v, B err=%v) -- TOCTOU: two callers now believe they hold the singleton lock simultaneously", path, aRes.err, bErr)
	}
	if successes == 0 {
		t.Fatalf("both racing AcquireLock calls against %q failed (A err=%v, B err=%v) -- expected exactly one to succeed", path, aRes.err, bErr)
	}
}
