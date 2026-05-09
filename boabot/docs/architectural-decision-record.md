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

## ADR-B015 — run_when as a composite queue mode rather than a flag combination

**Decision:** A fourth queue mode `run_when` was introduced to `domain.WorkItem.QueueMode` (alongside `asap`, `run_at`, `run_after`). It satisfies both a time condition and a predecessor-item condition before the `QueueRunner` dispatches the item. Either sub-condition may be omitted, in which case `run_when` degenerates to `run_at` or `run_after` respectively.

**Rationale:** Before `run_when`, operators had no way to express "start this task at 9 AM, but only if the previous task finished first." The options were to manually promote the item at the right time, or to chain two scheduled items with `run_after` and tolerate early dispatch if the predecessor finished before 9 AM. `run_when` eliminates both workarounds with a single scheduling rule.

An alternative design would have added separate boolean flags to the existing `run_at` / `run_after` modes (e.g., `also_require_predecessor`). A named composite mode was preferred because:
- The UI can present it as a distinct option with an intelligible label ("Run When both…") rather than showing confusing optional sub-fields inside a `run_at` form.
- `isReady()` in `QueueRunner` remains a clean switch on `QueueMode`; there is no need to branch on multiple flags per mode.
- The domain model remains self-documenting: the four mode names cover the four meaningful scheduling intents.

**Rejected:** Boolean flag `require_time_and_predecessor` added to existing modes (unclear semantics, harder to validate in the UI); separate `run_when_time` and `run_when_predecessor` fields without a composite mode (would require the runner to infer intent from combinations of empty fields); orchestrator-side scheduled jobs that poll and re-queue items (adds state machine complexity with no benefit over in-runner readiness checks).
