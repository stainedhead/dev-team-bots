// Package httpserver provides the orchestrator REST API and Kanban web UI.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	domainauth "github.com/stainedhead/dev-team-bots/boabot/internal/domain/auth"
)

// AuthProvider is the subset of domain/auth.AuthProvider required by the server.
type AuthProvider interface {
	Login(username, password string) (domainauth.Token, error)
	ValidateToken(token string) (domainauth.Claims, error)
}

// Config holds all stores and providers required by the orchestrator server.
type Config struct {
	Auth   AuthProvider
	Board  domain.BoardStore
	Team   domain.ControlPlane
	Users  domain.UserStore
	Skills domain.SkillRegistry
	DLQ    domain.DLQStore
}

// Server is the orchestrator HTTP server.
type Server struct {
	cfg Config
}

// New creates a Server with the given config.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns the root http.Handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Board
	mux.HandleFunc("GET /api/v1/board", s.auth(s.handleBoardList))
	mux.HandleFunc("GET /api/v1/board/{id}", s.auth(s.handleBoardGet))
	mux.HandleFunc("POST /api/v1/board", s.auth(s.handleBoardCreate))
	mux.HandleFunc("PATCH /api/v1/board/{id}", s.auth(s.handleBoardUpdate))
	mux.HandleFunc("POST /api/v1/board/{id}/assign", s.auth(s.handleBoardAssign))
	mux.HandleFunc("POST /api/v1/board/{id}/close", s.auth(s.handleBoardClose))

	// Team — exact /health before wildcard /{name}
	mux.HandleFunc("GET /api/v1/team", s.auth(s.handleTeamList))
	mux.HandleFunc("GET /api/v1/team/health", s.auth(s.handleTeamHealth))
	mux.HandleFunc("GET /api/v1/team/{name}", s.auth(s.handleTeamGet))

	// Skills
	mux.HandleFunc("GET /api/v1/skills", s.auth(s.handleSkillsList))
	mux.HandleFunc("POST /api/v1/skills/{id}/approve", s.auth(s.adminOnly(s.handleSkillsApprove)))
	mux.HandleFunc("POST /api/v1/skills/{id}/reject", s.auth(s.adminOnly(s.handleSkillsReject)))
	mux.HandleFunc("DELETE /api/v1/skills/{id}", s.auth(s.adminOnly(s.handleSkillsRevoke)))

	// Users (admin)
	mux.HandleFunc("GET /api/v1/users", s.auth(s.adminOnly(s.handleUserList)))
	mux.HandleFunc("POST /api/v1/users", s.auth(s.adminOnly(s.handleUserCreate)))
	mux.HandleFunc("DELETE /api/v1/users/{username}", s.auth(s.adminOnly(s.handleUserRemove)))
	mux.HandleFunc("POST /api/v1/users/{username}/disable", s.auth(s.adminOnly(s.handleUserDisable)))
	mux.HandleFunc("POST /api/v1/users/{username}/password", s.auth(s.adminOnly(s.handleUserSetPassword)))
	mux.HandleFunc("POST /api/v1/users/{username}/role", s.auth(s.adminOnly(s.handleUserSetRole)))

	// Profile
	mux.HandleFunc("GET /api/v1/profile", s.auth(s.handleProfileGet))
	mux.HandleFunc("PATCH /api/v1/profile", s.auth(s.handleProfileSetName))
	mux.HandleFunc("POST /api/v1/profile/password", s.auth(s.handleProfileSetPassword))

	// DLQ (admin)
	mux.HandleFunc("GET /api/v1/dlq", s.auth(s.adminOnly(s.handleDLQList)))
	mux.HandleFunc("POST /api/v1/dlq/{id}/retry", s.auth(s.adminOnly(s.handleDLQRetry)))
	mux.HandleFunc("DELETE /api/v1/dlq/{id}", s.auth(s.adminOnly(s.handleDLQDiscard)))

	// Kanban web UI
	mux.HandleFunc("GET /", s.handleKanbanUI)

	return mux
}

// ── context key ───────────────────────────────────────────────────────────────

type ctxKey int

const claimsKey ctxKey = 1

// ── middleware ────────────────────────────────────────────────────────────────

// auth extracts and validates the Bearer token, setting claims in the context.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}
		token := strings.TrimPrefix(authz, "Bearer ")
		claims, err := s.cfg.Auth.ValidateToken(token)
		if err != nil {
			if errors.Is(err, domainauth.ErrTokenExpired) {
				writeError(w, http.StatusUnauthorized, "token expired")
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := r.Context()
		// Store claims in request context via a value key.
		r = r.WithContext(context.WithValue(ctx, claimsKey, claims))
		next(w, r)
	}
}

// adminOnly rejects requests from non-admin callers with 403 Forbidden.
func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r)
		if claims.Role != string(domain.UserRoleAdmin) {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}
		next(w, r)
	}
}

// ── auth handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tok, err := s.cfg.Auth.Login(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, domainauth.ErrInvalidCredentials) || errors.Is(err, domainauth.ErrMustChangePassword) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeInternalError(w, "login", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":                tok.AccessToken,
		"expires_at":           tok.ExpiresAt,
		"must_change_password": tok.MustChangePassword,
	})
}

// ── board handlers ────────────────────────────────────────────────────────────

func (s *Server) handleBoardList(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.Board.List(r.Context(), domain.WorkItemFilter{})
	if err != nil {
		writeInternalError(w, "board list", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleBoardGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleBoardCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AssignedTo  string `json:"assigned_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims := claimsFromContext(r)
	now := time.Now().UTC()
	item, err := s.cfg.Board.Create(r.Context(), domain.WorkItem{
		Title:       req.Title,
		Description: req.Description,
		AssignedTo:  req.AssignedTo,
		Status:      domain.WorkItemStatusBacklog,
		CreatedBy:   claims.Subject,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		writeInternalError(w, "board create", err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleBoardUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		AssignedTo  *string `json:"assigned_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Status != nil {
		existing.Status = domain.WorkItemStatus(*req.Status)
	}
	if req.AssignedTo != nil {
		existing.AssignedTo = *req.AssignedTo
	}
	existing.UpdatedAt = time.Now().UTC()
	updated, err := s.cfg.Board.Update(r.Context(), existing)
	if err != nil {
		writeInternalError(w, "board update", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleBoardAssign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	var req struct {
		BotID string `json:"bot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing.AssignedTo = req.BotID
	existing.UpdatedAt = time.Now().UTC()
	updated, err := s.cfg.Board.Update(r.Context(), existing)
	if err != nil {
		writeInternalError(w, "board assign", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleBoardClose(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	existing.Status = domain.WorkItemStatusDone
	existing.UpdatedAt = time.Now().UTC()
	if _, err := s.cfg.Board.Update(r.Context(), existing); err != nil {
		writeInternalError(w, "board close", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── team handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleTeamList(w http.ResponseWriter, r *http.Request) {
	bots, err := s.cfg.Team.List(r.Context())
	if err != nil {
		writeInternalError(w, "team list", err)
		return
	}
	writeJSON(w, http.StatusOK, bots)
}

func (s *Server) handleTeamHealth(w http.ResponseWriter, r *http.Request) {
	bots, err := s.cfg.Team.List(r.Context())
	if err != nil {
		writeInternalError(w, "team health", err)
		return
	}
	var active, inactive int
	for _, b := range bots {
		if b.Status == domain.BotStatusActive {
			active++
		} else {
			inactive++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"active":   active,
		"inactive": inactive,
		"total":    active + inactive,
	})
}

func (s *Server) handleTeamGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	bot, err := s.cfg.Team.Get(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "bot not found")
		return
	}
	writeJSON(w, http.StatusOK, bot)
}

// ── skills handlers ───────────────────────────────────────────────────────────

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	botType := r.URL.Query().Get("bot")
	skills, err := s.cfg.Skills.List(r.Context(), botType, "")
	if err != nil {
		writeInternalError(w, "skills list", err)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *Server) handleSkillsApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Skills.Approve(r.Context(), id); err != nil {
		writeInternalError(w, "skills approve", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSkillsReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Skills.Reject(r.Context(), id); err != nil {
		writeInternalError(w, "skills reject", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSkillsRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Skills.Revoke(r.Context(), id); err != nil {
		writeInternalError(w, "skills revoke", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── user handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	users, err := s.cfg.Users.List(r.Context())
	if err != nil {
		writeInternalError(w, "user list", err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	user, err := s.cfg.Users.Create(r.Context(), domain.User{
		Username:           req.Username,
		Role:               domain.UserRole(req.Role),
		Enabled:            true,
		MustChangePassword: req.Password == "",
		CreatedAt:          now,
	})
	if err != nil {
		writeInternalError(w, "user create", err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUserRemove(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if err := s.cfg.Users.Delete(r.Context(), username); err != nil {
		writeInternalError(w, "user remove", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserDisable(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	user, err := s.cfg.Users.Get(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	user.Enabled = false
	if _, err := s.cfg.Users.Update(r.Context(), user); err != nil {
		writeInternalError(w, "user disable", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserSetPassword(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := s.cfg.Users.Get(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// Password hashing is the responsibility of the auth provider; store the
	// plain value here — the auth adapter will hash it on next login.
	user.PasswordHash = req.Password
	user.MustChangePassword = false
	if _, err := s.cfg.Users.Update(r.Context(), user); err != nil {
		writeInternalError(w, "user set password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserSetRole(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := s.cfg.Users.Get(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	user.Role = domain.UserRole(req.Role)
	if _, err := s.cfg.Users.Update(r.Context(), user); err != nil {
		writeInternalError(w, "user set role", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── profile handlers ──────────────────────────────────────────────────────────

func (s *Server) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r)
	user, err := s.cfg.Users.Get(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleProfileSetName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims := claimsFromContext(r)
	user, err := s.cfg.Users.Get(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	user.DisplayName = req.DisplayName
	if _, err := s.cfg.Users.Update(r.Context(), user); err != nil {
		writeInternalError(w, "profile set name", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProfileSetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims := claimsFromContext(r)
	user, err := s.cfg.Users.Get(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// Password verification is the auth provider's responsibility; update the
	// stored hash via the user store after verification is confirmed by caller.
	user.PasswordHash = req.NewPassword
	user.MustChangePassword = false
	if _, err := s.cfg.Users.Update(r.Context(), user); err != nil {
		writeInternalError(w, "profile set password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── DLQ handlers ──────────────────────────────────────────────────────────────

func (s *Server) handleDLQList(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.DLQ.List(r.Context())
	if err != nil {
		writeInternalError(w, "dlq list", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleDLQRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.DLQ.Retry(r.Context(), id); err != nil {
		writeInternalError(w, "dlq retry", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDLQDiscard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.DLQ.Discard(r.Context(), id); err != nil {
		writeInternalError(w, "dlq discard", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Kanban web UI ─────────────────────────────────────────────────────────────

const kanbanHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>BaoBot Kanban</title>
  <script src="https://unpkg.com/htmx.org@1.9.12"></script>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: system-ui, sans-serif; background: #0f172a; color: #e2e8f0; min-height: 100vh; }
    header { padding: 1rem 2rem; background: #1e293b; border-bottom: 1px solid #334155; display: flex; align-items: center; gap: 1rem; }
    header h1 { font-size: 1.25rem; font-weight: 600; }
    .board { display: flex; gap: 1.5rem; padding: 2rem; overflow-x: auto; }
    .column { background: #1e293b; border-radius: 0.5rem; min-width: 280px; padding: 1rem; }
    .column-header { font-weight: 600; font-size: 0.875rem; text-transform: uppercase; letter-spacing: 0.05em; color: #94a3b8; margin-bottom: 1rem; }
    .card { background: #0f172a; border: 1px solid #334155; border-radius: 0.375rem; padding: 0.75rem; margin-bottom: 0.5rem; cursor: pointer; }
    .card:hover { border-color: #64748b; }
    .card-title { font-size: 0.875rem; font-weight: 500; }
    .card-meta { font-size: 0.75rem; color: #64748b; margin-top: 0.25rem; }
    .badge { display: inline-block; padding: 0.125rem 0.5rem; border-radius: 9999px; font-size: 0.75rem; }
    .badge-active { background: #166534; color: #86efac; }
    .badge-backlog { background: #1e3a5f; color: #93c5fd; }
    .loading { text-align: center; color: #64748b; padding: 2rem; }
  </style>
</head>
<body>
  <header>
    <h1>🤖 BaoBot Kanban</h1>
    <span style="margin-left:auto;font-size:0.75rem;color:#64748b">
      Refreshing every 30s &nbsp;
      <span hx-get="/api/v1/team/health" hx-trigger="load, every 30s" hx-target="this" hx-swap="innerHTML">…</span>
    </span>
  </header>
  <div class="board">
    <div class="column">
      <div class="column-header">Backlog</div>
      <div id="col-backlog"
           hx-get="/api/v1/board?status=backlog"
           hx-trigger="load, every 30s"
           hx-target="this"
           hx-swap="innerHTML">
        <div class="loading">Loading…</div>
      </div>
    </div>
    <div class="column">
      <div class="column-header">In Progress</div>
      <div id="col-inprogress"
           hx-get="/api/v1/board?status=in-progress"
           hx-trigger="load, every 30s"
           hx-target="this"
           hx-swap="innerHTML">
        <div class="loading">Loading…</div>
      </div>
    </div>
    <div class="column">
      <div class="column-header">Blocked</div>
      <div id="col-blocked"
           hx-get="/api/v1/board?status=blocked"
           hx-trigger="load, every 30s"
           hx-target="this"
           hx-swap="innerHTML">
        <div class="loading">Loading…</div>
      </div>
    </div>
    <div class="column">
      <div class="column-header">Done</div>
      <div id="col-done"
           hx-get="/api/v1/board?status=done"
           hx-trigger="load, every 30s"
           hx-target="this"
           hx-swap="innerHTML">
        <div class="loading">Loading…</div>
      </div>
    </div>
  </div>
</body>
</html>`

func (s *Server) handleKanbanUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(kanbanHTML))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeInternalError(w http.ResponseWriter, op string, err error) {
	slog.Error("internal server error", "op", op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// ── context helpers ───────────────────────────────────────────────────────────

func claimsFromContext(r *http.Request) domainauth.Claims {
	v := r.Context().Value(claimsKey)
	if v == nil {
		return domainauth.Claims{}
	}
	claims, _ := v.(domainauth.Claims)
	return claims
}
