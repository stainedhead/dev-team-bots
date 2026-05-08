package httpserver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const (
	indexCacheTTL       = 5 * time.Minute
	defaultFetchTimeout = 10 * time.Second
	registriesFile      = "registries.json"
	maxArchiveWireSize  = 20 * 1024 * 1024 // 20 MB compressed
)

type cachedIndex struct {
	index     domain.RegistryIndex
	fetchedAt time.Time
}

// HTTPRegistryManager implements domain.RegistryManager backed by HTTP with an
// in-memory TTL cache.
type HTTPRegistryManager struct {
	mu         sync.RWMutex
	registries []domain.PluginRegistry
	persistDir string
	cache      map[string]cachedIndex
	httpClient *http.Client
}

// NewHTTPRegistryManager creates an HTTPRegistryManager.
// configRegistries are the registries from the static config (take precedence).
// persistDir is used to load/save runtime-added registries.
func NewHTTPRegistryManager(persistDir string, configRegistries []domain.PluginRegistry) *HTTPRegistryManager {
	return NewHTTPRegistryManagerWithTimeout(persistDir, configRegistries, defaultFetchTimeout)
}

// NewHTTPRegistryManagerWithTimeout creates an HTTPRegistryManager with a custom fetch timeout.
func NewHTTPRegistryManagerWithTimeout(persistDir string, configRegistries []domain.PluginRegistry, timeout time.Duration) *HTTPRegistryManager {
	m := &HTTPRegistryManager{
		persistDir: persistDir,
		cache:      make(map[string]cachedIndex),
		httpClient: &http.Client{Timeout: timeout},
	}

	// Load runtime-added registries from disk.
	runtimeRegs := m.loadPersistedRegistries()

	// Merge: config takes precedence over runtime (by name).
	configNames := make(map[string]bool)
	for _, r := range configRegistries {
		configNames[r.Name] = true
	}

	merged := make([]domain.PluginRegistry, 0, len(configRegistries)+len(runtimeRegs))
	merged = append(merged, configRegistries...)
	for _, r := range runtimeRegs {
		if !configNames[r.Name] {
			merged = append(merged, r)
		}
	}
	m.registries = merged
	return m
}

func (m *HTTPRegistryManager) loadPersistedRegistries() []domain.PluginRegistry {
	path := filepath.Join(m.persistDir, registriesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var regs []domain.PluginRegistry
	if err := json.Unmarshal(data, &regs); err != nil {
		return nil
	}
	return regs
}

func (m *HTTPRegistryManager) persistRegistries() {
	// Only persist runtime registries (those not from config).
	// For simplicity, persist all current registries.
	data, err := json.Marshal(m.registries)
	if err != nil {
		return
	}
	if err := os.MkdirAll(m.persistDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(m.persistDir, registriesFile), data, 0o644)
}

// List returns the current list of configured registries.
func (m *HTTPRegistryManager) List(_ context.Context) ([]domain.PluginRegistry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.PluginRegistry, len(m.registries))
	copy(out, m.registries)
	return out, nil
}

// Add adds a registry. URL must be HTTPS.
func (m *HTTPRegistryManager) Add(_ context.Context, reg domain.PluginRegistry) error {
	if !strings.HasPrefix(reg.URL, "https://") {
		return fmt.Errorf("registry manager: registry URL must use https:// (got %q)", reg.URL)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registries = append(m.registries, reg)
	m.persistRegistries()
	return nil
}

// Remove removes a registry by name.
func (m *HTTPRegistryManager) Remove(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	filtered := m.registries[:0]
	for _, r := range m.registries {
		if r.Name != name {
			filtered = append(filtered, r)
		}
	}
	m.registries = filtered
	m.persistRegistries()
	return nil
}

// indexCandidate is a URL to try when fetching a registry index, plus the
// format expected at that URL.
type indexCandidate struct {
	url    string
	format string // "index" or "marketplace"
}

// resolveIndexCandidates returns ordered candidates for fetching a registry index.
// GitHub repo URLs (with or without .git suffix) are rewritten to
// raw.githubusercontent.com and both main and master branches are tried.
// For all URLs, .claude-plugin/marketplace.json is tried before index.json.
func resolveIndexCandidates(registryURL string) []indexCandidate {
	base := strings.TrimRight(registryURL, "/")
	base = strings.TrimSuffix(base, ".git")

	if strings.HasPrefix(base, "https://github.com/") {
		parts := strings.SplitN(strings.TrimPrefix(base, "https://github.com/"), "/", 2)
		if len(parts) == 2 {
			raw := "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1]
			return []indexCandidate{
				{raw + "/main/.claude-plugin/marketplace.json", "marketplace"},
				{raw + "/master/.claude-plugin/marketplace.json", "marketplace"},
				{raw + "/main/index.json", "index"},
				{raw + "/master/index.json", "index"},
			}
		}
	}
	return []indexCandidate{
		{base + "/.claude-plugin/marketplace.json", "marketplace"},
		{base + "/index.json", "index"},
	}
}

// marketplaceAuthor is the author field in a .claude-plugin/marketplace.json entry.
type marketplaceAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// marketplaceEntry is one plugin entry in a .claude-plugin/marketplace.json file.
type marketplaceEntry struct {
	Name        string            `json:"name"`
	Source      string            `json:"source"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Author      marketplaceAuthor `json:"author"`
	Keywords    []string          `json:"keywords"`
	Category    string            `json:"category"`
}

// marketplaceIndex is the top-level structure of a .claude-plugin/marketplace.json file.
type marketplaceIndex struct {
	Name    string             `json:"name"`
	Plugins []marketplaceEntry `json:"plugins"`
}

// marketplaceToRegistryIndex converts a marketplace.json to a domain.RegistryIndex.
// rawBase is the raw content base URL (e.g. https://raw.githubusercontent.com/owner/repo/main)
// used to resolve relative source paths into manifest URLs.
func marketplaceToRegistryIndex(mp marketplaceIndex, rawBase string) domain.RegistryIndex {
	entries := make([]domain.RegistryEntry, 0, len(mp.Plugins))
	for _, p := range mp.Plugins {
		src := strings.TrimPrefix(strings.TrimPrefix(p.Source, "./"), "/")
		manifestURL := strings.TrimRight(rawBase, "/") + "/" + src + "/.claude-plugin/plugin.json"
		entries = append(entries, domain.RegistryEntry{
			Name:          p.Name,
			Description:   p.Description,
			Author:        p.Author.Name,
			LatestVersion: p.Version,
			Tags:          p.Keywords,
			Versions:      []string{p.Version},
			ManifestURL:   manifestURL,
		})
	}
	return domain.RegistryIndex{Registry: mp.Name, Plugins: entries}
}

// FetchIndex fetches the registry index for the registry at registryURL.
// GitHub repo URLs are automatically rewritten to raw content URLs and both
// .claude-plugin/marketplace.json and index.json formats are tried in order.
// If force is false and a cached copy is still fresh (< 5 min), returns the cache.
func (m *HTTPRegistryManager) FetchIndex(ctx context.Context, registryURL string, force bool) (domain.RegistryIndex, error) {
	cacheKey := strings.TrimRight(strings.TrimSuffix(registryURL, ".git"), "/")

	if !force {
		m.mu.RLock()
		cached, ok := m.cache[cacheKey]
		m.mu.RUnlock()
		if ok && time.Since(cached.fetchedAt) < indexCacheTTL {
			return cached.index, nil
		}
	}

	candidates := resolveIndexCandidates(registryURL)
	var lastErr error
	for _, c := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			return domain.RegistryIndex{}, fmt.Errorf("registry manager: build request: %w", err)
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("registry manager: fetch index: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("registry manager: fetch index: not found at %s", c.url)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return domain.RegistryIndex{}, fmt.Errorf("registry manager: fetch index: server returned %d", resp.StatusCode)
		}

		var idx domain.RegistryIndex
		if c.format == "marketplace" {
			var mp marketplaceIndex
			err = json.NewDecoder(resp.Body).Decode(&mp)
			_ = resp.Body.Close()
			if err != nil {
				lastErr = fmt.Errorf("registry manager: decode marketplace: %w", err)
				continue
			}
			// Derive the raw content base from the candidate URL by stripping the file path.
			rawBase := c.url[:strings.LastIndex(c.url, "/.claude-plugin/marketplace.json")]
			idx = marketplaceToRegistryIndex(mp, rawBase)
		} else {
			err = json.NewDecoder(resp.Body).Decode(&idx)
			_ = resp.Body.Close()
			if err != nil {
				lastErr = fmt.Errorf("registry manager: decode index: %w", err)
				continue
			}
		}

		m.mu.Lock()
		m.cache[cacheKey] = cachedIndex{index: idx, fetchedAt: time.Now()}
		m.mu.Unlock()

		return idx, nil
	}

	if lastErr != nil {
		return domain.RegistryIndex{}, lastErr
	}
	return domain.RegistryIndex{}, fmt.Errorf("registry manager: fetch index: no candidates resolved")
}

// FetchManifest downloads and parses a plugin manifest from manifestURL.
func (m *HTTPRegistryManager) FetchManifest(ctx context.Context, manifestURL string) (domain.PluginManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return domain.PluginManifest{}, fmt.Errorf("registry manager: build manifest request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return domain.PluginManifest{}, fmt.Errorf("registry manager: fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.PluginManifest{}, fmt.Errorf("registry manager: fetch manifest: server returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.PluginManifest{}, fmt.Errorf("registry manager: read manifest: %w", err)
	}

	var manifest domain.PluginManifest
	// Try YAML first, then JSON.
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
			return domain.PluginManifest{}, fmt.Errorf("registry manager: parse manifest: %w", err)
		}
	}
	return manifest, nil
}

// FetchArchive downloads a plugin archive from downloadURL.
func (m *HTTPRegistryManager) FetchArchive(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("registry manager: build archive request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry manager: fetch archive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry manager: fetch archive: server returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveWireSize+1))
	if err != nil {
		return nil, fmt.Errorf("registry manager: read archive: %w", err)
	}
	if len(data) > maxArchiveWireSize {
		return nil, fmt.Errorf("registry manager: archive exceeds %d byte wire size limit (20 MB)", maxArchiveWireSize)
	}
	return data, nil
}

// --- Claude Code plugin support ---

// claudePluginJSON is the format of .claude-plugin/plugin.json.
type claudePluginJSON struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
	Keywords []string `json:"keywords"`
	Category string   `json:"category"`
}

// claudePackageJSON holds the Claude-specific block inside a plugin's package.json.
type claudePackageJSON struct {
	Claude struct {
		Skills []string `json:"skills"`
	} `json:"claude"`
}

// skillFrontmatter is the YAML frontmatter block at the top of a skill markdown file.
type skillFrontmatter struct {
	Description string `yaml:"description"`
}

// parseSkillDescription extracts the description field from YAML frontmatter in a
// skill markdown file (between leading "---\n" delimiters).
func parseSkillDescription(content []byte) string {
	s := string(content)
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return ""
	}
	return fm.Description
}

// packClaudePluginFiles packs a map of relative-path → content into a tar.gz archive.
// Paths are sorted for deterministic output.
func packClaudePluginFiles(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, p := range paths {
		data := files[p]
		hdr := &tar.Header{
			Name:     p,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fetchRaw fetches a URL and returns the raw response body, capped at maxArchiveWireSize.
func (m *HTTPRegistryManager) fetchRaw(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: build request: %w", url, err)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: server returned %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveWireSize+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: read body: %w", url, err)
	}
	return data, nil
}

// FetchClaudePlugin downloads a Claude Code plugin from its .claude-plugin/plugin.json
// manifest URL. It fetches plugin.json, package.json, and all skill markdown files listed
// under claude.skills, packs them into a tar.gz archive, and returns a synthetic
// PluginManifest with the archive's SHA-256 checksum set.
func (m *HTTPRegistryManager) FetchClaudePlugin(ctx context.Context, manifestURL string) (domain.PluginManifest, []byte, error) {
	sep := "/.claude-plugin/plugin.json"
	idx := strings.LastIndex(manifestURL, sep)
	if idx < 0 {
		return domain.PluginManifest{}, nil, fmt.Errorf("claude plugin: manifest URL does not contain %q", sep)
	}
	baseURL := manifestURL[:idx]

	// Fetch .claude-plugin/plugin.json.
	pluginJSONData, err := m.fetchRaw(ctx, manifestURL)
	if err != nil {
		return domain.PluginManifest{}, nil, fmt.Errorf("claude plugin: fetch plugin.json: %w", err)
	}
	var cpj claudePluginJSON
	if err := json.Unmarshal(pluginJSONData, &cpj); err != nil {
		return domain.PluginManifest{}, nil, fmt.Errorf("claude plugin: parse plugin.json: %w", err)
	}

	files := map[string][]byte{
		".claude-plugin/plugin.json": pluginJSONData,
	}

	// Fetch package.json — optional; if missing or malformed, skills list is empty.
	var skills []string
	if pkgData, pkgErr := m.fetchRaw(ctx, baseURL+"/package.json"); pkgErr == nil {
		files["package.json"] = pkgData
		var pkg claudePackageJSON
		if json.Unmarshal(pkgData, &pkg) == nil {
			skills = pkg.Claude.Skills
		}
	}

	// Fetch each skill file and extract its description from YAML frontmatter.
	tools := make([]domain.MCPTool, 0, len(skills))
	for _, skillPath := range skills {
		skillData, err := m.fetchRaw(ctx, baseURL+"/"+skillPath)
		if err != nil {
			continue
		}
		files[skillPath] = skillData
		name := strings.TrimSuffix(filepath.Base(skillPath), ".md")
		desc := parseSkillDescription(skillData)
		tools = append(tools, domain.MCPTool{
			Name:        name,
			Description: desc,
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}

	// Pack all files into a tar.gz archive.
	archive, err := packClaudePluginFiles(files)
	if err != nil {
		return domain.PluginManifest{}, nil, fmt.Errorf("claude plugin: pack archive: %w", err)
	}

	sum := sha256.Sum256(archive)
	manifest := domain.PluginManifest{
		Name:        cpj.Name,
		Version:     cpj.Version,
		Description: cpj.Description,
		Author:      cpj.Author.Name,
		Tags:        cpj.Keywords,
		Entrypoint:  ".claude-plugin/plugin.json",
		Provides:    domain.PluginProvides{Tools: tools},
		Checksums:   map[string]string{"sha256": hex.EncodeToString(sum[:])},
	}

	return manifest, archive, nil
}
