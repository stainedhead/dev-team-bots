// Package codeagent provides a domain.ModelProvider that runs a code-agent CLI
// (claude, codex, or similar) as a subprocess and collects its output.
package codeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const defaultTimeout = 10 * time.Minute

// Dialect identifies which CLI tool and output format the Provider targets.
type Dialect string

const (
	// DialectClaude targets the Claude Code CLI (`claude`). It passes
	// --output-format=stream-json and parses streaming JSON events.
	DialectClaude Dialect = "claude"
	// DialectCodex targets the OpenAI Codex CLI (`codex`). It passes -q and
	// --approval-mode=full-auto and reads plain-text stdout.
	DialectCodex Dialect = "codex"
)

// Option configures a Provider.
type Option func(*Provider)

// WithTimeout sets the subprocess execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(p *Provider) {
		p.timeout = d
	}
}

// WithDialect selects which CLI dialect the Provider uses.
func WithDialect(d Dialect) Option {
	return func(p *Provider) {
		p.dialect = d
	}
}

// Provider implements domain.ModelProvider by running a code-agent CLI as a
// subprocess and collecting its output.
type Provider struct {
	// binaryPath is the absolute path (or name resolvable on PATH) of the CLI binary.
	binaryPath string
	// workDir is the working directory for the subprocess.
	workDir string
	// timeout limits total subprocess execution time.
	timeout time.Duration
	// dialect selects the CLI tool and output-parsing strategy.
	dialect Dialect
}

// New constructs a Provider.
//
//   - binaryPath: path to the CLI binary (e.g. "claude", "codex", or an absolute path).
//   - workDir: working directory for the subprocess.
//   - opts: optional configuration overrides (WithDialect, WithTimeout).
func New(binaryPath, workDir string, opts ...Option) *Provider {
	p := &Provider{
		binaryPath: binaryPath,
		workDir:    workDir,
		timeout:    defaultTimeout,
		dialect:    DialectClaude,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Invoke builds an instruction from req, launches the CLI subprocess, collects
// output according to the configured dialect, and returns the accumulated content.
func (p *Provider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	// Verify the binary exists before attempting to start the subprocess.
	if _, err := exec.LookPath(p.binaryPath); err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("codeagent: binary not found at %q: %w", p.binaryPath, errors.New("not found"))
	}

	instruction := buildInstruction(req)

	// Apply a timeout via context so the subprocess is killed if it hangs.
	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := buildArgs(p.dialect, instruction)
	cmd := exec.CommandContext(runCtx, p.binaryPath, args...)
	cmd.Dir = p.workDir
	// Force-close I/O pipes and unblock Wait if child processes outlive the
	// parent after context cancellation (e.g. sh spawning sleep).
	cmd.WaitDelay = 500 * time.Millisecond

	// Capture stderr for error reporting.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("codeagent: create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("codeagent: start subprocess: %w", err)
	}

	var accumulated strings.Builder
	scanner := bufio.NewScanner(stdout)

	switch p.dialect {
	case DialectCodex:
		// Codex emits plain text — read lines directly.
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				accumulated.WriteString(line)
				accumulated.WriteString("\n")
			}
		}
	default:
		// Claude emits streaming JSON — parse events.
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			text, ok := extractText(line)
			if ok && text != "" {
				accumulated.WriteString(text)
			}
		}
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		// Distinguish between context timeout/cancellation and subprocess failure.
		if runCtx.Err() != nil {
			return domain.InvokeResponse{}, fmt.Errorf("codeagent: subprocess timed out or context cancelled: %w", runCtx.Err())
		}
		stderr := strings.TrimSpace(stderrBuf.String())
		if stderr != "" {
			return domain.InvokeResponse{}, fmt.Errorf("codeagent: subprocess exited with error: %w; stderr: %s", waitErr, stderr)
		}
		return domain.InvokeResponse{}, fmt.Errorf("codeagent: subprocess exited with error: %w", waitErr)
	}

	return domain.InvokeResponse{
		Content:    strings.TrimRight(accumulated.String(), "\n"),
		StopReason: "end_turn",
	}, nil
}

// buildArgs returns the CLI arguments for the given dialect and instruction.
func buildArgs(d Dialect, instruction string) []string {
	switch d {
	case DialectCodex:
		// Codex CLI: quiet mode + full-auto approval to avoid interactive prompts.
		return []string{"-q", "--approval-mode=full-auto", instruction}
	default:
		// Claude CLI: stream-json output format, skip permission prompts.
		return []string{
			"--output-format=stream-json",
			"--dangerously-skip-permissions",
			"-p", instruction,
		}
	}
}

// buildInstruction constructs a single text prompt from the InvokeRequest.
// The system prompt (if any) is prepended, followed by each message.
func buildInstruction(req domain.InvokeRequest) string {
	var sb strings.Builder
	if req.SystemPrompt != "" {
		sb.WriteString(req.SystemPrompt)
		sb.WriteString("\n\n")
	}
	for _, m := range req.Messages {
		if m.Content != "" {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// streamEvent is the minimal structure shared by all claude stream-json events.
type streamEvent struct {
	Type   string          `json:"type"`
	Delta  *deltaField     `json:"delta,omitempty"`
	Result string          `json:"result,omitempty"`
	Usage  json.RawMessage `json:"usage,omitempty"`
}

type deltaField struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// extractText parses one JSON line from the stream and returns any text it
// contains, along with a boolean indicating whether parsing succeeded.
func extractText(line string) (string, bool) {
	var ev streamEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		// Malformed lines are silently skipped.
		return "", false
	}
	switch ev.Type {
	case "content_block_delta":
		if ev.Delta != nil && ev.Delta.Type == "text_delta" {
			return ev.Delta.Text, true
		}
	case "result":
		if ev.Result != "" {
			return ev.Result, true
		}
	}
	return "", true
}
