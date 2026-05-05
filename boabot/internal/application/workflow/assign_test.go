package workflow_test

import (
	"context"
	"errors"
	"testing"

	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
	wfmocks "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow/mocks"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
)

func TestAssignBotUseCase_HappyPath(t *testing.T) {
	item := makeItem("item-a", "backlog")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAssignBotUseCase(store, m)
	result, err := uc.Execute(context.Background(), "item-a", "bot-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssignedBotID != "bot-42" {
		t.Fatalf("expected AssignedBotID=bot-42, got %s", result.AssignedBotID)
	}
	if len(store.UpdateCalls) != 1 {
		t.Fatalf("expected 1 Update call, got %d", len(store.UpdateCalls))
	}
	if len(m.RecordCalls) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(m.RecordCalls))
	}
	if m.RecordCalls[0].Event.EventType != "bot_assigned" {
		t.Fatalf("expected bot_assigned metric, got %s", m.RecordCalls[0].Event.EventType)
	}
	if string(m.RecordCalls[0].Event.BotID) != "bot-42" {
		t.Fatalf("expected BotID=bot-42 in metric, got %s", m.RecordCalls[0].Event.BotID)
	}
}

func TestAssignBotUseCase_GetError(t *testing.T) {
	storeErr := errors.New("not found")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return wf.WorkItem{}, storeErr },
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAssignBotUseCase(store, m)
	_, err := uc.Execute(context.Background(), "missing", "bot-1")
	if err == nil {
		t.Fatal("expected error from store.Get")
	}
}

func TestAssignBotUseCase_UpdateError(t *testing.T) {
	item := makeItem("item-b", "backlog")
	updateErr := errors.New("update failed")
	store := &wfmocks.WorkItemStore{
		GetFn:    func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
		UpdateFn: func(_ context.Context, _ wf.WorkItem) error { return updateErr },
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAssignBotUseCase(store, m)
	_, err := uc.Execute(context.Background(), "item-b", "bot-1")
	if err == nil {
		t.Fatal("expected error from store.Update")
	}
	if len(m.RecordCalls) != 0 {
		t.Fatal("expected no metrics on Update error")
	}
}

func TestAssignBotUseCase_VersionIncremented(t *testing.T) {
	item := makeItem("item-c", "backlog")
	item.Version = 3
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAssignBotUseCase(store, m)
	result, err := uc.Execute(context.Background(), "item-c", "bot-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != 4 {
		t.Fatalf("expected Version=4, got %d", result.Version)
	}
}
