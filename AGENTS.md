# AGENTS.md — BaoBot Dev Team Monorepo

This file provides guidance for AI coding agents working on this repository.

## Repository Overview

This is a Go monorepo containing three modules that together form the BaoBot cooperative AI development team system:

| Directory | Purpose |
|---|---|
| `boabot/` | Agent runtime binary — shared container image deployed to ECS |
| `boabotctl/` | Operator CLI — distributed to users via GitHub Releases |
| `boabot-team/` | Bot personalities, configurations, and per-bot CDK infrastructure |

Refer to `PRODUCT.md` at the root for the full product specification.

## Language and Toolchain

- **Go 1.26** — use the most recent language features where they improve clarity.
- All code must be idiomatic Go: follow effective Go conventions, use the standard library where it serves the purpose, prefer simplicity over cleverness.
- Toolchain requirements apply to all modules:
  - `go fmt` — all code must be formatted before commit.
  - `go vet` — must pass with zero warnings.
  - `golangci-lint` — must pass with the project lint configuration. Every Go module must contain a `.golangci.yml` using `version: "2"` syntax. Modules without a config file inherit CI's strict defaults, which differ from older local versions and will cause unexpected failures.
  - `go test ./...` — all tests must pass.

## Test-Driven Development

**TDD is non-negotiable.** The red-green-refactor cycle is the only accepted workflow:

1. **Red** — write a failing test that describes the desired behaviour.
2. **Green** — write the minimum production code to make it pass.
3. **Refactor** — clean up without breaking the test.

No production code is written without a failing test first. This applies to bug fixes as well as new features — the first step for any bug is a failing regression test.

When implementing review fixes (dev-flow Step 9), every finding in the review PRD must have a corresponding commit before the step is marked complete. P0 findings that remain open block the PR. Before closing Step 9, check each finding off explicitly against the commit log — do not rely on memory.

**Coverage target: 90% or above on Domain and Application layers.** Coverage is measured on packages matching `internal/domain/...` and `internal/application/...`, excluding `mocks/` subdirectories. `cmd/`, `mocks/`, and `config/` packages are excluded from the threshold — they contain wiring or generated code with no meaningful unit-test surface. Do not reduce coverage when adding code.

## Clean Architecture

All modules enforce Clean Architecture boundaries:

- **Domain layer** — core business logic and interfaces. No infrastructure imports.
- **Application layer** — use cases that orchestrate domain logic.
- **Infrastructure layer** — implementations of domain interfaces (S3, SQS, Bedrock, Slack, etc.).

Dependencies point inward only. Infrastructure depends on domain, never the reverse. If you find yourself importing an AWS SDK package from a domain or application file, stop — you are in the wrong layer.

All key services are defined as interfaces in the domain layer. Infrastructure implementations satisfy those interfaces. This enables mocking at adapter seams for unit testing without hitting real external services.

## Mocking

Use interface-based mocking. Generate mocks with `mockery` or write them by hand for simple interfaces. Mocks live alongside the interface they mock in a `mocks/` subdirectory. Integration tests hit real infrastructure (localstack, test databases, etc.) — unit tests never do.

## Project Structure (per module)

```
<module>/
├── cmd/<binary>/       # main package — wiring only, no logic
├── internal/
│   ├── domain/         # interfaces, entities, value objects
│   ├── application/    # use cases
│   └── infrastructure/ # adapters: S3, SQS, DB, HTTP, etc.
├── docs/               # product-summary.md, product-details.md,
│                       # technical-details.md, architectural-decision-record.md
├── user-docs/          # adoption and usage documentation
├── bin/                # build output (gitignored)
├── AGENTS.md
├── CLAUDE.md
└── README.md
```

## CI/CD

GitHub Actions workflows live exclusively at `.github/workflows/` in the repository root — GitHub does not process workflow files in subdirectories. Each workflow uses `paths:` filters to trigger only on changes to its module:

| Workflow | Triggers on | Pipeline |
|---|---|---|
| `boabot.yml` | `boabot/**` | test → lint → build → containerise → CDK deploy (shared stack) |
| `boabotctl.yml` | `boabotctl/**` | test → lint → build; release on tag `boabotctl/v*` |
| `boabot-team.yml` | `boabot-team/**` | CDK test → CDK diff (PR) → CDK deploy (per-bot stack) |

Containerise and deploy steps run on merge to `main` only. PRs run test, lint, build, and CDK diff.

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

This applies to every PR opened in this repo, with no exceptions.

## Build and Configuration

- Builds output to `bin/` in each module directory. This directory is gitignored.
- The default config file location is **next to the binary** at runtime. Do not hardcode paths.
- Secrets are never stored in config files or committed to git. They are read from AWS Secrets Manager at runtime.

## Documentation Requirements

When making changes, the following files must be kept in sync with the code:

- `README.md` — always reflects the current state of the module.
- `docs/product-summary.md` — updated if the feature set changes.
- `docs/product-details.md` — updated if behaviour changes.
- `docs/technical-details.md` — updated if architecture or implementation changes.
- `docs/architectural-decision-record.md` — a new entry added for any significant decision.

Do not leave documentation stale. A PR that changes behaviour without updating docs is incomplete.

## Module-Specific Guidance

Each module has its own `AGENTS.md` and `CLAUDE.md` with module-specific instructions. Read them before working on that module.

## Dev-Flow Skills

The following skills are available for the developer workflow. Invoke them with the listed slash command.

| Skill | Invocation | Purpose |
|---|---|---|
| `init-dev-flow` | `/init-dev-flow` | One-time repo setup; updates AGENTS.md |
| `create-prd` | `/create-prd [title]` | Interactive PRD authoring; writes to repo root |
| `review-prd` | `/review-prd [file]` | PRD quality review |
| `create-spec` | `/create-spec [prd-file]` | Spec creation from PRD; YYMMDD-prefixed directory |
| `review-spec` | `/review-spec [spec-dir]` | Spec quality review |
| `archive-spec` | `/archive-spec [spec-dir]` | Move completed spec to specs/archive/ |
| `implm-frm-prd` | `/implm-frm-prd [prd-file]` | Implement from PRD (11 steps) |
| `implm-frm-change-dtls` | `/implm-frm-change-dtls [ticket-or-desc]` | Implement from ticket or description (12 steps) |
| `implm-from-spec` | `/implm-from-spec [spec-dir]` | Full 11-step orchestrated implementation |
| `create-review` | `/create-review` | Code and design review artifact |
| `review-code` | `/review-code` | Code review of current branch |
| `write-flow-analys` | `/write-flow-analys [spec-dir]` | Process analysis report (final step of implm-from-spec) |

## Feature Specification Workflow

### Specs Directory Structure

All feature development uses the `specs/` directory for planning and tracking. Each feature gets its own subdirectory named `YYMMDD-<feature-name>/`.

**Directory Structure:**
```
specs/
└── YYMMDD-<feature-name>/
    ├── spec.md                  # Feature specification and requirements
    ├── status.md                # **CRITICAL**: Phase progress tracking (update after each task)
    ├── plan.md                  # Implementation plan and architecture decisions
    ├── tasks.md                 # Task breakdown and progress tracking
    ├── research.md              # Research findings, API docs, examples
    ├── data-dictionary.md       # Data structures, types, schemas
    ├── architecture.md          # System architecture and component design
    └── implementation-notes.md  # Implementation details, gotchas, decisions
```

### Progressive Documentation Build

Documents are created progressively as the feature develops:

**Phase 0: Initial Research (PRD/Feature Research)**
- Input: Product Requirement Document, RFC, or feature research
- Purpose: Understand the problem, gather requirements, identify constraints
- **Update status.md**: Mark Phase 0 as "In Progress"

**Phase 1: Specification (spec.md)**
- Define what the feature does, user requirements, acceptance criteria, goals and non-goals
- **Update status.md**: Mark Phase 0 complete, Phase 1 in progress

**Phase 2: Research & Data Modeling (research.md, data-dictionary.md)**
- Gather documentation, explore existing code, define domain entities and data structures
- **Update status.md**: Mark Phase 1 complete, Phase 2 in progress

**Phase 3: Architecture & Planning (architecture.md, plan.md)**
- Design implementation approach, identify affected layers, document component architecture
- **Update status.md**: Mark Phase 2 complete, Phase 3 in progress

**Phase 4: Task Breakdown (tasks.md)**
- Break down work into concrete, testable tasks with dependencies and estimates
- **Update status.md**: Mark Phase 3 complete, Phase 4 in progress

**Phase 5: Implementation (code + implementation-notes.md)**
- Follow TDD (Red-Green-Refactor), record decisions, document edge cases
- **Update status.md**: After EACH task completion — MANDATORY

**Phase 6: Completion & Archival**
- Update product documentation, move spec to `specs/archive/`, capture lessons learned
- **Verify status.md**: Must show 100% completion before archiving

**MANDATORY**: Update `status.md` after completing each task or phase. This file is the single source of truth for progress tracking.

### Specs Workflow Rules

- **Create the spec directory** before starting any new feature work
- **Update progressively** — specs are living documents, not written once
- **Update status.md ALWAYS** after completing each task, phase, or milestone — this is MANDATORY
- **Reference from commits** — link to the spec directory in commit messages
- **Archive completed specs** — move to `specs/archive/` when 100% complete in status.md
- **Version control** — commit specs alongside code for team visibility
