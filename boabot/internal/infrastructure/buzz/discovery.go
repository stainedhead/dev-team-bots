package buzz

import (
	"context"
	"encoding/json"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// startDiscovery implements F1: subscribes to the relay-signed NIP-29
// discovery kinds (39000 group metadata, 39002 member list) over the
// already-authenticated WebSocket -- the primary and, for P0, the only
// discovery mechanism per FR-013 (the REST GET /api/channels?member=true
// listing is never called anywhere in this package).
func (m *Monitor) startDiscovery(ctx context.Context) error {
	ch, err := m.relay.Subscribe(ctx, domain.Filter{Kinds: []int{kindChannelMetadata, kindChannelMembers}})
	if err != nil {
		return err
	}
	go m.consumeDiscovery(ctx, ch)
	return nil
}

func (m *Monitor) consumeDiscovery(ctx context.Context, ch <-chan domain.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			m.handleDiscoveryEvent(ctx, evt)
		}
	}
}

// channelMetadataContent is NIP-29 kind:39000's content JSON shape.
type channelMetadataContent struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

// handleDiscoveryEvent extracts the channel UUID (the addressable "d" tag
// both 39000 and 39002 carry). Kind:39000's name/topic are routed through
// F7's content screener before appearing in logs (FR-028: "channel names
// and topics" are explicitly named as untrusted, regardless of author).
//
// Only kind:39002 (the relay-signed member list) carrying our OWN pubkey
// as a member -- i.e. an actual "p" tag entry equal to our own pubkey --
// is treated as membership proof and triggers a kind:9 subscription
// (F2/FR-013's literal "channels it is a member of"). Kind:39000 metadata
// alone is deliberately NOT sufficient: on a public channel, its metadata
// may be visible even to a pubkey that isn't a member, and subscribing to
// kind:9 on that basis would open a channel subscription we have no
// business holding. Metadata for an as-yet-undiscovered channel is still
// logged (via logChannelMetadata) so an operator can see it was observed,
// even though it doesn't by itself trigger a subscription.
func (m *Monitor) handleDiscoveryEvent(ctx context.Context, evt domain.Event) {
	channelUUID := firstTagValue(evt.Tags, "d")
	if channelUUID == "" {
		return
	}

	switch evt.Kind {
	case kindChannelMetadata:
		m.logChannelMetadata(channelUUID, evt.Content)
	case kindChannelMembers:
		if hasTagValue(evt.Tags, "p", m.cfg.AgentPubKeyHex) {
			m.subscribeToChannel(ctx, channelUUID)
		}
	}
}

func (m *Monitor) logChannelMetadata(channelUUID, content string) {
	var meta channelMetadataContent
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return // not a decodable metadata payload; nothing to screen or log
	}
	name := m.screen("channel_name", meta.Name)
	topic := m.screen("channel_topic", meta.About)
	m.logger.Info("buzz monitor: discovered channel", "channel", channelUUID, "name", name, "topic", topic)
}

// subscribeToChannel implements F2: subscribes to kind:9 messages for
// channelUUID, scoped by #h, if not already subscribed. Also the F3
// auto-subscribe target for kind:44100.
func (m *Monitor) subscribeToChannel(ctx context.Context, channelUUID string) {
	m.channelsMu.Lock()
	if _, ok := m.channels[channelUUID]; ok {
		m.channelsMu.Unlock()
		return
	}
	subCtx, cancel := context.WithCancel(ctx)
	m.channels[channelUUID] = cancel
	m.channelsMu.Unlock()

	ch, err := m.relay.Subscribe(subCtx, domain.Filter{
		Kinds: []int{kindChannelMessage},
		Tags:  map[string][]string{"h": {channelUUID}},
	})
	if err != nil {
		m.logger.Error("buzz monitor: subscribe to channel failed", "channel", channelUUID, "err", err)
		m.channelsMu.Lock()
		delete(m.channels, channelUUID)
		m.channelsMu.Unlock()
		cancel()
		return
	}

	m.logger.Info("buzz monitor: subscribed to channel", "channel", channelUUID)
	go m.consumeChannel(subCtx, channelUUID, ch)
}

// unsubscribeFromChannel implements F3's unsubscribe half (kind:44101):
// cancels the channel's kind:9 subscription context and drops it from the
// discovered-channel set, so a later kind:44100 for the same channel
// re-subscribes rather than being treated as a no-op duplicate.
func (m *Monitor) unsubscribeFromChannel(channelUUID string) {
	m.channelsMu.Lock()
	cancel, ok := m.channels[channelUUID]
	if ok {
		delete(m.channels, channelUUID)
	}
	m.channelsMu.Unlock()

	if ok {
		cancel()
		m.logger.Info("buzz monitor: unsubscribed from channel", "channel", channelUUID)
	}
}

func (m *Monitor) consumeChannel(ctx context.Context, channelUUID string, ch <-chan domain.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			m.handleChannelEvent(ctx, channelUUID, evt)
		}
	}
}

// startMembershipWatch implements F3's auto-subscribe half: subscribes to
// kind:44100 (member added) and kind:44101 (member removed), both p-gated
// to the agent's own pubkey per FR-016/F4 -- the #p filter here is exactly
// what RelayClient.Subscribe's own guard (guard.go) requires, so this
// subscription is guaranteed correct by construction, not just by
// intention.
func (m *Monitor) startMembershipWatch(ctx context.Context) error {
	ch, err := m.relay.Subscribe(ctx, domain.Filter{
		Kinds: []int{kindMemberAdded, kindMemberRemoved},
		Tags:  map[string][]string{"p": {m.cfg.AgentPubKeyHex}},
	})
	if err != nil {
		return err
	}
	go m.consumeMembership(ctx, ch)
	return nil
}

func (m *Monitor) consumeMembership(ctx context.Context, ch <-chan domain.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			m.handleMembershipEvent(ctx, evt)
		}
	}
}

// handleMembershipEvent reads the target channel's UUID from kind:44100/
// 44101's "h" tag -- the same tag kind:9 group messages use (data-
// dictionary.md's Filter.Tags example groups h/p/e together as the
// standard NIP-29-family tag set). This is an UNVERIFIED assumption: the
// PRD documents these kinds' existence and their #p gating requirement,
// but not their channel-identifying tag by name. If the real relay uses a
// different tag here, F3's auto-subscribe/unsubscribe silently never
// fires -- an empty stream with no error, the same failure class the PRD
// warns about for the two protocol traps this phase explicitly guards
// against. Flagged on implementation-notes.md's manual-verification
// checklist for confirmation against a live relay.
func (m *Monitor) handleMembershipEvent(ctx context.Context, evt domain.Event) {
	channelUUID := firstTagValue(evt.Tags, "h")
	if channelUUID == "" {
		return
	}
	switch evt.Kind {
	case kindMemberAdded:
		m.subscribeToChannel(ctx, channelUUID)
	case kindMemberRemoved:
		m.unsubscribeFromChannel(channelUUID)
	}
}
