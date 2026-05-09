# Implementation Plan: Bot Capability Access — Review Fixes

## Ordering Principles

1. P0 findings first (they block merge).
2. P1 findings ordered by risk and test complexity.
3. P2 findings last, grouped by file to minimise context switching.
4. Each finding follows the TDD cycle: write failing test → implement fix → verify.

---

## Phase 1: P0 Fixes

### Fix 1 — REQ-001/REQ-009: Validate `work_dir` in `callCLITool`

**Risk:** High — security gap. Fixes two findings (FR-001, FR-009) in a single change.

**Steps:**
1. Write failing test: call `callCLITool` with `work_dir` outside `allowedDirs`; assert `IsError: true`.
2. Write failing test: call `callCLITool` with empty `work_dir`; assert `IsError: true`.
3. In `callCLITool`, replace the bare string extraction at line 519 with `c.resolvePath(args, "work_dir")`.
4. Run tests; both must pass.
5. Verify no regression with a test that provides a valid `work_dir` inside `allowedDirs`.

---

## Phase 2: P1 Fixes

### Fix 2 — REQ-002: Correct `product-details.md` for `run_openai_codex`

**Risk:** Low (docs only). No test required; verify by visual inspection.

**Steps:**
1. Open `boabot/docs/product-details.md`, find the CLI Agent Tools table.
2. Change binary from `codex` to `openai-codex` for `run_openai_codex`.
3. Remove "Alias for `run_codex`" language; replace with a distinct description referencing `--full-auto`.

### Fix 3 — REQ-003: Honour caller context in `ListTools`

**Risk:** Low. Behaviour change is strictly more correct.

**Steps:**
1. Write failing test: pass a pre-cancelled context to `ListTools`; assert the mock `pluginStore.List` was called with that context (check `ctx.Err()` inside the mock).
2. Change `ListTools(_ context.Context)` to `ListTools(ctx context.Context)`.
3. Change `c.pluginStore.List(context.Background())` to `c.pluginStore.List(ctx)`.
4. Run tests; the new test must pass.

### Fix 4 — REQ-004: Executable bit check on plugin entrypoint

**Risk:** Medium — changes subprocess dispatch path.

**Steps:**
1. Write failing test: create a temp file without execute bit; call `callPluginTool`; assert `IsError: true` with "not executable" in message.
2. After the `os.Stat` existence check, add: `if info.Mode()&0o100 == 0 { return errResult(...) }`.
3. Run tests; must pass.

### Fix 5 — REQ-005: Race test reads `resolvedPluginStore` from goroutines

**Risk:** Medium — requires export_test.go or struct access in the fake.

**Steps:**
1. Expose `resolvedPluginStore` via `export_test.go` if not already exported (e.g. `func (tm *TeamManager) ResolvedPluginStore() domain.PluginStore`).
2. Update `TestTeamManager_PluginStorePreResolved` fake `botRunner` to read `tm.ResolvedPluginStore()` inside the goroutine body.
3. Run `go test -race ./internal/application/team/...`; must pass with `-race`.

### Fix 6 — REQ-006: Test context cancellation propagates through `callCLITool`

**Risk:** Medium — requires mock runner that blocks on `ctx.Done()`.

**Steps:**
1. Add a `RunFn` variant to the mock `CLIAgentRunner` (or extend the existing one) that blocks until `ctx.Done()`.
2. Write test: create a context with `cancel`; pass it to `client.CallTool(ctx, "run_claude_code", ...)`; cancel the context after starting; assert `CallTool` returns within a short timeout and returns an error.
3. Run the test; must pass.

---

## Phase 3: P2 Fixes

### Fix 7 — REQ-007: Test `drainStdin` context-cancel-while-forwarding

**Steps:**
1. Write test in `runner_test.go`: subprocess is a script that reads stdin indefinitely (`cat`); cancel context; assert `Run` returns within 5 seconds with a context cancellation error.
2. Run test; must pass.

### Fix 8 — REQ-008: Clarify `isPluginJSONEntrypoint` comment

**Steps:**
1. Update the comment in `isPluginJSONEntrypoint` to state exactly that only the base name `plugin.json` is matched, not any filename ending in `plugin.json`.

### Fix 9 — REQ-010: Align `product-details.md` table (broader than REQ-002)

**Steps:**
1. Extend the REQ-002 fix to also add `--full-auto` flag detail for `run_openai_codex` and ensure all four CLI tools have distinct, accurate descriptions.

### Fix 10 — REQ-011: Document single-threaded access assumption for `progressFn`

**Steps:**
1. Add a comment to the `progressFn` field (line 36 in `client.go`) and/or in `callCLITool` noting sequential access invariant.

### Fix 11 — REQ-012: Test stderr in runner error on non-zero exit

**Steps:**
1. Write test in `runner_test.go`: use a script that writes to stderr and exits non-zero; assert the returned error contains the expected stderr text.

### Fix 12 — REQ-013: Check executable bit on absolute-path binaries in `resolveBinary`

**Steps:**
1. Write failing test: create a non-executable temp file; call `resolveBinary` with it as an absolute path; assert `(_, false)`.
2. In `resolveBinary`, after `os.Stat(bin)`, check `info.Mode()&0o100 == 0` and return `("", false)` if not executable.
3. Write a companion test verifying the binary is excluded from `ListTools`.

### Fix 13 — REQ-014: Verify archived spec status.md

**Steps:**
1. Read `specs/archive/260509-bot-capability-access/status.md`; verify all Phase 5 and Phase 6 tasks show complete.
2. Update any incomplete entries if found.
