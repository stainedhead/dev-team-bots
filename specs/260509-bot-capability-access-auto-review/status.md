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
- [ ] FR-002 (REQ-002): `product-details.md` `run_openai_codex` binary name
- [ ] FR-003 (REQ-003): `ListTools` must honour caller context
- [ ] FR-004 (REQ-004): Executable bit check on plugin entrypoint
- [ ] FR-005 (REQ-005): Race test reads `resolvedPluginStore` from goroutines
- [ ] FR-006 (REQ-006): Context cancellation test for `callCLITool`

### P2 Findings
- [ ] FR-007 (REQ-007): `drainStdin` context-cancel-while-forwarding test
- [ ] FR-008 (REQ-008): `isPluginJSONEntrypoint` comment clarity
- [ ] FR-010 (REQ-010): `product-details.md` CLI Agent Tools table alignment
- [ ] FR-011 (REQ-011): `progressFn` single-threaded access comment
- [ ] FR-012 (REQ-012): Stderr-in-error test gap
- [ ] FR-013 (REQ-013): Executable bit check on absolute-path binaries in `resolveBinary`
- [ ] FR-014 (REQ-014): Verify archived spec status.md shows 100% completion

## Phase 6: Completion — ⬜ Pending
