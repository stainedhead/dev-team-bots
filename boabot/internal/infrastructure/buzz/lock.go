package buzz

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ErrAlreadyRunning is the typed sentinel error AcquireLock returns
// (always wrapped with contextual detail via %w) when another live
// process already holds the OQ-1/FR-031 singleton lock for a given Buzz
// identity. Callers -- see Monitor.Start -- MUST treat this as "decline
// to attach the Buzz monitor, log clearly, leave everything else running"
// (FR-003's fail-fast-without-crashing precedent, mirroring
// internal/infrastructure/credentials.Load's world-readable-file check),
// never as a fatal condition for the whole process.
var ErrAlreadyRunning = errors.New("buzz: another boabot process already holds the singleton lock for this identity")

// Lock is a held process-singleton lock file (FR-031/OQ-1). The zero value
// is not usable; obtain one via AcquireLock. Release removes the lock
// file, freeing the identity for a future acquirer (e.g. a clean restart
// of the same process).
type Lock struct {
	path string
}

// lockFileName returns the OQ-1 singleton lock file's basename for a
// hex-encoded pubkey. It intentionally takes only the derived PUBLIC key,
// never the raw nsec/secret key -- this package never writes secret key
// material to disk anywhere, including here (see keypair.go's own
// never-log-the-secret discipline, which this mirrors). A short prefix
// keeps the filename tidy; this is a local, single-operator lock file, not
// a security boundary, so prefix-collision resistance is not a concern.
func lockFileName(pubkeyHex string) string {
	short := pubkeyHex
	const shortLen = 16
	if len(short) > shortLen {
		short = short[:shortLen]
	}
	return fmt.Sprintf("buzz-%s.lock", short)
}

// LockPath returns the full OQ-1 singleton lock file path for pubkeyHex
// under dir. dir follows this codebase's existing per-bot memory-root
// convention (internal/infrastructure/local/fs, internal/application/
// team.ManagerConfig.MemoryRoot) -- Phase H's cmd/boabot/main.go wiring is
// expected to pass the bot's memory directory here.
func LockPath(dir, pubkeyHex string) string {
	return filepath.Join(dir, lockFileName(pubkeyHex))
}

// AcquireLock implements FR-031/OQ-1's process-level singleton lock. It is
// dependency-free (stdlib only, per architecture.md's portability
// constraint): the lock file's content is the acquiring process's PID,
// published atomically (see publishLockFile) so two processes racing to
// create it can never both "win" the create, and no third process can ever
// observe a partially-written lock file under its final name (FR-004).
//
// Stale-lock handling: if path already exists, its content is read as a
// PID. An empty file is treated as pid 0, which is never alive (see
// readLockPID's doc comment for exactly which crash window this defends
// against as of FR-004's fix). If that PID does not currently correspond
// to a running process (the classic "left behind by a process that
// crashed without cleanup" case), the file is removed and creation is
// retried -- so a stale lock can never permanently wedge future starts. If
// the existing PID *is* still running, or the file's content is non-empty
// but cannot be parsed as a PID at all (garbage content we cannot
// distinguish from "someone else's lock" without more information),
// AcquireLock fails closed and returns an error wrapping
// ErrAlreadyRunning -- never silently stealing a lock we cannot prove is
// abandoned.
//
// Known limitation, accepted per the design brief: PID reuse by an
// unrelated process between the original holder's crash and our liveness
// check would cause a false "still held" read. This is inherent to any
// PID-based lock and is bounded by how quickly PIDs recycle on the host
// OS; it does not affect the common case this guards against (operator
// double-start, botched upgrade).
func AcquireLock(path string) (*Lock, error) {
	for {
		acquired, err := publishLockFile(path)
		if err != nil {
			return nil, err
		}
		if acquired {
			return &Lock{path: path}, nil
		}

		holderPID, rerr := readLockPID(path)
		if rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				// Vanished between our EEXIST and our read (another
				// process released or reclaimed it) -- retry the create.
				continue
			}
			return nil, fmt.Errorf("%w: lock file %q exists and could not be read as a pid: %v", ErrAlreadyRunning, path, rerr)
		}

		if processAlive(holderPID) {
			return nil, fmt.Errorf("%w: pid %d holds lock file %q", ErrAlreadyRunning, holderPID, path)
		}

		// Stale lock: reclaim by removing the abandoned file and retrying
		// the atomic publish. A benign race with another process doing the
		// same reclaim is safe -- exactly one publish wins; the loser loops
		// back through this same path and observes the winner's fresh,
		// live PID next time.
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("buzz: remove stale lock file %q: %w", path, rmErr)
		}
	}
}

// acquireLockAfterCreateHook, when non-nil, is invoked by publishLockFile
// immediately after it has created the not-yet-populated carrier for the
// lock file's content (the same-directory temp file), but before that
// content is written. It exists solely as a test seam (see
// lock_race_test.go) for deterministically reproducing the FR-004 TOCTOU
// window under a controlled schedule rather than relying on scheduler
// luck. Nil (the production default) means no delay; it must never be set
// outside a test.
var acquireLockAfterCreateHook func()

// publishLockFile attempts to atomically publish path as a lock file
// containing the current process's PID as its entire content.
//
// It reports (true, nil) if this call won the race and path now names a
// lock file it created and fully populated; (false, nil) if path already
// existed (an fs.ErrExist-equivalent condition), so the caller should fall
// through to AcquireLock's stale-lock read/reclaim path; or a non-nil
// error for any other failure.
//
// FR-004: publication is atomic -- there is no window in which path names
// a file that exists but has not yet been fully written, observable by a
// concurrent AcquireLock racing against the same path. POSIX has no
// "create with content" primitive (open(O_CREATE|O_EXCL) and write() are
// always separate syscalls), so instead the PID is written into a
// same-directory temporary file first (same directory so the subsequent
// publish step is guaranteed to be on the same filesystem/volume), fsync'd
// and closed, and only then published under path's final name via
// os.Link. link(2) creates the new directory entry and the file's content
// atomically together -- there is no "created but empty" state visible
// under the final name -- and fails with an fs.ErrExist-compatible error
// if path already exists, without disturbing the existing file.
//
// os.Link, not the create-temp-then-os.Rename fallback, per architecture.md
// AD-3 and research.md research question 3 (which this resolves): Rename
// is not actually a viable fallback for mutual exclusion at all -- per its
// own doc comment, "If newpath already exists and is not a directory,
// Rename replaces it" on every platform, so two racing publishers would
// both rename onto path and both succeed, which is worse than the bug
// FR-004 fixes. os.Link is the only stdlib primitive with atomic
// create-with-content-or-fail-if-exists semantics; there is no live
// alternative to evaluate it against.
//
// What is actually verified about os.Link on windows GOOS (not merely
// asserted from general knowledge, the earlier retracted AD-3 claim this
// replaces) -- read directly from this toolchain's stdlib source:
//   - os/file_windows.go's Link calls syscall.CreateHardLink.
//   - syscall_windows.go's Errno.Is maps both ERROR_ALREADY_EXISTS and
//     ERROR_FILE_EXISTS to fs.ErrExist, and os.LinkError.Unwrap returns
//     the underlying error, so errors.Is(err, fs.ErrExist) IS reliable
//     on windows *if* CreateHardLink sets one of those two codes for an
//     existing target.
//   - That specific "if" -- what Win32 error CreateHardLinkW actually
//     returns on an existing target -- was not independently verified
//     against Win32 documentation or a live Windows run here. Indirect
//     but real evidence it holds: Go's own os_test.go TestHardLink (no
//     windows build skip, gated only on testenv.MustHaveLink) links onto
//     an existing name and asserts IsExist(err.Err) -- and this test runs
//     on Go's own Windows CI builders as part of every stdlib release,
//     which is the closest available confirmation without a local
//     Windows machine.
//   - A GOOS=windows GOARCH=amd64 cross-compile of this package (go build
//     and go vet) was also confirmed to succeed.
//   - Residual risk is bounded, not a safety hole: if CreateHardLinkW
//     ever returned a different errno for "already exists" than the two
//     handled here, os.Link would return a non-ErrExist error, and this
//     function's caller (AcquireLock) would surface it as a hard error
//     rather than granting a second lock -- see AcquireLock's err
//     handling. The failure mode of an unverified-but-wrong assumption
//     here is a wrongly-typed error, never a double-granted lock.
//
// See implementation-notes.md for the full verification trail.
func publishLockFile(path string) (bool, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".buzz-lock-*.tmp")
	if err != nil {
		return false, fmt.Errorf("buzz: create temp lock file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Always remove our temp file at the end. Harmless (not a no-op) once
	// Link has succeeded: Link creates a second directory entry pointing
	// at the same inode/content, so removing the temp name here only
	// drops the link count back to 1 -- the published path entry and its
	// content are unaffected.
	defer func() { _ = os.Remove(tmpPath) }()

	if acquireLockAfterCreateHook != nil {
		acquireLockAfterCreateHook()
	}

	_, werr := tmp.WriteString(strconv.Itoa(os.Getpid()))
	if werr == nil {
		werr = tmp.Sync()
	}
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return false, fmt.Errorf("buzz: write temp lock file %q: %w", tmpPath, errors.Join(werr, cerr))
	}

	if lerr := os.Link(tmpPath, path); lerr != nil {
		if errors.Is(lerr, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("buzz: publish lock file %q: %w", path, lerr)
	}
	return true, nil
}

// Release removes the lock file, freeing the identity for a future
// acquirer. Idempotent: releasing an already-released (or otherwise
// missing) lock is not an error.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("buzz: release lock file %q: %w", l.path, err)
	}
	return nil
}

// readLockPID reads path's content and parses it as a PID.
//
// An empty (or all-whitespace) file is treated as pid 0, not an error.
// As of FR-004's fix, publishLockFile makes lock-file publication atomic
// (same-directory temp file + os.Link), so AcquireLock's own write path
// can no longer produce an empty file under path's final name -- that
// specific in-process race is closed by construction, not merely made
// unlikely. This branch now exists for lock files that predate FR-004's
// fix (e.g. a zero-byte file left by an older boabot binary's
// O_CREATE|O_EXCL-then-WriteString sequence, crashed mid-write) or any
// other on-disk artifact with genuinely empty content, however it arose
// (e.g. external interference with the lock directory). processAlive(0)
// is always false, so this naturally routes AcquireLock into the
// stale-lock reclaim path rather than wedging future starts on a lock
// nobody is actually holding.
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
// process, portably across the three OSes this codebase supports
// (macOS, Linux, Windows -- NFR Portability).
//
// On Unix, os.FindProcess always succeeds regardless of whether pid
// exists, so existence is tested by sending the null signal (signal 0),
// which the OS validates without actually delivering anything.
//
// On Windows, os.FindProcess itself opens a handle to the process and
// fails if pid does not exist -- that success/failure *is* the liveness
// check there. proc.Signal is not meaningfully usable on Windows (only
// os.Kill is supported), so it is not consulted on that platform.
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
