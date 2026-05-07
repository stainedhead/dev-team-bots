package domain_test

import (
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestPoolEntryStatus_Constants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status domain.PoolEntryStatus
		want   string
	}{
		{domain.PoolEntryStatusIdle, "idle"},
		{domain.PoolEntryStatusAllocated, "allocated"},
		{domain.PoolEntryStatusTerminating, "terminating"},
	}
	for _, tc := range cases {
		if string(tc.status) != tc.want {
			t.Errorf("PoolEntryStatus %q: got %q", tc.want, string(tc.status))
		}
	}
}

func TestPoolEntry_ZeroValue(t *testing.T) {
	t.Parallel()
	var e domain.PoolEntry
	if e.InstanceName != "" {
		t.Error("expected empty InstanceName on zero value")
	}
	if e.Status != "" {
		t.Error("expected empty Status on zero value")
	}
	if !e.AllocatedAt.IsZero() {
		t.Error("expected zero AllocatedAt on zero value")
	}
}

func TestPoolEntry_Construction(t *testing.T) {
	t.Parallel()
	now := time.Now()
	e := domain.PoolEntry{
		InstanceName: "tech-lead-1",
		Status:       domain.PoolEntryStatusAllocated,
		ItemID:       "item-abc",
		AllocatedAt:  now,
		BusID:        "bus-xyz",
	}
	if e.InstanceName != "tech-lead-1" {
		t.Errorf("expected InstanceName=tech-lead-1, got %q", e.InstanceName)
	}
	if e.Status != domain.PoolEntryStatusAllocated {
		t.Errorf("expected Status=allocated, got %q", e.Status)
	}
	if e.ItemID != "item-abc" {
		t.Errorf("expected ItemID=item-abc, got %q", e.ItemID)
	}
	if !e.AllocatedAt.Equal(now) {
		t.Errorf("expected AllocatedAt=%v, got %v", now, e.AllocatedAt)
	}
}
