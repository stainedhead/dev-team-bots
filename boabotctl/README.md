# boabotctl — Operator CLI

The BaoBot operator CLI. A kubectl-style tool for managing the team, Kanban board, and users.

## Documentation

- [`docs/product-summary.md`](docs/product-summary.md) — what this tool does
- [`docs/product-details.md`](docs/product-details.md) — features and command reference
- [`docs/technical-details.md`](docs/technical-details.md) — architecture
- [`docs/architectural-decision-record.md`](docs/architectural-decision-record.md) — decisions specific to this module

## User Documentation

- [`user-docs/baobotctl.md`](user-docs/baobotctl.md) — full command reference
- [`user-docs/installation.md`](user-docs/installation.md) — installation guide

## Installation

Download the latest binary from [GitHub Releases](../../releases).

## Development

See [`AGENTS.md`](AGENTS.md) for package structure and [`CLAUDE.md`](CLAUDE.md) for Claude Code guidance.

### Build

```bash
go build -o bin/boabotctl ./cmd/boabotctl
```

### Test

```bash
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Lint

```bash
go fmt ./... && go vet ./... && golangci-lint run
```

## Quick Start

```bash
baobotctl config set endpoint https://<orchestrator-url>
baobotctl login
baobotctl team list
baobotctl board list
```

See [`user-docs/baobotctl.md`](user-docs/baobotctl.md) for the full command reference.
