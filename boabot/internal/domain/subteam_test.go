package domain_test

import (
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestAgentStatus_Constants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status domain.AgentStatus
		want   string
	}{
		{domain.AgentStatusIdle, "idle"},
		{domain.AgentStatusWorking, "working"},
		{domain.AgentStatusTerminating, "terminating"},
		{domain.AgentStatusTerminated, "terminated"},
	}
	for _, tc := range cases {
		if string(tc.status) != tc.want {
			t.Errorf("AgentStatus %q: got %q", tc.want, string(tc.status))
		}
	}
}

func TestSpawnedAgent_ZeroValue(t *testing.T) {
	t.Parallel()
	var a domain.SpawnedAgent
	if a.Name != "" {
		t.Error("expected empty Name on zero value")
	}
	if a.Status != "" {
		t.Error("expected empty Status on zero value")
	}
	if !a.SpawnedAt.IsZero() {
		t.Error("expected zero SpawnedAt on zero value")
	}
}

func TestSpawnedAgent_Construction(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a := domain.SpawnedAgent{
		Name:      "tech-lead-1",
		BotType:   "tech-lead",
		WorkDir:   "/tmp/work",
		BusID:     "bus-abc",
		Status:    domain.AgentStatusIdle,
		SpawnedAt: now,
	}
	if a.Name != "tech-lead-1" {
		t.Errorf("expected Name=tech-lead-1, got %q", a.Name)
	}
	if a.BotType != "tech-lead" {
		t.Errorf("expected BotType=tech-lead, got %q", a.BotType)
	}
	if a.Status != domain.AgentStatusIdle {
		t.Errorf("expected Status=idle, got %q", a.Status)
	}
	if !a.SpawnedAt.Equal(now) {
		t.Errorf("expected SpawnedAt=%v, got %v", now, a.SpawnedAt)
	}
}
