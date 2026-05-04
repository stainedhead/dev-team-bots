package domain

import "time"

type MessageType string

const (
	MessageTypeRegister            MessageType = "register"
	MessageTypeHeartbeat           MessageType = "heartbeat"
	MessageTypeShutdown            MessageType = "shutdown"
	MessageTypeOrchestratorPresence MessageType = "orchestrator.presence"
	MessageTypeOrchestratorConflict MessageType = "orchestrator.conflict"
	MessageTypeTask                MessageType = "task"
	MessageTypeTaskResult          MessageType = "task.result"
	MessageTypeBoardCreate         MessageType = "board.create"
	MessageTypeBoardUpdate         MessageType = "board.update"
	MessageTypeBoardQuery          MessageType = "board.query"
	MessageTypeTeamQuery           MessageType = "team.query"
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
	Name      string `json:"name"`
	BotType   string `json:"bot_type"`
	QueueURL  string `json:"queue_url"`
}

type HeartbeatPayload struct {
	Name   string `json:"name"`
	Status string `json:"status"`
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
