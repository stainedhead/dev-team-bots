package domain

import "time"

type WorkItem struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	AssignedTo  string         `json:"assigned_to"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CreateWorkItemRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AssignedTo  string `json:"assigned_to,omitempty"`
}

type UpdateWorkItemRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	AssignedTo  *string `json:"assigned_to,omitempty"`
}

type BotEntry struct {
	Name      string    `json:"name"`
	BotType   string    `json:"bot_type"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
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
}

type LoginResponse struct {
	Token              string `json:"token"`
	MustChangePassword bool   `json:"must_change_password"`
}
