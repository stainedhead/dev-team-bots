package workflow_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

func TestDefaultWorkflow_ValidDefinition(t *testing.T) {
	def := workflow.DefaultWorkflow()

	if def.Name != "default" {
		t.Fatalf("expected name=default, got %s", def.Name)
	}
	if len(def.Steps) == 0 {
		t.Fatal("expected at least one step")
	}
}

func TestDefaultWorkflow_AllStepsHaveNames(t *testing.T) {
	def := workflow.DefaultWorkflow()
	for i, s := range def.Steps {
		if s.Name == "" {
			t.Fatalf("step[%d] has empty name", i)
		}
	}
}

func TestDefaultWorkflow_NonTerminalStepsHaveRoles(t *testing.T) {
	def := workflow.DefaultWorkflow()
	for _, s := range def.Steps {
		if s.NextStep != "" && s.RequiredRole == "" {
			t.Fatalf("step %q has next step but no required role", s.Name)
		}
	}
}

func TestDefaultWorkflow_TerminalStepIsDone(t *testing.T) {
	def := workflow.DefaultWorkflow()
	last := def.Steps[len(def.Steps)-1]
	if last.Name != "done" {
		t.Fatalf("expected last step to be 'done', got %s", last.Name)
	}
	if last.NextStep != "" {
		t.Fatalf("expected done to be terminal, but NextStep=%s", last.NextStep)
	}
}

func TestDefaultWorkflow_NotifyOnEntry(t *testing.T) {
	def := workflow.DefaultWorkflow()
	shouldNotify := map[string]bool{
		"implement":          true,
		"code_design_review": true,
		"confirmation":       true,
		"done":               true,
	}
	for _, s := range def.Steps {
		expected := shouldNotify[s.Name]
		if s.NotifyOnEntry != expected {
			t.Errorf("step %q: NotifyOnEntry=%v, want %v", s.Name, s.NotifyOnEntry, expected)
		}
	}
}

func TestDefaultWorkflow_AdvanceReachesDone(t *testing.T) {
	def := workflow.DefaultWorkflow()
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{def})

	current := def.Steps[0].Name
	visited := map[string]bool{}
	for {
		if visited[current] {
			t.Fatalf("cycle detected at step %s", current)
		}
		visited[current] = true

		next, err := r.Advance("default", current)
		if errors.Is(err, workflow.ErrNoNextStep) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error at step %s: %v", current, err)
		}
		current = next.Name
	}

	if current != "done" {
		t.Fatalf("expected to end at done, ended at %s", current)
	}
}

func TestDefaultWorkflow_ExpectedStepsPresent(t *testing.T) {
	def := workflow.DefaultWorkflow()
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{def})

	expectedOrder := []string{
		"backlog", "review", "document_prd", "review_prd", "spec",
		"implement", "code_design_review", "remediate", "confirmation", "analysis", "done",
	}

	current := expectedOrder[0]
	for i := 1; i < len(expectedOrder); i++ {
		next, err := r.Advance("default", current)
		if errors.Is(err, workflow.ErrNoNextStep) {
			if i != len(expectedOrder)-1 {
				t.Fatalf("got ErrNoNextStep at step %s, expected more steps", current)
			}
			break
		}
		if err != nil {
			t.Fatalf("unexpected error advancing from %s: %v", current, err)
		}
		if next.Name != expectedOrder[i] {
			t.Fatalf("after %s expected %s, got %s", current, expectedOrder[i], next.Name)
		}
		current = next.Name
	}
}

func TestDefaultWorkflow_RoleAssignments(t *testing.T) {
	def := workflow.DefaultWorkflow()
	r := workflow.NewDefaultRouter([]workflow.WorkflowDefinition{def})

	cases := []struct {
		step string
		role string
	}{
		{"backlog", "orchestrator"},
		{"review", "reviewer"},
		{"document_prd", "architect"},
		{"review_prd", "reviewer"},
		{"spec", "architect"},
		{"implement", "implementer"},
		{"code_design_review", "reviewer"},
		{"remediate", "implementer"},
		{"confirmation", "orchestrator"},
		{"analysis", "orchestrator"},
	}

	for _, tc := range cases {
		role, err := r.Assign("default", tc.step)
		if err != nil {
			t.Fatalf("step %s: unexpected error: %v", tc.step, err)
		}
		if role != tc.role {
			t.Errorf("step %s: expected role %s, got %s", tc.step, tc.role, role)
		}
	}
}
