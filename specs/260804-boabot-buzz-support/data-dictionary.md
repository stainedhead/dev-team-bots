# Data Dictionary: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Purpose:** Defines the domain entities, value objects, interfaces, and enumerations introduced or modified by this feature.

---

## Domain Interfaces (new)

### `RelayClient` (finalized name and shape)

```go
type RelayClient interface {
    Connect(ctx context.Context) error
    Authenticate(ctx context.Context) error
    Publish(ctx context.Context, evt Event) error
    Subscribe(ctx context.Context, f Filter) (<-chan Event, error)
    Close() error
}
```

Confines `fiatjaf.com/nostr` to `internal/infrastructure/buzz/`. Per PRD §Architecture Decision, Layering consequence.

### `SecretStore` / `SecretProvider`

```go
type SecretRef struct {
    Name string // logical name, e.g. "buzz_private_key"
    Bot  string // optional per-bot namespace; "" = global
}

type SecretProvider interface {
    Name() string
    Lookup(ctx context.Context, ref SecretRef) (string, bool, error)
}

type SecretStore interface {
    Get(ctx context.Context, ref SecretRef) (string, error)
}
```

Per PRD FR-038. `SecretStore` holds an ordered `[]SecretProvider`, first-hit-wins.

## Value Objects

**Resolved (was TBD):** the domain layer defines its own minimal `Event`/`Filter` types rather than referencing `fiatjaf.com/nostr`'s library types. Reusing the library type directly would put a `fiatjaf.com/nostr` import in `internal/domain`, which fails the PRD acceptance criterion `grep -r "fiatjaf.com/nostr" internal/domain internal/application` returns no matches, and violates FR-033/FR-038 and the Maintainability NFR. `internal/infrastructure/buzz/relay_client.go` translates between the two at the `RelayClient` implementation boundary — this translation function is itself a required unit-tested component (see `tasks.md` Phase D).

```go
// internal/domain/buzz.go
type Event struct {
    ID        string
    PubKey    string
    CreatedAt int64
    Kind      int
    Tags      [][]string
    Content   string
    Sig       string
}

type Filter struct {
    Kinds []int
    Tags  map[string][]string // e.g. {"h": [...], "p": [...], "e": [...]}
    Since *int64
    Until *int64
    Limit int
}
```

- **`Event`** — Nostr event (id, pubkey, created_at, kind, tags, content, sig), as above. Domain-owned, no `fiatjaf.com/nostr` dependency.
- **`Filter`** — Nostr subscription filter (kinds, `#h`, `#p`, `#e`, since/until, limit), as above.
- **`AuthTag`** — NIP-OA `["auth", owner-pubkey-hex, conditions, sig-hex]`, 4-element tuple. Construction/validation logic per PRD FR-006/FR-007. Represented as a typed struct (`OwnerPubkeyHex`, `Conditions`, `SigHex` string fields) in `internal/infrastructure/buzz/nipoa.go`, not as a raw `[]string`, so malformed-length tags are a construction-time error rather than a runtime index-out-of-range.

## Enumerations

- **Nostr event kinds referenced:** `0` (profile), `5` (deletion), `7` (reaction), `9` (group message), `9000`–`9008`, `9021`, `9022` (NIP-29 group management), `9030`–`9033` (relay admin), `13534` (membership roster), `20001` (presence), `20002` (typing), `22242` (NIP-42 auth), `39000`–`39002` (discovery), `40002`/`40003` (Buzz rich content, out of scope), `44100`/`44101` (member added/removed), `1059` (gift-wrapped DM, P1).
- **Auth failure classes:** `invalid` vs. `restricted` (PRD FR-009) — distinct error categories requiring different operator remedies.
- **`SecretProvider` names:** `env`, `systemd`, `keystore`, `file` — used in diagnostic output (FR-050) and logs (FR-014 of the secret-storage NFRs).

## Configuration Types (new/modified)

- **`BuzzConfig`** (new, mirrors `SlackConfig`): `Enabled bool`, `RelayURL string`, `BotName string`, `OwnerPubkey string`, `RespondTo string`, `RespondToAllowlist []string`, `Channels []string`, `PresenceInterval time.Duration`. Per PRD FR-035. No secret fields.
- **`SlackConfig`** (modified): gains a secret-resolution path for `bot_token`/`app_token` per FR-047, existing inline fields retained through a deprecation window.

## API Request/Response Types

Not applicable — this feature has no new HTTP API surface. `boabotctl` gains new CLI subcommands (FR-049) with their own flag/argument shapes, to be defined during Architecture phase.
