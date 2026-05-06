//go:build integration

package backup

import (
	"context"
	"os"
	"testing"
)

// TestIntegration_Backup requires a real GitHub PAT in BOABOT_BACKUP_TOKEN
// and a repository configured via BOABOT_BACKUP_REPO_URL.
// Run with: go test -tags integration ./internal/infrastructure/github/backup/...
func TestIntegration_Backup(t *testing.T) {
	token := os.Getenv("BOABOT_BACKUP_TOKEN")
	if token == "" {
		t.Skip("BOABOT_BACKUP_TOKEN not set; skipping integration test")
	}
	repoURL := os.Getenv("BOABOT_BACKUP_REPO_URL")
	if repoURL == "" {
		t.Skip("BOABOT_BACKUP_REPO_URL not set; skipping integration test")
	}

	memPath := t.TempDir()
	// Seed the directory with a test file.
	if err := os.WriteFile(memPath+"/test.txt", []byte("hello backup"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		RepoURL:     repoURL,
		Branch:      "main",
		AuthorName:  "BaoBot Integration Test",
		AuthorEmail: "baobot-test@example.com",
		MemoryPath:  memPath,
		Token:       token,
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := g.Backup(ctx); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	st, err := g.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.LastBackupAt.IsZero() {
		t.Error("expected LastBackupAt to be set after backup")
	}
}
