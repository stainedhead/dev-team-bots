package buzz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip17"
	"fiatjaf.com/nostr/nip59"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- P2.2/P2.3: DM subscription, gift-unwrap, self-filter, author-gate, dispatch ---

// TestMonitor_HandleDMEvent_ValidGiftWrap_DecryptsAndDispatches is P2.2/P2.3's
// core acceptance criterion: a synthetic gift-wrapped kind:1059 event
// (built with the real nip17.PrepareMessage, exactly as a real sender's
// client would produce) decrypts to the expected plaintext and dispatches
// through the same BuzzTaskDispatcher bridge channel messages use, tagged
// with the DM conversation's threadID and the outer gift-wrap event's own
// ID for dedup.
func TestMonitor_HandleDMEvent_ValidGiftWrap_DecryptsAndDispatches(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	senderSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	senderKr := NewDMKeyer(senderSK)

	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "dm-task-1"}, nil
		},
	}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td), WithDMKeyer(selfKr))

	_, toThem, err := nip17.PrepareMessage(context.Background(), "hello from a DM", nil, senderKr, selfSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toThem)

	m.handleDMEvent(context.Background(), evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
	call := td.GetCalls()[0]
	if call.EventID != evt.ID {
		t.Fatalf("expected EventID == outer gift-wrap event ID %q, got %q (dedup must key off the outer envelope)", evt.ID, call.EventID)
	}
	if call.ThreadID != dmThreadID(senderSK.Public()) {
		t.Fatalf("expected ThreadID %q, got %q", dmThreadID(senderSK.Public()), call.ThreadID)
	}
	if !strings.Contains(call.Instruction, "hello from a DM") {
		t.Fatalf("expected the decrypted plaintext in the dispatched instruction, got %q", call.Instruction)
	}
	if call.BotName != "test-bot" {
		t.Fatalf("expected BotName test-bot, got %q", call.BotName)
	}
}

// TestMonitor_HandleDMEvent_SelfCopy_NotDispatched is spec.md's critical
// self-message-loop edge case: nip17.PrepareMessage produces a self-copy
// (toUs) of every outbound DM. Our own #p-tagged-to-self subscription
// receives it too -- it must be recognized (rumor.pubkey == our own pubkey)
// and skipped, or a reply-to-self-copy would loop forever.
func TestMonitor_HandleDMEvent_SelfCopy_NotDispatched(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	otherSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)

	td := &mocks.BuzzTaskDispatcher{}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td), WithDMKeyer(selfKr))

	toUs, _, err := nip17.PrepareMessage(context.Background(), "my own sent message", nil, selfKr, otherSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toUs)

	m.handleDMEvent(context.Background(), evt)

	time.Sleep(50 * time.Millisecond)
	if len(td.GetCalls()) != 0 {
		t.Fatal("expected the self-copy not to be dispatched (self-message-loop prevention)")
	}
}

// TestMonitor_HandleDMEvent_UnauthorizedSender_NotDispatched is FR-204: a
// DM from a sender outside the configured author gate must not dispatch.
func TestMonitor_HandleDMEvent_UnauthorizedSender_NotDispatched(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	senderSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	senderKr := NewDMKeyer(senderSK)

	cfg := testConfig()
	cfg.RespondToAllowlist = []string{"some-other-allowed-pubkey-hex"}
	td := &mocks.BuzzTaskDispatcher{}
	m := NewMonitor(fr, cfg, q, nil, WithMonitorLogger(discardLogger()), WithTaskDispatcher(td), WithDMKeyer(selfKr))

	_, toThem, err := nip17.PrepareMessage(context.Background(), "unsolicited dm", nil, senderKr, selfSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toThem)

	m.handleDMEvent(context.Background(), evt)

	time.Sleep(50 * time.Millisecond)
	if len(td.GetCalls()) != 0 {
		t.Fatal("expected a DM from an unauthorized sender not to dispatch (FR-204)")
	}
}

// TestMonitor_HandleDMEvent_AuthorizedSender_Dispatches is FR-204's positive
// case: reusing the same author-gate mechanism channel dispatch uses, an
// allow-listed sender's DM does dispatch.
func TestMonitor_HandleDMEvent_AuthorizedSender_Dispatches(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	senderSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	senderKr := NewDMKeyer(senderSK)

	cfg := testConfig()
	cfg.RespondToAllowlist = []string{senderSK.Public().Hex()}
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "dm-task-2"}, nil
		},
	}
	m := NewMonitor(fr, cfg, q, nil, WithMonitorLogger(discardLogger()), WithTaskDispatcher(td), WithDMKeyer(selfKr))

	_, toThem, err := nip17.PrepareMessage(context.Background(), "hi, allowed sender here", nil, senderKr, selfSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toThem)

	m.handleDMEvent(context.Background(), evt)

	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })
}

// TestMonitor_HandleDMEvent_MalformedCiphertext_LoggedNotCrash covers
// spec.md's "Malformed/corrupted gift-wrap event" edge case: a kind:1059
// event whose content is not valid gift-wrap ciphertext must be logged and
// skipped, never crash the monitor goroutine.
func TestMonitor_HandleDMEvent_MalformedCiphertext_LoggedNotCrash(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	td := &mocks.BuzzTaskDispatcher{}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td), WithDMKeyer(NewDMKeyer(selfSK)))

	evt := domain.Event{
		ID:      strings.Repeat("ab", 32),
		PubKey:  nostr.Generate().Public().Hex(),
		Kind:    kindGiftWrap,
		Tags:    [][]string{{"p", selfSK.Public().Hex()}},
		Content: "not valid gift-wrap ciphertext",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected a malformed gift-wrap event to be handled without panic, got: %v", r)
		}
	}()
	m.handleDMEvent(context.Background(), evt)

	time.Sleep(20 * time.Millisecond)
	if len(td.GetCalls()) != 0 {
		t.Fatal("expected no dispatch for a malformed gift-wrap event")
	}
}

// TestMonitor_HandleDMEvent_TranslateFailure_LoggedNotCrash covers an
// untranslatable domain.Event (e.g. a malformed hex ID from a
// non-conformant relay) -- must not crash before gift-unwrap is even
// attempted.
func TestMonitor_HandleDMEvent_TranslateFailure_LoggedNotCrash(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	td := &mocks.BuzzTaskDispatcher{}
	m := newTestMonitor(fr, q, nil, WithTaskDispatcher(td), WithDMKeyer(NewDMKeyer(selfSK)))

	evt := domain.Event{ID: "not-valid-hex", Kind: kindGiftWrap, Content: "x"}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected an untranslatable event to be handled without panic, got: %v", r)
		}
	}()
	m.handleDMEvent(context.Background(), evt)

	time.Sleep(20 * time.Millisecond)
	if len(td.GetCalls()) != 0 {
		t.Fatal("expected no dispatch for an untranslatable event")
	}
}

// TestMonitor_HandleDMEvent_NoTaskDispatcher_LoggedNotCrash verifies DM
// handling is safe (no nil-pointer panic) when no BuzzTaskDispatcher is
// wired -- mirroring the channel path's own no-bridge fallback isolation.
func TestMonitor_HandleDMEvent_NoTaskDispatcher_LoggedNotCrash(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	senderSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	senderKr := NewDMKeyer(senderSK)
	m := newTestMonitor(fr, q, nil, WithDMKeyer(selfKr)) // no WithTaskDispatcher

	_, toThem, err := nip17.PrepareMessage(context.Background(), "hello", nil, senderKr, selfSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toThem)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected no panic with no task dispatcher wired, got: %v", r)
		}
	}()
	m.handleDMEvent(context.Background(), evt)
}

// --- P3.1: log-safety --------------------------------------------------------

// TestMonitor_HandleDMEvent_ValidDM_PlaintextNeverLogged is P3.1's core
// acceptance criterion for the inbound path: processing a valid DM (decrypt
// success, dispatch) must never write the decrypted plaintext content, this
// persona's private key (hex or raw), or a nip44 conversation key into any
// log line.
func TestMonitor_HandleDMEvent_ValidDM_PlaintextNeverLogged(t *testing.T) {
	var logBuf bytes.Buffer
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	senderSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	senderKr := NewDMKeyer(senderSK)

	const secretContent = "the-quarterly-numbers-are-confidential-42"
	td := &mocks.BuzzTaskDispatcher{
		DispatchFn: func(_ context.Context, _, _, _, _ string) (domain.BuzzDispatchResult, error) {
			return domain.BuzzDispatchResult{TaskID: "dm-task-log-safety"}, nil
		},
	}
	m := NewMonitor(fr, testConfig(), q, nil,
		WithMonitorLogger(slog.New(slog.NewTextHandler(&logBuf, nil))),
		WithTaskDispatcher(td), WithDMKeyer(selfKr))

	_, toThem, err := nip17.PrepareMessage(context.Background(), secretContent, nil, senderKr, selfSK.Public(), nil)
	if err != nil {
		t.Fatalf("PrepareMessage: %v", err)
	}
	evt := FromLibraryEvent(toThem)

	m.handleDMEvent(context.Background(), evt)
	waitFor(t, time.Second, func() bool { return len(td.GetCalls()) == 1 })

	got := logBuf.String()
	if strings.Contains(got, secretContent) {
		t.Fatalf("decrypted DM plaintext leaked into logs: %q", got)
	}
	if strings.Contains(got, selfSK.Hex()) {
		t.Fatalf("this persona's private key leaked into logs: %q", got)
	}
	if strings.Contains(got, senderSK.Hex()) {
		t.Fatalf("the sender's private key leaked into logs: %q", got)
	}
}

// TestMonitor_HandleDMEvent_MalformedDM_NoErrTextLogged verifies the
// gift-unwrap-failure log path specifically omits the library's own error
// text (see handleDMEvent's doc comment: nip44's conversation-key
// derivation has a rare error path that formats raw secret-key bytes into
// its error string).
func TestMonitor_HandleDMEvent_MalformedDM_NoErrTextLogged(t *testing.T) {
	var logBuf bytes.Buffer
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	m := NewMonitor(fr, testConfig(), q, nil,
		WithMonitorLogger(slog.New(slog.NewTextHandler(&logBuf, nil))),
		WithDMKeyer(NewDMKeyer(selfSK)))

	evt := domain.Event{
		ID:      strings.Repeat("cd", 32),
		PubKey:  nostr.Generate().Public().Hex(),
		Kind:    kindGiftWrap,
		Tags:    [][]string{{"p", selfSK.Public().Hex()}},
		Content: "not valid gift-wrap ciphertext",
	}
	m.handleDMEvent(context.Background(), evt)

	got := logBuf.String()
	if !strings.Contains(got, "gift-unwrap failed") {
		t.Fatalf("expected the malformed-event log line, got: %q", got)
	}
	if strings.Contains(got, "err=") {
		t.Fatalf("expected no err= field on the gift-unwrap failure log (library error text may embed key material), got: %q", got)
	}
}

// TestMonitor_PublishDMReply_LogsNeverContainPlaintextOrKeys mirrors the
// inbound check for the outbound path: preparing/publishing a DM reply
// must never log its content or this persona's private key.
func TestMonitor_PublishDMReply_LogsNeverContainPlaintextOrKeys(t *testing.T) {
	var logBuf bytes.Buffer
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	recipientSK := nostr.Generate()
	m := NewMonitor(fr, testConfig(), q, nil,
		WithMonitorLogger(slog.New(slog.NewTextHandler(&logBuf, nil))),
		WithDMKeyer(NewDMKeyer(selfSK)))

	const secretReply = "the-secret-reply-content-99"
	if err := m.publishDMReply(context.Background(), recipientSK.Public(), dmThreadID(recipientSK.Public()), secretReply); err != nil {
		t.Fatalf("publishDMReply: %v", err)
	}

	got := logBuf.String()
	if strings.Contains(got, secretReply) {
		t.Fatalf("DM reply plaintext leaked into logs: %q", got)
	}
	if strings.Contains(got, selfSK.Hex()) {
		t.Fatalf("this persona's private key leaked into logs: %q", got)
	}
}

// --- P2.2: DM subscription wiring ---------------------------------------------

// TestMonitor_StartDMSubscription_UsesGiftWrapKindAndPGateFilter verifies
// the subscription filter itself: kind:1059, #p tagged to self -- the exact
// shape guard.go's p-gate (FR-016) already requires for this kind.
func TestMonitor_StartDMSubscription_UsesGiftWrapKindAndPGateFilter(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := newTestMonitor(fr, q, nil, WithDMKeyer(NewDMKeyer(nostr.Generate())))

	m.startDMSubscription(context.Background())

	waitFor(t, time.Second, func() bool { return fr.subCount() >= 1 })
	sub := fr.subAt(0)
	if len(sub.filter.Kinds) != 1 || sub.filter.Kinds[0] != kindGiftWrap {
		t.Fatalf("expected a kind:1059 filter, got %+v", sub.filter)
	}
	if got := sub.filter.Tags["p"]; len(got) != 1 || got[0] != m.cfg.AgentPubKeyHex {
		t.Fatalf("expected #p == %s, got %+v", m.cfg.AgentPubKeyHex, sub.filter.Tags)
	}
}

// TestMonitor_StartDMSubscription_SubscribeError_LoggedNotFatal covers
// spec.md's "Relay doesn't support kind:1059" edge case: a subscribe
// failure must be logged and isolated, never panic.
func TestMonitor_StartDMSubscription_SubscribeError_LoggedNotFatal(t *testing.T) {
	fr := newFakeRelay()
	fr.subscribeErrAt = map[int]error{0: errors.New("relay rejects kind 1059")}
	q := &mocks.MessageQueue{}
	m := newTestMonitor(fr, q, nil, WithDMKeyer(NewDMKeyer(nostr.Generate())))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected a dm subscribe failure to be handled without panic, got: %v", r)
		}
	}()
	m.startDMSubscription(context.Background())
}

// TestMonitor_Run_DMKeyerWired_AlsoOpensDMSubscription is an integration-
// level check that run() wires the DM subscription (in addition to
// discovery/membership-watch) exactly when a dmKeyer is configured.
func TestMonitor_Run_DMKeyerWired_AlsoOpensDMSubscription(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := NewMonitor(fr, testConfig(), q, nil, WithMonitorLogger(discardLogger()), WithDMKeyer(NewDMKeyer(nostr.Generate())))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Discovery (1) + membership watch (1) + DM subscription (1) = 3.
	waitFor(t, time.Second, func() bool { return fr.subCount() >= 3 })
}

// TestMonitor_Run_NoDMKeyer_NoDMSubscription verifies the pre-existing
// behaviour (no DM support) is unaffected when no DM keyer is wired.
func TestMonitor_Run_NoDMKeyer_NoDMSubscription(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := NewMonitor(fr, testConfig(), q, nil, WithMonitorLogger(discardLogger()))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, time.Second, func() bool { return fr.subCount() >= 2 })

	time.Sleep(50 * time.Millisecond)
	if got := fr.subCount(); got != 2 {
		t.Fatalf("expected exactly 2 subscriptions (discovery, membership) with no DM keyer wired, got %d", got)
	}
}

// --- P2.4: DM reply publishing -------------------------------------------------

// TestMonitor_PublishDMReply_GiftUnwrapsCorrectlyForRecipient is P2.4's core
// acceptance criterion: a published DM reply gift-unwraps correctly for the
// original sender's keypair, and preserves NIP-17's ephemeral-envelope
// privacy property (the gift-wrap's own PubKey is NOT the persona's real
// pubkey -- proof PublishRaw, not the signing/re-keying Publish, was used).
func TestMonitor_PublishDMReply_GiftUnwrapsCorrectlyForRecipient(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	recipientSK := nostr.Generate()
	selfKr := NewDMKeyer(selfSK)
	m := newTestMonitor(fr, q, nil, WithDMKeyer(selfKr))

	threadID := dmThreadID(recipientSK.Public())
	if err := m.publishDMReply(context.Background(), recipientSK.Public(), threadID, "the answer"); err != nil {
		t.Fatalf("publishDMReply: %v", err)
	}

	published := fr.publishedSnapshot()
	if len(published) != 2 {
		t.Fatalf("expected 2 published events (toUs self-copy + toThem recipient copy), got %d", len(published))
	}
	for _, evt := range published {
		if evt.Kind != kindGiftWrap {
			t.Fatalf("expected kind:1059 for every published event, got %d", evt.Kind)
		}
		if evt.PubKey == selfSK.Public().Hex() {
			t.Fatal("expected the gift-wrap envelope's PubKey to be an ephemeral key, not the persona's real pubkey (NIP-17 privacy property) -- PublishRaw must be used, not Publish")
		}
	}

	var toThem *domain.Event
	for i := range published {
		if firstTagValue(published[i].Tags, "p") == recipientSK.Public().Hex() {
			toThem = &published[i]
		}
	}
	if toThem == nil {
		t.Fatal("expected one published event p-tagged to the recipient")
	}

	recipientKr := NewDMKeyer(recipientSK)
	nevt, err := ToLibraryEvent(*toThem)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	rumor, err := nip59.GiftUnwrap(nevt, func(otherpubkey nostr.PubKey, ciphertext string) (string, error) {
		return recipientKr.Decrypt(context.Background(), ciphertext, otherpubkey)
	})
	if err != nil {
		t.Fatalf("gift unwrap failed for the recipient's own keypair: %v", err)
	}
	if rumor.Content != "the answer" {
		t.Fatalf("expected decrypted content %q, got %q", "the answer", rumor.Content)
	}
	if rumor.PubKey != selfSK.Public() {
		t.Fatalf("expected the rumor's author to be our persona's pubkey, got %s", rumor.PubKey.Hex())
	}
}

// TestMonitor_PublishDMReply_NoKeyerConfigured_ReturnsError verifies
// publishDMReply fails closed (not silently) when no dmKeyer is wired.
func TestMonitor_PublishDMReply_NoKeyerConfigured_ReturnsError(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := newTestMonitor(fr, q, nil) // no WithDMKeyer

	err := m.publishDMReply(context.Background(), nostr.Generate().Public(), "dm:x", "hi")
	if err == nil {
		t.Fatal("expected an error when no dmKeyer is configured")
	}
}

// TestMonitor_HandleResult_DMDispatchedTask_PublishesViaDMPath verifies
// HandleResult routes a DM-dispatched task's eventual result through
// publishDMReply (FR-209), not the channel-shaped publishReply.
func TestMonitor_HandleResult_DMDispatchedTask_PublishesViaDMPath(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	selfSK := nostr.Generate()
	recipientSK := nostr.Generate()
	m := newTestMonitor(fr, q, nil, WithDMKeyer(NewDMKeyer(selfSK)))

	threadID := dmThreadID(recipientSK.Public())
	m.awaitDMResult("dm-task-3", recipientSK.Public(), threadID)

	m.HandleResult(context.Background(), domain.TaskResultPayload{TaskID: "dm-task-3", Output: "the dm answer", Success: true})

	published := fr.publishedSnapshot()
	found := false
	for _, evt := range published {
		if evt.Kind != kindGiftWrap {
			t.Fatalf("expected only kind:1059 events for a DM reply, got kind %d", evt.Kind)
		}
		found = true
	}
	if !found {
		t.Fatal("expected the DM task's result to be published via the gift-wrap DM path")
	}

	m.mu.Lock()
	_, stillPending := m.pending["dm-task-3"]
	m.mu.Unlock()
	if stillPending {
		t.Fatal("expected the pending entry to be popped after HandleResult")
	}
}
