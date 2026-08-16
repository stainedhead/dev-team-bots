package sharedstate_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/sharedstate"
)

// captureLogs swaps slog's default logger for the duration of the test,
// returning a function that yields everything logged so far. Restores the
// prior default logger on test cleanup.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf.String
}

// TestEnsureOwner_FirstCall_ClaimsDirectoryAndReturnsMatched verifies that
// the first process to resolve a shared-state directory (e.g. the orchestrator
// persona's board.json/chat.json/tasks.json directory, FR-501) claims it by
// writing a marker file recording its identity, and reports a match.
func TestEnsureOwner_FirstCall_ClaimsDirectoryAndReturnsMatched(t *testing.T) {
	dir := t.TempDir()

	matched, err := sharedstate.EnsureOwner(dir, "orchestrator")
	if err != nil {
		t.Fatalf("EnsureOwner: %v", err)
	}
	if !matched {
		t.Fatalf("expected first call to claim the directory and report matched=true")
	}

	markerPath := filepath.Join(dir, sharedstate.MarkerFileName)
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("expected marker file to be created at %q: %v", markerPath, statErr)
	}
}

// TestEnsureOwner_SameIdentity_ReturnsMatched verifies that a process
// re-resolving the same directory with the identity that originally claimed
// it (e.g. native mode restarting, or an ACP worker pool starting a second
// instance) continues to report a match -- not a false-positive divergence
// warning on every restart.
func TestEnsureOwner_SameIdentity_ReturnsMatched(t *testing.T) {
	dir := t.TempDir()

	if _, err := sharedstate.EnsureOwner(dir, "orchestrator"); err != nil {
		t.Fatalf("first EnsureOwner: %v", err)
	}

	matched, err := sharedstate.EnsureOwner(dir, "orchestrator")
	if err != nil {
		t.Fatalf("second EnsureOwner: %v", err)
	}
	if !matched {
		t.Fatalf("expected second call with the same identity to report matched=true")
	}
}

// TestEnsureOwner_DifferentIdentity_ReturnsMismatchNotError verifies FR-501's
// central safety net: a directory already claimed by one identity, resolved
// again by a different identity (e.g. a renamed persona, or two unrelated
// bots accidentally configured to point at the same shared-state root),
// reports matched=false so the caller can log a loud warning -- but does not
// error, since construction must degrade gracefully (spec.md NFR-Reliability)
// rather than block startup.
func TestEnsureOwner_DifferentIdentity_ReturnsMismatchNotError(t *testing.T) {
	dir := t.TempDir()

	if _, err := sharedstate.EnsureOwner(dir, "orchestrator"); err != nil {
		t.Fatalf("first EnsureOwner: %v", err)
	}

	matched, err := sharedstate.EnsureOwner(dir, "architect")
	if err != nil {
		t.Fatalf("EnsureOwner with different identity should not error: %v", err)
	}
	if matched {
		t.Fatalf("expected mismatched identity to report matched=false")
	}
}

// TestEnsureOwner_ConcurrentFirstClaims_OneWinnerNoCorruption guards against
// the marker-write itself becoming a new instance of the exact clobber
// hazard this feature closes elsewhere (board.go, chat_store.go,
// direct_task_store.go): two processes racing to claim the same
// never-before-seen directory must not corrupt the marker file or crash --
// exactly one identity wins, and every subsequent caller (regardless of its
// own identity) observes that same winner consistently.
func TestEnsureOwner_ConcurrentFirstClaims_OneWinnerNoCorruption(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(2)
	var errA, errB error
	go func() {
		defer wg.Done()
		_, errA = sharedstate.EnsureOwner(dir, "identity-a")
	}()
	go func() {
		defer wg.Done()
		_, errB = sharedstate.EnsureOwner(dir, "identity-b")
	}()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("EnsureOwner errors: a=%v b=%v", errA, errB)
	}

	matchedA, err := sharedstate.EnsureOwner(dir, "identity-a")
	if err != nil {
		t.Fatalf("re-check identity-a: %v", err)
	}
	matchedB, err := sharedstate.EnsureOwner(dir, "identity-b")
	if err != nil {
		t.Fatalf("re-check identity-b: %v", err)
	}
	if matchedA == matchedB {
		t.Fatalf("expected exactly one identity to have won the race, got matchedA=%v matchedB=%v", matchedA, matchedB)
	}
}

// TestEnsureOwner_MalformedMarker_LogsDistinctWarningAndReclaims is
// FR-R2's regression test (acp-native-shared-state-auto-review-PRD.md): a
// marker file that exists but fails to parse (or has an empty owner) is
// still tolerated as "unclaimed" and overwritten -- mirroring board.go's
// "malformed = empty" convention -- but must log a warning distinct from
// the identity-mismatch case, so an operator has a trail if this ever
// happens in practice (e.g. a torn write from a crash mid-publish).
func TestEnsureOwner_MalformedMarker_LogsDistinctWarningAndReclaims(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, sharedstate.MarkerFileName)
	if err := os.WriteFile(markerPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("seed malformed marker: %v", err)
	}

	logs := captureLogs(t)
	matched, err := sharedstate.EnsureOwner(dir, "orchestrator")
	if err != nil {
		t.Fatalf("EnsureOwner: %v", err)
	}
	if !matched {
		t.Fatalf("expected a malformed marker to be tolerated as unclaimed and reclaimed, got matched=false")
	}
	if !strings.Contains(logs(), "malformed") {
		t.Errorf("expected a warning mentioning the malformed marker, got logs: %q", logs())
	}
	if strings.Contains(logs(), "already claimed by a different identity") {
		t.Errorf("malformed-marker warning must be distinct from the identity-mismatch warning, got logs: %q", logs())
	}
}

// TestEnsureOwner_FirstClaim_LogsNothing guards FR-R2's negative case: the
// normal "no marker exists yet" path must not log anything new -- only a
// marker that exists but fails to parse should.
func TestEnsureOwner_FirstClaim_LogsNothing(t *testing.T) {
	dir := t.TempDir()

	logs := captureLogs(t)
	if _, err := sharedstate.EnsureOwner(dir, "orchestrator"); err != nil {
		t.Fatalf("EnsureOwner: %v", err)
	}
	if got := logs(); got != "" {
		t.Errorf("expected no log output for a normal first claim, got: %q", got)
	}
}
