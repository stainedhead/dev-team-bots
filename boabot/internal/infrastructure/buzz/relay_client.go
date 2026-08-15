package buzz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// AuthTagFunc is D5's extension point for NIP-OA (Phase E): when set, it is
// called while building the NIP-42 AUTH event, and its returned tag (e.g.
// ["auth", owner-pubkey-hex, conditions, sig-hex]) is appended to the event
// before it is signed. Returning a nil/empty slice omits the tag for that
// AUTH attempt. A non-nil error aborts authentication.
type AuthTagFunc func(ctx context.Context) ([]string, error)

const (
	defaultAuthRetryInterval = 200 * time.Millisecond
	defaultAuthTimeout       = 10 * time.Second
	subscribeChannelBuffer   = 64

	// authFreshnessWindow is the relay's NIP-42/NIP-AA freshness tolerance
	// for an AUTH event's created_at (FR-008: "±120s RECOMMENDED").
	authFreshnessWindow = 120 * time.Second
)

// errAuthTokenRequired is returned by Connect when the relay is configured
// (via WithAPIToken(..., required=true)) to require BUZZ_API_TOKEN
// authentication and no token was resolved -- fail-closed per FR-010,
// rather than attempting a connection we already know will be rejected.
var errAuthTokenRequired = errors.New("buzz: relay requires an API token (BUZZ_REQUIRE_AUTH_TOKEN=true) but none was resolved")

// errNotConnected is returned by Authenticate/Publish when called before a
// successful Connect.
var errNotConnected = errors.New("buzz: not connected")

// errClosed is returned by Subscribe once the RelayClient has been closed.
var errClosed = errors.New("buzz: relay client is closed")

// errNoChallenge mirrors fiatjaf.com/nostr's *Relay.Auth error text
// ("no challenge, can't AUTH") returned when Auth is called before the
// relay's AUTH challenge frame has been processed. It is not a sentinel
// the library itself exposes (that error is constructed with fmt.Errorf,
// not wrapped from a package-level var), so isNoChallengeErr matches on
// substring; this var exists so tests (fakeConn) can return exactly the
// condition authenticateOn is built to retry on.
var errNoChallenge = errors.New("no challenge, can't AUTH")

// ErrAuthClockSkew is returned (wrapped) when the AUTH event's created_at,
// stamped from RelayClient's own clock (FR-008: "current wall-clock UTC"),
// would fall outside the relay's ±120s freshness window relative to the
// library's own independently-observed current time. This is a
// distinguishable local failure caught before the event is even signed or
// sent, rather than surfacing as a generic auth failure once the relay
// rejects it. See WithClock for how tests inject a skewed clock.
var ErrAuthClockSkew = errors.New("buzz: authenticate: local clock is skewed beyond the relay's ±120s freshness window")

// AuthFailureClass distinguishes NIP-AA's two relay AUTH-rejection
// buckets (FR-009): step-1 failures ("invalid: ...": malformed AUTH event,
// bad signature, wrong relay, stale timestamp) versus credential/membership
// failures ("restricted: ...": missing/invalid credential, non-member
// owner). These have different operator remedies and must never be
// collapsed into one generic class.
type AuthFailureClass int

const (
	// AuthFailureUnclassified is used when the relay's rejection reason
	// does not match either recognized NIP-AA prefix (e.g. a transport
	// error, or a local failure such as ErrAuthClockSkew that never
	// reached the relay at all).
	AuthFailureUnclassified AuthFailureClass = iota
	// AuthFailureInvalid corresponds to the relay's "invalid: ..." class.
	AuthFailureInvalid
	// AuthFailureRestricted corresponds to the relay's "restricted: ..."
	// class.
	AuthFailureRestricted
)

// String returns the class name used in log/metric attributes.
func (c AuthFailureClass) String() string {
	switch c {
	case AuthFailureInvalid:
		return "invalid"
	case AuthFailureRestricted:
		return "restricted"
	default:
		return "unclassified"
	}
}

// ErrAuthInvalid and ErrAuthRestricted are the FR-009 sentinels a caller
// can test for with errors.Is against the error Authenticate returns, so
// "invalid:" and "restricted:" relay rejections are never collapsed into
// one generic auth-failure check.
var (
	ErrAuthInvalid    = errors.New(`buzz: authenticate: relay rejected AUTH as "invalid:" (malformed AUTH event, bad signature, wrong relay, or stale timestamp)`)
	ErrAuthRestricted = errors.New(`buzz: authenticate: relay rejected AUTH as "restricted:" (missing/invalid credential, or non-member owner)`)
)

// classifyAuthFailure inspects a relay AUTH-rejection error's text for the
// NIP-01/NIP-AA "invalid:"/"restricted:" reason prefixes. It never
// reorders or prioritizes beyond checking both substrings -- a real relay
// sends exactly one of the two prefixes per rejection.
func classifyAuthFailure(err error) AuthFailureClass {
	if err == nil {
		return AuthFailureUnclassified
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "restricted:"):
		return AuthFailureRestricted
	case strings.Contains(msg, "invalid:"):
		return AuthFailureInvalid
	default:
		return AuthFailureUnclassified
	}
}

// RelayClient implements domain.RelayClient over fiatjaf.com/nostr. See
// package doc (translate.go) for the layering rationale.
type RelayClient struct {
	url string
	sk  nostr.SecretKey
	pk  nostr.PubKey

	dial              dialFunc
	authTagFn         AuthTagFunc
	apiToken          string
	requireAuthToken  bool
	authRetryInterval time.Duration
	authTimeout       time.Duration
	backoff           BackoffConfig
	sleep             func(time.Duration)
	jitter            func() float64
	clock             func() time.Time
	logger            *slog.Logger
	profile           *Profile

	mu               sync.Mutex
	conn             relayConn
	closed           bool
	watchStarted     bool
	profilePublished bool
	connStateFn      func(connected bool)

	closedCh chan struct{}
	pumpWG   sync.WaitGroup

	subMu     sync.Mutex
	subs      map[int]*subEntry
	nextSubID int
}

// subEntry is a registered subscription. id, ctx, filter, and out are set
// once at creation and never mutated. generation is only ever mutated
// under RelayClient.subMu. wg tracks every pump ever started for this
// entry (FR-002/FR-003 fix) -- see attachSub, pumpSub, and removeAndClose.
type subEntry struct {
	id     int
	ctx    context.Context
	filter nostr.Filter
	out    chan domain.Event

	// generation is bumped by attachSub each time it successfully attaches
	// a pump for this entry. A pump compares its own generation against
	// the current value before forwarding each event; a pump running
	// under a superseded generation (see attachSub's doc comment for how
	// that can happen) detects this and exits without sending, so at most
	// one generation's pump is ever actively forwarding onto out.
	generation int

	// wg has Add(1) called by attachSub for every pump it starts (every
	// generation, not just the latest) and Done() called by that pump on
	// exit. removeAndClose waits on wg -- not a single "most recent"
	// completion signal -- before closing out, so an orphaned earlier
	// generation's pump can never still be sending when out is closed.
	wg sync.WaitGroup
}

// subscribeAfterRegisterHook, when non-nil, is invoked by Subscribe
// immediately after registering a new entry into rc.subs and releasing
// subMu, but before Subscribe's own attachSub call. Test-only: it exists
// to deterministically open the exact race window between Subscribe's own
// initial attach and a concurrent reconnect's resubscribeAll attaching
// the same not-yet-attached entry (FR-002). Production code leaves this
// nil, in which case Subscribe proceeds immediately as before.
var subscribeAfterRegisterHook func()

// Option configures a RelayClient.
type Option func(*RelayClient)

// WithDial overrides the dial function used to open the connection. Tests
// use this to inject a fake relayConn instead of dialing a real WebSocket.
func WithDial(d dialFunc) Option { return func(rc *RelayClient) { rc.dial = d } }

// WithLogger sets the logger used for connection/auth/reconnect
// diagnostics. Never given the private key (FR-002/FR-051) -- only the
// client's own public key, the relay URL, and error text.
func WithLogger(l *slog.Logger) Option { return func(rc *RelayClient) { rc.logger = l } }

// WithAuthTagFunc sets D5's NIP-OA extension point. See AuthTagFunc.
func WithAuthTagFunc(fn AuthTagFunc) Option { return func(rc *RelayClient) { rc.authTagFn = fn } }

// WithAPIToken configures D6's BUZZ_API_TOKEN support. When required is
// true and token is empty, Connect fails closed without attempting a
// connection (FR-010). When token is non-empty, it is sent as an
// "Authorization: Bearer <token>" header on every dial (initial connect
// and every reconnect), whether or not required is set.
func WithAPIToken(token string, required bool) Option {
	return func(rc *RelayClient) {
		rc.apiToken = token
		rc.requireAuthToken = required
	}
}

// WithProfile configures D7's kind:0 profile publish: once Connect first
// succeeds, the given Profile is published exactly once for the lifetime
// of this RelayClient (not repeated on every reconnect).
func WithProfile(p Profile) Option { return func(rc *RelayClient) { rc.profile = &p } }

// WithBackoff overrides D8's reconnect backoff parameters.
func WithBackoff(b BackoffConfig) Option { return func(rc *RelayClient) { rc.backoff = b } }

// WithAuthRetryInterval overrides how long Authenticate waits between
// retries while the relay's AUTH challenge has not yet arrived. Tests use
// a small value to stay fast.
func WithAuthRetryInterval(d time.Duration) Option {
	return func(rc *RelayClient) { rc.authRetryInterval = d }
}

// WithSleep overrides the function used to wait out reconnect backoff
// delays. Tests inject a no-op to avoid real sleeping.
func WithSleep(fn func(time.Duration)) Option { return func(rc *RelayClient) { rc.sleep = fn } }

// WithJitter overrides the jitter source ([0,1)) used to randomize
// reconnect backoff delays. Tests inject a deterministic source.
func WithJitter(fn func() float64) Option { return func(rc *RelayClient) { rc.jitter = fn } }

// WithClock overrides the clock used to stamp AUTH events' created_at
// (FR-008) and to detect clock skew beyond the relay's ±120s freshness
// window (ErrAuthClockSkew). Tests inject a skewed clock to prove the
// skew case is caught locally rather than surfacing as a generic auth
// failure once the relay rejects it.
func WithClock(fn func() time.Time) Option { return func(rc *RelayClient) { rc.clock = fn } }

// NewRelayClient returns a RelayClient for url, authenticating as sk.
func NewRelayClient(url string, sk nostr.SecretKey, opts ...Option) *RelayClient {
	rc := &RelayClient{
		url:               url,
		sk:                sk,
		pk:                sk.Public(),
		dial:              dialLibRelay,
		authRetryInterval: defaultAuthRetryInterval,
		authTimeout:       defaultAuthTimeout,
		backoff:           DefaultBackoffConfig,
		sleep:             time.Sleep,
		jitter:            defaultJitter,
		clock:             time.Now,
		logger:            slog.Default(),
		closedCh:          make(chan struct{}),
		subs:              make(map[int]*subEntry),
	}
	for _, opt := range opts {
		opt(rc)
	}
	return rc
}

// PubKey returns the client's own public key, hex-encoded.
func (rc *RelayClient) PubKey() nostr.PubKey { return rc.pk }

// SetConnStateFunc registers fn to be called with true whenever the
// connection transitions to established (initial Connect, and every
// successful reconnect) and false whenever it is lost (watchLoop detecting
// a disconnect, before the reconnect loop starts retrying). It exists so a
// later phase's presence loop (F14) can suspend while disconnected and
// resume on reconnect without polling RelayClient's internal state or
// duplicating reconnect.go's logic. Safe to call before or after Connect;
// safe to leave unset (nil is a no-op).
func (rc *RelayClient) SetConnStateFunc(fn func(connected bool)) {
	rc.mu.Lock()
	rc.connStateFn = fn
	rc.mu.Unlock()
}

// notifyConnState invokes the registered ConnStateFunc, if any, with the
// lock released -- so a caller's hook (e.g. Monitor's presence loop, which
// itself spawns goroutines and never publishes synchronously from inside
// this callback) can never deadlock against RelayClient's own mu.
func (rc *RelayClient) notifyConnState(connected bool) {
	rc.mu.Lock()
	fn := rc.connStateFn
	rc.mu.Unlock()
	if fn != nil {
		fn(connected)
	}
}

var _ domain.RelayClient = (*RelayClient)(nil)

// Connect establishes the WebSocket connection and starts the background
// watch-and-reconnect loop (FR-012), which runs for the lifetime of this
// RelayClient once started.
func (rc *RelayClient) Connect(ctx context.Context) error {
	if rc.requireAuthToken && rc.apiToken == "" {
		return errAuthTokenRequired
	}

	conn, err := rc.dial(ctx, rc.url, rc.dialOpts())
	if err != nil {
		return fmt.Errorf("buzz: connect: %w", err)
	}

	rc.mu.Lock()
	rc.conn = conn
	startWatch := !rc.watchStarted
	rc.watchStarted = true
	rc.mu.Unlock()

	if startWatch {
		go rc.watchLoop()
	}

	// NOTE: the F14 connection-state signal is NOT fired here. A dialed-but-
	// not-yet-authenticated connection cannot legally publish anything on a
	// real Buzz relay (an unauthenticated kind:20001 presence publish would
	// be rejected "restricted:") -- see authenticateOn, where notifyConnState
	// actually fires, exactly once auth succeeds, covering both this initial
	// Connect->Authenticate sequence and every reconnect uniformly.

	if rc.profile != nil {
		rc.mu.Lock()
		alreadyPublished := rc.profilePublished
		rc.mu.Unlock()
		if !alreadyPublished {
			if perr := rc.publishProfile(ctx, conn, *rc.profile); perr != nil {
				rc.logger.Warn("buzz: profile publish failed", "err", perr)
			} else {
				rc.mu.Lock()
				rc.profilePublished = true
				rc.mu.Unlock()
			}
		}
	}

	return nil
}

func (rc *RelayClient) dialOpts() nostr.RelayOptions {
	var opts nostr.RelayOptions
	if rc.apiToken != "" {
		opts.RequestHeader = http.Header{"Authorization": []string{"Bearer " + rc.apiToken}}
	}
	return opts
}

// Authenticate performs the NIP-42 handshake against the current
// connection. See buildSignFn for how AuthTagFunc (D5) plugs in.
func (rc *RelayClient) Authenticate(ctx context.Context) error {
	rc.mu.Lock()
	conn := rc.conn
	rc.mu.Unlock()
	if conn == nil {
		return errNotConnected
	}
	return rc.authenticateOn(ctx, conn)
}

// authenticateOn drives the NIP-42 handshake on a specific connection. It
// is used both by the public Authenticate (explicit, caller-driven) and by
// the reconnect loop (automatic, per FR-012's "re-authenticate ... after
// every reconnect"). It is also, deliberately, the ONE place the F14
// connection-state signal fires "connected" (see notifyConnState call
// below): a dialed-but-unauthenticated connection cannot legally publish
// anything on a real Buzz relay, so firing the signal any earlier (e.g.
// from Connect, right after a bare dial) would let a presence loop attempt
// a publish the relay is guaranteed to reject "restricted:". Routing both
// the initial Connect->Authenticate sequence and every reconnect through
// this single success path keeps that guarantee uniform without either
// caller needing to remember it.
//
// It retries while the relay's AUTH challenge has not yet arrived
// (errNoChallenge) rather than failing immediately: unlike the library's
// own RelayOptions.AuthHandler auto-fire path (deliberately not used
// here -- see research.md for why), an explicit caller can race the read
// loop that processes the relay's incoming AUTH frame. Any other error,
// including the relay's own "invalid:"/"restricted:" AUTH rejection, is
// returned immediately.
func (rc *RelayClient) authenticateOn(ctx context.Context, conn relayConn) error {
	sign := rc.buildSignFn()
	for {
		err := conn.Auth(ctx, sign)
		if err == nil {
			rc.notifyConnState(true)
			return nil
		}
		if !isNoChallengeErr(err) {
			return rc.wrapAuthFailure(err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("buzz: authenticate: %w", ctx.Err())
		case <-time.After(rc.authRetryInterval):
		}
	}
}

func isNoChallengeErr(err error) bool {
	return errors.Is(err, errNoChallenge) || strings.Contains(err.Error(), "no challenge")
}

// wrapAuthFailure classifies a terminal (non-retriable) relay AUTH
// rejection per FR-009, logs the classification (never the private key --
// FR-002/FR-051, only the class name and the relay's own reason text), and
// wraps err with the matching ErrAuthInvalid/ErrAuthRestricted sentinel so
// callers can distinguish the two classes with errors.Is rather than
// string-matching.
func (rc *RelayClient) wrapAuthFailure(err error) error {
	class := classifyAuthFailure(err)
	rc.logger.Warn("buzz: authenticate: relay rejected AUTH", "class", class.String(), "err", err)
	switch class {
	case AuthFailureInvalid:
		return fmt.Errorf("%w: %w", ErrAuthInvalid, err)
	case AuthFailureRestricted:
		return fmt.Errorf("%w: %w", ErrAuthRestricted, err)
	default:
		return fmt.Errorf("buzz: authenticate: %w", err)
	}
}

// buildSignFn returns the callback passed to the library's (*Relay).Auth
// (see authenticateOn). Before signing, it (E4) stamps evt.CreatedAt from
// RelayClient's own clock -- current wall-clock UTC per FR-008 -- after
// confirming that clock agrees with the library's own independently
// observed current time (already on evt.CreatedAt when this callback runs)
// to within the relay's ±120s freshness window; a wider disagreement is
// caught here as ErrAuthClockSkew rather than being silently sent and
// rejected by the relay as a generic auth failure. It then (E3) applies
// D5's NIP-OA extension point, AuthTagFunc, before signing with the agent
// key.
func (rc *RelayClient) buildSignFn() func(context.Context, *nostr.Event) error {
	return func(ctx context.Context, evt *nostr.Event) error {
		reference := time.Unix(int64(evt.CreatedAt), 0).UTC()
		stamped := rc.clock().UTC()
		if skew := stamped.Sub(reference); skew > authFreshnessWindow || skew < -authFreshnessWindow {
			return fmt.Errorf("%w: local clock reads %s, reference %s (skew %s)", ErrAuthClockSkew, stamped, reference, skew)
		}
		evt.CreatedAt = nostr.Timestamp(stamped.Unix())

		if rc.authTagFn != nil {
			tag, err := rc.authTagFn(ctx)
			if err != nil {
				return fmt.Errorf("buzz: auth tag hook: %w", err)
			}
			if len(tag) > 0 {
				evt.Tags = append(evt.Tags, nostr.Tag(tag))
			}
		}
		return evt.Sign(rc.sk)
	}
}

// Publish signs evt with the client's own key and sends it, waiting for
// the relay's OK response.
func (rc *RelayClient) Publish(ctx context.Context, evt domain.Event) error {
	rc.mu.Lock()
	conn := rc.conn
	rc.mu.Unlock()
	if conn == nil {
		return errNotConnected
	}

	nevt, err := ToLibraryEvent(evt)
	if err != nil {
		return fmt.Errorf("buzz: publish: %w", err)
	}
	nevt.PubKey = rc.pk
	if nevt.CreatedAt == 0 {
		nevt.CreatedAt = nostr.Now()
	}
	if err := nevt.Sign(rc.sk); err != nil {
		return fmt.Errorf("buzz: publish: sign: %w", err)
	}

	if err := conn.Publish(ctx, nevt); err != nil {
		return fmt.Errorf("buzz: publish: %w", err)
	}
	return nil
}

// PublishRaw sends evt to the relay exactly as given -- unlike Publish, it
// does NOT sign evt or overwrite its PubKey. See domain.RelayClient's doc
// comment: this exists for NIP-17 gift-wrapped DM events, whose outer
// envelope is already signed with a throwaway ephemeral key by
// nip59.GiftWrap, and re-signing here (as Publish does) would silently
// defeat that privacy property and break the recipient's ability to
// decrypt (GiftUnwrap derives the seal's conversation key from the
// envelope's PubKey).
func (rc *RelayClient) PublishRaw(ctx context.Context, evt domain.Event) error {
	rc.mu.Lock()
	conn := rc.conn
	rc.mu.Unlock()
	if conn == nil {
		return errNotConnected
	}

	nevt, err := ToLibraryEvent(evt)
	if err != nil {
		return fmt.Errorf("buzz: publish raw: %w", err)
	}

	if err := conn.Publish(ctx, nevt); err != nil {
		return fmt.Errorf("buzz: publish raw: %w", err)
	}
	return nil
}

// Subscribe registers f and, if currently connected, attaches it
// immediately. The returned channel survives reconnects: it is
// re-attached to a fresh underlying subscription after every reconnect
// (see reconnect.go) and is only closed when ctx is canceled or the
// RelayClient itself is closed.
func (rc *RelayClient) Subscribe(ctx context.Context, f domain.Filter) (<-chan domain.Event, error) {
	// F4/F18: reject a filter that would trip either of the PRD's "Two
	// protocol traps" before it is ever translated and sent to the relay.
	// This lives here (the last hop before conn.Subscribe), not only in an
	// upstream caller, so it cannot be bypassed by a future call site.
	if err := validateSubscriptionFilter(f, rc.pk.Hex()); err != nil {
		return nil, err
	}

	nf, err := ToLibraryFilter(f)
	if err != nil {
		return nil, fmt.Errorf("buzz: subscribe: %w", err)
	}

	entry := &subEntry{
		ctx:    ctx,
		filter: nf,
		out:    make(chan domain.Event, subscribeChannelBuffer),
	}

	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return nil, errClosed
	}
	conn := rc.conn
	rc.mu.Unlock()

	rc.subMu.Lock()
	id := rc.nextSubID
	rc.nextSubID++
	entry.id = id
	rc.subs[id] = entry
	rc.subMu.Unlock()

	if subscribeAfterRegisterHook != nil {
		subscribeAfterRegisterHook()
	}

	if conn != nil {
		if err := rc.attachSub(ctx, conn, entry); err != nil {
			rc.subMu.Lock()
			delete(rc.subs, id)
			rc.subMu.Unlock()
			return nil, fmt.Errorf("buzz: subscribe: %w", err)
		}
	}

	go func() {
		<-ctx.Done()
		rc.removeAndClose(id)
	}()

	return entry.out, nil
}

// attachSub opens a fresh library subscription for entry.filter on conn
// and starts a pump goroutine forwarding translated events onto
// entry.out. It is called both from Subscribe (initial attach) and from
// the reconnect loop's resubscribeAll (re-attach after every reconnect,
// using the same caller-owned ctx and out channel).
//
// FR-002: Subscribe registers a new entry into rc.subs, then releases
// subMu, then calls attachSub itself -- a window in which a concurrent
// reconnect's resubscribeAll can observe that same not-yet-attached entry
// and call attachSub on it first (subscribeAfterRegisterHook exists so a
// test can force exactly this interleaving deterministically). Both
// calls then legitimately pass the "is entry still registered" check
// below, so attachSub does NOT try to prevent a second, concurrent attach
// -- that would require distinguishing "a legitimate re-attach after a
// real reconnect" from "a duplicate attach of the same never-yet-attached
// entry," which is not knowable locally. Instead it makes a double attach
// SAFE: every attach bumps entry.generation, and pumpSub (below) checks
// its own generation before every forward, so at most one generation's
// pump is ever actively delivering onto entry.out even if two started.
//
// FR-003: attachSub must never start a pump that removeAndClose/Close
// have already stopped waiting for. The entry-registration check and the
// generation bump + wg.Add happen inside ONE rc.mu-guarded section --
// the same lock Close uses to set rc.closed = true -- so the two
// operations are strictly ordered: either Close's transition happens
// first (attachSub then sees rc.closed and refuses), or attachSub's
// Add happens first (and is therefore guaranteed visible to Close's
// later rc.pumpWG.Wait(), which will correctly wait for it). Lock
// ordering is rc.mu outer, subMu inner, everywhere in this file --
// never the reverse -- so nesting subMu inside this rc.mu section
// cannot deadlock against any other rc.mu/subMu use (see
// implementation-notes.md for the full audit).
func (rc *RelayClient) attachSub(ctx context.Context, conn relayConn, entry *subEntry) error {
	// Fast-path check: avoid the network round trip below if the entry
	// was already removed. Not itself a correctness guard -- the
	// authoritative check is the second one, after conn.Subscribe,
	// combined with the rc.closed check under rc.mu.
	rc.subMu.Lock()
	if _, ok := rc.subs[entry.id]; !ok {
		rc.subMu.Unlock()
		return errClosed
	}
	rc.subMu.Unlock()

	inner, err := conn.Subscribe(ctx, entry.filter)
	if err != nil {
		return err
	}

	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return errClosed
	}
	rc.subMu.Lock()
	if _, ok := rc.subs[entry.id]; !ok {
		rc.subMu.Unlock()
		rc.mu.Unlock()
		return errClosed
	}
	entry.generation++
	myGen := entry.generation
	entry.wg.Add(1)
	rc.subMu.Unlock()
	rc.pumpWG.Add(1)
	rc.mu.Unlock()

	go rc.pumpSub(inner, entry, myGen)
	return nil
}

// pumpSub forwards events from inner onto entry.out for as long as this
// attach's generation (myGen) remains current. If a later attachSub call
// supersedes it (entry.generation advances past myGen -- see attachSub's
// doc comment for how this can legitimately happen), this pump detects
// that on its next event and exits without forwarding, leaving delivery
// to the newer generation's own pump. This is what makes a double attach
// safe rather than merely detected: at most one generation ever forwards.
func (rc *RelayClient) pumpSub(inner <-chan nostr.Event, entry *subEntry, myGen int) {
	defer rc.pumpWG.Done()
	defer entry.wg.Done()
	for {
		select {
		case evt, ok := <-inner:
			if !ok {
				return
			}
			rc.subMu.Lock()
			stale := entry.generation != myGen
			rc.subMu.Unlock()
			if stale {
				return
			}
			select {
			case entry.out <- FromLibraryEvent(evt):
			case <-rc.closedCh:
				return
			}
		case <-rc.closedCh:
			return
		}
	}
}

// removeAndClose unregisters id (so no future reconnect re-attaches it),
// waits for EVERY pump ever started for that entry -- every generation,
// not just the most recent -- to finish (entry.wg; see attachSub and
// pumpSub) so none can still be sending when this closes entry.out, and
// then closes entry.out. It is a no-op if id is no longer registered,
// which happens when Close() got to it first (Close closes every still-
// registered entry's channel itself, under the same subMu, after its own
// global pumpWG.Wait()).
func (rc *RelayClient) removeAndClose(id int) {
	rc.subMu.Lock()
	entry, ok := rc.subs[id]
	if ok {
		delete(rc.subs, id)
	}
	rc.subMu.Unlock()
	if !ok {
		return
	}

	entry.wg.Wait()
	close(entry.out)
}

// Close tears down the connection, stops the reconnect loop, and closes
// every subscription channel. Idempotent.
func (rc *RelayClient) Close() error {
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return nil
	}
	rc.closed = true
	conn := rc.conn
	rc.mu.Unlock()

	close(rc.closedCh)

	var err error
	if conn != nil {
		err = conn.Close()
	}

	rc.pumpWG.Wait()

	rc.subMu.Lock()
	for _, e := range rc.subs {
		close(e.out)
	}
	rc.subs = make(map[int]*subEntry)
	rc.subMu.Unlock()

	return err
}
