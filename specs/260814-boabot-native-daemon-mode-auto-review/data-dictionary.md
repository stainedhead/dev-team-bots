# Data Dictionary: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14

No new types introduced by these fixes. One existing type gains documentation (not a code change) in the *original* feature's data dictionary, as part of FR-108:

- `domain.TaskPayload.Source` (added by the original feature, `internal/domain/message.go`) — needs a `data-dictionary.md` entry in `specs/archive/260814-boabot-native-daemon-mode/data-dictionary.md` matching its existing entries' level of detail (FR-108 item 4).

No entities, value objects, interfaces, or enumerations are added or changed by FR-101–FR-110.
