# Data Dictionary: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15

No new types introduced by these fixes. Possible behavioral change to existing types:

- `domain.DirectTask`/`ChatStore` write pattern for `DirectTaskSourceBuzz` (FR-301) — if option (a) is chosen, `chatMessageThreadID` starts returning a real value for Buzz-sourced tasks instead of `""`; no structural change to `ChatStore` itself.

No entities, value objects, interfaces, or enumerations are added by FR-301–FR-306.
