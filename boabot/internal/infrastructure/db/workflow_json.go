package db

import (
	"encoding/json"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

func marshalWorkflow(def workflow.WorkflowDefinition) (string, error) {
	b, err := json.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("db: marshal workflow: %w", err)
	}
	return string(b), nil
}

func unmarshalWorkflow(data string) (workflow.WorkflowDefinition, error) {
	var def workflow.WorkflowDefinition
	if err := json.Unmarshal([]byte(data), &def); err != nil {
		return workflow.WorkflowDefinition{}, fmt.Errorf("db: unmarshal workflow: %w", err)
	}
	return def, nil
}
