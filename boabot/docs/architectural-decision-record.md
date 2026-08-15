# Architectural Decision Record — boabot

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-B001 — Worker goroutines recover from panics

**Decision:** Each worker goroutine wraps its execution in a `recover()`. A panicking worker logs the error and exits cleanly without propagating to the main thread.

**Rationale:** Worker tasks are agentic and unpredictable. A single bad task must not crash the agent. The main thread and other workers continue unaffected.

---

## ADR-B002 — Config loaded from filesystem, credentials from INI file and environment variables

**Decision:** Non-secret configuration is loaded from `config.yaml` next to the binary. Credentials (API keys, backup tokens) are loaded at startup from `~/.boabot/credentials` (INI format, profile selected by `BOABOT_PROFILE` env var) and from environment variables — never from `config.yaml`. World-readable credential files are rejected with an error.

**Rationale:** Keeps secrets out of config files and git. The credentials file follows the same pattern as AWS CLI and other developer tools, making it familiar and easy to manage on a local machine without requiring any cloud infrastructure. Environment variables remain a valid override for CI/CD and container environments.

---

## ADR-B003 — Orchestrator mode is additive, not a separate binary

**Decision:** Orchestrator features (control plane, Kanban board, REST API, web UI, shared memory write serialisation) are activated by a config flag in the standard bot binary — not a separate binary or container image.

**Rationale:** Maintains a single delivery artefact. The orchestrator is operationally a bot with extra responsibilities, not a fundamentally different system. The config flag gates all orchestrator code paths cleanly.

---

## ADR-B004 — MCP config merged from shared and private sources

**Decision:** MCP configuration is loaded from two optional S3 locations and merged at startup. Private config extends (not replaces) shared config. Missing files are not errors.

**Rationale:** Allows team-wide tools to be defined once while enabling role-specific tools without coordination overhead. Missing files are not errors — the system operates on whatever is present.

---

## ADR-B005 — Tool Attention as harness middleware, not model instruction

**Decision:** Tool schema injection is controlled by the harness via BM25 scoring, not by instructing the model to ignore certain tools. The model only sees tools that the harness has chosen to inject.

**Rationale:** Model-side filtering is unreliable and still consumes context tokens. Harness-side gating is enforced regardless of the model's behaviour. This is also a security boundary — a prompt-injected instruction cannot make the model invoke a tool that is not injected.

---

## ADR-B006 — Budget caps enforced before tool dispatch, not after

**Decision:** The harness checks budget caps before dispatching any tool call or model invocation. Requests that would exceed the cap are rejected before execution.

**Rationale:** Post-execution enforcement is meaningless — the tokens and tool calls have already been consumed. Pre-execution enforcement is the only effective gate. The DynamoDB flush (30s interval) means the counter may be slightly stale after a crash, which is acceptable given the cap windows.

---

## ADR-B007 — Skill scripts run as restricted subprocesses, not plugins

**Decision:** Agent Skill scripts are executed via `exec` with a stripped environment (no inherited env vars), filesystem access limited to a temporary working directory, and network access constrained by the ECS task's security group. No plugin API or SDK.

**Rationale:** Skills are operator-approved scripts, not trusted code. Restricting the subprocess environment limits the blast radius of a buggy or malicious skill without requiring OS-level sandboxing infrastructure (gVisor, Firecracker). The ECS security group already limits network egress — the subprocess inherits this boundary implicitly.

**Rejected:** Full OS-level sandboxing (unnecessary given the Admin approval gate and existing network controls); plugin API/SDK (over-engineered, skills are simple scripts).

---

## ADR-B008 — Local in-process adapters replace AWS services

**Decision:** The agent runtime uses local in-process adapters for all messaging and storage: `local/queue` (per-bot in-process queues) instead of SQS, `local/bus` (in-process broadcaster) instead of SNS, `local/fs` (local filesystem) instead of S3, and `local/budget` (local JSON file) instead of DynamoDB. AWS infrastructure is not required to run boabot.

**Rationale:** Zero-infrastructure developer experience — anyone can run the full team on a laptop without an AWS account. Local adapters are faster (no network RTT), simpler to debug (no cloud console), and eliminate operational cost for small self-hosted deployments. The domain interface layer (`domain.MessageQueue`, `domain.Broadcaster`, `domain.MemoryStore`, `domain.BudgetTracker`) is unchanged, so cloud-backed adapters can be introduced in future without touching application or domain code.

**Rejected:** Keeping SQS/SNS/DynamoDB/S3 as the only option (requires AWS account and infrastructure provisioning just to run; local development experience is poor); LocalStack (adds Docker dependency and partial AWS API emulation — the full domain interface approach is cleaner).

---

## ADR-B009 — BM25 feature-hashing as default embedder

**Decision:** The default semantic embedder is a BM25-style feature hasher using FNV-1a hashing into a fixed 512-dimensional float32 vector, L2-normalised. No external API or network call is required. Combined with a flat cosine similarity vector store (`local/vector`), search over 100k × 512-dim vectors completes in ~40ms on commodity hardware.

**Rationale:** No API key or network call needed — the embedder is self-contained in the process. FNV-1a hashing is deterministic and fast. The O(n) flat search is sufficient for memory stores up to ~100k documents before latency becomes a concern. The `domain.Embedder` interface is swappable: operators can replace BM25 with an OpenAI or other neural embedder by setting `memory.embedder` in config, with no application-layer changes required.

**Rejected:** Neural embedding model in-process (200–500 MB memory overhead, cgo complexity, GPU dependency); OpenAI embeddings as the default (requires API key, adds per-write latency and cost, unavailable offline); HNSW approximate nearest neighbour (complexity without evidence of need at current scale).

---

## ADR-B010 — Tech-lead sub-agent isolation via distinct Bus and Router instances

**Decision:** Each sub-agent spawned by `SubTeamManager.Spawn` receives a new `context.CancelFunc` (derived from the tech-lead's context) plus a unique bus ID. No shared in-process state (bus, queue, router) is reused between the parent tech-lead and its sub-agents, or between sibling sub-agents.

**Rationale:** Sub-agents must not be able to interfere with each other through shared queues or bus subscriptions. Giving each sub-agent its own cancellable context also ensures clean teardown: the tech-lead can terminate one sub-agent without affecting any others. A shared bus would require careful filtering to prevent message cross-contamination, which is error-prone; isolation by construction eliminates the problem entirely.

**Message-based spawn/terminate instead of LLM tool calls.** Spawn and terminate operations arrive as typed messages (`subteam.spawn`, `subteam.terminate`) on the tech-lead's existing queue, processed by `RunAgentUseCase`. This keeps the harness as the single entry point for all external control signals and avoids adding spawn/terminate as model-visible tools (which would allow an LLM to autonomously spawn unlimited sub-agents without any operator visibility).

**Heartbeat watchdog.** The 30s/90s heartbeat design (three missed intervals trigger self-termination) was chosen over a configurable TTL or an explicit "idle" signal because it provides automatic cleanup without requiring the parent to explicitly track sub-agent liveness. The watchdog runs entirely inside the sub-agent's goroutine — no separate monitor goroutine is required.

**Session file persistence.** Sub-agent state is persisted to `<memory>/session.json` using atomic writes (write .tmp → `os.Rename`). A corrupt or missing file returns an empty slice with no crash, enabling recovery from partial writes or unexpected process termination.

**Rejected:** Shared bus with per-bot topic filtering (complex, error-prone); LLM tool call as spawn trigger (no operator visibility, unbounded spawning risk); per-sub-agent monitor goroutine (one heartbeat loop per sub-agent within the sub-agent's own goroutine is simpler and avoids goroutine proliferation).

---

## ADR-B012 — Static file registry protocol rather than a hosted service

**Decision:** Plugin registries are static HTTPS file catalogs. A registry is any HTTPS origin that serves an `index.json` file at its root. Manifests and archive download URLs are absolute HTTPS links embedded in `index.json`. The boabot runtime fetches these directly using stdlib `net/http`; no registry server software or database is required to host a registry.

**Rationale:** A hosted registry service would add operational complexity (servers to run, databases to maintain, APIs to version) with no benefit at current scale. A GitHub repository with raw file access serves as a fully functional first-party registry at zero additional cost. The static protocol is also compatible with S3, GitHub Pages, and any CDN. The only requirement is anonymous HTTPS access, which is universally available.

**Trust model is in the client, not the server.** Each registry carries a `trusted` flag in the local configuration. This means the same registry URL can be trusted by one operator and untrusted by another without any server-side change. The trust decision is entirely local and does not require the registry to signal its own trustworthiness.

**Rejected:** Hosted registry service with search, ratings, and version management (operational overhead exceeds benefit for current scale); private/authenticated registries (unnecessary complexity; operator deployments that need privacy can self-host on a private HTTPS origin and restrict network access at the infrastructure level).

---

## ADR-B013 — In-memory index cache in the RegistryManager adapter, not the application layer

**Decision:** The 5-minute TTL cache for registry indexes is held inside `HTTPRegistryManager` (infrastructure layer), not in a cache managed by the application use case.

**Rationale:** The application use case (`InstallUseCase`, `RegistryUseCase`) is stateless by design — it orchestrates interfaces without retaining mutable state. Placing the cache in the application layer would require the use case to hold a map, protected by a mutex, and to manage TTL expiry logic — none of which is business logic. The `RegistryManager` interface already abstracts the concept of "fetch the index for this registry", and whether that fetch goes to the network or memory is purely an infrastructure concern.

Keeping the cache in the adapter also means test doubles (`mocks.MockRegistryManager`) return whatever the test configures without needing to worry about cache state.

**`force` parameter.** `FetchIndex(ctx, url, force bool)` is the mechanism by which the application or admin can bypass the cache — for example, on "reload" actions in the admin UI. This pushes the cache-invalidation decision to the caller without exposing cache internals.

**Rejected:** Application-layer cache (mixes infrastructure state into business logic; complicates unit testing); no cache at all (every install hits the network; slow user experience and fragile under registry unavailability); Redis or shared cache (unnecessary external dependency for a single-process runtime).

---

## ADR-B011 — Orchestrator pool management via board hook rather than polling

**Decision:** `TechLeadPool.Allocate` and `TechLeadPool.Deallocate` are called directly from the orchestrator's board mutation path when an item transitions into or out of `in-progress`. The pool does not poll the board for state changes.

**Rationale:** Hooking into the mutation path gives zero-latency allocation: the tech-lead is associated with an item at the exact moment the transition occurs, not after a polling interval. It also makes allocation causal — a tech-lead is guaranteed to exist before the assigned bot receives its task notification. Polling would require a separate goroutine, introduce latency, and risk double-allocation races.

**Warm standby pattern.** The last pool entry is never stopped on `Deallocate` — it is demoted to `idle`. This eliminates cold-start latency for the next allocation. The cost is one idle goroutine at all times once the pool has been used; this is considered acceptable given the typical cadence of kanban transitions.

**Serialised pool allocation.** All `Allocate` and `Deallocate` operations hold the pool mutex for their full duration (including the `spawnFn` call with a 1s timeout). This prevents double-allocation at the cost of brief serialisation on high-frequency board transitions. Given typical human-driven board update rates, contention is not expected to be a problem.

**Pool state file persistence.** Pool state is persisted to `<orchestrator-memory>/pool.json` on every mutation using the same atomic write strategy as `SessionFile`. Startup `Reconcile` re-derives liveness by calling the injected `isRunFn` predicate for each record, so the file is used as a hint rather than ground truth.

**Rejected:** Polling the board from a separate goroutine (latency, double-allocation risk); restarting all pool entries on process restart (expensive, breaks warm standby); blocking deallocation until `stopFn` completes under the mutex (could delay board transitions if stop is slow — `stopFn` is called after the entry is removed from the slice, outside the performance-critical path of the lock).

---

## ADR-B014 — ErrPluginNotFound defined in the domain layer, not infrastructure

**Decision:** `ErrPluginNotFound` is defined as `var ErrPluginNotFound = errors.New("plugin not found")` in the `domain` package. The infrastructure store (`LocalPluginStore`) returns `domain.ErrPluginNotFound`. The HTTP server checks `errors.Is(err, domain.ErrPluginNotFound)` to return HTTP 404.

**Rationale:** Sentinel errors that cross layer boundaries must live at the innermost layer that defines the concept — the domain. If `ErrPluginNotFound` were defined in the infrastructure package (`local/plugin`), the HTTP server (another infrastructure adapter) would need to import it, creating a lateral dependency between two infrastructure packages. This violates Clean Architecture: adapters must not depend on each other; both must depend only on the domain.

Placing the sentinel in the domain layer allows any adapter — HTTP server, CLI, future gRPC server — to check it via `errors.Is` by importing only the domain, which is always a legal dependency.

**Rejected:** Infrastructure-local sentinel with re-export (creates lateral infra-to-infra coupling); string comparison on `err.Error()` (fragile and not idiomatic Go); wrapping with a custom type defined in a shared `errors` package (unnecessary indirection; domain package already serves this purpose).

---

## ADR-B016 — read_skill over executable entrypoints for Claude Code plugins

**Decision:** Claude Code plugins declare a `plugin.json` manifest as their entrypoint rather than an executable script. When a bot calls a plugin tool whose entrypoint is a `plugin.json` file (detected by `filepath.Base(entrypoint) == "plugin.json"`), the MCP client reads the plugin's `commands/<name>.md` Markdown file and returns it as the tool result, rather than attempting to exec the JSON file.

**Rationale:** Claude Code plugins are designed to be executed by the Claude Code CLI, not by arbitrary subprocesses. Their "execution" model is: Claude reads the Markdown instructions and then carries out the described steps using its own built-in tools (`run_shell`, `read_file`, `write_file`, etc.). Attempting to exec `plugin.json` as a subprocess would always fail with a permission error and would not implement the intended behavior anyway.

The `read_skill` built-in tool and the `callPluginTool` JSON-entrypoint routing give bots a way to consume these plugins without any changes to the plugin format. Bots receive the Markdown instructions and follow them autonomously.

**Rejected:** Extracting a separate executable from plugin.json (would require changing the plugin format); adding an `is_markdown` flag to `PluginManifest` (unnecessary — the entrypoint filename is sufficient discrimination); requiring all plugins to have executable entrypoints (breaks compatibility with the Claude Code plugin ecosystem).

---

## ADR-B017 — CLIAgentRunner as a separate domain interface from codeagent.Provider

**Decision:** `domain.CLIAgentRunner` is a distinct interface from `domain.ModelProvider`. The existing `codeagent.Provider` implements `ModelProvider` (Invoke → InvokeResponse). The new `cliagent.SubprocessRunner` implements `CLIAgentRunner` (Run → string). These are not merged.

**Rationale:** The two abstractions serve different purposes:
- `ModelProvider.Invoke` is a turn-based prompt/response interface for the main agent loop. It must be composable with the ToolGater, BudgetTracker, and ContextManager harness middleware.
- `CLIAgentRunner.Run` is a long-running subprocess execution with streaming progress and optional stdin. It is invoked as an MCP tool, not as a model turn.

Merging them would force either interface to carry concepts irrelevant to the other (a model provider does not need stdin channels; a CLI runner does not need InvokeRequest/InvokeResponse message types). The interface segregation principle applies: keep interfaces focused on a single responsibility.

`ParseStreamLine` is the one piece of logic shared between the two worlds — it is now exported from `codeagent/stream.go` so the MCP client can post-process Claude Code stream-json output without duplicating the parser.

**Rejected:** Using `codeagent.Provider` as the CLIAgentRunner implementation (would expose model-provider concerns inside the MCP tool layer; would make testing harder since Provider requires a full InvokeRequest); a single generic "subprocess" interface (too broad; loses type safety about what kinds of subprocesses are expected).

---

## ADR-B018 — Plugin store pre-resolved in TeamManager.Run() before goroutine spawn

**Decision:** `TeamManager.Run()` resolves the plugin store and install directory once, synchronously, before launching any bot goroutines. The resolved values are passed as parameters to each `startBot` call rather than being written to struct fields inside goroutines.

**Rationale:** The previous design wrote `tm.pluginStore` and `tm.pluginInstallDir` from inside `startBot`, which ran concurrently for all bots. Any bot goroutine that began executing before the orchestrator goroutine wrote these fields received nil values and therefore saw no plugin tools. With pre-resolution, the data is available before the first goroutine starts, eliminating the race entirely. Local variables closed over by each goroutine are read-only after the goroutine starts — no locking required.

**Rejected:** A mutex protecting `tm.pluginStore` (adds locking overhead on every `ListTools` call; more complex than pre-resolution; still requires callers to handle the "not yet set" case); lazy initialisation inside each goroutine (each goroutine would race to load the same config file; adds redundant I/O and still requires synchronisation).

---

## ADR-B019 — File-backed in-memory store for agent notifications (not MariaDB)

**Decision:** `AgentNotificationStore` is implemented as a file-backed JSON store (`notifications.json` in the orchestrator memory directory), following the same pattern used by `DirectTaskStore` and `PoolStateFile`. No SQL schema or migration is required.

**Rationale:** The existing local storage pattern (load once at startup, mutate in-memory under a mutex, persist atomically via temp-file + `os.Rename`) is already proven by `DirectTaskStore`, `SessionFile`, and `PoolStateFile`. Notifications are orchestrator-local data — they do not need cross-node sharing or relational queries. Adding a MariaDB table would require a schema migration, a migration runner, and a DB dependency on the notification path, all of which add complexity with no operational benefit for single-process deployments. The JSON file approach keeps the system infrastructure-free and consistent with ADR-B008.

**Rejected:** MariaDB table (requires schema migration and DB dependency for local-only data); in-memory only without persistence (notifications would be lost on process restart, breaking the operator workflow); separate SQLite file (adds a CGO dependency and an additional storage layer for a small dataset).

---

## ADR-B015 — run_when as a composite queue mode rather than a flag combination

**Decision:** A fourth queue mode `run_when` was introduced to `domain.WorkItem.QueueMode` (alongside `asap`, `run_at`, `run_after`). It satisfies both a time condition and a predecessor-item condition before the `QueueRunner` dispatches the item. Either sub-condition may be omitted, in which case `run_when` degenerates to `run_at` or `run_after` respectively.

**Rationale:** Before `run_when`, operators had no way to express "start this task at 9 AM, but only if the previous task finished first." The options were to manually promote the item at the right time, or to chain two scheduled items with `run_after` and tolerate early dispatch if the predecessor finished before 9 AM. `run_when` eliminates both workarounds with a single scheduling rule.

An alternative design would have added separate boolean flags to the existing `run_at` / `run_after` modes (e.g., `also_require_predecessor`). A named composite mode was preferred because:
- The UI can present it as a distinct option with an intelligible label ("Run When both…") rather than showing confusing optional sub-fields inside a `run_at` form.
- `isReady()` in `QueueRunner` remains a clean switch on `QueueMode`; there is no need to branch on multiple flags per mode.
- The domain model remains self-documenting: the four mode names cover the four meaningful scheduling intents.

**Rejected:** Boolean flag `require_time_and_predecessor` added to existing modes (unclear semantics, harder to validate in the UI); separate `run_when_time` and `run_when_predecessor` fields without a composite mode (would require the runner to infer intent from combinations of empty fields); orchestrator-side scheduled jobs that poll and re-queue items (adds state machine complexity with no benefit over in-runner readiness checks).

---

## ADR-B020 — Native Go Nostr client (Option A) for Buzz support, not a Rust harness or CLI shellout

**Decision:** `internal/infrastructure/buzz/` implements `domain.ChannelMonitor` and `domain.RelayClient` as a native Go client over [`fiatjaf.com/nostr`](https://pkg.go.dev/fiatjaf.com/nostr), connecting directly to a `buzz-relay` WebSocket endpoint with NIP-42 auth (+ optional NIP-OA/NIP-AA attestation) and NIP-29-scoped `kind:9` channel messages. This is "Option A" from the Buzz Support PRD's Architecture Decision.

**Rejected alternatives:**
- **Option B — `buzz-acp` harness.** Block's own `crates/buzz-acp` is a Rust process implementing the Agent Client Protocol. Adopting it would mean shelling out to (or embedding) a second, Rust-toolchain-dependent runtime alongside boabot's own Go agent harness — duplicating the turn loop, worker dispatch, budget tracking, and calibrated-autonomy gates boabot already has, or bypassing them entirely for Buzz-originated tasks. It would also add a Rust build dependency to a project whose deployment NFR explicitly rules one out (single Go binary, no Rust toolchain, no sidecar container).
- **Option C — CLI shellout.** Piping through a Buzz CLI client as a subprocess avoids a new library dependency but reintroduces the same duplicated-control-plane problem as Option B (a second process owns the relay connection and event loop, not boabot), adds subprocess-management and restart complexity, and makes structured error handling (NIP-AA's `invalid:`/`restricted:` distinction, FR-009) dependent on parsing CLI stdout/stderr rather than a typed error from a library call.

**Rationale:** Option A keeps BaoBot as a single control plane: the existing worker harness, `BudgetTracker`, calibrated-autonomy gates, and `TeamManager` scheduling all apply unchanged to Buzz-triggered tasks (FR-021/FR-030 — "no Buzz-specific bypass"), because Buzz enters only as one more `domain.ChannelMonitor` implementation, exactly like the existing Slack adapter. Every P0 NIP primitive required (NIP-42 auth, NIP-OA/NIP-AA attestation, NIP-29 group messages, Schnorr sign/verify via `github.com/btcsuite/btcd/btcec/v2/schnorr`) was confirmed present and usable in `fiatjaf.com/nostr` during the Phase D research spike (`specs/260804-boabot-buzz-support/research.md`). Buzz is treated as a transport, not a new control plane.

**Validated versions:** `fiatjaf.com/nostr@v0.0.0-20260731140316-a8080728893f` — confirmed as the latest available pseudo-version both when the PRD was researched and when Phase D's D1 spike re-resolved `fiatjaf.com/nostr@master` against the module proxy (no newer commit existed at that point). **No specific `block/buzz` relay commit was pinned or validated against a live relay during this implementation run** — Buzz's own `docker-compose.yml` was not run as part of this job (see `plan.md`'s AC-handling note), and the PRD names the `block/buzz` GitHub repository and its NIP documentation as design references but does not itself cite a commit SHA. This is a known gap, not an oversight: it is called out explicitly on `implementation-notes.md`'s manual-verification checklist, alongside the `//go:build integration` stubs (`internal/infrastructure/buzz/conn_integration_test.go`) that exercise a live relay connection and are intended to be run — and their target commit recorded here — the first time this code is validated against a running `buzz-relay`.

**Known upstream issue (worked around, not blocking):** `fiatjaf.com/nostr`'s event-signing code has a genuine `unsafe.Pointer`/`checkptr` violation caught non-deterministically by `go test -race`; the workaround (`-gcflags=all=-d=checkptr=0`) is required for every `-race` run touching this package and is documented in full in `research.md` and `status.md`'s Phase D entry.

---

## ADR-B021 — `zalando/go-keyring` over `99designs/keyring` for the OS keystore secret provider

**Decision:** `internal/infrastructure/secret/keystore/` wraps [`github.com/zalando/go-keyring`](https://github.com/zalando/go-keyring) (pinned `v0.2.8`) rather than `github.com/99designs/keyring`.

**Rationale:** At the time of the Phase B research spike, `zalando/go-keyring` had a release in March 2026 and a push to its default branch in July 2026 — actively maintained. `99designs/keyring` was last released in December 2022, over three years prior, with no evidence of ongoing maintenance. `99designs/keyring`'s main advantage over `zalando/go-keyring` — bundling multiple backend options per OS (e.g. an encrypted-file fallback) — is not a differentiator here: `internal/infrastructure/secret.Store`'s own ordered provider chain (env → systemd → keystore → file, FR-040) already supplies exactly that kind of fallback at a layer above any single provider, so `zalando/go-keyring`'s narrower "one native backend per OS" scope (macOS Keychain via `security -i`/stdin, Windows Credential Manager via `wincred`, Linux Secret Service via `godbus/dbus`) is sufficient.

**Rejected:** `99designs/keyring` (dormant, last released Dec 2022); a hand-rolled per-OS wrapper over each platform's native API directly (`security`, `CredWriteW`, D-Bus Secret Service) — rejected as unnecessary reinvention of what `zalando/go-keyring` already provides, and higher-risk for the exact FR-052 concern (never passing a secret as a subprocess argument) that a mature, narrowly-scoped library has already solved and been used long enough to have that property verified by its own community.

**Verification note:** `zalando/go-keyring`'s darwin backend was confirmed, by reading `keyring_darwin.go` in the module cache during the B1 spike, to write via `security -i` and an stdin pipe — never a `-w` command-line argument — satisfying FR-052. This must be re-verified on any future `go-keyring` version bump; see the package doc comment on `internal/infrastructure/secret/keystore/keystore.go`.

---

## ADR-B022 — `AuthTagSecretName`/`LoadAuthTag` as a new `SecretStore`-resolved secret, pipe-delimited (FR-001)

**Decision:** The NIP-OA owner-attestation tag is resolved as a fourth `SecretStore` secret, `AuthTagSecretName = "buzz_auth_tag"` (`internal/infrastructure/buzz/token.go`), alongside the existing `PrivateKeySecretName`/`APITokenSecretName`. `LoadAuthTag(ctx, store, botName, agentPubkeyHex)` resolves it through the same ordered provider chain (env → systemd → keystore → file), parses a pipe-delimited `owner_pubkey_hex|conditions|sig_hex` string into the four-element `["auth", ...]` tag, and validates it via the existing `StaticAuthTagFunc`/`ValidateAuthTag` before returning an `AuthTagFunc`. `cmd/boabot/main.go`'s `buildBuzzMonitor` appends `buzzinfra.WithAuthTagFunc(fn)` to `opts` when a well-formed tag resolves; a missing or invalid secret is logged and the bot continues without owner attestation — the same log-and-continue shape already used for `buzz_api_token`, not a fail-closed path.

**Rejected alternatives:**
- **A new `boabotctl` output format / structured flag for the attestation tool.** OQ-R1 resolved this as an opaque, already-signed string handed off as-is — boabot has no need to reconstruct or re-derive the tag's fields, only to parse and validate the one it's given. Adding format-negotiation machinery to `boabotctl` would be speculative generality for a value that is produced once, out-of-band, by an external attestation tool.
- **A dedicated NIP-OA-specific config field instead of a `SecretStore` entry.** The tag is credential-shaped (it grants channel membership without enrollment) and must never appear in `config.yaml` or logs, the same constraint every other Buzz secret is already under (FR-051/FR-052) — routing it through `SecretStore` was the only option consistent with that constraint, not a new pattern.
- **Splitting the pipe-delimited string into three separate secrets** (`buzz_auth_tag_owner`, `buzz_auth_tag_conditions`, `buzz_auth_tag_sig`). Rejected as unnecessary provisioning friction: the three fields are produced together, by the same tool, as one unit, and delimiter safety was confirmed (`ValidateConditions` rejects whitespace and anchors every clause to `kind=`/`created_at<`/`created_at>` joined by `&`, so a literal `|` can never appear inside a valid `conditions` field; the other two fields are hex) — one opaque string round-trips losslessly.

**Why the original Phase H wiring gap happened.** The original feature's task breakdown (`specs/archive/260804-boabot-buzz-support/tasks.md`, Phase H) added `PrivateKeySecretName`/`APITokenSecretName` and wired both into `buildBuzzMonitor`, but never allocated a corresponding `BuzzConfig`/secret-resolution path for the auth tag — Phase H1's task scope covered only the two secrets it named, and no later phase task revisited it. The tag's *signing and validation* logic (`nipoa.go`'s `SignAuthTag`/`ValidateAuthTag`) was built and tested in an earlier phase, creating the appearance of completeness; only the wiring that connects a resolved secret to `WithAuthTagFunc` was missing. This is why the review PRD (FR-001) characterized it as a wiring gap rather than a missing capability — the capability existed, unreachable, one config path short of being usable.

---

## ADR-B023 — Attach-generation counter + `rc.mu`-guarded ordering for `RelayClient` subscribe/reconnect/close (FR-002/FR-003)

**Decision:** `subEntry` gained a `generation int` (bumped by `attachSub` on every attach) and a per-entry `sync.WaitGroup` (`wg`), replacing the previous single-slot `pumpDone` channel. `attachSub` does not attempt to *prevent* a concurrent double attach — distinguishing "a legitimate re-attach after a real reconnect" from "a duplicate attach of a not-yet-attached entry" is not knowable locally, and a false rejection would silently drop a subscription. Instead every attach is made *safe*: each successful attach captures its own generation and registers with `entry.wg`/`rc.pumpWG` before starting its pump; `pumpSub` compares its captured generation against the live `entry.generation` before every forward and exits silently the moment it is superseded, so at most one generation is ever actively delivering to `entry.out`; `removeAndClose`/`Close` wait on *every* generation ever started (not just the latest) before closing the channel, closing the "orphaned pump sends after close" panic path.

This is the most consequential fix in this review-fix round: it eliminates an unrecovered `send on closed channel` panic that would crash the entire `boabot` process (not just the Buzz monitor) on an ordinary, expected event — a concurrent reconnect racing a fresh `Subscribe` call, or racing a `Close()`.

**Deliberate departure from the review PRD's own suggestion — `rc.mu`-guarded ordering, not `atomic.Bool`.** The review PRD's FR-003 Green guidance proposed `RelayClient.closed` as `atomic.Bool` so it could be read from `attachSub`'s `subMu`-held section without acquiring `rc.mu`. The implementation instead performs the entry-existence check, the generation bump, and the `wg`/`pumpWG` `Add()` calls all inside one `rc.mu`-guarded critical section in `attachSub` — the same lock `Close()` uses to set `rc.closed = true`. Because mutex critical sections are totally ordered, this strictly sequences the two operations under any interleaving: either `Close()`'s transition completes first (a later `attachSub` observes it and refuses before calling `Add()`), or `attachSub`'s `Add()` completes first while `rc.mu` is held — which, via the mutex's own memory-ordering guarantee, makes that `Add()` visible-before `Close()`'s later `rc.mu.Lock()` and therefore before its `rc.pumpWG.Wait()`, avoiding the `Add()`/`Wait()` race Go's own `sync.WaitGroup` documentation warns produces undefined behavior. A mutex-based ordering achieves the same guarantee as `atomic.Bool` without introducing a second synchronization primitive, given the check-and-bump-and-add already had to be atomic together regardless — `atomic.Bool` would have made `closed` itself race-safe in isolation but would not, on its own, have ordered it against the generation bump and the `wg.Add()`, which is the actual invariant FR-003 needs.

**Rejected alternative — continuous-lock-holding across `Subscribe`'s register→attach sequence.** Holding `subMu` from `rc.subs[id] = entry` through `attachSub`'s own `conn.Subscribe(...)` network call would close the race window entirely, but performs a network call under lock — exactly the "liveness concern" the review PRD's own Refactor note flags. A slow or stuck `conn.Subscribe` would block every other `subMu`-guarded operation, including a concurrent `reconnect()`'s `resubscribeAll` (which needs `subMu` to snapshot `rc.subs`), trading the original race for a new deadlock-adjacent liveness risk. The generation-counter approach closes the race without ever holding a lock across a network call.

**Lock-ordering invariant, verified by direct code review (WS-B4), not inferred from an absent deadlock in one test run:** every `rc.mu`/`subMu` acquisition site in `relay_client.go` and `reconnect.go` was read directly. Exactly one place holds both simultaneously — `attachSub`'s critical section described above — always `rc.mu` outer, `subMu` inner, released in reverse order. No site acquires `subMu` and then attempts `rc.mu` while still holding it, so there is no AB-BA deadlock risk. This "no new lock-ordering dependency" invariant is carried as an explicit, standing constraint on any future change to either file.

---

## ADR-B024 — `os.Link`-over-`os.Rename` for the process-singleton lock's atomic publish (FR-004)

**Decision:** `AcquireLock`'s write path (`internal/infrastructure/buzz/lock.go`) publishes the PID lock file atomically via `publishLockFile`: the PID is written to a same-directory temp file (`os.CreateTemp`), `fsync`'d and closed, then published under the lock's final name via `os.Link(tmpPath, path)` (`EEXIST`-checked; falls through to the existing stale-lock reclaim path on conflict).

**Why `os.Rename` was rejected outright, not merely deprioritized.** `os.Rename`'s own documented behavior is: "If newpath already exists and is not a directory, Rename replaces it" — on every platform. Two racing publishers would both `Rename` onto the same `path` and both succeed, providing **no mutual exclusion at all** — this is worse than the original TOCTOU bug FR-004 fixes, which at least failed non-deterministically rather than never detecting the conflict. `os.Rename` is therefore not a viable fallback for this requirement (atomic create-with-content-or-fail-if-target-exists); `os.Link` is the only stdlib primitive in the standard library that carries that semantic, so there was never a live choice to weigh it against — this decision is a correction of `research.md`'s original two-primitive framing (`architecture.md`'s AD-3), not a preference between two equally valid options.

**What is verified versus inferred about Windows behavior — kept precise, not collapsed into "confirmed cross-platform" as an earlier draft of this decision did:**
- *Confirmed by direct stdlib source inspection:* `os/file_windows.go`'s `Link` calls `syscall.CreateHardLink` and wraps errors in `*os.LinkError`; `os.LinkError.Unwrap()` returns the underlying error for `errors.Is` traversal; `syscall/syscall_windows.go`'s `Errno.Is()` maps both `ERROR_ALREADY_EXISTS` (183) and `ERROR_FILE_EXISTS` (80) to `oserror.ErrExist` — so `errors.Is(err, os.ErrExist)` is a reliable check on the Go side, cross-platform, *given* that Windows returns one of those two codes.
- *Not independently verified:* that `CreateHardLinkW` actually returns one of those two codes when the destination already exists. No Win32 API documentation was read and no live Windows run was performed during this fix.
- *Indirect corroborating evidence:* Go's own `$GOROOT/src/os/os_test.go` `TestHardLink` links onto an already-existing name and asserts `IsExist(err.Err)`, with no Windows build-tag skip — this runs on Go's Windows CI builders as part of every stdlib release, the closest confirmation available without a local Windows machine.
- `GOOS=windows GOARCH=amd64 go build`/`go vet` of `internal/infrastructure/buzz/...` both succeed, confirming the code path compiles for that target — this says nothing about runtime error-code behavior.
- **Bounded residual risk:** `Monitor.Start` treats any non-nil `AcquireLock` error identically — log, decline to attach the Buzz monitor, leave everything else running. If `CreateHardLinkW` ever returned an unmapped errno on a future Windows/Go combination, `publishLockFile` would surface a hard, mis-typed error rather than silently granting a second lock — a diagnostics problem, never a double-granted-lock safety hole.

This resolves `architecture.md`'s AD-3, which recorded the cross-platform question as explicitly OPEN pending this verification — AD-3 is no longer open.

---

## ADR-B025 — `maxContentLen` as a package constant, not a `Monitor.Config` field (FR-005)

**Decision:** Inbound `kind:9` channel-message content is bounded by `maxContentLen = 64 * 1024` (64 KiB), a package constant in `internal/infrastructure/buzz/monitor.go`, checked early in `dispatch` (after the empty-content check, before the author gate) — oversized content is rejected with a structured `slog.Warn` (`event_id`, `content_len`, `max_content_len`, `channel`) rather than becoming a `TaskPayload`.

**Rationale for the bound.** 64 KiB is generous for any legitimate multi-paragraph chat-style message (comparable to typical Nostr relay/client content caps) while closing the uncontrolled-token/cost-spend concern FR-005 raised. This is defense-in-depth against a theoretical concern, not a response to an observed exploit — the bound deliberately favors generosity over tightness.

**Constant, not config field — and its promotion path.** Consistent with FR-007/OQ-R2's precedent (don't add operator-tunable surface without a demonstrated need — see the `BuzzConfig` doc comment on reconnect backoff), no concrete need for a non-default bound surfaced during implementation: no existing test fixture required one, and no operator-facing requirement calls for tuning it. Should one surface later, the promotion path is already documented directly on the constant in `monitor.go`: add `Config.MaxContentLen`, defaulting to the constant when zero — mirroring how `PresenceInterval` is already handled — rather than introducing a new pattern at that time.

**Scope note — `discovery.go` deliberately untouched.** FR-005's acceptance criterion targets a `kind:9` event exceeding the bound being rejected before becoming a `TaskPayload`. `discovery.go`'s `kind:39000` metadata handling never builds a `TaskPayload` (it only logs a screened `name`/`about` pair), and `kind:39000` is relay-signed NIP-29 metadata — a narrower attacker-control surface than `kind:9`, which any channel member can publish. Every tag-indexing site (`trigger.go`, `discovery.go`, `nipoa.go`) was independently confirmed length-safe by the review itself, so no tag-count bound gap was left open by limiting this fix to `dispatch`'s content check.

---

## ADR-B026 — `boabot -acp`: a thin ACP adapter over the existing `Worker`, not a second control plane (supersedes ADR-B020's Option B rejection for this narrower use)

**Decision:** `internal/infrastructure/acp/` implements [`github.com/coder/acp-go-sdk`](https://pkg.go.dev/github.com/coder/acp-go-sdk)'s `acp.Agent` interface as a thin adapter over BaoBot's existing `domain.Worker`. `cmd/boabot -acp -config <persona.yaml>` loads exactly one persona's config directly (not `team.yaml`) and serves it as an Agent Client Protocol agent over stdio — registrable as a `buzz-acp` custom harness (`--agent-command boabot --agent-args -acp`), the same mechanism Buzz uses to run `goose`. See `specs/260813-boabot-acp-stdio-harness-support/` for the full spec, research, and architecture record.

**Why this doesn't contradict ADR-B020.** ADR-B020 rejected running the *native Buzz relay integration* under `buzz-acp` ("Option B") — that would have meant `buzz-acp` owning the relay connection AND the turn loop for BaoBot's always-on team identities, duplicating (or bypassing) `TeamManager`, worker dispatch, and calibrated-autonomy gates. This ADR is a different, narrower thing: an *additional*, opt-in mode where `buzz-acp` owns only the relay connection and identity (as it already does for `goose`), and BaoBot's existing `Worker` — the same one native daemon mode uses — executes the actual turn. There is no relay code, no `ChannelMonitor`, and no `TeamManager` involvement in `internal/infrastructure/acp/` at all; ACP mode services exactly one persona in one process, with `buzz-acp` driving it exactly the way it drives any other ACP-speaking harness. Native `ChannelMonitor`-based Buzz integration (ADR-B020) remains the recommended path for an always-on, BaoBot-owned team identity; ACP mode is for the case ADR-B020's PRD itself flagged as a legitimate future need — exposing a BaoBot persona to a Buzz workspace without giving it its own relay identity/key.

**Confirmed via the real `buzz-acp` binary, not the public spec alone.** `strings` on the bundled `/Applications/Buzz.app/Contents/MacOS/buzz-acp` confirmed its actual method set (`initialize`, `session/new`, `session/prompt`, `session/update`, `session/cancel`, `session/request_permission`, `session/set_config_option`, `session/set_model`) and — critically — that `buzz-acp` runs a **persistent pool of long-lived agent processes reused across many turns**, not one process per turn (`--agents`/`--lazy-pool`/`--idle-pool-sleep`/respawn-with-backoff flags). `boabot -acp` is designed as a long-lived process accordingly, not a single-turn-then-exit one.

**`domain.Worker.Execute` has no incremental-output hook — the keep-alive mechanism compensates, and this is load-bearing, not cosmetic.** `buzz-acp`'s `--idle-timeout` kills a turn after N seconds of stdout silence. Since `Worker.Execute` blocks until the whole turn completes, `internal/infrastructure/acp` runs a concurrent keep-alive emitter (progress-driven when the `Worker` offers `WithProgressHandler`, a ticker fallback otherwise) alongside the blocking call, so a long tool-using turn is never silently killed by the host.

**Rejected alternative — adding streaming to `domain.Worker`.** Would have been the "more correct" long-term fix for true token-level streaming, but changes a core domain interface used by every existing `ChannelMonitor` and the whole `TeamManager` path, for a benefit (incremental ACP updates) the keep-alive mechanism already delivers adequately for buzz-acp's actual requirement (avoid idle-timeout kills). Left as a candidate future enhancement, not built here.

**No new domain or application interfaces.** `internal/infrastructure/acp` reuses `domain.Worker`/`Task`/`TaskResult`/`WorkerFactory` unchanged, and constructs an `application.ExecuteTaskUseCase` directly using the same provider-factory/memory/vector/MCP-client construction primitives `TeamManager.startBot` uses (duplicated at small scale rather than extracted from `startBot`'s larger, orchestrator-entangled body — a deliberate, documented v1 scope decision, not an oversight).

**Verified against the real compiled binary.** `cmd/boabot/acp_integration_test.go` (`//go:build integration`) builds the actual `boabot` binary, spawns it as a real OS subprocess, and drives it over real stdio pipes using `coder/acp-go-sdk`'s own client-side connection — proving the `initialize`/`session/new`/`session/prompt` contract and the keep-alive mechanism across a real process boundary, substituting a local mock OpenAI-compatible HTTP server for the model provider (no network access or API key required). It does not exercise the real `buzz-acp` binary itself, which has no dry-run mode and requires a live Buzz relay connection for every code path — standing up a real or faithfully-mocked Nostr relay with NIP-42 auth was judged out of scope for this pass.

---

## ADR-B027 — Fallback publish when a turn produces output but the model never calls `buzz messages send`

**Decision:** `internal/infrastructure/acp/publisher.go` adds a `Publisher` interface (`Publish(ctx, channelID, content) error`), defaulting to a `cliPublisher` that shells out to `buzz messages send --channel <id> --content <text>` (same binary resolution and inherited process environment a persona's own `run_shell` tool call would use). `turn.go`'s `Prompt` handler calls it once, after a successful turn, only when both hold: `result.Output` is non-empty, and `result.ToolCalls` (new field on `domain.TaskResult`, populated by `execute_task.go`'s tool loop) contains no `run_shell` call whose command matches `buzz messages send`. The channel UUID is parsed out of the turn's own instruction text via `extractChannelID` (a regex against the `[Context]` block's `Channel: ... (#uuid)` line buzz-acp already prepends to every prompt). A DM-scoped turn has no such line, so the fallback is skipped there — `Publish` is never attempted without a concrete channel target.

**Problem this closes — not a wiring bug, a model-judgment gap.** `buzz-acp`'s system prompt tells the persona "you MUST publish it, use `buzz messages send`" — but that is a soft instruction, not something the harness enforces. `boabot`'s ACP `Prompt` handler always emits `result.Output` as an ACP `session/update` regardless of whether the model's tool loop ever actually called `buzz messages send`; ACP has no concept of a persisted channel message, so an un-published reply is only ever visible as an ephemeral notification in the host UI. Live debugging (2026-08-13/14, against a real `buzz-acp`-managed "Boa" agent) confirmed this is a real, observed failure mode, not a hypothetical one — turns completing in a few seconds with `stop_reason=end_turn` and substantive `Output`, with no corresponding `buzz messages send` tool call anywhere in the turn's history. Function-calling models are known to skip a documented "must call this tool" instruction when a reply doesn't intuitively feel like it needs publishing (a plain greeting, e.g.) — the fix is to stop relying on the model remembering, and let the harness guarantee it instead.

**Why detection is a substring check on `run_shell`'s command, not a structured tool.** `boabot` has no dedicated `buzz_send_message` MCP tool — the persona reaches the `buzz` CLI exclusively through the generic `run_shell` tool (`internal/infrastructure/local/mcp/client.go`), passing an opaque shell string. `calledPublish` therefore checks `strings.Contains(cmd, "messages send")` across every `run_shell` call recorded on the turn. This has false-negative risk against pathological quoting/spacing the model might produce, but matches the exact invocation the system prompt itself documents as the standard case, and a false negative only costs a redundant (harmless) fallback publish — not a missed one.

**Best-effort, not turn-blocking.** A fallback `Publish` failure is logged (`slog.Warn`) and does not change the turn's `stop_reason` or fail the ACP call — the existing `a.emit(...)` step still always runs afterward, preserving today's ACP-echo/toast visibility as a floor regardless of whether the real publish succeeded.

**Scope note.** This intentionally only guards the ACP path (`internal/infrastructure/acp`), not native `ChannelMonitor`-based Buzz mode (ADR-B020/ADR-B026's distinction) — native mode's `Monitor` owns publishing its own replies directly and never depends on the model choosing to call a CLI tool for it.

## ADR-B028 — Multi-agent Buzz: `TeamManager`-side builder hook, `BuzzTaskDispatcher` seam, per-persona bridge isolation

**Decision:** Four coupled decisions extend native daemon mode's single-identity Buzz wiring to N `team.yaml` personas (spec: `specs/260814-boabot-native-daemon-mode/`):

1. **Monitor construction moves inside `TeamManager.Run()`, invoked via a caller-supplied `team.BuzzMonitorBuilder` hook (`main.go` still builds the actual `buzzinfra.Monitor`).** `Run()`'s shared `DirectTaskStore`/`BoardStore` (needed by decision 2 below) don't exist until `Run()` creates them, and every bot's `RunAgentUseCase` snapshots `tm.monitors` synchronously before any goroutine starts — so monitor registration must happen inside `Run()`, before its bot-goroutine loop, not in `main.go` beforehand (too early: no shared stores) or inside a per-bot goroutine (a race against other bots' `tm.monitors` snapshot). `main.go` still owns all `internal/infrastructure/buzz` construction (`buildBuzzMonitor`, `SecretStore` resolution, relay client) via the `BuzzMonitorBuilder` closure it registers with `mgr.WithBuzzMonitorBuilder(...)` before calling `mgr.Run(ctx)` — `internal/application/team`'s own signature for that hook only references `domain`/`config`/`queue` types, so it never imports `internal/infrastructure/buzz` itself (`WithChannelMonitor`'s pre-existing FR-034 rule, unchanged).
2. **The Buzz → Dispatcher/DirectTaskStore/BoardStore bridge is a `domain`-defined seam (`BuzzTaskDispatcher`/`BuzzDispatchResult`), implemented in `internal/application/orchestrator` (`BuzzTaskBridge`), consumed by `internal/infrastructure/buzz.Monitor` via a `WithTaskDispatcher` option.** `Monitor.dispatch()` needing to call *up* into application-layer orchestration (not just down into domain interfaces) inverts the usual infra→domain dependency direction; defining the seam in `domain` (mirroring the pre-existing `TaskDispatcher`/`ScheduledTaskDispatcher` pattern) keeps `Monitor`'s own import list domain-only while letting the concrete bridge live in `application` where the orchestration logic (NL-scheduling reuse, event dedup, board-item creation) belongs. `WithTaskDispatcher` is optional — unset, `dispatch()` falls back to the pre-existing direct `queue.Send` behavior verbatim — deliberately, to avoid rewriting roughly twenty pre-existing `Monitor` dispatch-path unit tests that have nothing to do with the bridge; production wiring (`main.go`) always supplies a bridge, so the fallback is dead code in shipped configuration.
3. **Each Buzz-enabled persona gets its own `BuzzTaskBridge`/`ChatTaskManager` instance, not one shared across all personas.** A shared instance's `ChatTaskManager` scheduling-confirmation pending map, keyed only by Buzz channel UUID, would let a bare "yes"/"cancel" from persona B's conversation confirm/cancel persona A's still-pending intent whenever both personas are mentioned in the same channel — a real FR-004 cross-talk bug, not a hypothetical one, since Buzz channels are inherently multi-agent by design (that's this feature's whole point). Per-persona instance isolation (event-ID dedup map, scheduling pending map) is the free, zero-extra-code fix instead of a composite `(botName, channelUUID)` key on a shared instance.
4. **Added `domain.TaskPayload.Source`, rather than adding an unreachable `"buzz"` branch to `execute_task.go`'s existing `"chat"` check.** `execute_task.go:100`'s chat-provider-selection check reads `task.Source`, which `run_agent.go`'s `handleTask` populates from `domain.Message.From` — the *dispatching bot's own name* (e.g. `"orchestrator"`), never the literal string `"chat"` or `"buzz"`. Concretely, this meant the pre-existing `"chat"` branch was already dead code for every task ever dispatched through `LocalTaskDispatcher` (chat, board, and now Buzz) — nothing set `Message.From = "chat"`. Bolting on a `"buzz"` branch next to it would only have added a second, equally-unreachable branch. **Decision:** added `Source string` to `domain.TaskPayload` (`internal/domain/message.go`), set from `task.Source` by `LocalTaskDispatcher.sendMessage`, and preferred over `Message.From` in `run_agent.go`'s `handleTask` when non-empty (empty falls back to today's `Message.From` behavior, fully backward compatible for every other message producer, e.g. `internal/infrastructure/acp`). This makes the `"chat"`/`"buzz"` check reachable for the first time for both sources. **User-visible side effect, deliberate and in scope:** an operator with `models.chat_provider` configured now actually gets that provider for chat-dispatched tasks, not just Buzz-dispatched ones — previously silently inert. Documented in `user-docs/Claude-Adoption-Config.md` and `user-docs/OpenAI-Adoption-Config.md`. Board- and operator-sourced tasks are unaffected (their `Source` values never match either branch).

**Rejected: reusing `DirectTaskSourceChat` for Buzz-originated tasks.** Would conflate two distinct origin channels in the UI/logs, defeating the NFR-Observability requirement ("multi-agent conversations must be traceable per-agent ... not lumped together"). `DirectTaskSourceBuzz` is additive (no exhaustive switch over the enum existed to update — confirmed by grep before adding it).

**Related, exposed-not-created bug fix bundled into this same change:** `TeamManager.Run()`'s team-entry pre-registration loop (`tm.router.Register(e.Name, 0)`, unconditional) and `buildBuzzMonitor`'s own registration both assumed they'd never collide — true only because native-daemon-mode-plus-team-enrolled-Buzz-persona had never actually been exercised together before (the only shipped Buzz path was ACP mode, which has no `team.yaml`/`TeamManager` at all). Both now use a new non-panicking `queue.Router.Lookup(botName) (*Queue, bool)` instead of unconditional `Register`, so a bot name registered before `Run()` (by Slack, or by an earlier persona's Buzz monitor) is reused rather than triggering `Router.Register`'s duplicate-name `panic`. See `specs/260814-boabot-native-daemon-mode/implementation-notes.md` for the full trace and regression tests.

---

## ADR-B029 — Buzz DM support via `nip17`'s high-level API over the single-relay seam; `PublishRaw` as a distinct unsigned-publish primitive; silent-ignore as the default for unauthorized DMs

**Decision:** Four coupled decisions extend native daemon mode's Buzz integration with NIP-17 direct-message reachability and complete threaded-reply support (spec: `specs/260814-buzz-dm-and-thread-support/`):

1. **Use `nip17`'s primitives (`PrepareMessage`, `GiftUnwrap`), not raw `nip44`/`nip59` calls — but not `nip17.ListenForMessages`/`PublishMessage` themselves.** The vendored `fiatjaf.com/nostr` dependency's `nip17` package already implements NIP-17's privacy-preserving behavior correctly (ephemeral gift-wrap keys, randomized timestamps); reimplementing at the `nip44`/`nip59` level would risk subtly defeating those properties for no benefit. However, `nip17.ListenForMessages`/`PublishMessage` — the package's own subscribe/publish convenience wrappers — both require a `*nostr.Pool`, which this codebase does not have and was never going to introduce: `Monitor` is built entirely around a single-relay `relayClient` abstraction, and FR-201 explicitly requires DM reachability over "the same relay connection" a persona already uses for channel participation. This was not visible from spec.md/architecture.md/research.md — all three assumed the Pool-based functions could be called directly; the gap surfaced only when the vendored library's actual source was read before writing wiring code. The resolution: inbound DM handling calls `nip59.GiftUnwrap` directly (the identical primitive `nip17.ListenForMessages` calls internally, just without the `Pool`-owning wrapper), and outbound DM replies still call `nip17.PrepareMessage` directly (it needs only a `nostr.Keyer`, no `Pool`) but publish its two gift-wrapped outputs through the existing `relayClient` seam (decision 2) instead of `nip17.PublishMessage`.

2. **Add `domain.RelayClient`/`relayClient.PublishRaw(ctx, Event) error`, distinct from the existing `Publish`.** `Publish` always sets `nevt.PubKey = rc.pk` and re-signs the event with the client's real key before sending — correct for every channel/presence event this client publishes today, but wrong for a NIP-17 gift-wrap. A gift-wrap's outer envelope is *always* signed with a freshly generated ephemeral key (`nostr.Generate()`), never the sender's real key; routing it through `Publish` would silently overwrite that ephemeral signature with the persona's real one, defeating NIP-17's sender-anonymity property outright, and would *also* break the recipient's ability to decrypt the message at all — `nip59.GiftUnwrap` derives the seal's conversation key from the gift-wrap envelope's own `PubKey` field, which `Publish` would have just replaced. `PublishRaw` forwards an already-signed event verbatim through the existing `relayConn.Publish(ctx, nostr.Event)` seam (which itself never signs anything), rather than introducing a second signing path or a boolean flag on `Publish` that would leave every other caller needing to reason about which mode applies.

3. **Unauthorized DMs are silently ignored, not sent a decline reply (FR-204's default).** DM dispatch reuses the exact same author-gate (`respond_to`/`respond_to_allowlist`) channel `@mention`s already use — no new gating mechanism. When a DM from a sender outside that gate arrives, it is logged and dropped with no reply sent back to the sender. This is the more conservative of the two options considered: a decline reply would confirm to an arbitrary Nostr identity that a given persona exists and is listening, a materially larger exposure than curated channel membership already carries (channel membership requires relay-confirmed enrollment; a DM requires only knowing a pubkey). **This interacts with the existing author-gate default in a way operators must understand, not just DM-specific behavior in isolation:** the gate is opt-in — an unconfigured `respond_to`/`respond_to_allowlist` (the default) allows every sender, on both the channel and DM paths. For channel `@mention`s this is bounded by relay-curated membership; for DMs it is not. An operator who wants DM-reachability restricted to known senders must explicitly configure the gate — leaving it unset does not silently restrict DMs the way it might be assumed to. This default is flagged as operator-overridable in principle (a future decline-reply mode is a small, isolated addition if wanted later) but no such option exists in this release.

4. **`DirectTask.ThreadID` for Buzz tasks is now keyed by the NIP-10 thread root (or `dm:<pubkey>` for a DM), not the Buzz channel UUID — extending ADR-B028's per-persona isolation with per-thread isolation.** ADR-B028 gave each Buzz-enabled persona its own `BuzzTaskBridge`/`ChatTaskManager` instance specifically to prevent one persona's scheduling-confirmation state from cross-talking with another's. That isolation was necessary but not sufficient: within a single persona's own bridge, every concurrent thread in one channel was still sharing one `ChatTaskManager` pending-intent slot, because the value passed as `ThreadID` was the channel's UUID rather than the specific thread's root event. Two people confirming or cancelling scheduled tasks in two different threads of the same channel, with the same persona, could therefore collide. The fix is a one-argument change at the `monitor.go` dispatch call site (pass the already-computed thread root instead of the channel UUID) — not a `pendingMap` restructure, since `ChatTaskManager.pendingMap` was already keyed by an opaque string and only the value supplied was wrong.

**Bundled, accepted tradeoff — not a fifth architectural decision, but disclosed here because it changes visible operator-facing behavior.** Recording each Buzz-dispatched task's outbound reply correctly `ThreadID`-keyed (needed so `ChatStore.List(threadID)` can find it for the next turn's history replay) required adding an explicit `Monitor.recordOutbound` append, deliberately *not* routed through `team_manager.go`'s existing generic `WithTaskResultHandler`/`chatMessageThreadID` mechanism — that mechanism returns `""` for `DirectTaskSourceBuzz` by design, predating this feature, specifically to keep Buzz activity out of the `GET /api/v1/chat` global feed's exclusion convention. The generic handler still runs unconditionally for every task regardless of source, so a Buzz reply is now recorded twice: once under `ThreadID=""` (the generic handler's copy, written regardless of publish success) and once under the correct `ThreadID` (`recordOutbound`'s copy, written only after a successful publish). Net effect: Buzz/DM conversations now surface in the operator's global chat feed (previously excluded), and every reply appears as two rows there. **Rejected fix, this pass:** changing `chatMessageThreadID` to pass Buzz `ThreadID`s through and removing the duplicate — rejected because `recordOutbound` only fires after a successful publish, so relying on it alone would mean a relay-publish failure leaves *no* chat record of the bot's output at all, a real regression from today's behavior where the generic handler's copy is written unconditionally. A proper fix needs the generic handler to stop writing its `""`-keyed copy for Buzz sources while `recordOutbound` (or an equivalent hook the generic handler itself calls) becomes the single source of truth for both success and failure — a small but real change to shared, non-Buzz-owned code (`team_manager.go`), judged out of scope for this pass without explicit go-ahead. Conversation continuation itself is unaffected (`ChatStore.List` filters by thread ID, so the `""`-keyed duplicate is invisible to history replay) — this is a chat-feed-noise issue, not a correctness issue.

**Rejected alternative — a new `buzz.dm_enabled` config flag to gate DM reachability separately from channel Buzz.** Considered and rejected: spec.md's stated goal is that any Buzz-enabled persona with a provisioned key is reachable via DM using its existing channel identity, with no separate DM key. Adding a separate enable flag would create a configuration surface with no clear operator benefit — the private key that makes DM decryption possible is the same key that makes channel participation possible, so there is no meaningful "channel-only" identity to protect by withholding a DM flag. Operators who want to restrict *who* can reach a persona by DM have the author-gate (decision 3) for that; a flag to disable DM listening entirely was not requested by any FR and was not built.

---

## ADR-B030 — ACP mode gains chat-provider/board/plugin/CLI-tool parity with native mode; mid-task clarifying questions stay out of scope

**Decision:** `specs/260815-acp-harness-feature-parity/` closes two of the three confirmed wiring gaps between `boabot -acp` and native daemon mode's task execution — both already share the identical `ExecuteTaskUseCase.Execute` loop and `maxToolIterations = 50` cap (`execute_task.go:12,122-178`), unchanged by this work. `isConversationalSource` (`execute_task.go`) now also matches the literal `Source: "acp"` string `internal/infrastructure/acp/turn.go` sets, and `cmd/boabot/acp.go`'s `buildACPAgent` gained two extracted, directly-testable helpers — `buildACPWorker` (chat-provider selection) and `buildACPMCPOptions` (board/plugin/CLI-tool wiring) — each mirroring `team_manager.go`'s exact native-mode gating conditions (persona type for the board store, `Orchestrator.Plugins.InstallDir` for the plugin store, unconditional-runner-plus-per-tool-`Enabled` for CLI tools) rather than an umbrella `orchestrator.enabled` flag, which native mode itself doesn't use to gate any of these features either. See `implementation-notes.md` for the full trace, including a scope addition beyond FR-401's literal text: `acp.go` never called `WithChatProvider` at all prior to this change, so the `isConversationalSource` string-match extension alone would have been dead code in production — the same failure mode ADR-B028 decision 4 already documented once for the pre-existing `"chat"` branch.

**The third gap — mid-task clarifying questions — remains explicitly unsupported in ACP mode, and stays that way pending an upstream `buzz-acp` change outside this repo's control.** Native mode has an ask-channel (`ExecuteTaskUseCase.askCh`/`WithAskChannel`, wired per-bot by `TeamManager.startBot`) that lets a worker pause mid-task, publish a question, and resume once an operator or Buzz participant answers. ACP mode's `Prompt` handler (`internal/infrastructure/acp/turn.go:19-130`) is a single synchronous request/response turn with no interrupt/resume point of its own — closing this gap would mean *adding* one, not wiring an existing mechanism the way FR-402–405 did.

**The relevant mechanism exists in the ACP protocol itself, but is explicitly unstable and requires client-side support `buzz-acp` doesn't implement.** The vendored `github.com/coder/acp-go-sdk v0.13.5` defines `AgentSideConnection.UnstableCreateElicitation`/`UnstableCompleteElicitation` (`agent_gen.go:471-477`), sending `elicitation/create`/`elicitation/complete` JSON-RPC calls to the ACP client — exactly the "pause a turn, ask the peer a question, resume with the answer" shape this gap needs. Both names carry the SDK's own `Unstable` prefix, its convention for marking a protocol extension not yet part of the stable ACP spec. On the receiving side, `client_gen.go:10-49` type-asserts the registered `sdk.Client` against an unexported optional interface (`interface{ UnstableCreateElicitation(...) }`) before dispatching `elicitation/create` — if the client doesn't implement it, the SDK has no fallback path; the request simply cannot be serviced. `buzz-acp` is exactly such a client (`ADR-B026`'s confirmed method set via `strings` on the real bundled binary: `initialize`, `session/new`, `session/prompt`, `session/update`, `session/cancel`, `session/request_permission`, `session/set_config_option`, `session/set_model` — no `elicitation/*` handling observed). Implementing this side is Buzz's, not `boabot`'s, to build.

**Why this isn't worked around some other way.** `RequestPermission` (`sdk.Client`'s stable, implemented method — see `acp_integration_test.go`'s `testACPClient.RequestPermission`) is a structurally different primitive: a bounded choice among host-supplied options for a specific tool-call authorization decision, not an open-ended natural-language question a persona can pose mid-task. Repurposing it would misrepresent what it's for to `buzz-acp` and to any other ACP host registering this harness. A custom out-of-band channel (e.g. a dedicated MCP tool that blocks until an external answer arrives) was also considered and rejected: `boabot -acp` has no channel of its own back to the human on the other end of a `buzz-acp`-mediated conversation — that's precisely the connection `buzz-acp` owns and ACP's own elicitation extension is the only currently-defined path for reaching it.

**Non-Goal, not a silently dropped requirement.** `specs/260815-acp-harness-feature-parity/spec.md`'s Non-Goals section states this explicitly, and this entry exists so a future contributor checks upstream `buzz-acp`/`acp-go-sdk` elicitation-support status before re-attempting the work, rather than rediscovering the same unstable-protocol/upstream-dependency finding from scratch. Should `buzz-acp` ever implement the client side of `elicitation/create`/`elicitation/complete`, closing this gap becomes a matter of wiring `UnstableCreateElicitation` into `ExecuteTaskUseCase`'s existing `askCh`/`WithAskChannel` mechanism from the ACP side (`turn.go`) — the domain-level ask-channel abstraction native mode already uses needs no redesign, only a second producer.
