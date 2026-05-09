# Status: Bot Capability Access — Review Fixes

## Phase 0: Research — ✅ Complete
## Phase 1: Specification — ✅ Complete
## Phase 2: Data Modeling — ✅ Complete
## Phase 3: Architecture — ✅ Complete
## Phase 4: Task Breakdown — ✅ Complete
## Phase 5: Implementation — 🔄 In Progress

### P0 Findings
- [x] FR-001 (REQ-001/REQ-009): `callCLITool` `work_dir` sandbox validation — fixed; `resolvePath` now called before subprocess launch
- [x] FR-009 (REQ-009): Empty `work_dir` error — satisfied by FR-001 fix (resolvePath rejects empty strings)

### P1 Findings
- [x] FR-002 (REQ-002): `product-details.md` `run_openai_codex` binary name — fixed binary to `openai-codex`, removed alias description
- [x] FR-003 (REQ-003): `ListTools` must honour caller context — fixed `_ context.Context` → `ctx`, passed to `pluginStore.List`
- [x] FR-004 (REQ-004): Executable bit check on plugin entrypoint — added `info.Mode()&0o100 == 0` check after stat
- [x] FR-005 (REQ-005): Race test reads `resolvedPluginStore` from goroutines — added accessor in `export_test.go`, reads from goroutine body
- [x] FR-006 (REQ-006): Context cancellation test for `callCLITool` — `TestCallCLITool_ContextCancelledDuringRun` added

### P2 Findings
- [x] FR-007 (REQ-007): `drainStdin` context-cancel-while-forwarding test — `TestSubprocessRunner_ContextCancelledWhileForwardingStdin` added
- [x] FR-008 (REQ-008): `isPluginJSONEntrypoint` comment clarity — updated to exact-base-name-match comment
- [x] FR-010 (REQ-010): `product-details.md` CLI Agent Tools table alignment — fixed in FR-002 fix (distinct binary names and descriptions for all four tools)
- [x] FR-011 (REQ-011): `progressFn` single-threaded access comment — added to struct field
- [x] FR-012 (REQ-012): Stderr-in-error test gap — `TestSubprocessRunner_NonZeroExitIncludesStderr` added
- [x] FR-013 (REQ-013): Executable bit check on absolute-path binaries in `resolveBinary` — added `info.Mode()&0o100 == 0` check
- [x] FR-014 (REQ-014): Verified archived spec status.md shows 100% completion — all phases marked complete

## Phase 6: Completion — ✅ Complete

All 14 findings addressed. P0: 1 finding fixed. P1: 5 findings fixed. P2: 7 findings fixed (FR-009 satisfied by FR-001 fix, FR-010 satisfied by FR-002 fix).

Final state:
- `go fmt ./...` — clean
- `go vet ./...` — clean
- `golangci-lint run` — 0 issues
- `go test -race ./...` — all pass
- domain/application coverage: not reduced (application/team was pre-existing at 76.1%)
