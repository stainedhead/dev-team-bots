# PRD: Paperclip Agent Factory Mobile (native iOS and Android, Kotlin Multiplatform)

**Created:** 2026-10-30
**Jira:** N/A
**Status:** Draft
**Owner:** J
**Related:** [Scope and design doc](https://claude.ai/artifact/QtSUpajPSwo5PQpfw6zAFx) · [Mobile UI canvas](https://claude.ai/artifact/9NMV8bCwZC91djgaRJD56v) · [INTENT.md](./INTENT.md) · [AGENTS.md](./AGENTS.md)

---

## Problem Statement

Paperclip is an open-source control plane for AI-agent "companies". Agents run on heartbeats, check out issues, spend budget, and escalate to humans through approvals, decisions and issue interactions. The only operator surface today is Paperclip's React web app. That web app is reachable only on the network where the factory runs (home LAN, tailnet, or a cloud host), is not built for a phone, and cannot alert anyone when an agent is blocked waiting on a human.

When the operator is away from their desk, the factory stalls. An agent that asks a question, a hire that needs approval, or a budget hard stop sits unresolved until someone opens a laptop. There is also no safe way to stop a runaway agent or run from a phone.

Paperclip Agent Factory Mobile is a pair of native mobile apps (iPhone and Android) that let a board operator **decide, monitor and intervene** in one or more Paperclip instances from anywhere, using the Paperclip REST API. All non-UI code (domain model, use cases, API client, persistence, secure storage access, sync and presentation state) is written once in Kotlin Multiplatform and shared by a SwiftUI iPhone app and a Jetpack Compose Android app.

## Goals

- **G1 Decide from anywhere.** Any pending approval, decision, agent question or budget incident can be resolved within two taps of opening a notification.
- **G2 Situational awareness.** At a glance: agents running, paused or in error; open and blocked work; month spend against budget; runs in progress.
- **G3 Safe intervention.** Pause or resume an agent, cancel a run, comment, change issue status, each confirmed and, where destructive, biometric-gated.
- **G4 Many instances.** Store multiple Paperclip instance configurations (home, cloud, test), each with its own URI and credentials, and switch between them and between companies inside each.
- **G5 Secure by default.** Credentials live only in the platform secret store (iOS Keychain, Android Keystore). TLS everywhere. `local_trusted` instances are refused.
- **G6 Build once.** At least 70% of production Kotlin/Swift lines live in shared KMP modules; platform UI layers contain no business rules, no HTTP and no persistence.
- **G7 Quality.** TDD throughout; 90% aggregate line coverage on shared `domain` and `application`; Clean Architecture boundaries enforced by automated tests; all toolchain gates green in CI.

## Non-Goals

- Re-implementing the Paperclip web app. Editing agent configuration, instruction bundles, skills, adapters, models, secrets, plugins, tool gateway, instance admin, invites or join requests stays in the web app.
- Hiring or terminating agents directly. Hiring happens only by approving a `hire_agent` approval.
- Document authoring beyond comments and replies.
- Running agents or hosting anything on the phone.
- Supporting Paperclip deployments in `local_trusted` mode.
- Tablet-optimised layouts, watchOS, Wear OS, widgets, Live Activities and voice intents (all v1.1 or later; see Release Plan).
- Building the push relay inside this PRD's MVP scope. Push is specified here (FR-080 to FR-086) so the client is ready, but the relay is delivered as Phase 3 and may move to its own PRD.

## Users and Roles

| Persona | Paperclip role | Can do in the app |
| --- | --- | --- |
| Owner | `owner` or instance admin | Everything in scope, including budget raise-and-resume and agent pause |
| Operator | `operator` | Decide, approve, comment, pause and resume agents, cancel runs |
| Viewer | `viewer` | Read-only: inbox (no actions), factory, work, agents, runs |

The app reads the caller's role from `GET /api/cli-auth/me` and the company membership, and hides or disables actions the role cannot perform. The server remains the authority; a `403` is shown as "You don't have permission for this in <company>".

---

## Architecture Summary

### Delivery order

1. **Phase 0: Foundations.** Repository, Gradle build, toolchain gates, CI, architecture tests.
2. **Phase 1: Shared core (KMP).** Built first, in two milestones:
   - **1a Domain, application and presentation contracts** with in-memory fakes and a demo data set. This milestone unblocks both UIs.
   - **1b Infrastructure adapters**: Ktor API client, WebSocket client, SQLDelight cache, secure storage (`expect`/`actual`), settings, and contract tests against a pinned Paperclip server.
3. **Phase 2: Native UIs, in parallel.** 2i iPhone (SwiftUI) and 2a Android (Jetpack Compose) start as soon as milestone 1a is merged, built against the shared presentation layer running on fakes. They switch to the real adapters when 1b lands. If only one UI engineer or agent stream is available, build Android first (fastest feedback loop on the KMP code), then iPhone.
4. **Phase 3: Notifications.** Push relay plus APNs and FCM wiring in both apps.
5. **Phase 4: v1.1 features.**

### Module layout (Gradle)

The project lives in the `paperclip-agent-factory-mobile/` subdirectory of the dev-team-bots monorepo, as an independent Gradle build. Its CI workflows sit in the repo-root `.github/workflows/` with `paths: paperclip-agent-factory-mobile/**` filters, like the other modules.

```
paperclip-agent-factory-mobile/
├── shared/
│   ├── domain/            # commonMain: entities, value objects, ports (interfaces), domain errors
│   ├── application/       # commonMain: use cases; depends on domain only
│   ├── presentation/      # commonMain: screen state holders (StateFlow), UI models, intents
│   ├── infrastructure/
│   │   ├── api/           # Ktor client, DTOs, mappers, auth plugin, error mapping
│   │   ├── realtime/      # Ktor WebSocket live-events client
│   │   ├── persistence/   # SQLDelight cache + non-secret instance config store
│   │   ├── securestore/   # expect/actual SecretStore: Keychain (iOS), Keystore (Android)
│   │   └── platform/      # clock, connectivity, biometrics bridge, push token bridge
│   ├── testing/           # fakes for every port, fixtures, demo data set
│   └── umbrella/          # exports PaperclipKit.xcframework for iOS; Koin DI graph
├── androidApp/            # Jetpack Compose UI, Android entry points, FCM service
├── iosApp/                # SwiftUI UI, Xcode project, APNs, Notification Service Extension
├── relay/                 # Phase 3 push relay (separate deployable)
├── docs/  user-docs/  specs/
├── AGENTS.md  CLAUDE.md  INTENT.md  README.md
```

Dependency rule: `presentation → application → domain`; every `infrastructure/*` module depends on `domain` only; `umbrella` and the app modules are the only composition roots. Architecture tests (Konsist) fail the build on any violation.

### Key technology choices

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | Kotlin 2.x (latest stable at project start), Swift 6 | Versions pinned in `gradle/libs.versions.toml` |
| Shared code | Kotlin Multiplatform: `commonMain`, `androidMain`, `iosMain` | Targets `androidTarget`, `iosArm64`, `iosSimulatorArm64` |
| Concurrency | kotlinx.coroutines, `Flow`/`StateFlow` | Exposed to Swift as async sequences via SKIE |
| HTTP | Ktor client (OkHttp engine on Android, Darwin engine on iOS) | Bearer auth plugin per instance |
| Serialization | kotlinx.serialization | DTOs generated from `GET /api/openapi.json` where present, hand-written elsewhere |
| Realtime | Ktor WebSockets | `/api/companies/{id}/events/ws` |
| Cache | SQLDelight | Native driver on iOS, Android driver on Android, in-memory driver in tests |
| Non-secret settings | SQLDelight table `instance_config` | No secrets, ever |
| Secrets | iOS Keychain (`kSecClassGenericPassword`), Android Keystore AES-GCM key wrapping an encrypted DataStore file | Not `androidx.security:security-crypto` (deprecated) |
| DI | Koin (shared graph), thin platform modules | |
| iOS UI | SwiftUI, iOS 17+, Observation | Consumes `PaperclipKit.xcframework` via Swift Package |
| Android UI | Jetpack Compose, Material 3, minSdk 28, targetSdk latest | Navigation Compose |
| Tests (shared) | kotlin.test, Kotest assertions, Turbine (Flow), Ktor `MockEngine`, Mokkery or hand-written fakes | MockK is JVM-only; not used in `commonTest` |
| Tests (apps) | Swift Testing / XCTest, XCUITest; JUnit 5, Compose UI test, Roborazzi screenshots | |
| Quality gates | ktlint (via Spotless), detekt, Android Lint, Kover, Konsist, SwiftLint, swift-format | |

The alternative of a single Compose Multiplatform UI for both platforms was considered and rejected for v1 because the brief asks for two native UIs with platform conventions (navigation, sheets, haptics, notification actions). It remains an open question for later phases.

---

## Functional Requirements

### Instance configuration (multiple Paperclip instances)

**FR-001:** The app stores any number of **instance configurations** ("factories"). Each has: `id` (UUID, app-generated), `displayName` (1–40 chars, unique per device), `baseUri`, `deploymentMode` and `exposure` (last reported by the server), `authMethod` (`PAIRED_BOARD_KEY` or `MANUAL_BOARD_KEY`), `boardKeyId`, `keyExpiresAt`, `defaultCompanyId`, `lastCompanyId`, `pinnedSpkiSha256` (optional list), `createdAt`, `lastConnectedAt`, `sortOrder`.

**FR-002:** Non-secret configuration is persisted in the shared SQLDelight store. **Secret material is never written to SQLDelight, settings, logs, crash reports, analytics, or OS backups.**

**FR-003:** The user can add an instance by **typing or pasting a URI**. The app normalises it (trims whitespace, lower-cases scheme and host, removes a trailing `/`, keeps a path prefix such as `/paperclip` for reverse-proxied installs) and validates it:
- scheme must be `https`; `http` is accepted only for `localhost`/`10.0.2.2` in debug builds;
- host must be present; user-info (`user:pass@`) is rejected;
- an explicit port is allowed.

**FR-004:** The user can add an instance by **scanning a QR code** or opening a **deep link** `paperclipmobile://add?uri=<urlencoded>&name=<optional>`. A deep link only pre-fills the form; the user must confirm. Links never carry credentials.

**FR-005:** Before pairing, the app **probes** `GET {baseUri}/api/health` and shows the reported deployment mode, exposure and server version. It refuses to continue when the mode is `local_trusted` and explains why. A hostname or TLS failure shows targeted help (for example "Add this hostname with `paperclipai allowed-hostname`", "Is Tailscale connected on this phone?").

**FR-006:** The user can rename, reorder, edit the URI of, re-pair, and delete an instance. Deleting removes its config row, its cache, and every secret under its namespace, and revokes the board key on the server when reachable (`POST /api/cli-auth/revoke-current`).

**FR-007:** The user can switch instance and company from the Factories tab and from the instance switcher in the Inbox and Factory headers. The last selection is restored on launch.

**FR-008:** Instance configurations (without secrets) can be exported as a QR code or deep link to set up a second device; the second device must still pair for its own credentials.

### Secure credential storage

**FR-010:** The shared `domain` defines a `SecretStore` port: `put(key, value, policy)`, `get(key, reason): Result<Secret>`, `delete(key)`, `deleteNamespace(instanceId)`. Keys are namespaced `instance/{instanceId}/{secretName}`. The `Secret` type never prints its value in `toString()`.

**FR-011:** iOS implementation stores items in the Keychain as generic passwords with `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`, `kSecAttrSynchronizable = false`, and, when biometric protection is on, a `SecAccessControl` of `.biometryCurrentSet` (fallback `.userPresence`).

**FR-012:** Android implementation generates a per-app AES-256-GCM key in the Android Keystore (StrongBox when available), with `setUserAuthenticationRequired(true)` and per-use biometric authentication when biometric protection is on. Ciphertext is stored in a DataStore file excluded from backup (`android:allowBackup="false"` plus `dataExtractionRules`).

**FR-013:** Secrets stored per instance: `boardToken` (required). Reserved for later: `clientCertificate` (mTLS, PKCS#12) and `clientCertificatePassword`.

**FR-014:** The app requires device authentication (Face ID / Touch ID / fingerprint / device PIN) to open the app after more than a configurable idle period (default 5 minutes) and before any destructive action: pausing an agent (FR-071), cancelling a run (FR-075), destructive decision options (FR-041), rejecting an approval (FR-051) and raising a budget (FR-052).

**FR-015:** If biometrics change (new fingerprint enrolled) and the key is invalidated, the app reports "Credentials for <instance> need re-pairing" and offers one-tap re-pair. It never silently falls back to unprotected storage.

**FR-016:** Optional certificate pinning per instance (`pinnedSpkiSha256`), recommended for public cloud instances. A pin mismatch blocks all requests to that instance.

### Authentication and pairing

**FR-020:** **Paired board key (default).** The app calls `POST /api/cli-auth/challenges` with `command: "paperclip-agent-factory-mobile pair"`, `clientName` (device name, editable), and optional `requestedCompanyId`. It opens the returned `approvalUrl` in `ASWebAuthenticationSession` (iOS) or a Custom Tab (Android), then polls `GET /api/cli-auth/challenges/{id}` at `suggestedPollIntervalMs` until approved, cancelled or expired. On approval the `boardApiToken` is written to the `SecretStore`; the in-memory copy is cleared.

**FR-021:** **Manual board key (fallback).** The user can paste a `pcp_board_…` key minted in the web app or CLI. It is validated with `GET /api/cli-auth/me` before being stored.

**FR-022:** After pairing, `GET /api/cli-auth/me` populates the user, accessible companies and instance-admin flag; the user picks a default company.

**FR-023:** Board keys expire after 30 days on current Paperclip servers. The app records `keyExpiresAt`, warns from 5 days before expiry (banner plus local notification), and offers one-tap re-pair. A `401` marks the instance "Signed out" without deleting its configuration.

**FR-024:** "Sign out of this instance" revokes the key server-side, then deletes it locally. "Sign out everywhere" does this for every instance.

**FR-025:** The user can list and revoke this account's board keys (`GET /api/board-api-keys`, `DELETE /api/board-api-keys/{keyId}`), with the current device marked.

### Inbox (Attention feed)

**FR-030:** The Inbox tab loads `GET /api/companies/{c}/attention?sort=decide&limit=50` with cursor paging, and groups items into **Decide now** (`decideBy = today` or `severity = critical`), **This week**, and **Whenever**.

**FR-031:** Each card shows severity, source kind, subject title and identifier, `whyNow`, origin agent and relative time. Filter chips come from `countsBySourceKind`.

**FR-032:** `decideNowCount` drives the Inbox tab badge and the app icon badge.

**FR-033:** Inline actions appear only when `inlineResolvable` is true and the role allows them:
- `approval`: Approve / Reject (Reject asks for a note);
- `budget_alert` hard stop: Keep paused / Raise and resume;
- everything else opens its detail screen.

**FR-034:** Swipe actions: Snooze (1 hour, tonight, tomorrow, next week) and Dismiss, via `POST /api/companies/{c}/inbox-dismissals`; Undo within 5 seconds via `DELETE …/inbox-dismissals/{itemKey}`. Setting a decide-by date uses `PUT /api/companies/{c}/decision-triage/{sourceKind}/{sourceId}`.

**FR-035:** Pull to refresh; refresh on foreground; background refresh only via push (Phase 3).

### Decisions

**FR-040:** Decision detail loads `GET /api/decisions/{id}` and shows the question, the origin agent, the target issue, the decide-by date, and **every option with a plain-language summary of its effects** (`comment_on_issue`, `create_issue`, `update_issue_status`, `assign_issue`, `cancel_issue_tree`, `resolve_blocker`) before the user chooses.

**FR-041:** Options with `style = destructive` or containing `cancel_issue_tree` are shown in the destructive colour and require device authentication.

**FR-042:** Option inputs (`inputs[]`) render as text fields honouring `required` and `maxLength`.

**FR-043:** Confirm calls `POST /api/decisions/{id}/decide`; Dismiss calls `POST /api/decisions/{id}/dismiss`. After deciding, the screen shows `executionStatus` and per-effect results (`executed`, `skipped`, `failed`), polling until terminal.

**FR-044:** A decision that is no longer `open` (decided elsewhere, expired, cancelled) shows its final state instead of options.

### Approvals and budget incidents

**FR-050:** Approval detail loads `GET /api/approvals/{id}`, linked issues and comments, and renders the payload per type: `hire_agent` (role, reports-to, adapter, budget), `approve_ceo_strategy` (strategy summary), `budget_override_required` (scope, current spend, requested amount), `request_board_approval` (markdown body).

**FR-051:** Actions: Approve, Reject, Request revision (requires a note), and Comment, using the Approvals endpoints. Request revision is offered only from `pending`.

**FR-052:** Budget incident resolution uses `POST /api/companies/{c}/budget-incidents/{id}/resolve` with `keep_paused` or `raise_budget_and_resume` and a new amount that must exceed observed spend. A `422` (unaccounted runs, unpriced usage) is shown with the server's reason.

### Work (issues)

**FR-060:** Work tab lists issues via `GET /api/companies/{c}/issues` with saved filters: Blocked, Critical and high, In review, Assigned to me, All open; plus search `q`.

**FR-061:** Issue detail shows status, priority, assignee, project, parent and subtasks, and tabs for Thread (comments + interactions), Plan (plan document, read-only), Runs (`GET /api/issues/{i}/runs`) and Files (attachments, opened with HTTP Range).

**FR-062:** The user can comment (`POST /api/issues/{i}/comments`, with optional reopen or interrupt), change status and assignee (`PATCH /api/issues/{i}`), and create a new issue or subtask (`POST /api/companies/{c}/issues`).

**FR-063:** Interactions render natively by kind: `ask_user_questions` (form; `respond`), `request_confirmation` (Accept / Reject), `request_checkbox_confirmation` (checklist; `respond`), `suggest_tasks` (accept selected), `request_item_verdicts` (per-item verdict). Unsupported kinds show "Open in web app" with the instance URL.

### Agents

**FR-070:** Agents tab lists `GET /api/companies/{c}/agents` grouped into Needs attention (`error`, budget-paused, `pending_approval`), Running, Paused and Idle, with `lifecycleState` shown when not `ready`.

**FR-071:** Actions: Pause and Resume (`POST /api/agents/{a}/pause|resume`, device auth for pause), Run now (`POST /api/agents/{a}/heartbeat/invoke`), and Set monthly budget (`PATCH /api/agents/{a}/budgets`, owner only).

**FR-072:** Agent detail shows role, reports-to, current issue, recent runs and month spend.

### Runs

**FR-075:** Run viewer loads `GET /api/heartbeat-runs/{r}`, its events and a log tail, and offers Cancel (`POST /api/heartbeat-runs/{r}/cancel`, device auth). It refreshes live while running (FR-090).

### Factory home

**FR-078:** Factory tab shows `GET /api/companies/{c}/dashboard` tiles (agents, tasks, month spend against budget, pending approvals, budget incidents), the runs in progress, and rolling spend from `GET /api/companies/{c}/costs/window-spend`.

### Notifications (Phase 3)

**FR-080:** A **push relay** runs beside each Paperclip instance (Paperclip plugin preferred, sidecar container otherwise). It authenticates with its own paired board key, polls the Attention feed per company every 30–60 seconds, and sends notifications through APNs and FCM.

**FR-081:** The app registers its push token with the relay after pairing, keyed by board key id; revoking the key unregisters the device.

**FR-082:** Default payloads are **content-free**: instance id, company id, item key, kind, severity and `decideNowCount`, with generic text ("2 decisions need you in Acme Labs"). Optional rich previews fetch the title on-device (iOS Notification Service Extension, Android handler) over the user's network.

**FR-083:** Notification rules per instance and company: severity threshold (default high), kinds (approvals, decisions, questions, failed runs, budget, agent errors), quiet hours, mute.

**FR-084:** Approval notifications carry Approve / Reject actions that require device unlock. Other kinds open the relevant screen.

**FR-085:** Tapping any notification deep-links to the item, switching instance and company if needed.

**FR-086:** With no relay configured, the app shows a one-time explanation and works without push.

### Realtime and data freshness

**FR-090:** While the app is in the foreground, the shared core keeps visible data fresh. It uses the live-events WebSocket when the server accepts board-key auth on it, and otherwise polls the visible screen every 15 seconds (5 seconds on a running run). Polling stops in the background.

**FR-091:** Live events invalidate cached entities by type: `agent.status` → agent, `heartbeat.run.*` → run, `activity.logged` → referenced issue and the Attention feed.

### Offline and resilience

**FR-095:** All read screens render from the cache first, marked with "Updated <time>", then refresh. With no connectivity, actions are disabled with an explanation; v1 has no offline write queue.

**FR-096:** Reads retry with exponential backoff (max 3). Mutations are never retried automatically unless the server supports an idempotency key (see Dependencies). A mutation that times out re-reads the entity and shows its true state.

**FR-097:** A server error body `{error, details?}` is mapped to a typed domain error; the UI shows a human-readable message and never raw JSON.

---

## Screen Inventory

Screens 1–8 are mocked on the [UI canvas](https://claude.ai/artifact/9NMV8bCwZC91djgaRJD56v). Screens 9–14 extend it for this PRD.

| # | Screen | Tab / entry | Primary FRs | Status |
| --- | --- | --- | --- | --- |
| 1 | Inbox (Attention) | Inbox tab | FR-030 to FR-035 | Mocked |
| 2 | Decision detail | Inbox card, push | FR-040 to FR-044 | Mocked |
| 3 | Approval detail | Inbox card, push | FR-050 to FR-052 | Mocked |
| 4 | Issue detail with interaction | Inbox, Work | FR-061 to FR-063 | Mocked |
| 5 | Factory home | Factory tab | FR-078 | Mocked |
| 6 | Agents | Agents tab | FR-070 to FR-072 | Mocked |
| 7 | Run viewer | Factory, Agents, Issue | FR-075, FR-090 | Mocked |
| 8 | Factories and pairing | Factories tab | FR-001 to FR-008, FR-020 to FR-023 | Mocked |
| 9 | Add / edit instance form (URI, name, probe result, pins) | Factories | FR-003 to FR-006, FR-016 | To design |
| 10 | Work list with saved filters | Work tab | FR-060, FR-062 | To design |
| 11 | Agent detail | Agents | FR-071, FR-072 | To design |
| 12 | Budget incident sheet | Inbox | FR-052 | To design |
| 13 | Security settings (app lock, biometrics, paired devices, sign out) | Factories → Settings | FR-014, FR-024, FR-025 | To design |
| 14 | Notification settings | Factories → Settings | FR-083, FR-086 | To design (Phase 3) |

Navigation: five tabs (Inbox, Factory, Work, Agents, Factories). Details push on a stack; confirmations use sheets. iOS follows the Human Interface Guidelines (large titles, swipe actions, sheets); Android follows Material 3 (top app bar, bottom navigation bar, modal bottom sheets, predictive back).

---

## Non-Functional Requirements

| Area | Requirement |
| --- | --- |
| Security | Meets OWASP MASVS L2 controls for storage, crypto, auth, network and platform. No secrets in logs; release builds strip debug logging. App-switcher snapshot is blurred. Screenshots are allowed except on the pairing screen. |
| Network | TLS 1.2+ only; no ATS exceptions on iOS; `cleartextTrafficPermitted=false` on Android except debug localhost. 15 s connect and 30 s read timeouts. |
| Performance | Cold start to cached Inbox under 1.5 s on an iPhone 13 and a Pixel 6; warm action round trip under 1 s p95 on a healthy network, excluding server time. |
| Battery and data | No background polling. WebSocket closed within 10 s of backgrounding. |
| Accessibility | WCAG 2.2 AA contrast; Dynamic Type and Android font scaling to 200%; VoiceOver and TalkBack labels on every control; 44 pt / 48 dp touch targets; no information carried by colour alone. |
| Localisation | All strings externalised (String Catalog on iOS, `strings.xml` on Android, shared UI models carry keys, not prose). English only at launch. |
| Platforms | iOS 17+ (iPhone), Android 9 / API 28+ (phones). |
| Privacy | No third-party analytics or crash SDKs that transmit content. Crash reporting, if added, is opt-in and scrubs URIs and identifiers. |
| Compatibility | Supports the Paperclip version pinned in CI and the two releases before it; warns when `/api/health` reports an untested newer version. |
| Code quality | TDD; 90% aggregate line coverage on `shared/domain` + `shared/application` (Kover); no reduction in coverage per module; zero detekt, ktlint, Android Lint, SwiftLint warnings; Konsist architecture tests green. |
| Maintainability | Platform UI layers contain no business rules: every rule has a shared test. |

---

## Acceptance Criteria

1. A user adds two instances by URI (one over Tailscale, one public HTTPS), pairs both through the browser approval flow, and switches between them; each instance's board key is present only in the Keychain / Keystore (verified by inspecting the SQLDelight file and app backups).
2. Adding a `local_trusted` instance is refused with an explanation; an `http://` URI is refused in a release build.
3. Deleting an instance removes its config, cache and secrets and revokes its key on the server.
4. With biometrics enabled, rejecting an approval and pausing an agent each require Face ID / fingerprint; enrolling a new fingerprint forces a re-pair.
5. A decision with three options shows each option's effects; choosing one calls `decide`, then shows per-effect results.
6. Every inline Inbox action resolves the item and the badge count updates without a manual refresh.
7. An `ask_user_questions` interaction can be answered on the phone and the answer appears in the web app thread.
8. Cancelling a running run from the Run viewer moves it to `cancelled` within one refresh cycle.
9. With the network off, every read screen shows cached data with its timestamp and actions are disabled.
10. CI runs `./gradlew check`, iOS build and tests, Android build and tests, and contract tests against the pinned Paperclip server; all gates pass, coverage at or above 90% on shared domain + application.
11. (Phase 3) A new critical Attention item produces a content-free push within 90 seconds; its Approve action works from the lock screen after unlock.

---

## Release Plan

| Phase | Scope | Exit criteria |
| --- | --- | --- |
| 0 Foundations | Repo, Gradle version catalog, KMP skeleton, toolchain gates, Konsist rules, CI (GitHub Actions: Linux for shared + Android, macOS for iOS), Paperclip test server in Docker | Empty modules build; all gates run in CI |
| 1a Shared contracts | Domain model, ports, use cases, presentation state holders, fakes, demo data | Use cases and state holders fully tested on fakes; coverage gate met |
| 1b Shared adapters | API client, auth/pairing, SecretStore actuals, SQLDelight, realtime, error mapping | Contract tests green against pinned Paperclip; Keychain/Keystore instrumented tests green |
| 2i iPhone UI | Screens 1–13 in SwiftUI | Acceptance criteria 1–9 on iOS |
| 2a Android UI | Screens 1–13 in Compose | Acceptance criteria 1–9 on Android |
| 3 Notifications | Relay, APNs/FCM, notification actions, settings screen 14 | Acceptance criterion 11 |
| 4 v1.1 | Costs drill-down, issue-tree holds, routines, agent chat, org chart, widgets, Live Activities / ongoing notifications | Separate PRDs |

Phases 2i and 2a run in parallel once 1a is merged. Each UI owns only its `iosApp/` or `androidApp/` folder; changes to `shared/` during Phase 2 go through a shared-module PR with tests first, so the two UI streams do not collide.

---

## Dependencies and Risks

| # | Item | Impact | Mitigation |
| --- | --- | --- | --- |
| 1 | Paperclip live-events WebSocket rejects board API keys (accepts session cookie or agent key only) | No live push to an open screen | Poll visible screen (FR-090); propose upstream change to accept board keys in `authorizeUpgrade` |
| 2 | Paperclip has no outbound webhooks or push | No notifications without a relay | Phase 3 relay polls Attention; propose an upstream attention-changed webhook |
| 3 | Board keys are unscoped, expire after 30 days, no refresh | Lost phone carries full board authority; monthly re-pair | Biometric gating, remote revoke, expiry warnings; propose scoped keys and a refresh endpoint |
| 4 | OpenAPI document covers about 217 operations of roughly 780 route handlers | Codegen incomplete | Hand-written DTOs verified by contract tests against the pinned server |
| 5 | No idempotency keys on mutations | Double-apply risk on retries | Never auto-retry mutations; re-read after timeout; propose `Idempotency-Key` |
| 6 | Paperclip changes fast | Client drift | Pin a server version in CI; nightly job against latest; version warning (NFR) |
| 7 | Home instances need Tailscale running on the phone | "Unreachable" confusion | Specific error state with a link to the Tailscale app |
| 8 | KMP–Swift interop for Flows and sealed classes | Awkward Swift API | SKIE; presentation layer exposes Swift-friendly UI models; interop reviewed in 1a |
| 9 | Two UI streams diverging | Inconsistent behaviour | All behaviour in shared presentation state holders with tests; UIs render state only |

---

## Open Questions

1. Should the push relay be a Paperclip plugin (TypeScript, inside the server) or a sidecar (Kotlin/JVM sharing this repo's `domain` and `api` modules)? Recommendation: sidecar in Kotlin first, plugin later.
2. Will operators and viewers other than J use the app at launch, or is v1 single-user?
3. Distribution: TestFlight and Play internal testing only, public stores, or enterprise MDM?
4. Which actions may run from the lock screen: approve and reject only, or also pause agent and cancel run?
5. Will J contribute the upstream Paperclip changes (board keys on the WebSocket, scoped keys, webhooks, idempotency)?
6. Should the app also manage BaoBot instances through a second adapter behind the same ports?
7. Package id: proposed `com.stainedhead.paperclipmobile`. The app lives in the `paperclip-agent-factory-mobile/` subdirectory of the dev-team-bots monorepo; should it move to its own repository before app-store release?
