// Package workflow defines the domain types and interfaces for multi-step
// workflow routing.
package workflow

import "fmt"

// DefaultRouter is a pure-logic implementation of workflow routing.
// It looks up workflow definitions by name and advances steps by name.
type DefaultRouter struct {
	definitions map[string]WorkflowDefinition
}

// NewDefaultRouter constructs a DefaultRouter from a slice of WorkflowDefinition.
func NewDefaultRouter(defs []WorkflowDefinition) *DefaultRouter {
	m := make(map[string]WorkflowDefinition, len(defs))
	for _, d := range defs {
		m[d.Name] = d
	}
	return &DefaultRouter{definitions: m}
}

// Advance returns the next WorkflowStep after currentStep in the named workflow.
// Returns ErrStepNotFound if currentStep does not exist.
// Returns ErrNoNextStep if currentStep is the terminal step (NextStep == "").
func (r *DefaultRouter) Advance(workflowName, currentStep string) (WorkflowStep, error) {
	def, err := r.findDef(workflowName)
	if err != nil {
		return WorkflowStep{}, err
	}
	step, err := r.findStep(def, currentStep)
	if err != nil {
		return WorkflowStep{}, err
	}
	if step.NextStep == "" {
		return WorkflowStep{}, ErrNoNextStep
	}
	next, err := r.findStep(def, step.NextStep)
	if err != nil {
		return WorkflowStep{}, fmt.Errorf("workflow: next step %q not found: %w", step.NextStep, ErrStepNotFound)
	}
	return next, nil
}

// StepForRole returns the first step in the workflow that requires the given role.
// Returns ErrStepNotFound if no step matches.
func (r *DefaultRouter) StepForRole(workflowName, role string) (WorkflowStep, error) {
	def, err := r.findDef(workflowName)
	if err != nil {
		return WorkflowStep{}, err
	}
	for _, s := range def.Steps {
		if string(s.RequiredRole) == role {
			return s, nil
		}
	}
	return WorkflowStep{}, fmt.Errorf("workflow: no step with role %q: %w", role, ErrStepNotFound)
}

// Assign returns the BotRole required for the given stepName in the named workflow.
// Returns ErrStepNotFound if the step does not exist.
func (r *DefaultRouter) Assign(workflowName, stepName string) (string, error) {
	def, err := r.findDef(workflowName)
	if err != nil {
		return "", err
	}
	step, err := r.findStep(def, stepName)
	if err != nil {
		return "", err
	}
	return string(step.RequiredRole), nil
}

func (r *DefaultRouter) findDef(workflowName string) (WorkflowDefinition, error) {
	def, ok := r.definitions[workflowName]
	if !ok {
		return WorkflowDefinition{}, fmt.Errorf("workflow %q: %w", workflowName, ErrWorkflowNotFound)
	}
	return def, nil
}

func (r *DefaultRouter) findStep(def WorkflowDefinition, stepName string) (WorkflowStep, error) {
	for _, s := range def.Steps {
		if s.Name == stepName {
			return s, nil
		}
	}
	return WorkflowStep{}, fmt.Errorf("workflow: step %q not found: %w", stepName, ErrStepNotFound)
}
