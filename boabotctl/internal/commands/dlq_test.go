package commands_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestDLQList_HappyPath(t *testing.T) {
	now := time.Now()
	mc := &mockClient{
		dlqListResp: []domain.DLQItem{
			{ID: "dlq-1", QueueName: "tasks", ReceivedCount: 3, LastReceived: now},
			{ID: "dlq-2", QueueName: "events", ReceivedCount: 1, LastReceived: now},
		},
	}
	var out bytes.Buffer
	cmd := commands.NewDLQCmd(mc, &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "dlq-1") || !strings.Contains(got, "dlq-2") {
		t.Errorf("missing DLQ items in output: %q", got)
	}
}

func TestDLQList_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewDLQCmd(newErrClient("dlq unavailable"), &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDLQRetry_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewDLQCmd(mc, &out)
	cmd.SetArgs([]string{"retry", "dlq-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastDLQRetryID != "dlq-1" {
		t.Errorf("expected retry for 'dlq-1', got %q", mc.lastDLQRetryID)
	}
	got := out.String()
	if !strings.Contains(got, "Retried") {
		t.Errorf("output missing 'Retried': %q", got)
	}
}

func TestDLQRetry_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewDLQCmd(newErrClient("retry failed"), &out)
	cmd.SetArgs([]string{"retry", "dlq-1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDLQDiscard_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	cmd := commands.NewDLQCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"discard", "dlq-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastDLQDiscardID != "dlq-1" {
		t.Errorf("expected discard for 'dlq-1', got %q", mc.lastDLQDiscardID)
	}
	got := out.String()
	if !strings.Contains(got, "Discarded") {
		t.Errorf("output missing 'Discarded': %q", got)
	}
}

func TestDLQDiscard_Cancelled(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	in := strings.NewReader("n\n")
	cmd := commands.NewDLQCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"discard", "dlq-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastDLQDiscardID != "" {
		t.Errorf("discard should not have been called when cancelled")
	}
}

func TestDLQDiscard_Error(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	cmd := commands.NewDLQCmdWithIO(newErrClient("discard failed"), &out, in)
	cmd.SetArgs([]string{"discard", "dlq-1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
