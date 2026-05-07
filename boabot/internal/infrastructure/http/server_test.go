package httpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
)

// ── fake stores ───────────────────────────────────────────────────────────────

type fakeAuth struct {
	loginFn          func(u, p string) (domainauth.Token, error)
	validateTokenFn  func(token string) (domainauth.Claims, error)
	setPasswordFn    func(ctx context.Context, username, newPassword string) error
	verifyPasswordFn func(ctx context.Context, username, password string) error
}

func (f *fakeAuth) Login(u, p string) (domainauth.Token, error) {
	if f.loginFn != nil {
		return f.loginFn(u, p)
	}
	return domainauth.Token{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (f *fakeAuth) ValidateToken(token string) (domainauth.Claims, error) {
	if f.validateTokenFn != nil {
		return f.validateTokenFn(token)
	}
	return domainauth.Claims{Subject: "admin", Role: "admin"}, nil
}
func (f *fakeAuth) OAuthCallback(_, _ string) (domainauth.Token, error) {
	return domainauth.Token{}, errors.New("not implemented")
}
func (f *fakeAuth) SetPassword(ctx context.Context, username, newPassword string) error {
	if f.setPasswordFn != nil {
		return f.setPasswordFn(ctx, username, newPassword)
	}
	return nil
}
func (f *fakeAuth) VerifyPassword(ctx context.Context, username, password string) error {
	if f.verifyPasswordFn != nil {
		return f.verifyPasswordFn(ctx, username, password)
	}
	return nil
}

type fakeBoardStore struct {
	createFn func(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error)
	updateFn func(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error)
	getFn    func(ctx context.Context, id string) (domain.WorkItem, error)
	listFn   func(ctx context.Context, filter domain.WorkItemFilter) ([]domain.WorkItem, error)
	deleteFn func(ctx context.Context, id string) error
}

func (f *fakeBoardStore) Create(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	if f.createFn != nil {
		return f.createFn(ctx, item)
	}
	item.ID = "generated-id"
	return item, nil
}
func (f *fakeBoardStore) Update(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, item)
	}
	return item, nil
}
func (f *fakeBoardStore) Get(ctx context.Context, id string) (domain.WorkItem, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.WorkItem{ID: id, Title: "item"}, nil
}
func (f *fakeBoardStore) List(ctx context.Context, filter domain.WorkItemFilter) ([]domain.WorkItem, error) {
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}
	return []domain.WorkItem{{ID: "1", Title: "first"}}, nil
}
func (f *fakeBoardStore) Delete(ctx context.Context, id string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}

type fakeControlPlane struct {
	listFn func(ctx context.Context) ([]domain.BotEntry, error)
	getFn  func(ctx context.Context, name string) (domain.BotEntry, error)
}

func (f *fakeControlPlane) Register(ctx context.Context, entry domain.BotEntry) error { return nil }
func (f *fakeControlPlane) Deregister(ctx context.Context, name string) error         { return nil }
func (f *fakeControlPlane) UpdateHeartbeat(ctx context.Context, name string) error    { return nil }
func (f *fakeControlPlane) Get(ctx context.Context, name string) (domain.BotEntry, error) {
	if f.getFn != nil {
		return f.getFn(ctx, name)
	}
	return domain.BotEntry{Name: name, BotType: "developer"}, nil
}
func (f *fakeControlPlane) List(ctx context.Context) ([]domain.BotEntry, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return []domain.BotEntry{{Name: "dev-1", BotType: "developer", Status: domain.BotStatusActive}}, nil
}
func (f *fakeControlPlane) IsTypeActive(ctx context.Context, botType string) (bool, error) {
	return true, nil
}

type fakeUserStore struct {
	createFn func(ctx context.Context, u domain.User) (domain.User, error)
	updateFn func(ctx context.Context, u domain.User) (domain.User, error)
	deleteFn func(ctx context.Context, username string) error
	getFn    func(ctx context.Context, username string) (domain.User, error)
	listFn   func(ctx context.Context) ([]domain.User, error)
}

func (f *fakeUserStore) Create(ctx context.Context, u domain.User) (domain.User, error) {
	if f.createFn != nil {
		return f.createFn(ctx, u)
	}
	return u, nil
}
func (f *fakeUserStore) Update(ctx context.Context, u domain.User) (domain.User, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, u)
	}
	return u, nil
}
func (f *fakeUserStore) Delete(ctx context.Context, username string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, username)
	}
	return nil
}
func (f *fakeUserStore) Get(ctx context.Context, username string) (domain.User, error) {
	if f.getFn != nil {
		return f.getFn(ctx, username)
	}
	return domain.User{Username: username, Role: domain.UserRoleUser, Enabled: true}, nil
}
func (f *fakeUserStore) List(ctx context.Context) ([]domain.User, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return []domain.User{{Username: "alice", Role: domain.UserRoleAdmin}}, nil
}

type fakeSkillRegistry struct {
	listFn    func(ctx context.Context, botType string, status domain.SkillStatus) ([]domain.Skill, error)
	getFn     func(ctx context.Context, id string) (domain.Skill, error)
	stageFn   func(ctx context.Context, name, botType string, files map[string][]byte) (domain.Skill, error)
	approveFn func(ctx context.Context, id string) error
	rejectFn  func(ctx context.Context, id string) error
	revokeFn  func(ctx context.Context, id string) error
}

func (f *fakeSkillRegistry) List(ctx context.Context, botType string, status domain.SkillStatus) ([]domain.Skill, error) {
	if f.listFn != nil {
		return f.listFn(ctx, botType, status)
	}
	return []domain.Skill{{ID: "s1", Name: "tdd-skill"}}, nil
}
func (f *fakeSkillRegistry) Get(ctx context.Context, id string) (domain.Skill, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.Skill{ID: id}, nil
}
func (f *fakeSkillRegistry) Stage(ctx context.Context, name, botType string, files map[string][]byte) (domain.Skill, error) {
	if f.stageFn != nil {
		return f.stageFn(ctx, name, botType, files)
	}
	return domain.Skill{ID: "staged-1", Name: name, Status: domain.SkillStatusStaged}, nil
}
func (f *fakeSkillRegistry) Approve(ctx context.Context, id string) error {
	if f.approveFn != nil {
		return f.approveFn(ctx, id)
	}
	return nil
}
func (f *fakeSkillRegistry) Reject(ctx context.Context, id string) error {
	if f.rejectFn != nil {
		return f.rejectFn(ctx, id)
	}
	return nil
}
func (f *fakeSkillRegistry) Revoke(ctx context.Context, id string) error {
	if f.revokeFn != nil {
		return f.revokeFn(ctx, id)
	}
	return nil
}

type fakeDLQStore struct {
	listFn    func(ctx context.Context) ([]domain.DLQItem, error)
	retryFn   func(ctx context.Context, id string) error
	discardFn func(ctx context.Context, id string) error
}

func (f *fakeDLQStore) List(ctx context.Context) ([]domain.DLQItem, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return []domain.DLQItem{{ID: "dlq-1", QueueName: "worker-dlq", Body: "{}"}}, nil
}
func (f *fakeDLQStore) Retry(ctx context.Context, id string) error {
	if f.retryFn != nil {
		return f.retryFn(ctx, id)
	}
	return nil
}
func (f *fakeDLQStore) Discard(ctx context.Context, id string) error {
	if f.discardFn != nil {
		return f.discardFn(ctx, id)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestServer(auth httpserver.AuthProvider) *httptest.Server {
	s := httpserver.New(httpserver.Config{
		Auth:   auth,
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	return httptest.NewServer(s.Handler())
}

func authHeader() string { return "Bearer valid-token" }

func doJSON(t *testing.T, srv *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	var br *strings.Reader
	if body != "" {
		br = strings.NewReader(body)
	} else {
		br = strings.NewReader("")
	}
	req, err := http.NewRequest(method, srv.URL+"/api/v1/"+path, br)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", authHeader())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	}).Handler())
	defer srv.Close()

	body := `{"username":"admin","password":"secret"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLogin_InvalidCredentials_Returns401(t *testing.T) {
	auth := &fakeAuth{loginFn: func(_, _ string) (domainauth.Token, error) {
		return domainauth.Token{}, domainauth.ErrInvalidCredentials
	}}
	srv := httptest.NewServer(httpserver.New(httpserver.Config{
		Auth: auth, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	}).Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login",
		strings.NewReader(`{"username":"x","password":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestProtectedRoute_NoToken_Returns401 verifies that write endpoints (POST /board)
// still require authentication. GET /board is intentionally public.
func TestProtectedRoute_NoToken_Returns401(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board", strings.NewReader(`{"title":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for POST /board without token, got %d", resp.StatusCode)
	}
}

// TestBoardGet_Public verifies that GET /api/v1/board does NOT require a token.
func TestBoardGet_Public(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board", nil)
	// Deliberately no Authorization header.
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for public GET /board, got %d", resp.StatusCode)
	}
}

// TestTeamHealth_Public verifies that GET /api/v1/team/health does NOT require a token.
func TestTeamHealth_Public(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/team/health", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for public GET /team/health, got %d", resp.StatusCode)
	}
}

func TestProtectedRoute_InvalidToken_Returns401(t *testing.T) {
	auth := &fakeAuth{validateTokenFn: func(_ string) (domainauth.Claims, error) {
		return domainauth.Claims{}, domainauth.ErrInvalidCredentials
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	// POST /board is still protected; verify bad token returns 401.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board", strings.NewReader(`{"title":"t"}`))
	req.Header.Set("Authorization", "Bearer bad-token")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ── Board ─────────────────────────────────────────────────────────────────────

func TestBoard_List(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "board", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var items []domain.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one item")
	}
}

func TestBoard_Get(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "board/abc-123", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var item domain.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ID != "abc-123" {
		t.Fatalf("expected ID abc-123, got %s", item.ID)
	}
}

func TestBoard_Get_NotFound_Returns404(t *testing.T) {
	board := &fakeBoardStore{getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "board/missing", "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestBoard_Create(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"title":"new item","description":"desc"}`
	resp := doJSON(t, srv, http.MethodPost, "board", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var item domain.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ID == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestBoard_Update(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"title":"updated"}`
	resp := doJSON(t, srv, http.MethodPatch, "board/abc-123", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBoard_Assign(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"bot_id":"dev-1"}`
	resp := doJSON(t, srv, http.MethodPost, "board/abc-123/assign", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBoard_Close(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/abc-123/close", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ── Team ──────────────────────────────────────────────────────────────────────

func TestTeam_List(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var bots []domain.BotEntry
	if err := json.NewDecoder(resp.Body).Decode(&bots); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(bots) == 0 {
		t.Fatal("expected at least one bot")
	}
}

func TestTeam_Health(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team/health", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var h struct {
		Active   int `json:"active"`
		Inactive int `json:"inactive"`
		Total    int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Total != h.Active+h.Inactive {
		t.Fatalf("total mismatch: %d != %d+%d", h.Total, h.Active, h.Inactive)
	}
}

func TestTeam_Get(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team/dev-1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var bot domain.BotEntry
	if err := json.NewDecoder(resp.Body).Decode(&bot); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bot.Name != "dev-1" {
		t.Fatalf("expected Name=dev-1, got %s", bot.Name)
	}
}

// ── Skills ────────────────────────────────────────────────────────────────────

func TestSkills_List(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "skills", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var skills []domain.Skill
	if err := json.NewDecoder(resp.Body).Decode(&skills); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("expected at least one skill")
	}
}

func TestSkills_Approve(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "skills/s1/approve", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestSkills_Reject(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "skills/s1/reject", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestSkills_Revoke(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "skills/s1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ── Users ─────────────────────────────────────────────────────────────────────

func TestUsers_List(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "users", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var users []domain.User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least one user")
	}
}

func TestUsers_Create(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"username":"bob","role":"user","password":"pass123"}`
	resp := doJSON(t, srv, http.MethodPost, "users", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestUsers_Remove(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "users/bob", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestUsers_NonAdmin_Returns403(t *testing.T) {
	auth := &fakeAuth{validateTokenFn: func(_ string) (domainauth.Claims, error) {
		return domainauth.Claims{Subject: "alice", Role: "user"}, nil
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "users", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin user list, got %d", resp.StatusCode)
	}
}

// ── Profile ───────────────────────────────────────────────────────────────────

func TestProfile_Get(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "profile", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var u domain.User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Username == "" {
		t.Fatal("expected non-empty username")
	}
}

func TestProfile_SetName(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"display_name":"Admin User"}`
	resp := doJSON(t, srv, http.MethodPatch, "profile", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestProfile_SetPassword(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"old_password":"old","new_password":"new"}`
	resp := doJSON(t, srv, http.MethodPost, "profile/password", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ── DLQ ───────────────────────────────────────────────────────────────────────

func TestDLQ_List(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "dlq", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var items []domain.DLQItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one DLQ item")
	}
}

func TestDLQ_Retry(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "dlq/dlq-1/retry", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestDLQ_Discard(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "dlq/dlq-1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ── Web UI ────────────────────────────────────────────────────────────────────

// TestKanbanUI_HasVanillaJS verifies that the Kanban UI uses vanilla JS (no
// external CDN dependencies) and contains the expected data-fetching calls.
func TestKanbanUI_HasVanillaJS(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf strings.Builder
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := buf.String()
	// Must NOT load htmx from an external CDN.
	if strings.Contains(body, "unpkg.com/htmx") {
		t.Error("kanban UI must not load htmx from external CDN")
	}
	// Must reference the board API endpoint.
	if !strings.Contains(body, "/api/v1/board") {
		t.Error("kanban UI must reference /api/v1/board")
	}
	// Must use setInterval for auto-refresh.
	if !strings.Contains(body, "setInterval(") {
		t.Error("kanban UI must use setInterval for periodic refresh")
	}
	// Must fetch team data.
	if !strings.Contains(body, "/api/v1/team") {
		t.Error("kanban UI must reference /api/v1/team")
	}
}

func TestKanbanUI_ServesHTML(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected HTML content-type, got %s", ct)
	}
}

// ── Security fixes ────────────────────────────────────────────────────────────

func TestProfile_SetPassword_WrongOldPassword_Returns401(t *testing.T) {
	auth := &fakeAuth{
		verifyPasswordFn: func(_ context.Context, _, _ string) error {
			return errors.New("password mismatch")
		},
	}
	srv := newTestServer(auth)
	defer srv.Close()

	body := `{"old_password":"wrong","new_password":"new123"}`
	resp := doJSON(t, srv, http.MethodPost, "profile/password", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when old password is wrong, got %d", resp.StatusCode)
	}
}

func TestProfile_SetPassword_EmptyNew_Returns400(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"old_password":"old","new_password":""}`
	resp := doJSON(t, srv, http.MethodPost, "profile/password", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty new password, got %d", resp.StatusCode)
	}
}

func TestUserSetPassword_CallsAuthSetPassword(t *testing.T) {
	var setCalled bool
	auth := &fakeAuth{
		setPasswordFn: func(_ context.Context, username, pw string) error {
			setCalled = true
			if username != "bob" {
				return errors.New("wrong username")
			}
			if pw == "" {
				return errors.New("empty password")
			}
			return nil
		},
	}
	srv := newTestServer(auth)
	defer srv.Close()

	body := `{"password":"securepass"}`
	resp := doJSON(t, srv, http.MethodPost, "users/bob/password", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if !setCalled {
		t.Fatal("expected Auth.SetPassword to be called")
	}
}

func TestUserSetPassword_Empty_Returns400(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"password":""}`
	resp := doJSON(t, srv, http.MethodPost, "users/bob/password", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty password, got %d", resp.StatusCode)
	}
}

// ── Users (additional) ────────────────────────────────────────────────────────

func TestUsers_Disable(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/bob/disable", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestUsers_Disable_NotFound_Returns404(t *testing.T) {
	users := &fakeUserStore{getFn: func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/nobody/disable", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUsers_SetRole(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	body := `{"role":"admin"}`
	resp := doJSON(t, srv, http.MethodPost, "users/bob/role", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestUsers_SetRole_NotFound_Returns404(t *testing.T) {
	users := &fakeUserStore{getFn: func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"role":"admin"}`
	resp := doJSON(t, srv, http.MethodPost, "users/nobody/role", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ── Error paths (store failures) ──────────────────────────────────────────────

func TestBoard_List_StoreError_Returns500(t *testing.T) {
	board := &fakeBoardStore{listFn: func(_ context.Context, _ domain.WorkItemFilter) ([]domain.WorkItem, error) {
		return nil, errors.New("db unavailable")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "board", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBoard_Create_StoreError_Returns500(t *testing.T) {
	board := &fakeBoardStore{createFn: func(_ context.Context, _ domain.WorkItem) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("db write error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board", `{"title":"t"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBoard_Update_StoreError_Returns500(t *testing.T) {
	board := &fakeBoardStore{updateFn: func(_ context.Context, _ domain.WorkItem) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("update failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPatch, "board/abc-123", `{"title":"t"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBoard_Assign_StoreError_Returns500(t *testing.T) {
	board := &fakeBoardStore{updateFn: func(_ context.Context, _ domain.WorkItem) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("assign failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/abc-123/assign", `{"bot_id":"dev-1"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestBoard_Close_StoreError_Returns500(t *testing.T) {
	board := &fakeBoardStore{updateFn: func(_ context.Context, _ domain.WorkItem) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("close failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/abc-123/close", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestTeam_List_StoreError_Returns500(t *testing.T) {
	team := &fakeControlPlane{listFn: func(_ context.Context) ([]domain.BotEntry, error) {
		return nil, errors.New("list failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: team,
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestTeam_Get_NotFound_Returns404(t *testing.T) {
	team := &fakeControlPlane{getFn: func(_ context.Context, _ string) (domain.BotEntry, error) {
		return domain.BotEntry{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: team,
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team/missing", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTeam_Health_StoreError_Returns500(t *testing.T) {
	team := &fakeControlPlane{listFn: func(_ context.Context) ([]domain.BotEntry, error) {
		return nil, errors.New("health check failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: team,
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "team/health", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestSkills_List_StoreError_Returns500(t *testing.T) {
	skills := &fakeSkillRegistry{listFn: func(_ context.Context, _ string, _ domain.SkillStatus) ([]domain.Skill, error) {
		return nil, errors.New("list failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: skills, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "skills", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestDLQ_List_StoreError_Returns500(t *testing.T) {
	dlq := &fakeDLQStore{listFn: func(_ context.Context) ([]domain.DLQItem, error) {
		return nil, errors.New("sqs unavailable")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: dlq,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "dlq", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestDLQ_Retry_StoreError_Returns500(t *testing.T) {
	dlq := &fakeDLQStore{retryFn: func(_ context.Context, _ string) error {
		return errors.New("retry failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: dlq,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "dlq/dlq-1/retry", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestDLQ_Discard_StoreError_Returns500(t *testing.T) {
	dlq := &fakeDLQStore{discardFn: func(_ context.Context, _ string) error {
		return errors.New("discard failed")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: dlq,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "dlq/dlq-1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestSkills_Approve_StoreError_Returns500(t *testing.T) {
	skills := &fakeSkillRegistry{approveFn: func(_ context.Context, _ string) error {
		return errors.New("store error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: skills, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "skills/s1/approve", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestSkills_Reject_StoreError_Returns500(t *testing.T) {
	skills := &fakeSkillRegistry{rejectFn: func(_ context.Context, _ string) error {
		return errors.New("store error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: skills, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "skills/s1/reject", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestSkills_Revoke_StoreError_Returns500(t *testing.T) {
	skills := &fakeSkillRegistry{revokeFn: func(_ context.Context, _ string) error {
		return errors.New("store error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: skills, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "skills/s1", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestUsers_List_StoreError_Returns500(t *testing.T) {
	users := &fakeUserStore{listFn: func(_ context.Context) ([]domain.User, error) {
		return nil, errors.New("db error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "users", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestUsers_Create_StoreError_Returns500(t *testing.T) {
	users := &fakeUserStore{createFn: func(_ context.Context, _ domain.User) (domain.User, error) {
		return domain.User{}, errors.New("db error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users", `{"username":"bob","role":"user"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestUsers_Remove_StoreError_Returns500(t *testing.T) {
	users := &fakeUserStore{deleteFn: func(_ context.Context, _ string) error {
		return errors.New("db error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "users/bob", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestProfile_Get_NotFound_Returns404(t *testing.T) {
	users := &fakeUserStore{getFn: func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "profile", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestProfile_SetName_NotFound_Returns404(t *testing.T) {
	users := &fakeUserStore{getFn: func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPatch, "profile", `{"display_name":"New"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestProfile_SetName_UpdateError_Returns500(t *testing.T) {
	users := &fakeUserStore{updateFn: func(_ context.Context, _ domain.User) (domain.User, error) {
		return domain.User{}, errors.New("update error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPatch, "profile", `{"display_name":"New"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestProfile_SetPassword_SetError_Returns500(t *testing.T) {
	auth := &fakeAuth{
		setPasswordFn: func(_ context.Context, _, _ string) error {
			return errors.New("hash error")
		},
	}
	srv := newTestServer(auth)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "profile/password", `{"old_password":"old","new_password":"new"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestLogin_InternalError_Returns500(t *testing.T) {
	auth := &fakeAuth{loginFn: func(_, _ string) (domainauth.Token, error) {
		return domainauth.Token{}, errors.New("db unavailable")
	}}
	srv := httptest.NewServer(httpserver.New(httpserver.Config{
		Auth: auth, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	}).Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login",
		strings.NewReader(`{"username":"x","password":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for internal login error, got %d", resp.StatusCode)
	}
}

func TestBoard_Update_GetNotFound_Returns404(t *testing.T) {
	board := &fakeBoardStore{getFn: func(_ context.Context, _ string) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPatch, "board/missing", `{"title":"t"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestBoard_Assign_GetNotFound_Returns404(t *testing.T) {
	board := &fakeBoardStore{getFn: func(_ context.Context, _ string) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/missing/assign", `{"bot_id":"dev-1"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestBoard_Close_GetNotFound_Returns404(t *testing.T) {
	board := &fakeBoardStore{getFn: func(_ context.Context, _ string) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("not found")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "board/missing/close", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUsers_SetPassword_AuthError_Returns500(t *testing.T) {
	auth := &fakeAuth{setPasswordFn: func(_ context.Context, _, _ string) error {
		return errors.New("hash failed")
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/bob/password", `{"password":"new123"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestUsers_SetRole_UpdateError_Returns500(t *testing.T) {
	users := &fakeUserStore{updateFn: func(_ context.Context, _ domain.User) (domain.User, error) {
		return domain.User{}, errors.New("update error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/bob/role", `{"role":"admin"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestUsers_Disable_UpdateError_Returns500(t *testing.T) {
	users := &fakeUserStore{updateFn: func(_ context.Context, _ domain.User) (domain.User, error) {
		return domain.User{}, errors.New("update error")
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/bob/disable", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestKanbanUI_404_OnUnknownPath(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/other/path")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown path, got %d", resp.StatusCode)
	}
}

// TestProtectedRoute_ExpiredToken_Returns401 verifies that a write endpoint
// (POST /board) returns 401 when an expired token is presented.
func TestProtectedRoute_ExpiredToken_Returns401(t *testing.T) {
	auth := &fakeAuth{validateTokenFn: func(_ string) (domainauth.Claims, error) {
		return domainauth.Claims{}, domainauth.ErrTokenExpired
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board", strings.NewReader(`{"title":"t"}`))
	req.Header.Set("Authorization", "Bearer expired-token")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token on POST /board, got %d", resp.StatusCode)
	}
}

// ── Board filter ──────────────────────────────────────────────────────────────

// ── Review-findings tests (must fail before fixes are applied) ────────────────

func TestUsers_Create_IgnoresProvidedPassword_MustChangePwdAlwaysTrue(t *testing.T) {
	var created domain.User
	users := &fakeUserStore{createFn: func(_ context.Context, u domain.User) (domain.User, error) {
		created = u
		return u, nil
	}}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: &fakeBoardStore{}, Team: &fakeControlPlane{},
		Users: users, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// Sending a "password" field must have no effect — MustChangePassword must always be true.
	resp := doJSON(t, srv, http.MethodPost, "users",
		`{"username":"bob","role":"user","password":"should-be-ignored"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if !created.MustChangePassword {
		t.Fatal("MustChangePassword must be true for all newly created users")
	}
}

func TestUsers_Create_InvalidRole_Returns400(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users",
		`{"username":"bob","role":"superadmin"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", resp.StatusCode)
	}
}

func TestUsers_SetRole_InvalidRole_Returns400(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "users/bob/role",
		`{"role":"superadmin"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", resp.StatusCode)
	}
}

func TestBoard_Update_InvalidStatus_Returns400(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPatch, "board/abc-123",
		`{"status":"wontfix"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d", resp.StatusCode)
	}
}

func TestUserResponse_DoesNotContainPasswordHash(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodGet, "users", "")
	var buf strings.Builder
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	body := buf.String()
	if strings.Contains(body, "PasswordHash") || strings.Contains(body, "password_hash") {
		t.Errorf("user list response must not expose PasswordHash field, got: %s", body)
	}
}

func TestBoard_List_FilterByStatus(t *testing.T) {
	var capturedFilter domain.WorkItemFilter
	board := &fakeBoardStore{
		listFn: func(_ context.Context, filter domain.WorkItemFilter) ([]domain.WorkItem, error) {
			capturedFilter = filter
			return []domain.WorkItem{{ID: "1", Status: filter.Status}}, nil
		},
	}
	s := httpserver.New(httpserver.Config{
		Auth: &fakeAuth{}, Board: board, Team: &fakeControlPlane{},
		Users: &fakeUserStore{}, Skills: &fakeSkillRegistry{}, DLQ: &fakeDLQStore{},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board?status=in-progress", nil)
	req.Header.Set("Authorization", authHeader())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedFilter.Status != domain.WorkItemStatusInProgress {
		t.Fatalf("expected filter.Status=in-progress, got %q", capturedFilter.Status)
	}
}

// ── Pool endpoint ─────────────────────────────────────────────────────────────

// fakeTechLeadPool implements domain.TechLeadPool for testing.
type fakeTechLeadPool struct {
	listEntriesFn func(ctx context.Context) ([]*domain.PoolEntry, error)
}

func (f *fakeTechLeadPool) Allocate(_ context.Context, _ string) (*domain.PoolEntry, error) {
	return nil, nil
}
func (f *fakeTechLeadPool) Deallocate(_ context.Context, _ string) error { return nil }
func (f *fakeTechLeadPool) Reconcile(_ context.Context) error            { return nil }
func (f *fakeTechLeadPool) ListEntries(ctx context.Context) ([]*domain.PoolEntry, error) {
	if f.listEntriesFn != nil {
		return f.listEntriesFn(ctx)
	}
	return nil, nil
}
func (f *fakeTechLeadPool) GetByItemID(_ context.Context, _ string) (*domain.PoolEntry, error) {
	return nil, nil
}

func TestPool_Endpoint_ReturnsEntries(t *testing.T) {
	t.Parallel()
	entries := []*domain.PoolEntry{
		{InstanceName: "tech-lead-1", Status: domain.PoolEntryStatusAllocated, ItemID: "item-1"},
		{InstanceName: "tech-lead-2", Status: domain.PoolEntryStatusIdle},
	}
	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  &fakeBoardStore{},
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
		Pool: &fakeTechLeadPool{listEntriesFn: func(_ context.Context) ([]*domain.PoolEntry, error) {
			return entries, nil
		}},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/pool", nil)
	req.Header.Set("Authorization", authHeader())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Pool []domain.PoolEntry `json:"pool"`
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Pool) != 2 {
		t.Errorf("expected 2 pool entries, got %d", len(out.Pool))
	}
}

func TestPool_Endpoint_NoPool_Returns200Empty(t *testing.T) {
	t.Parallel()
	// Pool is nil in config -- authenticated request should return empty pool JSON.
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

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/pool", nil)
	req.Header.Set("Authorization", authHeader())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPool_Endpoint_RequiresAuth(t *testing.T) {
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

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/pool", nil)
	// No Authorization header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

// ── Plugin 404 tests ──────────────────────────────────────────────────────────

// fakePluginStore is a no-op PluginStore used to enable plugin route registration.
type fakePluginStore struct{}

func (f *fakePluginStore) List(_ context.Context) ([]domain.Plugin, error) { return nil, nil }
func (f *fakePluginStore) Get(_ context.Context, _ string) (domain.Plugin, error) {
	return domain.Plugin{}, nil
}
func (f *fakePluginStore) Install(_ context.Context, _ domain.PluginManifest, _ []byte, _ string, _ bool) (domain.Plugin, error) {
	return domain.Plugin{}, nil
}
func (f *fakePluginStore) Approve(_ context.Context, _ string) error { return nil }
func (f *fakePluginStore) Reject(_ context.Context, _ string) error  { return nil }
func (f *fakePluginStore) Disable(_ context.Context, _ string) error { return nil }
func (f *fakePluginStore) Enable(_ context.Context, _ string) error  { return nil }
func (f *fakePluginStore) Update(_ context.Context, _ string, _ domain.PluginManifest, _ []byte) error {
	return nil
}
func (f *fakePluginStore) Reload(_ context.Context, _ string) error { return nil }
func (f *fakePluginStore) Remove(_ context.Context, _ string) error { return nil }

// fakePluginManager is a configurable PluginManager for testing.
type fakePluginManager struct {
	listFn    func(ctx context.Context) ([]domain.Plugin, error)
	getFn     func(ctx context.Context, id string) (domain.Plugin, error)
	approveFn func(ctx context.Context, id, actor string) error
	rejectFn  func(ctx context.Context, id, actor string) error
	enableFn  func(ctx context.Context, id, actor string) error
	disableFn func(ctx context.Context, id, actor string) error
	reloadFn  func(ctx context.Context, id, actor string) error
	removeFn  func(ctx context.Context, id, actor string) error
}

func (f *fakePluginManager) List(ctx context.Context) ([]domain.Plugin, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return []domain.Plugin{}, nil
}
func (f *fakePluginManager) Get(ctx context.Context, id string) (domain.Plugin, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.Plugin{}, nil
}
func (f *fakePluginManager) Approve(ctx context.Context, id, actor string) error {
	if f.approveFn != nil {
		return f.approveFn(ctx, id, actor)
	}
	return nil
}
func (f *fakePluginManager) Reject(ctx context.Context, id, actor string) error {
	if f.rejectFn != nil {
		return f.rejectFn(ctx, id, actor)
	}
	return nil
}
func (f *fakePluginManager) Enable(ctx context.Context, id, actor string) error {
	if f.enableFn != nil {
		return f.enableFn(ctx, id, actor)
	}
	return nil
}
func (f *fakePluginManager) Disable(ctx context.Context, id, actor string) error {
	if f.disableFn != nil {
		return f.disableFn(ctx, id, actor)
	}
	return nil
}
func (f *fakePluginManager) Reload(ctx context.Context, id, actor string) error {
	if f.reloadFn != nil {
		return f.reloadFn(ctx, id, actor)
	}
	return nil
}
func (f *fakePluginManager) Remove(ctx context.Context, id, actor string) error {
	if f.removeFn != nil {
		return f.removeFn(ctx, id, actor)
	}
	return nil
}

func newTestServerWithPluginManager(mgr httpserver.PluginManager) *httptest.Server {
	s := httpserver.New(httpserver.Config{
		Auth:         &fakeAuth{},
		Board:        &fakeBoardStore{},
		Team:         &fakeControlPlane{},
		Users:        &fakeUserStore{},
		Skills:       &fakeSkillRegistry{},
		DLQ:          &fakeDLQStore{},
		Plugins:      &fakePluginStore{},
		PluginManage: mgr,
	})
	return httptest.NewServer(s.Handler())
}

func TestPluginsApprove_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		approveFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "plugins/nonexistent-id/approve", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("approve: expected 404, got %d", resp.StatusCode)
	}
}

func TestPluginsReject_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		rejectFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "plugins/nonexistent-id/reject", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("reject: expected 404, got %d", resp.StatusCode)
	}
}

func TestPluginsEnable_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		enableFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "plugins/nonexistent-id/enable", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("enable: expected 404, got %d", resp.StatusCode)
	}
}

func TestPluginsDisable_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		disableFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "plugins/nonexistent-id/disable", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("disable: expected 404, got %d", resp.StatusCode)
	}
}

func TestPluginsReload_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		reloadFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodPost, "plugins/nonexistent-id/reload", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("reload: expected 404, got %d", resp.StatusCode)
	}
}

func TestPluginsRemove_NotFound(t *testing.T) {
	mgr := &fakePluginManager{
		removeFn: func(_ context.Context, _ string, _ string) error {
			return domain.ErrPluginNotFound
		},
	}
	srv := newTestServerWithPluginManager(mgr)
	defer srv.Close()

	resp := doJSON(t, srv, http.MethodDelete, "plugins/nonexistent-id", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("remove: expected 404, got %d", resp.StatusCode)
	}
}
