package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"
)

func makeTestIndex(registryName string) domain.RegistryIndex {
	return domain.RegistryIndex{
		Registry:    registryName,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Plugins: []domain.RegistryEntry{
			{
				Name:          "test-tool",
				Description:   "A test plugin",
				Author:        "tester",
				LatestVersion: "1.0.0",
				Versions:      []string{"1.0.0"},
				ManifestURL:   "https://example.com/manifest.yaml",
				DownloadURL:   "https://example.com/test-tool-1.0.0.tar.gz",
			},
		},
	}
}

func TestHTTPRegistryManager_FetchIndex_ReturnsIndex(t *testing.T) {
	idx := makeTestIndex("test-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	result, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if result.Registry != idx.Registry {
		t.Errorf("got registry %q, want %q", result.Registry, idx.Registry)
	}
	if len(result.Plugins) != 1 {
		t.Errorf("got %d plugins, want 1", len(result.Plugins))
	}
}

func TestHTTPRegistryManager_FetchIndex_UsesCache(t *testing.T) {
	callCount := 0
	idx := makeTestIndex("cached-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)

	// First call: tries marketplace.json (404) then index.json (success) = 2 HTTP calls.
	_, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("first FetchIndex: %v", err)
	}
	firstCallCount := callCount

	// Second call without force should use cache — no additional HTTP calls.
	_, err = mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("second FetchIndex: %v", err)
	}
	if callCount != firstCallCount {
		t.Errorf("expected no additional HTTP calls after cached fetch, got %d extra", callCount-firstCallCount)
	}
}

func TestHTTPRegistryManager_FetchIndex_ForceBypassesCache(t *testing.T) {
	callCount := 0
	idx := makeTestIndex("forced-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)

	_, _ = mgr.FetchIndex(context.Background(), srv.URL, false)
	firstCallCount := callCount
	_, err := mgr.FetchIndex(context.Background(), srv.URL, true)
	if err != nil {
		t.Fatalf("forced FetchIndex: %v", err)
	}
	// force=true must make at least one additional HTTP call (cache bypassed).
	if callCount <= firstCallCount {
		t.Errorf("expected additional HTTP calls with force=true, call count unchanged at %d", callCount)
	}
}

func TestHTTPRegistryManager_FetchIndex_Timeout(t *testing.T) {
	// Slow server that never responds within the timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client times out.
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManagerWithTimeout(t.TempDir(), nil, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mgr.FetchIndex(ctx, srv.URL, false)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHTTPRegistryManager_Add_HTTPURLRejected(t *testing.T) {
	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	reg := domain.PluginRegistry{
		Name:    "insecure",
		URL:     "http://example.com",
		Trusted: false,
	}
	err := mgr.Add(context.Background(), reg)
	if err == nil {
		t.Fatal("expected error for HTTP URL, got nil")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("expected error about HTTPS requirement, got: %v", err)
	}
}

func TestHTTPRegistryManager_Add_HTTPS_Persisted(t *testing.T) {
	dir := t.TempDir()
	mgr := httpserver.NewHTTPRegistryManager(dir, nil)

	reg := domain.PluginRegistry{
		Name:    "my-registry",
		URL:     "https://example.com/registry",
		Trusted: true,
	}
	if err := mgr.Add(context.Background(), reg); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Should be retrievable via List.
	regs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, r := range regs {
		if r.Name == reg.Name {
			found = true
			if r.URL != reg.URL {
				t.Errorf("URL mismatch: got %q, want %q", r.URL, reg.URL)
			}
		}
	}
	if !found {
		t.Errorf("registry %q not found in list", reg.Name)
	}

	// Create a new manager from the same dir — registry should be persisted.
	mgr2 := httpserver.NewHTTPRegistryManager(dir, nil)
	regs2, err := mgr2.List(context.Background())
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	found2 := false
	for _, r := range regs2 {
		if r.Name == reg.Name {
			found2 = true
		}
	}
	if !found2 {
		t.Errorf("registry %q not persisted after reload", reg.Name)
	}
}

func TestHTTPRegistryManager_FetchIndex_MarketplaceJSON(t *testing.T) {
	marketplace := `{
		"name": "my-marketplace",
		"plugins": [
			{
				"name": "dev-flow",
				"source": "./plugins/dev-flow",
				"description": "Workflow plugin",
				"version": "1.2.3",
				"author": {"name": "alice", "email": "alice@example.com"},
				"keywords": ["workflow", "prd"]
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.claude-plugin/marketplace.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(marketplace))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	result, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if result.Registry != "my-marketplace" {
		t.Errorf("got registry %q, want %q", result.Registry, "my-marketplace")
	}
	if len(result.Plugins) != 1 {
		t.Fatalf("got %d plugins, want 1", len(result.Plugins))
	}
	p := result.Plugins[0]
	if p.Name != "dev-flow" {
		t.Errorf("plugin name: got %q, want %q", p.Name, "dev-flow")
	}
	if p.LatestVersion != "1.2.3" {
		t.Errorf("version: got %q, want %q", p.LatestVersion, "1.2.3")
	}
	if p.Author != "alice" {
		t.Errorf("author: got %q, want %q", p.Author, "alice")
	}
	if len(p.Tags) != 2 {
		t.Errorf("tags: got %d, want 2", len(p.Tags))
	}
	if !strings.Contains(p.ManifestURL, "plugins/dev-flow/.claude-plugin/plugin.json") {
		t.Errorf("manifest_url %q does not contain expected path", p.ManifestURL)
	}
}

func TestHTTPRegistryManager_FetchIndex_FallsBackToIndexJSON(t *testing.T) {
	idx := makeTestIndex("fallback-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(idx)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	result, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if result.Registry != idx.Registry {
		t.Errorf("got registry %q, want %q", result.Registry, idx.Registry)
	}
}

func TestFetchArchive_WireSizeLimit(t *testing.T) {
	// Serve a response that exceeds the 20 MB wire size limit.
	oversize := make([]byte, 20*1024*1024+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(oversize)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	_, err := mgr.FetchArchive(context.Background(), srv.URL+"/plugin.tar.gz")
	if err == nil {
		t.Fatal("expected wire size limit error, got nil")
	}
	if !strings.Contains(err.Error(), "wire size") && !strings.Contains(err.Error(), "20") {
		t.Errorf("expected wire size error, got: %v", err)
	}
}

func TestFetchClaudePlugin_FullFlow(t *testing.T) {
	pluginJSON := `{"name":"dev-flow","version":"1.2.3","description":"Workflow plugin","author":{"name":"alice","email":"alice@example.com"},"keywords":["workflow","prd"]}`
	packageJSON := `{"name":"dev-flow","claude":{"skills":["commands/dev-flow.md","commands/write-flow-analys.md"]}}`
	skillMD := "---\ndescription: Create a spec from a PRD\n---\n\n# Dev Flow\nSkill content here."
	skill2MD := "---\ndescription: Write flow analysis report\n---\n\n# Write Flow Analysis"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/dev-flow/.claude-plugin/plugin.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(pluginJSON))
		case "/plugins/dev-flow/package.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageJSON))
		case "/plugins/dev-flow/commands/dev-flow.md":
			_, _ = w.Write([]byte(skillMD))
		case "/plugins/dev-flow/commands/write-flow-analys.md":
			_, _ = w.Write([]byte(skill2MD))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	manifestURL := srv.URL + "/plugins/dev-flow/.claude-plugin/plugin.json"

	manifest, archive, err := mgr.FetchClaudePlugin(context.Background(), manifestURL)
	if err != nil {
		t.Fatalf("FetchClaudePlugin: %v", err)
	}
	if manifest.Name != "dev-flow" {
		t.Errorf("name: got %q, want %q", manifest.Name, "dev-flow")
	}
	if manifest.Version != "1.2.3" {
		t.Errorf("version: got %q, want %q", manifest.Version, "1.2.3")
	}
	if manifest.Author != "alice" {
		t.Errorf("author: got %q, want %q", manifest.Author, "alice")
	}
	if len(manifest.Tags) != 2 {
		t.Errorf("tags: got %d, want 2", len(manifest.Tags))
	}
	if manifest.Entrypoint != ".claude-plugin/plugin.json" {
		t.Errorf("entrypoint: got %q, want %q", manifest.Entrypoint, ".claude-plugin/plugin.json")
	}
	if manifest.Checksums["sha256"] == "" {
		t.Error("expected sha256 checksum to be set")
	}
	if len(archive) == 0 {
		t.Error("expected non-empty archive")
	}
	if len(manifest.Provides.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(manifest.Provides.Tools))
	}
	if manifest.Provides.Tools[0].Name != "dev-flow" {
		t.Errorf("tool[0] name: got %q, want %q", manifest.Provides.Tools[0].Name, "dev-flow")
	}
	if manifest.Provides.Tools[0].Description != "Create a spec from a PRD" {
		t.Errorf("tool[0] desc: got %q, want %q", manifest.Provides.Tools[0].Description, "Create a spec from a PRD")
	}
	if manifest.Provides.Tools[1].Name != "write-flow-analys" {
		t.Errorf("tool[1] name: got %q, want %q", manifest.Provides.Tools[1].Name, "write-flow-analys")
	}
}

func TestFetchClaudePlugin_MissingPackageJSON_NoSkills(t *testing.T) {
	pluginJSON := `{"name":"simple-tool","version":"0.1.0","description":"No skills","author":{"name":"bob"}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.claude-plugin/plugin.json" {
			_, _ = w.Write([]byte(pluginJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	manifest, archive, err := mgr.FetchClaudePlugin(context.Background(), srv.URL+"/.claude-plugin/plugin.json")
	if err != nil {
		t.Fatalf("FetchClaudePlugin: %v", err)
	}
	if manifest.Name != "simple-tool" {
		t.Errorf("name: got %q, want %q", manifest.Name, "simple-tool")
	}
	if len(manifest.Provides.Tools) != 0 {
		t.Errorf("expected 0 tools (no package.json), got %d", len(manifest.Provides.Tools))
	}
	if len(archive) == 0 {
		t.Error("expected non-empty archive even with no skills")
	}
	if manifest.Checksums["sha256"] == "" {
		t.Error("expected sha256 checksum even with no skills")
	}
}

func TestFetchClaudePlugin_ChecksumDeterministic(t *testing.T) {
	pluginJSON := `{"name":"det-tool","version":"1.0.0","description":"Deterministic","author":{"name":"alice"}}`
	packageJSON := `{"claude":{"skills":["commands/cmd.md"]}}`
	skillMD := "---\ndescription: A command\n---\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.claude-plugin/plugin.json":
			_, _ = w.Write([]byte(pluginJSON))
		case "/package.json":
			_, _ = w.Write([]byte(packageJSON))
		case "/commands/cmd.md":
			_, _ = w.Write([]byte(skillMD))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	url := srv.URL + "/.claude-plugin/plugin.json"

	m1, a1, err := mgr.FetchClaudePlugin(context.Background(), url)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	m2, a2, err := mgr.FetchClaudePlugin(context.Background(), url)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if m1.Checksums["sha256"] != m2.Checksums["sha256"] {
		t.Errorf("checksum not deterministic: %q vs %q", m1.Checksums["sha256"], m2.Checksums["sha256"])
	}
	if string(a1) != string(a2) {
		t.Error("archive bytes not deterministic across two calls")
	}
}
