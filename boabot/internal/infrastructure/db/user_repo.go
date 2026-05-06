package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrUserNotFound is returned by UserRepo when a username lookup finds no row.
var ErrUserNotFound = errors.New("db: user not found")

// UserRepo implements domain.UserStore against the PostgreSQL users table.
type UserRepo struct {
	db DB
}

// NewUserRepo creates a UserRepo backed by db.
func NewUserRepo(db DB) *UserRepo {
	return &UserRepo{db: db}
}

// Create inserts a new user row. PasswordHash defaults to the locked sentinel
// '!' if empty — callers must follow up with AuthProvider.SetPassword.
func (r *UserRepo) Create(ctx context.Context, u domain.User) (domain.User, error) {
	hash := u.PasswordHash
	if hash == "" {
		hash = "!"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role, disabled, must_change_password, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		u.Username, hash, string(u.Role), !u.Enabled, u.MustChangePassword, u.CreatedAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("db: user create: %w", err)
	}
	return u, nil
}

// Get returns the user with the given username.
func (r *UserRepo) Get(ctx context.Context, username string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT username, role, disabled, must_change_password, created_at
		FROM users WHERE username = $1`, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("db: user get: %w", err)
	}
	return u, nil
}

// Update persists mutable user fields (role, enabled, must_change_password).
// PasswordHash and CreatedAt are not updated here — use AuthProvider.SetPassword
// for password changes.
func (r *UserRepo) Update(ctx context.Context, u domain.User) (domain.User, error) {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET role=$1, disabled=$2, must_change_password=$3
		WHERE username=$4`,
		string(u.Role), !u.Enabled, u.MustChangePassword, u.Username)
	if err != nil {
		return domain.User{}, fmt.Errorf("db: user update: %w", err)
	}
	return u, nil
}

// Delete removes the user row permanently.
func (r *UserRepo) Delete(ctx context.Context, username string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM users WHERE username=$1`, username)
	if err != nil {
		return fmt.Errorf("db: user delete: %w", err)
	}
	return nil
}

// List returns all users ordered by username.
func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT username, role, disabled, must_change_password, created_at
		FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("db: user list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []domain.User
	for rows.Next() {
		var (
			username           string
			role               string
			disabled           bool
			mustChangePassword bool
			createdAt          time.Time
		)
		if err := rows.Scan(&username, &role, &disabled, &mustChangePassword, &createdAt); err != nil {
			return nil, fmt.Errorf("db: user list scan: %w", err)
		}
		users = append(users, domain.User{
			Username:           username,
			Role:               domain.UserRole(role),
			Enabled:            !disabled,
			MustChangePassword: mustChangePassword,
			CreatedAt:          createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: user list rows: %w", err)
	}
	return users, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (domain.User, error) {
	var (
		username           string
		role               string
		disabled           bool
		mustChangePassword bool
		createdAt          time.Time
	)
	if err := row.Scan(&username, &role, &disabled, &mustChangePassword, &createdAt); err != nil {
		return domain.User{}, err
	}
	return domain.User{
		Username:           username,
		Role:               domain.UserRole(role),
		Enabled:            !disabled,
		MustChangePassword: mustChangePassword,
		CreatedAt:          createdAt,
	}, nil
}
