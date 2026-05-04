# Product Summary — boabotctl

`boabotctl` is the BaoBot operator CLI. A kubectl-style command-line tool that gives human operators terminal access to the orchestrator's REST API.

## What It Does

- Authenticates users against the orchestrator (JWT, username/password).
- Provides commands for Kanban board management, team inspection, user administration, and profile management.
- Stores credentials locally and attaches them transparently to all API requests.
- Distributed as a pre-built binary for macOS and Linux via GitHub Releases.

## Who Uses It

- **Admins** — manage users, inspect team health, manage the board.
- **Users** — interact with the Kanban board, update their own profile.
