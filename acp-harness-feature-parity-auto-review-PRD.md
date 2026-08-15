# Code and Design Review: ACP Harness Feature Parity

**Branch:** `feat/acp-harness-feature-parity` (base `main`, reviewed through `894e7da`)
**Spec:** `specs/260815-acp-harness-feature-parity/`
**Reviewer:** automated dev-flow Step 5 review (independent verification pass, not a re-read of the implementation notes)

## Executive Summary

This branch closes two of three confirmed wiring gaps between `boabot -acp` and native daemon mode: `models.chat_provider` selection for ACP-sourced tasks (FR-401, plus an in-scope, correctly-justified deviation to actually wire `WithChatProvider`), and board/plugin/CLI-tool infrastructure standing up inside the ACP process (FR-402–405). The third gap (mid-task clarifying questions) is documented as a Non-Goal with a well-sourced ADR (ADR-B030).

I independently re-derived every load-bearing claim rather than trusting the implementation notes or code comments. Confidence levels below are stated explicitly per claim.

- **`go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...`** — re-ran all four myself. All clean. **Confidence: high** (ran directly, not inferred from CI or the report).
- **Domain+application aggregate coverage 92.2%, no regression** — re-derived using AGENTS.md's literal gate definition (`-coverpkg` spanning `internal/domain/...`+`internal/application/...`, excluding `mocks/`), and independently re-measured the *same* command against `main` in a throwaway detached worktree. Both sides: 92.2%. **Confidence: high, independently reproduced on both branches**, not taken from implementation-notes.md.
- **The `WithChatProvider` deviation is real and correctly fixed.** Confirmed via `git show main:boabot/cmd/boabot/acp.go` — `main`'s `buildACPAgent` calls `application.NewExecuteTaskUseCase(...)` with no `WithChatProvider` call anywhere in the file. The new `buildACPWorker`'s gating condition (`chatName != "" && chatName != cfg.Models.Default`) was checked token-for-token against `team_manager.go:1047` (`providerName := botCfg.Models.Default` at line 979, used at line 1048) — it is an exact match, not an approximation. **Confidence: high.**
- **The end-to-end tests are genuine, not shallow.** Read every test body in `acp_mcp_options_test.go`. The board/plugin/CLI-tool "end-to-end" tests each build a real `localmcp.Client` via `buildACPMCPOptions`, drive it through a real `application.ExecuteTaskUseCase.Execute()` call with a scripted mock `ModelProvider` that emits a real tool call, and assert on the tool-result message content (e.g. `board item "item-1" marked as done"`, the plugin's actual markdown content, the fake CLI binary's actual stdout). None of them call an internal store method directly and skip the MCP dispatch path. **Confidence: high.**
- **The per-persona board asymmetry is disclosed adequately.** See the dedicated section below. **Confidence: high — but see the P1 finding below for a second, undisclosed asymmetry of the same shape that the docs do not cover.**
- **New finding, not called out anywhere in spec.md/architecture.md/implementation-notes.md: `board.json` now has a real cross-process concurrent-write hazard that is reachable by construction, not just by deployment coincidence.** See FR-1 below. **Confidence: high**, on both the mechanism and reachability. Mechanism: read `board.go`'s `persist()`/`loadFromDisk()` directly — full in-memory-state overwrite on every mutation, no lock, no re-read before write. Reachability: `docs/architectural-decision-record.md`'s ADR-B026 already documents, from direct inspection of the real `buzz-acp` binary, that it "runs a **persistent pool of long-lived agent processes reused across many turns**" and exposes `--agents`/`--lazy-pool`/`--idle-pool-sleep`/respawn-with-backoff flags. A pool of N processes running the *identical* harness registration (`--agent-command boabot --agent-args -acp -agent orchestrator`) means N simultaneous `boabot -acp` processes with an **identical** `cfg.Bot.Name`, hence an identical `memPath`, hence an identical `board.json` path — by construction, not by an operator coincidentally pointing two different deployments at the same memory root. This doesn't require native mode to be running at all.
- **New finding: FR-404/405's "mirrors team_manager.go's exact condition" claim is not accurate for multi-persona teams.** See FR-2 below. **Confidence: high** — read `team_manager.go`'s plugin/CLI pre-resolution loop (lines ~501–522) directly, confirmed it reads only the *team's orchestrator entry's* config and shares the result across every bot via `tm.resolvedPluginStore`/`tm.resolvedCLITools`, and confirmed empirically (via `boabot-team/bots/*/config.yaml`) that only `orchestrator/config.yaml` sets `plugins.install_dir` today.

**Overall assessment: Request changes.** FR-1 (P0) is a genuine blocker: a silent data-loss hazard on shared Kanban board state, reachable by construction (a `buzz-acp` agent pool of size >1 running the identical harness registration this PRD is meant to make production-viable), not by an unlikely operator misconfiguration. It must be resolved — technically or via an explicit, prominent operator warning — before this branch is done. Everything else (FR-2 through FR-5) is P1/P2 documentation-accuracy and design-hygiene work that does not need to block merge on its own, though given how cheap it is (documentation-only, no code risk), bundling it into the same fix pass as FR-1 is the efficient path. None of FR-2 through FR-5 contradicts a literal, stated acceptance criterion in spec.md.

---

## Findings

### FR-1 — Board store has no cross-process concurrency protection; ACP mode newly creates a second writer to a path native mode already owns exclusively

**Priority: P0**

`internal/infrastructure/local/orchestrator/board.go`'s `InMemoryBoardStore`:

- `loadFromDisk()` reads the full JSON array from `persistPath` exactly once, at `NewInMemoryBoardStore(...)` construction time.
- `persist()` marshals the *entire in-memory `s.items` map* to a temp file and renames it over `persistPath` on every mutation — a full-file overwrite, not an append or merge.
- `s.mu` is a `sync.RWMutex` — process-local only. There is no file lock, advisory lock, or any mechanism protecting `persistPath` against a second process (in-memory state is never re-read from disk after construction).

Before this branch, `board.json` at `<memRoot>/<orchestratorName>/board.json` had exactly one writer: the native `TeamManager` process's `tm.sharedBoard`. This branch adds a second writer type — `boabot -acp -agent orchestrator` — that constructs its *own* `InMemoryBoardStore` instance at `filepath.Join(memPath, "board.json")`, where `memPath = <memRoot>/<cfg.Bot.Name>`.

**Reachable by construction, via `buzz-acp`'s own process pool — the primary path, no coincidental deployment overlap required.** `docs/architectural-decision-record.md`'s ADR-B026, from direct inspection of the real `buzz-acp` binary, already documents that it "runs a **persistent pool of long-lived agent processes reused across many turns**" and exposes `--agents`/`--lazy-pool`/`--idle-pool-sleep`/respawn-with-backoff flags for exactly that pool. `boabot -acp` is registered with `buzz-acp` as one harness command (`--agent-command boabot --agent-args -acp -agent orchestrator`); a pool size greater than 1 for that registration means `buzz-acp` runs **multiple simultaneous `boabot -acp` processes executing the identical command line** — identical `cfg.Bot.Name`, hence identical `memPath`, hence identical `board.json` path, guaranteed by construction. Nothing about this requires native daemon mode to be running at all, and nothing about it requires an operator to coincidentally point two different deployments at the same memory root — it is the designed, documented steady-state operating mode of the very harness this PRD's own ADR-B026 says `boabot -acp` is built for.

**Secondary path — native mode + ACP mode co-running for the same persona.** Per `implementation-notes.md`'s own (accurate) analysis, when `cfg.Bot.Name == "orchestrator"` (the real, deployed persona name — confirmed in `boabot-team/bots/orchestrator/config.yaml`), ACP mode's board path is also byte-identical to native mode's shared board path. The PRD's own problem statement — "the live `orchestrator` persona ... has repeatedly hit `exceeded max tool iterations` ... on tasks that complete cleanly via native mode's web-UI chat" — implies both native mode (serving web-UI chat) and ACP mode (serving `buzz-acp`) are already live for the `orchestrator` persona in production today, which would make this a second, independent way to reach the same collision.

The new `user-docs/ACP-Harness-Adoption-Config.md` bullet describes the path coincidence as a *feature* ("`complete_board_item` calls there land on the same Kanban board the native web dashboard shows, when both point at the same memory root") without any warning that two processes writing to that path concurrently is unsafe — and doesn't mention the pool scenario at all, which needs no "when both point at the same memory root" qualifier since the pool's processes share that path by definition. Either path leads to the same failure mode: two independent `InMemoryBoardStore` instances, each holding a stale in-memory snapshot of the other's writes, silently clobber each other's Kanban board state on the next `persist()` call from either side (last-writer-wins, full-file overwrite — a completed item marked done by one process can be silently reverted to "in progress" by the other process's next unrelated write, or vice versa).

This is a materially different and more severe class of problem than the already-disclosed "isolated empty board per non-orchestrator persona" asymmetry — that one is a feature gap (no board), this one is a **data-loss hazard on shared state that operators/dashboards depend on**, introduced by a new writer this branch adds to a resource that previously had exactly one.

Notably, this codebase already has precedent for exactly this class of problem and solved it for a sibling resource: `docs/architectural-decision-record.md`'s ADR-B024 and `user-docs/Buzz-Adoption-Config.md`'s "Process-singleton protection" describe a real file-lock mechanism (`lock.go`, FR-031) that makes a second `boabot` process started against the same Buzz private key refuse to attach its monitor rather than risk duplicate identity/replies. No analogous protection — or even a warning — exists for `board.json`. (The singleton lock doesn't cover this case anyway: ACP mode does not require the persona to have a `buzz:` private key at all, so the existing lock never engages for the ACP-vs-native board collision.)

**Acceptance criterion for "fixed":** A technical safeguard should be the target, since the primary reachability path (a `buzz-acp` agent pool of size >1 for one harness registration) is normal, documented operation of the very deployment this PRD exists to support — not a misconfiguration an operator can simply be told to avoid. Concretely: either (a) `InMemoryBoardStore` gains real cross-process safety (re-reading current on-disk state and merging before each `persist()`, or an OS-level advisory lock around `loadFromDisk()`/`persist()`), or (b) ACP mode is documented as **requiring `--agents 1`/pool size 1** for any persona whose board path can collide with another process (own-pool siblings or a co-running native daemon), with `buzz-acp`'s pool flags called out explicitly in `ACP-Harness-Adoption-Config.md` (today the doc doesn't mention the pool at all in connection with the board). (a) is preferable and should be pursued if feasible in reasonable scope; (b) is the minimum acceptable interim mitigation and must name the pool-size constraint explicitly, not just the already-documented (but incomplete) same-memory-root caveat.

---

### FR-2 — FR-404/FR-405's "exact mirror of team_manager.go" claim is inaccurate for any team beyond the orchestrator persona; plugin/CLI-tool config is sourced from the wrong scope

**Priority: P1**

`team_manager.go`'s plugin-store/CLI-tools resolution (lines ~501–522) runs **once**, before any bot goroutine starts, and scans `teamCfg.Team` for the entry with `Orchestrator: true` — i.e. it reads `Orchestrator.Plugins.InstallDir`/`Orchestrator.CLITools` from **the team's orchestrator persona's own config.yaml only**, storing the result in `tm.resolvedPluginStore`/`tm.resolvedInstallDir`/`tm.resolvedCLITools` on the `TeamManager` receiver. Every bot's `startBot` (including `architect`, `reviewer`, `implementer`, `maintainer`, `tech-lead` — anyone) then wires `WithPluginStore`/`WithCLITools` from those **shared, orchestrator-sourced** fields; `botCfg.Orchestrator.Plugins.InstallDir` (that specific bot's own config) is **never read** for this purpose anywhere in `startBot`. Native mode's plugin/CLI-tool config is team-wide, sourced from one place, applied to everyone.

`buildACPMCPOptions` instead reads `cfg.Orchestrator.Plugins.InstallDir`/`cfg.Orchestrator.CLITools` from **the config of whichever persona `boabot -acp` is currently running as** — there is no team.yaml, no concept of "the orchestrator entry," so there is no way to replicate native mode's actual sourcing without loading team.yaml (which spec.md's Non-Goals correctly rule out). This is architecturally understandable, but it means the claim — repeated in FR-404/FR-405's text, `architecture.md`, `research.md` RQ3, ADR-B030, and the `ACP-Harness-Adoption-Config.md` bullet ("plugin tools ... when `orchestrator.plugins.install_dir` is set ... all activated under the exact same `config.yaml` settings that activate them in native mode — no separate ACP-specific configuration needed") — is **not accurate** for any persona other than the one flagged `orchestrator: true` in `team.yaml`.

Confirmed empirically against the real deployed config in this repo (`boabot-team/bots/*/config.yaml`): only `bots/orchestrator/config.yaml` sets `plugins.install_dir`; no persona config sets `cli_tools` at all. So, as configured today, `boabot -acp -agent architect` (or `reviewer`/`implementer`/`maintainer`/`tech-lead`) activates **zero** plugin tools under ACP mode, even though native mode's `architect` bot *does* get plugin tools (inherited from the orchestrator persona's shared config). This directly contradicts the "same tools available ... no separate ACP-specific configuration" framing for every persona except the one literally named `orchestrator`.

The relative-install-dir resolution has the same scope mismatch as a corollary: native mode resolves a relative `install_dir` against `<MemoryRoot>/<orchestrator-entry-name>`; ACP mode resolves it against `<MemoryRoot>/<cfg.Bot.Name>` (the running persona's own path) — consistent within each mode, but not "the same resolution" across modes for a non-orchestrator persona.

**Acceptance criterion for "fixed":** Correct the "mirrors team_manager.go's exact condition" language in FR-404/FR-405 (spec.md), `architecture.md`, `research.md`, and ADR-B030 to state the actual scope precisely (ACP mode reads the *running persona's own* `config.yaml`; native mode reads the *team's orchestrator entry's* `config.yaml` and shares it team-wide). Add a bullet to `ACP-Harness-Adoption-Config.md`, next to the existing board-scope disclosure, stating plainly: plugin/CLI-tool activation in ACP mode depends on the specific persona's *own* `config.yaml` carrying `orchestrator.plugins.install_dir`/`orchestrator.cli_tools.*` — copy those settings into a non-orchestrator persona's own config if you want ACP mode to activate them for that persona, since ACP mode (unlike native mode) will not inherit them from a team's orchestrator config. No code change is required to satisfy this AC — this is a documentation/claim-accuracy fix, not a behavior change (a behavior change to replicate native mode's team-wide sourcing would require reading `team.yaml`, which spec.md's Non-Goals correctly forbid).

---

### FR-3 — Board-store activation gate compares a different field than the one `team_manager.go` actually gates on

**Priority: P2**

`team_manager.go:1023` gates board wiring on `entry.Type != "tech-lead"`, where `entry.Type` is the `team.yaml` entry's `type:` field (also the `<bots-dir>/<type>/` directory name used to load that bot's config). `buildACPMCPOptions` gates on `cfg.Bot.BotType != "tech-lead"` — the *loaded persona's own* `bot.type:` field from its `config.yaml`. These are two different pieces of data. In every real persona config in this repo (`boabot-team/bots/*/config.yaml`), `bot.type` matches its own directory name exactly, so the two conditions are equivalent in practice today — but FR-402/architecture.md's claim of mirroring "team_manager.go's exact condition" is, strictly, describing a different field than the one actually compared.

**Acceptance criterion for "fixed":** Either soften the "exact condition" language to "equivalent by convention (every persona's own `bot.type` matches the directory name it's loaded from)" in FR-402/architecture.md, or, if precision matters more than wording, note explicitly in a code comment on `buildACPMCPOptions` that ACP mode has no `team.yaml` entry-type field to read and is relying on the `bot.type`-matches-directory-name convention `resolveACPConfigPath` itself already depends on.

---

### FR-4 — `ACP-Harness-Adoption-Config.md`'s "No filesystem/tool differences from native mode" bullet overstates parity given the two scope caveats above

**Priority: P2**

The bullet heading "**No filesystem/tool differences from native mode.**" is immediately followed (in the same bullet, and the next two) by the accurately-disclosed per-process board scope caveat and, once FR-2 above is addressed, a plugin/CLI-tool scope caveat. A heading asserting unqualified parity followed by two paragraphs of divergence is misleading to a reader skimming section headers.

**Acceptance criterion for "fixed":** Reword the bullet's lead sentence to something that doesn't overclaim, e.g. "**Same tool/provider mechanisms as native mode, with two scope differences** (see below)" — so the section doesn't assert parity and then spend the next two bullets walking it back.

---

### FR-5 — Business rule for board/plugin/CLI-tool activation gating is now duplicated across two independent implementations

**Priority: P2**

The gating rules ("this persona type doesn't get a board," "this install-dir presence activates plugin tools," "these per-tool flags gate CLI tools") now exist as two separately-maintained implementations: `team_manager.go`'s `startBot` (application layer, native mode) and `acp.go`'s `buildACPMCPOptions`/`buildACPWorker` (cmd layer, ACP mode). Nothing enforces the two stay in sync; a future change to one native-mode gating condition (e.g. excluding a new bot type from the board, or changing the plugin-store failure-handling policy) requires remembering to make the equivalent change in `acp.go` too, with no compiler or test-suite signal if someone forgets. This is consistent with existing precedent in this codebase (`cmd/boabot/main.go`'s `buildBuzzMonitor`/`newBuzzMonitorBuilder` already duplicates some native-mode wiring shape in the `cmd` layer), so it is not a new anti-pattern this branch introduces — but the duplication surface has now grown to three separate features (board gate, plugin gate, CLI-tool gate) rather than one, which raises the drift risk proportionally.

**Acceptance criterion for "fixed":** No fix required to merge (matches established precedent). If addressed, the remedy is extracting the three gating *decisions* (not the store construction itself) into small, pure, directly-testable functions shared by both `team_manager.go` and `acp.go` — e.g. `func boardActivatesFor(botType string) bool`, `func resolvePluginInstallDir(installDir, basePath string) string` — living in a package both `cmd` and `internal/application/team` can import without violating Clean Architecture's dependency direction (e.g. `internal/application` itself, since both callers already depend on `internal/application`/`internal/domain`).

---

## Per-Persona Board Asymmetry — Independent Conclusion

**Conclusion: the disclosure is adequate. No fix required on the disclosure itself.** (See FR-2 above for a *related but separate* scope-accuracy issue in the surrounding documentation, and FR-1 for a more serious, undisclosed consequence of the *coincidence* this asymmetry creates for the `orchestrator` persona specifically.)

Reasoning:

- `implementation-notes.md`'s "Deviations from Plan" section documents the asymmetry in detail: it explains the mechanism (ACP mode's board path is `<memRoot>/<cfg.Bot.Name>/board.json`; native mode's shared board is `<memRoot>/<orchestratorName>/board.json`), states precisely where they coincide (only when `cfg.Bot.Name == team.yaml`'s orchestrator entry name — true for the real `orchestrator` persona in `boabot-team/`) and where they diverge (every other persona gets an isolated, empty, persona-private board), and explicitly frames it as "accepted, not fixed here," consistent with spec.md's Non-Goals (no `TeamManager` merge) and the Risks table's own mitigation instruction ("state explicitly ... don't assume").
- Critically, this isn't just an internal dev-flow artifact — it is **also disclosed to operators** in `user-docs/ACP-Harness-Adoption-Config.md`'s dedicated bullet ("**The board is per-ACP-process, not automatically the team's shared board.**"), which names the exact path convention, names which persona coincides and why, names which personas diverge with concrete examples (`architect`, `reviewer`), and gives explicit remediation guidance ("If you need a non-orchestrator persona's ACP-mode board tool to reflect the team's real Kanban board ... run that persona under native daemon mode instead"). An operator running `boabot -acp -agent architect` and expecting board integration has a direct, specific answer to "why doesn't this work" one bullet away in the adoption doc — this is not buried in an ADR or an implementation-notes file most operators would never read.
- I verified this is not merely asserted but actually true by reading `acp.go`'s `memPath` computation (`filepath.Join(memRoot, cfg.Bot.Name)`) against `team_manager.go`'s shared-board computation (`filepath.Join(tm.cfg.MemoryRoot, orchestratorName)`, `orchestratorName` resolved from the `team.yaml` entry with `Orchestrator: true`) and confirmed both reduce to the same string only when the two names match, which they do for the real `orchestrator` persona's config (`boabot-team/bots/orchestrator/config.yaml`: `bot.name: orchestrator`) and `team.yaml`'s orchestrator entry (`name: orchestrator`).

The one place this conclusion needs qualifying: the *coincidence* this asymmetry creates for the `orchestrator` persona (byte-identical board path across two independently-writing processes) is exactly what makes FR-1's concurrent-write hazard reachable. The board-scope disclosure itself is fine; what's missing is a disclosure of the *consequence* of the one case where scopes do coincide.

---

## Regression Check

Confirmed via `git diff main...HEAD --stat`: no files under `internal/application/team/`, `internal/infrastructure/http/`, or `internal/infrastructure/acp/` (turn handling, fallback-publish, keep-alive) appear in the diff at all — native mode and existing ACP turn-execution behavior are untouched by this branch, not just "claimed unchanged." The only application-layer production-code change is `internal/application/execute_task.go`'s one-line `isConversationalSource` extension, confirmed by direct diff inspection (`internal/application/team/*.go` has zero changes).

## Test/Lint/Coverage Verification (re-run independently, not trusted from reports)

| Check | Result | Method |
|---|---|---|
| `gofmt -l .` | clean | ran directly |
| `go vet ./...` | clean | ran directly |
| `golangci-lint run` | `0 issues.` | ran directly (v2.9.0) |
| `go test -race -gcflags=all=-d=checkptr=0 ./...` | all packages `ok` | ran directly |
| Domain+application aggregate coverage | **92.2%**, matches claim, no regression | re-derived using AGENTS.md's literal gate definition (`-coverpkg` over `internal/domain/...`+`internal/application/...` minus `mocks/`); independently re-measured the identical command against a detached `main` worktree — also 92.2% |
| README's per-package `internal/application/team` figure (79.1% → 81.9%) | **accurate, not a regression-masking error** | ran README's own documented command (`go test -race -coverprofile=... ./internal/domain/... ./internal/application/...`) against both branch and `main` in isolation for that one package: both report 81.9%. This branch touches zero files in `internal/application/team/`, so this was a **pre-existing stale figure in `main`'s README** (81.9% was already the true value at the branch's merge-base) that this branch's docs-sync pass happened to correct as a side effect — not a number this branch's code changed, and not inaccurate as now written. |

---

## Implementation Guidance for Fixes

- **TDD for every fix.** Each finding above that requires a code change (FR-1, if a technical fix is chosen) needs a failing regression test first — for FR-1, a test that spins up two `InMemoryBoardStore` instances against the same `persistPath`, has each mutate independently, and asserts the second process's write doesn't silently discard the first's, before any production code changes.
- **Brief review per fix**, not one giant re-review at the end — each finding is independent enough to review in isolation.
- **Use worktrees/agent teammates for parallel independent fixes.** FR-1 (board concurrency), FR-2/FR-4 (documentation scope corrections), FR-3 (comment/wording precision), and FR-5 (optional refactor) touch non-overlapping files and can be parallelized safely: FR-1 touches `internal/infrastructure/local/orchestrator/board.go` (+ its tests) or docs-only; FR-2/FR-3/FR-4 touch only `specs/260815-acp-harness-feature-parity/*.md`, `docs/architectural-decision-record.md`, and `user-docs/ACP-Harness-Adoption-Config.md`; FR-5 (if pursued) touches `cmd/boabot/acp.go` and `internal/application/team/team_manager.go`.
- **P0 items block mergeability.** FR-1 is P0 and blocks: this branch should not merge until it is resolved (technical fix preferred; at minimum, the pool-size mitigation and explicit documentation described in FR-1's acceptance criterion). FR-2 through FR-5 (P1/P2) should be fixed but do not independently gate merge — though given how cheap FR-2/FR-3/FR-4 are (documentation-only, no code risk), there's little reason not to bundle them into the same fix pass as FR-1 rather than opening a second PR.
