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
	logger            *slog.Logger
	profile           *Profile

	mu               sync.Mutex
	conn             relayConn
	closed           bool
	watchStarted     bool
	profilePublished bool

	closedCh chan struct{}
	pumpWG   sync.WaitGroup

	subMu     sync.Mutex
	subs      map[int]*subEntry
	nextSubID int
}

// subEntry is a registered subscription. Every field is only ever mutated
// under RelayClient.subMu.
type subEntry struct {
	id       int
	ctx      context.Context
	filter   nostr.Filter
	out      chan domain.Event
	pumpDone chan struct{} // set by the most recent attachSub; closed by that pump on exit
}

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
// every reconnect").
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
			return nil
		}
		if !isNoChallengeErr(err) {
			return fmt.Errorf("buzz: authenticate: %w", err)
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

func (rc *RelayClient) buildSignFn() func(context.Context, *nostr.Event) error {
	return func(ctx context.Context, evt *nostr.Event) error {
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

// Subscribe registers f and, if currently connected, attaches it
// immediately. The returned channel survives reconnects: it is
// re-attached to a fresh underlying subscription after every reconnect
// (see reconnect.go) and is only closed when ctx is canceled or the
// RelayClient itself is closed.
func (rc *RelayClient) Subscribe(ctx context.Context, f domain.Filter) (<-chan domain.Event, error) {
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
// the reconnect loop (re-attach after every reconnect, using the same
// caller-owned ctx and out channel).
//
// The entry must still be present in rc.subs (checked and mutated
// atomically under subMu, alongside creating this attach generation's
// pumpDone channel) so a concurrent removeAndClose can never observe a
// pumpWG.Add() that races past its own wait -- see removeAndClose.
func (rc *RelayClient) attachSub(ctx context.Context, conn relayConn, entry *subEntry) error {
	rc.subMu.Lock()
	if _, ok := rc.subs[entry.id]; !ok {
		rc.subMu.Unlock()
		return errClosed
	}
	inner, err := conn.Subscribe(ctx, entry.filter)
	if err != nil {
		rc.subMu.Unlock()
		return err
	}
	done := make(chan struct{})
	entry.pumpDone = done
	rc.pumpWG.Add(1)
	rc.subMu.Unlock()

	go rc.pumpSub(inner, entry.out, done)
	return nil
}

func (rc *RelayClient) pumpSub(inner <-chan nostr.Event, out chan<- domain.Event, done chan<- struct{}) {
	defer rc.pumpWG.Done()
	defer close(done)
	for {
		select {
		case evt, ok := <-inner:
			if !ok {
				return
			}
			select {
			case out <- FromLibraryEvent(evt):
			case <-rc.closedCh:
				return
			}
		case <-rc.closedCh:
			return
		}
	}
}

// removeAndClose unregisters id (so no future reconnect re-attaches it),
// waits for any in-flight pump for that entry to finish (so it is never
// still writing to entry.out when this closes it -- see pumpSub), and
// closes entry.out. It is a no-op if id is no longer registered, which
// happens when Close() got to it first (Close closes every still-
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

	if entry.pumpDone != nil {
		<-entry.pumpDone
	}
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
