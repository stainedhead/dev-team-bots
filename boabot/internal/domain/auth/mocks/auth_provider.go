// Package mocks provides hand-written test doubles for the auth domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"

// LoginCall records a single call to Login.
type LoginCall struct {
	Username string
	Password string
}

// ValidateTokenCall records a single call to ValidateToken.
type ValidateTokenCall struct {
	Token string
}

// OAuthCallbackCall records a single call to OAuthCallback.
type OAuthCallbackCall struct {
	Code  string
	State string
}

// AuthProvider is a hand-written mock of auth.AuthProvider.
type AuthProvider struct {
	LoginFn         func(username, password string) (auth.Token, error)
	ValidateTokenFn func(token string) (auth.Claims, error)
	OAuthCallbackFn func(code, state string) (auth.Token, error)

	LoginCalls         []LoginCall
	ValidateTokenCalls []ValidateTokenCall
	OAuthCallbackCalls []OAuthCallbackCall
}

func (m *AuthProvider) Login(username, password string) (auth.Token, error) {
	m.LoginCalls = append(m.LoginCalls, LoginCall{Username: username, Password: password})
	if m.LoginFn != nil {
		return m.LoginFn(username, password)
	}
	return auth.Token{}, nil
}

func (m *AuthProvider) ValidateToken(token string) (auth.Claims, error) {
	m.ValidateTokenCalls = append(m.ValidateTokenCalls, ValidateTokenCall{Token: token})
	if m.ValidateTokenFn != nil {
		return m.ValidateTokenFn(token)
	}
	return auth.Claims{}, nil
}

func (m *AuthProvider) OAuthCallback(code, state string) (auth.Token, error) {
	m.OAuthCallbackCalls = append(m.OAuthCallbackCalls, OAuthCallbackCall{Code: code, State: state})
	if m.OAuthCallbackFn != nil {
		return m.OAuthCallbackFn(code, state)
	}
	return auth.Token{}, nil
}
