# Plugin Registry — Getting Started

This guide walks through everything you need to start using the BaoBot plugin system: what plugins are, where they come from, and how to install, approve, and manage them.

## What Plugins Are

A plugin is a versioned capability package that adds MCP tools to BaoBot without requiring a code change or process restart. Each plugin is a `.tar.gz` archive containing:

- A `plugin.yaml` manifest describing the plugin and declaring the tools it provides.
- An entrypoint script or binary that BaoBot calls when a bot invokes one of the plugin's tools.
- Any supporting files the entrypoint needs.

When a plugin is active, its tools appear in every bot's `ListTools` response alongside the built-in harness tools. Tools are dispatched to the plugin's entrypoint as a subprocess; arguments are passed as JSON on stdin and results are read from stdout.

## Default Registry

The default configuration ships with `stainedhead/shared-plugins` as a trusted first-party registry. This registry is listed under `orchestrator.plugins.registries` in your `config.yaml`:

```yaml
orchestrator:
  plugins:
    install_dir: ./plugins
    registries:
      - name: shared-plugins
        url: https://raw.githubusercontent.com/stainedhead/shared-plugins/main
        trusted: true
```

Because this registry is `trusted: true`, plugins installed from it activate immediately after checksum verification — no admin approval step is needed.

## Installing a Plugin from the Admin UI

1. Open the BaoBot admin UI and navigate to the **Plugins & Skills** tab.
2. Click **Browse Registry** next to the registry you want to browse.
3. Find the plugin you want to install and click **Install**.
4. For trusted registries, the plugin status changes to `active` and its tools are immediately available to bots.
5. For untrusted registries, the plugin status is `staged` (see [Approving a Staged Plugin](#approving-a-staged-plugin-from-an-untrusted-registry) below).

## Installing a Plugin via boabotctl

```bash
boabotctl plugin install my-plugin
```

By default this installs the latest version from the first registry that lists the plugin. You can pin the version or registry:

```bash
boabotctl plugin install my-plugin --version 1.2.0
boabotctl plugin install my-plugin --registry shared-plugins
```

## Adding a New Registry

From the admin UI, go to the **Plugins & Skills** tab and click **Add Registry**. Fill in:

- **Name** — a short identifier used in the CLI and API (e.g., `my-org-plugins`).
- **URL** — the HTTPS base URL of the registry index (e.g., `https://example.com/plugins`). HTTP URLs are rejected.
- **Trusted** — check this only for registries you control or fully trust. Trusted registry plugins activate without an approval step.

The registry index is fetched immediately when you add it. If the fetch fails (invalid URL, unreachable host, malformed JSON), the registry is not saved and an error is returned.

Runtime-added registries are persisted to `<install_dir>/registries.json` and survive process restarts.

## Plugin Trust Levels

| Level | When to use | Effect |
|---|---|---|
| **Trusted** | First-party registries you control | Plugins activate immediately after SHA-256 checksum verification |
| **Untrusted** | Third-party or community registries | Plugins land in `staged` status; an admin must approve before tools are visible to bots |

All plugins — trusted or not — go through the same checksum verification, zip-slip protection, and size limits during installation.

## Approving a Staged Plugin from an Untrusted Registry

When a plugin from an untrusted registry is installed, it appears in the admin UI's **Staged** section with status `staged`.

To approve:
1. In the admin UI, click the plugin name to open the detail panel.
2. Review the manifest: tools, permissions, entrypoint, and SHA-256 checksum.
3. Click **Approve**.

The plugin status changes to `active` and its tools appear in `ListTools` on the next bot call.

To reject a staged plugin, click **Reject**. The plugin files are removed and the plugin record is deleted.

## Reloading a Plugin Without Restarting

If you update a plugin's `plugin.yaml` on disk (for example, to change a tool description), you can reload it without restarting the boabot process:

From the admin UI, open the plugin detail panel and click **Reload**.

From the CLI:

```bash
boabotctl plugin reload my-plugin
```

Reload re-reads `plugin.yaml` from disk and updates the in-memory manifest. If the entrypoint file no longer exists at the declared path, the plugin is moved to `disabled` status and an error is returned.

Changes are visible to bots on the next `ListTools` call — no process restart required.

## Viewing Installed Plugins

In the admin UI, the **Plugins & Skills** tab lists all installed plugins with their name, version, registry, status, and installation date.

From the CLI:

```bash
boabotctl plugin list
```

For detailed information about a specific plugin:

```bash
boabotctl plugin info my-plugin
```
