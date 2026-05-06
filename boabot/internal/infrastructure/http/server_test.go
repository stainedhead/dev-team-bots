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

func TestProtectedRoute_NoToken_Returns401(t *testing.T) {
	srv := newTestServer(&fakeAuth{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestProtectedRoute_InvalidToken_Returns401(t *testing.T) {
	auth := &fakeAuth{validateTokenFn: func(_ string) (domainauth.Claims, error) {
		return domainauth.Claims{}, domainauth.ErrInvalidCredentials
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
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

func TestKanbanUI_HasSRIHash(t *testing.T) {
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
	if !strings.Contains(body, `integrity="sha384-`) {
		t.Error("kanban HTML missing integrity= SRI attribute on HTMX script tag")
	}
	if !strings.Contains(body, `crossorigin="anonymous"`) {
		t.Error("kanban HTML missing crossorigin=anonymous on HTMX script tag")
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

func TestProtectedRoute_ExpiredToken_Returns401(t *testing.T) {
	auth := &fakeAuth{validateTokenFn: func(_ string) (domainauth.Claims, error) {
		return domainauth.Claims{}, domainauth.ErrTokenExpired
	}}
	srv := newTestServer(auth)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", resp.StatusCode)
	}
}

// ── Board filter ──────────────────────────────────────────────────────────────

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
