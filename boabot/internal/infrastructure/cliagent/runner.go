// Package cliagent provides a domain.CLIAgentRunner implementation that
// spawns a CLI binary as a managed subprocess.
package cliagent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const defaultTimeout = 30 * time.Minute

// SubprocessRunner implements domain.CLIAgentRunner by spawning a binary as a
// managed subprocess. It is safe for concurrent use — each Run call is independent.
type SubprocessRunner struct{}

// New returns a SubprocessRunner.
func New() *SubprocessRunner { return &SubprocessRunner{} }

// Run spawns cfg.Binary with cfg.Args, appending instruction as a final argument
// when instruction is non-empty. Stdout is streamed line-by-line; each non-empty
// line is passed to progress (if non-nil) and accumulated into the return string.
// If stdin is non-nil, lines from the channel are written to the subprocess stdin
// until the channel is closed or ctx is cancelled.
//
// On context cancellation or timeout, SIGTERM is sent; after a 5-second grace
// period, the process is force-killed (via WaitDelay on Go 1.20+).
func (r *SubprocessRunner) Run(
	ctx context.Context,
	cfg domain.CLIAgentConfig,
	instruction string,
	stdin <-chan string,
	progress func(line string),
) (string, error) {
	// Verify binary before attempting to start.
	if _, err := exec.LookPath(cfg.Binary); err != nil {
		return "", fmt.Errorf("cliagent: binary %q not found: %w", cfg.Binary, err)
	}

	// Apply timeout.
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build arg list: configured args, then instruction (if provided).
	args := make([]string, 0, len(cfg.Args)+1)
	args = append(args, cfg.Args...)
	if instruction != "" {
		args = append(args, instruction)
	}

	cmd := exec.CommandContext(runCtx, cfg.Binary, args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	// SIGTERM on cancel, then SIGKILL after WaitDelay.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second

	// Capture stderr for error reporting.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	// Set up stdout pipe for line-by-line reading.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("cliagent: create stdout pipe: %w", err)
	}

	// Wire stdin if provided.
	if stdin != nil {
		stdinPipe, stdinErr := cmd.StdinPipe()
		if stdinErr != nil {
			return "", fmt.Errorf("cliagent: create stdin pipe: %w", stdinErr)
		}
		go drainStdin(runCtx, stdin, stdinPipe)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("cliagent: start subprocess: %w", err)
	}

	// Accumulate stdout in a goroutine.
	var accumulated strings.Builder
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if progress != nil {
				progress(line)
			}
			accumulated.WriteString(line)
			accumulated.WriteString("\n")
		}
	}()

	waitErr := cmd.Wait()
	// Always wait for the scanner goroutine to drain (pipe is closed by Wait).
	<-scanDone

	if waitErr != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("cliagent: subprocess timed out or context cancelled: %w", runCtx.Err())
		}
		stderr := strings.TrimSpace(stderrBuf.String())
		if stderr != "" {
			return "", fmt.Errorf("cliagent: subprocess exited with error: %w; stderr: %s", waitErr, stderr)
		}
		return "", fmt.Errorf("cliagent: subprocess exited with error: %w", waitErr)
	}

	return strings.TrimRight(accumulated.String(), "\n"), nil
}

// drainStdin reads from stdinCh and writes each line to w until the channel is
// closed or ctx is done. It always closes w when it exits.
func drainStdin(ctx context.Context, stdinCh <-chan string, w io.WriteCloser) {
	defer w.Close()
	for {
		select {
		case line, ok := <-stdinCh:
			if !ok {
				return // channel closed
			}
			_, _ = io.WriteString(w, line+"\n")
		case <-ctx.Done():
			return
		}
	}
}
