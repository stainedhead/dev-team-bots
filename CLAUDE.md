# CLAUDE.md — BaoBot Dev Team Monorepo

## Project Context

This is the BaoBot Dev Team monorepo. Three Go modules: `boabot/` (agent runtime), `boabotctl/` (operator CLI), `boabot-team/` (bot personalities and CDK infrastructure). See `PRODUCT.md` for the full specification and `AGENTS.md` for coding standards.

## Non-Negotiable Rules

- **TDD always.** Write the failing test first. No exceptions, including bug fixes.
- **90%+ test coverage.** Do not reduce coverage.
- **Clean Architecture boundaries.** Domain never imports infrastructure.
- **`go fmt`, `go vet`, `golangci-lint` must all pass** before any commit.
- **Docs stay in sync.** Any behaviour change requires a docs update in the same PR.

## Working in This Repo

Each module is an independent Go module. When working on a module, `cd` into it first.

```bash
# Run all checks for a module
cd boabot
go fmt ./...
go vet ./...
golangci-lint run
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out   # check coverage %
```

## Builds

```bash
cd boabot && go build -o bin/boabot ./cmd/boabot
cd boabotctl && go build -o bin/boabotctl ./cmd/boabotctl
```

Output goes to `bin/` (gitignored). Config file is expected next to the binary at runtime.

## Adding a New Bot

1. Create `boabot-team/bots/<type>/` with `SOUL.md`, `AGENTS.md`, `config.yaml`.
2. Add the bot entry to `boabot-team/team.yaml` (set `enabled: false` until ready to deploy).
3. Update `boabot-team/docs/` and `boabot-team/README.md`.

## Documentation

- `docs/product-summary.md` — what this module does (high level).
- `docs/product-details.md` — full feature and behaviour description.
- `docs/technical-details.md` — architecture, key packages, data flows.
- `docs/architectural-decision-record.md` — append a new ADR entry for significant decisions.
- `user-docs/` — usage and adoption guides linked from `README.md`.

Update all relevant files when behaviour changes. The goal is documentation that is always true.

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

This must be done for every PR opened in this repo.

## Key Patterns

- Interfaces defined in `internal/domain/`. Implementations in `internal/infrastructure/`.
- Mocks in `mocks/` subdirectory alongside the interface package.
- `cmd/<binary>/main.go` does wiring only — no business logic.
- All external service calls go through interfaces — never call AWS SDK, Slack SDK, etc. directly from domain or application code.
