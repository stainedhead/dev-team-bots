package mocks

import (
	domainwf "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

// AdvanceCall records a single call to Advance.
type AdvanceCall struct {
	WorkflowName string
	CurrentStep  string
}

// WorkflowAdvancer is a mock of workflow.WorkflowAdvancer.
type WorkflowAdvancer struct {
	AdvanceFn    func(workflowName, currentStep string) (domainwf.WorkflowStep, error)
	AdvanceCalls []AdvanceCall
}

func (m *WorkflowAdvancer) Advance(workflowName, currentStep string) (domainwf.WorkflowStep, error) {
	m.AdvanceCalls = append(m.AdvanceCalls, AdvanceCall{WorkflowName: workflowName, CurrentStep: currentStep})
	if m.AdvanceFn != nil {
		return m.AdvanceFn(workflowName, currentStep)
	}
	return domainwf.WorkflowStep{}, nil
}
