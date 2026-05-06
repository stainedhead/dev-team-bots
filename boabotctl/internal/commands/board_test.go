package commands_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestBoardList_HappyPath(t *testing.T) {
	now := time.Now()
	mc := &mockClient{
		boardListResp: []domain.WorkItem{
			{ID: "wi-1", Title: "Fix bug", Status: "open", AssignedTo: "dev-bot"},
			{ID: "wi-2", Title: "Write tests", Status: "closed", AssignedTo: "", CreatedAt: now},
		},
	}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "wi-1") || !strings.Contains(got, "Fix bug") {
		t.Errorf("output missing expected content: %q", got)
	}
	if !strings.Contains(got, "wi-2") {
		t.Errorf("output missing second item: %q", got)
	}
}

func TestBoardList_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(newErrClient("board unavailable"), &out)
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBoardGet_HappyPath(t *testing.T) {
	mc := &mockClient{
		boardGetResp: domain.WorkItem{ID: "wi-5", Title: "Deploy service", Status: "in-progress", AssignedTo: "ops-bot"},
	}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"get", "wi-5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "wi-5") || !strings.Contains(got, "Deploy service") {
		t.Errorf("output missing expected content: %q", got)
	}
}

func TestBoardGet_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(newErrClient("not found"), &out)
	cmd.SetArgs([]string{"get", "wi-999"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestBoardCreate_HappyPath(t *testing.T) {
	mc := &mockClient{
		boardCreateResp: domain.WorkItem{ID: "wi-10", Title: "New feature", Status: "open"},
	}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"create", "--title", "New feature", "--description", "Do the thing"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastBoardCreateReq.Title != "New feature" {
		t.Errorf("expected title 'New feature', got %q", mc.lastBoardCreateReq.Title)
	}
	got := out.String()
	if !strings.Contains(got, "wi-10") {
		t.Errorf("output missing id: %q", got)
	}
}

func TestBoardCreate_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(newErrClient("create failed"), &out)
	cmd.SetArgs([]string{"create", "--title", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestBoardUpdate_HappyPath(t *testing.T) {
	mc := &mockClient{
		boardUpdateResp: domain.WorkItem{ID: "wi-3", Title: "Updated title", Status: "open"},
	}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"update", "wi-3", "--title", "Updated title", "--status", "in-progress"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastBoardUpdateID != "wi-3" {
		t.Errorf("wrong id: %q", mc.lastBoardUpdateID)
	}
	got := out.String()
	if !strings.Contains(got, "wi-3") {
		t.Errorf("output missing id: %q", got)
	}
}

func TestBoardAssign_HappyPath(t *testing.T) {
	mc := &mockClient{
		boardAssignResp: domain.WorkItem{ID: "wi-4", AssignedTo: "bot-x"},
	}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"assign", "wi-4", "--to", "bot-x"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastBoardAssignBot != "bot-x" {
		t.Errorf("wrong bot: %q", mc.lastBoardAssignBot)
	}
	got := out.String()
	if !strings.Contains(got, "bot-x") {
		t.Errorf("output missing bot name: %q", got)
	}
}

func TestBoardClose_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(mc, &out)
	cmd.SetArgs([]string{"close", "wi-7"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastBoardCloseID != "wi-7" {
		t.Errorf("wrong id: %q", mc.lastBoardCloseID)
	}
	got := out.String()
	if !strings.Contains(got, "Closed") {
		t.Errorf("output missing 'Closed': %q", got)
	}
}

func TestBoardClose_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewBoardCmd(newErrClient("close failed"), &out)
	cmd.SetArgs([]string{"close", "wi-7"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
