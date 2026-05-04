# AGENTS.md — boabotctl

The BaoBot operator CLI. A kubectl-style command-line tool for human operators to manage the team.

## Module Purpose

`boabotctl` is a standalone Go binary that communicates with the orchestrator's REST API via the ALB. It provides terminal access to everything the web UI offers: board management, team inspection, user administration, and profile management.

## Package Structure

```
cmd/boabotctl/      # main — wiring and cobra root command
internal/
  commands/         # one package per command group: board, team, user, profile, login
  client/           # HTTP client wrapping the orchestrator REST API
  auth/             # JWT storage and attachment (local credential store)
  config/           # endpoint config (~/.baobotctl/config.yaml)
  domain/           # request/response types, shared value objects
```

## Key Design Points

- `cobra` for CLI command structure.
- All orchestrator communication goes through `internal/client/` — no raw HTTP calls in command handlers.
- JWT is stored in `~/.baobotctl/credentials` (mode 0600). Never stored in the config file.
- Commands are thin: parse flags, call client method, format and print response. No business logic in command handlers.
- All client methods are behind an interface so command handlers can be unit tested with a mock client.

## Development Rules

- Follow TDD: failing test before any production code.
- 90%+ coverage target.
- Command handler tests use a mock client — never hit a real orchestrator in unit tests.
- Integration tests (tagged `//go:build integration`) test against a real orchestrator endpoint.

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

## Distribution

Released as pre-built binaries via GitHub Releases (macOS arm64, macOS amd64, Linux amd64). See CI pipeline in `.github/workflows/`.
