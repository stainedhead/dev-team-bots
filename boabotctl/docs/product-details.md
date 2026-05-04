# Product Details — boabotctl

## Command Groups

### Session

| Command | Description |
|---|---|
| `baobotctl login` | Authenticate and store JWT locally |
| `baobotctl logout` | Clear stored credentials |

### Board

| Command | Description |
|---|---|
| `baobotctl board list` | List all work items |
| `baobotctl board get <id>` | Get a single work item |
| `baobotctl board create` | Create a new work item |
| `baobotctl board update <id>` | Update a work item |
| `baobotctl board assign <id> --to <bot>` | Assign a work item to a bot |
| `baobotctl board close <id>` | Close a work item |

### Team

| Command | Description |
|---|---|
| `baobotctl team list` | List all registered bots |
| `baobotctl team get <name>` | Get details for a specific bot |
| `baobotctl team health` | Overall team health summary |

### User (Admin only)

| Command | Description |
|---|---|
| `baobotctl user list` | List all users |
| `baobotctl user add` | Create a new user account |
| `baobotctl user remove <username>` | Remove a user |
| `baobotctl user disable <username>` | Disable a user account |
| `baobotctl user set-pwd <username>` | Reset a user's password |
| `baobotctl user set-role <username>` | Change a user's role (admin/user) |

### Profile

| Command | Description |
|---|---|
| `baobotctl profile get` | View own profile |
| `baobotctl profile set-name` | Update display name |
| `baobotctl profile set-pwd` | Change own password |

### Config

| Command | Description |
|---|---|
| `baobotctl config set endpoint <url>` | Set the orchestrator endpoint |
| `baobotctl config get` | View current configuration |

## Authentication Flow

1. Admin creates account — temporary password delivered out-of-band.
2. User runs `baobotctl login` — prompted for username and temporary password.
3. On first login, user is prompted to set a new password.
4. JWT stored in `~/.baobotctl/credentials` (mode 0600).
5. All subsequent commands attach the JWT automatically.
6. On expiry, `baobotctl login` is required again.
