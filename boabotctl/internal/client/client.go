package client

import (
	"context"
	"time"

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
	BoardActivity(ctx context.Context, id string) (domain.ActivityResponse, error)
	BoardAsk(ctx context.Context, id, content, threadID string) (domain.ChatMessage, error)
	BoardAttachmentUpload(ctx context.Context, id string, paths []string) (domain.WorkItem, error)
	BoardAttachmentGet(ctx context.Context, id, attID string) (content []byte, contentType, filename string, err error)
	BoardAttachmentDelete(ctx context.Context, id, attID string) error

	// Tasks
	TaskList(ctx context.Context) ([]domain.DirectTask, error)
	TaskListByBot(ctx context.Context, botName string) ([]domain.DirectTask, error)
	TaskCreate(ctx context.Context, botName, instruction string, scheduledAt *time.Time) (domain.DirectTask, error)
	TaskGet(ctx context.Context, id string) (domain.DirectTask, error)

	// Chat / Threads
	ThreadList(ctx context.Context) ([]domain.ChatThread, error)
	ThreadCreate(ctx context.Context, title string, participants []string) (domain.ChatThread, error)
	ThreadDelete(ctx context.Context, id string) error
	ThreadMessages(ctx context.Context, id string) ([]domain.ChatMessage, error)
	ChatSend(ctx context.Context, botName, content, threadID string) (domain.ChatMessage, error)

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

	// Memory backup
	MemoryBackup(ctx context.Context) error
	MemoryRestore(ctx context.Context) error
	MemoryStatus(ctx context.Context) (domain.MemoryStatusResponse, error)
}
