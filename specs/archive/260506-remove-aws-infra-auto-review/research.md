# Research — remove-aws-infra Auto Code Review Fixes

**Feature:** remove-aws-infra-auto-review
**Created:** 2026-05-06
**Source PRD:** specs/260506-remove-aws-infra-auto-review/remove-aws-infra-auto-review-PRD.md

## Research Questions

**RQ-001:** Does `gopkg.in/yaml.v3` `KnownFields(true)` propagate to nested structs, or only top-level fields?
→ Resolved: It applies recursively to all struct fields.

**RQ-002:** What error message format does `yaml.v3` produce for unknown fields with `KnownFields(true)`?
→ Resolved: `"yaml: unmarshal errors:\n  line N: field <name> not found in type <type>"` — the field name is always present.

**RQ-003:** Does `go-git` `PlainClone` with an invalid URL fail synchronously or with a timeout?
→ Resolved: Connection refused (`localhost:1`) fails immediately; suitable for unit tests.

**RQ-004:** Is it safe to call `os.Setenv` from `applyCredential` in a concurrent test environment?
→ Resolved: `t.Setenv` is used in tests (not `os.Setenv`), which is safe and restored automatically.

## References

- `gopkg.in/yaml.v3` docs: KnownFields method
- go-git PlainClone source
