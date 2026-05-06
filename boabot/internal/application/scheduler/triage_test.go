package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/scheduler"
	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
	wfmocks "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow/mocks"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	notifmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification/mocks"
)

func makeTriageUC(store *wfmocks.WorkItemStore, notifier *notifmocks.NotificationSender) *scheduler.TriageUseCase {
	m := &metricsmocks.MetricsStore{}
	createUC := wf.NewCreateWorkItemUseCase(store, m)
	return scheduler.NewTriageUseCase(store, notifier, createUC, "arn:aws:sns:us-east-1:123:po-topic")
}

func TestTriageUseCase_BugLabel_DirectResolve(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "App crashes on login",
		Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriageDirectResolve {
		t.Fatalf("expected direct_resolve, got %s", decision)
	}
	if len(store.CreateCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(store.CreateCalls))
	}
	if store.CreateCalls[0].Priority != 1 {
		t.Fatalf("expected priority=1 for bug, got %d", store.CreateCalls[0].Priority)
	}
	if store.CreateCalls[0].Type != wf.WorkItemTypeBug {
		t.Fatalf("expected type=bug, got %s", store.CreateCalls[0].Type)
	}
}

func TestTriageUseCase_CriticalLabel_DirectResolve(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "Critical outage",
		Labels: []string{"critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriageDirectResolve {
		t.Fatalf("expected direct_resolve, got %s", decision)
	}
}

func TestTriageUseCase_EnhancementLabel_DevQueue(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "Add dark mode",
		Labels: []string{"enhancement"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriageDevQueue {
		t.Fatalf("expected dev_queue, got %s", decision)
	}
	if len(store.CreateCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(store.CreateCalls))
	}
}

func TestTriageUseCase_FeatureLabel_DevQueue(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "New feature request",
		Labels: []string{"feature"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriageDevQueue {
		t.Fatalf("expected dev_queue, got %s", decision)
	}
}

func TestTriageUseCase_QuestionLabel_POReview(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "How does X work?",
		Labels: []string{"question"},
		URL:    "https://github.com/org/repo/issues/42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriagePOReview {
		t.Fatalf("expected po_review, got %s", decision)
	}
	if len(store.CreateCalls) != 0 {
		t.Fatalf("expected no work item created for po_review, got %d", len(store.CreateCalls))
	}
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.RecipientARN != "arn:aws:sns:us-east-1:123:po-topic" {
		t.Fatalf("unexpected RecipientARN: %s", notifier.SendCalls[0].Notification.RecipientARN)
	}
}

func TestTriageUseCase_DiscussionLabel_POReview(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "RFC: new approach",
		Labels: []string{"discussion"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriagePOReview {
		t.Fatalf("expected po_review, got %s", decision)
	}
}

func TestTriageUseCase_NoLabels_DevQueue(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	decision, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "Unlabelled issue",
		Labels: []string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != scheduler.TriageDevQueue {
		t.Fatalf("expected dev_queue (default), got %s", decision)
	}
}

func TestTriageUseCase_BugLabel_CreateError(t *testing.T) {
	createErr := errors.New("db error")
	store := &wfmocks.WorkItemStore{
		CreateFn: func(_ context.Context, _ wf.WorkItem) error { return createErr },
	}
	notifier := &notifmocks.NotificationSender{}
	uc := makeTriageUC(store, notifier)

	_, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "Bug",
		Labels: []string{"bug"},
	})
	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestTriageUseCase_QuestionLabel_NotifyError(t *testing.T) {
	store := &wfmocks.WorkItemStore{}
	notifErr := errors.New("sns error")
	notifier := &notifmocks.NotificationSender{
		SendFn: func(_ notification.Notification) error { return notifErr },
	}
	uc := makeTriageUC(store, notifier)

	_, err := uc.Execute(context.Background(), scheduler.TriageInput{
		Title:  "Question",
		Labels: []string{"question"},
	})
	if err == nil {
		t.Fatal("expected error from notifier")
	}
}
