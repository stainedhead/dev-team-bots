package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestTeamList_HappyPath(t *testing.T) {
	mc := &mockClient{
		teamListResp: []domain.BotEntry{
			{Name: "dev-bot", BotType: "developer", Status: "active"},
			{Name: "qa-bot", BotType: "tester", Status: "idle"},
		},
	}
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(mc, &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "dev-bot") || !strings.Contains(got, "qa-bot") {
		t.Errorf("missing bots in output: %q", got)
	}
}

func TestTeamList_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(newErrClient("team unavailable"), &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestTeamGet_HappyPath(t *testing.T) {
	mc := &mockClient{
		teamGetResp: domain.BotEntry{Name: "dev-bot", BotType: "developer", Status: "active"},
	}
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(mc, &out)
	cmd.SetArgs([]string{"get", "dev-bot"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastTeamGetName != "dev-bot" {
		t.Errorf("expected team get for dev-bot, got %q", mc.lastTeamGetName)
	}
	got := out.String()
	if !strings.Contains(got, "dev-bot") {
		t.Errorf("missing bot name in output: %q", got)
	}
}

func TestTeamGet_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(newErrClient("not found"), &out)
	cmd.SetArgs([]string{"get", "missing-bot"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestTeamHealth_HappyPath(t *testing.T) {
	mc := &mockClient{
		teamHealthResp: domain.TeamHealth{Active: 3, Inactive: 1, Total: 4},
	}
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(mc, &out)
	cmd.SetArgs([]string{"health"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "3") || !strings.Contains(got, "4") {
		t.Errorf("missing health data in output: %q", got)
	}
}

func TestTeamHealth_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewTeamCmd(newErrClient("health unavailable"), &out)
	cmd.SetArgs([]string{"health"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
