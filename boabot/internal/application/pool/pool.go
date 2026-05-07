// Package pool provides the TechLeadPool application service for the orchestrator.
// It manages a pool of tech-lead instances, one per In Progress kanban item,
// with idle instance reuse and a warm-standby guarantee.
package pool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

// Config configures the TechLeadPool.
type Config struct {
	// BotsDir is the directory containing bots/<type>/config.yaml.
	BotsDir string
	// MemoryRoot is the base path for per-bot memory files.
	MemoryRoot string
	// SpawnTimeout is the maximum time to wait for a new spawn. Defaults to 1s.
	SpawnTimeout time.Duration
	// SoftPoolLimit is the pool size at which a warning is logged. Defaults to 10.
	SoftPoolLimit int
}

func (c *Config) applyDefaults() {
	if c.SpawnTimeout <= 0 {
		c.SpawnTimeout = time.Second
	}
	if c.SoftPoolLimit <= 0 {
		c.SoftPoolLimit = 10
	}
}

// Pool implements domain.TechLeadPool.
type Pool struct {
	mu      sync.Mutex
	entries []*domain.PoolEntry
	counter int
	cfg     Config
	file    *infrastructure.PoolStateFile
	spawnFn func(ctx context.Context, instanceName string) error
	stopFn  func(ctx context.Context, instanceName string) error
	isRunFn func(ctx context.Context, instanceName string) bool
}

// New creates a new Pool.
func New(cfg Config, file *infrastructure.PoolStateFile) *Pool {
	cfg.applyDefaults()
	return &Pool{
		cfg:  cfg,
		file: file,
		spawnFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("pool: no spawnFn configured")
		},
		stopFn:  func(_ context.Context, _ string) error { return nil },
		isRunFn: func(_ context.Context, _ string) bool { return false },
	}
}

// SetSpawnFn injects the function used to start a new tech-lead instance.
// Used for testing.
func (p *Pool) SetSpawnFn(fn func(ctx context.Context, instanceName string) error) {
	p.spawnFn = fn
}

// SetStopFn injects the function used to stop a tech-lead instance.
// Used for testing.
func (p *Pool) SetStopFn(fn func(ctx context.Context, instanceName string) error) {
	p.stopFn = fn
}

// SetIsRunningFn injects the function used to check if an instance is running.
// Used for testing and Reconcile.
func (p *Pool) SetIsRunningFn(fn func(ctx context.Context, instanceName string) bool) {
	p.isRunFn = fn
}

// nextName returns the next tech-lead instance name. Must be called under p.mu.
func (p *Pool) nextName() string {
	p.counter++
	return fmt.Sprintf("tech-lead-%d", p.counter)
}

// Allocate assigns a pool entry to itemID. All operations are serialised by a mutex.
// If an idle entry exists it is reused; otherwise a new instance is spawned.
func (p *Pool) Allocate(ctx context.Context, itemID string) (*domain.PoolEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for duplicate.
	for _, e := range p.entries {
		if e.ItemID == itemID {
			return nil, fmt.Errorf("pool: itemID %q already has an entry", itemID)
		}
	}

	if len(p.entries)+1 > p.cfg.SoftPoolLimit {
		slog.Warn("pool: soft pool limit exceeded",
			"limit", p.cfg.SoftPoolLimit,
			"current", len(p.entries)+1)
	}

	// Reuse the first idle entry if available.
	for _, e := range p.entries {
		if e.Status == domain.PoolEntryStatusIdle {
			e.Status = domain.PoolEntryStatusAllocated
			e.ItemID = itemID
			e.AllocatedAt = time.Now().UTC()
			p.saveUnlocked()
			return e, nil
		}
	}

	// No idle entry — spawn a new instance.
	name := p.nextName()

	spawnCtx, cancel := context.WithTimeout(ctx, p.cfg.SpawnTimeout)
	defer cancel()

	if err := p.spawnFn(spawnCtx, name); err != nil {
		p.counter-- // roll back counter on failure
		return nil, fmt.Errorf("pool: spawn %q: %w", name, err)
	}

	entry := &domain.PoolEntry{
		InstanceName: name,
		Status:       domain.PoolEntryStatusAllocated,
		ItemID:       itemID,
		AllocatedAt:  time.Now().UTC(),
		BusID:        fmt.Sprintf("bus-%s", name),
	}
	p.entries = append(p.entries, entry)
	p.saveUnlocked()
	return entry, nil
}

// Deallocate releases the pool entry for itemID. If it is the last entry, the
// instance is kept as an idle warm standby. Otherwise the instance is stopped
// and the entry is removed.
func (p *Pool) Deallocate(ctx context.Context, itemID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := -1
	for i, e := range p.entries {
		if e.ItemID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("pool: no entry found for itemID %q", itemID)
	}

	entry := p.entries[idx]

	if len(p.entries) == 1 {
		// Last entry — keep as warm standby.
		entry.Status = domain.PoolEntryStatusIdle
		entry.ItemID = ""
		p.saveUnlocked()
		return nil
	}

	// Not the last — stop and remove.
	instanceName := entry.InstanceName
	p.entries = append(p.entries[:idx], p.entries[idx+1:]...)
	p.saveUnlocked()

	if err := p.stopFn(ctx, instanceName); err != nil {
		slog.Warn("pool: stopFn error on deallocate",
			"instance", instanceName, "err", err)
	}
	return nil
}

// Reconcile loads the persisted pool state, checks which instances are still
// running, and removes stale records.
func (p *Pool) Reconcile(ctx context.Context) error {
	records, err := p.file.Load()
	if err != nil {
		return fmt.Errorf("pool: reconcile load: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.entries = p.entries[:0]
	for _, r := range records {
		if !p.isRunFn(ctx, r.InstanceName) {
			slog.Info("pool: reconcile removing stale entry", "instance", r.InstanceName)
			continue
		}
		p.entries = append(p.entries, &domain.PoolEntry{
			InstanceName: r.InstanceName,
			Status:       r.Status,
			ItemID:       r.ItemID,
			AllocatedAt:  r.AllocatedAt,
			BusID:        r.BusID,
		})
	}
	p.saveUnlocked()
	return nil
}

// ListEntries returns a snapshot of all pool entries.
func (p *Pool) ListEntries(_ context.Context) ([]*domain.PoolEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*domain.PoolEntry, len(p.entries))
	for i, e := range p.entries {
		cp := *e
		out[i] = &cp
	}
	return out, nil
}

// GetByItemID returns the pool entry associated with itemID.
// Returns an error if no matching entry is found.
func (p *Pool) GetByItemID(_ context.Context, itemID string) (*domain.PoolEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.entries {
		if e.ItemID == itemID {
			cp := *e
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("pool: no entry found for itemID %q", itemID)
}

// saveUnlocked persists the current entries to the state file.
// Must be called with p.mu held.
func (p *Pool) saveUnlocked() {
	if p.file == nil {
		return
	}
	records := make([]infrastructure.PoolStateRecord, len(p.entries))
	for i, e := range p.entries {
		records[i] = infrastructure.PoolStateRecord{
			InstanceName: e.InstanceName,
			Status:       e.Status,
			ItemID:       e.ItemID,
			BusID:        e.BusID,
			AllocatedAt:  e.AllocatedAt,
		}
	}
	if err := p.file.Save(records); err != nil {
		slog.Warn("pool: failed to persist state", "err", err)
	}
}
