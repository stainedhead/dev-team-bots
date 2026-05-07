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

## baobotctl skills  (Admin only)

```
baobotctl skills push <path> --bot <name>     Upload a skill package to staging
baobotctl skills list [--bot <name>]          List staged and active skills
baobotctl skills approve <skill-id>           Promote a staged skill to active
baobotctl skills reject <skill-id>            Reject and discard a staged skill
baobotctl skills revoke <skill-id>            Remove an active skill
```

`push` uploads a skill directory (containing `SKILL.md` and optional scripts) to the staging prefix of the named bot's private S3 bucket. An Admin must then run `approve` to make the skill available to the bot.

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

## baobotctl plugin  (Admin only)

Manage installed plugins. All `plugin` subcommands communicate with the orchestrator REST API.

```
baobotctl plugin list
baobotctl plugin info <name>
baobotctl plugin install <name> [--version <v>] [--registry <r>]
baobotctl plugin remove <name>
baobotctl plugin reload <name>
```

### plugin list

List all installed plugins as a table.

```
baobotctl plugin list
```

Example output:

```
NAME            VERSION  REGISTRY        STATUS   INSTALLED
github-tools    1.2.0    shared-plugins  active   2026-05-07
data-fetcher    0.9.1    community       staged   2026-05-06
old-util        0.5.0    —               disabled 2026-04-01
```

Columns:

| Column | Description |
|---|---|
| NAME | Plugin name |
| VERSION | Installed version |
| REGISTRY | Registry the plugin was installed from (`—` if unknown) |
| STATUS | Current lifecycle status (`active`, `staged`, `disabled`, etc.) |
| INSTALLED | Installation date (YYYY-MM-DD) |

### plugin info \<name\>

Print full manifest detail for an installed plugin.

```
baobotctl plugin info github-tools
```

Example output:

```
Name:        github-tools
Version:     1.2.0
Status:      active
Registry:    shared-plugins
Installed:   2026-05-07T12:00:00Z
Entrypoint:  run.sh
Tools:
  - github_list_prs: List open pull requests for a repository
  - github_get_pr: Get details for a specific pull request
Permissions:
  network: api.github.com
  env_vars: GITHUB_TOKEN
```

If the plugin is not found, the command exits with a non-zero code and prints:

```
plugin "github-tools" not found
```

### plugin install \<name\>

Install a plugin from a registry. By default, installs the latest version from the first registry that lists the plugin.

```
baobotctl plugin install github-tools
baobotctl plugin install github-tools --version 1.1.0
baobotctl plugin install github-tools --registry shared-plugins
```

**Flags:**

| Flag | Description |
|---|---|
| `--version <v>` | Pin to a specific version (default: latest) |
| `--registry <r>` | Registry name to install from (default: first matching registry) |

Example output:

```
Plugin "github-tools" installation initiated (status: active, id: 550e8400-e29b-41d4-a716-446655440000)
```

If the plugin comes from an untrusted registry, the status will be `staged` rather than `active`. Use the admin UI to approve it.

### plugin remove \<name\>

Remove an installed plugin. The plugin directory is deleted and its tools are no longer available.

```
baobotctl plugin remove github-tools
```

Example output:

```
Plugin "github-tools" removed
```

### plugin reload \<name\>

Reload a plugin's manifest from disk without restarting the boabot process. Use this after manually editing `plugin.yaml` on disk.

```
baobotctl plugin reload github-tools
```

Example output:

```
Plugin "github-tools" reloaded
```

If the entrypoint file is missing after reload, the plugin is moved to `disabled` status and an error is returned.
