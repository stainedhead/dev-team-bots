# Data Dictionary: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04

**Purpose:** This is a review-fix pass over existing types, not new system design — most of the original feature's `data-dictionary.md` (`specs/archive/260804-boabot-buzz-support/data-dictionary.md`) still applies unchanged. This file only documents the small number of new or modified data shapes each finding's Green guidance introduces.

---

## New / Modified Types

### FR-001: Auth-tag secret

- **`AuthTagSecretName`** (new constant, `internal/infrastructure/buzz/keypair.go` or `token.go`, alongside existing `PrivateKeySecretName`/`APITokenSecretName`): the `domain.SecretRef` name for the NIP-OA auth tag, e.g. `"buzz_auth_tag"`.
- **Serialized value format** (resolved via OQ-R1, see `research.md`): pipe-delimited string `owner_pubkey_hex|conditions|sig_hex` — matching `SignAuthTag`'s own construction shape (owner pubkey, conditions string, Schnorr signature, all hex-encoded). Parsed by `boabot`'s `buildBuzzMonitor`, not by `boabotctl` (which treats it as an opaque string like every other secret). No JSON envelope — pipe-delimited chosen for consistency with how the tag's three components are already named in the review PRD's own FR-001 text.

### FR-002/FR-003: `subEntry` attach-generation tracking

- **`subEntry.generation`** (new field, `uint64` or similar, `internal/infrastructure/buzz/relay_client.go`): incremented by `attachSub` on each attach attempt (compare-and-swap style); a pump that discovers it is no longer the current generation exits without sending, closing the FR-002 double-attach window.
- **`subEntry.pumpWG` (or equivalent) replacing the single `pumpDone` channel**: a per-entry `sync.WaitGroup` (or a slice of completion channels) covering *every* attach generation ever started for that entry, not just the most recent — required so `removeAndClose` waits on all pumps, not just the last-registered one. `pumpDone`'s doc comment ("set by the most recent attachSub") is the single-slot design being replaced.
- **`RelayClient.closed`**: type changed from a plain `bool` guarded by `rc.mu` to `atomic.Bool`, so both `Close()` and `attachSub` can check it from either critical section (`rc.mu` or `subMu`) without introducing lock nesting — the specific mechanism FR-003's Green guidance identifies as making a combined FR-002+FR-003 fix safe.

### FR-004: Lock-file atomic write

- No new types — `AcquireLock`'s internal write path changes from `OpenFile(O_CREATE|O_EXCL) → WriteString → Close` to `write-to-temp-file-in-same-dir → os.Link(tmp, path) → remove-temp`. `readLockPID`'s empty-file-as-pid-0 handling is retained (still defends the SIGKILL-mid-write case, now provably unreachable via the new path but harmless to keep as defense-in-depth per the PRD's own Refactor note).

### FR-005: Content-size bound

- **`maxContentLen`** (new constant or `Monitor.Config` field, `internal/infrastructure/buzz/monitor.go` — decision left to WS-D per the PRD's own Refactor note, default recommendation: package constant unless an operator-tunable need is identified during implementation, consistent with OQ-R2's resolution for FR-007). Type: `int`, byte length bound on `evt.Content` before it becomes a `TaskPayload.Instruction`.

### FR-008: `kindChannelMessage` reference

- No new type — `trigger.go:28`'s bare `9` literal is replaced with the existing `kindChannelMessage` constant already defined in `monitor.go`. No new symbol introduced.

## Unaffected (carried from the original feature, no change)

`domain.SecretRef`, `domain.SecretStore`, `domain.Event`, `domain.Filter`, `domain.RelayClient`, `TaskPayload`, `pendingEntry`, `Monitor.Config` (except FR-005's possible new field) — see `specs/archive/260804-boabot-buzz-support/data-dictionary.md` for full definitions.
