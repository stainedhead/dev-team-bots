package buzz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// APITokenSecretName is the domain.SecretRef.Name used to resolve
// BUZZ_API_TOKEN through domain.SecretStore, per FR-010. The default
// provider chain resolves this from the BUZZ_API_TOKEN environment
// variable, falling back to a buzz_api_token credentials-file entry --
// the same resolution path as PrivateKeySecretName (FR-002).
const APITokenSecretName = "buzz_api_token"

// LoadAPIToken resolves BUZZ_API_TOKEN through store, scoped to botName.
// Unlike LoadKeypair, an all-provider-miss resolution is not itself an
// error: the token is optional unless the relay requires it
// (WithAPIToken's `required` flag, enforced at Connect time), so
// LoadAPIToken reports a clean miss via found=false rather than failing
// the caller.
//
// A genuine provider failure is different from a clean miss (FR-053
// exists precisely so an operator can tell them apart) and is NOT
// swallowed here: it is distinguished via errors.As against
// *secret.NotFoundError -- the sentinel-ish error internal/infrastructure/
// secret.Store.Get returns specifically for "every provider was
// consulted and none had this reference" (see internal/infrastructure/
// secret/store.go). Any other error (a provider timeout surfaced as part
// of a wrapped chain error, a malformed value, etc.) is returned to the
// caller to log/act on, per FR-053's intent.
func LoadAPIToken(ctx context.Context, store domain.SecretStore, botName string) (token string, found bool, err error) {
	raw, err := store.Get(ctx, domain.SecretRef{Name: APITokenSecretName, Bot: botName})
	if err != nil {
		var notFound *secret.NotFoundError
		if errors.As(err, &notFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("buzz: failed to resolve API token: %w", err)
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, nil
	}
	return trimmed, true, nil
}
