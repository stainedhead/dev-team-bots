# AGENTS.md — Paperclip Agent Factory Mobile

Guidance for AI coding agents and humans working in this repository. Read [INTENT.md](./INTENT.md) first for *why* the product exists, and the current PRD for *what* to build. This file covers *how* we build it.

## Repository Overview

Paperclip Agent Factory Mobile is a pair of native mobile apps (iPhone and Android) for remotely managing one or more [Paperclip](https://docs.paperclip.ing) AI-agent instances through the Paperclip REST API. All non-UI code is written once in Kotlin Multiplatform (KMP) and shared by both apps.

This project is the `paperclip-agent-factory-mobile/` subdirectory of the dev-team-bots monorepo. It is an independent Gradle build: `cd paperclip-agent-factory-mobile` before running any command below. The Go toolchain rules in the repo-root `AGENTS.md` do not apply here; this file replaces them for this module. Never modify files outside this directory as part of app work, except the module's own workflow files in the repo-root `.github/workflows/`.

| Directory | Purpose | Language |
|---|---|---|
| `shared/domain/` | Entities, value objects, ports (interfaces), domain errors | Kotlin (commonMain) |
| `shared/application/` | Use cases that orchestrate domain logic | Kotlin (commonMain) |
| `shared/presentation/` | Screen state holders (`StateFlow`), UI models, user intents | Kotlin (commonMain) |
| `shared/infrastructure/api/` | Ktor HTTP client, DTOs, mappers, auth, error mapping | Kotlin |
| `shared/infrastructure/realtime/` | Ktor WebSocket live-events client | Kotlin |
| `shared/infrastructure/persistence/` | SQLDelight cache and non-secret instance config | Kotlin |
| `shared/infrastructure/securestore/` | `SecretStore` actuals: iOS Keychain, Android Keystore | Kotlin (`expect`/`actual`) |
| `shared/infrastructure/platform/` | Clock, connectivity, biometrics and push-token bridges | Kotlin (`expect`/`actual`) |
| `shared/testing/` | Fakes for every port, fixtures, demo data set | Kotlin (commonMain) |
| `shared/umbrella/` | Koin DI graph; exports `PaperclipKit.xcframework` | Kotlin |
| `androidApp/` | Jetpack Compose UI and Android entry points | Kotlin |
| `iosApp/` | SwiftUI UI, Xcode project, extensions | Swift |
| `relay/` | Push relay (Phase 3) | Kotlin/JVM |

Build order: `shared/` first, then `iosApp/` and `androidApp/` in parallel. A UI never waits on a real server: it can run entirely on `shared/testing` fakes ("demo mode").

## Toolchain

- **Kotlin 2.x** and **Gradle** with the version catalog `gradle/libs.versions.toml`. Never hard-code a dependency version in a build script.
- **Swift 6** and **Xcode** (latest stable). iOS deployment target 17.0.
- **Android**: minSdk 28, targetSdk and compileSdk latest stable, JDK 21 toolchain.

Every change must pass all of these locally before you commit:

```bash
# Shared + Android (from paperclip-agent-factory-mobile/)
./gradlew spotlessCheck            # ktlint formatting
./gradlew detekt                   # static analysis, zero issues
./gradlew lint                     # Android Lint, zero new warnings
./gradlew allTests                 # commonTest, androidUnitTest, iosSimulatorArm64Test
./gradlew konsistTest              # architecture rules
./gradlew koverVerify              # coverage gate (see TDD)
./gradlew check                    # runs everything above

# iOS (from paperclip-agent-factory-mobile/)
./gradlew :shared:umbrella:assembleXCFramework
swiftlint --strict iosApp
swift-format lint --recursive --strict iosApp
xcodebuild test -project iosApp/iosApp.xcodeproj -scheme iosApp \
  -destination 'platform=iOS Simulator,name=iPhone 16'
```

Use `./gradlew spotlessApply` and `swift-format format --in-place` to fix formatting. Never disable a rule inline to get a build green; if a rule is wrong for this codebase, change it in its config file in its own commit and explain why.

## Test-Driven Development

**TDD is non-negotiable.** Red, green, refactor is the only accepted workflow:

1. **Red** — write a failing test that describes the behaviour. Run it and see it fail for the right reason.
2. **Green** — write the minimum production code to make it pass.
3. **Refactor** — clean up production and test code with all tests green.

No production code is written without a failing test first. Bug fixes start with a failing regression test that reproduces the bug.

**Where tests live and what they use**

| Layer | Test source set | Tools | Notes |
|---|---|---|---|
| domain, application, presentation | `commonTest` | kotlin.test, Kotest assertions, Turbine, `kotlinx-coroutines-test` | Use fakes from `shared/testing`. Runs on JVM and iOS simulator. |
| api, realtime | `commonTest` | Ktor `MockEngine`, JSON fixtures recorded from the pinned Paperclip server | One test per endpoint mapping and per error shape |
| persistence | `commonTest` | SQLDelight in-memory driver | |
| securestore, platform | `androidInstrumentedTest`, `iosTest` | Real Keychain / Keystore on simulator and emulator | Verify secrets never reach SQLDelight or backups |
| contract | `contractTest` (JVM) | Ktor client against Paperclip in Docker, pinned version | Runs in CI, not on every local build |
| Android UI | `androidApp/src/test`, `androidTest` | JUnit 5, Compose UI test, Roborazzi screenshots | UI renders state; behaviour is tested in shared |
| iOS UI | `iosAppTests`, `iosAppUITests` | Swift Testing, XCTest, XCUITest | Same rule |

MockK is JVM-only and is **not** used in `commonTest`. Prefer hand-written fakes in `shared/testing`; use Mokkery only when a fake would be more code than the test.

**Coverage target: 90% or above on Domain and Application.** This is an aggregate gate measured by Kover as one combined line-coverage total across `shared/domain` and `shared/application`, excluding `shared/testing`. `shared/presentation` has the same 90% target as a separate gate. Infrastructure, `umbrella` and the app modules are excluded from the threshold, but the per-module rule always applies: **do not reduce coverage when adding code.**

When implementing review fixes, every finding must have its own commit before the step is marked complete. P0 findings that remain open block the PR.

## Clean Architecture

Dependencies point inward only:

```
androidApp / iosApp  →  umbrella (composition root)
                          ↓
presentation  →  application  →  domain  ←  infrastructure/*
```

- **Domain** holds entities (`Instance`, `Company`, `AttentionItem`, `Approval`, `Decision`, `Issue`, `Agent`, `HeartbeatRun`, …), value objects (`BaseUri`, `InstanceId`, `Severity`, `Money`), ports (`InstanceRepository`, `SecretStore`, `PaperclipGateway`, `LiveEvents`, `Clock`, `Biometrics`) and domain errors. It imports only the Kotlin standard library, kotlinx-coroutines-core and kotlinx-datetime.
- **Application** holds use cases (`AddInstance`, `PairInstance`, `ResolveApproval`, `DecideDecision`, `PauseAgent`, `CancelRun`, `LoadInbox`, …). One public `operator fun invoke` per use case. Depends on domain only.
- **Presentation** holds one state holder per screen exposing `StateFlow<ScreenState>` and accepting `Intent`s. It holds every UI rule (what is enabled, what needs confirmation, how errors read). Depends on application and domain.
- **Infrastructure** implements domain ports. DTOs, Ktor, SQLDelight, Keychain and Keystore types never cross into domain, application or presentation; mappers convert at the boundary.
- **Apps** render state and forward intents. A SwiftUI `View` or Compose `@Composable` contains no business rules, no HTTP, no persistence and no secret handling.

These rules are enforced by Konsist tests in `shared/umbrella/src/konsistTest`. If you need to break one, stop: you are in the wrong layer.

## Security Rules

- Secrets (board API tokens, future client certificates) are read and written **only** through the `SecretStore` port. Never store a secret in SQLDelight, settings, `UserDefaults`, `SharedPreferences`, files, logs, crash reports, analytics, test fixtures or screenshots.
- The `Secret` value type redacts itself in `toString()`. Do not add accessors that leak it into strings.
- iOS Keychain items use `kSecAttrAccessibleWhenUnlockedThisDeviceOnly` and are never synchronizable. Android keys live in the Keystore (StrongBox when available); encrypted data is excluded from backup.
- Instance URIs must be `https`. `http` is allowed only for `localhost` or `10.0.2.2` in debug builds. No ATS exceptions; no `cleartextTrafficPermitted` in release.
- Refuse instances whose `/api/health` reports `local_trusted`.
- Destructive actions (pause agent, cancel run, reject approval, destructive decision option, budget raise) go through the `Biometrics` port before the use case runs.
- Never auto-retry a mutation. Reads may retry with backoff.
- Test fixtures use obviously fake tokens (`pcp_board_TESTONLY…`). Secret scanning runs in CI.

## Paperclip API Rules

- Base path is `{baseUri}/api`. All company-scoped calls carry the selected `companyId`.
- Error bodies are `{"error": string, "details"?: object}`; map them to typed `DomainError`s in `infrastructure/api`, never surface raw JSON.
- DTOs are generated from `GET /api/openapi.json` where the spec covers an endpoint and hand-written otherwise. Either way, every DTO has a contract test against the pinned Paperclip version.
- The pinned Paperclip version lives in `gradle/libs.versions.toml` as `paperclip-server`. Bumping it is its own PR with refreshed fixtures.
- Known server gaps (board keys rejected on the WebSocket, no push, no idempotency keys) are handled behind ports, so fixing them upstream changes one adapter only.

## Mocking and Fakes

Every port in `shared/domain` has a fake in `shared/testing` that is complete enough to run the whole app in demo mode. Keep fakes behaviourally honest: when you add behaviour to a real adapter, add it to the fake and to the port's shared contract test (the same test suite runs against fake and real implementations).

## Project Structure (per module)

```
<module>/
├── build.gradle.kts
└── src/
    ├── commonMain/kotlin/com/stainedhead/paperclipmobile/<layer>/…
    ├── commonTest/kotlin/…
    ├── androidMain/  androidUnitTest/  androidInstrumentedTest/
    └── iosMain/      iosTest/
```

Package root: `com.stainedhead.paperclipmobile`. Each layer has its own package segment (`.domain`, `.application`, `.presentation`, `.infrastructure.api`, …).

## Swift and Kotlin Interop

- Shared types exposed to Swift go through `shared/umbrella`; it is the only module the iOS app imports.
- Use SKIE so `Flow` becomes an `AsyncSequence` and sealed classes become exhaustive Swift enums.
- Presentation UI models are plain data classes with Swift-friendly names; avoid generics, default arguments and `Result` in the public iOS surface.

## CI/CD

GitHub Actions in the repo-root `.github/workflows/` (GitHub ignores workflow files in subdirectories), each with a `paths: ['paperclip-agent-factory-mobile/**']` filter and named `pafm-*.yml`:

| Workflow | Runner | Pipeline |
|---|---|---|
| `pafm-shared.yml` | ubuntu | spotless → detekt → konsist → allTests (JVM) → kover verify |
| `pafm-android.yml` | ubuntu | lint → unit tests → Compose screenshot tests → assembleRelease |
| `pafm-ios.yml` | macos | XCFramework → SwiftLint → swift-format → xcodebuild test |
| `pafm-contract.yml` | ubuntu | start pinned Paperclip in Docker → contract tests; nightly against latest |
| `pafm-security.yml` | ubuntu | secret scanning, dependency vulnerability scan |

PRs must be green on every workflow before merge. Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `test:`, `refactor:`, `chore:`, `docs:`).

## Pull Requests

After opening a PR, enable automerge so it merges once checks pass. One logical change per PR; a change to `shared/` and a UI change that consumes it are separate PRs, shared first.

## Documentation Requirements

Keep these in sync with the code in the same PR:

- `README.md` — current state, setup and how to run demo mode.
- `INTENT.md` — only when product intent changes (owner approval required).
- `docs/product-summary.md`, `docs/product-details.md` — when features or behaviour change.
- `docs/technical-details.md` — when architecture or implementation changes.
- `docs/architectural-decision-record.md` — a new entry for any significant decision.
- `user-docs/` — setup guides (pairing, Tailscale, allowed hostnames).

A PR that changes behaviour without updating docs is incomplete.

## Feature Specification Workflow

All feature work uses `specs/YYMMDD-<feature-name>/` with `spec.md`, `status.md`, `plan.md`, `tasks.md`, `research.md`, `data-dictionary.md`, `architecture.md` and `implementation-notes.md`, built progressively from the PRD:

0. Research the PRD → 1. Specification → 2. Research and data model → 3. Architecture and plan → 4. Task breakdown → 5. Implementation (TDD) → 6. Completion and archive to `specs/archive/`.

**Update `status.md` after every task.** It is the single source of truth for progress. Reference the spec directory in commit messages.
