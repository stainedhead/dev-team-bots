package workflow_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

func makeTestWorkflow() workflow.WorkflowDefinition {
	return workflow.WorkflowDefinition{
		Name: "test",
		Steps: []workflow.WorkflowStep{
			{Name: "a", RequiredRole: "orchestrator", NextStep: "b"},
			{Name: "b", RequiredRole: "reviewer", NextStep: "c"},
			{Name: "c", RequiredRole: "implementer", NextStep: ""},
		},
	}
}

func TestDefaultRouter_Advance_HappyPath(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	got, err := r.Advance("test", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "b" {
		t.Fatalf("expected b, got %s", got.Name)
	}

	got, err = r.Advance("test", "b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "c" {
		t.Fatalf("expected c, got %s", got.Name)
	}
}

func TestDefaultRouter_Advance_TerminalStep(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.Advance("test", "c")
	if !errors.Is(err, workflow.ErrNoNextStep) {
		t.Fatalf("expected ErrNoNextStep, got %v", err)
	}
}

func TestDefaultRouter_Advance_UnknownStep(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.Advance("test", "zzz")
	if !errors.Is(err, workflow.ErrStepNotFound) {
		t.Fatalf("expected ErrStepNotFound, got %v", err)
	}
}

func TestDefaultRouter_Advance_UnknownWorkflow(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.Advance("no-such-workflow", "a")
	if !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestDefaultRouter_Assign_ReturnsRole(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	role, err := r.Assign("test", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "orchestrator" {
		t.Fatalf("expected orchestrator, got %s", role)
	}
}

func TestDefaultRouter_Assign_UnknownStep(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.Assign("test", "zzz")
	if !errors.Is(err, workflow.ErrStepNotFound) {
		t.Fatalf("expected ErrStepNotFound, got %v", err)
	}
}

func TestDefaultRouter_Assign_UnknownWorkflow(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.Assign("no-such", "a")
	if !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestDefaultRouter_StepForRole_Found(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	step, err := r.StepForRole("test", "reviewer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name != "b" {
		t.Fatalf("expected b, got %s", step.Name)
	}
}

func TestDefaultRouter_StepForRole_NotFound(t *testing.T) {
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow()})

	_, err := r.StepForRole("test", "no-such-role")
	if !errors.Is(err, workflow.ErrStepNotFound) {
		t.Fatalf("expected ErrStepNotFound, got %v", err)
	}
}

func TestDefaultRouter_MultipleWorkflows(t *testing.T) {
	w2 := workflow.WorkflowDefinition{
		Name: "other",
		Steps: []workflow.WorkflowStep{
			{Name: "x", RequiredRole: "admin", NextStep: ""},
		},
	}
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{makeTestWorkflow(), w2})

	role, err := r.Assign("other", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "admin" {
		t.Fatalf("expected admin, got %s", role)
	}
}

// TestDefaultRouter_Advance_InvalidNextStep verifies that Advance returns an error
// when the current step's NextStep name does not match any step in the workflow.
func TestDefaultRouter_Advance_InvalidNextStep(t *testing.T) {
	wf := workflow.WorkflowDefinition{
		Name: "wf-bad-next",
		Steps: []workflow.WorkflowStep{
			{Name: "step1", RequiredRole: "dev", NextStep: "does-not-exist"},
		},
	}
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{wf})
	_, err := r.Advance("wf-bad-next", "step1")
	if err == nil {
		t.Fatal("expected error for non-existent NextStep")
	}
}

// TestDefaultRouter_StepForRole_UnknownWorkflow verifies that StepForRole returns
// an error when the workflow name is not registered.
func TestDefaultRouter_StepForRole_UnknownWorkflow(t *testing.T) {
	r := workflow.NewDefaultRouter(nil)
	_, err := r.StepForRole("no-such-workflow", "dev")
	if err == nil {
		t.Fatal("expected error for unknown workflow in StepForRole")
	}
}
