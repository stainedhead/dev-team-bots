package notifications

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrRequeueConflict is returned when RequeueTask is called on a running task.
var ErrRequeueConflict = errors.New("notifications: task is currently running")

// discussCap is the maximum number of entries in a notification's discuss thread.
const discussCap = 100

// NotificationService manages in-app agent notifications.
type NotificationService struct {
	store        domain.AgentNotificationStore
	taskStore    domain.DirectTaskStore
	appendDiscMu sync.Mutex // serialises concurrent AppendDiscuss calls
}

// NewNotificationService constructs a NotificationService.
func NewNotificationService(store domain.AgentNotificationStore, taskStore domain.DirectTaskStore) *NotificationService {
	return &NotificationService{
		store:     store,
		taskStore: taskStore,
	}
}

// newID generates a random hex-encoded ID using 8 bytes from crypto/rand.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RaiseNotification creates and persists a new AgentNotification.
// Generates a UUID for the ID, sets Status=unread, sets CreatedAt=now.
func (s *NotificationService) RaiseNotification(ctx context.Context, botName, taskID, workItemID, message, contextSummary string) (domain.AgentNotification, error) {
	id, err := newID()
	if err != nil {
		return domain.AgentNotification{}, fmt.Errorf("notifications: generate ID: %w", err)
	}

	n := domain.AgentNotification{
		ID:             id,
		BotName:        botName,
		TaskID:         taskID,
		WorkItemID:     workItemID,
		Message:        message,
		ContextSummary: contextSummary,
		Status:         domain.AgentNotificationStatusUnread,
		CreatedAt:      time.Now().UTC(),
	}

	if err := s.store.Save(ctx, n); err != nil {
		return domain.AgentNotification{}, fmt.Errorf("notifications: save: %w", err)
	}
	return n, nil
}

// List returns notifications matching the filter.
func (s *NotificationService) List(ctx context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
	return s.store.List(ctx, filter)
}

// UnreadCount returns the number of unread notifications.
func (s *NotificationService) UnreadCount(ctx context.Context) (int, error) {
	return s.store.UnreadCount(ctx)
}

// AppendDiscuss adds a DiscussEntry to a notification's discuss thread.
// Enforces the 100-entry cap: if the thread already has 100 entries, the oldest
// is removed before appending. Also transitions Status from unread → read if it
// was unread. Safe for concurrent use.
func (s *NotificationService) AppendDiscuss(ctx context.Context, id, author, message string) error {
	s.appendDiscMu.Lock()
	defer s.appendDiscMu.Unlock()

	n, err := s.store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("notifications: get for append discuss: %w", err)
	}

	entry := domain.DiscussEntry{
		Author:    author,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}

	// Enforce 100-entry cap: drop the oldest if already at capacity.
	if len(n.DiscussThread) >= discussCap {
		n.DiscussThread = n.DiscussThread[1:]
	}
	n.DiscussThread = append(n.DiscussThread, entry)

	// Transition status from unread → read.
	if n.Status == domain.AgentNotificationStatusUnread {
		n.Status = domain.AgentNotificationStatusRead
	}

	if err := s.store.Save(ctx, n); err != nil {
		return fmt.Errorf("notifications: save after append discuss: %w", err)
	}
	return nil
}

// ActionNotification marks a notification as actioned.
func (s *NotificationService) ActionNotification(ctx context.Context, id string) error {
	if err := s.store.MarkActioned(ctx, id); err != nil {
		return fmt.Errorf("notifications: action: %w", err)
	}
	return nil
}

// RequeueTask appends the notification's discuss thread as context to the
// originating task and re-queues it. Returns an error if:
//   - the notification has no TaskID
//   - the task does not exist
//   - the task is currently running (status == "running") — wrapped RequeueConflictErr
func (s *NotificationService) RequeueTask(ctx context.Context, notificationID string) error {
	n, err := s.store.Get(ctx, notificationID)
	if err != nil {
		return fmt.Errorf("notifications: get notification: %w", err)
	}

	if n.TaskID == "" {
		return fmt.Errorf("notifications: notification %s has no task ID", notificationID)
	}

	task, err := s.taskStore.Get(ctx, n.TaskID)
	if err != nil {
		return fmt.Errorf("notifications: get task %s: %w", n.TaskID, err)
	}

	if task.Status == domain.DirectTaskStatusRunning {
		return fmt.Errorf("notifications: requeue task %s: %w", n.TaskID, ErrRequeueConflict)
	}

	// Format the discuss thread as context and prepend to the task instruction.
	task.Instruction = buildDiscussContext(notificationID, n.DiscussThread, task.Instruction)
	task.Status = domain.DirectTaskStatusPending
	task.NextRunAt = nil
	task.Schedule = domain.Schedule{Mode: domain.ScheduleModeASAP}

	if _, err := s.taskStore.Update(ctx, task); err != nil {
		return fmt.Errorf("notifications: update task %s: %w", n.TaskID, err)
	}
	return nil
}

// Delete removes notifications by ID.
func (s *NotificationService) Delete(ctx context.Context, ids []string) error {
	if err := s.store.Delete(ctx, ids); err != nil {
		return fmt.Errorf("notifications: delete: %w", err)
	}
	return nil
}

// buildDiscussContext formats the discuss thread entries and prepends them to
// the original instruction with a header and separator.
func buildDiscussContext(notificationID string, thread []domain.DiscussEntry, originalInstruction string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Discuss context from notification %s]\n", notificationID)
	for _, entry := range thread {
		fmt.Fprintf(&sb, "%s: %s\n", entry.Author, entry.Message)
	}
	sb.WriteString("---\n")
	sb.WriteString(originalInstruction)
	return sb.String()
}
