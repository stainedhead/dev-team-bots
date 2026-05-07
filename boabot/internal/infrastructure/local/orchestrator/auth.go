package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
)

const (
	bcryptCostAuth = 12
	tokenTTLAuth   = 24 * time.Hour
)

// inMemoryUser stores user data including the bcrypt-hashed password.
type inMemoryUser struct {
	domain.User
	passwordHash string
}

// persistedAuthUser is the on-disk representation of a user including the bcrypt hash.
type persistedAuthUser struct {
	domain.User
	PasswordHash string `json:"password_hash"`
}

// InMemoryAuthProvider implements both httpserver.AuthProvider and domain.UserStore
// using an in-memory map. It is safe for concurrent use.
type InMemoryAuthProvider struct {
	mu          sync.RWMutex
	users       map[string]*inMemoryUser
	jwtSecret   []byte
	persistPath string
}

// NewInMemoryAuthProvider creates an InMemoryAuthProvider with an initial admin
// user hashed with bcrypt. If jwtSecret is empty a random 32-byte hex secret is
// generated. If persistPath is non-empty, existing users are loaded from that
// file and every mutation is written back atomically.
func NewInMemoryAuthProvider(adminPassword, jwtSecret, persistPath string) (*InMemoryAuthProvider, error) {
	secret := []byte(jwtSecret)
	if len(secret) == 0 {
		// Derive a secret file path next to the persist file so the JWT secret
		// survives server restarts (tokens issued before restart remain valid).
		secretFile := ""
		if persistPath != "" {
			secretFile = filepath.Join(filepath.Dir(persistPath), "jwt.secret")
		}
		if secretFile != "" {
			if existing, readErr := os.ReadFile(secretFile); readErr == nil && len(existing) > 0 {
				// Trim whitespace/newline in case the file was written with a trailing newline.
				secret = []byte(strings.TrimSpace(string(existing)))
			}
		}
		if len(secret) == 0 {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return nil, fmt.Errorf("orchestrator auth: generate jwt secret: %w", err)
			}
			secret = []byte(hex.EncodeToString(b))
			if secretFile != "" {
				_ = os.MkdirAll(filepath.Dir(secretFile), 0o755)
				_ = os.WriteFile(secretFile, secret, 0o600)
			}
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcryptCostAuth)
	if err != nil {
		return nil, fmt.Errorf("orchestrator auth: hash admin password: %w", err)
	}

	now := time.Now().UTC()
	admin := &inMemoryUser{
		User: domain.User{
			Username:  "admin",
			Role:      domain.UserRoleAdmin,
			Enabled:   true,
			CreatedAt: now,
		},
		passwordHash: string(hash),
	}

	p := &InMemoryAuthProvider{
		users:       make(map[string]*inMemoryUser),
		jwtSecret:   secret,
		persistPath: persistPath,
	}

	// Load persisted users; if none exist, seed with the admin account.
	if persistPath != "" {
		p.loadFromDisk()
	}
	if _, exists := p.users["admin"]; !exists {
		p.users["admin"] = admin
		p.persist()
	}
	return p, nil
}

// loadFromDisk reads persisted user state from disk. Caller must not hold lock.
func (p *InMemoryAuthProvider) loadFromDisk() {
	data, err := os.ReadFile(p.persistPath)
	if err != nil {
		return
	}
	var records []persistedAuthUser
	if err := json.Unmarshal(data, &records); err != nil {
		return
	}
	for _, r := range records {
		rec := &inMemoryUser{
			User:         r.User,
			passwordHash: r.PasswordHash,
		}
		rec.PasswordHash = ""
		p.users[r.Username] = rec
	}
}

// persist writes all users (including password hashes) to disk atomically.
// Caller must hold the write lock.
func (p *InMemoryAuthProvider) persist() {
	if p.persistPath == "" {
		return
	}
	records := make([]persistedAuthUser, 0, len(p.users))
	for _, u := range p.users {
		records = append(records, persistedAuthUser{
			User:         u.User,
			PasswordHash: u.passwordHash,
		})
	}
	data, err := json.Marshal(records)
	if err != nil {
		return
	}
	tmp := p.persistPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(p.persistPath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // 0o600: owner-only, contains hashes
		return
	}
	_ = os.Rename(tmp, p.persistPath)
}

// ── httpserver.AuthProvider ────────────────────────────────────────────────────

// Login validates credentials and returns a signed JWT on success.
// Returns domainauth.ErrInvalidCredentials if the user is not found, disabled,
// or the password does not match.
func (p *InMemoryAuthProvider) Login(username, password string) (domainauth.Token, error) {
	p.mu.RLock()
	u, ok := p.users[username]
	p.mu.RUnlock()

	if !ok || !u.Enabled {
		return domainauth.Token{}, domainauth.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(password)); err != nil {
		return domainauth.Token{}, domainauth.ErrInvalidCredentials
	}
	return p.issueToken(username, string(u.Role), u.MustChangePassword)
}

// ValidateToken parses and verifies a JWT. Returns domainauth.ErrTokenExpired or
// domainauth.ErrInvalidCredentials for invalid/expired tokens.
func (p *InMemoryAuthProvider) ValidateToken(tokenStr string) (domainauth.Claims, error) {
	parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("orchestrator auth: unexpected signing method: %v", t.Header["alg"])
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

// SetPassword hashes newPassword and stores it for username.
func (p *InMemoryAuthProvider) SetPassword(_ context.Context, username, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCostAuth)
	if err != nil {
		return fmt.Errorf("orchestrator auth: hash password: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	u, ok := p.users[username]
	if !ok {
		return fmt.Errorf("orchestrator auth: user %q not found", username)
	}
	u.passwordHash = string(hash)
	u.MustChangePassword = false
	p.persist()
	return nil
}

// VerifyPassword checks that password matches the stored credential for username.
func (p *InMemoryAuthProvider) VerifyPassword(_ context.Context, username, password string) error {
	p.mu.RLock()
	u, ok := p.users[username]
	p.mu.RUnlock()

	if !ok {
		return domainauth.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(password)); err != nil {
		return domainauth.ErrInvalidCredentials
	}
	return nil
}

// ── domain.UserStore ───────────────────────────────────────────────────────────

// Create adds a new user. If PasswordHash is set on the input it is stored
// directly; otherwise the hash is left empty. Sets CreatedAt if zero.
func (p *InMemoryAuthProvider) Create(_ context.Context, user domain.User) (domain.User, error) {
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Strip PasswordHash from the stored User value; hold it separately.
	storedUser := user
	storedUser.PasswordHash = ""
	rec := &inMemoryUser{
		User:         storedUser,
		passwordHash: user.PasswordHash,
	}
	p.users[user.Username] = rec
	p.persist()

	result := storedUser
	result.PasswordHash = ""
	return result, nil
}

// Update replaces a user record. PasswordHash on the input is ignored to avoid
// accidentally overwriting a bcrypt hash with a plain text value.
func (p *InMemoryAuthProvider) Update(_ context.Context, user domain.User) (domain.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing, ok := p.users[user.Username]
	if !ok {
		return domain.User{}, fmt.Errorf("orchestrator auth: user %q not found", user.Username)
	}
	// Preserve the existing password hash.
	user.PasswordHash = ""
	existing.User = user
	p.users[user.Username] = existing
	p.persist()

	result := existing.User
	result.PasswordHash = ""
	return result, nil
}

// Delete removes a user record.
func (p *InMemoryAuthProvider) Delete(_ context.Context, username string) error {
	p.mu.Lock()
	delete(p.users, username)
	p.persist()
	p.mu.Unlock()
	return nil
}

// Get returns a user record without the PasswordHash field set.
func (p *InMemoryAuthProvider) Get(_ context.Context, username string) (domain.User, error) {
	p.mu.RLock()
	u, ok := p.users[username]
	p.mu.RUnlock()

	if !ok {
		return domain.User{}, fmt.Errorf("orchestrator auth: user %q not found", username)
	}
	result := u.User
	result.PasswordHash = ""
	return result, nil
}

// List returns all users sorted by username, without PasswordHash fields.
func (p *InMemoryAuthProvider) List(_ context.Context) ([]domain.User, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	users := make([]domain.User, 0, len(p.users))
	for _, u := range p.users {
		out := u.User
		out.PasswordHash = ""
		users = append(users, out)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})
	return users, nil
}

// ── private helpers ────────────────────────────────────────────────────────────

func (p *InMemoryAuthProvider) issueToken(subject, role string, mustChange bool) (domainauth.Token, error) {
	now := time.Now().UTC()
	exp := now.Add(tokenTTLAuth)

	claims := jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"iat":  now.Unix(),
		"exp":  exp.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(p.jwtSecret)
	if err != nil {
		return domainauth.Token{}, fmt.Errorf("orchestrator auth: sign token: %w", err)
	}
	return domainauth.Token{
		AccessToken:        signed,
		ExpiresAt:          exp,
		MustChangePassword: mustChange,
	}, nil
}
