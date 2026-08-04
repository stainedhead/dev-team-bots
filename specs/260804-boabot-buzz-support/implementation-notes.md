# Implementation Notes: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04

**Purpose:** Running log of technical decisions, edge cases, deviations from plan, and lessons learned during implementation. Update this file as work proceeds — do not wait until the end.

---

## Technical Decisions

Decisions made during PRD/spec pre-flight, recorded here so implementation doesn't re-litigate them:

- **Scope:** full PRD, both workstreams (Buzz + Secret Storage), implemented in one dev-flow run — user's explicit choice over "Buzz P0 only."
- **OQ-1 (multi-instance singleton):** resolved as a process-level lock on the nsec (PRD option (a)), not a startup presence probe (b) or reply dedup (c). FR-031 is implemented and tested, not deferred.
- **Untestable acceptance criteria:** implemented as `//go:build integration` tests, not run automatically in this job. Flagged in this file (see below) for manual verification against real infrastructure.
- **OQ-9 (secret namespace key):** bot **name**, not bot type — consistent with `BotName` usage elsewhere in the codebase.
- **Other non-blocking open questions (OQ-2, OQ-3, OQ-4, OQ-5, OQ-6, OQ-8, OQ-10, OQ-11):** not resolved with a hard decision — implementation proceeds per the PRD's own stated lean where a concrete choice is unavoidable (e.g., OQ-2 doesn't block implementing FR-005–007 since attestation *validation* doesn't require deciding the *issuance* workflow); genuinely deferred items (OQ-5 `boabotctl` attestation tooling, OQ-6 NIP-AE engrams) remain out of this implementation's scope per the PRD's own P1/P2 phasing.
- **OQ-11 resolved for `boabotctl secret` (FR-049):** local machine only, no remote-bot writes. Simpler and matches the PRD's stated lean.

## Decisions made during spec review (Step 2)

Recorded here so Step 3 implementation doesn't have to re-derive them from the review conversation:

- **Scope resolution (spec.md/plan.md/tasks.md were inconsistent before this review):** the full in-scope FR range for this run is FR-001–FR-054, including FR-025 (typing), FR-026 (gated `!shutdown`), and FR-027 (reactions) pulled forward from PRD P1 — `plan.md`'s Phase F already implemented them, and leaving them out while implementing everything else in Phase F would have left in-progress code contradicting the phasing doc. FR-048 is in scope for its warn-only clause only; the post-deprecation hard-rejection trigger is deferred one release, per the PRD's own P2 phasing. See `spec.md` §Timeline and Milestones and `tasks.md` §Deferred Items for the authoritative list.
- **`domain.Event`/`domain.Filter` are domain-owned types, not `fiatjaf.com/nostr`'s library types.** `data-dictionary.md` previously left this "TBD, likely reused directly" — that would fail the PRD's own `grep -r "fiatjaf.com/nostr" internal/domain` acceptance criterion. `internal/infrastructure/buzz/relay_client.go` now owns a translation layer between the two, itself a tested component (`tasks.md` D3).
- **`boabotctl`'s secret subcommands (FR-049) cannot share code with `boabot`'s `internal/infrastructure/secret/keystore`** — different Go module, and `internal/` visibility is per-module-root. `boabotctl` takes its own `zalando/go-keyring` dependency and only shares the FR-045 key-naming *convention* with `boabot`, not code. See `architecture.md` §Cross-Module Constraint.
- **Edge cases with no prior design note, now resolved** (see `architecture.md` §Edge Cases and Failure Paths for full detail): FR-022 reply-publish failure (log, don't re-enqueue — the worker already ran); per-secret-provider timeout (2s per provider via `context.WithTimeout`, a timeout is a miss not an error); the pending task-ID map is untouched across reconnect (only connection/auth/subscriptions are re-established); the presence ticker suspends while disconnected and resumes on reconnect; `respond_to_allowlist` nil (no gate) vs. explicit empty list (allow-none) are distinct configurations.

## Manual Verification Required (not run in this job)

To be filled in as each integration-tagged test is written. Placeholder categories from the PRD's acceptance criteria:

- [ ] Live `buzz-relay` (Buzz's `docker-compose.yml`): NIP-42 auth, `kind:0` profile publish (renders name/description, not a bare pubkey, in the Buzz desktop client), `kind:9` pub/sub, reconnect-after-restart, relay-side membership revocation (agent's next connection fails `restricted:`). (`tasks.md` I1)
- [ ] NIP-AA virtual membership, write path: a NIP-AA-authenticated bot publishes its `kind:0` profile without being explicitly enrolled in `relay_members`. (`tasks.md` I1)
- [ ] NIP-AA virtual membership, read path (negative): the same bot is confirmed **not** to inherit the owner's channel memberships — it cannot read a private channel the owner belongs to, and does not. (`tasks.md` I1)
- [ ] Two boabot processes started against the same nsec: exactly one reply event on the relay, one presence identity — confirms FR-031/G1's lock has the intended effect end-to-end, not just in the single-process unit test. (`tasks.md` G1, I1)
- [ ] macOS interactive: a secret written to the login keychain is resolved by an interactively-run boabot, nothing in `config.yaml` or `~/.boabot/credentials`. (`tasks.md` I2)
- [ ] Windows interactive: the same, via Credential Manager. (`tasks.md` I2)
- [ ] Linux interactive: the same, via Secret Service in a desktop session. (`tasks.md` I2)
- [ ] macOS LaunchDaemon: System keychain reachability for the keystore provider (FR-041's core open question). (`tasks.md` I2)
- [ ] Windows service: Credential Manager read/write under the service's own account identity (OQ-7). (`tasks.md` I2)
- [ ] Linux systemd unit: `LoadCredentialEncrypted=` → `$CREDENTIALS_DIRECTORY` resolution with no session D-Bus available. (`tasks.md` I2)
- [ ] Linux systemd unit, negative path: only a Secret Service entry present, no systemd credential — resolution fails with an error naming every provider consulted, not a hang. (`tasks.md` I2)
- [ ] `boabotctl secret set/get/delete` exercised for real on each of macOS, Windows, and Linux. (`tasks.md` C5, I2)
- [ ] Full three-OS CI matrix (`go build`/`go vet`/`golangci-lint run`/`go test -race ./...`, coverage ≥90%) actually green on macOS, Windows, and Linux runners — this job runs the suite on its own development platform only; the multi-OS CI matrix itself (`.github/workflows/` today is Linux-only) is infrastructure setup, not something a single dev-flow run can execute. (`tasks.md` I4)

## Edge Cases & Solutions

[To be filled in during implementation.]

## Deviations from Plan

[To be filled in during implementation.]

## Lessons Learned

[To be filled in during implementation.]
