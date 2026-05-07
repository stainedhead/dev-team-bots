# Plugin Manifest Reference

Every plugin archive must contain a `plugin.yaml` at its root. This file declares the plugin's identity, the MCP tools it exposes, its permissions, and the SHA-256 checksum of the archive itself.

## Full Example

```yaml
name: github-tools
version: 1.2.0
description: "MCP tools for interacting with the GitHub API"
author: stainedhead
homepage: https://github.com/stainedhead/github-tools-plugin
license: MIT
tags:
  - github
  - vcs
min_runtime: 1.0.0
entrypoint: run.sh
checksums:
  sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
provides:
  tools:
    - name: github_list_prs
      description: "List open pull requests for a repository"
      inputSchema:
        type: object
        required: [owner, repo]
        properties:
          owner:
            type: string
            description: "GitHub organisation or user name"
          repo:
            type: string
            description: "Repository name"
    - name: github_get_pr
      description: "Get details for a specific pull request"
      inputSchema:
        type: object
        required: [owner, repo, number]
        properties:
          owner:
            type: string
          repo:
            type: string
          number:
            type: integer
            description: "Pull request number"
permissions:
  network:
    - api.github.com
  env_vars:
    - GITHUB_TOKEN
  filesystem: false
```

## Field Reference

### Top-Level Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Plugin name. Must match the archive filename and the directory name under `install_dir`. Only lowercase letters, digits, and hyphens. |
| `version` | string | yes | Semantic version string (e.g., `1.2.0`). |
| `description` | string | yes | One-line description shown in the admin UI and registry browser. |
| `author` | string | yes | Author name or organisation. |
| `homepage` | string | no | URL to the plugin's home page or source repository. |
| `license` | string | no | SPDX license identifier (e.g., `MIT`, `Apache-2.0`). |
| `tags` | list of strings | no | Arbitrary tags for filtering in the registry browser. |
| `min_runtime` | string | no | Minimum boabot version required. Advisory only — not enforced at install time. |
| `entrypoint` | string | yes | Path to the executable relative to the plugin directory root (e.g., `run.sh`). |
| `checksums` | map | yes | Must contain a `sha256` key whose value is the lowercase hex SHA-256 digest of the `.tar.gz` archive. |
| `provides` | object | yes | Declares the tools the plugin exposes. See [provides](#provides). |
| `permissions` | object | no | Declares what the plugin process is allowed to access. See [permissions](#permissions). |

### provides

```yaml
provides:
  tools:
    - name: <string>
      description: <string>
      inputSchema: <JSON Schema object>
```

| Field | Type | Required | Description |
|---|---|---|---|
| `tools` | list | yes | List of MCP tool definitions. An empty list is valid (a plugin that provides no tools installs successfully but contributes nothing to `ListTools`). |
| `tools[].name` | string | yes | Tool name as it appears in `ListTools`. Must be unique across all active plugins and built-in tools. Collisions with an already-active tool cause this tool to be silently skipped. |
| `tools[].description` | string | yes | Human-readable description injected into the model context. |
| `tools[].inputSchema` | object | yes | A JSON Schema object describing the tool's input. Must be a `type: object` schema with a `properties` map. |

### permissions

```yaml
permissions:
  network:
    - api.github.com
  env_vars:
    - GITHUB_TOKEN
  filesystem: false
```

| Field | Type | Default | Description |
|---|---|---|---|
| `network` | list of strings | `[]` | Hostnames the plugin entrypoint is permitted to contact. Currently advisory — used for display in the admin UI and `boabotctl plugin info`; runtime enforcement depends on the subprocess sandbox configuration. |
| `env_vars` | list of strings | `[]` | Environment variable names the entrypoint requires. Currently advisory. The runtime does not automatically inject these; the operator must ensure they are available to the subprocess via the ECS task definition or system environment. |
| `filesystem` | bool | `false` | Whether the entrypoint requires general filesystem access beyond the plugin directory. Currently advisory. |

## checksums

The `checksums.sha256` value is the SHA-256 hex digest of the `.tar.gz` archive as shipped to the downloader. This is verified by the installer before any files are extracted. A mismatch causes the install to abort with no files written.

To generate the checksum:

```bash
sha256sum my-plugin-1.0.0.tar.gz
# or on macOS:
shasum -a 256 my-plugin-1.0.0.tar.gz
```

The hex string (lowercase) goes into `checksums.sha256`. The manifest is embedded inside the archive and the archive is checksummed including the manifest, so the `plugin.yaml` and archive must be built together in a single step.

## Entrypoint Contract

The entrypoint is an executable file at the path declared in `entrypoint` (relative to the plugin directory). When a bot calls one of the plugin's tools:

1. The runtime resolves the full path: `<install_dir>/<plugin-name>/<entrypoint>`.
2. If the file does not exist, `CallTool` returns an error result immediately.
3. The entrypoint is executed with `exec.CommandContext` with a 30-second timeout.
4. Tool arguments are passed as a JSON object on stdin.
5. The entrypoint must write a single JSON object to stdout and exit 0 on success.
6. Non-zero exit or a write to stderr is reported as an error result; the plugin remains active.
7. The entrypoint must exit within 30 seconds or it is killed.

### stdout format

The entrypoint must write a JSON object matching `domain.MCPToolResult`:

```json
{
  "isError": false,
  "content": [
    {"type": "text", "text": "the result text"}
  ]
}
```

For an error result:

```json
{
  "isError": true,
  "content": [
    {"type": "text", "text": "description of the error"}
  ]
}
```

## Archive Structure

The archive must be a `.tar.gz` (gzip-compressed tar). Members must not escape the extraction directory — any path containing `../` traversal sequences causes the install to abort (zip-slip protection). The total uncompressed size must not exceed 50 MB.

Typical archive layout:

```
my-plugin-1.0.0.tar.gz
  plugin.yaml
  run.sh
  lib/
    helper.py
```

All files land directly under `<install_dir>/<plugin-name>/` after extraction.
