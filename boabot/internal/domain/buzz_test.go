package domain_test

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeRelayClient is a minimal domain.RelayClient implementation used only
// to assert, at compile time and via a trivial call, that the interface
// shape declared in data-dictionary.md is exactly what internal/domain
// exposes -- with zero third-party Nostr library (or any other
// infrastructure) import required to satisfy it.
type fakeRelayClient struct {
	connected     bool
	authenticated bool
	published     []domain.Event
	subs          []domain.Filter
}

var _ domain.RelayClient = (*fakeRelayClient)(nil)

func (f *fakeRelayClient) Connect(_ context.Context) error {
	f.connected = true
	return nil
}

func (f *fakeRelayClient) Authenticate(_ context.Context) error {
	f.authenticated = true
	return nil
}

func (f *fakeRelayClient) Publish(_ context.Context, evt domain.Event) error {
	f.published = append(f.published, evt)
	return nil
}

func (f *fakeRelayClient) Subscribe(_ context.Context, filter domain.Filter) (<-chan domain.Event, error) {
	f.subs = append(f.subs, filter)
	ch := make(chan domain.Event)
	close(ch)
	return ch, nil
}

func (f *fakeRelayClient) Close() error {
	f.connected = false
	return nil
}

func TestRelayClient_InterfaceShape(t *testing.T) {
	ctx := context.Background()
	f := &fakeRelayClient{}

	if err := f.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !f.connected {
		t.Fatal("expected connected=true after Connect")
	}

	if err := f.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !f.authenticated {
		t.Fatal("expected authenticated=true after Authenticate")
	}

	evt := domain.Event{
		ID:        "deadbeef",
		PubKey:    "cafebabe",
		CreatedAt: 1234567890,
		Kind:      9,
		Tags:      [][]string{{"h", "channel-uuid"}},
		Content:   "hello",
		Sig:       "sig-hex",
	}
	if err := f.Publish(ctx, evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.published) != 1 || f.published[0].ID != evt.ID || f.published[0].Content != evt.Content {
		t.Fatalf("expected published event to round-trip through the interface, got %+v", f.published)
	}

	since := int64(100)
	until := int64(200)
	filter := domain.Filter{
		Kinds: []int{9},
		Tags:  map[string][]string{"h": {"channel-uuid"}},
		Since: &since,
		Until: &until,
		Limit: 50,
	}
	ch, err := f.Subscribe(ctx, filter)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(f.subs) != 1 || f.subs[0].Kinds[0] != 9 {
		t.Fatalf("expected filter to round-trip through the interface, got %+v", f.subs)
	}
	for range ch {
		t.Fatal("expected the fake's channel to be pre-closed with no events")
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.connected {
		t.Fatal("expected connected=false after Close")
	}
}

func TestEvent_ZeroValue(t *testing.T) {
	var evt domain.Event
	if evt.ID != "" || evt.Kind != 0 || evt.Tags != nil || evt.Content != "" {
		t.Fatalf("expected zero-value Event to be fully empty, got %+v", evt)
	}
}

func TestFilter_ZeroValue(t *testing.T) {
	var f domain.Filter
	if f.Kinds != nil || f.Tags != nil || f.Since != nil || f.Until != nil || f.Limit != 0 {
		t.Fatalf("expected zero-value Filter to be fully empty, got %+v", f)
	}
}
