package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
	wfmocks "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow/mocks"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
)

func TestCreateWorkItemUseCase_HappyPath(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewCreateWorkItemUseCase(store, m)

	item, err := uc.Execute(context.Background(), "My Feature", "desc", "default", wf.WorkItemTypeFeature, 2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if item.Title != "My Feature" {
		t.Fatalf("expected title=My Feature, got %s", item.Title)
	}
	if item.WorkflowStep != "backlog" {
		t.Fatalf("expected step=backlog, got %s", item.WorkflowStep)
	}
	if item.Status != wf.WorkItemStatusBacklog {
		t.Fatalf("expected status=backlog, got %s", item.Status)
	}
	if item.Version != 1 {
		t.Fatalf("expected version=1, got %d", item.Version)
	}
	if item.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
	if item.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero UpdatedAt")
	}
	if len(store.CreateCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(store.CreateCalls))
	}
	if len(m.RecordCalls) != 1 {
		t.Fatalf("expected 1 metric event, got %d", len(m.RecordCalls))
	}
	if m.RecordCalls[0].Event.EventType != "item_created" {
		t.Fatalf("expected event_type=item_created, got %s", m.RecordCalls[0].Event.EventType)
	}
}

func TestCreateWorkItemUseCase_EmptyTitle(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	m := &metricsmocks.MetricsStore{}
	uc := wf.NewCreateWorkItemUseCase(store, m)

	_, err := uc.Execute(context.Background(), "   ", "desc", "default", wf.WorkItemTypeFeature, 1, nil)
	if !errors.Is(err, wf.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if len(store.CreateCalls) != 0 {
		t.Fatal("expected no Create calls for invalid input")
	}
}

func TestCreateWorkItemUseCase_StoreError(t *testing.T) {
	storeErr := errors.New("db error")
	store := &wfmocks.WorkItemStore{
		CreateFn: func(_ context.Context, _ wf.WorkItem) error { return storeErr },
	}
	m := &metricsmocks.MetricsStore{}
	uc := wf.NewCreateWorkItemUseCase(store, m)

	_, err := uc.Execute(context.Background(), "Title", "desc", "default", wf.WorkItemTypeFeature, 1, nil)
	if err == nil {
		t.Fatal("expected error from store")
	}
	if len(m.RecordCalls) != 0 {
		t.Fatal("expected no metric events on store error")
	}
}

func TestCreateWorkItemUseCase_WithFutureStartAt(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	m := &metricsmocks.MetricsStore{}
	uc := wf.NewCreateWorkItemUseCase(store, m)

	future := time.Now().Add(24 * time.Hour)
	item, err := uc.Execute(context.Background(), "Scheduled", "desc", "default", wf.WorkItemTypeFeature, 1, &future)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.FutureStartAt == nil || !item.FutureStartAt.Equal(future) {
		t.Fatal("expected FutureStartAt to be set correctly")
	}
}

func TestCreateWorkItemUseCase_UniqueIDs(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	m := &metricsmocks.MetricsStore{}
	uc := wf.NewCreateWorkItemUseCase(store, m)

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		item, err := uc.Execute(context.Background(), "Title", "", "default", wf.WorkItemTypeFeature, 1, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ids[item.ID] {
			t.Fatalf("duplicate ID: %s", item.ID)
		}
		ids[item.ID] = true
	}
}
