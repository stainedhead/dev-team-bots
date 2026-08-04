package domain

import "context"

// Event is BaoBot's domain-owned representation of a Nostr event.
//
// It intentionally mirrors, field for field, the shape of a Nostr event
// (NIP-01) but is not any particular Nostr client library's Event type:
// reusing a third-party library type directly here would put that
// library's import in internal/domain, which the PRD's acceptance
// criteria and FR-033/FR-038 forbid. All fields are plain, JSON/log-
// friendly Go types (hex-encoded strings rather than fixed-size byte
// arrays) so the domain layer has no dependency on any particular Nostr
// library's representation.
//
// internal/infrastructure/buzz translates between this type and the
// underlying Nostr client library's own event type at the RelayClient
// implementation boundary.
type Event struct {
	// ID is the 32-byte event id, hex-encoded (64 hex chars). Empty for an
	// event that has not yet been assigned an id (e.g. before signing).
	ID string

	// PubKey is the 32-byte author public key, hex-encoded (64 hex chars).
	PubKey string

	// CreatedAt is the Unix timestamp (seconds) the event was created.
	CreatedAt int64

	// Kind is the Nostr event kind (e.g. 0 profile, 9 group message, 22242
	// NIP-42 auth). Nostr kinds are defined as an unsigned 16-bit integer
	// (0-65535); translation to the library's Kind type validates this
	// range.
	Kind int

	// Tags is the event's tag list, e.g. [["h", "<channel-uuid>"]]. Each
	// inner slice is a single tag: its first element is the tag name, the
	// rest are its values.
	Tags [][]string

	// Content is the event's freeform content string.
	Content string

	// Sig is the 64-byte Schnorr signature, hex-encoded (128 hex chars).
	// Empty for an event that has not yet been signed.
	Sig string
}

// Filter is BaoBot's domain-owned representation of a Nostr subscription
// filter (NIP-01 REQ).
//
// Tags carries tag filters keyed by the bare tag letter -- e.g.
// {"h": ["<channel-uuid>"]} for a channel-scoped subscription, {"p":
// ["<pubkey>"]} for a p-gated one -- without the "#" prefix used in the
// wire-level filter JSON; internal/infrastructure/buzz's translation layer
// adds/strips that prefix as needed.
//
// Since and Until are pointers so an absent bound (no timestamp
// constraint) is distinguishable from an explicit bound of zero.
type Filter struct {
	Kinds []int
	Tags  map[string][]string
	Since *int64
	Until *int64
	Limit int
}

// RelayClient is the domain port over a single Nostr relay connection. It
// confines the underlying Nostr client library to internal/infrastructure/
// buzz; nothing in internal/domain or internal/application ever imports it
// directly.
//
// The expected call sequence is Connect, then Authenticate (NIP-42), after
// which Publish and Subscribe may be used; Close tears the connection down.
// An implementation MUST transparently reconnect, re-authenticate, and
// re-establish all outstanding subscriptions on disconnect (FR-012) --
// callers do not need to detect disconnects and re-drive this sequence
// themselves.
type RelayClient interface {
	// Connect establishes the WebSocket connection to the configured
	// relay.
	Connect(ctx context.Context) error

	// Authenticate performs the NIP-42 AUTH handshake against the relay
	// Connect established. It blocks until the relay's challenge has been
	// answered and the relay's AUTH response (OK/CLOSED) is known.
	Authenticate(ctx context.Context) error

	// Publish signs evt with the client's own key and sends it to the
	// relay, returning once the relay's OK response is known (or ctx is
	// done).
	Publish(ctx context.Context, evt Event) error

	// Subscribe opens a subscription for f and returns a channel of
	// matching events. The channel is closed when ctx is canceled or the
	// RelayClient itself is closed; a disconnect/reconnect does not close
	// it -- the subscription is transparently re-established on the new
	// connection and delivery resumes on the same channel.
	Subscribe(ctx context.Context, f Filter) (<-chan Event, error)

	// Close tears down the relay connection and releases all resources,
	// including canceling every outstanding subscription's channel.
	Close() error
}
