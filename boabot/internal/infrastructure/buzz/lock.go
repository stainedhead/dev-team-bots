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
// created with os.O_EXCL so two processes racing to create it can never
// both "win" the create.
//
// Stale-lock handling: if path already exists, its content is read as a
// PID (an empty file -- e.g. a process SIGKILLed between O_CREATE and the
// PID write -- counts as pid 0, which is never alive; see readLockPID).
// If that PID does not currently correspond to a running process (the
// classic "left behind by a process that crashed without cleanup" case),
// the file is removed and creation is retried -- so a stale lock can
// never permanently wedge future starts. If the existing PID *is* still
// running, or the file's content is non-empty but cannot be parsed as a
// PID at all (garbage content we cannot distinguish from "someone else's
// lock" without more information), AcquireLock fails closed and returns
// an error wrapping ErrAlreadyRunning -- never silently stealing a lock we
// cannot prove is abandoned.
//
// Known limitation, accepted per the design brief: PID reuse by an
// unrelated process between the original holder's crash and our liveness
// check would cause a false "still held" read. This is inherent to any
// PID-based lock and is bounded by how quickly PIDs recycle on the host
// OS; it does not affect the common case this guards against (operator
// double-start, botched upgrade).
func AcquireLock(path string) (*Lock, error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.WriteString(strconv.Itoa(os.Getpid()))
			cerr := f.Close()
			if werr != nil || cerr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("buzz: write lock file %q: %w", path, errors.Join(werr, cerr))
			}
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("buzz: create lock file %q: %w", path, err)
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
		// the O_EXCL create. A benign race with another process doing the
		// same reclaim is safe -- exactly one create wins; the loser loops
		// back through this same path and observes the winner's fresh,
		// live PID next time.
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("buzz: remove stale lock file %q: %w", path, rmErr)
		}
	}
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
// An empty (or all-whitespace) file is treated as pid 0, not an error --
// the realistic crash artifact this represents is a process SIGKILLed
// between AcquireLock's O_CREATE|O_EXCL and its WriteString, leaving a
// zero-byte file behind. processAlive(0) is always false, so this
// naturally routes AcquireLock into the stale-lock reclaim path rather
// than wedging future starts on a lock nobody is actually holding -- the
// exact "left behind by a process that crashed without cleanup" case the
// design brief requires never permanently blocks a future start.
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
