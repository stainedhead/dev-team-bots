package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// helper to build a test server and return an HTTPClient pointed at it.
func newTestClient(t *testing.T, handler http.Handler) *client.HTTPClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return client.NewHTTPClient(srv.URL, func() string { return "test-token" })
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func TestHTTPClient_Login_Success(t *testing.T) {
	want := domain.LoginResponse{Token: "jwt-abc", MustChangePassword: false}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// must NOT require auth token for login
		writeJSON(w, http.StatusOK, want)
	}))
	defer srv.Close()

	c := client.NewHTTPClient(srv.URL, func() string { return "" })
	got, err := c.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != want.Token {
		t.Errorf("token: got %q want %q", got.Token, want.Token)
	}
}

func TestHTTPClient_Login_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	}))
	_, err := c.Login(context.Background(), "alice", "wrong")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error should contain 'invalid credentials', got %q", err.Error())
	}
}

func TestHTTPClient_Login_5xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := c.Login(context.Background(), "alice", "secret")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── Board ─────────────────────────────────────────────────────────────────────

func TestHTTPClient_BoardList_Success(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	want := []domain.WorkItem{{ID: "wi-1", Title: "Do thing", Status: "open", CreatedAt: now}}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/board" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing auth header")
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.BoardList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "wi-1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestHTTPClient_BoardList_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "forbidden")
	}))
	_, err := c.BoardList(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("got %q", err.Error())
	}
}

func TestHTTPClient_BoardGet_Success(t *testing.T) {
	want := domain.WorkItem{ID: "wi-2", Title: "Fix bug"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/board/wi-2" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.BoardGet(context.Background(), "wi-2")
	if err != nil || got.ID != "wi-2" {
		t.Fatalf("got err=%v item=%+v", err, got)
	}
}

func TestHTTPClient_BoardCreate_Success(t *testing.T) {
	want := domain.WorkItem{ID: "wi-3", Title: "New item"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/board" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusCreated, want)
	}))
	got, err := c.BoardCreate(context.Background(), domain.CreateWorkItemRequest{Title: "New item"})
	if err != nil || got.ID != "wi-3" {
		t.Fatalf("got err=%v item=%+v", err, got)
	}
}

func TestHTTPClient_BoardUpdate_Success(t *testing.T) {
	want := domain.WorkItem{ID: "wi-4", Title: "Updated"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/board/wi-4" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	title := "Updated"
	got, err := c.BoardUpdate(context.Background(), "wi-4", domain.UpdateWorkItemRequest{Title: &title})
	if err != nil || got.ID != "wi-4" {
		t.Fatalf("got err=%v item=%+v", err, got)
	}
}

func TestHTTPClient_BoardAssign_Success(t *testing.T) {
	want := domain.WorkItem{ID: "wi-5", AssignedTo: "bot-a"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/board/wi-5/assign" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.BoardAssign(context.Background(), "wi-5", "bot-a")
	if err != nil || got.AssignedTo != "bot-a" {
		t.Fatalf("got err=%v item=%+v", err, got)
	}
}

func TestHTTPClient_BoardClose_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/board/wi-6/close" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.BoardClose(context.Background(), "wi-6"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_BoardClose_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}))
	err := c.BoardClose(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

// ── Team ──────────────────────────────────────────────────────────────────────

func TestHTTPClient_TeamList_Success(t *testing.T) {
	want := []domain.BotEntry{{Name: "dev-bot", Status: "active"}}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/team" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.TeamList(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "dev-bot" {
		t.Fatalf("got err=%v list=%+v", err, got)
	}
}

func TestHTTPClient_TeamGet_Success(t *testing.T) {
	want := domain.BotEntry{Name: "dev-bot", Status: "active"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/team/dev-bot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.TeamGet(context.Background(), "dev-bot")
	if err != nil || got.Name != "dev-bot" {
		t.Fatalf("got err=%v bot=%+v", err, got)
	}
}

func TestHTTPClient_TeamHealth_Success(t *testing.T) {
	want := domain.TeamHealth{Active: 3, Inactive: 1, Total: 4}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/team/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.TeamHealth(context.Background())
	if err != nil || got.Total != 4 {
		t.Fatalf("got err=%v health=%+v", err, got)
	}
}

func TestHTTPClient_TeamHealth_5xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	_, err := c.TeamHealth(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Skills ────────────────────────────────────────────────────────────────────

func TestHTTPClient_SkillsList_Success(t *testing.T) {
	want := []domain.Skill{{ID: "sk-1", Name: "write-tests"}}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.SkillsList(context.Background(), "")
	if err != nil || len(got) != 1 {
		t.Fatalf("got err=%v skills=%+v", err, got)
	}
}

func TestHTTPClient_SkillsList_WithBot(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bot") != "dev-bot" {
			t.Errorf("expected bot query param, got %s", r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, []domain.Skill{})
	}))
	_, err := c.SkillsList(context.Background(), "dev-bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_SkillsApprove_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/skills/sk-1/approve" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.SkillsApprove(context.Background(), "sk-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_SkillsReject_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/skills/sk-1/reject" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.SkillsReject(context.Background(), "sk-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_SkillsRevoke_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/skills/sk-1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.SkillsRevoke(context.Background(), "sk-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_SkillsRevoke_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "skill not found")
	}))
	err := c.SkillsRevoke(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "skill not found") {
		t.Fatalf("expected 'skill not found' error, got %v", err)
	}
}

// ── User ──────────────────────────────────────────────────────────────────────

func TestHTTPClient_UserList_Success(t *testing.T) {
	want := []domain.User{{Username: "alice", Role: "admin", Enabled: true}}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/users" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.UserList(context.Background())
	if err != nil || len(got) != 1 || got[0].Username != "alice" {
		t.Fatalf("got err=%v users=%+v", err, got)
	}
}

func TestHTTPClient_UserCreate_Success(t *testing.T) {
	want := domain.User{Username: "bob", Role: "user", Enabled: true}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusCreated, want)
	}))
	got, err := c.UserCreate(context.Background(), domain.CreateUserRequest{Username: "bob", Role: "user"})
	if err != nil || got.Username != "bob" {
		t.Fatalf("got err=%v user=%+v", err, got)
	}
}

func TestHTTPClient_UserRemove_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/users/bob" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.UserRemove(context.Background(), "bob"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_UserDisable_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users/bob/disable" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.UserDisable(context.Background(), "bob"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_UserSetPassword_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users/bob/password" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.UserSetPassword(context.Background(), "bob", "newpw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_UserSetRole_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users/bob/role" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.UserSetRole(context.Background(), "bob", "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_UserRemove_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "user not found")
	}))
	err := c.UserRemove(context.Background(), "nobody")
	if err == nil || !strings.Contains(err.Error(), "user not found") {
		t.Fatalf("expected 'user not found' error, got %v", err)
	}
}

// ── Profile ───────────────────────────────────────────────────────────────────

func TestHTTPClient_ProfileGet_Success(t *testing.T) {
	want := domain.User{Username: "alice", DisplayName: "Alice Smith", Role: "admin"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/profile" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.ProfileGet(context.Background())
	if err != nil || got.Username != "alice" {
		t.Fatalf("got err=%v user=%+v", err, got)
	}
}

func TestHTTPClient_ProfileSetName_Success(t *testing.T) {
	want := domain.User{Username: "alice", DisplayName: "Alicia"}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/profile" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	if err := c.ProfileSetName(context.Background(), "Alicia"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ProfileSetPassword_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/profile/password" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.ProfileSetPassword(context.Background(), "old", "new"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ProfileSetPassword_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnprocessableEntity, "weak password")
	}))
	err := c.ProfileSetPassword(context.Background(), "old", "weak")
	if err == nil || !strings.Contains(err.Error(), "weak password") {
		t.Fatalf("expected 'weak password' error, got %v", err)
	}
}

// ── DLQ ───────────────────────────────────────────────────────────────────────

func TestHTTPClient_DLQList_Success(t *testing.T) {
	want := []domain.DLQItem{{ID: "dlq-1", QueueName: "tasks", ReceivedCount: 3}}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/dlq" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, want)
	}))
	got, err := c.DLQList(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "dlq-1" {
		t.Fatalf("got err=%v list=%+v", err, got)
	}
}

func TestHTTPClient_DLQRetry_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/dlq/dlq-1/retry" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.DLQRetry(context.Background(), "dlq-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_DLQDiscard_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/dlq/dlq-1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.DLQDiscard(context.Background(), "dlq-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_DLQDiscard_4xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "item not found")
	}))
	err := c.DLQDiscard(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "item not found") {
		t.Fatalf("expected 'item not found' error, got %v", err)
	}
}

func TestHTTPClient_DLQList_5xx(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := c.DLQList(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Token header omitted when empty ───────────────────────────────────────────

func TestHTTPClient_NoTokenWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", r.Header.Get("Authorization"))
		}
		writeJSON(w, http.StatusOK, []domain.WorkItem{})
	}))
	defer srv.Close()
	c := client.NewHTTPClient(srv.URL, func() string { return "" })
	_, err := c.BoardList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
