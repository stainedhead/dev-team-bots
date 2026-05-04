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
  - `golangci-lint` — must pass with the project lint configuration.
  - `go test ./...` — all tests must pass.

## Test-Driven Development

**TDD is non-negotiable.** The red-green-refactor cycle is the only accepted workflow:

1. **Red** — write a failing test that describes the desired behaviour.
2. **Green** — write the minimum production code to make it pass.
3. **Refactor** — clean up without breaking the test.

No production code is written without a failing test first. This applies to bug fixes as well as new features — the first step for any bug is a failing regression test.

**Coverage target: 90% or above for all modules.** Coverage is measured and enforced in CI. Do not reduce coverage when adding code.

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
