package domain

import (
	"context"
	"time"
)

// AgentStatus represents the lifecycle state of a spawned sub-agent.
type AgentStatus string

const (
	AgentStatusIdle        AgentStatus = "idle"
	AgentStatusWorking     AgentStatus = "working"
	AgentStatusTerminating AgentStatus = "terminating"
	AgentStatusTerminated  AgentStatus = "terminated"
)

// SpawnedAgent is an entity representing a live spawned sub-agent.
type SpawnedAgent struct {
	Name      string
	BotType   string
	WorkDir   string
	BusID     string
	Status    AgentStatus
	SpawnedAt time.Time
}

// SubTeamManager is the domain interface for managing a tech-lead's sub-agents.
type SubTeamManager interface {
	Spawn(ctx context.Context, botType, name, workDir string) (*SpawnedAgent, error)
	Terminate(ctx context.Context, name string) error
	SendHeartbeat(ctx context.Context) error
	ListAgents(ctx context.Context) ([]*SpawnedAgent, error)
	TearDownAll(ctx context.Context) error
}
