package domain

import (
	"context"
	"time"
)

// AgentCard describes a bot's capabilities and delegation interface.
// Published to S3 on startup and distributed via SNS broadcast.
type AgentCard struct {
	Name         string    `json:"name"`
	BotType      string    `json:"bot_type"`
	QueueURL     string    `json:"queue_url"`
	Description  string    `json:"description"`
	Capabilities []string  `json:"capabilities"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CardRegistry is a local in-memory cache of team Agent Cards,
// seeded from team_snapshot on startup and kept current via SNS broadcasts.
type CardRegistry interface {
	Get(ctx context.Context, name string) (AgentCard, error)
	Set(ctx context.Context, card AgentCard) error
	List(ctx context.Context) ([]AgentCard, error)
}
