// Package backup provides a GitHub-backed implementation of domain.MemoryBackup.
// It uses go-git to commit and push changes in the memory directory to a remote
// GitHub repository, enabling durable off-box backup without AWS infrastructure.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Config holds configuration for the GitHub backup adapter.
type Config struct {
	// RepoURL is the remote repository URL,
	// e.g. "https://github.com/owner/boabot-memory.git".
	RepoURL string

	// Branch is the branch to push to. Defaults to "main" if empty.
	Branch string

	// AuthorName is the git commit author name.
	AuthorName string

	// AuthorEmail is the git commit author email.
	AuthorEmail string

	// MemoryPath is the local directory to back up (= memory.path in config).
	MemoryPath string

	// Token is the GitHub personal access token. Read from BOABOT_BACKUP_TOKEN
	// or a credentials file at runtime.
	Token string
}

func (c *Config) branch() string {
	if c.Branch == "" {
		return "main"
	}
	return c.Branch
}

// gitRepo abstracts *gogit.Repository for unit-testability.
// Push and Pull live here so tests can control both without real disk I/O.
type gitRepo interface {
	Worktree() (*gogit.Worktree, error)
	Head() (*plumbing.Reference, error)
	CommitObject(h plumbing.Hash) (*object.Commit, error)
	Push(opts *gogit.PushOptions) error
	Pull(ctx context.Context, opts *gogit.PullOptions) error
}

// repoOpener is an injectable function for opening or initialising a git repo.
type repoOpener func(path string) (gitRepo, error)

// repoWrapper wraps *gogit.Repository to satisfy the gitRepo interface.
type repoWrapper struct{ r *gogit.Repository }

func (w *repoWrapper) Worktree() (*gogit.Worktree, error) { return w.r.Worktree() }
func (w *repoWrapper) Head() (*plumbing.Reference, error) { return w.r.Head() }
func (w *repoWrapper) CommitObject(h plumbing.Hash) (*object.Commit, error) {
	return w.r.CommitObject(h)
}
func (w *repoWrapper) Push(opts *gogit.PushOptions) error { return w.r.Push(opts) }
func (w *repoWrapper) Pull(ctx context.Context, opts *gogit.PullOptions) error {
	wt, err := w.r.Worktree()
	if err != nil {
		return fmt.Errorf("pull: worktree: %w", err)
	}
	return wt.PullContext(ctx, opts)
}

// GitHubBackup implements domain.MemoryBackup backed by a GitHub repository.
type GitHubBackup struct {
	cfg    Config
	opener repoOpener
}

// New constructs a GitHubBackup. It validates the config but does not perform
// any I/O.
func New(cfg Config) (*GitHubBackup, error) {
	if cfg.RepoURL == "" {
		return nil, fmt.Errorf("github backup: RepoURL is required")
	}
	if cfg.MemoryPath == "" {
		return nil, fmt.Errorf("github backup: MemoryPath is required")
	}
	g := &GitHubBackup{cfg: cfg}
	g.opener = g.defaultOpen
	return g, nil
}

// defaultOpen attempts PlainOpen; falls back to PlainInit on ErrRepositoryNotExists.
func (g *GitHubBackup) defaultOpen(path string) (gitRepo, error) {
	r, err := gogit.PlainOpen(path)
	if err == nil {
		return &repoWrapper{r}, nil
	}
	if !errors.Is(err, gogit.ErrRepositoryNotExists) {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	// Not a repo yet — initialise one.
	r, err = gogit.PlainInit(path, false)
	if err != nil {
		return nil, fmt.Errorf("init repo: %w", err)
	}
	return &repoWrapper{r}, nil
}

func (g *GitHubBackup) auth() *gogithttp.BasicAuth {
	return &gogithttp.BasicAuth{Username: "token", Password: g.cfg.Token}
}

// Backup stages all files in MemoryPath, commits them, and pushes to remote.
// If nothing has changed since the last commit it returns nil without pushing.
func (g *GitHubBackup) Backup(ctx context.Context) error {
	start := time.Now()

	r, err := g.opener(g.cfg.MemoryPath)
	if err != nil {
		return fmt.Errorf("github backup: %w", err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("github backup: worktree: %w", err)
	}

	if err := wt.AddGlob("."); err != nil {
		return fmt.Errorf("github backup: add: %w", err)
	}

	st, err := wt.Status()
	if err != nil {
		return fmt.Errorf("github backup: status: %w", err)
	}

	// Count staged changes.
	staged := countStaged(st)
	if staged == 0 {
		slog.InfoContext(ctx, "memory backup skipped: nothing changed")
		return nil
	}

	commitMsg := "backup: " + time.Now().UTC().Format(time.RFC3339)
	sig := &object.Signature{
		Name:  g.cfg.AuthorName,
		Email: g.cfg.AuthorEmail,
		When:  time.Now().UTC(),
	}
	if _, err := wt.Commit(commitMsg, &gogit.CommitOptions{Author: sig}); err != nil {
		return fmt.Errorf("github backup: commit: %w", err)
	}

	pushOpts := &gogit.PushOptions{
		RemoteName: "origin",
		Auth:       g.auth(),
	}
	if err := g.pushWithRetry(ctx, r, pushOpts); err != nil {
		return fmt.Errorf("github backup: %w", err)
	}

	slog.InfoContext(ctx, "memory backup complete",
		"files_changed", staged,
		"duration", time.Since(start).String(),
	)
	return nil
}

// pushWithRetry attempts a push via the repo; on diverged-remote error it pulls
// then retries once.
func (g *GitHubBackup) pushWithRetry(ctx context.Context, r gitRepo, opts *gogit.PushOptions) error {
	err := r.Push(opts)
	if err == nil || errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}

	// Attempt pull-then-retry for diverged remote.
	pullErr := r.Pull(ctx, &gogit.PullOptions{
		RemoteName: "origin",
		Auth:       g.auth(),
		Force:      true,
	})
	if pullErr != nil && !errors.Is(pullErr, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push: pull before retry: %w", pullErr)
	}

	retryErr := r.Push(opts)
	if retryErr != nil && !errors.Is(retryErr, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push (retry): %w", retryErr)
	}
	return nil
}

// Restore clones the remote repository into MemoryPath if it does not exist,
// or pulls the latest changes if it already does.
func (g *GitHubBackup) Restore(ctx context.Context) error {
	r, err := gogit.PlainOpen(g.cfg.MemoryPath)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		// Clone fresh.
		_, cloneErr := gogit.PlainCloneContext(ctx, g.cfg.MemoryPath, false, &gogit.CloneOptions{
			URL:           g.cfg.RepoURL,
			Auth:          g.auth(),
			ReferenceName: plumbing.NewBranchReferenceName(g.cfg.branch()),
			SingleBranch:  true,
		})
		if cloneErr != nil {
			return fmt.Errorf("github backup: clone: %w", cloneErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("github backup: open for restore: %w", err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("github backup: restore worktree: %w", err)
	}
	pullErr := wt.PullContext(ctx, &gogit.PullOptions{
		RemoteName: "origin",
		Auth:       g.auth(),
		Force:      true,
	})
	if pullErr != nil && !errors.Is(pullErr, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("github backup: restore pull: %w", pullErr)
	}
	return nil
}

// Status returns the current backup state: last commit timestamp, number of
// uncommitted files, and the configured remote URL.
// If MemoryPath is not yet a git repository, a zero BackupStatus is returned
// with a nil error.
func (g *GitHubBackup) Status(_ context.Context) (domain.BackupStatus, error) {
	r, err := gogit.PlainOpen(g.cfg.MemoryPath)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		return domain.BackupStatus{RemoteURL: g.cfg.RepoURL}, nil
	}
	if err != nil {
		return domain.BackupStatus{}, fmt.Errorf("github backup: status open: %w", err)
	}

	var lastBackupAt time.Time
	ref, err := r.Head()
	if err == nil {
		commit, cerr := r.CommitObject(ref.Hash())
		if cerr == nil {
			lastBackupAt = commit.Author.When
		}
	}

	wt, err := r.Worktree()
	if err != nil {
		return domain.BackupStatus{}, fmt.Errorf("github backup: status worktree: %w", err)
	}
	st, err := wt.Status()
	if err != nil {
		return domain.BackupStatus{}, fmt.Errorf("github backup: status check: %w", err)
	}

	return domain.BackupStatus{
		LastBackupAt:   lastBackupAt,
		PendingChanges: len(st),
		RemoteURL:      g.cfg.RepoURL,
	}, nil
}

// countStaged returns the number of files with a staged (index) change.
func countStaged(st gogit.Status) int {
	n := 0
	for _, fs := range st {
		if fs.Staging != gogit.Unmodified {
			n++
		}
	}
	return n
}
