// Package auth defines the domain types and interfaces for operator
// authentication, token management, and OAuth callback handling.
package auth

import (
	"errors"
	"time"
)

// ErrInvalidCredentials is returned when a login attempt fails due to an
// unrecognised username or incorrect password.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrTokenExpired is returned when a token is syntactically valid but has
// passed its expiry time.
var ErrTokenExpired = errors.New("auth: token expired")

// ErrMustChangePassword is returned when a user has logged in successfully but
// is required to choose a new password before accessing any other resource.
var ErrMustChangePassword = errors.New("auth: password change required")

// Token is the result of a successful authentication.
type Token struct {
	// AccessToken is the opaque bearer token string.
	AccessToken string

	// ExpiresAt is the UTC time after which the token is no longer valid.
	ExpiresAt time.Time

	// MustChangePassword is true when the user must set a new password before
	// proceeding.
	MustChangePassword bool
}

// Claims is the decoded representation of a validated token.
type Claims struct {
	// Subject is the username (or user ID) the token was issued to.
	Subject string

	// Role is the named role granted to this principal (e.g. "admin", "user").
	Role string

	// ExpiresAt is the UTC expiry time encoded in the token.
	ExpiresAt time.Time
}

// AuthProvider handles credential verification, token validation, and OAuth
// code exchange.
type AuthProvider interface {
	// Login verifies username/password and returns a Token on success.
	// Returns ErrInvalidCredentials if the credentials are not recognised.
	// Returns ErrMustChangePassword (wrapped) when the account requires a
	// password change.
	Login(username, password string) (Token, error)

	// ValidateToken parses and validates a raw token string and returns the
	// embedded Claims.
	// Returns ErrTokenExpired if the token has expired.
	// Returns ErrInvalidCredentials if the token is malformed or the signature
	// is invalid.
	ValidateToken(token string) (Claims, error)

	// OAuthCallback exchanges an OAuth authorisation code and state for a Token.
	// Returns ErrInvalidCredentials if the code or state is invalid.
	OAuthCallback(code, state string) (Token, error)
}
