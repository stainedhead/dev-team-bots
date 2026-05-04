# CLAUDE.md — boabotctl

Operator CLI module. See root `CLAUDE.md` for repo-wide rules. This file adds boabotctl-specific guidance.

## What This Module Does

A kubectl-style CLI binary. Users run `baobotctl <command>` to interact with the orchestrator REST API. Communicates via JWT-authenticated HTTPS through the ALB.

## Critical Rules

- **TDD always.** No production code without a failing test.
- **Command handlers are thin.** Parse flags → call client → print result. No logic in handlers.
- **The client interface is the seam.** All handler tests use a mock client — never make real HTTP calls in unit tests.
- **JWT never in config.** Store credentials only in `~/.baobotctl/credentials` at mode 0600.

## Key Commands

```bash
# Build
go build -o bin/boabotctl ./cmd/boabotctl

# Test with coverage
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Lint
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
