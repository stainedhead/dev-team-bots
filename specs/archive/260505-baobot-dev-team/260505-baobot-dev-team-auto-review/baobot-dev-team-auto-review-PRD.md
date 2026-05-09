# BaoBot Dev Team — Auto-Review PRD

**Source implementation:** `feat/baobot-dev-team`
**Review date:** 2026-05-05
**Reviewer:** dev-flow automated code review (Step 5)

---

## Executive Summary

The BaoBot implementation is architecturally sound. Clean Architecture boundaries are respected throughout — domain interfaces are used everywhere in application and HTTP layers, and no AWS SDK calls appear outside the infrastructure layer. Test coverage is high across application and domain packages (94–100%). TDD discipline is evident in the structure of test files.

Two critical security defects were found that must not merge: password plaintext storage and missing old-password verification. Four warning-level gaps remain that reduce production robustness. All P0 findings have since been remediated in the same branch; the P1 and P2 findings remain open for the review-fix implementation phase.

**Overall verdict:** Request changes (P0 findings found and remediated; P1/P2 gaps remain).

---

## Non-Goals / Out of Scope

The following are explicitly excluded from this fix phase:

- No changes to existing domain interfaces or the domain model beyond what is required to introduce the `DynamoBudgetTrackerAdapter`.
- No architectural refactoring — the fix phase corrects named findings, not general code style or structure.
- No new bot types, workflow steps, or orchestrator features.
- No changes to CDK stacks or AWS infrastructure configuration.
- No changes to `boabotctl/` or `boabot-team/` — fixes are confined to the `boabot/` module.
- No remediation of coverage gaps in packages not named in FR-006 (`aws/dynamodb` and `otel` only).
- No migration or schema changes — the `users` table schema already exists via `db.Migrate`; `UserRepo` must use it as-is.

---

## Functional Requirements

### FR-001 — Password must never be stored as plaintext (P0)

**Finding:** `handleUserSetPassword` and `handleProfileSetPassword` both wrote the incoming password string directly to `domain.User.PasswordHash` via `UserStore.Update()`. This stored the plaintext password in the database.

**Acceptance criteria:**
- `handleUserSetPassword` calls `AuthProvider.SetPassword(ctx, username, password)` — never `UserStore.Update` with a raw password field.
- `handleProfileSetPassword` calls `AuthProvider.SetPassword(ctx, username, newPassword)` after verification — never `UserStore.Update`.
- `AuthProvider.SetPassword` hashes the password with bcrypt (cost ≥ 12) before persisting.
- A test asserts that `SetPassword` is called (not `UserStore.Update`) when the admin sets a user's password.
- A test asserts that `SetPassword` is called (not `UserStore.Update`) when a user changes their own password.
- No test passes a raw password string to any `UserStore` method.

**Status:** ✅ Fixed

---

### FR-002 — Old password must be verified before allowing self-service password change (P0)

**Finding:** `handleProfileSetPassword` accepted an `old_password` field in the request JSON but discarded it — the old password was never checked against the stored credential. Any authenticated user could change their password to anything without knowing their current password.

**Acceptance criteria:**
- `handleProfileSetPassword` calls `AuthProvider.VerifyPassword(ctx, subject, oldPassword)` before calling `SetPassword`.
- If `VerifyPassword` returns an error, the handler responds with HTTP 401 and does not proceed.
- `AuthProvider.VerifyPassword` compares the provided password against the bcrypt hash using `bcrypt.CompareHashAndPassword`.
- A test asserts that providing a wrong old password returns 401.
- A test asserts that providing an empty new password returns 400.

**Status:** ✅ Fixed

---

### FR-003 — HTMX must be loaded with Subresource Integrity (P1)

**Finding:** The Kanban web UI loads HTMX from `https://unpkg.com/htmx.org@1.9.12` with no `integrity` attribute. A CDN compromise or version substitution attack could inject arbitrary JavaScript into the UI without detection.

**Acceptance criteria:**
- The `<script>` tag loading HTMX includes a valid `integrity` attribute with the SHA-384 hash of the pinned version.
- The `<script>` tag includes `crossorigin="anonymous"`.
- The hash is the official SRI hash for htmx.org@1.9.12 (verifiable at srihash.org).
- Implementation must use TDD: write a test asserting the Kanban HTML contains an `integrity=` attribute before adding the hash.

**Status:** ⬜ Open

---

### FR-004 — domain.UserStore must have a PostgreSQL implementation (P1)

**Finding:** `domain.UserStore` is an interface defined in `internal/domain/` but has no concrete implementation in `internal/infrastructure/`. The HTTP server accepts it as a `Config` field but production wiring in `cmd/boabot/main.go` would not compile without a concrete adapter. The `LocalAuthProvider` reads the `users` table but the `UserStore` interface (CRUD for `domain.User` records) is unimplemented.

**Acceptance criteria:**
- A `UserRepo` struct is implemented in `internal/infrastructure/db/` implementing `domain.UserStore`.
- It supports `Create`, `Get`, `Update`, `Delete`, and `List` against the PostgreSQL `users` table.
- The `db.Migrate` function already creates the `users` table — the repo must use the same schema.
- Tests use `go-sqlmock` following the pattern in `db_test.go`. Coverage ≥ 90%.
- `cmd/boabot/main.go` wires `UserRepo` into the `httpserver.Config.Users` field.
- Implementation must use TDD.

**Status:** ⬜ Open

---

### FR-005 — domain.BudgetTracker must be bridgeable to DynamoDB BudgetTracker (P1)

**Finding:** The `domain.BudgetTracker` interface (`CheckAndRecordTokens`, `CheckAndRecordToolCall`, `Flush`) is different from the `dynamodb.BudgetTracker` struct (`CheckBudget`, `RecordSpend`, `DailySpend` — all parameterized by `botID`). `ExecuteTaskUseCase.WithBudgetTracker` accepts a `domain.BudgetTracker` but there is no adapter that wraps the DynamoDB implementation to satisfy this interface, so budget enforcement cannot be wired in production.

**Acceptance criteria:**
- An adapter `DynamoBudgetTrackerAdapter` (or similar) is implemented in `internal/infrastructure/aws/dynamodb/` that wraps the existing `BudgetTracker` struct and satisfies `domain.BudgetTracker`.
- The adapter maps `CheckAndRecordToolCall` to the DynamoDB `CheckBudget` call and `CheckAndRecordTokens` to `RecordSpend` using the bot's configured ID.
- The adapter tests verify correct delegation to the underlying DynamoDB tracker using the mock.
- `cmd/boabot/main.go` wires the adapter into `ExecuteTaskUseCase.WithBudgetTracker`.
- Implementation must use TDD.

**Status:** ⬜ Open

---

### FR-006 — Test coverage must be ≥ 90% across all packages with testable statements (P2)

**Finding:** Two infrastructure packages are below the 90% threshold: `internal/infrastructure/aws/dynamodb` at 86.5% and `internal/infrastructure/otel` at 85.7%.

**Acceptance criteria:**
- `aws/dynamodb` package test coverage is ≥ 90%.
- `otel` package test coverage is ≥ 90%.
- No existing package coverage is reduced.
- New tests must use TDD (failing test first, then implementation).

**Status:** ⬜ Open

---

## Open Questions

| # | Question | Owner | Resolution |
|---|---|---|---|
| OQ-1 | How does `DynamoBudgetTrackerAdapter` obtain the `botID`? Injected at construction time from config, or passed per call? | implementer | Recommend: construction-time injection from `config.yaml` `bot.id` field — keeps `domain.BudgetTracker` interface free of bot-specific parameters. |
| OQ-2 | Should `domain.BudgetTracker.Flush()` perform a DynamoDB write in the adapter, or is it a no-op? | implementer | The DynamoDB tracker writes on every `RecordSpend` call. `Flush()` can be a no-op in the adapter unless the DynamoDB client is buffered. Confirm before implementing. |
| OQ-3 | For FR-003 (HTMX SRI), is the `kanbanHTML` constant the right test target, or should it be externalised to a file for easier hash updates? | implementer | Recommend: keep the constant, but extract the SRI hash to a named constant at the top of `server.go` so it can be updated in one place and tested independently. |
| OQ-4 | For FR-004 (`UserRepo`), should `Delete` do a hard delete or soft delete (set `disabled=true`)? The `users` table has a `disabled` column already used by `LocalAuthProvider`. | implementer | Recommend: hard delete via `DELETE FROM users WHERE username = $1` — soft deletion is handled by the existing `DisableUser` path. Confirm with team before implementing. |

---

## Implementation Guidance

- Use TDD (Red → Green → Refactor) for every fix.
- Conduct a brief code and design review as each fix is completed before moving to the next.
- Clean Architecture must be maintained: no AWS SDK imports in domain or application packages.
- Use agent teammates and git worktrees for independent P1 workstreams (FR-003, FR-004, FR-005 can run in parallel).
- Update `<review-spec-dir>/status.md` after every task (mandatory).
- P0 items take priority, then P1, then P2.
- Commit and push as stable groups of fixes complete.
