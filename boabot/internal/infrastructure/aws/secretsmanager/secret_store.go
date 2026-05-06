// Package secretsmanager provides a cached AWS Secrets Manager adapter.
package secretsmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

const defaultTTL = 5 * time.Minute

// SecretsManagerClient is the subset of the AWS SDK client required by SecretStore.
type SecretsManagerClient interface {
	GetSecretValue(ctx context.Context, params *awssm.GetSecretValueInput, optFns ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error)
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// SecretStore fetches secrets from AWS Secrets Manager with a TTL-based
// in-process cache to avoid redundant API calls on startup.
type SecretStore struct {
	client SecretsManagerClient
	ttl    time.Duration
	now    func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewSecretStore constructs a SecretStore with the default 5-minute TTL.
func NewSecretStore(client SecretsManagerClient) *SecretStore {
	return NewSecretStoreWithClock(client, defaultTTL, time.Now)
}

// NewSecretStoreWithClock constructs a SecretStore with a custom TTL and clock.
// Intended for use in tests only.
func NewSecretStoreWithClock(client SecretsManagerClient, ttl time.Duration, now func() time.Time) *SecretStore {
	return &SecretStore{
		client: client,
		ttl:    ttl,
		now:    now,
		cache:  make(map[string]cacheEntry),
	}
}

// GetSecret returns the plaintext value of the named secret. Cached values are
// returned without an API call if they have not yet expired.
func (s *SecretStore) GetSecret(ctx context.Context, secretID string) (string, error) {
	if v, ok := s.fromCache(secretID); ok {
		return v, nil
	}

	out, err := s.client.GetSecretValue(ctx, &awssm.GetSecretValueInput{
		SecretId: &secretID,
	})
	if err != nil {
		return "", fmt.Errorf("secrets manager: get %s: %w", secretID, err)
	}

	var value string
	switch {
	case out.SecretString != nil:
		value = *out.SecretString
	case out.SecretBinary != nil:
		value = string(out.SecretBinary)
	default:
		return "", fmt.Errorf("secrets manager: %s has neither SecretString nor SecretBinary", secretID)
	}

	s.store(secretID, value)
	return value, nil
}

// GetSecretJSON fetches the secret and unmarshals it as JSON into out.
func (s *SecretStore) GetSecretJSON(ctx context.Context, secretID string, out interface{}) error {
	raw, err := s.GetSecret(ctx, secretID)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("secrets manager: unmarshal %s: %w", secretID, err)
	}
	return nil
}

func (s *SecretStore) fromCache(secretID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[secretID]
	if !ok || s.now().After(e.expiresAt) {
		return "", false
	}
	return e.value, true
}

func (s *SecretStore) store(secretID, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[secretID] = cacheEntry{value: value, expiresAt: s.now().Add(s.ttl)}
}
