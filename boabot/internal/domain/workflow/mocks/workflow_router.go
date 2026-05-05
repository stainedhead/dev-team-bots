// Package mocks provides hand-written test doubles for the workflow domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"

// WorkflowRouter is a hand-written mock of workflow.WorkflowRouter.
type WorkflowRouter struct {
	AdvanceFn func(item workflow.WorkItemID, step string) (workflow.WorkflowStep, error)
	AssignFn  func(item workflow.WorkItemID) (workflow.BotRole, error)

	AdvanceCalls []AdvanceCall
	AssignCalls  []AssignCall
}

// AdvanceCall records arguments passed to Advance.
type AdvanceCall struct {
	Item workflow.WorkItemID
	Step string
}

// AssignCall records arguments passed to Assign.
type AssignCall struct {
	Item workflow.WorkItemID
}

func (m *WorkflowRouter) Advance(item workflow.WorkItemID, step string) (workflow.WorkflowStep, error) {
	m.AdvanceCalls = append(m.AdvanceCalls, AdvanceCall{Item: item, Step: step})
	if m.AdvanceFn != nil {
		return m.AdvanceFn(item, step)
	}
	return workflow.WorkflowStep{}, nil
}

func (m *WorkflowRouter) Assign(item workflow.WorkItemID) (workflow.BotRole, error) {
	m.AssignCalls = append(m.AssignCalls, AssignCall{Item: item})
	if m.AssignFn != nil {
		return m.AssignFn(item)
	}
	return "", nil
}
