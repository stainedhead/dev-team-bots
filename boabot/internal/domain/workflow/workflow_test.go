package workflow_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow/mocks"
)

func TestWorkflowStep_Fields(t *testing.T) {
	s := workflow.WorkflowStep{
		Name:          "implement",
		RequiredRole:  "developer",
		NextStep:      "review",
		NotifyOnEntry: true,
	}
	if s.Name != "implement" {
		t.Fatalf("expected Name=implement got %s", s.Name)
	}
	if s.RequiredRole != "developer" {
		t.Fatalf("expected RequiredRole=developer got %s", s.RequiredRole)
	}
	if s.NextStep != "review" {
		t.Fatalf("expected NextStep=review got %s", s.NextStep)
	}
	if !s.NotifyOnEntry {
		t.Fatal("expected NotifyOnEntry=true")
	}
}

func TestWorkflowDefinition_Fields(t *testing.T) {
	d := workflow.WorkflowDefinition{
		Name: "feature",
		Steps: []workflow.WorkflowStep{
			{Name: "design", RequiredRole: "architect", NextStep: "implement"},
			{Name: "implement", RequiredRole: "developer", NextStep: "review"},
			{Name: "review", RequiredRole: "reviewer", NextStep: ""},
		},
	}
	if d.Name != "feature" {
		t.Fatalf("expected Name=feature got %s", d.Name)
	}
	if len(d.Steps) != 3 {
		t.Fatalf("expected 3 steps got %d", len(d.Steps))
	}
}

func TestWorkflowRouter_MockAdvance(t *testing.T) {
	m := &mocks.WorkflowRouter{
		AdvanceFn: func(item workflow.WorkItemID, step string) (workflow.WorkflowStep, error) {
			if step == "design" {
				return workflow.WorkflowStep{Name: "implement", RequiredRole: "developer"}, nil
			}
			return workflow.WorkflowStep{}, workflow.ErrNoNextStep
		},
	}

	got, err := m.Advance("item-1", "design")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "implement" {
		t.Fatalf("expected implement got %s", got.Name)
	}
	if len(m.AdvanceCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.AdvanceCalls))
	}
}

func TestWorkflowRouter_MockAdvance_ErrNoNextStep(t *testing.T) {
	m := &mocks.WorkflowRouter{
		AdvanceFn: func(_ workflow.WorkItemID, _ string) (workflow.WorkflowStep, error) {
			return workflow.WorkflowStep{}, workflow.ErrNoNextStep
		},
	}

	_, err := m.Advance("item-1", "review")
	if !errors.Is(err, workflow.ErrNoNextStep) {
		t.Fatalf("expected ErrNoNextStep got %v", err)
	}
}

func TestWorkflowRouter_MockAssign(t *testing.T) {
	m := &mocks.WorkflowRouter{
		AssignFn: func(_ workflow.WorkItemID) (workflow.BotRole, error) {
			return "developer", nil
		},
	}

	role, err := m.Assign("item-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "developer" {
		t.Fatalf("expected developer got %s", role)
	}
	if len(m.AssignCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.AssignCalls))
	}
}

func TestWorkflowRouter_MockAssign_Error(t *testing.T) {
	m := &mocks.WorkflowRouter{
		AssignFn: func(_ workflow.WorkItemID) (workflow.BotRole, error) {
			return "", workflow.ErrWorkflowNotFound
		},
	}

	_, err := m.Assign("item-3")
	if !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("expected ErrWorkflowNotFound got %v", err)
	}
}

func TestSentinelErrors(t *testing.T) {
	if workflow.ErrNoNextStep == nil {
		t.Fatal("ErrNoNextStep must not be nil")
	}
	if workflow.ErrStepNotFound == nil {
		t.Fatal("ErrStepNotFound must not be nil")
	}
	if workflow.ErrWorkflowNotFound == nil {
		t.Fatal("ErrWorkflowNotFound must not be nil")
	}
}
