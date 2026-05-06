// Package local implements the domain AuthProvider interface using bcrypt
// password hashing and HS256 JWT tokens. User records are stored in the users
// table managed by the db.Migrate function.
package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/db"
)

// ErrNotImplemented is returned by OAuthCallback because OAuth2 support is
// deferred to milestone 7.
var ErrNotImplemented = errors.New("auth: oauth2 not implemented (M7)")

// bcryptCost is the work factor used when hashing new passwords.
const bcryptCost = 12

// tokenTTL is the lifetime of issued access tokens.
const tokenTTL = 24 * time.Hour

// userRecord mirrors the users table columns used in queries.
type userRecord struct {
	username           string
	passwordHash       string
	role               string
	disabled           bool
	mustChangePassword bool
}

// LocalAuthProvider implements domain/auth.AuthProvider using the users table.
type LocalAuthProvider struct {
	db        db.DB
	jwtSecret []byte
}

// NewLocalAuthProvider creates a LocalAuthProvider that reads users from db and
// signs tokens with jwtSecret.
func NewLocalAuthProvider(database db.DB, jwtSecret string) *LocalAuthProvider {
	return &LocalAuthProvider{
		db:        database,
		jwtSecret: []byte(jwtSecret),
	}
}

// Login verifies username/password and returns a signed JWT on success.
// Returns ErrInvalidCredentials if the user is not found, the password is
// wrong, or the account is disabled.
// Returns ErrMustChangePassword (wrapped) inside the Token when the user must
// change their password — the token is still issued so the client can call
// SetPassword.
func (p *LocalAuthProvider) Login(username, password string) (domainauth.Token, error) {
	u, err := p.loadUser(context.Background(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainauth.Token{}, domainauth.ErrInvalidCredentials
		}
		return domainauth.Token{}, fmt.Errorf("auth: login: %w", err)
	}

	if u.disabled {
		return domainauth.Token{}, domainauth.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(password)); err != nil {
		return domainauth.Token{}, domainauth.ErrInvalidCredentials
	}

	tok, err := p.issueToken(u.username, u.role, u.mustChangePassword)
	if err != nil {
		return domainauth.Token{}, fmt.Errorf("auth: login: %w", err)
	}
	return tok, nil
}

// ValidateToken parses and verifies a JWT. Returns ErrTokenExpired or
// ErrInvalidCredentials for invalid tokens.
func (p *LocalAuthProvider) ValidateToken(tokenStr string) (domainauth.Claims, error) {
	parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return p.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return domainauth.Claims{}, domainauth.ErrTokenExpired
		}
		return domainauth.Claims{}, domainauth.ErrInvalidCredentials
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return domainauth.Claims{}, domainauth.ErrInvalidCredentials
	}

	sub, _ := claims.GetSubject()
	role, _ := claims["role"].(string)
	exp, _ := claims.GetExpirationTime()

	var expiresAt time.Time
	if exp != nil {
		expiresAt = exp.Time
	}

	return domainauth.Claims{
		Subject:   sub,
		Role:      role,
		ExpiresAt: expiresAt,
	}, nil
}

// OAuthCallback is not implemented until M7.
func (p *LocalAuthProvider) OAuthCallback(_, _ string) (domainauth.Token, error) {
	return domainauth.Token{}, ErrNotImplemented
}

// CreateUser hashes plainPassword with bcrypt and inserts a new user row.
func (p *LocalAuthProvider) CreateUser(ctx context.Context, username, plainPassword, role string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("auth: create user: %w", err)
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role, disabled, must_change_password, created_at)
		VALUES ($1, $2, $3, false, false, now())`,
		username, string(hash), role)
	if err != nil {
		return fmt.Errorf("auth: create user: %w", err)
	}
	return nil
}

// SetPassword hashes plainPassword and updates the user row, clearing
// must_change_password.
func (p *LocalAuthProvider) SetPassword(ctx context.Context, username, plainPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}
	_, err = p.db.ExecContext(ctx,
		`UPDATE users SET password_hash=$1, must_change_password=false WHERE username=$2`,
		string(hash), username)
	if err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}
	return nil
}

// VerifyPassword checks that password matches the stored bcrypt hash for username.
// Returns domainauth.ErrInvalidCredentials if the user is not found, the account
// is disabled, or the password does not match.
func (p *LocalAuthProvider) VerifyPassword(ctx context.Context, username, password string) error {
	u, err := p.loadUser(ctx, username)
	if err != nil {
		return domainauth.ErrInvalidCredentials
	}
	if u.disabled {
		return domainauth.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(password)); err != nil {
		return domainauth.ErrInvalidCredentials
	}
	return nil
}

// DisableUser marks the user account as disabled.
func (p *LocalAuthProvider) DisableUser(ctx context.Context, username string) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE users SET disabled=true WHERE username=$1`, username)
	if err != nil {
		return fmt.Errorf("auth: disable user: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (p *LocalAuthProvider) loadUser(ctx context.Context, username string) (userRecord, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT username, password_hash, role, disabled, must_change_password
		FROM users WHERE username = $1`, username)
	var u userRecord
	if err := row.Scan(&u.username, &u.passwordHash, &u.role, &u.disabled, &u.mustChangePassword); err != nil {
		return userRecord{}, err
	}
	return u, nil
}

func (p *LocalAuthProvider) issueToken(subject, role string, mustChange bool) (domainauth.Token, error) {
	now := time.Now().UTC()
	exp := now.Add(tokenTTL)

	claims := jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"iat":  now.Unix(),
		"exp":  exp.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(p.jwtSecret)
	if err != nil {
		return domainauth.Token{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return domainauth.Token{
		AccessToken:        signed,
		ExpiresAt:          exp,
		MustChangePassword: mustChange,
	}, nil
}
