package secretsmanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	infrasecrets "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/secretsmanager"
)

// mockSMClient is a hand-written mock for SecretsManagerClient.
type mockSMClient struct {
	fn    func(ctx context.Context, in *awssm.GetSecretValueInput, opts ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error)
	calls int
}

func (m *mockSMClient) GetSecretValue(ctx context.Context, in *awssm.GetSecretValueInput, opts ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
	m.calls++
	return m.fn(ctx, in, opts...)
}

func strSecret(val string) *awssm.GetSecretValueOutput {
	return &awssm.GetSecretValueOutput{SecretString: &val}
}

func TestGetSecret_StringSecret(t *testing.T) {
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		return strSecret("mysecret"), nil
	}}
	store := infrasecrets.NewSecretStore(mock)

	val, err := store.GetSecret(context.Background(), "my-secret-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "mysecret" {
		t.Errorf("expected 'mysecret', got %q", val)
	}
}

func TestGetSecret_BinarySecret(t *testing.T) {
	bin := []byte("binary-data")
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		return &awssm.GetSecretValueOutput{SecretBinary: bin}, nil
	}}
	store := infrasecrets.NewSecretStore(mock)

	val, err := store.GetSecret(context.Background(), "bin-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "binary-data" {
		t.Errorf("expected 'binary-data', got %q", val)
	}
}

func TestGetSecret_CacheHit_APICalledOnce(t *testing.T) {
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		return strSecret("cached-value"), nil
	}}
	store := infrasecrets.NewSecretStore(mock)

	_, _ = store.GetSecret(context.Background(), "my-secret")
	_, _ = store.GetSecret(context.Background(), "my-secret")

	if mock.calls != 1 {
		t.Errorf("expected API to be called once, got %d calls", mock.calls)
	}
}

func TestGetSecret_CacheExpires_APICalledAgain(t *testing.T) {
	now := time.Now()
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		return strSecret("fresh"), nil
	}}
	store := infrasecrets.NewSecretStoreWithClock(mock, 1*time.Second, func() time.Time { return now })

	_, _ = store.GetSecret(context.Background(), "my-secret")

	// Advance clock past TTL
	now = now.Add(2 * time.Second)
	_, _ = store.GetSecret(context.Background(), "my-secret")

	if mock.calls != 2 {
		t.Errorf("expected 2 API calls after TTL expiry, got %d", mock.calls)
	}
}

func TestGetSecret_APIError(t *testing.T) {
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		return nil, errors.New("access denied")
	}}
	store := infrasecrets.NewSecretStore(mock)

	_, err := store.GetSecret(context.Background(), "denied-secret")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetSecretJSON_UnmarshalsCorrectly(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"user": "admin", "pass": "hunter2"})
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		s := string(data)
		return &awssm.GetSecretValueOutput{SecretString: &s}, nil
	}}
	store := infrasecrets.NewSecretStore(mock)

	var creds struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	if err := store.GetSecretJSON(context.Background(), "db-creds", &creds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.User != "admin" || creds.Pass != "hunter2" {
		t.Errorf("unexpected creds: %+v", creds)
	}
}

func TestGetSecretJSON_InvalidJSON_ReturnsError(t *testing.T) {
	mock := &mockSMClient{fn: func(_ context.Context, _ *awssm.GetSecretValueInput, _ ...func(*awssm.Options)) (*awssm.GetSecretValueOutput, error) {
		s := "not-json"
		return &awssm.GetSecretValueOutput{SecretString: &s}, nil
	}}
	store := infrasecrets.NewSecretStore(mock)

	var out struct{ X int }
	if err := store.GetSecretJSON(context.Background(), "bad", &out); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}
