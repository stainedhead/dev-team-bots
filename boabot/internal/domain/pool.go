package domain

import (
	"context"
	"time"
)

// PoolEntryStatus represents the allocation state of a tech-lead pool entry.
type PoolEntryStatus string

const (
	PoolEntryStatusIdle        PoolEntryStatus = "idle"
	PoolEntryStatusAllocated   PoolEntryStatus = "allocated"
	PoolEntryStatusTerminating PoolEntryStatus = "terminating"
)

// PoolEntry represents a single tech-lead instance in the orchestrator pool.
type PoolEntry struct {
	InstanceName string
	Status       PoolEntryStatus
	ItemID       string
	AllocatedAt  time.Time
	BusID        string
}

// TechLeadPool manages a pool of tech-lead instances, one per In Progress kanban item.
type TechLeadPool interface {
	Allocate(ctx context.Context, itemID string) (*PoolEntry, error)
	Deallocate(ctx context.Context, itemID string) error
	Reconcile(ctx context.Context) error
	ListEntries(ctx context.Context) ([]*PoolEntry, error)
	GetByItemID(ctx context.Context, itemID string) (*PoolEntry, error)
}
