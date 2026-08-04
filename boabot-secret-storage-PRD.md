# PRD: OS Keystore Secret Storage

**Created:** 2026-08-04
**Jira:** N/A
**Status:** Draft
**Owner:** stainedhead

---

## Problem Statement

BaoBot holds long-lived secrets — `ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`, Slack bot and app tokens, and (per `boabot-buzz-support-PRD.md`) a Nostr private key that *is* the agent's identity. Today they land in two places:

| Secret | Where it lives today |
|---|---|
| `ANTHROPIC_API_KEY` | env var, or `~/.boabot/credentials` (INI, mode-checked) |
| `BOABOT_BACKUP_TOKEN` | env var, or `~/.boabot/credentials` |
| Slack `bot_token`, `app_token` | **`config.yaml`, in plaintext** (`SlackConfig`, `internal/infrastructure/config/config.go:23-27`) — no env or credentials-file path exists |
| Buzz nsec (planned) | env var, or `~/.boabot/credentials` |

Two problems follow.

1. **`config.yaml` holds plaintext secrets.** Slack tokens have no resolution path other than the config file. That file sits next to the binary, is not mode-checked, and is the file most likely to be copied, shared, or committed by accident.
2. **A dotfile is the strongest control we offer.** `credentials.Load` refuses to start on a world-readable file (`mode & 0o004`), which is a real control — but it is still plaintext at rest, readable by any process running as that user, and captured by any backup or sync tool pointed at `$HOME`.

Every desktop OS ships an encrypted-at-rest credential store. We should use it. The complication — and the reason this needs a design rather than a library call — is that **BaoBot must also run unattended as a service**, and that is precisely the mode where OS keystores stop behaving uniformly.

---

## Background — What Each OS Actually Supports

The interactive case is easy and uniform. The service case is not, and it differs *per OS*, not just in configuration.

### The matrix

| OS | Interactive (desktop / CLI / logged-in user) | Unattended service / daemon |
|---|---|---|
| **macOS** | ✅ **login keychain** via `security` / Security framework. Unlocked at login. | ⚠️ **System keychain** (`/Library/Keychains/System.keychain`), reachable by a root LaunchDaemon. The **login keychain is unreachable** — a daemon gets `errSecInteractionNotAllowed` (-25308) because there is no GUI session to prompt in, and the data-protection keychain is not available to third-party daemons at all. A daemon's keychain search list is normally just the System keychain. **Needs validation** — see FR-004. |
| **Windows** | ✅ **Credential Manager** via `CredRead`/`CredWrite`. | ⚠️ Per `CredWriteW`, a credential is written "in the user's credential set" and "associated with the logon session of the current token"; network logon sessions have no credential set at all. So a service **cannot read a credential an interactive administrator wrote** — the credential must be written under the service's own account identity. Whether that is workable under `LocalSystem` specifically is **unverified** — see OQ-1. |
| **Linux** | ✅ **Secret Service** (gnome-keyring, KWallet) over session D-Bus. | ❌ **Does not work.** Secret Service needs a session bus and an unlocked keyring; a headless systemd unit has neither, and `gnome-keyring-daemon --login` exits if it does not reach a session bus within minutes. The correct mechanism is **systemd credentials**: `LoadCredentialEncrypted=` / `SetCredentialEncrypted=`, materialised at `$CREDENTIALS_DIRECTORY` on tmpfs, auto-removed when the unit stops, optionally sealed to the TPM2 via `systemd-creds encrypt --with-key=tpm2`. |

**The headline:** an OS-keystore abstraction cleanly covers all three *interactive* cells and at most one *service* cell. Linux-as-a-service needs a different mechanism entirely, and Windows-as-a-service needs a provisioning step. Any design that claims "one keyring library, works everywhere" is wrong about half the matrix.

### What a keystore does and does not buy

Worth stating plainly so the change is not oversold:

- It **does** remove plaintext secrets from `config.yaml` and from a dotfile in `$HOME`, and it puts them behind an OS-managed, encrypted-at-rest store with per-application access control.
- It **does not** stop the secret from being plaintext in the process's memory once loaded. Nothing here changes that.
- On Linux-service it **does not** avoid disk entirely either: `LoadCredentialEncrypted=` decrypts to `$CREDENTIALS_DIRECTORY`, which is tmpfs (not persistent storage) and readable by the service user. That is a genuine improvement over a dotfile — encrypted at rest, scoped to the unit, wiped on stop — but it is not a TPM-sealed-at-use story.

### Library evaluation

| Library | Stars | Latest release | Last push | Verdict |
|---|---|---|---|---|
| [`zalando/go-keyring`](https://github.com/zalando/go-keyring) | 1,307 | **v0.2.8, Mar 2026** | **Jul 2026** | ✅ **Recommended.** Actively maintained. Backends: macOS `security`, Windows Credential Manager (via `danieljoos/wincred`), Secret Service over D-Bus — exactly the three cells where a keystore is the right answer. MIT. |
| [`99designs/keyring`](https://github.com/99designs/keyring) | 655 | v1.2.2, **Dec 2022** | **May 2024** | ❌ Rejected. More backends (encrypted file, Pass, KeyCtl, KWallet), but effectively dormant — no release in over three years. Its extra fallback backends are the only reason to prefer it, and our provider chain supplies fallbacks itself. MIT. |

The fallback logic belongs in our chain, not in the library. That makes the maintained three-backend library the better fit, and keeps `keyring` confined to one provider implementation.

**Verified caveat:** `zalando/go-keyring`'s darwin backend shells out to `/usr/bin/security find-generic-password -s <service> -wa <username>` with **no `-k` keychain argument** (`keyring_darwin.go:43-49`). It therefore relies on the process's implicit keychain search list rather than naming a keychain. For a root daemon that list is normally just the System keychain, so it may work unmodified — but this is an assumption, not a documented guarantee, and it is the cell most likely to be wrongly marked as working. FR-004 requires validating it on a real LaunchDaemon.

---

## Code Analysis — Current State

### The single call site

`cmd/boabot/main.go:56-69` is the whole of today's secret resolution:

```go
credsPath, err := credentials.DefaultPath()          // ~/.boabot/credentials
creds, err := credentials.Load(credsPath)            // world-readable → fatal
applyCredential(creds, "anthropic_api_key", "ANTHROPIC_API_KEY")
applyCredential(creds, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")
```

`credentials.Load` (`internal/infrastructure/credentials/credentials.go`) is an INI parser: it selects a profile from `BOABOT_PROFILE` (default `"default"`), returns an empty map and `nil` when the file is absent, and returns an error when the file's mode has the world-readable bit set. `applyCredential` promotes a value to an env var **only when that env var is unset** — so an explicit env var already wins over the file.

This is a good foundation. It is already an ordered chain with two links (env var → file), it already fails closed on a permissions mistake, and it is already funnelled through one call site. **This PRD extends that chain rather than replacing it.**

### Gaps

1. **Slack tokens bypass it entirely.** `SlackConfig` reads `bot_token` and `app_token` straight from `config.yaml`. There is no env var or credentials-file path for them, so the only way to run Slack today is plaintext in the config file.
2. **The chain is hardcoded, not a port.** `applyCredential` is a package-level function in `main`, called twice with literal key names. There is no interface, so no provider can be added without editing `main` and no provider can be unit-tested through a seam.
3. **Nothing is namespaced per bot.** All bots in the process share one credentials file and one env space. A per-bot Buzz nsec (per `boabot-buzz-support-PRD.md` OQ-4) has no key convention to slot into.

---

## Architecture Decision

### A `SecretStore` port with an ordered provider chain

```go
// internal/domain/secret.go (illustrative)
type SecretRef struct {
    Name string // logical name, e.g. "buzz_private_key"
    Bot  string // optional per-bot namespace; "" = global
}

type SecretProvider interface {
    Name() string                                          // for logs: "env", "keystore", ...
    Lookup(ctx context.Context, ref SecretRef) (string, bool, error)
}

type SecretStore interface {
    Get(ctx context.Context, ref SecretRef) (string, error)
}
```

`SecretStore` holds `[]SecretProvider` and returns the **first hit**. A provider that is unavailable on this platform, or has no entry, returns `(false, nil)` — not an error. Only a provider that is present and *malfunctioning* returns an error.

### Default chain order

| # | Provider | Rationale |
|---|---|---|
| 1 | **Explicit environment variable** | Highest precedence. Containers, CI, and `docker run -e` must always win, and this preserves today's behaviour exactly. |
| 2 | **systemd credentials** (`$CREDENTIALS_DIRECTORY`) | Linux-service only; the directory is present only when systemd set it. Correct answer for the Linux-service cell. |
| 3 | **OS keystore** (`zalando/go-keyring`) | The new capability. Covers all three interactive cells and, pending FR-004, macOS-service. |
| 4 | **Credentials file** (`~/.boabot/credentials`) | Today's mechanism, retained unchanged as the floor. Keeps every existing deployment working with no migration. |

Ordering 2 above 3 matters: on a Linux service both may nominally be "available," and the systemd credential is the one that actually works.

### Why not replace the credentials file

Because it works, it is already mode-checked, and it is the only mechanism that functions identically in every cell of the matrix — including containers, which have no keystore and no systemd. It becomes the fallback, not the recommendation.

---

## Goals

- **G1** — Secrets can be stored in the OS-native encrypted credential store on macOS, Windows, and Linux, for interactive use.
- **G2** — BaoBot resolves secrets correctly when started unattended as a service on all three platforms, using the mechanism appropriate to that platform rather than assuming a keystore is reachable.
- **G3** — No secret is required to live in `config.yaml`, including the Slack tokens that have no alternative today.
- **G4** — Secret resolution moves behind a domain port with an ordered, testable provider chain, replacing the hardcoded two-link chain in `main`.
- **G5** — Existing deployments keep working with no configuration change: env vars and `~/.boabot/credentials` continue to resolve exactly as they do now.

## Non-Goals

- **NG1** — Remote secret managers (AWS Secrets Manager, Vault, 1Password). The port makes them additive later; none is built here.
- **NG2** — Encrypting the secret in process memory, or defending against a local attacker who can read our process memory or attach a debugger.
- **NG3** — TPM/Secure-Enclave sealed-at-use key operations. `systemd-creds --with-key=tpm2` is supported as a *storage* option because systemd provides it for free; we do not build TPM support ourselves.
- **NG4** — Secret rotation, expiry, or lease renewal. This PRD covers storage and retrieval only.
- **NG5** — A GUI or TUI for secret entry. Provisioning is CLI and OS-native tooling.
- **NG6** — Changing what secrets exist or how they are used by the subsystems that consume them.

---

## Functional Requirements

### The port and the chain

**FR-001:** A `SecretStore` port and a `SecretProvider` interface MUST be defined in `internal/domain/`. Neither may import any keystore, D-Bus, or OS-specific package.

**FR-002:** `SecretStore.Get` MUST consult providers in configured order and return the first hit. A provider that is unavailable on the current platform, or that holds no entry for the reference, MUST return "not found" rather than an error, and MUST NOT halt the chain.

**FR-003:** The default provider order MUST be: explicit environment variable → systemd credentials directory → OS keystore → credentials file. The order MUST be configurable, and any provider MUST be omissible.

**FR-004:** An OS keystore provider MUST be implemented over `zalando/go-keyring`, confined to `internal/infrastructure/secret/keystore/`. Its behaviour **MUST be validated on a real service on each platform** before the platform is documented as supported — specifically including whether the library's `security` invocation reaches the **System** keychain from a root LaunchDaemon, given it passes no `-k` argument. If it does not, the provider MUST either name the keychain explicitly or the macOS-service cell MUST be documented as requiring a provisioning step (`security add-generic-password -A -k /Library/Keychains/System.keychain …`).

**FR-005:** A systemd credentials provider MUST be implemented that reads `$CREDENTIALS_DIRECTORY/<name>`. It MUST be inert when that variable is unset, so it costs nothing on non-Linux platforms and on Linux outside systemd.

**FR-006:** The existing credentials-file loader MUST be wrapped as a provider with its behaviour unchanged, **including the world-readable check remaining fatal**. That check is the floor control for the file provider and MUST NOT be downgraded to a warning.

**FR-007:** The environment-variable provider MUST preserve today's precedence exactly: an explicitly-set env var wins over every other provider.

**FR-008:** Secret lookups MUST be namespaced per bot where a per-bot secret is meaningful, via `SecretRef.Bot`. The keystore key convention MUST be documented and stable, since it becomes the on-disk contract in users' keychains.

### Callers and configuration

**FR-009:** `cmd/boabot/main.go` MUST resolve `ANTHROPIC_API_KEY` and `BOABOT_BACKUP_TOKEN` through `SecretStore` rather than the current two `applyCredential` calls, with no change in observable behaviour for existing deployments.

**FR-010:** `SlackConfig` MUST gain a secret-resolution path so `bot_token` and `app_token` can be supplied without appearing in `config.yaml`. The existing inline fields MUST continue to work for one release, and MUST log a deprecation warning naming the file when used.

**FR-011:** Config loading MUST reject any `config.yaml` that inlines a secret for which a resolution path now exists, once the deprecation period in FR-010 ends. Until then it MUST warn.

**FR-012:** `boabotctl` MUST gain subcommands to write, read-presence-of (never the value), and delete secrets in the OS keystore, so operators are not required to learn `security`, `cmdkey`, and `secret-tool` separately.

**FR-013:** A diagnostic command MUST report, for each configured secret, **which provider resolved it** — never the value, and never a prefix or suffix of the value. Without this, a four-link chain is undebuggable in the field.

### Safety

**FR-014:** No secret value may be logged at any level, by any provider, on any code path, including error paths. Provider errors MUST be reported by provider name and reference name only.

**FR-015:** A secret value MUST NOT be passed to a subprocess as a command-line argument by any provider, since process arguments are world-readable on all three platforms.

**FR-016:** When no provider resolves a required secret, the error MUST name the reference and enumerate the providers consulted, so the operator knows where the value was looked for.

**FR-017:** Documentation MUST state the residual exposure explicitly: secrets are plaintext in process memory once loaded, and on Linux-service the systemd credential is plaintext at `$CREDENTIALS_DIRECTORY` (tmpfs, unit-scoped, wiped on stop). The keystore MUST NOT be presented as meaning "the secret never touches disk."

---

## Non-Functional Requirements

- **Performance:** Secret resolution happens at startup only. The full chain MUST complete within 2 s per secret; an unreachable D-Bus or keychain MUST time out rather than hang startup indefinitely.
- **Reliability:** An unavailable provider MUST degrade to the next provider, never to a crash. A locked or absent keystore MUST NOT prevent startup when a later provider can supply the value.
- **Security:** FR-014 through FR-017. The world-readable check on the credentials file stays fatal. No secret in `config.yaml`, no secret on a command line, no secret in a log.
- **Portability:** MUST build and pass tests on macOS, Windows, and Linux. Platform-specific code MUST be behind build tags with a working no-op or fallback on every other platform. CI MUST run the test suite on all three.
- **Observability:** Startup MUST log, per secret, which provider resolved it — by name only (FR-013).
- **Compatibility:** An existing deployment using env vars or `~/.boabot/credentials` MUST work unchanged with no config edit. This is a strict requirement, not a best effort.
- **Maintainability:** `zalando/go-keyring` and `godbus/dbus` imports confined to `internal/infrastructure/secret/`. Domain and application layers MUST have zero keystore imports.
- **Testing:** TDD per `AGENTS.md`; 90%+ coverage on domain and application. The chain MUST be unit-testable with fake providers and no OS keystore. Real-keystore tests MUST be tagged `//go:build integration`.

---

## Acceptance Criteria

- [ ] A secret written to the macOS login keychain is resolved by an interactively-run boabot, with nothing in `config.yaml` or `~/.boabot/credentials`.
- [ ] The same, on Windows via Credential Manager.
- [ ] The same, on Linux via Secret Service in a desktop session.
- [ ] **macOS service:** a LaunchDaemon-started boabot resolves a secret from the System keychain — or, if the library cannot reach it, the documented `-k /Library/Keychains/System.keychain` provisioning step is verified to work and the limitation is recorded. Either outcome is acceptable; silently claiming support is not.
- [ ] **Linux service:** a systemd unit using `LoadCredentialEncrypted=` starts with no session D-Bus and no unlocked keyring, and resolves the secret from `$CREDENTIALS_DIRECTORY`.
- [ ] **Linux service, negative:** the same unit with only a Secret Service entry and no systemd credential fails with an error naming every provider consulted — not a hang, and not a generic failure.
- [ ] **Windows service:** a boabot Windows service resolves a credential written under its own service account identity; the OQ-1 finding on `LocalSystem` is recorded either way.
- [ ] Provider precedence: with the same logical secret present in all four providers, the env var wins; unset it and the systemd credential wins; unset that and the keystore wins; remove that and the file wins. Asserted as a single ordered test.
- [ ] A provider that errors (e.g. D-Bus refuses the connection) does not halt the chain — the next provider is consulted and resolution succeeds.
- [ ] A world-readable `~/.boabot/credentials` remains fatal at startup, with the existing error message.
- [ ] Slack `bot_token` and `app_token` resolve from the keystore with neither present in `config.yaml`, and the bot connects to Slack.
- [ ] A `config.yaml` with inline Slack tokens still works and emits a deprecation warning naming the alternative.
- [ ] `boabotctl` writes, checks presence of, and deletes a keystore secret on each of the three platforms.
- [ ] The diagnostic command reports the resolving provider for every configured secret and no secret value, verified by asserting against captured output.
- [ ] No secret value appears in log output at any level, including on every provider error path, verified by test against a captured log buffer with a sentinel value.
- [ ] No provider passes a secret as a subprocess argument, verified by inspecting the constructed command in test.
- [ ] An existing deployment using only env vars, and one using only `~/.boabot/credentials`, both start with a byte-identical config to before this change.
- [ ] `go build`, `go vet`, `golangci-lint run`, and `go test -race ./...` pass on macOS, Windows, and Linux in CI; domain and application coverage ≥90% and not regressed.
- [ ] `grep -r "go-keyring\|godbus" internal/domain internal/application` returns no matches.
- [ ] `docs/technical-details.md`, `docs/product-details.md`, and `docs/architectural-decision-record.md` updated; the ADR records the per-OS matrix and the `zalando` over `99designs` decision with the maintenance evidence.
- [ ] `user-docs/` gains a secret-provisioning guide with the per-OS × per-mode matrix and the residual-exposure statement from FR-017.

---

## Phasing

**P0 — The chain, with no new backends** (FR-001, FR-002, FR-003, FR-006, FR-007, FR-009, FR-013–FR-017)
Introduce `SecretStore`, wrap the existing env-var and credentials-file behaviour as providers, move `main` onto it, add the diagnostic. **Ships with zero behaviour change** and is independently valuable: it converts a hardcoded chain into a tested port.

**P1 — Keystore and systemd providers** (FR-004, FR-005, FR-008, FR-012)
The actual capability, plus `boabotctl` provisioning. Gated on the FR-004 per-platform service validation.

**P2 — Get secrets out of `config.yaml`** (FR-010, FR-011)
Slack tokens, the deprecation warning, then the hard rejection one release later.

**Deferred** — remote secret managers (AWS Secrets Manager, Vault, 1Password), rotation, TPM sealed-at-use.

---

## Dependencies and Risks

| Item | Type | Notes |
|---|---|---|
| `zalando/go-keyring` | Dependency | MIT, v0.2.8 (Mar 2026), pushed Jul 2026, 1.3k stars — actively maintained. Pulls in `godbus/dbus/v5` and `danieljoos/wincred`. |
| macOS System keychain reachability | Risk | The library passes no `-k` argument and relies on the implicit search list. If a root LaunchDaemon's search list is not what we assume, the macOS-service cell needs explicit keychain naming or a provisioning step. **FR-004 makes validation mandatory before claiming support.** |
| Windows service account identity | Risk | `CredWriteW` associates a credential with "the logon session of the current token", so a service cannot read what an interactive admin wrote. Provisioning must happen under the service's own identity. Behaviour under `LocalSystem` specifically is unverified — see OQ-1. |
| Linux Secret Service is a trap | Risk | It is *present* on desktop Linux and *absent* on servers, so a naive implementation works on the developer's laptop and fails in production. Mitigated by ordering systemd credentials ahead of the keystore and by the negative-path acceptance criterion. |
| `security` CLI dependency on macOS | Risk | The library shells out to `/usr/bin/security` rather than linking the Security framework. Fine on stock macOS, but it is a subprocess on the startup path and its output is parsed as text. Note FR-015 — the library uses `-i` stdin mode for writes, which keeps values off the command line; this MUST be re-verified on any library upgrade. |
| Cross-platform CI | Dependency | Requires macOS, Windows, and Linux runners. GitHub Actions provides all three, but the workflows in `.github/workflows/` are Linux-only today and will need matrix builds. |
| Testing keystores in CI | Risk | Headless CI has no unlocked keychain. Real-keystore tests must be `//go:build integration` and either skipped in CI or run against a keychain created and unlocked in the job. Do not let this pressure the unit tests into depending on a real keystore. |
| Deprecating inline Slack tokens | Risk | FR-011's eventual hard rejection breaks any deployment that ignored the warning. Mitigation: warn for a full release, name the exact replacement in the message, and call it out in release notes. |
| Scope interaction with Buzz PRD | Dependency | `boabot-buzz-support-PRD.md` FR-002 resolves the nsec via env var → credentials file, which works today and does **not** depend on this PRD. This work adds a keystore link to that same chain later, with no change to any Buzz requirement. |

---

## Open Questions

- **OQ-1:** Can a Windows service running as `LocalSystem` write and read its *own* Credential Manager entry? `CredWriteW` documents credentials as bound to the current token's logon session, and `LocalSystem` does have a profile — but this was not verified, and one widely-cited source conflated the wincred API with the Biometric Framework credential manager, which has a documented restriction on non-interactive accounts. **This must be tested on a real Windows service**, not researched. If the answer is no, Windows-service deployments need either a dedicated service *user* account or a DPAPI machine-scope blob provider, and the latter is additional scope.
- **OQ-2:** Do we run BaoBot as a service on all three platforms today, or is Windows-as-a-service hypothetical? If nobody runs it, OQ-1 drops from blocking to informational and P1 can ship covering macOS and Linux service modes only.
- **OQ-3:** Should the per-bot namespace (FR-008) key on bot *name* or bot *type*? Names are unique but change when a bot is renamed — which would orphan keychain entries with no migration path. Types are stable but collide across multiple bots of one type.
- **OQ-4:** Is `~/.boabot/credentials` the right home when running as a service, given the service account's `$HOME` may be `/var/lib/...`, `C:\Windows\System32\config\systemprofile`, or unset? A service-mode path override may be needed independently of the keystore work.
- **OQ-5:** Should `boabotctl secret set` (FR-012) be able to write to a *remote* bot's keystore, or only the local machine's? Local-only is far simpler and probably right, but the team runs bots on shared hosts.

---

## Appendix — Sources

Verified 2026-08-04:

- [`CredWriteW` (wincred.h)](https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew) — "creates a new credential … in the user's credential set"; "associated with the logon session of the current token"; `ERROR_NO_SUCH_LOGON_SESSION` — "Network logon sessions do not have an associated credential set."
- [systemd — System and Service Credentials](https://systemd.io/CREDENTIALS/) and [`systemd-creds(1)`](https://manpages.debian.org/testing/systemd/systemd-creds.1.en.html) — `LoadCredentialEncrypted=`, `SetCredentialEncrypted=`, `$CREDENTIALS_DIRECTORY`, TPM2 sealing.
- [Apple Developer Forums — daemon keychain access](https://developer.apple.com/forums/thread/656000) — `errSecInteractionNotAllowed` (-25308); daemon search list is the System keychain; data-protection keychain unavailable to third-party daemons.
- [ArchWiki — GNOME/Keyring](https://wiki.archlinux.org/title/GNOME/Keyring) and [Fedora — ModularGnomeKeyring](https://fedoraproject.org/wiki/Changes/ModularGnomeKeyring) — Secret Service requires session D-Bus; `gnome-keyring-daemon --login` exits without one.
- [`zalando/go-keyring`](https://github.com/zalando/go-keyring) — source inspected at v0.2.8 in the module cache; `keyring_darwin.go:43-49` confirms no `-k` keychain argument.
- [`99designs/keyring`](https://github.com/99designs/keyring) — release and push dates from the GitHub API.

**Not a source:** [Managing Credentials (ee207400)](https://learn.microsoft.com/en-us/previous-versions/ee207400(v=vs.85)) states "Credentials cannot be stored, queried, or deleted for … non-interactive accounts such as LocalSystem, LocalService, or NetworkService" — but that page documents the **Windows Biometric Framework** credential manager, *not* the `wincred.h` Credential Manager API this PRD uses. It is recorded here because it surfaces in searches and is easily mistaken for authority on `CredRead`/`CredWrite`. It is the reason OQ-1 is an open question rather than a settled answer.

Codebase facts taken from this repository at commit `ea41be3`.
