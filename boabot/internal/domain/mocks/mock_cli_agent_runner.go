package mocks

import (
	"context"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// MockCLIAgentRunner is a hand-written test double for domain.CLIAgentRunner.
// Set RunFn to control the mock's behaviour.
type MockCLIAgentRunner struct {
	RunFn func(ctx context.Context, cfg domain.CLIAgentConfig, instruction string,
		stdin <-chan string, progress func(line string)) (string, error)

	// RunCalls records each Run invocation for assertion in tests.
	RunCalls []CLIAgentRunCall
}

// CLIAgentRunCall captures the arguments of a single Run invocation.
type CLIAgentRunCall struct {
	Cfg         domain.CLIAgentConfig
	Instruction string
}

// Run implements domain.CLIAgentRunner.
func (m *MockCLIAgentRunner) Run(
	ctx context.Context,
	cfg domain.CLIAgentConfig,
	instruction string,
	stdin <-chan string,
	progress func(line string),
) (string, error) {
	m.RunCalls = append(m.RunCalls, CLIAgentRunCall{Cfg: cfg, Instruction: instruction})
	if m.RunFn != nil {
		return m.RunFn(ctx, cfg, instruction, stdin, progress)
	}
	return "", nil
}
