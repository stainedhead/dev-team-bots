# GitHub Copilot Instructions — BaoBot Dev Team

## Language

Go 1.26. Idiomatic Go only. Use the standard library where it serves the purpose.

## Non-Negotiable Standards

- **TDD (red-green-refactor).** Always write the failing test before the implementation.
- **90%+ test coverage** across all modules.
- **Clean Architecture.** Domain layer has no infrastructure imports. Dependencies point inward only.
- **Interfaces for all external services.** AWS, Slack, Teams, databases — always behind an interface defined in the domain layer.
- **`go fmt`, `go vet`, `golangci-lint`** must all pass.

## Code Style

- No unnecessary comments. Name things clearly instead.
- No over-engineering. The simplest correct implementation wins.
- No global state. Pass dependencies explicitly.
- Errors are values. Handle them explicitly — do not suppress or ignore.
- Prefer small, focused functions with a single responsibility.
- Table-driven tests for any function with multiple input/output cases.

## Project Structure

Each module follows this layout:

```
cmd/<binary>/       # main — wiring only
internal/
  domain/           # interfaces, entities — no infra imports
  application/      # use cases
  infrastructure/   # adapters (S3, SQS, DB, HTTP, Slack, etc.)
docs/               # product-summary, product-details, technical-details, ADR
user-docs/          # end-user documentation
bin/                # build output (gitignored)
```

## Testing

- Unit tests mock at interface boundaries. Never hit real AWS, databases, or external APIs in unit tests.
- Mocks live in `mocks/` alongside the interface package.
- Integration tests are clearly separated and tagged.
- Use `testify` for assertions. Use `mockery`-style mocks or hand-written mocks for simple interfaces.

## Documentation

When suggesting code changes, also suggest the corresponding documentation update in:
- `docs/technical-details.md` for architectural or implementation changes.
- `docs/product-details.md` for behaviour changes.
- `docs/architectural-decision-record.md` for significant decisions.

## Configuration and Secrets

- Config files live next to the binary at runtime.
- Secrets come from AWS Secrets Manager — never from config files or environment variables directly.
- Never suggest hardcoded credentials, keys, or secrets.

## Builds

Binaries output to `bin/` (gitignored). Build with:
```bash
go build -o bin/<name> ./cmd/<name>
```
