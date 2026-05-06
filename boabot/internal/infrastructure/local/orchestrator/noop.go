package orchestrator

import (
	"context"
	"errors"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrSkillNotFound is returned by NoopSkillRegistry for all ID-based operations.
var ErrSkillNotFound = errors.New("orchestrator: skill not found")

// ErrDLQItemNotFound is returned by NoopDLQStore for all ID-based operations.
var ErrDLQItemNotFound = errors.New("orchestrator: dlq item not found")

// NoopSkillRegistry implements domain.SkillRegistry with no-op behaviour.
// List returns an empty non-nil slice; all ID operations return ErrSkillNotFound.
type NoopSkillRegistry struct{}

// List returns an empty non-nil slice of skills.
func (NoopSkillRegistry) List(_ context.Context, _ string, _ domain.SkillStatus) ([]domain.Skill, error) {
	return []domain.Skill{}, nil
}

// Get always returns ErrSkillNotFound.
func (NoopSkillRegistry) Get(_ context.Context, _ string) (domain.Skill, error) {
	return domain.Skill{}, ErrSkillNotFound
}

// Approve always returns ErrSkillNotFound.
func (NoopSkillRegistry) Approve(_ context.Context, _ string) error {
	return ErrSkillNotFound
}

// Reject always returns ErrSkillNotFound.
func (NoopSkillRegistry) Reject(_ context.Context, _ string) error {
	return ErrSkillNotFound
}

// Revoke always returns ErrSkillNotFound.
func (NoopSkillRegistry) Revoke(_ context.Context, _ string) error {
	return ErrSkillNotFound
}

// NoopDLQStore implements domain.DLQStore with no-op behaviour.
// List returns an empty non-nil slice; Retry and Discard return ErrDLQItemNotFound.
type NoopDLQStore struct{}

// List returns an empty non-nil slice of DLQ items.
func (NoopDLQStore) List(_ context.Context) ([]domain.DLQItem, error) {
	return []domain.DLQItem{}, nil
}

// Retry always returns ErrDLQItemNotFound.
func (NoopDLQStore) Retry(_ context.Context, _ string) error {
	return ErrDLQItemNotFound
}

// Discard always returns ErrDLQItemNotFound.
func (NoopDLQStore) Discard(_ context.Context, _ string) error {
	return ErrDLQItemNotFound
}
