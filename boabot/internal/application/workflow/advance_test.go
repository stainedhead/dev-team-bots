package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
	wfmocks "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow/mocks"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
	notifmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification/mocks"
	domainwf "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

func makeItem(id, step string) wf.WorkItem {
	return wf.WorkItem{
		ID:           id,
		Title:        "test item",
		WorkflowName: "default",
		WorkflowStep: step,
		Status:       wf.WorkItemStatusInProgress,
		Version:      1,
	}
}

func TestAdvanceWorkflowUseCase_HappyPath(t *testing.T) {
	item := makeItem("item-1", "backlog")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(wfName, step string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "review", RequiredRole: "reviewer"}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)

	result, err := uc.Execute(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WorkflowStep != "review" {
		t.Fatalf("expected step=review, got %s", result.WorkflowStep)
	}
	if result.Status != wf.WorkItemStatusInProgress {
		t.Fatalf("expected status=in_progress, got %s", result.Status)
	}
	if len(store.UpdateCalls) != 1 {
		t.Fatalf("expected 1 Update call, got %d", len(store.UpdateCalls))
	}
	if len(m.RecordCalls) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(m.RecordCalls))
	}
	if m.RecordCalls[0].Event.EventType != "step_advanced" {
		t.Fatalf("expected step_advanced, got %s", m.RecordCalls[0].Event.EventType)
	}
	if m.RecordCalls[0].Event.StepName != "review" {
		t.Fatalf("expected step=review in metric, got %s", m.RecordCalls[0].Event.StepName)
	}
}

func TestAdvanceWorkflowUseCase_NotifyOnEntry(t *testing.T) {
	item := makeItem("item-2", "implement")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "code_design_review", NotifyOnEntry: true}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "item-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.SendCalls))
	}
}

func TestAdvanceWorkflowUseCase_NoNotifyOnEntry(t *testing.T) {
	item := makeItem("item-3", "backlog")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "review", NotifyOnEntry: false}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "item-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(notifier.SendCalls))
	}
}

func TestAdvanceWorkflowUseCase_TerminalStep_Completes(t *testing.T) {
	started := time.Now().Add(-30 * time.Minute)
	item := makeItem("item-4", "done")
	item.StartedAt = &started

	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{}, domainwf.ErrNoNextStep
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	result, err := uc.Execute(context.Background(), "item-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != wf.WorkItemStatusComplete {
		t.Fatalf("expected status=complete, got %s", result.Status)
	}
	if len(m.RecordCalls) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(m.RecordCalls))
	}
	if m.RecordCalls[0].Event.EventType != "item_completed" {
		t.Fatalf("expected item_completed, got %s", m.RecordCalls[0].Event.EventType)
	}
	if m.RecordCalls[0].Event.DurationMinutes <= 0 {
		t.Fatal("expected positive DurationMinutes")
	}
}

func TestAdvanceWorkflowUseCase_ErrConflict_RetrySucceeds(t *testing.T) {
	item := makeItem("item-5", "backlog")
	callCount := 0

	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
		UpdateFn: func(_ context.Context, _ wf.WorkItem) error {
			callCount++
			if callCount == 1 {
				return wf.ErrConflict
			}
			return nil
		},
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "review"}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "item-5")
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	// Update is called twice (once conflict, once retry).
	if callCount != 2 {
		t.Fatalf("expected 2 Update calls, got %d", callCount)
	}
}

func TestAdvanceWorkflowUseCase_GetError(t *testing.T) {
	storeErr := errors.New("db error")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return wf.WorkItem{}, storeErr },
	}
	router := &wfmocks.WorkflowAdvancer{}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestAdvanceWorkflowUseCase_RouterError(t *testing.T) {
	item := makeItem("item-6", "backlog")
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{}, domainwf.ErrWorkflowNotFound
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "item-6")
	if err == nil {
		t.Fatal("expected error from router")
	}
}

func TestAdvanceWorkflowUseCase_ErrConflict_RetryFetchFails(t *testing.T) {
	item := makeItem("item-8", "backlog")
	fetchErr := errors.New("db down")
	getCount := 0

	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) {
			getCount++
			if getCount > 1 {
				return wf.WorkItem{}, fetchErr
			}
			return item, nil
		},
		UpdateFn: func(_ context.Context, _ wf.WorkItem) error {
			return wf.ErrConflict
		},
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "review"}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	_, err := uc.Execute(context.Background(), "item-8")
	if err == nil {
		t.Fatal("expected error when retry fetch fails")
	}
}

func TestAdvanceWorkflowUseCase_SetsStartedAt_OnFirstAdvance(t *testing.T) {
	item := makeItem("item-7", "backlog")
	// StartedAt is nil initially.
	store := &wfmocks.WorkItemStore{
		GetFn: func(_ context.Context, _ string) (wf.WorkItem, error) { return item, nil },
	}
	router := &wfmocks.WorkflowAdvancer{
		AdvanceFn: func(_, _ string) (domainwf.WorkflowStep, error) {
			return domainwf.WorkflowStep{Name: "review"}, nil
		},
	}
	notifier := &notifmocks.NotificationSender{}
	m := &metricsmocks.MetricsStore{}

	uc := wf.NewAdvanceWorkflowUseCase(store, router, notifier, m)
	result, err := uc.Execute(context.Background(), "item-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StartedAt == nil {
		t.Fatal("expected StartedAt to be set on first advance")
	}
}
