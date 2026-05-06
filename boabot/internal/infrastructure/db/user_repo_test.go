package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	infradb "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/db"
)

var userCols = []string{"username", "role", "disabled", "must_change_password", "created_at"}

func newUser() domain.User {
	return domain.User{
		Username:           "alice",
		Role:               domain.UserRoleAdmin,
		Enabled:            true,
		MustChangePassword: false,
		CreatedAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestUserRepo_Create(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)
	u := newUser()

	mock.ExpectExec(`INSERT INTO users`).
		WithArgs(u.Username, "!", string(u.Role), !u.Enabled, u.MustChangePassword, u.CreatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := repo.Create(context.Background(), u)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Username != u.Username {
		t.Errorf("username: got %q want %q", got.Username, u.Username)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUserRepo_Create_WithPasswordHash(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)
	u := newUser()
	u.PasswordHash = "$2a$12$existinghash"

	mock.ExpectExec(`INSERT INTO users`).
		WithArgs(u.Username, u.PasswordHash, string(u.Role), !u.Enabled, u.MustChangePassword, u.CreatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if _, err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestUserRepo_Create_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectExec(`INSERT INTO users`).
		WillReturnError(errors.New("duplicate key"))

	if _, err := repo.Create(context.Background(), newUser()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestUserRepo_Get(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)
	u := newUser()

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs(u.Username).
		WillReturnRows(sqlmock.NewRows(userCols).AddRow(
			u.Username, string(u.Role), !u.Enabled, u.MustChangePassword, u.CreatedAt))

	got, err := repo.Get(context.Background(), u.Username)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Username != u.Username {
		t.Errorf("username: got %q want %q", got.Username, u.Username)
	}
	if got.Role != u.Role {
		t.Errorf("role: got %q want %q", got.Role, u.Role)
	}
	if !got.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestUserRepo_Get_NotFound(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WithArgs("nobody").
		WillReturnRows(sqlmock.NewRows(userCols))

	_, err := repo.Get(context.Background(), "nobody")
	if !errors.Is(err, infradb.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserRepo_Get_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM users WHERE username`).
		WillReturnError(errors.New("connection refused"))

	if _, err := repo.Get(context.Background(), "alice"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUserRepo_Update(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)
	u := newUser()
	u.Enabled = false

	mock.ExpectExec(`UPDATE users SET`).
		WithArgs(string(u.Role), !u.Enabled, u.MustChangePassword, u.Username).
		WillReturnResult(sqlmock.NewResult(0, 1))

	got, err := repo.Update(context.Background(), u)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Enabled {
		t.Error("expected Enabled=false after update")
	}
}

func TestUserRepo_Update_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectExec(`UPDATE users SET`).
		WillReturnError(errors.New("row locked"))

	if _, err := repo.Update(context.Background(), newUser()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestUserRepo_Delete(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectExec(`DELETE FROM users WHERE username`).
		WithArgs("alice").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Delete(context.Background(), "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestUserRepo_Delete_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectExec(`DELETE FROM users WHERE username`).
		WillReturnError(errors.New("connection refused"))

	if err := repo.Delete(context.Background(), "alice"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestUserRepo_List(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)
	now := time.Now().UTC()

	mock.ExpectQuery(`SELECT .+ FROM users ORDER BY username`).
		WillReturnRows(sqlmock.NewRows(userCols).
			AddRow("alice", "admin", false, false, now).
			AddRow("bob", "user", true, false, now))

	users, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("first user: got %q want alice", users[0].Username)
	}
	if users[1].Enabled {
		t.Error("bob should be disabled (disabled=true in DB)")
	}
}

func TestUserRepo_List_Empty(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM users ORDER BY username`).
		WillReturnRows(sqlmock.NewRows(userCols))

	users, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users, got %d", len(users))
	}
}

func TestUserRepo_List_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewUserRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM users ORDER BY username`).
		WillReturnError(errors.New("db unavailable"))

	if _, err := repo.List(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}
