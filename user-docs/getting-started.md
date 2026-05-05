# Getting Started — BaoBot Dev Team

This guide covers connecting to a running BaoBot team as a human operator.

## Prerequisites

- A user account provisioned by your team Admin.
- `baobotctl` installed (see installation below).
- The orchestrator endpoint URL (provided by your Admin).

## Install baobotctl

Download the latest binary for your platform from the [GitHub Releases](../../releases) page.

**macOS (arm64):**
```bash
curl -L https://github.com/stainedhead/dev-team-bots/releases/latest/download/baobotctl-darwin-arm64 -o baobotctl
chmod +x baobotctl
mv baobotctl /usr/local/bin/
```

**macOS (amd64):**
```bash
curl -L https://github.com/stainedhead/dev-team-bots/releases/latest/download/baobotctl-darwin-amd64 -o baobotctl
chmod +x baobotctl
mv baobotctl /usr/local/bin/
```

**Linux (amd64):**
```bash
curl -L https://github.com/stainedhead/dev-team-bots/releases/latest/download/baobotctl-linux-amd64 -o baobotctl
chmod +x baobotctl
mv baobotctl /usr/local/bin/
```

## Configure the Endpoint

```bash
baobotctl config set endpoint https://<orchestrator-url>
```

## Log In

```bash
baobotctl login
```

You will be prompted for your username and the temporary password provided by your Admin. On first login you will be required to set a new password.

## Basic Commands

```bash
# View the team
baobotctl team list

# View the Kanban board
baobotctl board list

# Create a work item
baobotctl board create --title "Add feature X" --assign implementer

# View your profile
baobotctl profile get
```

See [`baobotctl/user-docs/baobotctl.md`](../baobotctl/user-docs/baobotctl.md) for the full command reference.

## Web UI

The Kanban board is also accessible via browser at the orchestrator URL. Log in with your username and password.

## For Admins

Admins have access to additional commands:

```bash
# Manage users
baobotctl user list
baobotctl user add --username <u> --role <admin|user>

# Manage Agent Skills
baobotctl skills list --bot <name>
baobotctl skills approve <skill-id>
baobotctl skills revoke <skill-id>
```

See [`baobotctl/user-docs/baobotctl.md`](../baobotctl/user-docs/baobotctl.md) for the full Admin command reference.
