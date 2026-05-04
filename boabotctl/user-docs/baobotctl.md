# baobotctl Command Reference

Full command reference for the BaoBot operator CLI.

## Global Flags

| Flag | Description |
|---|---|
| `--config <path>` | Path to config file (default: `~/.baobotctl/config.yaml`) |
| `--output json` | Output as JSON (default: human-readable table) |

## baobotctl config

```
baobotctl config set endpoint <url>   Set the orchestrator endpoint
baobotctl config get                   Show current configuration
```

## baobotctl login / logout

```
baobotctl login      Authenticate and store credentials locally
baobotctl logout     Clear stored credentials
```

On first login you will be prompted to change your password.

## baobotctl board

```
baobotctl board list                          List all work items
baobotctl board get <id>                      Get a single work item
baobotctl board create                        Create a new work item (interactive)
baobotctl board create --title <t> \
  [--description <d>] [--assign <bot>]        Create non-interactively
baobotctl board update <id> \
  [--title <t>] [--description <d>] \
  [--status <s>]                              Update a work item
baobotctl board assign <id> --to <bot>        Assign to a bot
baobotctl board close <id>                    Close a work item
```

Valid statuses: `backlog`, `in-progress`, `blocked`, `done`.

## baobotctl team

```
baobotctl team list             List all registered bots
baobotctl team get <name>       Get details for a specific bot
baobotctl team health           Overall team health summary
```

## baobotctl user  (Admin only)

```
baobotctl user list                        List all users
baobotctl user add                         Create a user (interactive)
baobotctl user add --username <u> \
  --role <admin|user>                      Create non-interactively
baobotctl user remove <username>           Remove a user
baobotctl user disable <username>          Disable a user account
baobotctl user set-pwd <username>          Reset a user's password
baobotctl user set-role <username> \
  --role <admin|user>                      Change a user's role
```

## baobotctl profile

```
baobotctl profile get               View own profile
baobotctl profile set-name <name>   Update display name
baobotctl profile set-pwd           Change own password (interactive)
```
