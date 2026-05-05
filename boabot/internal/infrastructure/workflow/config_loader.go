// Package workflow provides infrastructure-level loading and hot-reloading of
// workflow definitions from YAML configuration files.
package workflow

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"gopkg.in/yaml.v3"

	domainwf "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

// workflowYAML is the top-level structure of the workflow config file.
type workflowYAML struct {
	Workflows []workflowDefYAML `yaml:"workflows"`
}

type workflowDefYAML struct {
	Name  string         `yaml:"name"`
	Steps []workStepYAML `yaml:"steps"`
}

type workStepYAML struct {
	Name          string `yaml:"name"`
	RequiredRole  string `yaml:"required_role"`
	NextStep      string `yaml:"next_step"`
	NotifyOnEntry bool   `yaml:"notify_on_entry"`
}

// ConfigLoader loads WorkflowDefinitions from a YAML file and supports
// atomic hot-reload on SIGHUP.
type ConfigLoader struct {
	path   string
	mu     sync.RWMutex
	router *domainwf.DefaultRouter
}

// NewConfigLoader constructs a ConfigLoader and performs the initial load.
// Returns an error if the file cannot be read or parsed.
func NewConfigLoader(path string) (*ConfigLoader, error) {
	cl := &ConfigLoader{path: path}
	if err := cl.Reload(); err != nil {
		return nil, err
	}
	return cl, nil
}

// Router returns the current WorkflowRouter in a thread-safe manner.
func (cl *ConfigLoader) Router() *domainwf.DefaultRouter {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.router
}

// WatchSIGHUP blocks until ctx is cancelled, reloading the config on each
// SIGHUP signal received.
func (cl *ConfigLoader) WatchSIGHUP(ctx context.Context) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			_ = cl.Reload() // best-effort; errors ignored in background watcher
		}
	}
}

// Reload reads the YAML file from disk and atomically replaces the router.
func (cl *ConfigLoader) Reload() error {
	defs, err := cl.loadFile()
	if err != nil {
		return err
	}

	router := domainwf.NewDefaultRouter(defs)

	cl.mu.Lock()
	cl.router = router
	cl.mu.Unlock()

	return nil
}

func (cl *ConfigLoader) loadFile() ([]domainwf.WorkflowDefinition, error) {
	data, err := os.ReadFile(cl.path)
	if err != nil {
		return nil, fmt.Errorf("workflow config: read %s: %w", cl.path, err)
	}

	var raw workflowYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("workflow config: parse %s: %w", cl.path, err)
	}

	defs := make([]domainwf.WorkflowDefinition, 0, len(raw.Workflows))
	for _, wd := range raw.Workflows {
		steps := make([]domainwf.WorkflowStep, 0, len(wd.Steps))
		for _, s := range wd.Steps {
			steps = append(steps, domainwf.WorkflowStep{
				Name:          s.Name,
				RequiredRole:  domainwf.BotRole(s.RequiredRole),
				NextStep:      s.NextStep,
				NotifyOnEntry: s.NotifyOnEntry,
			})
		}
		defs = append(defs, domainwf.WorkflowDefinition{
			Name:  wd.Name,
			Steps: steps,
		})
	}
	return defs, nil
}
