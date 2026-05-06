package team

import (
	"errors"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/anthropic"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

// localProviderFactory implements domain.ProviderFactory for local single-binary
// operation.  It pre-builds every provider at construction time so that Get is
// a simple map lookup.
type localProviderFactory struct {
	providers map[string]domain.ModelProvider
	errs      map[string]error
}

// newLocalProviderFactory builds a provider for each entry in cfgs.
// Unsupported provider types are recorded as errors and returned by Get.
func newLocalProviderFactory(cfgs []config.ProviderConfig) *localProviderFactory {
	f := &localProviderFactory{
		providers: make(map[string]domain.ModelProvider, len(cfgs)),
		errs:      make(map[string]error, len(cfgs)),
	}
	for _, pc := range cfgs {
		p, err := buildProvider(pc)
		if err != nil {
			f.errs[pc.Name] = err
		} else {
			f.providers[pc.Name] = p
		}
	}
	return f
}

// Get returns the named ModelProvider, or an error if it could not be built or
// is unknown.
func (f *localProviderFactory) Get(name string) (domain.ModelProvider, error) {
	if err, bad := f.errs[name]; bad {
		return nil, err
	}
	if p, ok := f.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("team: unknown provider %q", name)
}

// buildProvider constructs a ModelProvider for a single ProviderConfig.
func buildProvider(pc config.ProviderConfig) (domain.ModelProvider, error) {
	switch pc.Type {
	case "anthropic":
		p, err := anthropic.NewFromEnv(pc.ModelID)
		if err != nil {
			return nil, fmt.Errorf("team: build anthropic provider %q: %w", pc.Name, err)
		}
		return p, nil

	case "bedrock":
		return nil, errors.New(
			"team: bedrock provider requires AWS SDK setup; use ANTHROPIC_API_KEY for local mode",
		)

	case "openai":
		return nil, errors.New("team: openai provider not yet implemented")

	default:
		return nil, fmt.Errorf("team: unsupported provider type %q", pc.Type)
	}
}
