package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth/mocks"
)

func TestSentinelErrors(t *testing.T) {
	if auth.ErrInvalidCredentials == nil {
		t.Fatal("ErrInvalidCredentials must not be nil")
	}
	if auth.ErrTokenExpired == nil {
		t.Fatal("ErrTokenExpired must not be nil")
	}
	if auth.ErrMustChangePassword == nil {
		t.Fatal("ErrMustChangePassword must not be nil")
	}
}

func TestToken_Fields(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	tok := auth.Token{
		AccessToken:        "abc123",
		ExpiresAt:          expiry,
		MustChangePassword: true,
	}
	if tok.AccessToken != "abc123" {
		t.Fatalf("unexpected AccessToken %s", tok.AccessToken)
	}
	if !tok.MustChangePassword {
		t.Fatal("expected MustChangePassword=true")
	}
}

func TestClaims_Fields(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	c := auth.Claims{Subject: "alice", Role: "admin", ExpiresAt: expiry}
	if c.Subject != "alice" {
		t.Fatalf("unexpected Subject %s", c.Subject)
	}
	if c.Role != "admin" {
		t.Fatalf("unexpected Role %s", c.Role)
	}
}

func TestAuthProviderMock_Login_OK(t *testing.T) {
	tok := auth.Token{AccessToken: "token-xyz"}
	m := &mocks.AuthProvider{
		LoginFn: func(u, p string) (auth.Token, error) {
			if u == "alice" && p == "secret" {
				return tok, nil
			}
			return auth.Token{}, auth.ErrInvalidCredentials
		},
	}

	got, err := m.Login("alice", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AccessToken != "token-xyz" {
		t.Fatalf("unexpected token %s", got.AccessToken)
	}
	if len(m.LoginCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.LoginCalls))
	}
}

func TestAuthProviderMock_Login_InvalidCredentials(t *testing.T) {
	m := &mocks.AuthProvider{
		LoginFn: func(_, _ string) (auth.Token, error) {
			return auth.Token{}, auth.ErrInvalidCredentials
		},
	}
	_, err := m.Login("bad", "wrong")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials got %v", err)
	}
}

func TestAuthProviderMock_ValidateToken_OK(t *testing.T) {
	claims := auth.Claims{Subject: "alice", Role: "user"}
	m := &mocks.AuthProvider{
		ValidateTokenFn: func(_ string) (auth.Claims, error) { return claims, nil },
	}
	got, err := m.ValidateToken("some-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "alice" {
		t.Fatalf("unexpected subject %s", got.Subject)
	}
}

func TestAuthProviderMock_ValidateToken_Expired(t *testing.T) {
	m := &mocks.AuthProvider{
		ValidateTokenFn: func(_ string) (auth.Claims, error) {
			return auth.Claims{}, auth.ErrTokenExpired
		},
	}
	_, err := m.ValidateToken("expired-token")
	if !errors.Is(err, auth.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired got %v", err)
	}
}

func TestAuthProviderMock_OAuthCallback_OK(t *testing.T) {
	tok := auth.Token{AccessToken: "oauth-token"}
	m := &mocks.AuthProvider{
		OAuthCallbackFn: func(code, _ string) (auth.Token, error) {
			if code == "valid-code" {
				return tok, nil
			}
			return auth.Token{}, auth.ErrInvalidCredentials
		},
	}
	got, err := m.OAuthCallback("valid-code", "state-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AccessToken != "oauth-token" {
		t.Fatalf("unexpected token %s", got.AccessToken)
	}
	if len(m.OAuthCallbackCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.OAuthCallbackCalls))
	}
}

func TestAuthProviderMock_OAuthCallback_Invalid(t *testing.T) {
	m := &mocks.AuthProvider{
		OAuthCallbackFn: func(_, _ string) (auth.Token, error) {
			return auth.Token{}, auth.ErrInvalidCredentials
		},
	}
	_, err := m.OAuthCallback("bad-code", "bad-state")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials got %v", err)
	}
}

func TestAuthProviderMock_DefaultBehaviour(t *testing.T) {
	m := &mocks.AuthProvider{}
	// Default implementations return zero values with nil error.
	tok, err := m.Login("u", "p")
	if err != nil || tok.AccessToken != "" {
		t.Fatalf("unexpected default Login result: %v %v", tok, err)
	}
	claims, err := m.ValidateToken("t")
	if err != nil || claims.Subject != "" {
		t.Fatalf("unexpected default ValidateToken result: %v %v", claims, err)
	}
	tok2, err := m.OAuthCallback("c", "s")
	if err != nil || tok2.AccessToken != "" {
		t.Fatalf("unexpected default OAuthCallback result: %v %v", tok2, err)
	}
}
