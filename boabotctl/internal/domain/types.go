package domain

import "time"

// Attachment holds a file uploaded to a WorkItem.
type Attachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Content     string    `json:"content"` // base64-encoded
	Size        int       `json:"size"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

type WorkItem struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	WorkDir      string       `json:"work_dir,omitempty"`
	Status       string       `json:"status"`
	AssignedTo   string       `json:"assigned_to"`
	ActiveTaskID string       `json:"active_task_id,omitempty"`
	LastResult   string       `json:"last_result,omitempty"`
	LastResultAt *time.Time   `json:"last_result_at,omitempty"`
	Attachments  []Attachment `json:"attachments,omitempty"`
	CreatedBy    string       `json:"created_by"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// ActivityResponse is returned by GET /api/v1/board/{id}/activity.
type ActivityResponse struct {
	Item WorkItem    `json:"item"`
	Task *DirectTask `json:"task,omitempty"`
}

// DirectTask is a task dispatched directly to a bot.
type DirectTask struct {
	ID           string     `json:"id"`
	BotName      string     `json:"bot_name"`
	Source       string     `json:"source,omitempty"`
	ThreadID     string     `json:"thread_id,omitempty"`
	Instruction  string     `json:"instruction"`
	Status       string     `json:"status"`
	Output       string     `json:"output,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	DispatchedAt *time.Time `json:"dispatched_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ChatThread is a named conversation session.
type ChatThread struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Participants []string  `json:"participants"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ChatMessage is one turn in an operator↔bot conversation.
type ChatMessage struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id,omitempty"`
	BotName   string    `json:"bot_name"`
	Direction string    `json:"direction"` // "inbound" | "outbound"
	Content   string    `json:"content"`
	TaskID    string    `json:"task_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateWorkItemRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AssignedTo  string `json:"assigned_to,omitempty"`
	WorkDir     string `json:"work_dir,omitempty"`
}

type UpdateWorkItemRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	AssignedTo  *string `json:"assigned_to,omitempty"`
	WorkDir     *string `json:"work_dir,omitempty"`
}

type BotEntry struct {
	Name     string    `json:"name"`
	BotType  string    `json:"bot_type"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

type TeamHealth struct {
	Active   int `json:"active"`
	Inactive int `json:"inactive"`
	Total    int `json:"total"`
}

type Skill struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary"`
	BotName    string    `json:"bot_name"`
	Status     string    `json:"status"`
	UploadedAt time.Time `json:"uploaded_at"`
	ApprovedAt time.Time `json:"approved_at,omitempty"`
}

type User struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Password string `json:"password,omitempty"`
}

type LoginResponse struct {
	Token              string `json:"token"`
	MustChangePassword bool   `json:"must_change_password"`
}

// DLQItem represents a message in the dead-letter queue.
type DLQItem struct {
	ID            string    `json:"id"`
	QueueName     string    `json:"queue_name"`
	Body          string    `json:"body"`
	ReceivedCount int       `json:"received_count"`
	FirstReceived time.Time `json:"first_received"`
	LastReceived  time.Time `json:"last_received"`
}

// MemoryStatusResponse describes the current state of the memory backup.
type MemoryStatusResponse struct {
	LastBackupAt   time.Time `json:"last_backup_at"`
	PendingChanges int       `json:"pending_changes"`
	RemoteURL      string    `json:"remote_url"`
}

// Priority and start fields for board create.
type CreateWorkItemRequestV2 struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	AssignedTo  string  `json:"assigned_to,omitempty"`
	Priority    int     `json:"priority,omitempty"`
	StartAt     *string `json:"start_at,omitempty"`
}

// UpdateWorkItemRequestV2 includes priority.
type UpdateWorkItemRequestV2 struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	AssignedTo  *string `json:"assigned_to,omitempty"`
}
