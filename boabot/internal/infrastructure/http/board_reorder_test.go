package httpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// TestBoardReorder_UpdatesOrder verifies that POST /api/v1/board/reorder with a
// valid ids payload returns 204 and invokes Reorder with those IDs.
func TestBoardReorder_UpdatesOrder(t *testing.T) {
	t.Parallel()

	var gotIDs []string
	board := &fakeBoardStore{
		reorderFn: func(_ context.Context, ids []string) error {
			gotIDs = ids
			return nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  board,
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"ids":["b","a"]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board/reorder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if len(gotIDs) != 2 || gotIDs[0] != "b" || gotIDs[1] != "a" {
		t.Errorf("expected Reorder called with [b a], got %v", gotIDs)
	}
}

// TestBoardReorder_RequiresAuth verifies that an unauthenticated request to
// the reorder endpoint returns 401.
func TestBoardReorder_RequiresAuth(t *testing.T) {
	t.Parallel()

	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"ids":["a","b"]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board/reorder", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestBoardReorder_EmptyIDs_Returns400 verifies that an empty ids array
// returns 400 Bad Request.
func TestBoardReorder_EmptyIDs_Returns400(t *testing.T) {
	t.Parallel()

	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"ids":[]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board/reorder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Ensure fakeBoardStore used in this file satisfies domain.BoardStore.
// The Reorder method is added via reorderFn; all other methods have defaults
// inherited from the base struct in server_test.go.
var _ domain.BoardStore = (*fakeBoardStore)(nil)
