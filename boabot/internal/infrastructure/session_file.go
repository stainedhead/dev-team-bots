// Package infrastructure provides shared infrastructure helpers used by
// multiple sub-packages in the local single-binary runtime.
package infrastructure

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// SessionRecord is the persisted form of a spawned sub-agent.
type SessionRecord struct {
	Name      string             `json:"name"`
	BotType   string             `json:"bot_type"`
	WorkDir   string             `json:"work_dir,omitempty"`
	BusID     string             `json:"bus_id"`
	Status    domain.AgentStatus `json:"status"`
	SpawnedAt time.Time          `json:"spawned_at"`
}

// SessionFile manages atomic JSON persistence of session records.
type SessionFile struct {
	path string
}

// NewSessionFile creates a SessionFile backed by the given path.
func NewSessionFile(path string) *SessionFile {
	return &SessionFile{path: path}
}

// Load reads all session records from disk. If the file does not exist, an
// empty slice is returned with no error. If the file is corrupt, a warning is
// logged and an empty slice is returned (no crash).
func (f *SessionFile) Load() ([]SessionRecord, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionRecord{}, nil
		}
		slog.Warn("session_file: failed to read file; returning empty", "path", f.path, "err", err)
		return []SessionRecord{}, nil
	}
	var records []SessionRecord
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Warn("session_file: failed to parse JSON; returning empty", "path", f.path, "err", err)
		return []SessionRecord{}, nil
	}
	return records, nil
}

// Save writes records to disk atomically by writing to a .tmp file then
// renaming it over the target path.
func (f *SessionFile) Save(records []SessionRecord) error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// Remove loads all records, filters out the one matching name, then saves.
// No-op if name is not found.
func (f *SessionFile) Remove(name string) error {
	records, err := f.Load()
	if err != nil {
		return err
	}
	filtered := records[:0]
	for _, r := range records {
		if r.Name != name {
			filtered = append(filtered, r)
		}
	}
	return f.Save(filtered)
}
