// Package httpserver provides the orchestrator REST API and Kanban web UI.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// SetPassword hashes newPassword and updates the stored credential for username.
	SetPassword(ctx context.Context, username, newPassword string) error
	// VerifyPassword checks that password matches the stored credential for username.
	VerifyPassword(ctx context.Context, username, password string) error
}

// Config holds all stores and providers required by the orchestrator server.
type Config struct {
	Auth       AuthProvider
	Board      domain.BoardStore
	Team       domain.ControlPlane
	Users      domain.UserStore
	Skills     domain.SkillRegistry
	DLQ        domain.DLQStore
	Tasks      domain.DirectTaskStore
	Dispatcher domain.TaskDispatcher
	Chat       domain.ChatStore
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

	// Board — read endpoints are public; write endpoints require auth
	mux.HandleFunc("GET /api/v1/board", s.handleBoardList)
	mux.HandleFunc("GET /api/v1/board/{id}", s.handleBoardGet)
	mux.HandleFunc("POST /api/v1/board", s.auth(s.handleBoardCreate))
	mux.HandleFunc("PATCH /api/v1/board/{id}", s.auth(s.handleBoardUpdate))
	mux.HandleFunc("POST /api/v1/board/{id}/assign", s.auth(s.handleBoardAssign))
	mux.HandleFunc("POST /api/v1/board/{id}/close", s.auth(s.handleBoardClose))

	// Team — read endpoints are public; exact /health before wildcard /{name}
	mux.HandleFunc("GET /api/v1/team", s.handleTeamList)
	mux.HandleFunc("GET /api/v1/team/health", s.handleTeamHealth)
	mux.HandleFunc("GET /api/v1/team/{name}", s.handleTeamGet)

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

	// Direct tasks — require auth
	mux.HandleFunc("GET /api/v1/tasks", s.auth(s.handleTaskList))
	mux.HandleFunc("POST /api/v1/bots/{name}/tasks", s.auth(s.handleBotTaskCreate))
	mux.HandleFunc("GET /api/v1/bots/{name}/tasks", s.auth(s.handleBotTaskList))

	// Chat — require auth
	mux.HandleFunc("GET /api/v1/chat", s.auth(s.handleChatList))
	mux.HandleFunc("GET /api/v1/chat/{bot}", s.auth(s.handleChatBotList))
	mux.HandleFunc("POST /api/v1/chat/{bot}", s.auth(s.handleChatSend))

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
	filter := domain.WorkItemFilter{}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = domain.WorkItemStatus(s)
	}
	if bot := r.URL.Query().Get("assigned_to"); bot != "" {
		filter.AssignedTo = bot
	}
	items, err := s.cfg.Board.List(r.Context(), filter)
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
		if !isValidWorkItemStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
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

	// If status moved to in-progress and a bot is assigned, dispatch a task.
	if updated.Status == domain.WorkItemStatusInProgress && updated.AssignedTo != "" && s.cfg.Dispatcher != nil {
		instruction := fmt.Sprintf("Board item assigned to you:\n\nTitle: %s\n\nDescription: %s\n\nItem ID: %s",
			updated.Title, updated.Description, updated.ID)
		if _, dispErr := s.cfg.Dispatcher.Dispatch(r.Context(), updated.AssignedTo, instruction, nil); dispErr != nil {
			slog.Warn("board→bot dispatch failed", "bot", updated.AssignedTo, "item", updated.ID, "err", dispErr)
			// Non-fatal: the board update already succeeded.
		}
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !isValidRole(req.Role) {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}
	now := time.Now().UTC()
	user, err := s.cfg.Users.Create(r.Context(), domain.User{
		Username:           req.Username,
		Role:               domain.UserRole(req.Role),
		Enabled:            true,
		MustChangePassword: true,
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
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password must not be empty")
		return
	}
	if err := s.cfg.Auth.SetPassword(r.Context(), username, req.Password); err != nil {
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
	if !isValidRole(req.Role) {
		writeError(w, http.StatusBadRequest, "invalid role")
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
	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "new_password must not be empty")
		return
	}
	claims := claimsFromContext(r)
	if err := s.cfg.Auth.VerifyPassword(r.Context(), claims.Subject, req.OldPassword); err != nil {
		writeError(w, http.StatusUnauthorized, "incorrect current password")
		return
	}
	if err := s.cfg.Auth.SetPassword(r.Context(), claims.Subject, req.NewPassword); err != nil {
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

// ── direct task handlers ──────────────────────────────────────────────────────

func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "task dispatch not available")
		return
	}
	tasks, err := s.cfg.Tasks.ListAll(r.Context())
	if err != nil {
		writeInternalError(w, "task list all", err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleBotTaskCreate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "task dispatch not available")
		return
	}
	name := r.PathValue("name")
	var req struct {
		Instruction string     `json:"instruction"`
		ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Instruction == "" {
		writeError(w, http.StatusBadRequest, "instruction must not be empty")
		return
	}
	task, err := s.cfg.Dispatcher.Dispatch(r.Context(), name, req.Instruction, req.ScheduledAt)
	if err != nil {
		writeInternalError(w, "bot task create", err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleBotTaskList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "task dispatch not available")
		return
	}
	name := r.PathValue("name")
	tasks, err := s.cfg.Tasks.List(r.Context(), name)
	if err != nil {
		writeInternalError(w, "bot task list", err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// ── chat handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleChatList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	msgs, err := s.cfg.Chat.ListAll(r.Context())
	if err != nil {
		writeInternalError(w, "chat list all", err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleChatBotList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	bot := r.PathValue("bot")
	msgs, err := s.cfg.Chat.List(r.Context(), bot)
	if err != nil {
		writeInternalError(w, "chat bot list", err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil || s.cfg.Dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	bot := r.PathValue("bot")
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content must not be empty")
		return
	}

	msg := domain.ChatMessage{
		BotName:   bot,
		Direction: domain.ChatDirectionOutbound,
		Content:   req.Content,
	}
	if err := s.cfg.Chat.Append(r.Context(), msg); err != nil {
		writeInternalError(w, "chat append", err)
		return
	}

	task, err := s.cfg.Dispatcher.Dispatch(r.Context(), bot, req.Content, nil)
	if err != nil {
		writeInternalError(w, "chat dispatch", err)
		return
	}

	msg.TaskID = task.ID
	writeJSON(w, http.StatusCreated, msg)
}

// ── enum validation helpers ────────────────────────────────────────────────────

func isValidRole(role string) bool {
	switch domain.UserRole(role) {
	case domain.UserRoleAdmin, domain.UserRoleUser:
		return true
	}
	return false
}

func isValidWorkItemStatus(status string) bool {
	switch domain.WorkItemStatus(status) {
	case domain.WorkItemStatusBacklog, domain.WorkItemStatusInProgress,
		domain.WorkItemStatusBlocked, domain.WorkItemStatusDone:
		return true
	}
	return false
}

// ── Kanban web UI ─────────────────────────────────────────────────────────────

const kanbanHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>BaoBot Control</title>
  <style>
    *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
    body{font-family:system-ui,-apple-system,sans-serif;background:#080e1a;color:#e2e8f0;height:100vh;display:flex;flex-direction:column;overflow:hidden}

    /* ── Header ── */
    header{padding:.6rem 1.25rem;background:#0d1424;border-bottom:1px solid #1a2744;display:flex;align-items:center;gap:.75rem;flex-shrink:0;z-index:10}
    .logo{font-size:1rem;font-weight:700;color:#60a5fa;letter-spacing:-.02em;white-space:nowrap}
    .logo span{color:#475569;font-weight:400}
    .hdr-mid{flex:1;display:flex;align-items:center;gap:.75rem}
    .hpill{padding:.15rem .6rem;border-radius:9999px;font-size:.7rem;font-weight:600}
    .hpill-ok{background:#14532d;color:#86efac}
    .hpill-warn{background:#78350f;color:#fde68a}
    .hdr-right{display:flex;align-items:center;gap:.5rem}
    .tick{font-size:.65rem;color:#334155}

    /* ── Buttons ── */
    .btn{padding:.3rem .7rem;border-radius:.35rem;cursor:pointer;font-size:.75rem;font-weight:500;border:none;line-height:1.4;transition:filter .15s}
    .btn:hover{filter:brightness(1.15)}
    .btn-primary{background:#2563eb;color:#fff}
    .btn-secondary{background:#1e293b;color:#cbd5e1;border:1px solid #2d3e5a}
    .btn-ghost{background:transparent;color:#64748b;border:1px solid #1a2744}
    .btn-ghost:hover{color:#e2e8f0;border-color:#334155}
    .btn-danger{background:#7f1d1d;color:#fca5a5}
    .btn-success{background:#14532d;color:#86efac}
    .btn-warn{background:#78350f;color:#fde68a}
    .btn-sm{padding:.15rem .45rem;font-size:.68rem}

    /* ── App shell ── */
    .shell{display:flex;flex:1;overflow:hidden}

    /* ── Sidebar ── */
    aside{width:210px;flex-shrink:0;background:#0a1020;border-right:1px solid #1a2744;display:flex;flex-direction:column;overflow:hidden}
    .sb-hdr{padding:.5rem .75rem;font-size:.62rem;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:#334155;border-bottom:1px solid #1a2744;flex-shrink:0}
    .bot-list{flex:1;overflow-y:auto;padding:.375rem}
    .bcard{padding:.5rem .625rem;border-radius:.35rem;margin-bottom:.3rem;background:#0f1829;border:1px solid #1a2744;cursor:default}
    .bcard:hover{border-color:#2d3e5a}
    .brow{display:flex;align-items:center;gap:.4rem}
    .bdot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
    .bdot-on{background:#22c55e;box-shadow:0 0 5px #22c55e88}
    .bdot-off{background:#334155}
    .bname{font-size:.72rem;font-weight:600;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .bbadge{padding:.1rem .35rem;border-radius:9999px;font-size:.62rem;font-weight:700;background:#1e3a5f;color:#60a5fa;flex-shrink:0}
    .bmeta{margin-top:.25rem;font-size:.62rem;color:#334155;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .bbar{height:2px;background:#111827;border-radius:1px;margin-top:.3rem;overflow:hidden}
    .bfill{height:100%;border-radius:1px;transition:width .4s}
    .bfill-none{width:0}
    .bfill-lo{background:#22c55e}
    .bfill-md{background:#f59e0b}
    .bfill-hi{background:#ef4444}

    /* ── Main ── */
    main{flex:1;display:flex;flex-direction:column;overflow:hidden;min-width:0}

    /* ── Tab bar ── */
    .tabbar{display:flex;align-items:stretch;background:#0a1020;border-bottom:1px solid #1a2744;flex-shrink:0;padding:0 1rem}
    .tab{padding:.55rem .85rem;font-size:.75rem;font-weight:500;color:#475569;cursor:pointer;border:none;background:transparent;border-bottom:2px solid transparent;white-space:nowrap;transition:color .15s,border-color .15s}
    .tab:hover{color:#94a3b8}
    .tab.on{color:#60a5fa;border-bottom-color:#3b82f6}

    /* ── Panes ── */
    .pane{display:none;flex:1;overflow:auto;padding:1.25rem}
    .pane.on{display:flex;flex-direction:column}

    /* ── Board ── */
    .board{display:flex;gap:.875rem;flex:1;align-items:flex-start;min-height:0}
    .col{background:#0f1829;border:1px solid #1a2744;border-radius:.5rem;flex:1;min-width:200px;display:flex;flex-direction:column;max-height:100%}
    .col.over{border-color:#3b82f6;background:#0d1d35}
    .col-hdr{padding:.6rem .75rem;font-size:.65rem;font-weight:700;text-transform:uppercase;letter-spacing:.07em;color:#64748b;border-bottom:1px solid #1a2744;display:flex;align-items:center;gap:.4rem;flex-shrink:0}
    .col-cnt{padding:.05rem .35rem;border-radius:9999px;background:#1a2744;color:#475569;font-size:.6rem;font-weight:600}
    .col-body{flex:1;overflow-y:auto;padding:.375rem;min-height:60px}
    .card{background:#080e1a;border:1px solid #1a2744;border-radius:.35rem;padding:.55rem .65rem;margin-bottom:.3rem;cursor:grab;user-select:none;transition:border-color .15s,opacity .15s}
    .card:hover{border-color:#2d3e5a}
    .card.dragging{opacity:.35;cursor:grabbing}
    .card-title{font-size:.78rem;font-weight:500;line-height:1.35}
    .card-desc{font-size:.68rem;color:#475569;margin-top:.2rem;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
    .card-foot{display:flex;align-items:center;gap:.3rem;margin-top:.4rem}
    .card-who{font-size:.62rem;color:#60a5fa;background:#1e3a5f22;padding:.08rem .35rem;border-radius:9999px;border:1px solid #1e3a5f44}
    .card-age{font-size:.62rem;color:#334155;margin-left:auto}
    .nil{text-align:center;color:#1e2d4a;padding:1.5rem .5rem;font-size:.75rem;font-style:italic}

    /* ── Tables ── */
    .sec-hdr{display:flex;align-items:center;gap:.75rem;margin-bottom:.875rem;flex-shrink:0}
    .sec-title{font-size:.875rem;font-weight:600}
    .sec-acts{margin-left:auto;display:flex;gap:.375rem}
    table{width:100%;border-collapse:collapse;font-size:.78rem}
    th{text-align:left;padding:.4rem .65rem;font-size:.62rem;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:#334155;border-bottom:1px solid #1a2744;white-space:nowrap}
    td{padding:.55rem .65rem;border-bottom:1px solid #0f1829;vertical-align:middle}
    tr:hover td{background:#0d1424}
    .pill{display:inline-block;padding:.1rem .45rem;border-radius:9999px;font-size:.62rem;font-weight:600;white-space:nowrap}
    .pill-ok{background:#14532d;color:#86efac}
    .pill-warn{background:#78350f;color:#fde68a}
    .pill-off{background:#1e293b;color:#475569}
    .pill-admin{background:#312e81;color:#a5b4fc}
    .pill-user{background:#1e293b;color:#64748b}
    .acts{display:flex;gap:.3rem;align-items:center}
    .empty-state{text-align:center;padding:3rem;color:#1e2d4a;font-style:italic;font-size:.8rem}

    /* ── Dialogs ── */
    dialog{background:#0f1829;color:#e2e8f0;border:1px solid #1a2744;border-radius:.625rem;padding:1.375rem;min-width:330px;box-shadow:0 20px 60px #000a}
    dialog::backdrop{background:#000b}
    dialog h2{font-size:.95rem;font-weight:600;margin-bottom:1rem}
    .fg{margin-bottom:.75rem}
    .fl{display:block;font-size:.65rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#64748b;margin-bottom:.3rem}
    .fi{width:100%;padding:.45rem .6rem;background:#080e1a;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.82rem}
    .fi:focus{outline:none;border-color:#3b82f6}
    textarea.fi{resize:vertical;min-height:72px}
    select.fi{cursor:pointer}
    .da{margin-top:1rem;display:flex;gap:.4rem;justify-content:flex-end}
    .errmsg{color:#f87171;font-size:.7rem;margin-top:.4rem}

    /* ── Chat ── */
    .chat-wrap{display:flex;flex-direction:column;flex:1;overflow:hidden}
    .chat-hist{flex:1;overflow-y:auto;padding:1rem;display:flex;flex-direction:column;gap:.5rem}
    .chat-bubble{max-width:70%;padding:.5rem .75rem;border-radius:.5rem;font-size:.8rem;line-height:1.4}
    .chat-out{background:#1e3a5f;color:#e2e8f0;align-self:flex-end;border-bottom-right-radius:.125rem}
    .chat-in{background:#1e293b;color:#e2e8f0;align-self:flex-start;border-bottom-left-radius:.125rem}
    .chat-meta{font-size:.62rem;color:#475569;margin-top:.2rem}
    .chat-input-row{display:flex;gap:.5rem;padding:.75rem 1rem;border-top:1px solid #1a2744;flex-shrink:0}
    .chat-input-row textarea{flex:1;padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.82rem;resize:none;height:56px}
    .chat-input-row select{padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.78rem}

    /* ── Scrollbars ── */
    ::-webkit-scrollbar{width:4px;height:4px}
    ::-webkit-scrollbar-track{background:transparent}
    ::-webkit-scrollbar-thumb{background:#1a2744;border-radius:2px}
    ::-webkit-scrollbar-thumb:hover{background:#2d3e5a}
  </style>
</head>
<body>
<header>
  <div class="logo">BaoBot <span>Control</span></div>
  <div class="hdr-mid">
    <span id="hpill" class="hpill hpill-warn">loading…</span>
    <span class="tick" id="tick">–</span>
  </div>
  <div class="hdr-right">
    <button id="btn-new" class="btn btn-primary" style="display:none" onclick="openNewItem()">+ New Item</button>
    <span id="uinfo" style="display:none;align-items:center;gap:.5rem">
      <span id="ulabel" style="font-size:.72rem;color:#64748b"></span>
      <button class="btn btn-ghost btn-sm" onclick="openChgPw()">Password</button>
      <button class="btn btn-ghost btn-sm" onclick="doLogout()">Logout</button>
    </span>
    <button id="btn-login" class="btn btn-secondary" onclick="dlg('login-dlg')">Login</button>
  </div>
</header>

<div class="shell">
  <aside>
    <div class="sb-hdr">Team Roster</div>
    <div class="bot-list" id="roster"><div class="nil" style="padding:1rem">Loading…</div></div>
  </aside>

  <main>
    <div class="tabbar">
      <button class="tab on" onclick="tab('board')">Board</button>
      <button class="tab" onclick="tab('tasks')" id="t-tasks">Tasks</button>
      <button class="tab" onclick="tab('chat')" id="t-chat">Chat</button>
      <button class="tab" onclick="tab('skills')" id="t-skills">Skills</button>
      <button class="tab" onclick="tab('dlq')" id="t-dlq">Dead Letter Queue</button>
      <button class="tab" onclick="tab('users')" id="t-users" style="display:none">Users</button>
    </div>

    <!-- Board -->
    <div class="pane on" id="pane-board">
      <div class="board">
        <div class="col" id="col-backlog" data-status="backlog" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Backlog <span class="col-cnt" id="n-backlog">0</span></div>
          <div class="col-body" id="b-backlog"><div class="nil">No items</div></div>
        </div>
        <div class="col" id="col-inprogress" data-status="in-progress" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">In Progress <span class="col-cnt" id="n-inprogress">0</span></div>
          <div class="col-body" id="b-inprogress"><div class="nil">No items</div></div>
        </div>
        <div class="col" id="col-blocked" data-status="blocked" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Blocked <span class="col-cnt" id="n-blocked">0</span></div>
          <div class="col-body" id="b-blocked"><div class="nil">No items</div></div>
        </div>
        <div class="col" id="col-done" data-status="done" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Done <span class="col-cnt" id="n-done">0</span></div>
          <div class="col-body" id="b-done"><div class="nil">No items</div></div>
        </div>
      </div>
    </div>

    <!-- Tasks -->
    <div class="pane" id="pane-tasks">
      <div class="sec-hdr"><div class="sec-title">Direct Tasks</div><div class="sec-acts"><button class="btn btn-secondary btn-sm" onclick="loadTasks()">Refresh</button></div></div>
      <div id="tasks-body"><div class="empty-state">Loading…</div></div>
    </div>

    <!-- Chat -->
    <div class="pane" id="pane-chat">
      <div class="chat-wrap">
        <div class="sec-hdr" style="flex-shrink:0;padding:.75rem 1rem 0">
          <div class="sec-title">Chat</div>
        </div>
        <div class="chat-hist" id="chat-hist"></div>
        <div class="chat-input-row">
          <select id="chat-bot-sel"><option value="">— select bot —</option></select>
          <textarea id="chat-input" placeholder="Message… (Enter to send, Shift+Enter for newline)"></textarea>
          <button class="btn btn-primary" onclick="sendChat()">Send</button>
        </div>
      </div>
    </div>

    <!-- Skills -->
    <div class="pane" id="pane-skills">
      <div class="sec-hdr"><div class="sec-title">Skills</div><div class="sec-acts"><button class="btn btn-secondary btn-sm" onclick="loadSkills()">Refresh</button></div></div>
      <div id="skills-body"><div class="empty-state">Loading…</div></div>
    </div>

    <!-- DLQ -->
    <div class="pane" id="pane-dlq">
      <div class="sec-hdr"><div class="sec-title">Dead Letter Queue</div><div class="sec-acts"><button class="btn btn-secondary btn-sm" onclick="loadDLQ()">Refresh</button></div></div>
      <div id="dlq-body"><div class="empty-state">Loading…</div></div>
    </div>

    <!-- Users (admin) -->
    <div class="pane" id="pane-users">
      <div class="sec-hdr">
        <div class="sec-title">Users</div>
        <div class="sec-acts"><button class="btn btn-primary btn-sm" onclick="dlg('cu-dlg')">+ Add User</button></div>
      </div>
      <div id="users-body"><div class="empty-state">Loading…</div></div>
    </div>
  </main>
</div>

<!-- Login -->
<dialog id="login-dlg">
  <h2>Sign In</h2>
  <div class="fg"><label class="fl">Username</label><input class="fi" id="login-u" type="text" autocomplete="username"/></div>
  <div class="fg"><label class="fl">Password</label><input class="fi" id="login-p" type="password" autocomplete="current-password"/></div>
  <div class="errmsg" id="login-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('login-dlg')">Cancel</button><button class="btn btn-primary" onclick="doLogin()">Sign in</button></div>
</dialog>

<!-- New Item -->
<dialog id="ni-dlg">
  <h2>New Work Item</h2>
  <div class="fg"><label class="fl">Title</label><input class="fi" id="ni-title" type="text" placeholder="What needs to be done?"/></div>
  <div class="fg"><label class="fl">Description</label><textarea class="fi" id="ni-desc" placeholder="Optional details…"></textarea></div>
  <div class="fg"><label class="fl">Assign to bot</label><select class="fi" id="ni-bot"><option value="">Unassigned</option></select></div>
  <div class="errmsg" id="ni-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('ni-dlg')">Cancel</button><button class="btn btn-primary" onclick="doCreateItem()">Create</button></div>
</dialog>

<!-- Create User (admin) -->
<dialog id="cu-dlg">
  <h2>Add User</h2>
  <div class="fg"><label class="fl">Username</label><input class="fi" id="cu-u" type="text" autocomplete="off"/></div>
  <div class="fg"><label class="fl">Role</label><select class="fi" id="cu-r"><option value="user">user</option><option value="admin">admin</option></select></div>
  <div class="errmsg" id="cu-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('cu-dlg')">Cancel</button><button class="btn btn-primary" onclick="doCreateUser()">Create</button></div>
</dialog>

<!-- Set Password (admin) -->
<dialog id="sp-dlg">
  <h2>Set Password</h2>
  <div class="fg"><label class="fl">Username</label><div id="sp-who" style="font-size:.82rem;color:#64748b;padding:.25rem 0"></div></div>
  <div class="fg"><label class="fl">New Password</label><input class="fi" id="sp-pw" type="password" autocomplete="new-password"/></div>
  <div class="errmsg" id="sp-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('sp-dlg')">Cancel</button><button class="btn btn-primary" onclick="doSetPw()">Update</button></div>
</dialog>

<!-- Assign Task -->
<dialog id="at-dlg">
  <h2>Assign Task</h2>
  <div class="fg"><label class="fl">Bot</label><div id="at-bot" style="font-size:.82rem;color:#64748b;padding:.25rem 0"></div></div>
  <div class="fg"><label class="fl">Instruction</label><textarea class="fi" id="at-instr" placeholder="Describe the task…" required></textarea></div>
  <div class="fg">
    <label class="fl">Timing</label>
    <label style="font-size:.8rem;display:inline-flex;align-items:center;gap:.4rem;margin-right:.75rem"><input type="radio" name="at-timing" id="at-now" checked onchange="ge('at-sched-wrap').style.display='none'"> Now</label>
    <label style="font-size:.8rem;display:inline-flex;align-items:center;gap:.4rem"><input type="radio" name="at-timing" id="at-later" onchange="ge('at-sched-wrap').style.display='block'"> Schedule</label>
  </div>
  <div class="fg" id="at-sched-wrap" style="display:none"><label class="fl">Schedule At</label><input class="fi" id="at-sched" type="datetime-local"/></div>
  <div class="errmsg" id="at-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('at-dlg')">Cancel</button><button class="btn btn-primary" onclick="doDispatchTask()">Dispatch</button></div>
</dialog>

<!-- Change Own Password -->
<dialog id="cp-dlg">
  <h2>Change Password</h2>
  <div class="fg"><label class="fl">Current Password</label><input class="fi" id="cp-old" type="password"/></div>
  <div class="fg"><label class="fl">New Password</label><input class="fi" id="cp-new" type="password" autocomplete="new-password"/></div>
  <div class="errmsg" id="cp-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('cp-dlg')">Cancel</button><button class="btn btn-primary" onclick="doChangePw()">Update</button></div>
</dialog>

<script>
  // ── State ───────────────────────────────────────────────────────────────────
  var token=null, me=null, myRole=null;
  var allItems=[], allBots=[];
  var dragId=null, setPwTarget=null;
  var activeTab='board', countdown=30, tickTimer=null;

  // ── Util ────────────────────────────────────────────────────────────────────
  function esc(s){if(!s)return'';return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}
  function dlg(id){document.getElementById(id).showModal()}
  function cls(id){document.getElementById(id).close()}
  function ge(id){return document.getElementById(id)}
  function ago(iso){
    var d=new Date(iso),diff=Math.floor((Date.now()-d)/1000);
    if(diff<0||isNaN(diff))return'?';
    if(diff<60)return diff+'s ago';
    if(diff<3600)return Math.floor(diff/60)+'m ago';
    if(diff<86400)return Math.floor(diff/3600)+'h ago';
    return Math.floor(diff/86400)+'d ago';
  }

  // ── API ─────────────────────────────────────────────────────────────────────
  function api(method,url,body){
    var opts={method:method,headers:{}};
    if(body!==null&&body!==undefined){opts.headers['Content-Type']='application/json';opts.body=JSON.stringify(body)}
    if(token)opts.headers['Authorization']='Bearer '+token;
    return fetch(url,opts).then(function(r){
      if(r.status===204)return null;
      return r.json().then(function(d){if(!r.ok)throw new Error(d.error||r.statusText);return d});
    });
  }

  // ── Auth ────────────────────────────────────────────────────────────────────
  function doLogin(){
    var u=ge('login-u').value,p=ge('login-p').value,e=ge('login-err');
    e.style.display='none';
    api('POST','/api/v1/auth/login',{username:u,password:p})
      .then(function(d){
        token=d.token; me=u;
        try{var pl=JSON.parse(atob(token.split('.')[1]));myRole=pl.role||'user'}catch(_){myRole='user'}
        cls('login-dlg'); ge('login-p').value='';
        updateAuthUI(); refreshAll();
      })
      .catch(function(err){e.textContent=err.message||'Login failed';e.style.display='block'});
  }

  function doLogout(){token=null;me=null;myRole=null;updateAuthUI();refreshAll()}

  function updateAuthUI(){
    var on=!!token,admin=myRole==='admin';
    ge('btn-login').style.display=on?'none':'inline-block';
    ge('uinfo').style.display=on?'inline-flex':'none';
    ge('btn-new').style.display=on?'inline-block':'none';
    if(on)ge('ulabel').textContent=me+(admin?' (admin)':'');
    ge('t-users').style.display=admin?'inline-block':'none';
    // re-render so card draggability updates
    renderBoard();
    renderRoster();
  }

  // ── Tab ─────────────────────────────────────────────────────────────────────
  function tab(name){
    activeTab=name;
    document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('on',t.getAttribute('onclick').indexOf("'"+name+"'")>-1)});
    document.querySelectorAll('.pane').forEach(function(p){p.classList.toggle('on',p.id==='pane-'+name)});
    if(name==='tasks')loadTasks();
    if(name==='chat')loadChat();
    if(name==='skills')loadSkills();
    if(name==='dlq')loadDLQ();
    if(name==='users')loadUsers();
  }

  // ── Drag & Drop ─────────────────────────────────────────────────────────────
  function ov(ev){if(!token)return;ev.preventDefault();ev.currentTarget.classList.add('over')}
  function ol(ev){ev.currentTarget.classList.remove('over')}
  function dp(ev){
    ev.preventDefault();
    var col=ev.currentTarget;col.classList.remove('over');
    if(!token||!dragId)return;
    var status=col.dataset.status;
    api('PATCH','/api/v1/board/'+dragId,{status:status})
      .then(function(){dragId=null;loadBoard()})
      .catch(function(e){alert('Move failed: '+e.message)});
  }

  // ── Board ────────────────────────────────────────────────────────────────────
  var colCfg=[
    {status:'backlog',   hdr:'b-backlog',   cnt:'n-backlog'},
    {status:'in-progress',hdr:'b-inprogress',cnt:'n-inprogress'},
    {status:'blocked',   hdr:'b-blocked',   cnt:'n-blocked'},
    {status:'done',      hdr:'b-done',      cnt:'n-done'},
  ];

  function makeCard(it){
    var d=document.createElement('div');
    d.className='card';
    d.draggable=!!token;
    d.style.cursor=token?'grab':'default';
    d.innerHTML=
      '<div class="card-title">'+esc(it.title)+'</div>'+
      (it.description?'<div class="card-desc">'+esc(it.description)+'</div>':'')+
      '<div class="card-foot">'+
        (it.assigned_to?'<span class="card-who">'+esc(it.assigned_to)+'</span>':'')+
        '<span class="card-age">'+ago(it.updated_at)+'</span>'+
      '</div>';
    d.addEventListener('dragstart',function(ev){dragId=it.id;d.classList.add('dragging');ev.dataTransfer.effectAllowed='move'});
    d.addEventListener('dragend',function(){d.classList.remove('dragging')});
    return d;
  }

  function renderBoard(){
    var buckets={backlog:[],blocked:[],done:[],'in-progress':[]};
    allItems.forEach(function(it){(buckets[it.status]||(buckets[it.status]=[])).push(it)});
    colCfg.forEach(function(c){
      var body=ge(c.hdr),cnt=ge(c.cnt),list=buckets[c.status]||[];
      cnt.textContent=list.length;
      body.innerHTML='';
      if(!list.length){body.innerHTML='<div class="nil">No items</div>';return}
      list.forEach(function(it){body.appendChild(makeCard(it))});
    });
  }

  function loadBoard(){
    api('GET','/api/v1/board',null)
      .then(function(items){allItems=items||[];renderBoard();renderRoster()})
      .catch(function(){});
  }

  // ── Roster ───────────────────────────────────────────────────────────────────
  function renderRoster(){
    var el=ge('roster');
    if(!allBots.length){el.innerHTML='<div class="nil" style="padding:1rem;font-size:.7rem">No bots registered</div>';return}
    var active={};
    allItems.forEach(function(it){if(it.status!=='done'&&it.assigned_to)active[it.assigned_to]=(active[it.assigned_to]||0)+1});
    el.innerHTML='';
    allBots.forEach(function(b){
      var on=b.status==='active',n=active[b.name]||0;
      var pct=Math.min(n/6*100,100);
      var fc=n===0?'bfill-none':n<=2?'bfill-lo':n<=5?'bfill-md':'bfill-hi';
      var c=document.createElement('div');c.className='bcard';
      c.innerHTML=
        '<div class="brow">'+
          '<div class="bdot '+(on?'bdot-on':'bdot-off')+'"></div>'+
          '<div class="bname">'+esc(b.name)+'</div>'+
          (n?'<div class="bbadge">'+n+'</div>':'')+
          (on&&token?'<button class="btn btn-ghost btn-sm" onclick="openAssignTask(\''+esc(b.name)+'\')">&#x26A1; Task</button>':'')+
        '</div>'+
        '<div class="bmeta">'+esc(b.bot_type||'')+(on?' &bull; '+ago(b.last_heartbeat):' &bull; inactive')+'</div>'+
        (on?'<div class="bbar"><div class="bfill '+fc+'" style="width:'+pct+'%"></div></div>':'');
      el.appendChild(c);
    });
  }

  function populateBotSelectors(){
    // Keep the chat selector in sync whenever team data changes.
    var sel=ge('chat-bot-sel');
    if(!sel)return;
    var prev=sel.value;
    sel.innerHTML='';
    var defaultVal='';
    allBots.forEach(function(b){
      var o=document.createElement('option');
      o.value=b.name;
      o.textContent=b.name+(b.status==='active'?'':' (offline)');
      if(!defaultVal||b.bot_type==='orchestrator')defaultVal=b.name;
      sel.appendChild(o);
    });
    sel.value=(prev&&allBots.some(function(b){return b.name===prev}))?prev:defaultVal;
  }

  function loadTeam(){
    api('GET','/api/v1/team',null)
      .then(function(bots){
        allBots=bots||[];
        renderRoster();
        populateBotSelectors();
        var act=allBots.filter(function(b){return b.status==='active'}).length;
        var pill=ge('hpill');
        pill.textContent=act+' / '+allBots.length+' active';
        pill.className='hpill '+(act===allBots.length&&allBots.length?'hpill-ok':'hpill-warn');
      })
      .catch(function(){});
  }

  // ── New Item ─────────────────────────────────────────────────────────────────
  function openNewItem(){
    var sel=ge('ni-bot');
    sel.innerHTML='<option value="">Unassigned</option>';
    allBots.forEach(function(b){
      var o=document.createElement('option');o.value=b.name;o.textContent=b.name+(b.status==='active'?'':' (offline)');sel.appendChild(o);
    });
    ge('ni-err').style.display='none';
    dlg('ni-dlg');
  }

  function doCreateItem(){
    var title=ge('ni-title').value.trim(),desc=ge('ni-desc').value.trim(),bot=ge('ni-bot').value,e=ge('ni-err');
    e.style.display='none';
    if(!title){e.textContent='Title is required';e.style.display='block';return}
    api('POST','/api/v1/board',{title:title,description:desc,assigned_to:bot})
      .then(function(){cls('ni-dlg');ge('ni-title').value='';ge('ni-desc').value='';loadBoard()})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Skills ───────────────────────────────────────────────────────────────────
  function loadSkills(){
    var el=ge('skills-body');
    if(!token){el.innerHTML='<div class="empty-state">Sign in to manage skills</div>';return}
    api('GET','/api/v1/skills')
      .then(function(skills){
        if(!skills||!skills.length){el.innerHTML='<div class="empty-state">No skills registered</div>';return}
        var rows=skills.map(function(s){
          var acts='';
          if(s.status==='staged')acts='<div class="acts"><button class="btn btn-success btn-sm" onclick="approveSkill(\''+esc(s.id)+'\')">Approve</button><button class="btn btn-danger btn-sm" onclick="rejectSkill(\''+esc(s.id)+'\')">Reject</button></div>';
          else if(s.status==='active')acts='<button class="btn btn-warn btn-sm" onclick="revokeSkill(\''+esc(s.id)+'\')">Revoke</button>';
          return'<tr><td>'+esc(s.name||s.id)+'</td><td>'+esc(s.bot_type)+'</td><td><span class="pill '+(s.status==='active'?'pill-ok':'pill-warn')+'">'+esc(s.status)+'</span></td><td>'+ago(s.uploaded_at)+'</td><td>'+acts+'</td></tr>';
        }).join('');
        el.innerHTML='<table><thead><tr><th>Name</th><th>Bot Type</th><th>Status</th><th>Uploaded</th><th>Actions</th></tr></thead><tbody>'+rows+'</tbody></table>';
      })
      .catch(function(){el.innerHTML='<div class="empty-state">Failed to load skills</div>'});
  }
  function approveSkill(id){api('POST','/api/v1/skills/'+id+'/approve',{}).then(loadSkills).catch(function(e){alert(e.message)})}
  function rejectSkill(id){api('POST','/api/v1/skills/'+id+'/reject',{}).then(loadSkills).catch(function(e){alert(e.message)})}
  function revokeSkill(id){if(!confirm('Revoke this skill?'))return;api('DELETE','/api/v1/skills/'+id).then(loadSkills).catch(function(e){alert(e.message)})}

  // ── DLQ ──────────────────────────────────────────────────────────────────────
  function loadDLQ(){
    var el=ge('dlq-body');
    if(!token){el.innerHTML='<div class="empty-state">Sign in to view the dead letter queue</div>';return}
    api('GET','/api/v1/dlq')
      .then(function(items){
        if(!items||!items.length){el.innerHTML='<div class="empty-state">Dead letter queue is empty</div>';return}
        var rows=items.map(function(it){
          var body=esc((it.body||'').substring(0,90));
          return'<tr><td style="font-family:monospace;font-size:.68rem">'+esc(it.id)+'</td><td>'+esc(it.queue_name)+'</td><td>'+it.received_count+'</td><td style="max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+body+'</td><td>'+ago(it.last_received)+'</td><td><div class="acts"><button class="btn btn-success btn-sm" onclick="retryDLQ(\''+esc(it.id)+'\')">Retry</button><button class="btn btn-danger btn-sm" onclick="discardDLQ(\''+esc(it.id)+'\')">Discard</button></div></td></tr>';
        }).join('');
        el.innerHTML='<table><thead><tr><th>ID</th><th>Queue</th><th>Attempts</th><th>Body</th><th>Last Seen</th><th>Actions</th></tr></thead><tbody>'+rows+'</tbody></table>';
      })
      .catch(function(){el.innerHTML='<div class="empty-state">Failed to load DLQ</div>'});
  }
  function retryDLQ(id){api('POST','/api/v1/dlq/'+id+'/retry',{}).then(loadDLQ).catch(function(e){alert(e.message)})}
  function discardDLQ(id){if(!confirm('Permanently discard this message?'))return;api('DELETE','/api/v1/dlq/'+id).then(loadDLQ).catch(function(e){alert(e.message)})}

  // ── Users ─────────────────────────────────────────────────────────────────────
  function loadUsers(){
    var el=ge('users-body');
    if(myRole!=='admin'){el.innerHTML='<div class="empty-state">Admin access required</div>';return}
    api('GET','/api/v1/users')
      .then(function(users){
        if(!users||!users.length){el.innerHTML='<div class="empty-state">No users</div>';return}
        var rows=users.map(function(u){
          var acts='<div class="acts">';
          if(u.username!==me){
            acts+='<button class="btn btn-secondary btn-sm" onclick="openSetPw(\''+esc(u.username)+'\')">Set PW</button>';
            acts+=u.enabled?'<button class="btn btn-danger btn-sm" onclick="disableUser(\''+esc(u.username)+'\')">Disable</button>':'<span class="pill pill-off">Disabled</span>';
          }else{
            acts+='<span class="pill pill-ok">You</span>';
          }
          acts+='</div>';
          return'<tr><td>'+esc(u.username)+'</td><td>'+esc(u.display_name||'—')+'</td><td><span class="pill '+(u.role==='admin'?'pill-admin':'pill-user')+'">'+esc(u.role)+'</span></td><td>'+(u.enabled?'<span class="pill pill-ok">Active</span>':'<span class="pill pill-off">Disabled</span>')+'</td><td>'+ago(u.created_at)+'</td><td>'+acts+'</td></tr>';
        }).join('');
        el.innerHTML='<table><thead><tr><th>Username</th><th>Display Name</th><th>Role</th><th>Status</th><th>Created</th><th>Actions</th></tr></thead><tbody>'+rows+'</tbody></table>';
      })
      .catch(function(){el.innerHTML='<div class="empty-state">Failed to load users</div>'});
  }
  function doCreateUser(){
    var u=ge('cu-u').value.trim(),r=ge('cu-r').value,e=ge('cu-err');
    e.style.display='none';
    if(!u){e.textContent='Username required';e.style.display='block';return}
    api('POST','/api/v1/users',{username:u,role:r})
      .then(function(){cls('cu-dlg');ge('cu-u').value='';loadUsers()})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }
  function disableUser(u){if(!confirm('Disable '+u+'?'))return;api('POST','/api/v1/users/'+u+'/disable',{}).then(loadUsers).catch(function(e){alert(e.message)})}
  function openSetPw(u){setPwTarget=u;ge('sp-who').textContent=u;ge('sp-pw').value='';ge('sp-err').style.display='none';dlg('sp-dlg')}
  function doSetPw(){
    var pw=ge('sp-pw').value,e=ge('sp-err');e.style.display='none';
    if(!pw){e.textContent='Password required';e.style.display='block';return}
    api('POST','/api/v1/users/'+setPwTarget+'/password',{password:pw})
      .then(function(){cls('sp-dlg')})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Change own password ───────────────────────────────────────────────────────
  function openChgPw(){ge('cp-old').value='';ge('cp-new').value='';ge('cp-err').style.display='none';dlg('cp-dlg')}
  function doChangePw(){
    var o=ge('cp-old').value,n=ge('cp-new').value,e=ge('cp-err');e.style.display='none';
    if(!o||!n){e.textContent='Both fields required';e.style.display='block';return}
    api('POST','/api/v1/profile/password',{old_password:o,new_password:n})
      .then(function(){cls('cp-dlg')})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Tasks ─────────────────────────────────────────────────────────────────────
  function loadTasks(){
    var el=ge('tasks-body');
    if(!token){el.innerHTML='<div class="empty-state">Sign in to view tasks</div>';return}
    api('GET','/api/v1/tasks')
      .then(function(tasks){
        if(!tasks||!tasks.length){el.innerHTML='<div class="empty-state">No direct tasks</div>';return}
        var rows=tasks.map(function(t){
          var sc=t.status==='pending'?'pill-warn':t.status==='dispatched'?'pill-ok':'pill-off';
          var instr=esc((t.instruction||'').substring(0,60))+(t.instruction&&t.instruction.length>60?'…':'');
          return'<tr><td>'+esc(t.bot_name)+'</td><td>'+instr+'</td><td><span class="pill '+sc+'">'+esc(t.status)+'</span></td><td>'+(t.scheduled_at?ago(t.scheduled_at):'—')+'</td><td>'+(t.dispatched_at?ago(t.dispatched_at):'—')+'</td><td>'+ago(t.created_at)+'</td></tr>';
        }).join('');
        el.innerHTML='<table><thead><tr><th>Bot</th><th>Instruction</th><th>Status</th><th>Scheduled At</th><th>Dispatched At</th><th>Created</th></tr></thead><tbody>'+rows+'</tbody></table>';
      })
      .catch(function(){el.innerHTML='<div class="empty-state">Failed to load tasks</div>'});
  }

  function openAssignTask(botName){
    ge('at-bot').textContent=botName;
    ge('at-instr').value='';
    ge('at-now').checked=true;
    ge('at-sched-wrap').style.display='none';
    ge('at-sched').value='';
    ge('at-err').style.display='none';
    dlg('at-dlg');
  }

  function doDispatchTask(){
    var botName=ge('at-bot').textContent;
    var instruction=ge('at-instr').value.trim();
    var isNow=ge('at-now').checked;
    var schedVal=ge('at-sched').value;
    var e=ge('at-err');
    e.style.display='none';
    if(!instruction){e.textContent='Instruction is required';e.style.display='block';return}
    var body={instruction:instruction};
    if(!isNow&&schedVal){body.scheduled_at=new Date(schedVal).toISOString()}
    api('POST','/api/v1/bots/'+botName+'/tasks',body)
      .then(function(){cls('at-dlg');tab('tasks');loadTasks()})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Chat ──────────────────────────────────────────────────────────────────────
  function loadChat(){
    var el=ge('chat-hist');
    if(!token){el.innerHTML='<div class="nil">Sign in to chat</div>';return}
    // Ensure selector is current (loadTeam populates it; this is a safety call
    // for the case where the user switches to Chat before loadTeam resolves).
    if(!ge('chat-bot-sel').options.length)populateBotSelectors();
    api('GET','/api/v1/chat',null)
      .then(function(msgs){
        el.innerHTML='';
        if(!msgs||!msgs.length){el.innerHTML='<div class="nil">No messages yet</div>';return}
        // API returns newest-first; reverse to show oldest-first.
        var ordered=msgs.slice().reverse();
        ordered.forEach(function(m){el.appendChild(renderChatMsg(m))});
        el.scrollTop=el.scrollHeight;
      })
      .catch(function(){el.innerHTML='<div class="nil">Failed to load chat</div>'});
  }

  function renderChatMsg(msg){
    var wrap=document.createElement('div');
    var isOut=msg.direction==='outbound';
    wrap.style.display='flex';
    wrap.style.flexDirection='column';
    wrap.style.alignItems=isOut?'flex-end':'flex-start';
    var bubble=document.createElement('div');
    bubble.className='chat-bubble '+(isOut?'chat-out':'chat-in');
    bubble.textContent=msg.content||'';
    var meta=document.createElement('div');
    meta.className='chat-meta';
    meta.textContent=esc(msg.bot_name)+' &bull; '+ago(msg.created_at);
    wrap.appendChild(bubble);
    wrap.appendChild(meta);
    return wrap;
  }

  function sendChat(){
    if(!token){alert('Please sign in first');return}
    var bot=ge('chat-bot-sel').value;
    var content=ge('chat-input').value.trim();
    if(!bot){alert('Select a bot first');return}
    if(!content)return;
    api('POST','/api/v1/chat/'+bot,{content:content})
      .then(function(){ge('chat-input').value='';loadChat()})
      .catch(function(e){alert('Send failed: '+e.message)});
  }

  // Enter sends; Shift+Enter inserts newline.
  document.addEventListener('DOMContentLoaded',function(){
    var ta=ge('chat-input');
    if(ta){
      ta.addEventListener('keydown',function(e){
        if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendChat()}
      });
    }
  });

  // ── Refresh loop ──────────────────────────────────────────────────────────────
  function refreshAll(){
    loadBoard(); loadTeam();
    if(activeTab==='tasks')loadTasks();
    if(activeTab==='chat')loadChat();
    if(activeTab==='skills')loadSkills();
    if(activeTab==='dlq')loadDLQ();
    if(activeTab==='users')loadUsers();
  }

  function startTick(){
    clearInterval(tickTimer); countdown=30;
    ge('tick').textContent='refresh in 30s';
    tickTimer=setInterval(function(){
      countdown--;
      ge('tick').textContent=countdown<=0?'refreshing…':'refresh in '+countdown+'s';
      if(countdown<=0){refreshAll();countdown=30}
    },1000);
  }

  refreshAll();
  startTick();
</script>
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
