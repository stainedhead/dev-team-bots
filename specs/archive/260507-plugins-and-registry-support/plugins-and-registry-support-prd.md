# PRD — Plugin Registry Support

**Status:** Reviewed  
**Author:** stainedhead  
**Module:** `boabot` (runtime + HTTP server), `boabotctl` (CLI — plugin subcommands)

---

## Problem

Skills today are manually uploaded zip archives that require admin approval before bots can use them. There is no discovery mechanism, no versioning, no sharing between teams, and no way to distribute capabilities from a central catalog. Each installation is a one-off upload. As the number of bots and capability packages grows, this model does not scale.

Teams need a way to browse, install, update, and remove named, versioned plugins from one or more hosted registries — with a trust model that allows first-party plugins to activate without friction and third-party plugins to go through an approval gate.

---

## Goals

- Define a standard plugin manifest format that any registry can serve.
- Define a simple, static-file-compatible registry protocol so a GitHub repository (or any HTTPS host) can act as a registry.
- Provide a unified "Plugins & Skills" admin UI that supports browsing, installing, updating, reloading, and removing both plugins and legacy skills.
- Support multiple registries simultaneously, each with an independent trust level.
- Default configuration points to `stainedhead/shared-plugins` as a trusted first-party registry.
- Preserve full backwards compatibility with the existing `SkillRegistry` and uploaded skills.

---

## Non-Goals

- A hosted registry service is not built as part of this feature. Registries are static HTTP catalogs served from GitHub, S3, or any HTTPS host.
- Plugins do not auto-update without explicit admin action.
- Plugin sandboxing beyond the existing subprocess isolation model is out of scope.
- Bot-initiated plugin installation is out of scope. Only admin actions install or update plugins.
- Private/authenticated registries are out of scope. Only anonymous HTTPS registries are supported.
- `min_runtime` version enforcement is out of scope; the field is advisory metadata only.
- Per-bot plugin scoping is out of scope; all active plugins are available to all bots.

---

## User Stories

| As… | I want… | So that… |
|---|---|---|
| Admin | to browse a catalog of available plugins | I can discover capabilities without leaving the UI |
| Admin | to install a plugin in one click | I can add capabilities quickly without a manual upload |
| Admin | to see which version is installed and whether an update is available | I can keep the team current |
| Admin | to update a single plugin or all plugins at once | I can apply fixes without re-uploading archives |
| Admin | to remove a plugin and have all bots stop using it immediately | I can revoke capabilities cleanly |
| Admin | to add a second registry URL | I can mix first-party and community plugins |
| Admin | to reload a plugin without restarting the runtime | I can pick up changes to a running bot |
| Bot | to receive new tools from an installed plugin automatically | capabilities appear without a process restart |

---

## Non-Functional Requirements

### Performance

- Registry index fetches must complete within 10 seconds or be aborted with an error.
- Plugin archive downloads must complete within 60 seconds or be aborted and cleaned up.
- Maximum plugin archive size on the wire: 20 MB compressed. Requests exceeding this are rejected before extraction begins.
- Maximum total extracted size per plugin: 50 MB. Extraction is aborted and the temp directory cleaned up if this limit is exceeded.
- The registry index is cached in memory with a 5-minute TTL. A "Reload" action in the UI forces a fresh fetch regardless of cache state.

### Reliability

- Plugin install is atomic: the archive is extracted to a temp directory inside `install_dir`, validated, then renamed to the final directory. Any failure at any stage deletes the temp directory. `install_dir` never contains a partially-extracted plugin.
- `install_dir` is a persistent directory. For cloud deployments (ECS), operators must mount it on a persistent volume — same contract as the `memory/` directory.
- Runtime-added registries are persisted in `install_dir/registries.json` and survive process restarts. Registries declared in `config.yaml` are always loaded on startup and take precedence.

### Security

- All registry fetches use HTTPS only; HTTP URLs are rejected at config validation time.
- Checksum verification: the `sha256` declared in `plugin.yaml` must match `sha256(downloaded .tar.gz)`. Mismatch aborts installation with no files written.
- Archive extraction enforces zip-slip protection: any member whose cleaned path resolves outside the target directory causes the install to abort and the temp directory to be deleted.
- Plugin entrypoint scripts run in the existing subprocess sandbox. Network access is limited to hosts declared in `permissions.network`. Env vars are injected from the credentials config; the subprocess cannot read env vars not declared in the manifest.
- Trusted registries skip the admin approval gate but still require checksum verification.

### Observability

- A structured `slog` log line is emitted for every plugin lifecycle event: install, approve, reject, disable, enable, update, reload, remove. Each log line includes: `plugin_name`, `version`, `registry`, `actor` (admin username from JWT claims), `status`, and `timestamp`.

---

## Plugin Manifest Schema

Every plugin must contain a `plugin.yaml` at the root of its archive or directory. This is the single source of truth for a plugin's identity, capabilities, and requirements.

```yaml
# plugin.yaml
# ── Identity ──────────────────────────────────────────────────────────────────
name: "github-pr-reviewer"         # lowercase, hyphen-separated, globally unique in registry
version: "1.2.0"                   # semver
description: "GitHub PR review and creation tools for bots"
author: "stainedhead"
homepage: "https://github.com/stainedhead/shared-plugins/tree/main/github-pr-reviewer"
license: "MIT"
tags: ["github", "pr", "code-review"]

# ── Runtime compatibility ─────────────────────────────────────────────────────
min_runtime: "1.0.0"               # minimum boabot runtime version

# ── Tools provided ────────────────────────────────────────────────────────────
# Each entry becomes an MCP tool available to bots.
provides:
  tools:
    - name: "create_pr"
      description: "Creates a GitHub pull request"
      input_schema:
        type: object
        properties:
          repo:   { type: string, description: "owner/repo" }
          title:  { type: string }
          body:   { type: string }
          head:   { type: string, description: "source branch" }
          base:   { type: string, description: "target branch", default: "main" }
        required: [repo, title, head]

    - name: "list_prs"
      description: "Lists open pull requests for a repository"
      input_schema:
        type: object
        properties:
          repo:  { type: string }
          state: { type: string, enum: [open, closed, all], default: open }
        required: [repo]

    - name: "request_review"
      description: "Requests a review on an open PR"
      input_schema:
        type: object
        properties:
          repo:      { type: string }
          pr_number: { type: integer }
          reviewers: { type: array, items: { type: string } }
        required: [repo, pr_number, reviewers]

# ── Permissions required ──────────────────────────────────────────────────────
permissions:
  network:
    - "api.github.com"
  env_vars:
    - "GITHUB_TOKEN"          # runtime injects these from credentials config
  filesystem: false           # no local filesystem access needed

# ── Entrypoint ────────────────────────────────────────────────────────────────
# Script executed by the MCP subprocess runner for each tool call.
# Receives the tool name and JSON-encoded args via stdin.
# Must write a JSON MCPToolResult to stdout.
entrypoint: "run.sh"

# ── Integrity ─────────────────────────────────────────────────────────────────
checksums:
  sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
```

### Schema rules

- `name` must match `^[a-z][a-z0-9-]{1,63}$`.
- `version` must be valid semver.
- `provides.tools[].name` must match `^[a-z][a-z0-9_]{1,63}$`. Tool names are globally namespaced at runtime — name collisions between plugins are rejected at install time.
- `entrypoint` must be a relative path within the plugin archive; must not traverse upward.
- `permissions.env_vars` values are declared only; the runtime resolves them from the credentials config. Plugins do not receive env var values they did not declare.
- `checksums.sha256` is the SHA-256 of the downloaded tarball, hex-encoded. Computed by the registry at publish time.

---

## Registry Protocol

A registry is an HTTPS endpoint that serves three static resources. Any file host (GitHub raw content, S3, Cloudflare R2) can act as a registry.

### Resource 1 — Registry index

```
GET {registry_base_url}/index.json
```

Response (200 OK, `Content-Type: application/json`):

```json
{
  "registry": "stainedhead/shared-plugins",
  "schema_version": "1",
  "generated_at": "2026-05-07T17:00:00Z",
  "plugins": [
    {
      "name": "github-pr-reviewer",
      "description": "GitHub PR review and creation tools for bots",
      "author": "stainedhead",
      "latest_version": "1.2.0",
      "tags": ["github", "pr", "code-review"],
      "versions": ["1.0.0", "1.1.0", "1.2.0"],
      "manifest_url": "{registry_base_url}/plugins/github-pr-reviewer/1.2.0/plugin.yaml",
      "download_url":  "{registry_base_url}/plugins/github-pr-reviewer/1.2.0/plugin.tar.gz"
    }
  ]
}
```

The index is the only resource the UI fetches during browsing. Manifest and archive fetches happen only when installing.

### Resource 2 — Plugin manifest

```
GET {registry_base_url}/plugins/{name}/{version}/plugin.yaml
```

Returns the `plugin.yaml` for that specific version. Used to validate the plugin before downloading the archive.

### Resource 3 — Plugin archive

```
GET {registry_base_url}/plugins/{name}/{version}/plugin.tar.gz
```

A gzip-compressed tar archive. The root must contain `plugin.yaml` and the `entrypoint` script. No archive may contain symlinks or absolute paths.

### GitHub-hosted registry layout

For a registry hosted on GitHub (the default case):

```
shared-plugins/
├── index.json
└── plugins/
    ├── github-pr-reviewer/
    │   ├── 1.0.0/
    │   │   ├── plugin.yaml
    │   │   └── plugin.tar.gz
    │   └── 1.2.0/
    │       ├── plugin.yaml
    │       └── plugin.tar.gz
    └── jira-integration/
        └── 2.0.0/
            ├── plugin.yaml
            └── plugin.tar.gz
```

`index.json` is regenerated by a GitHub Actions workflow whenever a new version directory is pushed.

### Registry base URL for stainedhead/shared-plugins

```
https://raw.githubusercontent.com/stainedhead/shared-plugins/main
```

---

## Multi-Registry Configuration

Registries are declared in `config.yaml` under an `orchestrator.plugins` key:

```yaml
orchestrator:
  plugins:
    install_dir: "./plugins"       # local directory where installed plugins live
    registries:
      - name: "official"
        url: "https://raw.githubusercontent.com/stainedhead/shared-plugins/main"
        trusted: true              # no admin approval required; checksum verification only
      - name: "community"
        url: "https://plugins.example.com"
        trusted: false             # admin approval required before activation
    auto_update: false             # never silently update; always require explicit action
```

- `trusted: true` registries bypass the approval gate; the plugin is installed and activated immediately after checksum verification passes.
- `trusted: false` registries place the plugin in `staged` status after download; an admin must approve it in the UI before bots can use it.
- Multiple registries with the same plugin name are disambiguated by `{registry_name}/{plugin_name}` in the UI. The first registry in list order wins at install time if no registry is explicitly specified.
- Registries can be added, reordered, or removed at runtime via the admin UI without restarting the process.

---

## Trust and Security Model

| Source | Install flow |
|---|---|
| Trusted registry (e.g. `official`) | Download → checksum verify → install → activate. No approval step. |
| Untrusted registry | Download → checksum verify → stage (status = `staged`). Admin reviews and approves or rejects. |
| Manual upload (existing skills) | Upload → stage. Admin reviews and approves or rejects. Unchanged from current behaviour. |

All installed plugin entrypoint scripts run in the existing subprocess sandbox (same as current skills). Network access at runtime is limited to the hosts declared in `permissions.network`. Env vars are injected from the credentials config; the subprocess cannot read arbitrary env vars.

Checksum verification: the `sha256` in `plugin.yaml` (fetched from the manifest URL) must match `sha256(downloaded .tar.gz)`. Mismatch aborts installation and logs a warning. For trusted registries this is the only gate.

---

## Plugin Lifecycle States

```
registry → downloading → checksum_fail (terminal)
                       → staged     (untrusted registry or manual upload)
                           → rejected  (terminal; admin rejected)
                           → active    (admin approved)
                       → active     (trusted registry, passes checksum)
active → disabled  (admin disabled; tools hidden from bots but files kept)
active → update_available (newer version detected in registry index)
update_available → active  (admin applied update; old version removed)
active/disabled → removed  (admin removed; files deleted)
```

A plugin in `active` status exposes its tools via the MCP client for the bots that have it in scope. Tools appear without a process restart (the MCP client re-reads the installed plugin directory on each `ListTools` call).

---

## Domain Model (new types)

```go
// Plugin represents an installed plugin, whether sourced from a registry or
// uploaded manually.
type Plugin struct {
    ID           string       `json:"id"`
    Name         string       `json:"name"`
    Version      string       `json:"version"`
    Description  string       `json:"description"`
    Author       string       `json:"author"`
    Registry     string       `json:"registry,omitempty"`   // "" for manual upload
    Status       PluginStatus `json:"status"`
    InstalledAt  time.Time    `json:"installed_at"`
    Manifest     PluginManifest `json:"manifest"`
}

type PluginStatus string
const (
    PluginStatusDownloading    PluginStatus = "downloading"
    PluginStatusStaged         PluginStatus = "staged"
    PluginStatusActive         PluginStatus = "active"
    PluginStatusDisabled       PluginStatus = "disabled"
    PluginStatusUpdateAvailable PluginStatus = "update_available"
    PluginStatusRejected       PluginStatus = "rejected"
)

type PluginManifest struct {
    Name        string            `yaml:"name"`
    Version     string            `yaml:"version"`
    Description string            `yaml:"description"`
    Author      string            `yaml:"author"`
    MinRuntime  string            `yaml:"min_runtime"`
    Tags        []string          `yaml:"tags"`
    Provides    PluginProvides    `yaml:"provides"`
    Permissions PluginPermissions `yaml:"permissions"`
    Entrypoint  string            `yaml:"entrypoint"`
    Checksums   map[string]string `yaml:"checksums"`
}

type PluginProvides struct {
    Tools []MCPTool `yaml:"tools"`
}

type PluginPermissions struct {
    Network   []string `yaml:"network"`
    EnvVars   []string `yaml:"env_vars"`
    Filesystem bool    `yaml:"filesystem"`
}

// PluginRegistry is the configuration for one remote registry.
type PluginRegistry struct {
    Name    string `yaml:"name"`
    URL     string `yaml:"url"`
    Trusted bool   `yaml:"trusted"`
}

// PluginStore manages the lifecycle of installed plugins.
type PluginStore interface {
    List(ctx context.Context) ([]Plugin, error)
    Get(ctx context.Context, id string) (Plugin, error)
    Install(ctx context.Context, manifest PluginManifest, archive []byte, registry string) (Plugin, error)
    Approve(ctx context.Context, id string) error
    Reject(ctx context.Context, id string) error
    Disable(ctx context.Context, id string) error
    Enable(ctx context.Context, id string) error
    Update(ctx context.Context, id string, manifest PluginManifest, archive []byte) error
    Remove(ctx context.Context, id string) error
}

// RegistryClient fetches index and plugin data from a remote registry.
type RegistryClient interface {
    FetchIndex(ctx context.Context, registryURL string) (RegistryIndex, error)
    FetchManifest(ctx context.Context, manifestURL string) (PluginManifest, error)
    FetchArchive(ctx context.Context, downloadURL string) ([]byte, error)
}

type RegistryIndex struct {
    Registry    string          `json:"registry"`
    GeneratedAt time.Time       `json:"generated_at"`
    Plugins     []RegistryEntry `json:"plugins"`
}

type RegistryEntry struct {
    Name          string   `json:"name"`
    Description   string   `json:"description"`
    Author        string   `json:"author"`
    LatestVersion string   `json:"latest_version"`
    Tags          []string `json:"tags"`
    Versions      []string `json:"versions"`
    ManifestURL   string   `json:"manifest_url"`
    DownloadURL   string   `json:"download_url"`
}
```

---

## REST API (new endpoints)

All endpoints require admin auth.

### Registries

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/registries` | List configured registries |
| `POST` | `/api/v1/registries` | Add a registry |
| `DELETE` | `/api/v1/registries/{name}` | Remove a registry |
| `GET` | `/api/v1/registries/{name}/index` | Fetch and return the live registry index (forces re-fetch, no cache) |

### Plugins

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/plugins` | List all installed plugins |
| `GET` | `/api/v1/plugins/{id}` | Get a specific plugin |
| `POST` | `/api/v1/plugins` | Install a plugin (`{"registry": "official", "name": "github-pr-reviewer", "version": "1.2.0"}`) |
| `POST` | `/api/v1/plugins/{id}/approve` | Approve a staged plugin |
| `POST` | `/api/v1/plugins/{id}/reject` | Reject a staged plugin |
| `POST` | `/api/v1/plugins/{id}/enable` | Re-enable a disabled plugin |
| `POST` | `/api/v1/plugins/{id}/disable` | Disable without removing |
| `POST` | `/api/v1/plugins/{id}/update` | Update to a newer version from the same registry |
| `POST` | `/api/v1/plugins/{id}/reload` | Reload the plugin from disk (picks up file changes without restart) |
| `DELETE` | `/api/v1/plugins/{id}` | Remove a plugin and delete its files |

### Skills (unchanged, surfaced together in UI)

Existing endpoints remain unchanged: `GET/POST /api/v1/skills`, `/approve`, `/reject`, `DELETE`.

---

## Admin UI — Plugins & Skills Tab

The current "Skills" tab is replaced by a unified "Plugins & Skills" tab. The page is split into three sections:

### 1. Registry Browser

- Registry selector dropdown (lists all configured registries; "All" shows merged view).
- Search/filter by name, tag, or author.
- Card grid showing: plugin name, description, author, latest version, tags, install status badge (Not installed / Installed vX.Y.Z / Update available / Staged).
- "Install" button on each card. For plugins already installed, shows "Installed" (disabled) or "Update to vX.Y.Z" (active).
- "Add Registry" button opens a modal: URL input, name input, trusted toggle. Saves to config and immediately fetches the index to validate.

### 2. Installed Plugins

Table with columns: Name, Version, Registry, Status, Installed, Actions.

Actions per row:
- **Active**: Disable | Update (if available) | Reload | Remove
- **Disabled**: Enable | Remove
- **Staged**: Approve | Reject | Remove
- **Update available**: Update | Dismiss

"Update All" button at top applies available updates to all active plugins from trusted registries.

### Plugin Detail Panel

Clicking any plugin name (in the Registry Browser or Installed Plugins table) opens a side panel or modal showing:

- Full metadata: name, version, description, author, tags, min_runtime, homepage.
- Status badge and installed-at timestamp.
- Tools list: name, description, and full input schema for each tool provided.
- Permissions summary: network hosts, required env vars, filesystem access flag.
- Checksum: sha256 of the installed archive.
- Registry source URL (or "Manual upload" for legacy skills).
- Action buttons matching the current status (same set as the Installed Plugins table row).

### 3. Uploaded Skills (Legacy)

Existing skills table. Unchanged behaviour. Label: "Manually uploaded skills".

"Upload Skill" button retained. Skills approved here appear in the same MCP tool pool as active plugins.

---

## MCP Client Integration

The local MCP client (`infrastructure/local/mcp`) currently returns 5 built-in tools. After this feature:

1. On `ListTools`, the client scans `install_dir/` for directories that contain an `active` plugin (reads a sidecar `status.json` per plugin dir).
2. Each active plugin's `provides.tools` entries are added to the tool list.
3. On `CallTool`, if the tool name belongs to an installed plugin, the client launches the plugin's entrypoint subprocess (same sandbox model as skills) with the tool name and JSON args on stdin, reads the JSON result from stdout.
4. Tool name collision detection: if two active plugins declare the same tool name, the second one is disabled at activation time and an admin warning is surfaced.

---

## boabotctl — Plugin Subcommands

`boabotctl` gains a `plugin` subcommand group. All commands call the boabot REST API; auth is handled via the existing token mechanism.

```
boabotctl plugin list                          # List all installed plugins and their status
boabotctl plugin info <name>                   # Show full detail for a single installed plugin
boabotctl plugin install <name>                # Install latest version from the first matching registry
  --registry <name>                            # Specify which registry to install from
  --version <version>                          # Pin to a specific version
boabotctl plugin remove <name>                 # Remove an installed plugin and delete its files
boabotctl plugin reload <name>                 # Reload plugin from disk without restarting the runtime
```

Output format for `plugin list`:

```
NAME                   VERSION   REGISTRY   STATUS             INSTALLED
github-pr-reviewer     1.2.0     official   active             2026-05-07
jira-integration       2.0.0     official   update_available   2026-04-01
custom-tool            1.0.0     —          staged             2026-05-06
```

Output format for `plugin info <name>`:

```
Name:           github-pr-reviewer
Version:        1.2.0
Registry:       official
Status:         active
Installed:      2026-05-07
Description:    GitHub PR review and creation tools for bots
Author:         stainedhead
Tags:           github, pr, code-review
Min Runtime:    1.0.0

Tools:
  create_pr         Creates a GitHub pull request
  list_prs          Lists open pull requests for a repository
  request_review    Requests a review on an open PR

Permissions:
  Network:      api.github.com
  Env vars:     GITHUB_TOKEN
  Filesystem:   false

Checksums:
  sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

All write operations (`install`, `remove`, `reload`) require admin credentials. `list` and `info` are read-only and require only a valid token.

---

## Skills Integration (Backwards Compatibility)

Existing skills continue to work. The `SkillRegistry` interface and all existing endpoints remain unchanged. Skills are surfaced in the UI under "Uploaded Skills" alongside the new plugins. No migration is required.

Future: a migration utility to convert an existing skill archive into a `plugin.yaml`-conformant package is a follow-on, not part of this feature.

---

## Implementation Phases

### Phase 1 — Domain and registry client
- `domain/plugin.go` — manifest schema types, Plugin, PluginStore, RegistryClient interfaces
- `infrastructure/local/plugin/` — PluginStore implementation (filesystem-backed, `install_dir/`)
- `infrastructure/http/registry_client.go` — HTTP RegistryClient (fetches index, manifest, archive)
- Unit tests for checksum verification, manifest validation, archive extraction

### Phase 2 — REST API
- New handler group in `infrastructure/http/server.go`
- Registries CRUD + live index fetch
- Plugins install, approve/reject, enable/disable, update, reload, remove
- Integration tests for the install and approval flows

### Phase 3 — MCP client integration
- Modify `infrastructure/local/mcp/client.go` — dynamic tool loading from `install_dir/`
- Subprocess invocation for plugin entrypoints
- Tool name collision detection
- Unit tests

### Phase 4 — Admin UI
- Replace Skills tab with unified Plugins & Skills tab
- Registry browser, installed plugins table, legacy skills section
- "Add Registry" modal, "Install", "Update All", per-plugin action buttons

### Phase 5 — Default registry wiring
- Add `stainedhead/shared-plugins` as the default trusted registry in the default `config.yaml` template
- Publish `index.json` bootstrap to `stainedhead/shared-plugins` on GitHub

### Phase 6 — boabotctl plugin subcommands
- `boabotctl plugin list` — calls `GET /api/v1/plugins`, renders table
- `boabotctl plugin info <name>` — calls `GET /api/v1/plugins/{id}`, renders full detail view
- `boabotctl plugin install <name>` — calls `POST /api/v1/plugins`; supports `--registry` and `--version` flags
- `boabotctl plugin remove <name>` — calls `DELETE /api/v1/plugins/{id}`
- `boabotctl plugin reload <name>` — calls `POST /api/v1/plugins/{id}/reload`
- Unit tests for CLI output formatting; integration tests against a running boabot instance

---

## Acceptance Criteria

- Admin can add a registry by URL and see its plugin catalog in the UI within one request.
- Installing a plugin from a trusted registry requires no extra approval step; tools are available to bots immediately after install.
- Installing a plugin from an untrusted registry lands in `staged` status; admin must approve before tools appear.
- Checksum mismatch aborts installation; no files are written to `install_dir`.
- Removing a plugin causes `ListTools` to stop returning its tools on the next call.
- All existing skill upload, approve, reject, revoke flows continue to work without change.
- Runtime-added registries survive a process restart: they are persisted to `install_dir/registries.json` and loaded on startup.
- A plugin reload picks up changes to its `entrypoint` without restarting the boabot process.
- Default config includes `stainedhead/shared-plugins` as the first trusted registry.
- Archive extraction enforces zip-slip protection: any archive member whose resolved path escapes the target directory causes the install to abort; no files from that archive are retained.
- Archives that would expand beyond 50 MB are aborted during extraction; the temp directory is deleted.
- Every plugin lifecycle event (install, approve, reject, disable, enable, update, reload, remove) produces a structured `slog` log line containing `plugin_name`, `version`, `registry`, `actor`, `status`, and `timestamp`.
- `boabotctl plugin info <name>` prints the full manifest detail, tool list, and permissions for an installed plugin.
- Clicking a plugin name in the UI opens a detail panel showing full manifest metadata, tools, permissions, and checksum.

---

## Open Questions (Resolved)

| # | Question | Decision |
|---|---|---|
| OQ-1 | Private/authenticated registries in scope? | **Out of scope.** Only anonymous HTTPS registries are supported. Private registries are a future feature. |
| OQ-2 | Where is the runtime registry list persisted? | **`install_dir/registries.json`** — a JSON sidecar in the persistent install directory. Registries declared in `config.yaml` always load on startup and take precedence over runtime additions. |
| OQ-3 | Global or per-bot plugin scoping? | **Global scope.** All active plugins are available to all bots. Per-bot scoping is listed as a non-goal. |
| OQ-4 | Registry index cache strategy? | **5-minute in-memory TTL.** A "Reload" action in the UI forces a fresh fetch regardless of cache state. |
| OQ-5 | `min_runtime` enforcement? | **Advisory only.** The field is metadata; the runtime logs a warning if the installed runtime version is below `min_runtime` but does not block installation. |
| OQ-6 | `boabotctl` plugin subcommands? | **`list`, `info`, `install`, `remove`, `reload`.** All call the boabot REST API. |
| OQ-7 | Atomic install strategy? | **Temp dir → rename.** Extract to a temp dir inside `install_dir`, validate (checksum + zip-slip), then rename to final directory. Any failure deletes the temp dir. |
| OQ-8 | Audit logging mechanism? | **Structured `slog`** — one log line per lifecycle event with standard fields (`plugin_name`, `version`, `registry`, `actor`, `status`, `timestamp`). |
| OQ-9 | Archive security constraints? | **Zip-slip protection + 50 MB extraction cap.** Any path traversal attempt aborts the install. Extraction is aborted if total extracted size exceeds 50 MB. |
