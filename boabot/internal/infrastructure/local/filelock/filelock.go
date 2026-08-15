// Package filelock provides a small, stdlib-only, cross-platform
// (macOS/Linux/Windows) file-based mutual-exclusion primitive for
// coordinating multiple OS processes that share a single on-disk resource.
//
// Its atomic-publish and stale-lock-reclaim mechanics are adapted from
// internal/infrastructure/buzz/lock.go's FR-031/OQ-1 singleton-lock
// primitive (same-directory temp file + os.Link publish, PID-liveness
// staleness check -- see that file's doc comments for the full portability
// verification trail, including the Windows os.Link behavior this package
// also relies on). This package is a deliberate, independent extraction,
// not a refactor of buzz/lock.go: that package's AcquireLock is fail-fast
// (one process per Buzz identity is a bug, so a second acquirer must error
// immediately), whereas AcquireWait here is wait-your-turn (board.go, this
// package's motivating caller, expects many legitimate concurrent writers
// that should each get a turn, not error out on contention). Keeping the
// two independent avoids an awkward cross-adapter dependency between the
// buzz and orchestrator infrastructure packages for a small amount of
// shared logic.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lock is a held lock file obtained via AcquireWait. The zero value is not
// usable. Release removes the lock file, freeing it for the next acquirer.
type Lock struct {
	path string
}

// Release removes the lock file. Idempotent: releasing an already-released
// (or otherwise missing) lock is not an error.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("filelock: release lock file %q: %w", l.path, err)
	}
	return nil
}

// afterCreateHook, when non-nil, is invoked by publishLockFile immediately
// after it creates the not-yet-populated same-directory temp file that
// carries the lock file's content, but before that content is written --
// a test seam only, mirroring buzz/lock.go's acquireLockAfterCreateHook.
// Nil (the production default) means no delay; must never be set outside
// a test.
var afterCreateHook func()

// publishLockFile attempts to atomically publish path as a lock file
// containing the current process's PID as its entire content. It reports
// (true, nil) if this call won the race and created path; (false, nil) if
// path already existed, so the caller should fall through to a stale-lock
// read/reclaim path; or a non-nil error for any other failure.
//
// Atomicity: the PID is written to a same-directory temporary file first
// (fsync'd and closed), then published under path's final name via
// os.Link -- link(2) creates the new directory entry and the file's
// content atomically together, so there is no "created but empty" state
// visible under path, and it fails with an fs.ErrExist-compatible error if
// path already exists. See buzz/lock.go's publishLockFile doc comment for
// the full os.Link-on-Windows verification trail this package relies on
// identically.
func publishLockFile(path string) (bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("filelock: create lock directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".filelock-*.tmp")
	if err != nil {
		return false, fmt.Errorf("filelock: create temp lock file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if afterCreateHook != nil {
		afterCreateHook()
	}

	_, werr := tmp.WriteString(strconv.Itoa(os.Getpid()))
	if werr == nil {
		werr = tmp.Sync()
	}
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return false, fmt.Errorf("filelock: write temp lock file %q: %w", tmpPath, errors.Join(werr, cerr))
	}

	if lerr := os.Link(tmpPath, path); lerr != nil {
		if errors.Is(lerr, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("filelock: publish lock file %q: %w", path, lerr)
	}
	return true, nil
}

// readLockPID reads path's content and parses it as a PID. An empty (or
// all-whitespace) file is treated as pid 0, which processAlive always
// reports as not alive -- routing the caller into the stale-lock reclaim
// path rather than wedging on a lock nobody holds.
func readLockPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("content %q is not a valid pid: %w", string(data), err)
	}
	return pid, nil
}

// processAlive reports whether pid corresponds to a currently running
// process, portably across macOS, Linux, and Windows. See buzz/lock.go's
// processAlive doc comment for the per-OS mechanism this mirrors exactly.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// AcquireWait acquires the lock file at path, retrying with a short capped
// exponential backoff until it succeeds or timeout elapses. Unlike a
// fail-fast singleton lock, AcquireWait is for callers that expect many
// legitimate concurrent holders over time, each of which should wait its
// turn rather than error out on contention.
//
// A stale lock (its content names a PID that is no longer running -- e.g.
// left behind by a process that crashed while holding it) is reclaimed and
// retried automatically, using the same PID-liveness heuristic as
// buzz.AcquireLock.
func AcquireWait(path string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	const (
		initialBackoff = 2 * time.Millisecond
		maxBackoff     = 50 * time.Millisecond
	)
	backoff := initialBackoff

	for {
		acquired, err := publishLockFile(path)
		if err != nil {
			return nil, err
		}
		if acquired {
			return &Lock{path: path}, nil
		}

		if holderPID, rerr := readLockPID(path); rerr == nil && !processAlive(holderPID) {
			// Stale lock left behind by a holder that is no longer
			// running; reclaim it and retry immediately. A benign race
			// with another waiter doing the same reclaim is safe --
			// exactly one publish wins next iteration.
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return nil, fmt.Errorf("filelock: remove stale lock file %q: %w", path, rmErr)
			}
			continue
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("filelock: timed out after %s waiting to acquire lock %q", timeout, path)
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
