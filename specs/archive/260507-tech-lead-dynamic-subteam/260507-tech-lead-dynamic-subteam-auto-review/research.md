# Research: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07  
**Source PRD:** `specs/260507-tech-lead-dynamic-subteam-auto-review/tech-lead-dynamic-subteam-auto-review-PRD.md`

---

## Research Questions

**Q1:** Does `TeamManager.startBot` need any changes to support dynamically named instances, or does the existing parameter-based naming work as-is?

**Q2:** When `spawnTechLead` starts a new goroutine via `runBotWithRestart`, should it pre-register the instance name with `tm.router.Register()` before starting? What happens if a message arrives for the name before the router channel exists?

**Q3:** Is there a race condition between `markTerminated` (which doesn't delete from `m.bots`) and a concurrent `Spawn` call that passes the updated status check but then overwrites the entry?

**Q4:** Does the `fakeAuth` in `server_test.go` already return valid claims for any token, or does it check the token value? (Needed to confirm `TestPool_Endpoint_RequiresAuth` works with the existing test helper.)

**Q5:** Are there any callers of `TearDownAll` that pass a non-background context that might be affected by the `context.Background()` switch?

---

## Industry Standards

[TBD — not required for these targeted fixes]

---

## Existing Implementations

All findings resolved. See commit `27cdc9a` on `feat/tech-lead-dynamic-subteam`.

---

## API Documentation

[TBD]

---

## Best Practices

[TBD]

---

## Open Questions

All resolved — see PRD resolution notes.

---

## References

- `boabot/internal/infrastructure/http/server.go`
- `boabot/internal/infrastructure/local/orchestrator/board.go`
- `boabot/internal/application/subteam/manager.go`
- `boabot/internal/application/pool/pool.go`
- `boabot/internal/application/team/team_manager.go`
