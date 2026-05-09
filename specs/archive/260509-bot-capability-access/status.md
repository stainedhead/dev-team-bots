# Status: Bot Capability Access

## Phase 0: Research — ✅ Complete
## Phase 1: Specification — ✅ Complete
## Phase 2: Data Modeling — ✅ Complete
## Phase 3: Architecture — ✅ Complete
## Phase 4: Task Breakdown — ✅ Complete
## Phase 5: Implementation — ✅ Complete

### Tasks completed
- FR-1: Fix plugin store goroutine data race (pre-resolve in `Run()` before goroutine loop)
- FR-2: `read_skill` built-in MCP tool + JSON-entrypoint routing for Claude Code plugins
- FR-3: `domain.CLIAgentRunner` interface, mock, and `cliagent.SubprocessRunner` infrastructure adapter
- FR-4: `run_claude_code` MCP tool (stream-json parsing)
- FR-5/FR-6: `run_codex` / `run_openai_codex` MCP tools
- FR-7: `run_opencode` MCP tool
- FR-8: `CLIToolsConfig` / `CLIToolConfig` config additions
- FR-9: Complete tool schemas (no TBD placeholders)
- FR-10: Docs updated — `technical-details.md`, `architectural-decision-record.md` (ADR-B016, ADR-B017), `product-details.md`
- All tests pass with `go test -race ./...`; `golangci-lint run` clean; ≥90% domain/application coverage

## Phase 6: Completion — ✅ Complete
