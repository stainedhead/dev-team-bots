package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	localrules "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/rules"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestTracker_InitialLoad_AddsRulesFromRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "# Root rules")

	tr := localrules.NewTracker([]string{dir})
	update := tr.UpdateForDir(context.Background(), dir)

	if len(update.Add) != 1 {
		t.Fatalf("expected 1 add, got %d", len(update.Add))
	}
	if update.Add[0].Content != "# Root rules" {
		t.Errorf("unexpected content: %q", update.Add[0].Content)
	}
	if update.Add[0].File != "AGENTS.md" {
		t.Errorf("expected AGENTS.md, got %q", update.Add[0].File)
	}
	if len(update.Remove) != 0 {
		t.Errorf("expected no removes, got %d", len(update.Remove))
	}
}

func TestTracker_FallbackToCLAUDEMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude rules")

	tr := localrules.NewTracker([]string{dir})
	update := tr.UpdateForDir(context.Background(), dir)

	if len(update.Add) != 1 {
		t.Fatalf("expected 1 add, got %d", len(update.Add))
	}
	if update.Add[0].File != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md fallback, got %q", update.Add[0].File)
	}
}

func TestTracker_AGENTSMDTakesPrecedenceOverCLAUDEMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "# Agents rules")
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude rules")

	tr := localrules.NewTracker([]string{dir})
	update := tr.UpdateForDir(context.Background(), dir)

	if len(update.Add) != 1 {
		t.Fatalf("expected 1 add, got %d", len(update.Add))
	}
	if update.Add[0].File != "AGENTS.md" {
		t.Errorf("expected AGENTS.md to take precedence, got %q", update.Add[0].File)
	}
}

func TestTracker_NoRulesFile_EmptyUpdate(t *testing.T) {
	dir := t.TempDir()

	tr := localrules.NewTracker([]string{dir})
	update := tr.UpdateForDir(context.Background(), dir)

	if update.HasChanges() {
		t.Errorf("expected no changes for dir with no rules file, got %+v", update)
	}
}

func TestTracker_EnterSubdir_AdditiveLayers(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Root")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "# Src")

	tr := localrules.NewTracker([]string{root})

	// Enter root.
	update := tr.UpdateForDir(context.Background(), root)
	if len(update.Add) != 1 || update.Add[0].Content != "# Root" {
		t.Fatalf("entering root: expected 1 add with root content, got %+v", update)
	}

	// Enter sub.
	update = tr.UpdateForDir(context.Background(), sub)
	if len(update.Add) != 1 {
		t.Fatalf("entering sub: expected 1 add, got %d adds", len(update.Add))
	}
	if update.Add[0].Content != "# Src" {
		t.Errorf("entering sub: unexpected content %q", update.Add[0].Content)
	}
	if len(update.Remove) != 0 {
		t.Errorf("entering sub: expected no removes, got %d", len(update.Remove))
	}
}

func TestTracker_MoveToSibling_RemovesDeepRules(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Root")
	writeFile(t, filepath.Join(src, "AGENTS.md"), "# Src")
	writeFile(t, filepath.Join(pkg, "AGENTS.md"), "# Pkg")

	tr := localrules.NewTracker([]string{root})

	// Enter src.
	tr.UpdateForDir(context.Background(), root)
	tr.UpdateForDir(context.Background(), src)

	// Move to pkg (sibling of src).
	update := tr.UpdateForDir(context.Background(), pkg)

	// src rules should be removed; pkg rules should be added.
	if len(update.Remove) != 1 || update.Remove[0].Dir != src {
		t.Errorf("expected src to be removed, got removes: %+v", update.Remove)
	}
	if len(update.Add) != 1 || update.Add[0].Content != "# Pkg" {
		t.Errorf("expected pkg to be added, got adds: %+v", update.Add)
	}
}

func TestTracker_MoveBackToParent_RemovesChildRules(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Root")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "# Sub")

	tr := localrules.NewTracker([]string{root})
	tr.UpdateForDir(context.Background(), root)
	tr.UpdateForDir(context.Background(), sub)

	// Move back up to root.
	update := tr.UpdateForDir(context.Background(), root)

	if len(update.Remove) != 1 || update.Remove[0].Dir != sub {
		t.Errorf("expected sub rules removed, got %+v", update.Remove)
	}
	if len(update.Add) != 0 {
		t.Errorf("expected no new adds, got %+v", update.Add)
	}
}

func TestTracker_Reset_ClearsStack(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Root")

	tr := localrules.NewTracker([]string{root})
	tr.UpdateForDir(context.Background(), root) // load root

	tr.Reset()

	// After reset, visiting root again should re-add rules.
	update := tr.UpdateForDir(context.Background(), root)
	if len(update.Add) != 1 {
		t.Errorf("expected rules re-added after reset, got %+v", update)
	}
}

func TestTracker_PathOutsideAllowedDirs_EmptyUpdate(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "AGENTS.md"), "# Outside")

	tr := localrules.NewTracker([]string{allowed})
	update := tr.UpdateForDir(context.Background(), outside)

	if update.HasChanges() {
		t.Errorf("expected no changes for path outside allowed dirs, got %+v", update)
	}
}

func TestTracker_DeepSubdir_LoadsIntermediateDirsWithRules(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "a", "b")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Root")
	// a/ has no rules file
	writeFile(t, filepath.Join(mid, "AGENTS.md"), "# Mid")
	// deep/ has no rules file

	tr := localrules.NewTracker([]string{root})
	update := tr.UpdateForDir(context.Background(), deep)

	// Should load root and mid, skip a/ and deep/ (no rules files).
	if len(update.Add) != 2 {
		t.Fatalf("expected 2 adds (root + mid), got %d: %+v", len(update.Add), update.Add)
	}
	dirs := map[string]bool{update.Add[0].Dir: true, update.Add[1].Dir: true}
	if !dirs[root] || !dirs[mid] {
		t.Errorf("expected root and mid in adds, got %+v", update.Add)
	}
}
