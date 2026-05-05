package local_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/auth/local"
)

const testSecret = "super-secret-jwt-key-for-testing"

// userCols matches the SELECT column order in loadUser.
var userCols = []string{"username", "password_hash", "role", "disabled", "must_change_password"}

func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// hashPassword returns a bcrypt hash (MinCost for test speed).
func hashPassword(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

// buildExpiredToken creates a valid HS256 JWT whose exp is in the past.
func buildExpiredToken(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":  "test-user",
		"role": "user",
		"iat":  now.Add(-2 * time.Hour).Unix(),
		"exp":  now.Add(-1 * time.Hour).Unix(), // already expired
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("buildExpiredToken: %v", err)
	}
	return signed
}

// ---------------------------------------------------------------------------
// Login — success
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "correct-password")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("alice", hash, "admin", false, false))

	tok, err := provider.Login("alice", "correct-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if tok.MustChangePassword {
		t.Error("expected MustChangePassword=false")
	}
	if tok.ExpiresAt.Before(time.Now()) {
		t.Error("token already expired")
	}
}

// ---------------------------------------------------------------------------
// Login — wrong password
// ---------------------------------------------------------------------------

func TestLogin_WrongPassword(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "correct-password")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("alice", hash, "admin", false, false))

	_, err := provider.Login("alice", "wrong-password")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Login — user not found
// ---------------------------------------------------------------------------

func TestLogin_UserNotFound(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("nobody").
		WillReturnRows(sqlmock.NewRows(userCols)) // empty → sql.ErrNoRows on Scan

	_, err := provider.Login("nobody", "any")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Login — disabled account
// ---------------------------------------------------------------------------

func TestLogin_DisabledUser(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "secret")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("disabled-user").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("disabled-user", hash, "user", true, false))

	_, err := provider.Login("disabled-user", "secret")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for disabled user, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Login — must change password
// ---------------------------------------------------------------------------

func TestLogin_MustChangePassword(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "temp-pass")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("newbie").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("newbie", hash, "user", false, true))

	tok, err := provider.Login("newbie", "temp-pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !tok.MustChangePassword {
		t.Error("expected MustChangePassword=true")
	}
	if tok.AccessToken == "" {
		t.Error("expected access token even when must change password")
	}
}

// ---------------------------------------------------------------------------
// ValidateToken — success
// ---------------------------------------------------------------------------

func TestValidateToken_Success(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "pass")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("bob").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("bob", hash, "user", false, false))

	tok, err := provider.Login("bob", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	claims, err := provider.ValidateToken(tok.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Subject != "bob" {
		t.Errorf("Subject: got %q want bob", claims.Subject)
	}
	if claims.Role != "user" {
		t.Errorf("Role: got %q want user", claims.Role)
	}
	if claims.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be set")
	}
}

// ---------------------------------------------------------------------------
// ValidateToken — expired
// ---------------------------------------------------------------------------

func TestValidateToken_Expired(t *testing.T) {
	db, _ := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	expiredToken := buildExpiredToken(t, testSecret)

	_, err := provider.ValidateToken(expiredToken)
	if !errors.Is(err, domainauth.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateToken — invalid / garbage
// ---------------------------------------------------------------------------

func TestValidateToken_Garbage(t *testing.T) {
	db, _ := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	_, err := provider.ValidateToken("not.a.valid.token")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	db, mock := newMock(t)
	providerA := local.NewLocalAuthProvider(db, "secret-a")
	providerB := local.NewLocalAuthProvider(db, "secret-b")

	hash := hashPassword(t, "pw")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("carol").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("carol", hash, "user", false, false))

	tok, err := providerA.Login("carol", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	_, err = providerB.ValidateToken(tok.AccessToken)
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong secret, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// OAuthCallback — not implemented
// ---------------------------------------------------------------------------

func TestOAuthCallback_NotImplemented(t *testing.T) {
	db, _ := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	_, err := provider.OAuthCallback("code", "state")
	if !errors.Is(err, local.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CreateUser
// ---------------------------------------------------------------------------

func TestCreateUser(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`INSERT INTO users`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := provider.CreateUser(context.Background(), "dave", "pass123", "user"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateUser_DBError(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`INSERT INTO users`).
		WillReturnError(errors.New("duplicate key"))

	if err := provider.CreateUser(context.Background(), "dave", "pass123", "user"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// SetPassword
// ---------------------------------------------------------------------------

func TestSetPassword(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`UPDATE users SET password_hash`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := provider.SetPassword(context.Background(), "dave", "newpass"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
}

func TestSetPassword_DBError(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`UPDATE users SET password_hash`).
		WillReturnError(errors.New("row locked"))

	if err := provider.SetPassword(context.Background(), "dave", "newpass"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// DisableUser
// ---------------------------------------------------------------------------

func TestDisableUser(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`UPDATE users SET disabled=true`).
		WithArgs("eve").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := provider.DisableUser(context.Background(), "eve"); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
}

func TestDisableUser_DBError(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectExec(`UPDATE users SET disabled=true`).
		WillReturnError(errors.New("connection refused"))

	if err := provider.DisableUser(context.Background(), "eve"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Login — DB error (not ErrNoRows)
// ---------------------------------------------------------------------------

func TestLogin_DBError(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("alice").
		WillReturnError(errors.New("connection refused"))

	_, err := provider.Login("alice", "any-password")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should NOT be ErrInvalidCredentials — it's a DB error
	if errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatal("expected a non-credential error for DB failures")
	}
}

// ---------------------------------------------------------------------------
// VerifyPassword
// ---------------------------------------------------------------------------

func TestVerifyPassword_Success(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "correct")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("alice", hash, "user", false, false))

	if err := provider.VerifyPassword(context.Background(), "alice", "correct"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "correct")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("alice", hash, "user", false, false))

	err := provider.VerifyPassword(context.Background(), "alice", "wrong")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestVerifyPassword_UserNotFound(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("nobody").
		WillReturnRows(sqlmock.NewRows(userCols))

	err := provider.VerifyPassword(context.Background(), "nobody", "any")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestVerifyPassword_DisabledUser(t *testing.T) {
	db, mock := newMock(t)
	provider := local.NewLocalAuthProvider(db, testSecret)

	hash := hashPassword(t, "pass")
	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("disabled").
		WillReturnRows(sqlmock.NewRows(userCols).AddRow("disabled", hash, "user", true, false))

	err := provider.VerifyPassword(context.Background(), "disabled", "pass")
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for disabled user, got %v", err)
	}
}

// Ensure the unused import for context is included.
var _ = context.Background
