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

## Manual Verification Required (not run in this job)

To be filled in as each integration-tagged test is written. Placeholder categories from the PRD's acceptance criteria:

- [ ] Live `buzz-relay` (Buzz's `docker-compose.yml`): NIP-42 auth, `kind:0` profile publish, `kind:9` pub/sub, reconnect-after-restart, relay-side membership revocation.
- [ ] macOS LaunchDaemon: System keychain reachability for the keystore provider (FR-041's core open question).
- [ ] Windows service: Credential Manager read/write under the service's own account identity (OQ-7).
- [ ] Linux systemd unit: `LoadCredentialEncrypted=` → `$CREDENTIALS_DIRECTORY` resolution with no session D-Bus available.

## Edge Cases & Solutions

[To be filled in during implementation.]

## Deviations from Plan

[To be filled in during implementation.]

## Lessons Learned

[To be filled in during implementation.]
