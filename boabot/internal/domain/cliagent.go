package domain

import (
	"context"
	"time"
)

// CLIAgentConfig configures a single CLIAgentRunner.Run invocation.
type CLIAgentConfig struct {
	// Binary is the CLI binary name or absolute path (e.g. "claude", "codex").
	Binary string
	// WorkDir is the working directory for the subprocess.
	WorkDir string
	// Args are additional command-line arguments passed before the instruction.
	Args []string
	// Model, when non-empty, selects the model (passed via dialect-specific flag).
	Model string
	// Timeout limits total subprocess execution time. Zero means use the runner's default (30 min).
	Timeout time.Duration
}

// CLIAgentRunner executes a CLI agent binary as a managed subprocess.
// The interface is satisfied by cliagent.SubprocessRunner in the infrastructure layer.
type CLIAgentRunner interface {
	// Run spawns the binary described by cfg, passes instruction as the prompt,
	// optionally streams lines from the stdin channel to the subprocess stdin,
	// and calls progress for each non-empty stdout line.
	//
	// It returns the accumulated stdout output and any error.
	// If stdin is nil, the subprocess receives no stdin input.
	// If progress is nil, output lines are accumulated but not forwarded.
	Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
		stdin <-chan string, progress func(line string)) (string, error)
}
