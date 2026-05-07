// Package rules provides directory-aware rules file loading for the agentic loop.
// Rules files (AGENTS.md, with CLAUDE.md as fallback) are loaded hierarchically
// as the bot navigates the directory tree and removed when moving back to a parent.
package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type frame struct {
	dir     string
	file    string // AGENTS.md or CLAUDE.md; empty if no rules file in this dir
	content string
}

// Tracker implements domain.RulesTracker using the local filesystem.
type Tracker struct {
	mu          sync.Mutex
	allowedDirs []string
	stack       []frame
}

// NewTracker creates a Tracker that reads rules files from allowedDirs.
func NewTracker(allowedDirs []string) *Tracker {
	cleaned := make([]string, len(allowedDirs))
	for i, d := range allowedDirs {
		cleaned[i] = filepath.Clean(d)
	}
	return &Tracker{allowedDirs: cleaned}
}

// Reset clears all tracked state. Call before each new task.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stack = t.stack[:0]
}

// UpdateForDir returns the rules delta for entering dir. New rules files on the
// path from the current stack top to dir are returned in Add; files no longer on
// the current path are in Remove.
func (t *Tracker) UpdateForDir(_ context.Context, dir string) domain.RulesUpdate {
	dir = filepath.Clean(dir)

	t.mu.Lock()
	defer t.mu.Unlock()

	root := t.allowedRoot(dir)
	if root == "" {
		return domain.RulesUpdate{} // outside allowed dirs
	}

	target := dirSequence(root, dir)

	// Find common prefix length with current stack.
	commonLen := 0
	for i, f := range t.stack {
		if i >= len(target) || f.dir != target[i] {
			break
		}
		commonLen = i + 1
	}

	// Pop frames beyond the common prefix and collect removes.
	var removed []domain.RulesEntry
	for i := len(t.stack) - 1; i >= commonLen; i-- {
		if t.stack[i].content != "" {
			removed = append(removed, domain.RulesEntry{
				Dir:  t.stack[i].dir,
				File: t.stack[i].file,
			})
		}
	}
	t.stack = t.stack[:commonLen]

	// Push new frames and collect adds.
	var added []domain.RulesEntry
	for _, d := range target[commonLen:] {
		content, file := loadRulesFile(d)
		t.stack = append(t.stack, frame{dir: d, file: file, content: content})
		if content != "" {
			added = append(added, domain.RulesEntry{Dir: d, File: file, Content: content})
		}
	}

	return domain.RulesUpdate{Add: added, Remove: removed}
}

// allowedRoot returns the allowed root that contains dir, or "" if none.
func (t *Tracker) allowedRoot(dir string) string {
	for _, root := range t.allowedDirs {
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return root
		}
	}
	return ""
}

// dirSequence returns the ordered list of directories from root down to dir.
func dirSequence(root, dir string) []string {
	if root == dir {
		return []string{root}
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return []string{root}
	}
	parts := strings.Split(rel, string(filepath.Separator))
	seq := make([]string, 0, len(parts)+1)
	seq = append(seq, root)
	current := root
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		current = filepath.Join(current, p)
		seq = append(seq, current)
	}
	return seq
}

// loadRulesFile reads AGENTS.md or CLAUDE.md from dir (AGENTS.md takes precedence).
func loadRulesFile(dir string) (content, file string) {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return string(data), name
		}
	}
	return "", ""
}
