# Technical Details — boabotctl

## Package Layout

```
cmd/boabotctl/
  main.go               # wiring — cobra root command, inject client

internal/
  commands/
    login.go            # login / logout
    board.go            # board subcommands
    team.go             # team subcommands
    user.go             # user subcommands (admin only)
    profile.go          # profile subcommands
    config.go           # config subcommands
  client/
    client.go           # OrchestratorClient interface
    http.go             # HTTP implementation of OrchestratorClient
  auth/
    store.go            # JWT read/write to ~/.baobotctl/credentials (mode 0600)
  config/
    config.go           # ~/.baobotctl/config.yaml (endpoint, etc.)
  domain/
    types.go            # request/response types shared across commands
```

## Key Interface

```go
type OrchestratorClient interface {
    Login(username, password string) (token string, mustChangePassword bool, err error)
    BoardList() ([]WorkItem, error)
    BoardCreate(req CreateWorkItemRequest) (WorkItem, error)
    // ... all API operations
}
```

Command handlers depend only on `OrchestratorClient`. The HTTP implementation is injected at startup. Tests inject a mock.

## Credential Storage

- Config: `~/.baobotctl/config.yaml` — endpoint URL and non-sensitive preferences.
- Credentials: `~/.baobotctl/credentials` — JWT, written at mode 0600. Never in config.

## Output Formatting

Commands default to human-readable table output. `--output json` flag available on all commands for scripting. Format logic is separate from command logic.
