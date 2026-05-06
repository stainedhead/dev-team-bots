// Package team internal tests — exercises unexported types that cannot be
// reached through the public API without the export_test.go seams.
package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNoopMCPClient_ListTools(t *testing.T) {
	c := &noopMCPClient{}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned unexpected error: %v", err)
	}
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}

func TestNoopMCPClient_CallTool(t *testing.T) {
	c := &noopMCPClient{}
	_, err := c.CallTool(context.Background(), "any", nil)
	if err == nil {
		t.Fatal("expected error from noopMCPClient.CallTool, got nil")
	}
}

func TestSimpleWorkerFactory_New(t *testing.T) {
	sw := &simpleWorkerFactory{worker: nil}
	if sw.New() != nil {
		t.Error("expected nil worker from uninitialized factory")
	}
}

func TestManagerConfig_ApplyDefaults(t *testing.T) {
	var cfg ManagerConfig
	cfg.applyDefaults()
	if cfg.RestartDelay <= 0 {
		t.Error("RestartDelay should be positive after applyDefaults")
	}
	if cfg.MaxRestartDelay <= 0 {
		t.Error("MaxRestartDelay should be positive after applyDefaults")
	}
	if cfg.MemoryRoot == "" {
		t.Error("MemoryRoot should be non-empty after applyDefaults")
	}
}

func TestLoadTeamConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	badYAML := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("team: [\ninvalid"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := loadTeamConfig(badYAML)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadTeamConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `team:
  - name: bot1
    type: worker
    enabled: true
    orchestrator: false
`
	p := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(p, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write team.yaml: %v", err)
	}
	tc, err := loadTeamConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tc.Team) != 1 || tc.Team[0].Name != "bot1" {
		t.Errorf("unexpected team config: %+v", tc)
	}
}
