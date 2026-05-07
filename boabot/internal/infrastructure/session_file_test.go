package infrastructure_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

func TestSessionFile_LoadEmpty_WhenPathMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.json")
	sf := infrastructure.NewSessionFile(path)

	records, err := sf.Load()
	if err != nil {
		t.Fatalf("Load on missing file: unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice, got %d records", len(records))
	}
}

func TestSessionFile_SaveAndLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.json")
	sf := infrastructure.NewSessionFile(path)

	now := time.Now().UTC().Truncate(time.Second)
	records := []infrastructure.SessionRecord{
		{
			Name:      "tech-lead-1",
			BotType:   "tech-lead",
			WorkDir:   "/tmp/work",
			BusID:     "bus-abc",
			Status:    domain.AgentStatusIdle,
			SpawnedAt: now,
		},
	}

	if err := sf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := sf.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 record, got %d", len(loaded))
	}
	if loaded[0].Name != "tech-lead-1" {
		t.Errorf("expected Name=tech-lead-1, got %q", loaded[0].Name)
	}
	if loaded[0].Status != domain.AgentStatusIdle {
		t.Errorf("expected Status=idle, got %q", loaded[0].Status)
	}
	if !loaded[0].SpawnedAt.Equal(now) {
		t.Errorf("expected SpawnedAt=%v, got %v", now, loaded[0].SpawnedAt)
	}
}

func TestSessionFile_Save_Atomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	sf := infrastructure.NewSessionFile(path)

	// Save initial records.
	initial := []infrastructure.SessionRecord{
		{Name: "tech-lead-1", BotType: "tech-lead", Status: domain.AgentStatusIdle, SpawnedAt: time.Now()},
	}
	if err := sf.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Verify .tmp file is NOT left behind after a successful save.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be gone after successful save")
	}

	// The main file should exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected main file to exist after save: %v", err)
	}
}

func TestSessionFile_Load_CorruptFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	// Write corrupt JSON.
	if err := os.WriteFile(path, []byte("{corrupt json..."), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	sf := infrastructure.NewSessionFile(path)
	records, err := sf.Load()
	if err != nil {
		t.Fatalf("Load on corrupt file: expected nil error, got %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice on corrupt file, got %d records", len(records))
	}
}

func TestSessionFile_Remove(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.json")
	sf := infrastructure.NewSessionFile(path)

	now := time.Now().UTC()
	records := []infrastructure.SessionRecord{
		{Name: "tech-lead-1", BotType: "tech-lead", Status: domain.AgentStatusIdle, SpawnedAt: now},
		{Name: "tech-lead-2", BotType: "tech-lead", Status: domain.AgentStatusWorking, SpawnedAt: now},
	}
	if err := sf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := sf.Remove("tech-lead-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	loaded, err := sf.Load()
	if err != nil {
		t.Fatalf("Load after Remove: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 record after Remove, got %d", len(loaded))
	}
	if loaded[0].Name != "tech-lead-2" {
		t.Errorf("expected tech-lead-2 to remain, got %q", loaded[0].Name)
	}
}

func TestSessionFile_Remove_UnknownName_Noop(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.json")
	sf := infrastructure.NewSessionFile(path)

	now := time.Now().UTC()
	records := []infrastructure.SessionRecord{
		{Name: "tech-lead-1", BotType: "tech-lead", Status: domain.AgentStatusIdle, SpawnedAt: now},
	}
	if err := sf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Remove a non-existent entry — should be a no-op, not an error.
	if err := sf.Remove("nobody"); err != nil {
		t.Fatalf("Remove unknown name: unexpected error: %v", err)
	}

	loaded, err := sf.Load()
	if err != nil {
		t.Fatalf("Load after Remove unknown: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 record (unchanged), got %d", len(loaded))
	}
}

func TestSessionFile_Save_MultipleRecords_Roundtrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.json")
	sf := infrastructure.NewSessionFile(path)

	now := time.Now().UTC().Truncate(time.Second)
	input := []infrastructure.SessionRecord{
		{Name: "tech-lead-1", BotType: "tech-lead", WorkDir: "/a", BusID: "b1", Status: domain.AgentStatusIdle, SpawnedAt: now},
		{Name: "tech-lead-2", BotType: "tech-lead", WorkDir: "/b", BusID: "b2", Status: domain.AgentStatusWorking, SpawnedAt: now},
	}

	if err := sf.Save(input); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify raw JSON has expected fields.
	data, _ := os.ReadFile(path)
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw JSON: %v", err)
	}
	if len(raw) != 2 {
		t.Errorf("expected 2 raw records, got %d", len(raw))
	}
}
