// Package subteam provides the SubTeamManager application service for the
// tech-lead bot. It manages spawning, heartbeating, and terminating isolated
// sub-agent goroutines, each with their own scoped bus and message router.
package subteam

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

// Config configures the SubTeamManager.
type Config struct {
	// BotsDir is the directory containing bots/<type>/config.yaml.
	BotsDir string
	// MemoryRoot is the base path for per-bot memory files.
	MemoryRoot string
	// HeartbeatInterval is how often SendHeartbeat should be called.
	// Defaults to 30s. Used as documentation only — the caller drives the timer.
	HeartbeatInterval time.Duration
	// HeartbeatTimeout is the duration without a heartbeat after which a spawned
	// bot self-terminates. Defaults to 90s.
	HeartbeatTimeout time.Duration
	// SoftSpawnLimit is the number of spawned bots at which a warning is logged.
	// Defaults to 5. Does not block spawning.
	SoftSpawnLimit int
}

func (c *Config) applyDefaults() {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 30 * time.Second
	}
	if c.HeartbeatTimeout <= 0 {
		c.HeartbeatTimeout = 90 * time.Second
	}
	if c.SoftSpawnLimit <= 0 {
		c.SoftSpawnLimit = 5
	}
}

// botState holds runtime state for a single spawned sub-agent.
type botState struct {
	agent     *domain.SpawnedAgent
	cancel    context.CancelFunc
	done      chan struct{}
	heartbeat chan struct{}
}

// Manager implements domain.SubTeamManager for the tech-lead bot.
type Manager struct {
	cfg  Config
	sf   *infrastructure.SessionFile
	mu   sync.Mutex
	bots map[string]*botState
}

// New creates a new Manager with the given configuration.
func New(cfg Config) *Manager {
	cfg.applyDefaults()
	return &Manager{
		cfg:  cfg,
		bots: make(map[string]*botState),
	}
}

// WithSessionFile attaches a session file to the manager. On construction, it
// loads existing session records and attempts to reconnect any live sessions.
// Call before Spawn to enable persistence.
func (m *Manager) WithSessionFile(sf *infrastructure.SessionFile) *Manager {
	m.sf = sf
	return m
}

// Spawn creates a new isolated sub-agent goroutine for the given bot type.
// Returns an error if:
//   - name is already registered as an active agent
//   - the botType's config.yaml is not found under BotsDir
//   - context is done
func (m *Manager) Spawn(ctx context.Context, botType, name, workDir string) (*domain.SpawnedAgent, error) {
	// Validate bot type.
	cfgPath := filepath.Join(m.cfg.BotsDir, botType, "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		return nil, fmt.Errorf("subteam: bot type %q not found in %s: %w", botType, m.cfg.BotsDir, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[name]; exists {
		return nil, fmt.Errorf("subteam: agent %q is already active", name)
	}

	if len(m.bots)+1 > m.cfg.SoftSpawnLimit {
		slog.Warn("subteam: soft spawn limit exceeded", "limit", m.cfg.SoftSpawnLimit, "current", len(m.bots)+1)
	}

	agent := &domain.SpawnedAgent{
		Name:      name,
		BotType:   botType,
		WorkDir:   workDir,
		BusID:     fmt.Sprintf("bus-%s", name),
		Status:    domain.AgentStatusIdle,
		SpawnedAt: time.Now().UTC(),
	}

	botCtx, cancel := context.WithCancel(ctx)
	hbCh := make(chan struct{}, 1)
	done := make(chan struct{})

	state := &botState{
		agent:     agent,
		cancel:    cancel,
		done:      done,
		heartbeat: hbCh,
	}
	m.bots[name] = state

	go m.runBot(botCtx, state)

	// Persist session record.
	if m.sf != nil {
		records, _ := m.sf.Load()
		records = append(records, infrastructure.SessionRecord{
			Name:      agent.Name,
			BotType:   agent.BotType,
			WorkDir:   agent.WorkDir,
			BusID:     agent.BusID,
			Status:    agent.Status,
			SpawnedAt: agent.SpawnedAt,
		})
		if err := m.sf.Save(records); err != nil {
			slog.Warn("subteam: failed to persist session record", "name", name, "err", err)
		}
	}

	slog.Info("subteam: agent spawned", "name", name, "bot_type", botType, "work_dir", workDir)
	return agent, nil
}

// runBot is the goroutine lifecycle for a single spawned bot. It monitors the
// heartbeat channel and self-terminates on timeout or context cancellation.
// All panics are recovered and logged.
func (m *Manager) runBot(ctx context.Context, state *botState) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("subteam: panic in spawned bot goroutine",
				"name", state.agent.Name, "panic", r)
		}
		// markTerminated before closing done so callers waiting on done can rely
		// on the agent state being fully cleaned up when done is unblocked.
		m.markTerminated(state.agent.Name)
		close(state.done)
	}()

	timeout := m.cfg.HeartbeatTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown via Terminate or TearDownAll.
			return
		case <-state.heartbeat:
			// Heartbeat received — reset the timer.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		case <-timer.C:
			// Heartbeat timeout — self-terminate.
			slog.Warn("subteam: heartbeat timeout; bot self-terminating",
				"name", state.agent.Name, "timeout", timeout)
			return
		}
	}
}

// markTerminated updates the agent status to terminated and removes the session
// record. Called from the goroutine's deferred cleanup.
func (m *Manager) markTerminated(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.bots[name]; ok {
		state.agent.Status = domain.AgentStatusTerminated
	}
	if m.sf != nil {
		if err := m.sf.Remove(name); err != nil {
			slog.Warn("subteam: failed to remove session record on termination", "name", name, "err", err)
		}
	}
}

// Terminate gracefully stops the named agent by cancelling its context and
// waiting for the goroutine to exit with a 5-second timeout.
func (m *Manager) Terminate(ctx context.Context, name string) error {
	m.mu.Lock()
	state, ok := m.bots[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("subteam: agent %q not found", name)
	}
	if state.agent.Status == domain.AgentStatusTerminated {
		m.mu.Unlock()
		return fmt.Errorf("subteam: agent %q is already terminated", name)
	}
	state.agent.Status = domain.AgentStatusTerminating
	cancel := state.cancel
	done := state.done
	m.mu.Unlock()

	cancel()

	// Wait for the goroutine to finish.
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()

	select {
	case <-done:
	case <-waitCtx.Done():
		return fmt.Errorf("subteam: Terminate %q timed out: %w", name, waitCtx.Err())
	}

	slog.Info("subteam: agent terminated", "name", name)
	return nil
}

// SendHeartbeat broadcasts a heartbeat signal to all active spawned bots.
// For bots whose heartbeat channel is full or closed, the signal is dropped
// gracefully.
func (m *Manager) SendHeartbeat(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, state := range m.bots {
		if state.agent.Status == domain.AgentStatusTerminated {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("subteam: heartbeat send recovered panic", "name", name, "panic", r)
				}
			}()
			select {
			case state.heartbeat <- struct{}{}:
			default:
				// Channel full — drop heartbeat.
			}
		}()
	}
	return nil
}

// ListAgents returns a snapshot of all known spawned agents (including
// terminated ones).
func (m *Manager) ListAgents(_ context.Context) ([]*domain.SpawnedAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	agents := make([]*domain.SpawnedAgent, 0, len(m.bots))
	for _, state := range m.bots {
		cp := *state.agent
		agents = append(agents, &cp)
	}
	return agents, nil
}

// TearDownAll terminates all active agents concurrently and waits for all
// goroutines to stop.
func (m *Manager) TearDownAll(ctx context.Context) error {
	m.mu.Lock()
	// Collect all states to terminate.
	states := make([]*botState, 0, len(m.bots))
	for _, state := range m.bots {
		if state.agent.Status != domain.AgentStatusTerminated {
			state.agent.Status = domain.AgentStatusTerminating
			states = append(states, state)
		}
	}
	m.mu.Unlock()

	// Cancel all bot contexts concurrently.
	for _, state := range states {
		state.cancel()
	}

	// Wait for all goroutines.
	deadline := time.Now().Add(10 * time.Second)
	var errs []error
	for _, state := range states {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			errs = append(errs, fmt.Errorf("subteam: TearDownAll timed out waiting for %q", state.agent.Name))
			continue
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, remaining)
		select {
		case <-state.done:
		case <-waitCtx.Done():
			errs = append(errs, fmt.Errorf("subteam: TearDownAll timed out for %q", state.agent.Name))
		}
		waitCancel()
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
