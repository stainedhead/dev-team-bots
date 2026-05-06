// Package mocks provides hand-written test doubles for workflow application
// interfaces.
package mocks

import (
	"context"
	"sync"
	"time"

	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
)

// WorkItemStore is a thread-safe mock of wf.WorkItemStore.
type WorkItemStore struct {
	mu sync.Mutex

	CreateFn          func(ctx context.Context, item wf.WorkItem) error
	GetFn             func(ctx context.Context, id string) (wf.WorkItem, error)
	UpdateFn          func(ctx context.Context, item wf.WorkItem) error
	ListByStatusFn    func(ctx context.Context, status wf.WorkItemStatus) ([]wf.WorkItem, error)
	ListByBotFn       func(ctx context.Context, botID string) ([]wf.WorkItem, error)
	ListStalledFn     func(ctx context.Context, cutoff time.Time) ([]wf.WorkItem, error)
	UpdateHeartbeatFn func(ctx context.Context, id string) error

	CreateCalls          []wf.WorkItem
	GetCalls             []string
	UpdateCalls          []wf.WorkItem
	UpdateHeartbeatCalls []string
}

func (m *WorkItemStore) Create(ctx context.Context, item wf.WorkItem) error {
	m.mu.Lock()
	m.CreateCalls = append(m.CreateCalls, item)
	fn := m.CreateFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, item)
	}
	return nil
}

func (m *WorkItemStore) Get(ctx context.Context, id string) (wf.WorkItem, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, id)
	fn := m.GetFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return wf.WorkItem{ID: id}, nil
}

func (m *WorkItemStore) Update(ctx context.Context, item wf.WorkItem) error {
	m.mu.Lock()
	m.UpdateCalls = append(m.UpdateCalls, item)
	fn := m.UpdateFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, item)
	}
	return nil
}

func (m *WorkItemStore) ListByStatus(ctx context.Context, status wf.WorkItemStatus) ([]wf.WorkItem, error) {
	if m.ListByStatusFn != nil {
		return m.ListByStatusFn(ctx, status)
	}
	return nil, nil
}

func (m *WorkItemStore) ListByBot(ctx context.Context, botID string) ([]wf.WorkItem, error) {
	if m.ListByBotFn != nil {
		return m.ListByBotFn(ctx, botID)
	}
	return nil, nil
}

func (m *WorkItemStore) ListStalled(ctx context.Context, cutoff time.Time) ([]wf.WorkItem, error) {
	if m.ListStalledFn != nil {
		return m.ListStalledFn(ctx, cutoff)
	}
	return nil, nil
}

func (m *WorkItemStore) UpdateHeartbeat(ctx context.Context, id string) error {
	m.mu.Lock()
	m.UpdateHeartbeatCalls = append(m.UpdateHeartbeatCalls, id)
	fn := m.UpdateHeartbeatFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return nil
}
