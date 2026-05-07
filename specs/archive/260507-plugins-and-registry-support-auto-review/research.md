# Research — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07
**Source PRD:** [plugins-and-registry-support-auto-review-PRD.md](plugins-and-registry-support-auto-review-PRD.md)

---

## Research Questions

1. **FR-001:** What sentinel error type does the domain layer expose for plugin-not-found, and how is it currently surfaced in the HTTP layer? (Where is `ErrPluginNotFound` defined and what handlers currently call `writeInternalError` for it?)

2. **FR-002:** What is the current `Update` implementation in `store.go`? Does it use `installer.Extract` directly, and at what point could it leave the plugin directory in an inconsistent state?

3. **FR-002:** What filesystem rename semantics are available on the target OS (Linux/macOS)? Are `os.Rename` calls within the same filesystem guaranteed atomic?

4. **FR-003:** Where does `install.go` currently construct the `FetchManifest` and `FetchArchive` URLs, and does it unconditionally use `entry.LatestVersion` or `entry.ManifestURL`/`entry.DownloadURL` fields?

5. **FR-003:** What does the registry `RegistryEntry` type expose for version-specific URLs, and does the registry protocol support per-version URL patterns?

---

## Industry Standards

- HTTP 404 is the correct status for resource-not-found; HTTP 500 implies a server fault. Plugin-not-found is a client-supplied bad ID — correct status is 404.
- Atomic file replace: write to temp path, fsync, rename. `os.Rename` within one filesystem is atomic on POSIX.
- Version pinning in package managers: always resolve version before fetching, verify against known-versions list.

---

## Existing Implementations

[To be populated after reading store.go, server.go, and install.go]

---

## API Documentation

- `domain.ErrPluginNotFound` — sentinel error defined in domain layer
- `installer.Extract(installDir, id, manifest, archive)` — used by both install and update paths
- `os.Rename` — atomic within same filesystem on Linux/macOS

---

## Best Practices

- Use `errors.Is(err, domain.ErrPluginNotFound)` in HTTP handlers to distinguish 404 from 500.
- For atomic update: always extract to a temp path first, never overwrite in-place.
- For version pinning: validate version exists in `entry.Versions` slice before constructing URLs.

---

## Open Questions

None. All findings have clear resolutions per the review PRD.

---

## References

- Review PRD: `specs/260507-plugins-and-registry-support-auto-review/plugins-and-registry-support-auto-review-PRD.md`
- Original implementation spec: `specs/archive/260507-plugins-and-registry-support/`
