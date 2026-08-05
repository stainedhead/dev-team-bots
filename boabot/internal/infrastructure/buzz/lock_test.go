package buzz

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestAcquireLock_SecondAttempt_FailsWhileFirstHoldsLock is the G1 red test:
// two lock-acquisition attempts against the SAME path, in the SAME test
// process, demonstrate FR-031/OQ-1's exact required behaviour -- the second
// attempt fails with a clear, typed error (ErrAlreadyRunning) while the
// first continues to hold the lock, undisturbed.
func TestAcquireLock_SecondAttempt_FailsWhileFirstHoldsLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buzz-abc123.lock")

	first, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	defer func() { _ = first.Release() }()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file should exist after first acquire: %v", err)
	}

	second, err := AcquireLock(path)
	if err == nil {
		_ = second.Release()
		t.Fatal("second AcquireLock unexpectedly succeeded while first still holds the lock")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second AcquireLock error = %v, want wrapping ErrAlreadyRunning", err)
	}

	// The first lock is unaffected: the file it created is still present
	// and still names our own (live) PID.
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read lock file after contended second attempt: %v", rerr)
	}
	gotPID, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if perr != nil {
		t.Fatalf("lock file content not a pid: %q", string(data))
	}
	if gotPID != os.Getpid() {
		t.Fatalf("lock file pid = %d, want our own pid %d", gotPID, os.Getpid())
	}
}

// TestAcquireLock_ReleaseThenReacquire confirms Release genuinely frees the
// lock for a subsequent acquirer -- e.g. a clean restart of the same
// process/identity must not be permanently blocked by its own prior run.
func TestAcquireLock_ReleaseThenReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buzz-def456.lock")

	first, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock after Release should succeed, got: %v", err)
	}
	defer func() { _ = second.Release() }()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file should exist after reacquire: %v", err)
	}
}

// TestAcquireLock_StaleLock_Reclaimed proves the "left behind by a process
// that crashed without cleanup" case never permanently wedges future
// starts: a lock file naming a PID that is provably no longer running is
// reclaimed rather than treated as a live holder.
func TestAcquireLock_StaleLock_Reclaimed(t *testing.T) {
	deadPID := deadPIDForTest(t)

	path := filepath.Join(t.TempDir(), "buzz-stale.lock")
	if err := os.WriteFile(path, []byte(strconv.Itoa(deadPID)), 0o600); err != nil {
		t.Fatalf("seed stale lock file: %v", err)
	}

	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock over a stale lock should reclaim it, got: %v", err)
	}
	defer func() { _ = lock.Release() }()

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read reclaimed lock file: %v", rerr)
	}
	if got, _ := strconv.Atoi(strings.TrimSpace(string(data))); got != os.Getpid() {
		t.Fatalf("reclaimed lock file pid = %s, want our own pid %d", data, os.Getpid())
	}
}

// TestAcquireLock_GarbageLockFile_TreatedAsHeld exercises the defensive
// branch for a non-empty, unparseable lock file (garbage content we
// cannot attribute to any specific PID): treated conservatively as held,
// not silently reclaimed, since we cannot prove the holder is dead.
func TestAcquireLock_GarbageLockFile_TreatedAsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buzz-corrupt.lock")
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatalf("seed corrupt lock file: %v", err)
	}

	_, err := AcquireLock(path)
	if err == nil {
		t.Fatal("AcquireLock over a garbage lock file should fail closed, not succeed")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v, want wrapping ErrAlreadyRunning (fail closed)", err)
	}
}

// TestAcquireLock_EmptyLockFile_Reclaimed is the specific stale-lock shape
// a real crash produces: a process SIGKILLed between AcquireLock's
// O_CREATE|O_EXCL and its PID WriteString leaves a zero-byte file behind.
// That must never permanently wedge future starts -- unlike genuinely
// unparseable garbage (which we cannot attribute to a dead vs. live
// holder), an empty file unambiguously has no live holder recorded in it.
func TestAcquireLock_EmptyLockFile_Reclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buzz-empty.lock")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("seed empty lock file: %v", err)
	}

	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock over an empty (crash-mid-write) lock file should reclaim it, got: %v", err)
	}
	defer func() { _ = lock.Release() }()

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read reclaimed lock file: %v", rerr)
	}
	if got, _ := strconv.Atoi(strings.TrimSpace(string(data))); got != os.Getpid() {
		t.Fatalf("reclaimed lock file pid = %s, want our own pid %d", data, os.Getpid())
	}
}

// TestLockFileName_TruncatesLongPubkey confirms lockFileName produces a
// short, tidy filename even for a full 64-char hex pubkey, and that
// LockPath joins it under the given directory.
func TestLockFileName_TruncatesLongPubkey(t *testing.T) {
	long := strings.Repeat("a", 64)
	name := lockFileName(long)
	want := "buzz-aaaaaaaaaaaaaaaa.lock" // 16-char prefix
	if name != want {
		t.Fatalf("lockFileName(%d-char pubkey) = %q, want %q", len(long), name, want)
	}

	dir := t.TempDir()
	got := LockPath(dir, long)
	want2 := filepath.Join(dir, want)
	if got != want2 {
		t.Fatalf("LockPath = %q, want %q", got, want2)
	}
}

// TestLock_Release_NilReceiver confirms Release is safe to call on a nil
// *Lock (defensive: a caller that never successfully acquired one, e.g.
// after a refused Start, must never be able to panic by calling Release).
func TestLock_Release_NilReceiver(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Fatalf("Release on nil *Lock: %v", err)
	}
}

// TestProcessAlive_InvalidPID confirms the pid<=0 guard rejects
// nonsensical input outright rather than passing it to os.FindProcess.
func TestProcessAlive_InvalidPID(t *testing.T) {
	if processAlive(0) {
		t.Fatal("pid 0 must never be considered alive")
	}
	if processAlive(-1) {
		t.Fatal("a negative pid must never be considered alive")
	}
}

// TestProcessAlive_OwnPID confirms the running test process itself is
// always reported alive -- the same check AcquireLock relies on to detect
// contention against a genuinely live holder.
func TestProcessAlive_OwnPID(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("the current process must be reported alive")
	}
}

// deadPIDForTest returns a PID that is guaranteed not to be running: it
// spawns a trivial child process and waits for it to exit, then returns
// its (now-vacated) PID. This is the standard, portable way to obtain a
// PID that provably does not correspond to a live process, for exercising
// the stale-lock-reclaim path without depending on any specific unused PID
// happening to be free on the test machine.
func deadPIDForTest(t *testing.T) int {
	t.Helper()

	var cmd *exec.Cmd
	if os.PathSeparator == '\\' {
		cmd = exec.Command("cmd", "/c", "exit 0")
	} else {
		cmd = exec.Command("true")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for throwaway process: %v", err)
	}
	return pid
}
