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

func TestStalledItemRecoveryUseCase_HappyPath(t *testing.T) {
	stalled := []wf.WorkItem{
		{ID: "s1", AssignedBotID: "bot-1", Status: wf.WorkItemStatusInProgress, Version: 2},
		{ID: "s2", AssignedBotID: "bot-2", Status: wf.WorkItemStatusInProgress, Version: 1},
	}
	var capturedCutoff time.Time

	store := &wfmocks.WorkItemStore{
		ListStalledFn: func(_ context.Context, cutoff time.Time) ([]wf.WorkItem, error) {
			capturedCutoff = cutoff
			return stalled, nil
		},
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewStalledItemRecoveryUseCase(store, m)
	recovered, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recovered) != 2 {
		t.Fatalf("expected 2 recovered items, got %d", len(recovered))
	}
	for _, item := range recovered {
		if item.AssignedBotID != "" {
			t.Errorf("expected AssignedBotID cleared, got %s for %s", item.AssignedBotID, item.ID)
		}
		if item.Status != wf.WorkItemStatusBacklog {
			t.Errorf("expected status=backlog for %s, got %s", item.ID, item.Status)
		}
	}
	if len(store.UpdateCalls) != 2 {
		t.Fatalf("expected 2 Update calls, got %d", len(store.UpdateCalls))
	}
	if len(m.RecordCalls) != 2 {
		t.Fatalf("expected 2 metric events, got %d", len(m.RecordCalls))
	}
	for _, call := range m.RecordCalls {
		if call.Event.EventType != "item_requeued" {
			t.Errorf("expected item_requeued, got %s", call.Event.EventType)
		}
	}

	// Cutoff should be approximately 5 minutes ago.
	expectedCutoff := time.Now().Add(-5 * time.Minute)
	diff := expectedCutoff.Sub(capturedCutoff)
	if diff < -time.Second || diff > time.Second {
		t.Fatalf("cutoff %v not within 1s of expected %v", capturedCutoff, expectedCutoff)
	}
}

func TestStalledItemRecoveryUseCase_NoStalledItems(t *testing.T) {
	store := &wfmocks.WorkItemStore{
		ListStalledFn: func(_ context.Context, _ time.Time) ([]wf.WorkItem, error) {
			return nil, nil
		},
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewStalledItemRecoveryUseCase(store, m)
	recovered, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("expected 0 recovered items, got %d", len(recovered))
	}
	if len(m.RecordCalls) != 0 {
		t.Fatal("expected no metric events when nothing is stalled")
	}
}

func TestStalledItemRecoveryUseCase_ListStalledError(t *testing.T) {
	listErr := errors.New("db error")
	store := &wfmocks.WorkItemStore{
		ListStalledFn: func(_ context.Context, _ time.Time) ([]wf.WorkItem, error) {
			return nil, listErr
		},
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewStalledItemRecoveryUseCase(store, m)
	_, err := uc.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error from ListStalled")
	}
}

func TestStalledItemRecoveryUseCase_UpdateError(t *testing.T) {
	stalled := []wf.WorkItem{
		{ID: "s1", AssignedBotID: "bot-1", Status: wf.WorkItemStatusInProgress, Version: 1},
	}
	updateErr := errors.New("update failed")

	store := &wfmocks.WorkItemStore{
		ListStalledFn: func(_ context.Context, _ time.Time) ([]wf.WorkItem, error) {
			return stalled, nil
		},
		UpdateFn: func(_ context.Context, _ wf.WorkItem) error { return updateErr },
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewStalledItemRecoveryUseCase(store, m)
	_, err := uc.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error from Update")
	}
}

func TestStalledItemRecoveryUseCase_VersionIncremented(t *testing.T) {
	stalled := []wf.WorkItem{
		{ID: "s3", Version: 5, Status: wf.WorkItemStatusInProgress},
	}
	store := &wfmocks.WorkItemStore{
		ListStalledFn: func(_ context.Context, _ time.Time) ([]wf.WorkItem, error) {
			return stalled, nil
		},
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewStalledItemRecoveryUseCase(store, m)
	recovered, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recovered) == 0 {
		t.Fatal("expected at least one recovered item")
	}
	if recovered[0].Version != 6 {
		t.Fatalf("expected Version=6, got %d", recovered[0].Version)
	}
}
