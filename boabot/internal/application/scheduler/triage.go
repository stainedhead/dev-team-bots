package scheduler

import (
	"context"
	"fmt"

	wf "github.com/stainedhead/dev-team-bots/boabot/internal/application/workflow"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
)

// TriageDecision is the routing outcome of the triage process.
type TriageDecision string

const (
	// TriageDirectResolve creates a high-priority work item for immediate implementation.
	TriageDirectResolve TriageDecision = "direct_resolve"

	// TriageDevQueue creates a work item and places it in the standard dev workflow.
	TriageDevQueue TriageDecision = "dev_queue"

	// TriagePOReview notifies the PO for review; no work item is created.
	TriagePOReview TriageDecision = "po_review"
)

// TriageInput carries the incoming issue or PR data to be triaged.
type TriageInput struct {
	// Source is the origin system: "github_issue", "github_pr", "direct".
	Source string

	// Title is the short description.
	Title string

	// Description is the full body.
	Description string

	// URL links back to the source system.
	URL string

	// Labels are the classification tags applied in the source system.
	Labels []string
}

// TriageUseCase evaluates an incoming issue or PR and routes it accordingly.
type TriageUseCase struct {
	workItemStore wf.WorkItemStore
	notifSender   notification.NotificationSender
	createUC      *wf.CreateWorkItemUseCase
	poNotifARN    string
}

// NewTriageUseCase constructs a TriageUseCase.
func NewTriageUseCase(
	store wf.WorkItemStore,
	notifSender notification.NotificationSender,
	createUC *wf.CreateWorkItemUseCase,
	poNotifARN string,
) *TriageUseCase {
	return &TriageUseCase{
		workItemStore: store,
		notifSender:   notifSender,
		createUC:      createUC,
		poNotifARN:    poNotifARN,
	}
}

// Execute applies rule-based triage to the input and returns the routing
// decision. Label precedence: bug/critical > question/discussion > enhancement/feature > default.
func (uc *TriageUseCase) Execute(ctx context.Context, input TriageInput) (TriageDecision, error) {
	labels := labelSet(input.Labels)

	switch {
	case labels["bug"] || labels["critical"]:
		return uc.handleDirectResolve(ctx, input)

	case labels["question"] || labels["discussion"]:
		return uc.handlePOReview(input)

	default:
		// enhancement, feature, or anything else → dev queue
		return uc.handleDevQueue(ctx, input, 2)
	}
}

func (uc *TriageUseCase) handleDirectResolve(ctx context.Context, input TriageInput) (TriageDecision, error) {
	_, err := uc.createUC.Execute(ctx, input.Title, input.Description, "default", wf.WorkItemTypeBug, 1, nil)
	if err != nil {
		return "", fmt.Errorf("triage direct_resolve: %w", err)
	}
	return TriageDirectResolve, nil
}

func (uc *TriageUseCase) handleDevQueue(ctx context.Context, input TriageInput, priority int) (TriageDecision, error) {
	_, err := uc.createUC.Execute(ctx, input.Title, input.Description, "default", wf.WorkItemTypeFeature, priority, nil)
	if err != nil {
		return "", fmt.Errorf("triage dev_queue: %w", err)
	}
	return TriageDevQueue, nil
}

func (uc *TriageUseCase) handlePOReview(input TriageInput) (TriageDecision, error) {
	n := notification.Notification{
		Type:         notification.NotifBlocked,
		RecipientARN: uc.poNotifARN,
		Subject:      fmt.Sprintf("PO Review Required: %s", input.Title),
		Body:         fmt.Sprintf("Source: %s\nURL: %s\n\n%s", input.Source, input.URL, input.Description),
		Metadata: map[string]string{
			"source": input.Source,
			"url":    input.URL,
		},
	}
	if err := uc.notifSender.Send(n); err != nil {
		return "", fmt.Errorf("triage po_review notify: %w", err)
	}
	return TriagePOReview, nil
}

// labelSet converts a slice of labels into a lowercase-keyed lookup set.
func labelSet(labels []string) map[string]bool {
	set := make(map[string]bool, len(labels))
	for _, l := range labels {
		set[l] = true
	}
	return set
}
