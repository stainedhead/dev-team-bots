# Research: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04
**Source PRD:** `boabot-buzz-support-auto-review-PRD.md` (this directory)

---

## Research Questions

1. **OQ-R1 (FR-001 Refactor, from the review PRD):** Should `boabotctl secret set` gain a `--format` hint for the new multi-part auth-tag secret (`owner_pubkey_hex|conditions|sig_hex` or a JSON envelope), or is asking the operator to paste an external attestation tool's raw tag output sufficient?
   **Resolution (this pass, following the original run's pattern of resolving non-blocking OQs with the PRD's own stated lean):** No `--format` flag. The secret is a single opaque string the operator pastes verbatim from an external attestation-issuance tool's output (issuance tooling remains out of scope per OQ-2/OQ-5's already-accepted scoping). `AuthTagSecretName`'s resolved value is parsed by `boabot` itself (pipe-delimited `owner_pubkey_hex|conditions|sig_hex`, matching `SignAuthTag`'s own tag-construction shape) — `boabotctl` treats it as an opaque string like every other secret. Document this format precisely in `user-docs/Buzz-Adoption-Config.md` (WS-A).

2. **OQ-R2 (FR-007, from the review PRD):** Is reconnect backoff (`WithBackoff`/`WithAuthRetryInterval`) something an operator actually needs to tune, or is the hardcoded default (1s/30s backoff, 200ms auth retry) permanent?
   **Resolution (this pass):** No operator need has surfaced in the original PRD or this review. Permanent default — record as a one-line `BuzzConfig` doc comment (WS-E) rather than adding new config fields. Revisitable in a future run if an operator need surfaces.

3. **Does `os.Link`'s `EEXIST`-atomic "create with content" semantics (FR-004's Green guidance) hold identically on Windows/NTFS, or only on POSIX filesystems?** Not asked by the review PRD itself but load-bearing for WS-C, since the process-singleton lock ships on macOS/Windows/Linux (per the original PRD's NFR/Portability requirement) and `AcquireLock`'s existing PID-liveness code already has documented Windows-specific caveats (Phase G's "Known limitation" note in the archived `status.md`).
   **Status: OPEN — not verified.** An earlier draft of this file asserted this was "confirmed" based on general knowledge of `CreateHardLinkW`/NTFS semantics, without running any tool call (no source read, no cross-compile test, no live Windows check) against this toolchain's actual `os.Link` behavior. That assertion has been retracted — asserting a resolution path nobody verified is exactly the defect class FR-001 exists to fix, and this spec should not repeat it. **This question is now owned by WS-C's Red step (`tasks.md` WS-C1/WS-C2):** before depending on `os.Link` cross-platform, confirm empirically (at minimum a `GOOS=windows` cross-compiled unit test asserting the expected `fs.ErrExist`-compatible error when the link target exists; a real Windows run if available) that `os.Link` on `windows` GOOS behaves atomically and surfaces the expected error. **Fallback if it does not check out:** same-directory temp file, `fsync`, then atomic `os.Rename` into place — `rename(2)`/`MoveFileEx` is a more universally-established atomic same-directory publish primitive than `link` if `os.Link`'s Windows semantics prove unsuitable. See `tasks.md` WS-C2 for the task-level instruction to verify before committing to either primitive.

4. **Does the review PRD's WS-B recommend a specific mechanism for FR-002/FR-003's shared fix, or is "attach-generation counter vs. continuous-lock-holding" genuinely still open?** The PRD's Refactor note under FR-002 explicitly poses this as an open choice ("Consider whether `Subscribe` should hold `subMu` continuously... likely the smaller, more auditable fix, provided it doesn't reintroduce a 'network call under lock' liveness concern"), while FR-003's Green guidance leans toward a generation/atomic-closed-flag approach as the concretely safe combined mechanism.
   **Resolution:** Recorded as a decided architectural choice, not left open — see `architecture.md`'s Architectural Decisions section. Continuous-lock-holding is rejected (network call under `subMu` risks a liveness/deadlock surface with `reconnect()`'s own lock ordering); attach-generation counter + `closed atomic.Bool` is adopted, matching FR-003's own Green guidance and avoiding the lock-order hazard FR-003 explicitly flags.

## Existing Implementations

- `internal/infrastructure/buzz/lock.go` (Phase G, archived): existing `AcquireLock`/`Lock.Release` — FR-004's fix modifies this file's write path only, not its liveness-check logic.
- `internal/infrastructure/buzz/relay_client.go`, `reconnect.go` (Phase D/D8, archived): existing `attachSub`/`subEntry`/`pumpDone`/`resubscribeAll` — FR-002/FR-003's fix target. See `nipoa.go`, `keypair.go`, `token.go` (Phase E/D4/D6) for the existing `SecretRef`-resolution pattern FR-001's new `AuthTagSecretName` should mirror exactly.

## API Documentation

No new third-party APIs introduced by any finding. `os.Link` (Go stdlib) is the only newly-relied-upon primitive (FR-004) — see research question 3 above.

## Best Practices

- `sync.WaitGroup`'s documented misuse pattern ("new `Add` calls must happen after all previous `Wait` calls have returned") is the precise defect class FR-003 describes — the fix must ensure this invariant holds structurally, not merely avoid observing a violation in testing.
- POSIX has no "create with content" primitive — `open(O_CREATE|O_EXCL)` and `write()` are always two syscalls (FR-004's Green guidance already states this; confirmed, not re-derived here).

## Open Questions

Both OQ-R1 and OQ-R2 above are resolved for this pass (see Resolution notes). **Research question 3 (Windows/NTFS `os.Link` semantics) is OPEN** — owned by WS-C's Red/Green steps in `tasks.md`, must be verified empirically before WS-C2 commits to `os.Link` over the `os.Rename` fallback.

## References

- `boabot-buzz-support-auto-review-PRD.md` (this directory) — source of OQ-R1, OQ-R2, and all eight findings.
- `specs/archive/260804-boabot-buzz-support/research.md` — original feature's research, including the confirmed `fiatjaf.com/nostr` `checkptr` bug (still applies to every `-race` run touching `internal/infrastructure/buzz`).
