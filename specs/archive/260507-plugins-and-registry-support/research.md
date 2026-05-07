# Research — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07
**Source PRD:** [plugins-and-registry-support-prd.md](./plugins-and-registry-support-prd.md)

---

## Research Questions

**RQ-1 — Existing skill infrastructure:** How does the current `SkillRegistry` interface and local MCP client work? What is the `ListTools` / `CallTool` dispatch path for skills? Which files need to be modified vs. extended?

**RQ-2 — Archive security:** What is the correct Go pattern for zip-slip-safe tar extraction? How is the 50 MB cap enforced during streaming extraction without buffering the entire archive in memory?

**RQ-3 — Subprocess sandboxing:** How does the current skill entrypoint subprocess work? What env var injection and network restriction mechanism is in place that plugin entrypoints must use identically?

**RQ-4 — Config loading:** How does the current `config.go` load and validate the YAML config? Where does the new `orchestrator.plugins` block need to be added? What is the validation pattern for HTTPS-only URLs?

**RQ-5 — Registry index cache:** Where should the in-memory TTL cache live — in the RegistryClient infrastructure adapter, or in the application layer use case? What concurrency pattern is appropriate (sync.Map, RWMutex-guarded map)?

**RQ-6 — Tool name namespace at runtime:** How does the MCP client currently namespace built-in tools? Are tool names already globally unique across skills, or is collision possible today?

---

## Industry Standards

[TBD — to be populated during Phase 1 research]

---

## Existing Implementations

[TBD — read `boabot/internal/domain/skill.go`, `boabot/internal/infrastructure/local/mcp/client.go`, `boabot/internal/infrastructure/local/config/config.go` during Phase 1]

---

## API Documentation

- Go stdlib `archive/tar` — streaming tar extraction
- Go stdlib `compress/gzip` — gzip decompression
- `gopkg.in/yaml.v3` — YAML manifest parsing (already in go.mod)
- `log/slog` — structured audit logging (Go 1.21+)

---

## Best Practices

[TBD]

---

## Open Questions

All product-level open questions were resolved in the PRD review. Implementation-level questions are captured in RQ-1 through RQ-6 above.

---

## References

- [PRD Open Questions section](./plugins-and-registry-support-prd.md)
- Existing skill domain: `boabot/internal/domain/skill.go`
- Existing MCP client: `boabot/internal/infrastructure/local/mcp/client.go`
