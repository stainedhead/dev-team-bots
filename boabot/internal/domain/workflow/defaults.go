package workflow

// DefaultWorkflow returns the standard BaoBot development workflow.
// Steps progress: backlog → review → document_prd → review_prd → spec →
// implement → code_design_review → remediate → confirmation → analysis → done.
func DefaultWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Name: "default",
		Steps: []WorkflowStep{
			{
				Name:          "backlog",
				RequiredRole:  "orchestrator",
				NextStep:      "review",
				NotifyOnEntry: false,
			},
			{
				Name:          "review",
				RequiredRole:  "reviewer",
				NextStep:      "document_prd",
				NotifyOnEntry: false,
			},
			{
				Name:          "document_prd",
				RequiredRole:  "architect",
				NextStep:      "review_prd",
				NotifyOnEntry: false,
			},
			{
				Name:          "review_prd",
				RequiredRole:  "reviewer",
				NextStep:      "spec",
				NotifyOnEntry: false,
			},
			{
				Name:          "spec",
				RequiredRole:  "architect",
				NextStep:      "implement",
				NotifyOnEntry: false,
			},
			{
				Name:          "implement",
				RequiredRole:  "implementer",
				NextStep:      "code_design_review",
				NotifyOnEntry: true,
			},
			{
				Name:          "code_design_review",
				RequiredRole:  "reviewer",
				NextStep:      "remediate",
				NotifyOnEntry: true,
			},
			{
				Name:          "remediate",
				RequiredRole:  "implementer",
				NextStep:      "confirmation",
				NotifyOnEntry: false,
			},
			{
				Name:          "confirmation",
				RequiredRole:  "orchestrator",
				NextStep:      "analysis",
				NotifyOnEntry: true,
			},
			{
				Name:          "analysis",
				RequiredRole:  "orchestrator",
				NextStep:      "done",
				NotifyOnEntry: false,
			},
			{
				Name:          "done",
				RequiredRole:  "",
				NextStep:      "",
				NotifyOnEntry: true,
			},
		},
	}
}
