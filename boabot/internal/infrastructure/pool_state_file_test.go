package infrastructure_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

func TestPoolStateFile_LoadEmpty_WhenPathMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pool.json")
	psf := infrastructure.NewPoolStateFile(path)

	records, err := psf.Load()
	if err != nil {
		t.Fatalf("Load on missing file: unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice, got %d records", len(records))
	}
}

func TestPoolStateFile_SaveAndLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pool.json")
	psf := infrastructure.NewPoolStateFile(path)

	now := time.Now().UTC().Truncate(time.Second)
	records := []infrastructure.PoolStateRecord{
		{
			InstanceName: "tech-lead-1",
			Status:       domain.PoolEntryStatusAllocated,
			ItemID:       "item-abc",
			BusID:        "bus-xyz",
			AllocatedAt:  now,
		},
	}

	if err := psf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := psf.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 record, got %d", len(loaded))
	}
	if loaded[0].InstanceName != "tech-lead-1" {
		t.Errorf("expected InstanceName=tech-lead-1, got %q", loaded[0].InstanceName)
	}
	if loaded[0].Status != domain.PoolEntryStatusAllocated {
		t.Errorf("expected Status=allocated, got %q", loaded[0].Status)
	}
	if loaded[0].ItemID != "item-abc" {
		t.Errorf("expected ItemID=item-abc, got %q", loaded[0].ItemID)
	}
	if !loaded[0].AllocatedAt.Equal(now) {
		t.Errorf("expected AllocatedAt=%v, got %v", now, loaded[0].AllocatedAt)
	}
}

func TestPoolStateFile_Save_Atomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pool.json")
	psf := infrastructure.NewPoolStateFile(path)

	records := []infrastructure.PoolStateRecord{
		{InstanceName: "tech-lead-1", Status: domain.PoolEntryStatusIdle, BusID: "b1"},
	}
	if err := psf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// .tmp file must be gone after successful save.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be removed after successful save")
	}

	// Main file must exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected main file to exist: %v", err)
	}
}

func TestPoolStateFile_Load_CorruptFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pool.json")

	if err := os.WriteFile(path, []byte("not-valid-json!!!"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	psf := infrastructure.NewPoolStateFile(path)
	records, err := psf.Load()
	if err != nil {
		t.Fatalf("Load on corrupt file: expected nil error, got %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice on corrupt file, got %d records", len(records))
	}
}

func TestPoolStateFile_Save_MultipleRecords(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pool.json")
	psf := infrastructure.NewPoolStateFile(path)

	now := time.Now().UTC().Truncate(time.Second)
	records := []infrastructure.PoolStateRecord{
		{InstanceName: "tech-lead-1", Status: domain.PoolEntryStatusIdle, BusID: "b1", AllocatedAt: now},
		{InstanceName: "tech-lead-2", Status: domain.PoolEntryStatusAllocated, ItemID: "i1", BusID: "b2", AllocatedAt: now},
	}
	if err := psf.Save(records); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := psf.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 records, got %d", len(loaded))
	}
}
