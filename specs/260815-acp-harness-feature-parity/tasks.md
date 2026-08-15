# Tasks: ACP Harness Feature Parity

**Created:** 2026-08-15
**Status:** Planning

## Progress Summary

0/6 tasks complete.

## Phase 1 — Independent, smallest first

### P1.1 — `isConversationalSource` recognizes `"acp"`

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-401. TDD: failing test first (ACP-sourced task with `chat_provider` configured currently ignores it), then extend `isConversationalSource` (`execute_task.go:20-22`) to match `"acp"` alongside `"chat"`/`"buzz"`. Test asserts provider selection, not just that the string comparison changed.

## Phase 2 — Board store (FR-402/403)

### P2.1 — ACP mode constructs its own board store

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-402. TDD: test asserting `buildACPAgent` constructs a board store at `<memPath>/board.json` when `cfg.Bot.Type != "tech-lead"`, and does not when it equals `"tech-lead"`. Construction failure (bad path/permissions) logged clearly, does not prevent ACP mode from starting (NFR-Reliability).

### P2.2 — Wire `WithBoardStore` into ACP mode's MCP client

- **Depends on:** P2.1
- **Acceptance criteria:** per spec.md FR-403. End-to-end test: an ACP-sourced task successfully calls a board-completion tool when the board store is wired.

## Phase 3 — Plugin store (FR-404)

### P3.1 — ACP mode constructs its own plugin store, gated on install-dir

- **Depends on:** none (parallelizable with Phase 2)
- **Acceptance criteria:** per spec.md FR-404. TDD: test asserting plugin store construction happens only when `cfg.Orchestrator.Plugins.InstallDir != ""`, wired via `WithPluginStore`/`WithInstallDir`. Construction failure logged, non-fatal. End-to-end test: an ACP-sourced task successfully invokes a plugin-provided tool.

## Phase 4 — CLI tools (FR-405)

### P4.1 — Wire CLI runner (unconditional) and per-tool CLI tools

- **Depends on:** none (parallelizable with Phases 2-3)
- **Acceptance criteria:** per spec.md FR-405. TDD: test asserting `WithCLIRunner` is always present, and `WithCLITools`' per-tool availability matches `cfg.Orchestrator.CLITools.<tool>.Enabled` exactly. End-to-end test: an ACP-sourced task successfully invokes an enabled CLI tool.

## Phase 5 — Documentation (FR-406)

### P5.1 — Document the mid-task-question non-goal

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-406. `docs/architectural-decision-record.md` gains an entry citing the unstable ACP `elicitation` extension and `buzz-acp`'s lack of support for it, so a future contributor checks upstream status before attempting this. Docs-only, no code.
