package commands_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// ── memory backup ─────────────────────────────────────────────────────────────

func TestMemoryBackup_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(mc, &out)
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Backup triggered") {
		t.Errorf("expected 'Backup triggered' in output, got %q", got)
	}
}

func TestMemoryBackup_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(newErrClient("backup failed"), &out)
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

// ── memory restore ────────────────────────────────────────────────────────────

func TestMemoryRestore_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(mc, &out)
	cmd.SetArgs([]string{"restore"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Restore triggered") {
		t.Errorf("expected 'Restore triggered' in output, got %q", got)
	}
}

func TestMemoryRestore_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(newErrClient("restore failed"), &out)
	cmd.SetArgs([]string{"restore"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

// ── memory status ─────────────────────────────────────────────────────────────

func TestMemoryStatus_HappyPath(t *testing.T) {
	backupTime := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	mc := &mockClient{
		memoryStatusResp: domain.MemoryStatusResponse{
			LastBackupAt:   backupTime,
			PendingChanges: 3,
			RemoteURL:      "https://github.com/owner/boabot-memory.git",
		},
	}
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(mc, &out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "2026-05-06") {
		t.Errorf("expected backup time in output, got %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("expected pending changes '3' in output, got %q", got)
	}
	if !strings.Contains(got, "https://github.com/owner/boabot-memory.git") {
		t.Errorf("expected remote URL in output, got %q", got)
	}
}

func TestMemoryStatus_NeverBackedUp(t *testing.T) {
	mc := &mockClient{
		memoryStatusResp: domain.MemoryStatusResponse{
			LastBackupAt:   time.Time{}, // zero
			PendingChanges: 0,
			RemoteURL:      "https://github.com/owner/boabot-memory.git",
		},
	}
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(mc, &out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "never") {
		t.Errorf("expected 'never' when no backup yet, got %q", got)
	}
}

func TestMemoryStatus_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewMemoryCmd(newErrClient("status unavailable"), &out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

// ── nil writer defaults to stdout ─────────────────────────────────────────────

func TestMemoryCmd_NilWriter(t *testing.T) {
	mc := &mockClient{}
	// Should not panic; falls back to os.Stdout.
	cmd := commands.NewMemoryCmd(mc, nil)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}
