package infrastructure

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// PoolStateRecord is the persisted form of a pool entry.
type PoolStateRecord struct {
	InstanceName string                 `json:"instance_name"`
	Status       domain.PoolEntryStatus `json:"status"`
	ItemID       string                 `json:"item_id,omitempty"`
	BusID        string                 `json:"bus_id"`
	AllocatedAt  time.Time              `json:"allocated_at,omitempty"`
}

// PoolStateFile manages atomic JSON persistence of pool state records.
type PoolStateFile struct {
	path string
}

// NewPoolStateFile creates a PoolStateFile backed by the given path.
func NewPoolStateFile(path string) *PoolStateFile {
	return &PoolStateFile{path: path}
}

// Load reads all pool state records from disk. If the file does not exist, an
// empty slice is returned with no error. If the file is corrupt, a warning is
// logged and an empty slice is returned.
func (f *PoolStateFile) Load() ([]PoolStateRecord, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []PoolStateRecord{}, nil
		}
		slog.Warn("pool_state_file: failed to read file; returning empty", "path", f.path, "err", err)
		return []PoolStateRecord{}, nil
	}
	var records []PoolStateRecord
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Warn("pool_state_file: failed to parse JSON; returning empty", "path", f.path, "err", err)
		return []PoolStateRecord{}, nil
	}
	return records, nil
}

// Save writes records to disk atomically by writing to a .tmp file then
// renaming it over the target path.
func (f *PoolStateFile) Save(records []PoolStateRecord) error {
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
