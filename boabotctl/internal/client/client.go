package client

import (
	"context"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// OrchestratorClient is the interface all command handlers depend on.
// The HTTP implementation is injected at startup; tests inject a mock.
type OrchestratorClient interface {
	// Auth
	Login(ctx context.Context, username, password string) (domain.LoginResponse, error)

	// Board
	BoardList(ctx context.Context) ([]domain.WorkItem, error)
	BoardGet(ctx context.Context, id string) (domain.WorkItem, error)
	BoardCreate(ctx context.Context, req domain.CreateWorkItemRequest) (domain.WorkItem, error)
	BoardUpdate(ctx context.Context, id string, req domain.UpdateWorkItemRequest) (domain.WorkItem, error)
	BoardAssign(ctx context.Context, id, botName string) (domain.WorkItem, error)
	BoardClose(ctx context.Context, id string) error

	// Team
	TeamList(ctx context.Context) ([]domain.BotEntry, error)
	TeamGet(ctx context.Context, name string) (domain.BotEntry, error)
	TeamHealth(ctx context.Context) (domain.TeamHealth, error)

	// Skills (admin only)
	SkillsList(ctx context.Context, botName string) ([]domain.Skill, error)
	SkillsApprove(ctx context.Context, id string) error
	SkillsReject(ctx context.Context, id string) error
	SkillsRevoke(ctx context.Context, id string) error

	// User (admin only)
	UserList(ctx context.Context) ([]domain.User, error)
	UserCreate(ctx context.Context, req domain.CreateUserRequest) (domain.User, error)
	UserRemove(ctx context.Context, username string) error
	UserDisable(ctx context.Context, username string) error
	UserSetPassword(ctx context.Context, username, newPassword string) error
	UserSetRole(ctx context.Context, username, role string) error

	// Profile
	ProfileGet(ctx context.Context) (domain.User, error)
	ProfileSetName(ctx context.Context, displayName string) error
	ProfileSetPassword(ctx context.Context, currentPassword, newPassword string) error

	// DLQ
	DLQList(ctx context.Context) ([]domain.DLQItem, error)
	DLQRetry(ctx context.Context, id string) error
	DLQDiscard(ctx context.Context, id string) error
}
