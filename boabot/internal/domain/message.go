package domain

import "time"

type MessageType string

const (
	// Lifecycle
	MessageTypeRegister  MessageType = "register"
	MessageTypeHeartbeat MessageType = "heartbeat"
	MessageTypeShutdown  MessageType = "shutdown"

	// Orchestrator coordination
	MessageTypeOrchestratorPresence MessageType = "orchestrator.presence"
	MessageTypeOrchestratorConflict MessageType = "orchestrator.conflict"

	// Team snapshot (startup registry sync)
	MessageTypeTeamSnapshot      MessageType = "team.snapshot"
	MessageTypeTeamSnapshotReply MessageType = "team.snapshot.reply"

	// Task dispatch
	MessageTypeTask       MessageType = "task"
	MessageTypeTaskResult MessageType = "task.result"

	// Structured delegation (A2A-shaped lifecycle)
	MessageTypeDelegateSubmitted     MessageType = "delegate.submitted"
	MessageTypeDelegateWorking       MessageType = "delegate.working"
	MessageTypeDelegateInputRequired MessageType = "delegate.input_required"
	MessageTypeDelegateCompleted     MessageType = "delegate.completed"
	MessageTypeDelegateFailed        MessageType = "delegate.failed"

	// Shared memory
	MessageTypeMemoryWrite MessageType = "memory.write"

	// Board
	MessageTypeBoardCreate MessageType = "board.create"
	MessageTypeBoardUpdate MessageType = "board.update"
	MessageTypeBoardQuery  MessageType = "board.query"

	// Team query
	MessageTypeTeamQuery MessageType = "team.query"
)

type Message struct {
	ID        string      `json:"id"`
	Type      MessageType `json:"type"`
	From      string      `json:"from"`
	To        string      `json:"to,omitempty"`
	Payload   []byte      `json:"payload,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

type RegisterPayload struct {
	Name     string `json:"name"`
	BotType  string `json:"bot_type"`
	QueueURL string `json:"queue_url"`
}

type HeartbeatPayload struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type TeamSnapshotPayload struct {
	RequesterName string `json:"requester_name"`
}

type TeamSnapshotReplyPayload struct {
	Bots  []BotEntry  `json:"bots"`
	Cards []AgentCard `json:"cards"`
}

type TaskPayload struct {
	TaskID      string `json:"task_id"`
	BoardItemID string `json:"board_item_id,omitempty"`
	Instruction string `json:"instruction"`
}

type TaskResultPayload struct {
	TaskID  string `json:"task_id"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type DelegatePayload struct {
	TaskID          string `json:"task_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	Instruction     string `json:"instruction,omitempty"`
	StatusMessage   string `json:"status_message,omitempty"`
	Output          string `json:"output,omitempty"`
	Error           string `json:"error,omitempty"`
	InputPrompt     string `json:"input_prompt,omitempty"`
}

type MemoryWritePayload struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}
