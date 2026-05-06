// Package workflow defines the domain types and interfaces for multi-step
// workflow routing used by the orchestrator to advance work items through
// named sequences of steps.
package workflow

import "errors"

// BotRole is the named role required to handle a workflow step.
type BotRole string

// WorkItemID is the unique identifier of a board work item.
type WorkItemID string

// ErrNoNextStep is returned by Advance when the supplied step is the terminal
// step in the workflow (no further progression possible).
var ErrNoNextStep = errors.New("workflow: no next step")

// ErrStepNotFound is returned when the requested step name does not exist in
// the workflow definition.
var ErrStepNotFound = errors.New("workflow: step not found")

// ErrWorkflowNotFound is returned when no workflow is registered for an item.
var ErrWorkflowNotFound = errors.New("workflow: no workflow registered for item")

// WorkflowStep describes a single step in a named workflow.
type WorkflowStep struct {
	// Name is the canonical step identifier (e.g. "design", "implement").
	Name string

	// RequiredRole is the bot role that must handle this step.
	RequiredRole BotRole

	// NextStep is the Name of the step that follows on completion.
	// An empty string indicates this is the terminal step.
	NextStep string

	// NotifyOnEntry triggers a user-facing notification when the item enters
	// this step.
	NotifyOnEntry bool
}

// WorkflowDefinition is a named, ordered collection of workflow steps.
type WorkflowDefinition struct {
	// Name uniquely identifies the workflow (e.g. "feature", "bugfix").
	Name string

	// Steps is the ordered list of steps; index 0 is the initial step.
	Steps []WorkflowStep
}

// WorkflowRouter advances work items through a WorkflowDefinition.
type WorkflowRouter interface {
	// Advance returns the next WorkflowStep for item after completing step.
	// Returns ErrNoNextStep if step is the terminal step.
	Advance(item WorkItemID, step string) (WorkflowStep, error)

	// Assign returns the BotRole responsible for the current step of item.
	Assign(item WorkItemID) (BotRole, error)
}
