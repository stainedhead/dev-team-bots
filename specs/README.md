# specs/

Feature specifications for the BaoBot Dev Team monorepo.

## Naming Convention

```
YYMMDD-<feature-name>/
```

The `YYMMDD` prefix is the spec **creation** date (when `create-spec` was run), not the implementation date.

## Per-Spec Directory Contents

| File | Purpose |
|---|---|
| `spec.md` | Feature specification (populated from PRD) |
| `status.md` | Phase progress tracking — **update after every task** |
| `plan.md` | Implementation plan |
| `tasks.md` | Task breakdown |
| `research.md` | Research findings |
| `data-dictionary.md` | Data structures and schemas |
| `architecture.md` | Architecture and component design |
| `implementation-notes.md` | Decisions and gotchas |
| `<feature>-PRD.md` | Source PRD (moved here by `create-spec`) |

## Lifecycle

Active specs live in `specs/`. Completed specs (100% in `status.md`) are moved to `specs/archive/` by `archive-spec`.

## Key Rules

- Update `status.md` after every task or phase — mandatory.
- Never modify another spec's directory.
- Commit specs alongside code so progress is visible in git history.
