# Architecture: ACP/Native Shared-State Parity — Review Fixes

**Created:** 2026-08-17
**Status:** Draft

## Architecture Overview

No architectural change. FR-R2 adds one conditional log statement inside `sharedstate.EnsureOwner`'s existing malformed-marker fallthrough branch. FR-R1 is a documentation edit.
