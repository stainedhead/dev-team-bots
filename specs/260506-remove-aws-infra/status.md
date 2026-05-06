# Status: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Spec dir:** specs/260506-remove-aws-infra/

---

## Overall Progress

| Phase | Description | Status |
|---|---|---|
| 0 | Spec Creation | In Progress |
| 1 | Research | Not Started |
| 2 | Data Modeling | Not Started |
| 3 | Architecture | Not Started |
| 4 | Implementation — M1: Local Adapters | Complete |
| 5 | Implementation — M2: Anthropic Provider | Not Started |
| 6 | Implementation — M3: Vector Store + Embedder | Not Started |
| 7 | Implementation — M4: TeamManager + Wiring | Not Started |
| 8 | Implementation — M5: GitHub Backup | Not Started |
| 9 | Implementation — M6: Config + Credentials + Watchdog | Not Started |
| 10 | Implementation — M7: Remove AWS + CDK + Docs | Not Started |
| 11 | Tests & Quality | Not Started |

---

## Phase 0 Tasks

- [x] Spec directory created
- [x] PRD moved into spec directory
- [x] spec.md populated from PRD
- [x] All phase files initialized
- [x] Research questions identified and recorded in research.md (5 RQs)
- [x] Edge cases documented in spec.md
- [x] Open questions documented in spec.md (OQ-1, OQ-2, OQ-3)
- [x] Acceptance criteria expanded to cover all 22 FRs
- [ ] architecture.md draft reviewed

---

## Phase 4 Tasks (M1: Local Adapters)

- [x] `internal/infrastructure/local/queue/queue.go` — Router + Queue implementing domain.MessageQueue (coverage: 95.1%)
- [x] `internal/infrastructure/local/bus/bus.go` — Bus implementing domain.Broadcaster (coverage: 96.0%)
- [x] `internal/infrastructure/local/fs/fs.go` — FS implementing domain.MemoryStore (coverage: 93.5%)
- [x] `internal/infrastructure/local/budget/budget.go` — BudgetTracker implementing domain.BudgetTracker (coverage: 91.9%)
- [x] All packages pass `go test -race ./internal/infrastructure/local/...`
- [x] `go fmt`, `go vet`, `golangci-lint` all pass with 0 issues
- [x] No new external dependencies (stdlib only)

---

## Blockers

None.

---

## Recent Activity

- 2026-05-06 — Spec directory created from remove-aws-infra-PRD.md
- 2026-05-06 — M1 complete: four local adapters implemented with TDD, all ≥90% coverage
