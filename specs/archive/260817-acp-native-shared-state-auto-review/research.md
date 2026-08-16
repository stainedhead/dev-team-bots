# Research: ACP/Native Shared-State Parity — Review Fixes

**Created:** 2026-08-17
**Source PRD:** [acp-native-shared-state-auto-review-PRD.md](./acp-native-shared-state-auto-review-PRD.md)

## Research Questions

1. **FR-R2:** What's the exact malformed-marker code path in `EnsureOwner`? (Already located during review: `sharedstate.go`'s `os.ReadFile`/`json.Unmarshal` fallthrough — `if readErr == nil { ... }` block, no `else` branch logs anything for the unmarshal-failure/empty-owner sub-case today.)
2. **FR-R1:** Which file is the right place to record the deferred-verification note — `implementation-notes.md` under `specs/archive/260816-acp-native-shared-state/` (already archived) is correct per the review PRD's own guidance; confirm editing an archived spec's file is acceptable practice (it is — archiving moves a directory, it doesn't freeze its contents against later amendment).

## Findings

Both questions resolved directly from reading the existing code/archive during the review itself — no further research needed before implementation.

## References

- `acp-native-shared-state-auto-review-PRD.md` (this directory)
- `specs/archive/260816-acp-native-shared-state/` (reviewed feature)
