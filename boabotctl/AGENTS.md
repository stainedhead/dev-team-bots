# AGENTS.md — boabotctl

The BaoBot operator CLI. A kubectl-style command-line tool for human operators to manage the team.

## Module Purpose

`boabotctl` is a standalone Go binary that communicates with the orchestrator's REST API via the ALB. It provides terminal access to everything the web UI offers: board management, team inspection, user administration, profile management, and Agent Skills lifecycle management.

## Package Structure

```
cmd/boabotctl/      # main — wiring and cobra root command
internal/
  commands/         # one file per command group: board, team, skills, user, profile, login, config
  client/           # OrchestratorClient interface + HTTP implementation
  auth/             # JWT storage and attachment (local credential store)
  config/           # endpoint config (~/.baobotctl/config.yaml)
  domain/           # request/response types, shared value objects
```

## Key Design Points

- `cobra` for CLI command structure.
- All orchestrator communication goes through `internal/client/` — no raw HTTP calls in command handlers.
- JWT is stored in `~/.baobotctl/credentials` (mode 0600). Never stored in the config file.
- Commands are thin: parse flags, call client method, format and print response. No business logic in command handlers.
- All client methods are behind an `OrchestratorClient` interface so command handlers can be unit tested with a mock client.
- `skills push` writes directly to the bot's S3 staging prefix — it does not route through the orchestrator. All other skills operations (list, approve, reject, revoke) go through the REST API.

## Development Rules

- Follow TDD: failing test before any production code.
- 90%+ coverage target.
- Command handler tests use a mock client — never hit a real orchestrator in unit tests.
- Integration tests (tagged `//go:build integration`) test against a real orchestrator endpoint.

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

## Build

```bash
go build -o bin/boabotctl ./cmd/boabotctl
```

Config stored in `~/.baobotctl/config.yaml`. Credentials in `~/.baobotctl/credentials`.

## Test

```bash
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Lint

```bash
go fmt ./... && go vet ./... && golangci-lint run
```

## Adding a New Command

1. Write a failing test for the command handler using a mock client.
2. Add the cobra command in `internal/commands/<group>.go`.
3. Add the corresponding method to the client interface in `internal/client/`.
4. Implement the method in the client.
5. Wire the command into the root in `cmd/boabotctl/main.go`.
6. Update `user-docs/baobotctl.md` with the new command.

## Docs to Update When Changing This Module

- `user-docs/baobotctl.md` — always, for any command change.
- `docs/product-details.md` — if CLI capability changes.
- `docs/technical-details.md` — if client architecture changes.

## Distribution

Released as pre-built binaries via GitHub Releases (macOS arm64, macOS amd64, Linux amd64). See CI pipeline in `.github/workflows/`.
