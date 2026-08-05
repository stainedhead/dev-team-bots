package buzz

import (
	"context"
	"encoding/json"
	"fmt"

	"fiatjaf.com/nostr"
)

// Profile is the (name, bot type, description) triple published as a
// kind:0 event on first successful connection (D7/FR-011). Parameters are
// passed in by the caller (a later phase wires them from bot config and
// AGENTS.md) rather than hardcoded here.
type Profile struct {
	Name        string
	BotType     string
	Description string
}

// profileMetadata is the kind:0 content JSON shape (NIP-01). Name is the
// canonical display name; About carries the bot type and description so a
// human in the workspace sees a named, described agent rather than a bare
// pubkey.
type profileMetadata struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

func (p Profile) content() (string, error) {
	about := p.Description
	if p.BotType != "" {
		if about != "" {
			about = fmt.Sprintf("%s (%s)", about, p.BotType)
		} else {
			about = p.BotType
		}
	}
	b, err := json.Marshal(profileMetadata{Name: p.Name, About: about})
	if err != nil {
		return "", fmt.Errorf("buzz: encode profile content: %w", err)
	}
	return string(b), nil
}

// publishProfile signs and publishes p as a kind:0 event on conn. It is
// not conditional on explicit relay enrollment (FR-011): virtual members
// are permitted to publish community-global events per NIP-AA.
func (rc *RelayClient) publishProfile(ctx context.Context, conn relayConn, p Profile) error {
	content, err := p.content()
	if err != nil {
		return err
	}

	evt := nostr.Event{
		Kind:      nostr.KindProfileMetadata,
		CreatedAt: nostr.Now(),
		Content:   content,
	}
	evt.PubKey = rc.pk
	if err := evt.Sign(rc.sk); err != nil {
		return fmt.Errorf("buzz: publish profile: sign: %w", err)
	}
	if err := conn.Publish(ctx, evt); err != nil {
		return fmt.Errorf("buzz: publish profile: %w", err)
	}
	return nil
}
