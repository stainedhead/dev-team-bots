package domain

import "context"

// RulesEntry represents one rules file loaded from a directory.
type RulesEntry struct {
	Dir     string // absolute directory path
	File    string // AGENTS.md or CLAUDE.md
	Content string // file content; empty for Remove entries
}

// RulesUpdate is the delta returned by RulesTracker on each directory transition.
type RulesUpdate struct {
	Add    []RulesEntry // rules newly entering context
	Remove []RulesEntry // rules leaving context (Content will be empty)
}

// HasChanges reports whether the update contains any additions or removals.
func (u RulesUpdate) HasChanges() bool {
	return len(u.Add) > 0 || len(u.Remove) > 0
}

// RulesTracker tracks which directory rules files are currently in conversation
// context and returns incremental deltas as the bot navigates the directory tree.
//
// Rules files are named AGENTS.md, with CLAUDE.md as a fallback. Files from
// ancestor directories are loaded additively when entering subdirectories and
// removed when moving back to a parent or a different branch.
type RulesTracker interface {
	// UpdateForDir returns the rules delta for entering dir.
	// New rules files discovered on the path from the current location to dir
	// are returned in Add; files no longer on the current path are in Remove.
	UpdateForDir(ctx context.Context, dir string) RulesUpdate
	// Reset clears all tracked state. Call at the start of each new task.
	Reset()
}
