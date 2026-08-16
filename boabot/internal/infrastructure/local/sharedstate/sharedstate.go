// Package sharedstate provides a small safety net for directories that
// native daemon mode and ACP mode (boabot -acp) may share -- the orchestrator
// persona's board.json/chat.json/tasks.json directory (FR-501,
// specs/260816-acp-native-shared-state/spec.md).
//
// The two modes have no channel to compare configuration with each other
// directly: an ACP worker only ever loads its own persona's config.yaml and
// has no way to read native mode's separate top-level config.yaml, and
// native mode may not even be running. Cross-process validation of "are
// these two processes configured to point at the same root" is therefore
// not possible. What EnsureOwner validates instead is narrower but
// genuinely implementable: whether a directory that has already been
// claimed by one identity (a bot/persona name) is now being resolved again
// by a different identity -- e.g. a renamed persona reusing an old
// directory, or two unrelated personas accidentally configured to point at
// the same shared-state root. This is checkable purely from what the
// directory itself already contains, no cross-process communication
// required.
package sharedstate

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/filelock"
)

// MarkerFileName is the name of the marker file EnsureOwner reads and
// writes inside a shared-state directory.
const MarkerFileName = ".shared-state-owner"

// markerLockTimeout bounds how long EnsureOwner waits to acquire the
// marker file's own lock before giving up -- generous relative to the
// sub-millisecond cost of reading/writing a few bytes of JSON.
const markerLockTimeout = 5 * time.Second

type markerState struct {
	Owner string `json:"owner"`
}

// EnsureOwner claims dir for identity if no marker exists yet, or checks an
// existing marker's recorded owner against identity if one does. It reports
// (true, nil) when this call's identity matches the directory's owner
// (either because it just claimed it, or because it already owned it), and
// (false, nil) -- not an error -- when the directory is already owned by a
// different identity, so callers can log a specific warning without
// blocking construction (spec.md's NFR-Reliability: degrade gracefully).
// Concurrent first-claims on the same never-before-seen directory are
// resolved by the same cross-process filelock primitive board.go/
// chat_store.go/direct_task_store.go already use: exactly one identity
// wins, and every later caller observes that winner consistently.
func EnsureOwner(dir, identity string) (bool, error) {
	markerPath := filepath.Join(dir, MarkerFileName)
	lockPath := markerPath + ".lock"

	lock, err := filelock.AcquireWait(lockPath, markerLockTimeout)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Release() }()

	data, readErr := os.ReadFile(markerPath)
	if readErr == nil {
		var state markerState
		unmarshalErr := json.Unmarshal(data, &state)
		if unmarshalErr == nil && state.Owner != "" {
			return state.Owner == identity, nil
		}
		// Malformed marker content: treat as unclaimed and overwrite below,
		// mirroring the other shared-state stores' "malformed = empty"
		// tolerance (board.go's readDiskItems doc comment) -- but log it,
		// distinct from the identity-mismatch warning below, since an
		// unexpectedly malformed marker (e.g. a torn write from a crash
		// mid-publish) is worth an operator's attention even though it's
		// tolerated rather than treated as fatal.
		slog.Warn("sharedstate: malformed owner marker encountered; treating directory as unclaimed and reclaiming it",
			"path", markerPath, "unmarshal_err", unmarshalErr)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	marshaled, err := json.Marshal(markerState{Owner: identity})
	if err != nil {
		return false, err
	}
	tmp := markerPath + ".tmp"
	if err := os.WriteFile(tmp, marshaled, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, markerPath); err != nil {
		return false, err
	}
	return true, nil
}
