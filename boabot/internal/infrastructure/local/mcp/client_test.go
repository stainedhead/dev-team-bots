package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	localmcp "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
)

// stubBoardStore is a minimal domain.BoardStore for tests.
type stubBoardStore struct {
	items  map[string]domain.WorkItem
	getErr error
	updErr error
}

func newStubBoardStore(items ...domain.WorkItem) *stubBoardStore {
	s := &stubBoardStore{items: make(map[string]domain.WorkItem)}
	for _, it := range items {
		s.items[it.ID] = it
	}
	return s
}

func (s *stubBoardStore) Get(_ context.Context, id string) (domain.WorkItem, error) {
	if s.getErr != nil {
		return domain.WorkItem{}, s.getErr
	}
	it, ok := s.items[id]
	if !ok {
		return domain.WorkItem{}, errors.New("not found")
	}
	return it, nil
}

func (s *stubBoardStore) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	if s.updErr != nil {
		return domain.WorkItem{}, s.updErr
	}
	s.items[item.ID] = item
	return item, nil
}

func (s *stubBoardStore) Create(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	s.items[item.ID] = item
	return item, nil
}

func (s *stubBoardStore) List(_ context.Context, _ domain.WorkItemFilter) ([]domain.WorkItem, error) {
	return nil, nil
}

func (s *stubBoardStore) Delete(_ context.Context, id string) error {
	delete(s.items, id)
	return nil
}

func (s *stubBoardStore) Reorder(_ context.Context, _ []string) error {
	return nil
}

func TestClient_ListTools_ReturnsExpectedTools(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"read_file", "write_file", "create_dir", "list_dir", "run_shell"} {
		if !names[expected] {
			t.Errorf("expected tool %q in list", expected)
		}
	}
}

func TestClient_WriteFile_ReadFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	path := filepath.Join(dir, "hello.txt")

	result, err := c.CallTool(context.Background(), "write_file", map[string]any{
		"path":    path,
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if result.IsError {
		t.Fatalf("write_file returned error: %v", result.Content)
	}

	result, err = c.CallTool(context.Background(), "read_file", map[string]any{"path": path})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if result.IsError {
		t.Fatalf("read_file returned error: %v", result.Content)
	}
	if len(result.Content) == 0 || result.Content[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %v", result.Content)
	}
}

func TestClient_WriteFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	path := filepath.Join(dir, "nested", "deep", "file.txt")
	_, err := c.CallTool(context.Background(), "write_file", map[string]any{
		"path":    path,
		"content": "nested",
	})
	if err != nil {
		t.Fatalf("write_file with nested path: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("expected file to exist at %s: %v", path, statErr)
	}
}

func TestClient_CreateDir(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	newDir := filepath.Join(dir, "subdir", "nested")
	result, err := c.CallTool(context.Background(), "create_dir", map[string]any{"path": newDir})
	if err != nil {
		t.Fatalf("create_dir: %v", err)
	}
	if result.IsError {
		t.Fatalf("create_dir returned error: %v", result.Content)
	}
	info, statErr := os.Stat(newDir)
	if statErr != nil || !info.IsDir() {
		t.Errorf("expected directory at %s", newDir)
	}
}

func TestClient_ListDir(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	// Create a few entries.
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	result, err := c.CallTool(context.Background(), "list_dir", map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("list_dir: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("list_dir returned error or empty: %v", result.Content)
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &entries); err != nil {
		t.Fatalf("unmarshal list_dir output: %v", err)
	}
	names := make(map[string]bool)
	for _, e := range entries {
		names[e["name"].(string)] = true
	}
	if !names["a.txt"] {
		t.Error("expected a.txt in listing")
	}
	if !names["sub"] {
		t.Error("expected sub in listing")
	}
}

func TestClient_RunShell_Simple(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "run_shell", map[string]any{
		"command":     "echo hello",
		"working_dir": dir,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}
	if result.IsError {
		t.Fatalf("run_shell returned error: %v", result.Content)
	}
	if !strings.Contains(result.Content[0].Text, "hello") {
		t.Errorf("expected 'hello' in output, got %q", result.Content[0].Text)
	}
}

func TestClient_RunShell_NonZeroExit_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "run_shell", map[string]any{
		"command":     "exit 1",
		"working_dir": dir,
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for non-zero exit")
	}
}

func TestClient_PathOutsideAllowedDirs_Rejected(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "read_file", map[string]any{
		"path": "/etc/passwd",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for path outside allowed dirs")
	}
}

func TestClient_PathTraversal_Rejected(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "read_file", map[string]any{
		"path": filepath.Join(dir, "../../../etc/passwd"),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for path traversal attempt")
	}
}

func TestClient_RunShell_WorkdirOutsideAllowed_Rejected(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "run_shell", map[string]any{
		"command":     "echo hi",
		"working_dir": "/etc",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when working_dir is outside allowed dirs")
	}
}

func TestClient_UnknownTool_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	result, err := c.CallTool(context.Background(), "nonexistent_tool", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for unknown tool")
	}
}

func TestClient_ReadFile_NonExistent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "read_file", map[string]any{
		"path": filepath.Join(dir, "does_not_exist.txt"),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing file")
	}
}

func TestClient_ListDir_NonExistent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	c := localmcp.NewClient([]string{dir})

	result, err := c.CallTool(context.Background(), "list_dir", map[string]any{
		"path": filepath.Join(dir, "no_such_dir"),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for nonexistent directory")
	}
}

func TestClient_WriteFile_MissingPathArg_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	result, err := c.CallTool(context.Background(), "write_file", map[string]any{
		"content": "hello",
		// "path" intentionally omitted
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing path argument")
	}
}

func TestClient_CreateDir_MissingPathArg_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	result, err := c.CallTool(context.Background(), "create_dir", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing path argument")
	}
}

func TestClient_CompleteBoardItem_MarksItemDone(t *testing.T) {
	bs := newStubBoardStore(domain.WorkItem{ID: "item-1", Status: domain.WorkItemStatusInProgress})
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))

	result, err := c.CallTool(context.Background(), "complete_board_item", map[string]any{
		"item_id": "item-1",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	if bs.items["item-1"].Status != domain.WorkItemStatusDone {
		t.Errorf("expected item status 'done', got %q", bs.items["item-1"].Status)
	}
}

func TestClient_CompleteBoardItem_NotFound_ReturnsError(t *testing.T) {
	bs := newStubBoardStore() // empty
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))

	result, err := c.CallTool(context.Background(), "complete_board_item", map[string]any{
		"item_id": "does-not-exist",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for unknown item")
	}
}

func TestClient_CompleteBoardItem_MissingArg_ReturnsError(t *testing.T) {
	bs := newStubBoardStore()
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))

	result, err := c.CallTool(context.Background(), "complete_board_item", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing item_id")
	}
}

func TestClient_CompleteBoardItem_NoBoardStore_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"}) // no board store
	result, err := c.CallTool(context.Background(), "complete_board_item", map[string]any{
		"item_id": "item-1",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when board store is unavailable")
	}
}

func TestClient_ListTools_WithBoardStore_IncludesCompleteTool(t *testing.T) {
	bs := newStubBoardStore()
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "complete_board_item" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected complete_board_item in tool list when board store is provided")
	}
}

func TestClient_ListTools_WithoutBoardStore_ExcludesCompleteTool(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"}) // no board store
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "complete_board_item" {
			t.Error("complete_board_item should not be present without a board store")
		}
	}
}
