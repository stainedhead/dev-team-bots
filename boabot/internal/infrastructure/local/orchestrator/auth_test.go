package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

func TestNewInMemoryAuthProvider_CreatesAdminUser(t *testing.T) {
	t.Parallel()
	auth, err := orchestrator.NewInMemoryAuthProvider("adminpass", "")
	if err != nil {
		t.Fatalf("NewInMemoryAuthProvider: %v", err)
	}

	// Should be able to log in as admin
	tok, err := auth.Login("admin", "adminpass")
	if err != nil {
		t.Fatalf("Login as admin: %v", err)
	}
	if tok.AccessToken == "" {
		t.Error("expected non-empty AccessToken")
	}
	if tok.ExpiresAt.Before(time.Now()) {
		t.Error("expected ExpiresAt in the future")
	}
}

func TestNewInMemoryAuthProvider_GeneratesJWTSecretWhenEmpty(t *testing.T) {
	t.Parallel()
	auth1, err1 := orchestrator.NewInMemoryAuthProvider("pass", "")
	auth2, err2 := orchestrator.NewInMemoryAuthProvider("pass", "")
	if err1 != nil || err2 != nil {
		t.Fatalf("NewInMemoryAuthProvider errors: %v, %v", err1, err2)
	}

	tok1, _ := auth1.Login("admin", "pass")
	// Token from auth2 should NOT validate against auth1 (different secrets)
	_, err := auth1.ValidateToken(tok1.AccessToken)
	if err != nil {
		t.Errorf("token from auth1 should validate in auth1: %v", err)
	}

	tok2, _ := auth2.Login("admin", "pass")
	// Different secrets — cross-validation should fail
	_, errCross := auth1.ValidateToken(tok2.AccessToken)
	if errCross == nil {
		t.Error("expected cross-provider token validation to fail (different secrets)")
	}
}

func TestInMemoryAuthProvider_Login_Success(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("mypassword", "supersecret")

	tok, err := auth.Login("admin", "mypassword")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken == "" {
		t.Error("expected non-empty AccessToken")
	}
}

func TestInMemoryAuthProvider_Login_WrongPassword(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("mypassword", "supersecret")

	_, err := auth.Login("admin", "wrongpassword")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
	if !isErrInvalidCredentials(err) {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestInMemoryAuthProvider_Login_UserNotFound(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("mypassword", "supersecret")

	_, err := auth.Login("nonexistent", "any")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	if !isErrInvalidCredentials(err) {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestInMemoryAuthProvider_Login_DisabledUser(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("mypassword", "supersecret")
	ctx := context.Background()

	// Create a user and disable them
	_, err := auth.Create(ctx, orchestratorTestUser("bob", "user"))
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	_ = auth.SetPassword(ctx, "bob", "bobpass")

	bob, _ := auth.Get(ctx, "bob")
	bob.Enabled = false
	_, _ = auth.Update(ctx, bob)

	_, loginErr := auth.Login("bob", "bobpass")
	if loginErr == nil {
		t.Fatal("expected error for disabled user, got nil")
	}
	if !isErrInvalidCredentials(loginErr) {
		t.Errorf("expected ErrInvalidCredentials for disabled user, got: %v", loginErr)
	}
}

func TestInMemoryAuthProvider_ValidateToken_Success(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("pass", "secret123")

	tok, _ := auth.Login("admin", "pass")
	claims, err := auth.ValidateToken(tok.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Subject != "admin" {
		t.Errorf("expected Subject=admin, got %q", claims.Subject)
	}
	if claims.Role != "admin" {
		t.Errorf("expected Role=admin, got %q", claims.Role)
	}
}

func TestInMemoryAuthProvider_ValidateToken_Invalid(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("pass", "secret123")

	_, err := auth.ValidateToken("not-a-valid-jwt")
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
	if !isErrInvalidCredentials(err) {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestInMemoryAuthProvider_SetPassword(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("initial", "secret")
	ctx := context.Background()

	if err := auth.SetPassword(ctx, "admin", "newpassword"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	// Should be able to log in with new password
	_, err := auth.Login("admin", "newpassword")
	if err != nil {
		t.Errorf("Login after SetPassword: %v", err)
	}

	// Old password should no longer work
	_, err = auth.Login("admin", "initial")
	if err == nil {
		t.Error("expected error with old password after SetPassword")
	}
}

func TestInMemoryAuthProvider_VerifyPassword_Success(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("thepassword", "secret")
	ctx := context.Background()

	if err := auth.VerifyPassword(ctx, "admin", "thepassword"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
}

func TestInMemoryAuthProvider_VerifyPassword_Wrong(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("thepassword", "secret")
	ctx := context.Background()

	err := auth.VerifyPassword(ctx, "admin", "wrongpassword")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestInMemoryAuthProvider_VerifyPassword_NotFound(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("pass", "secret")
	ctx := context.Background()

	err := auth.VerifyPassword(ctx, "nobody", "any")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

// ── UserStore interface tests ─────────────────────────────────────────────────

func TestInMemoryAuthProvider_UserStore_Create(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	user := orchestratorTestUser("alice", "user")
	created, err := auth.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Username != "alice" {
		t.Errorf("expected Username=alice, got %q", created.Username)
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set after Create")
	}
}

func TestInMemoryAuthProvider_UserStore_Create_SetsCreatedAt(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	user := orchestratorTestUser("bob", "user")
	before := time.Now()
	created, err := auth.Create(ctx, user)
	after := time.Now()

	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.Before(before) || created.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not in expected range [%v, %v]", created.CreatedAt, before, after)
	}
}

func TestInMemoryAuthProvider_UserStore_Get_NoPasswordHash(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	got, err := auth.Get(ctx, "admin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PasswordHash != "" {
		t.Error("Get must not return PasswordHash")
	}
}

func TestInMemoryAuthProvider_UserStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	_, err := auth.Get(ctx, "nobody")
	if err == nil {
		t.Error("expected error for nonexistent user, got nil")
	}
}

func TestInMemoryAuthProvider_UserStore_Update(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	user, _ := auth.Get(ctx, "admin")
	user.DisplayName = "Admin User"
	updated, err := auth.Update(ctx, user)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DisplayName != "Admin User" {
		t.Errorf("expected DisplayName=Admin User, got %q", updated.DisplayName)
	}
}

func TestInMemoryAuthProvider_UserStore_Delete(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	_, _ = auth.Create(ctx, orchestratorTestUser("todelete", "user"))
	if err := auth.Delete(ctx, "todelete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := auth.Get(ctx, "todelete")
	if err == nil {
		t.Error("expected error getting deleted user, got nil")
	}
}

func TestInMemoryAuthProvider_UserStore_List(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	// admin already exists; add another
	_, _ = auth.Create(ctx, orchestratorTestUser("beta", "user"))

	users, err := auth.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) < 2 {
		t.Errorf("expected at least 2 users, got %d", len(users))
	}
}

func TestInMemoryAuthProvider_UserStore_List_NoPasswordHashes(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	users, err := auth.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, u := range users {
		if u.PasswordHash != "" {
			t.Errorf("List must not return PasswordHash for user %q", u.Username)
		}
	}
}

func TestInMemoryAuthProvider_UserStore_List_SortedByUsername(t *testing.T) {
	t.Parallel()
	auth, _ := orchestrator.NewInMemoryAuthProvider("adminpass", "secret")
	ctx := context.Background()

	_, _ = auth.Create(ctx, orchestratorTestUser("zed", "user"))
	_, _ = auth.Create(ctx, orchestratorTestUser("ann", "user"))

	users, err := auth.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for i := 1; i < len(users); i++ {
		if users[i-1].Username > users[i].Username {
			t.Errorf("list not sorted: %q > %q", users[i-1].Username, users[i].Username)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func orchestratorTestUser(username, role string) domain.User {
	return domain.User{
		Username: username,
		Role:     domain.UserRole(role),
		Enabled:  true,
	}
}

func isErrInvalidCredentials(err error) bool {
	return err != nil && err.Error() == domainauth.ErrInvalidCredentials.Error()
}
