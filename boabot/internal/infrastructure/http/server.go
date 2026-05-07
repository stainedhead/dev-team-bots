// Package httpserver provides the orchestrator REST API and Kanban web UI.
package httpserver

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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

// PluginInstaller is the subset of the install use case required by the server.
type PluginInstaller interface {
	Install(ctx context.Context, registryName, name, version, actor string) (domain.Plugin, error)
}

// PluginManager is the subset of the manage use case required by the server.
type PluginManager interface {
	List(ctx context.Context) ([]domain.Plugin, error)
	Get(ctx context.Context, id string) (domain.Plugin, error)
	Approve(ctx context.Context, id, actor string) error
	Reject(ctx context.Context, id, actor string) error
	Enable(ctx context.Context, id, actor string) error
	Disable(ctx context.Context, id, actor string) error
	Reload(ctx context.Context, id, actor string) error
	Remove(ctx context.Context, id, actor string) error
}

// PluginRegistryUseCase is the subset of the registry use case required by the server.
type PluginRegistryUseCase interface {
	List(ctx context.Context) ([]domain.PluginRegistry, error)
	Add(ctx context.Context, reg domain.PluginRegistry) error
	Remove(ctx context.Context, name string) error
	FetchIndex(ctx context.Context, name string, force bool) (domain.RegistryIndex, error)
}

// Config holds all stores and providers required by the orchestrator server.
type Config struct {
	Auth            AuthProvider
	Board           domain.BoardStore
	Team            domain.ControlPlane
	Users           domain.UserStore
	Skills          domain.SkillRegistry
	DLQ             domain.DLQStore
	Tasks           domain.DirectTaskStore
	Dispatcher      domain.TaskDispatcher
	Chat            domain.ChatStore
	AskRouter       domain.AskRouter    // optional; routes mid-task questions to running bots
	Pool            domain.TechLeadPool // optional; nil means pool endpoint returns empty
	AllowedWorkDirs []string            // whitelisted base directories for item working directories
	TaskLogBase     string              // base directory for per-task log directories (optional)
	// Plugin system — optional. Routes are registered only when Plugins is non-nil.
	Plugins        domain.PluginStore
	RegistryMgr    domain.RegistryManager
	PluginInstall  PluginInstaller
	PluginManage   PluginManager
	PluginRegistry PluginRegistryUseCase
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

	// Work directory roots (public read — UI needs these before login)
	mux.HandleFunc("GET /api/v1/workdirs", s.handleWorkDirList)

	// Board — read endpoints are public; write endpoints require auth
	mux.HandleFunc("GET /api/v1/board", s.handleBoardList)
	mux.HandleFunc("GET /api/v1/board/{id}", s.handleBoardGet)
	mux.HandleFunc("POST /api/v1/board", s.auth(s.handleBoardCreate))
	mux.HandleFunc("PATCH /api/v1/board/{id}", s.auth(s.handleBoardUpdate))
	mux.HandleFunc("DELETE /api/v1/board/{id}", s.auth(s.handleBoardDelete))
	mux.HandleFunc("POST /api/v1/board/{id}/assign", s.auth(s.handleBoardAssign))
	mux.HandleFunc("POST /api/v1/board/{id}/close", s.auth(s.handleBoardClose))
	mux.HandleFunc("POST /api/v1/board/{id}/attachments", s.auth(s.handleBoardAttachmentUpload))
	mux.HandleFunc("GET /api/v1/board/{id}/attachments/{attId}", s.auth(s.handleBoardAttachmentGet))
	mux.HandleFunc("DELETE /api/v1/board/{id}/attachments/{attId}", s.auth(s.handleBoardAttachmentDelete))

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
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.auth(s.handleTaskGet))
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.auth(s.adminOnly(s.handleTaskDelete)))
	mux.HandleFunc("POST /api/v1/tasks/{id}/run", s.auth(s.handleTaskRunNow))
	mux.HandleFunc("POST /api/v1/bots/{name}/tasks", s.auth(s.handleBotTaskCreate))
	mux.HandleFunc("GET /api/v1/bots/{name}/tasks", s.auth(s.handleBotTaskList))

	// Skill upload (admin)
	mux.HandleFunc("POST /api/v1/skills", s.auth(s.adminOnly(s.handleSkillUpload)))

	// Board activity, ask, and per-item messages — require auth
	mux.HandleFunc("GET /api/v1/board/{id}/activity", s.auth(s.handleBoardActivity))
	mux.HandleFunc("POST /api/v1/board/{id}/ask", s.auth(s.handleBoardAsk))
	mux.HandleFunc("GET /api/v1/board/{id}/messages", s.auth(s.handleBoardMessages))

	// Threads — require auth
	mux.HandleFunc("GET /api/v1/threads", s.auth(s.handleThreadList))
	mux.HandleFunc("POST /api/v1/threads", s.auth(s.handleThreadCreate))
	mux.HandleFunc("DELETE /api/v1/threads/{id}", s.auth(s.handleThreadDelete))
	mux.HandleFunc("GET /api/v1/threads/{id}/messages", s.auth(s.handleThreadMessages))

	// Chat — require auth
	mux.HandleFunc("GET /api/v1/chat", s.auth(s.handleChatList))
	mux.HandleFunc("GET /api/v1/chat/{bot}", s.auth(s.handleChatBotList))
	mux.HandleFunc("POST /api/v1/chat/{bot}", s.auth(s.handleChatSend))

	// Tech-lead pool
	mux.HandleFunc("GET /api/v1/pool", s.auth(s.handlePoolList))

	// Plugin registry & management (optional — registered only if plugin store is configured)
	if s.cfg.Plugins != nil {
		mux.HandleFunc("GET /api/v1/registries", s.handleRegistriesList)
		mux.HandleFunc("POST /api/v1/registries", s.auth(s.adminOnly(s.handleRegistriesAdd)))
		mux.HandleFunc("DELETE /api/v1/registries/{name}", s.auth(s.adminOnly(s.handleRegistriesRemove)))
		mux.HandleFunc("GET /api/v1/registries/{name}/index", s.auth(s.adminOnly(s.handleRegistriesFetchIndex)))

		mux.HandleFunc("GET /api/v1/plugins", s.handlePluginsList)
		mux.HandleFunc("GET /api/v1/plugins/{id}", s.handlePluginsGet)
		mux.HandleFunc("POST /api/v1/plugins", s.auth(s.adminOnly(s.handlePluginsInstall)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/approve", s.auth(s.adminOnly(s.handlePluginsApprove)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/reject", s.auth(s.adminOnly(s.handlePluginsReject)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/enable", s.auth(s.adminOnly(s.handlePluginsEnable)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/disable", s.auth(s.adminOnly(s.handlePluginsDisable)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/reload", s.auth(s.adminOnly(s.handlePluginsReload)))
		mux.HandleFunc("DELETE /api/v1/plugins/{id}", s.auth(s.adminOnly(s.handlePluginsRemove)))
	}

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

// ── work-dir helpers ──────────────────────────────────────────────────────────

// isAllowedWorkDir reports whether p is equal to or a child of one of the
// configured allowed roots. Both p and each root are cleaned before comparison
// to prevent path-traversal bypasses.
func isAllowedWorkDir(p string, roots []string) bool {
	p = filepath.Clean(p)
	for _, root := range roots {
		root = filepath.Clean(root)
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Server) handleWorkDirList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.AllowedWorkDirs)
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
		WorkDir     string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkDir != "" {
		if len(s.cfg.AllowedWorkDirs) == 0 {
			writeError(w, http.StatusBadRequest, "no work directories are configured on this server")
			return
		}
		if !isAllowedWorkDir(req.WorkDir, s.cfg.AllowedWorkDirs) {
			writeError(w, http.StatusBadRequest, "work_dir is outside the configured allowed directories")
			return
		}
	}
	claims := claimsFromContext(r)
	now := time.Now().UTC()
	item, err := s.cfg.Board.Create(r.Context(), domain.WorkItem{
		Title:       req.Title,
		Description: req.Description,
		AssignedTo:  req.AssignedTo,
		WorkDir:     req.WorkDir,
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
		WorkDir     *string `json:"work_dir"`
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
		if existing.Status != domain.WorkItemStatusInProgress {
			existing.ActiveTaskID = ""
		}
	}
	if req.AssignedTo != nil {
		existing.AssignedTo = *req.AssignedTo
	}
	if req.WorkDir != nil {
		if *req.WorkDir != "" {
			if len(s.cfg.AllowedWorkDirs) == 0 {
				writeError(w, http.StatusBadRequest, "no work directories are configured on this server")
				return
			}
			if !isAllowedWorkDir(*req.WorkDir, s.cfg.AllowedWorkDirs) {
				writeError(w, http.StatusBadRequest, "work_dir is outside the configured allowed directories")
				return
			}
		}
		existing.WorkDir = *req.WorkDir
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
		if updated.WorkDir != "" {
			instruction += fmt.Sprintf("\n\nWorking directory: %s\nYou may read and write files in this directory to complete your work. If it is a git repository you may also use git commands.", updated.WorkDir)
		}
		if len(s.cfg.AllowedWorkDirs) > 0 {
			instruction += fmt.Sprintf("\n\nSECURITY CONSTRAINT: You are only permitted to access files within these directories: %s\nDo not read, write, or execute files outside these paths.", strings.Join(s.cfg.AllowedWorkDirs, ", "))
		}
		for _, att := range updated.Attachments {
			raw, decErr := base64.StdEncoding.DecodeString(att.Content)
			if decErr != nil {
				continue
			}
			ct := att.ContentType
			if ct == "" || strings.HasPrefix(ct, "text/") || ct == "application/json" ||
				strings.HasSuffix(att.Name, ".md") || strings.HasSuffix(att.Name, ".yaml") ||
				strings.HasSuffix(att.Name, ".yml") || strings.HasSuffix(att.Name, ".go") ||
				strings.HasSuffix(att.Name, ".txt") {
				instruction += fmt.Sprintf("\n\n--- Attachment: %s ---\n%s", att.Name, string(raw))
			}
		}
		if task, dispErr := s.cfg.Dispatcher.Dispatch(r.Context(), updated.AssignedTo, instruction, nil, domain.DirectTaskSourceBoard, "", updated.WorkDir); dispErr != nil {
			slog.Warn("board→bot dispatch failed", "bot", updated.AssignedTo, "item", updated.ID, "err", dispErr)
			// Non-fatal: the board update already succeeded.
		} else {
			// Store task ID back into the board item so the UI can track progress.
			updated.ActiveTaskID = task.ID
			if _, updateErr := s.cfg.Board.Update(r.Context(), updated); updateErr != nil {
				slog.Warn("board item active_task_id update failed", "err", updateErr)
			}
		}
	}

	writeJSON(w, http.StatusOK, updated)
}

// newAttachmentID generates a random hex ID for an attachment.
func newAttachmentID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) handleBoardAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}
	const maxFileSize = 10 << 20 // 10 MB
	for _, fh := range r.MultipartForm.File["files"] {
		f, openErr := fh.Open()
		if openErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to open uploaded file")
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, maxFileSize+1))
		_ = f.Close()
		if readErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read uploaded file")
			return
		}
		if len(raw) > maxFileSize {
			writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 10 MB limit")
			return
		}
		attID, idErr := newAttachmentID()
		if idErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate attachment ID")
			return
		}
		ct := fh.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		att := domain.Attachment{
			ID:          attID,
			Name:        filepath.Base(fh.Filename), // strip any path the client may have sent
			ContentType: ct,
			Size:        len(raw),
			UploadedAt:  time.Now().UTC(),
		}
		if item.WorkDir != "" {
			// Write to disk inside the working directory.
			destPath := filepath.Join(item.WorkDir, att.Name)
			if mkErr := os.MkdirAll(item.WorkDir, 0o755); mkErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to create working directory")
				return
			}
			if wErr := os.WriteFile(destPath, raw, 0o644); wErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to write file to working directory")
				return
			}
			att.StoragePath = destPath
		} else {
			att.Content = base64.StdEncoding.EncodeToString(raw)
		}
		item.Attachments = append(item.Attachments, att)
	}
	updated, err := s.cfg.Board.Update(r.Context(), item)
	if err != nil {
		writeInternalError(w, "attachment upload", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleBoardAttachmentGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	attId := r.PathValue("attId")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	var found *domain.Attachment
	for i := range item.Attachments {
		if item.Attachments[i].ID == attId {
			found = &item.Attachments[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	var raw []byte
	if found.StoragePath != "" {
		raw, err = os.ReadFile(found.StoragePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "attachment file not found on disk")
			return
		}
	} else {
		raw, err = base64.StdEncoding.DecodeString(found.Content)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode attachment")
			return
		}
	}
	ct := found.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if strings.HasPrefix(ct, "text/") || ct == "application/json" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, found.Name))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, found.Name))
	}
	_, _ = w.Write(raw)
}

func (s *Server) handleBoardAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	attId := r.PathValue("attId")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	var toDelete *domain.Attachment
	filtered := item.Attachments[:0]
	for i, a := range item.Attachments {
		if a.ID == attId {
			toDelete = &item.Attachments[i]
		} else {
			filtered = append(filtered, a)
		}
	}
	if toDelete == nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if toDelete.StoragePath != "" {
		_ = os.Remove(toDelete.StoragePath) // best-effort; don't block the delete on a missing file
	}
	item.Attachments = filtered
	if _, err := s.cfg.Board.Update(r.Context(), item); err != nil {
		writeInternalError(w, "attachment delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBoardAssign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	var req struct {
		BotName string `json:"bot_name"`
		BotID   string `json:"bot_id"` // legacy alias
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := req.BotName
	if name == "" {
		name = req.BotID
	}
	existing.AssignedTo = name
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

func (s *Server) handleBoardDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Board.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "item not found")
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
	var filtered []domain.DirectTask
	for _, t := range tasks {
		if t.Source != domain.DirectTaskSourceChat {
			filtered = append(filtered, t)
		}
	}
	if filtered == nil {
		filtered = []domain.DirectTask{}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleBotTaskCreate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "task dispatch not available")
		return
	}
	name := r.PathValue("name")
	var req struct {
		Title       string     `json:"title,omitempty"`
		Instruction string     `json:"instruction"`
		ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
		WorkDir     string     `json:"work_dir,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Instruction == "" {
		writeError(w, http.StatusBadRequest, "instruction must not be empty")
		return
	}
	task, err := s.cfg.Dispatcher.Dispatch(r.Context(), name, req.Instruction, req.ScheduledAt, domain.DirectTaskSourceOperator, "", req.WorkDir)
	if err != nil {
		writeInternalError(w, "bot task create", err)
		return
	}
	if req.Title != "" && s.cfg.Tasks != nil {
		task.Title = req.Title
		if updated, updErr := s.cfg.Tasks.Update(r.Context(), task); updErr == nil {
			task = updated
		}
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
	var filtered []domain.DirectTask
	for _, t := range tasks {
		if t.Source != domain.DirectTaskSourceChat {
			filtered = append(filtered, t)
		}
	}
	if filtered == nil {
		filtered = []domain.DirectTask{}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "tasks not available")
		return
	}
	id := r.PathValue("id")
	task, err := s.cfg.Tasks.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "tasks not available")
		return
	}
	id := r.PathValue("id")
	task, err := s.cfg.Tasks.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if delErr := s.cfg.Tasks.Delete(r.Context(), id); delErr != nil {
		writeInternalError(w, "task delete", delErr)
		return
	}
	// Clean up log directory if configured.
	if s.cfg.TaskLogBase != "" {
		_ = os.RemoveAll(filepath.Join(s.cfg.TaskLogBase, task.ID))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTaskRunNow(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "task dispatch not available")
		return
	}
	id := r.PathValue("id")
	task, err := s.cfg.Dispatcher.RunNow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleSkillUpload(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skills not available")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse upload")
		return
	}
	name := r.FormValue("name")
	botType := r.FormValue("bot_type")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	fh := r.MultipartForm.File["file"]
	if len(fh) == 0 {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	header := fh[0]
	f, err := header.Open()
	if err != nil {
		writeInternalError(w, "open upload", err)
		return
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(f)
	if err != nil {
		writeInternalError(w, "read upload", err)
		return
	}

	skillFiles := make(map[string][]byte)

	filename := header.Filename
	if strings.HasSuffix(strings.ToLower(filename), ".zip") {
		// Unzip and preserve directory structure.
		zr, zipErr := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if zipErr != nil {
			writeError(w, http.StatusBadRequest, "invalid zip file")
			return
		}
		for _, zf := range zr.File {
			if zf.FileInfo().IsDir() {
				continue
			}
			rc, openErr := zf.Open()
			if openErr != nil {
				continue
			}
			data, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				continue
			}
			// Sanitize path to prevent path traversal.
			clean := filepath.Clean(zf.Name)
			if strings.HasPrefix(clean, "..") {
				continue
			}
			skillFiles[clean] = data
		}
	} else {
		// Single .md or other text file.
		skillFiles[filename] = content
	}

	if len(skillFiles) == 0 {
		writeError(w, http.StatusBadRequest, "no files in upload")
		return
	}

	skill, err := s.cfg.Skills.Stage(r.Context(), name, botType, skillFiles)
	if err != nil {
		writeInternalError(w, "skill stage", err)
		return
	}
	writeJSON(w, http.StatusCreated, skill)
}

func (s *Server) handleBoardActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	type activityResponse struct {
		Item domain.WorkItem    `json:"item"`
		Task *domain.DirectTask `json:"task,omitempty"`
	}
	resp := activityResponse{Item: item}
	if item.ActiveTaskID != "" && s.cfg.Tasks != nil {
		if task, taskErr := s.cfg.Tasks.Get(r.Context(), item.ActiveTaskID); taskErr == nil {
			resp.Task = &task
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleBoardAsk(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	item, err := s.cfg.Board.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if item.AssignedTo == "" {
		writeError(w, http.StatusBadRequest, "item has no assigned bot")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}

	ctx := r.Context()
	threadID := "board-" + id

	// Store the user's question in the item-private thread.
	userMsg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   item.AssignedTo,
		Direction: domain.ChatDirectionOutbound,
		Content:   req.Content,
	}
	_ = s.cfg.Chat.Append(ctx, userMsg)

	// Route the question to the running bot for a mid-task reply.
	if s.cfg.AskRouter != nil {
		s.cfg.AskRouter.Enqueue(item.AssignedTo, domain.AskRequest{
			Question: fmt.Sprintf("Regarding board item '%s': %s", item.Title, req.Content),
			ReplyFn: func(reply string) {
				botMsg := domain.ChatMessage{
					ThreadID:  threadID,
					BotName:   item.AssignedTo,
					Direction: domain.ChatDirectionInbound,
					Content:   reply,
				}
				_ = s.cfg.Chat.Append(context.Background(), botMsg)
			},
		})
	}

	writeJSON(w, http.StatusCreated, userMsg)
}

// handleBoardMessages returns the private per-item conversation thread.
func (s *Server) handleBoardMessages(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	msgs, err := s.cfg.Chat.List(r.Context(), "board-"+id)
	if err != nil {
		writeInternalError(w, "board messages", err)
		return
	}
	if msgs == nil {
		msgs = []domain.ChatMessage{}
	}
	// Reverse to chronological order (oldest first for conversation display).
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	writeJSON(w, http.StatusOK, msgs)
}

// ── chat handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleChatList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	all, err := s.cfg.Chat.ListAll(r.Context())
	if err != nil {
		writeInternalError(w, "chat list all", err)
		return
	}
	// Exclude board-item private conversations from the global chat screen.
	msgs := all[:0]
	for _, m := range all {
		if !strings.HasPrefix(m.ThreadID, "board-") {
			msgs = append(msgs, m)
		}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleChatBotList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	bot := r.PathValue("bot")
	msgs, err := s.cfg.Chat.ListByBot(r.Context(), bot)
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
		Content  string `json:"content"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content must not be empty")
		return
	}

	ctx := r.Context()

	// Ensure a thread exists.
	threadID := req.ThreadID
	if threadID == "" {
		t, err := s.cfg.Chat.CreateThread(ctx, fmt.Sprintf("Chat with %s", bot), []string{bot})
		if err != nil {
			writeInternalError(w, "create thread", err)
			return
		}
		threadID = t.ID
	}

	// Record the outbound message.
	msg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   bot,
		Direction: domain.ChatDirectionOutbound,
		Content:   req.Content,
	}
	if err := s.cfg.Chat.Append(ctx, msg); err != nil {
		writeInternalError(w, "chat append", err)
		return
	}

	// Build instruction with conversation context (last 10 messages in thread).
	instruction := req.Content
	if history, err := s.cfg.Chat.List(ctx, threadID); err == nil && len(history) > 1 {
		// history is newest-first; reverse for chronological order, skip the message we just appended.
		var prior []domain.ChatMessage
		for i := len(history) - 1; i >= 1 && len(prior) < 10; i-- {
			prior = append(prior, history[i])
		}
		if len(prior) > 0 {
			var sb strings.Builder
			sb.WriteString("Prior conversation context (oldest first):\n")
			for _, m := range prior {
				who := "Operator"
				if m.Direction == domain.ChatDirectionInbound {
					who = m.BotName
				}
				sb.WriteString(fmt.Sprintf("%s: %s\n", who, m.Content))
			}
			sb.WriteString("\nOperator: ")
			sb.WriteString(req.Content)
			instruction = sb.String()
		}
	}

	task, err := s.cfg.Dispatcher.Dispatch(ctx, bot, instruction, nil, domain.DirectTaskSourceChat, threadID, "")
	if err != nil {
		writeInternalError(w, "chat dispatch", err)
		return
	}

	msg.TaskID = task.ID
	msg.ThreadID = threadID
	writeJSON(w, http.StatusCreated, msg)
}

// ── thread handlers ───────────────────────────────────────────────────────────

func (s *Server) handleThreadList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	threads, err := s.cfg.Chat.ListThreads(r.Context())
	if err != nil {
		writeInternalError(w, "thread list", err)
		return
	}
	if threads == nil {
		threads = []domain.ChatThread{}
	}
	writeJSON(w, http.StatusOK, threads)
}

func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	var req struct {
		Title        string   `json:"title"`
		Participants []string `json:"participants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	thread, err := s.cfg.Chat.CreateThread(r.Context(), req.Title, req.Participants)
	if err != nil {
		writeInternalError(w, "thread create", err)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	if err := s.cfg.Chat.DeleteThread(r.Context(), id); err != nil {
		writeInternalError(w, "thread delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleThreadMessages(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	msgs, err := s.cfg.Chat.List(r.Context(), id)
	if err != nil {
		writeInternalError(w, "thread messages", err)
		return
	}
	if msgs == nil {
		msgs = []domain.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
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

    /* ── Pre-login locked state ── */
    body.locked .tab{opacity:.3;pointer-events:none;cursor:not-allowed}
    body.locked .btn:not(#btn-login){opacity:.3;pointer-events:none;cursor:not-allowed}
    body.locked #login-dlg .btn{opacity:1!important;pointer-events:auto!important;cursor:pointer!important}
    body.locked #btn-login{background:#16a34a!important;color:#fff!important;border-color:#15803d!important}
    body.locked #btn-login:hover{filter:brightness(1.12)}

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
    .pane.on{display:flex;flex-direction:column;overflow:hidden}

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
    .pill-ok{background:#166534;color:#bbf7d0}
    .pill-warn{background:#854d0e;color:#fef08a}
    .pill-info{background:#c2410c;color:#ffedd5}
    .pill-off{background:#991b1b;color:#fecaca}
    .pill-admin{background:#312e81;color:#a5b4fc}
    .pill-user{background:#1e293b;color:#64748b}
    .acts{display:flex;gap:.3rem;align-items:center}
    .empty-state{text-align:center;padding:3rem;color:#1e2d4a;font-style:italic;font-size:.8rem}

    /* ── Dialogs ── */
    dialog{background:#0f1829;color:#e2e8f0;border:1px solid #1a2744;border-radius:.625rem;padding:1.375rem;min-width:min(560px,95vw);box-shadow:0 20px 60px #000a}
    dialog::backdrop{background:#000b}
    dialog h2{font-size:.95rem;font-weight:600;margin-bottom:1rem}
    .fg{margin-bottom:.75rem}
    .fl{display:block;font-size:.65rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#64748b;margin-bottom:.3rem}
    .fi{width:100%;padding:.6rem .75rem;background:#080e1a;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.9rem}
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
    .chat-thinking{display:flex;gap:4px;align-items:center;padding:.4rem .6rem}
    .chat-thinking span{width:7px;height:7px;border-radius:50%;background:#475569;animation:blink 1.4s infinite both}
    .chat-thinking span:nth-child(2){animation-delay:.2s}
    .chat-thinking span:nth-child(3){animation-delay:.4s}
    @keyframes blink{0%,80%,100%{opacity:.2}40%{opacity:1}}
    .convo-bar{display:flex;flex-wrap:wrap;gap:.35rem;align-items:center;padding:.5rem 1rem;border-bottom:1px solid #1a2744;min-height:2.5rem}
    .convo-chip{display:flex;align-items:center;gap:.3rem;background:#1e3a5f;border-radius:1rem;padding:.15rem .55rem;font-size:.72rem;color:#93c5fd}
    .convo-chip button{background:none;border:none;color:#64748b;cursor:pointer;font-size:.8rem;line-height:1;padding:0}
    .convo-chip button:hover{color:#e2e8f0}
    .convo-add{background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.72rem;padding:.15rem .4rem;cursor:pointer}
    .chat-input-row{display:flex;gap:.5rem;padding:.75rem 1rem;border-top:1px solid #1a2744;flex-shrink:0}
    .chat-input-row textarea{flex:1;padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.82rem;resize:none;height:56px}
    .chat-input-row select{padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.78rem}

    /* ── Thread sidebar ── */
    .thread-sidebar{width:220px;border-right:1px solid #1a2744;display:flex;flex-direction:column;overflow:hidden;flex-shrink:0}
    .thread-sidebar-hdr{display:flex;justify-content:space-between;align-items:center;padding:.5rem .75rem;border-bottom:1px solid #1a2744;font-size:.75rem;color:#94a3b8}
    .thread-list{flex:1;overflow-y:auto}
    .thread-item{position:relative;padding:.55rem .75rem;cursor:pointer;border-bottom:1px solid #0f1929;transition:background .15s}
    .thread-item:hover{background:#0d1a30}
    .thread-item.active{background:#1e3a5f}
    .thread-title{font-size:.78rem;color:#e2e8f0;margin-bottom:.15rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;padding-right:1.2rem}
    .thread-meta{font-size:.66rem;color:#475569;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .thread-del{position:absolute;top:.4rem;right:.4rem;background:none;border:none;color:#475569;cursor:pointer;font-size:.75rem;opacity:0;transition:opacity .15s}
    .thread-item:hover .thread-del{opacity:1}

    /* ── Task filter buttons ── */
    .task-filter-btn{background:#0f1829;border:1px solid #1a2744;color:#64748b;font-size:.72rem;padding:.25rem .7rem}
    .task-filter-btn.active{background:#1e3a5f;border-color:#2d5a8e;color:#93c5fd;box-shadow:inset 0 1px 3px rgba(0,0,0,.5)}
    .task-filter-btn:hover:not(.active){border-color:#2d3e5a;color:#94a3b8}

    /* ── Board card badge ── */
    .card-working{font-size:.65rem;color:#fbbf24;margin-top:.2rem;animation:blink 1.5s infinite}

    /* ── Context panel ── */
    .ctx-panel{background:#070d1a;border-top:2px solid #1a2744;overflow:hidden;display:flex;flex-direction:column;flex-shrink:0}
    .ctx-resize-handle{height:6px;cursor:ns-resize;background:transparent;flex-shrink:0;display:flex;align-items:center;justify-content:center}
    .ctx-resize-handle::after{content:'';width:32px;height:2px;border-radius:1px;background:#1a2744}
    .ctx-hdr{display:flex;align-items:center;gap:.75rem;padding:.5rem 1rem;border-bottom:1px solid #1a2744;flex-shrink:0}
    .ctx-title{font-size:.82rem;color:#e2e8f0;font-weight:500;flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .ctx-tabs{display:flex;gap:.25rem}
    .ctx-tab{background:none;border:none;color:#64748b;cursor:pointer;font-size:.72rem;padding:.2rem .5rem;border-radius:.25rem}
    .ctx-tab.on{background:#1e3a5f;color:#93c5fd}
    .ctx-body{flex:1;overflow-y:auto;padding:.75rem 1rem;font-size:.78rem;color:#cbd5e1}
    .ctx-row{display:flex;gap:.5rem;margin-bottom:.4rem}
    .ctx-lbl{color:#475569;min-width:80px;flex-shrink:0}
    .ctx-val{color:#e2e8f0;word-break:break-word;flex:1;min-width:0}
    .ctx-output{background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;padding:.6rem .75rem;white-space:pre-wrap;font-family:monospace;font-size:.74rem;line-height:1.5;color:#94a3b8;max-height:160px;overflow-y:auto}
    .ctx-ask-row{display:flex;gap:.5rem;padding:.5rem 1rem;border-top:1px solid #1a2744;flex-shrink:0}
    .ctx-ask-row input{flex:1;padding:.35rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.78rem}
    .ask-thread{display:flex;flex-direction:column;gap:.5rem}
    .ask-msg{max-width:92%;padding:.4rem .6rem;border-radius:.4rem;font-size:.78rem;line-height:1.45}
    .ask-msg-user{align-self:flex-end;background:#1e3a5f;color:#e2e8f0}
    .ask-msg-bot{align-self:flex-start;background:#0f1e35;color:#cbd5e1;border:1px solid #1a2744}
    .ask-msg-label{font-size:.65rem;color:#64748b;margin-bottom:.2rem}
    .ctx-close{background:none;border:none;color:#475569;cursor:pointer;font-size:.9rem;padding:0}
    .ctx-close:hover{color:#e2e8f0}
    .ctx-working{color:#fbbf24;font-size:.75rem;animation:blink 1.5s infinite}

    /* ── Scrollbars ── */
    ::-webkit-scrollbar{width:4px;height:4px}
    ::-webkit-scrollbar-track{background:transparent}
    ::-webkit-scrollbar-thumb{background:#1a2744;border-radius:2px}
    ::-webkit-scrollbar-thumb:hover{background:#2d3e5a}

    /* ── Attachments ── */
    .att-list{display:flex;flex-direction:column;gap:.35rem;margin-top:.5rem}
    .att-row{display:flex;align-items:center;gap:.5rem;padding:.35rem .5rem;background:#0d1829;border-radius:4px;font-size:.78rem}
    .att-name{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#94a3b8}
    .att-acts{display:flex;gap:.25rem;flex-shrink:0}
    .upload-btn{display:inline-flex;align-items:center;gap:.4rem;padding:.3rem .7rem;background:#1a2744;border:1px solid #2d3f6b;border-radius:4px;color:#94a3b8;font-size:.78rem;cursor:pointer}
    .upload-btn:hover{background:#243460;color:#e2e8f0}
    .viewer-overlay{position:fixed;inset:0;background:rgba(0,0,0,.7);z-index:1000;display:flex;align-items:center;justify-content:center}
    .viewer-box{background:#070d1a;border:1px solid #1a2744;border-radius:8px;width:min(90vw,900px);max-height:85vh;display:flex;flex-direction:column}
    .viewer-hdr{display:flex;align-items:center;padding:.75rem 1rem;border-bottom:1px solid #1a2744;gap:.5rem}
    .viewer-title{flex:1;font-size:.9rem;color:#e2e8f0;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .viewer-body{flex:1;overflow:auto;padding:1rem}
    .viewer-pre{margin:0;font-size:.78rem;color:#94a3b8;white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,monospace}
  </style>
</head>
<body class="locked">
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
      <button class="tab" onclick="tab('plugins')" id="t-plugins">Plugins &amp; Skills</button>
      <button class="tab" onclick="tab('dlq')" id="t-dlq">Dead Letter Queue</button>
      <button class="tab" onclick="tab('users')" id="t-users" style="display:none">Users</button>
    </div>

    <!-- Board -->
    <div class="pane on" id="pane-board" style="overflow:hidden">
      <div class="board" id="board" style="flex:1;overflow:auto;min-height:0">
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
      <div class="ctx-panel" id="board-ctx" style="display:none">
        <div class="ctx-resize-handle" id="bctx-resize"></div>
        <div class="ctx-hdr">
          <span class="ctx-title" id="board-ctx-title">Select an item</span>
          <div class="ctx-tabs">
            <button class="ctx-tab on" id="bctx-t-detail" onclick="bctxTab('detail')">Details</button>
            <button class="ctx-tab" id="bctx-t-output" onclick="bctxTab('output')">Output</button>
            <button class="ctx-tab" id="bctx-t-ask" onclick="bctxTab('ask')">Ask</button>
            <button class="ctx-tab" id="bctx-t-files" onclick="bctxTab('files')">Files</button>
          </div>
          <button class="ctx-close" onclick="closeBoardCtx()">&#x2715;</button>
        </div>
        <div class="ctx-body" id="board-ctx-body"></div>
        <div class="ctx-ask-row" id="board-ctx-ask" style="display:none">
          <input id="board-ctx-ask-input" placeholder="Ask the assigned bot&#x2026;" onkeydown="if(event.key==='Enter')boardAsk()"/>
          <button class="btn btn-primary btn-sm" onclick="boardAsk()">Ask</button>
        </div>
      </div>
    </div>

    <!-- Tasks -->
    <div class="pane" id="pane-tasks" style="overflow:hidden">
      <div class="sec-hdr">
        <div style="display:flex;gap:.4rem;align-items:center">
          <button class="btn btn-sm task-filter-btn active" id="tf-all"       onclick="setTaskFilter('all')">All</button>
          <button class="btn btn-sm task-filter-btn"        id="tf-immediate" onclick="setTaskFilter('immediate')">Immediate</button>
          <button class="btn btn-sm task-filter-btn"        id="tf-scheduled" onclick="setTaskFilter('scheduled')">Scheduled</button>
        </div>
        <div class="sec-acts">
          <button id="task-run-btn" class="btn btn-primary btn-sm" disabled onclick="runSelectedTasks()" style="opacity:.35;cursor:not-allowed">Run Selected</button>
          <button id="task-del-btn" class="btn btn-danger btn-sm" disabled onclick="deleteSelectedTasks()" style="opacity:.35;cursor:not-allowed">Delete Selected</button>
          <button class="btn btn-secondary btn-sm" onclick="loadTasks()">Refresh</button>
        </div>
      </div>
      <div id="tasks-list" style="flex:1;overflow:auto"><div class="empty-state">Loading&#x2026;</div></div>
      <div class="ctx-panel" id="task-ctx" style="display:none">
        <div class="ctx-resize-handle" id="tctx-resize"></div>
        <div class="ctx-hdr">
          <span class="ctx-title" id="task-ctx-title">Select a task</span>
          <div class="ctx-tabs">
            <button class="ctx-tab on" id="tctx-t-detail" onclick="tctxTab('detail')">Details</button>
            <button class="ctx-tab" id="tctx-t-output" onclick="tctxTab('output')">Output</button>
          </div>
          <button class="ctx-close" onclick="closeTaskCtx()">&#x2715;</button>
        </div>
        <div class="ctx-body" id="task-ctx-body"></div>
      </div>
    </div>

    <!-- Chat -->
    <div class="pane" id="pane-chat">
      <div style="display:flex;flex:1;overflow:hidden;height:100%">
        <div class="thread-sidebar">
          <div class="thread-sidebar-hdr">
            <span>Threads</span>
            <button class="btn btn-ghost btn-sm" onclick="newThread()">+ New</button>
          </div>
          <div id="thread-list" class="thread-list"></div>
        </div>
        <div class="chat-wrap" style="flex:1;overflow:hidden">
          <div class="chat-hist" id="chat-hist"></div>
          <div class="convo-bar" id="convo-bar"></div>
          <div class="chat-input-row">
            <textarea id="chat-input" placeholder="Message… (Enter to send, Shift+Enter for newline)"></textarea>
            <button class="btn btn-primary" onclick="sendChat()">Send</button>
          </div>
        </div>
      </div>
      <select id="chat-bot-sel" style="display:none"></select>
    </div>

    <!-- Plugins & Skills -->
    <div class="pane" id="pane-plugins">

      <!-- Registry Browser -->
      <div class="sec-hdr">
        <div class="sec-title">Registry Browser</div>
        <div class="sec-acts">
          <select id="registry-select" onchange="loadRegistryIndex()" style="margin-right:8px;padding:4px 8px;border-radius:4px;border:1px solid #ccc"></select>
          <input type="text" id="registry-search" placeholder="Search plugins…" oninput="filterRegistryCards()" style="margin-right:8px;padding:4px 8px;border-radius:4px;border:1px solid #ccc" />
          <button class="btn btn-secondary btn-sm" onclick="showAddRegistryModal()">Add Registry</button>
          <button class="btn btn-secondary btn-sm" onclick="loadRegistries()">Refresh</button>
        </div>
      </div>
      <div id="registry-cards" style="display:flex;flex-wrap:wrap;gap:12px;padding:12px;"><div class="empty-state">Select a registry above</div></div>

      <!-- Add Registry Modal -->
      <div id="add-registry-modal" style="display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.5);z-index:1000;align-items:center;justify-content:center">
        <div style="background:#fff;padding:24px;border-radius:8px;min-width:400px">
          <h3 style="margin-top:0">Add Registry</h3>
          <div style="margin-bottom:12px"><label>Name<br/><input id="reg-name" type="text" style="width:100%;box-sizing:border-box;padding:6px" /></label></div>
          <div style="margin-bottom:12px"><label>URL (https://)<br/><input id="reg-url" type="text" style="width:100%;box-sizing:border-box;padding:6px" placeholder="https://..." /></label></div>
          <div style="margin-bottom:16px"><label><input id="reg-trusted" type="checkbox" /> Trusted registry</label></div>
          <div style="display:flex;gap:8px">
            <button class="btn btn-primary btn-sm" onclick="addRegistry()">Add</button>
            <button class="btn btn-secondary btn-sm" onclick="ge('add-registry-modal').style.display='none'">Cancel</button>
          </div>
        </div>
      </div>

      <hr style="margin:12px 0"/>

      <!-- Installed Plugins -->
      <div class="sec-hdr">
        <div class="sec-title">Installed Plugins</div>
        <div class="sec-acts"><button class="btn btn-secondary btn-sm" onclick="loadPlugins()">Refresh</button></div>
      </div>
      <div id="plugins-body"><div class="empty-state">Loading…</div></div>

      <!-- Plugin Detail Side Panel -->
      <div id="plugin-detail-panel" style="display:none;position:fixed;top:0;right:0;width:400px;height:100%;background:#fff;box-shadow:-2px 0 8px rgba(0,0,0,0.15);overflow:auto;padding:20px;z-index:500">
        <button onclick="ge('plugin-detail-panel').style.display='none'" style="float:right;background:none;border:none;font-size:18px;cursor:pointer">✕</button>
        <h3 id="plugin-detail-name" style="margin-top:0"></h3>
        <div id="plugin-detail-content"></div>
      </div>

      <hr style="margin:12px 0"/>

      <!-- Uploaded Skills (Legacy) -->
      <div class="sec-hdr">
        <div class="sec-title">Manually Uploaded Skills (Legacy)</div>
        <div class="sec-acts">
          <button class="btn btn-secondary btn-sm" onclick="ge('skill-upload-inp').click()">Upload Skill</button>
          <button class="btn btn-secondary btn-sm" onclick="loadSkills()">Refresh</button>
        </div>
      </div>
      <input type="file" id="skill-upload-inp" accept=".md,.zip" style="display:none" onchange="uploadSkill(this)"/>
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
  <div class="fg"><label class="fl">Username</label><input class="fi" id="login-u" type="text" autocomplete="username" onkeydown="if(event.key==='Enter')doLogin();if(event.key==='Escape')cls('login-dlg')"/></div>
  <div class="fg"><label class="fl">Password</label><input class="fi" id="login-p" type="password" autocomplete="current-password" onkeydown="if(event.key==='Enter')doLogin();if(event.key==='Escape')cls('login-dlg')"/></div>
  <div class="errmsg" id="login-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('login-dlg')">Cancel</button><button class="btn btn-primary" onclick="doLogin()">Sign in</button></div>
</dialog>

<!-- New Item -->
<dialog id="ni-dlg">
  <h2>New Work Item</h2>
  <div class="fg"><label class="fl">Title</label><input class="fi" id="ni-title" type="text" placeholder="What needs to be done?"/></div>
  <div class="fg"><label class="fl">Description</label><textarea class="fi" id="ni-desc" placeholder="Optional details…"></textarea></div>
  <div class="fg"><label class="fl">Assign to bot</label><select class="fi" id="ni-bot"><option value="">Unassigned</option></select></div>
  <div class="fg"><label class="fl">Working directory</label><select class="fi" id="ni-workdir-sel" onchange="ge('ni-workdir-txt').style.display=this.value?'block':'none'"><option value="">None</option></select><input class="fi" id="ni-workdir-txt" type="text" placeholder="sub/path/within/root (optional)" style="margin-top:.35rem;display:none"/></div>
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
  <div class="fg"><label class="fl">Title</label><input class="fi" id="at-title" type="text" placeholder="Brief label (optional)"/></div>
  <div class="fg"><label class="fl">Instruction</label><textarea class="fi" id="at-instr" placeholder="Describe the task…" required></textarea></div>
  <div class="fg">
    <label class="fl">Timing</label>
    <label style="font-size:.8rem;display:inline-flex;align-items:center;gap:.4rem;margin-right:.75rem"><input type="radio" name="at-timing" id="at-now" checked onchange="ge('at-sched-wrap').style.display='none'"> Now</label>
    <label style="font-size:.8rem;display:inline-flex;align-items:center;gap:.4rem"><input type="radio" name="at-timing" id="at-later" onchange="ge('at-sched-wrap').style.display='block'"> Schedule</label>
  </div>
  <div class="fg" id="at-sched-wrap" style="display:none"><label class="fl">Schedule At</label><input class="fi" id="at-sched" type="datetime-local"/></div>
  <div class="fg"><label class="fl">Working directory (optional)</label><select class="fi" id="at-workdir-sel" onchange="ge('at-workdir-txt').style.display=this.value?'block':'none'"><option value="">None</option></select><input class="fi" id="at-workdir-txt" type="text" placeholder="sub/path/within/root (optional)" style="margin-top:.35rem;display:none"/></div>
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

<div id="viewer-overlay" class="viewer-overlay" style="display:none" onclick="if(event.target===this)closeViewer()">
  <div class="viewer-box">
    <div class="viewer-hdr">
      <span class="viewer-title" id="viewer-title"></span>
      <a id="viewer-dl" class="btn btn-secondary btn-sm" download>Download</a>
      <button class="ctx-close" onclick="closeViewer()">&#x2715;</button>
    </div>
    <div class="viewer-body"><pre class="viewer-pre" id="viewer-pre"></pre></div>
  </div>
</div>

<script>
  // ── State ───────────────────────────────────────────────────────────────────
  var token=null, me=null, myRole=null;
  var allItems=[], allBots=[], allWorkDirs=[];
  var selectedBots=[], pendingTasks={}, fastPollTimer=null;
  var dragId=null, setPwTarget=null;
  var activeTab='board', countdown=30, tickTimer=null;
  var activeThreadID=null, allThreads=[];
  var boardCtxItem=null, boardCtxThread=null, boardCtxTab='detail', outputPollTimer=null, askPollTimer=null;
  var taskCtxTask=null, taskCtxActiveTab='detail';
  var allTasksList=[], currentTaskFilter='all';
  var dragging=false;

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
    document.body.classList.toggle('locked',!on);
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
    closeBoardCtx();
    closeTaskCtx();
    if(name==='tasks')loadTasks();
    if(name==='chat'){loadThreads();loadChat()}
    if(name==='plugins'){loadPlugins();loadRegistries();}
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
      (it.status==='in-progress'&&it.active_task_id?'<div class="card-working">&#x2699; working&hellip;</div>':'')+
      (it.description?'<div class="card-desc">'+esc(it.description)+'</div>':'')+
      '<div class="card-foot">'+
        (it.assigned_to?'<span class="card-who">'+esc(it.assigned_to)+'</span>':'')+
        '<span class="card-age">'+ago(it.updated_at)+'</span>'+
      '</div>';
    d.addEventListener('dragstart',function(ev){dragging=true;dragId=it.id;d.classList.add('dragging');ev.dataTransfer.effectAllowed='move'});
    d.addEventListener('dragend',function(){dragging=false;d.classList.remove('dragging')});
    d.onclick=(function(item,el){return function(){if(!dragging&&token)openBoardCtx(item,el)}})(it,d);
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
      .then(function(items){
        allItems=items||[];
        if(boardCtxItem){
          var fresh=allItems.find(function(x){return x.id===boardCtxItem.id});
          if(fresh){var prev=boardCtxItem.status;boardCtxItem=fresh;if(fresh.status!==prev)loadBoardCtx();}
        }
        renderBoard();renderRoster();
      })
      .catch(function(){});
  }

  // ── Roster ───────────────────────────────────────────────────────────────────
  function renderRoster(){
    var el=ge('roster');
    if(!allBots.length){el.innerHTML='<div class="nil" style="padding:1rem;font-size:.7rem">No bots registered</div>';return}
    var active={};
    allItems.forEach(function(it){if(it.active_task_id&&it.assigned_to)active[it.assigned_to]=(active[it.assigned_to]||0)+1});
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
    // Init selectedBots to orchestrator on first load.
    if(!selectedBots.length){
      var orch=allBots.find(function(b){return b.bot_type==='orchestrator'});
      if(orch)selectedBots=[orch.name];
      else if(allBots.length)selectedBots=[allBots[0].name];
    }
    renderConvoBar();
  }

  function renderConvoBar(){
    var bar=ge('convo-bar');
    if(!bar)return;
    bar.innerHTML='';
    selectedBots.forEach(function(name){
      var chip=document.createElement('div');chip.className='convo-chip';
      chip.innerHTML='<span>'+esc(name)+'</span>'+(selectedBots.length>1?'<button onclick="removeBotFromChat(\''+esc(name)+'\')">&#x2715;</button>':'');
      bar.appendChild(chip);
    });
    // "Add bot" dropdown — only show bots not already in conversation
    var remaining=allBots.filter(function(b){return selectedBots.indexOf(b.name)<0});
    if(remaining.length){
      var sel=document.createElement('select');sel.className='convo-add';sel.id='convo-add-sel';
      var ph=document.createElement('option');ph.value='';ph.textContent='+ Add bot';sel.appendChild(ph);
      remaining.forEach(function(b){
        var o=document.createElement('option');o.value=b.name;o.textContent=b.name;sel.appendChild(o);
      });
      sel.onchange=function(){if(this.value){addBotToChat(this.value);this.value=''}};
      bar.appendChild(sel);
    }
  }

  function addBotToChat(name){
    if(selectedBots.indexOf(name)<0){selectedBots.push(name);renderConvoBar()}
  }

  function removeBotFromChat(name){
    selectedBots=selectedBots.filter(function(n){return n!==name});
    if(!selectedBots.length&&allBots.length){
      var orch=allBots.find(function(b){return b.bot_type==='orchestrator'});
      selectedBots=orch?[orch.name]:[allBots[0].name];
    }
    renderConvoBar();
  }

  function startFastPoll(){
    if(fastPollTimer)return;
    fastPollTimer=setInterval(function(){if(activeTab==='chat')loadChat()},2000);
  }

  function stopFastPoll(){
    clearInterval(fastPollTimer);fastPollTimer=null;
  }

  function showThinking(bot,taskId){
    var hist=ge('chat-hist');if(!hist)return;
    var wrap=document.createElement('div');
    wrap.id='thinking-'+taskId;
    wrap.style.display='flex';wrap.style.flexDirection='column';wrap.style.alignItems='flex-start';
    var label=document.createElement('div');label.className='chat-meta';label.style.marginBottom='.2rem';
    label.textContent=esc(bot)+' • thinking…';
    var dots=document.createElement('div');dots.className='chat-thinking';
    dots.innerHTML='<span></span><span></span><span></span>';
    wrap.appendChild(label);wrap.appendChild(dots);
    hist.appendChild(wrap);
    hist.scrollTop=hist.scrollHeight;
  }

  function hideThinking(taskId){
    var el=ge('thinking-'+taskId);if(el)el.remove();
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
    var wsel=ge('ni-workdir-sel');
    wsel.innerHTML='<option value="">None</option>';
    (allWorkDirs||[]).forEach(function(d){var o=document.createElement('option');o.value=d;o.textContent=d;wsel.appendChild(o);});
    ge('ni-workdir-txt').style.display='none';
    ge('ni-workdir-txt').value='';
    ge('ni-err').style.display='none';
    dlg('ni-dlg');
  }

  function doCreateItem(){
    var title=ge('ni-title').value.trim(),desc=ge('ni-desc').value.trim(),bot=ge('ni-bot').value,e=ge('ni-err');
    e.style.display='none';
    if(!title){e.textContent='Title is required';e.style.display='block';return}
    var root=ge('ni-workdir-sel').value,sub=ge('ni-workdir-txt').value.trim();
    var workdir=root?(sub?root+'/'+sub:root):'';
    var body={title:title,description:desc,assigned_to:bot};
    if(workdir)body.work_dir=workdir;
    api('POST','/api/v1/board',body)
      .then(function(){cls('ni-dlg');ge('ni-title').value='';ge('ni-desc').value='';ge('ni-workdir-sel').value='';ge('ni-workdir-txt').value='';ge('ni-workdir-txt').style.display='none';loadBoard()})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Plugins & Registry ──────────────────────────────────────────────────────
  var registryData=[];

  function loadRegistries(){
    api('GET','/api/v1/registries',null).then(function(regs){
      registryData=regs||[];
      var sel=ge('registry-select');
      if(!sel)return;
      var prev=sel.value;
      sel.innerHTML='<option value="">-- select registry --</option>';
      regs.forEach(function(r){
        var opt=document.createElement('option');
        opt.value=r.name;opt.textContent=r.name+(r.trusted?' (trusted)':'');
        sel.appendChild(opt);
      });
      if(prev)sel.value=prev;
    }).catch(function(){});
  }

  function loadRegistryIndex(){
    var name=ge('registry-select').value;
    if(!name){ge('registry-cards').innerHTML='<div class="empty-state">Select a registry above</div>';return;}
    ge('registry-cards').innerHTML='<div class="empty-state">Loading…</div>';
    api('GET','/api/v1/registries/'+encodeURIComponent(name)+'/index',null).then(function(idx){
      renderRegistryCards(idx.plugins||[]);
    }).catch(function(e){ge('registry-cards').innerHTML='<div class="empty-state">Error: '+e.message+'</div>';});
  }

  function renderRegistryCards(plugins){
    var q=(ge('registry-search').value||'').toLowerCase();
    var filtered=plugins.filter(function(p){return !q||p.name.toLowerCase().includes(q)||p.description.toLowerCase().includes(q);});
    if(!filtered.length){ge('registry-cards').innerHTML='<div class="empty-state">No plugins found</div>';return;}
    ge('registry-cards').innerHTML=filtered.map(function(p){
      return '<div style="background:#f9f9f9;border:1px solid #ddd;border-radius:6px;padding:14px;min-width:220px;max-width:260px">'+
        '<div style="font-weight:600;margin-bottom:4px">'+esc(p.name)+'</div>'+
        '<div style="font-size:12px;color:#666;margin-bottom:8px">'+esc(p.latest_version)+'</div>'+
        '<div style="font-size:13px;margin-bottom:10px">'+esc(p.description)+'</div>'+
        (p.tags&&p.tags.length?'<div style="font-size:11px;color:#888;margin-bottom:8px">'+p.tags.map(function(t){return '<span style="background:#e8e8e8;padding:2px 6px;border-radius:10px;margin-right:4px">'+esc(t)+'</span>'}).join('')+'</div>':'')+
        '<button class="btn btn-primary btn-sm" onclick="installPlugin(\''+esc(ge('registry-select').value)+'\',\''+esc(p.name)+'\',\''+esc(p.latest_version)+'\')">Install</button>'+
        '</div>';
    }).join('');
  }

  function filterRegistryCards(){
    var name=ge('registry-select').value;
    if(name)loadRegistryIndex();
  }

  function showAddRegistryModal(){
    ge('add-registry-modal').style.display='flex';
  }

  function addRegistry(){
    var name=ge('reg-name').value.trim();
    var url=ge('reg-url').value.trim();
    var trusted=ge('reg-trusted').checked;
    if(!name||!url){alert('Name and URL are required');return;}
    api('POST','/api/v1/registries',{name:name,url:url,trusted:trusted}).then(function(){
      ge('add-registry-modal').style.display='none';
      ge('reg-name').value='';ge('reg-url').value='';ge('reg-trusted').checked=false;
      loadRegistries();
    }).catch(function(e){alert('Error: '+e.message);});
  }

  function installPlugin(registry,name,version){
    if(!token){alert('Login required');return;}
    api('POST','/api/v1/plugins',{registry:registry,name:name,version:version}).then(function(p){
      alert('Plugin "'+name+'" installation initiated (status: '+p.status+')');
      loadPlugins();
    }).catch(function(e){alert('Install failed: '+e.message);});
  }

  function loadPlugins(){
    api('GET','/api/v1/plugins',null).then(function(plugins){
      renderPluginsTable(plugins||[]);
    }).catch(function(){ge('plugins-body').innerHTML='<div class="empty-state">Not available</div>';});
  }

  function renderPluginsTable(plugins){
    var el=ge('plugins-body');
    if(!plugins.length){el.innerHTML='<div class="empty-state">No plugins installed</div>';return;}
    var rows=plugins.map(function(p){
      var acts='';
      if(p.status==='staged'){acts+='<button class="btn btn-primary btn-sm" onclick="pluginAction(\'approve\',\''+p.id+'\')">Approve</button> <button class="btn btn-danger btn-sm" onclick="pluginAction(\'reject\',\''+p.id+'\')">Reject</button> ';}
      if(p.status==='active'){acts+='<button class="btn btn-secondary btn-sm" onclick="pluginAction(\'disable\',\''+p.id+'\')">Disable</button> ';}
      if(p.status==='disabled'){acts+='<button class="btn btn-primary btn-sm" onclick="pluginAction(\'enable\',\''+p.id+'\')">Enable</button> ';}
      if(p.status==='active'||p.status==='disabled'){acts+='<button class="btn btn-secondary btn-sm" onclick="pluginAction(\'reload\',\''+p.id+'\')">Reload</button> ';}
      acts+='<button class="btn btn-danger btn-sm" onclick="pluginAction(\'remove\',\''+p.id+'\')">Remove</button>';
      return '<tr>'+
        '<td><a href="#" onclick="showPluginDetail(\''+p.id+'\');return false" style="text-decoration:none;font-weight:500">'+esc(p.name)+'</a></td>'+
        '<td>'+esc(p.version)+'</td>'+
        '<td>'+esc(p.registry||'—')+'</td>'+
        '<td><span class="badge" style="background:'+statusColor(p.status)+'">'+esc(p.status)+'</span></td>'+
        '<td>'+esc(p.installed_at?p.installed_at.substring(0,10):'—')+'</td>'+
        '<td>'+acts+'</td>'+
        '</tr>';
    }).join('');
    el.innerHTML='<table class="tbl"><thead><tr><th>NAME</th><th>VERSION</th><th>REGISTRY</th><th>STATUS</th><th>INSTALLED</th><th>ACTIONS</th></tr></thead><tbody>'+rows+'</tbody></table>';
  }

  function statusColor(s){
    var m={'active':'#22863a','staged':'#e36209','disabled':'#666','rejected':'#cb2431','update_available':'#0366d6','checksum_fail':'#cb2431'};
    return m[s]||'#888';
  }

  function pluginAction(action,id){
    var method=action==='remove'?'DELETE':'POST';
    var path='/api/v1/plugins/'+id+(action!=='remove'?'/'+action:'');
    api(method,path,null).then(function(){loadPlugins();}).catch(function(e){alert('Error: '+e.message);});
  }

  function showPluginDetail(id){
    api('GET','/api/v1/plugins/'+id,null).then(function(p){
      ge('plugin-detail-name').textContent=p.name+' '+p.version;
      var m=p.manifest||{};
      var tools=(m.provides&&m.provides.tools)||[];
      var perms=m.permissions||{};
      ge('plugin-detail-content').innerHTML=
        '<p><b>Author:</b> '+esc(m.author||'—')+'</p>'+
        '<p><b>Description:</b> '+esc(m.description||'—')+'</p>'+
        '<p><b>Status:</b> <span style="color:'+statusColor(p.status)+'">'+esc(p.status)+'</span></p>'+
        '<p><b>Registry:</b> '+esc(p.registry||'—')+'</p>'+
        '<p><b>Entrypoint:</b> '+esc(m.entrypoint||'—')+'</p>'+
        (m.homepage?'<p><b>Homepage:</b> <a href="'+esc(m.homepage)+'" target="_blank">'+esc(m.homepage)+'</a></p>':'')+
        '<hr/><b>Tools provided:</b>'+
        (tools.length?'<ul>'+tools.map(function(t){return '<li><b>'+esc(t.name)+'</b> — '+esc(t.description||'')+'</li>';}).join('')+'</ul>':'<p>None</p>')+
        '<hr/><b>Permissions:</b>'+
        '<ul>'+
        (perms.filesystem?'<li>Filesystem access</li>':'')+
        ((perms.network||[]).map(function(n){return '<li>Network: '+esc(n)+'</li>';}).join(''))+
        ((perms.env_vars||[]).map(function(e){return '<li>Env var: '+esc(e)+'</li>';}).join(''))+
        '</ul>'+
        (m.checksums?'<hr/><b>Checksums:</b><pre style="font-size:11px;overflow:auto">'+esc(JSON.stringify(m.checksums,null,2))+'</pre>':'');
      ge('plugin-detail-panel').style.display='block';
    }).catch(function(e){alert('Error: '+e.message);});
  }

  // ── Skills ───────────────────────────────────────────────────────────────────
  function uploadSkill(input){
    var file=input.files[0];
    if(!file)return;
    input.value='';
    var name=prompt('Skill name:',file.name.replace(/\.[^.]+$/,''))||'';
    if(!name)return;
    var botType=prompt('Bot type (leave blank for all):','')||'';
    var fd=new FormData();
    fd.append('file',file);
    fd.append('name',name);
    fd.append('bot_type',botType);
    var opts={method:'POST',headers:{},body:fd};
    if(token)opts.headers['Authorization']='Bearer '+token;
    fetch('/api/v1/skills',opts)
      .then(function(r){return r.json().then(function(d){if(!r.ok)throw new Error(d.error||r.statusText);return d})})
      .then(function(){loadSkills()})
      .catch(function(e){alert('Upload failed: '+e.message)});
  }

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
  function setTaskFilter(f){
    currentTaskFilter=f;
    ['all','immediate','scheduled'].forEach(function(n){ge('tf-'+n).classList.toggle('active',n===f)});
    renderTaskList();
  }

  function getFilteredTasks(){
    if(currentTaskFilter==='immediate')return allTasksList.filter(function(t){return!t.scheduled_at});
    if(currentTaskFilter==='scheduled')return allTasksList.filter(function(t){return!!t.scheduled_at});
    return allTasksList;
  }

  function updateTaskDeleteBtn(){
    var checked=ge('tasks-list').querySelectorAll('input[data-cid]:checked').length;
    function setBtn(btn,on){if(!btn)return;btn.disabled=!on;btn.style.opacity=on?'1':'0.35';btn.style.cursor=on?'pointer':'not-allowed'}
    setBtn(ge('task-del-btn'),checked>0);
    setBtn(ge('task-run-btn'),checked>0);
  }

  function renderTaskList(){
    var el=ge('tasks-list');
    var tasks=getFilteredTasks();
    if(!tasks||!tasks.length){el.innerHTML='<div class="empty-state">None</div>';return}
    var rows=tasks.map(function(t){
      var sc=t.status==='pending'?'pill-warn':t.status==='running'?'pill-info':t.status==='succeeded'?'pill-ok':'pill-off';
      var label=esc(t.title||(t.instruction||'').substring(0,60))+((!t.title&&t.instruction&&t.instruction.length>60)?'&#x2026;':'');
      return'<tr data-tid="'+esc(t.id)+'"><td style="width:1.5rem;text-align:center"><input type="checkbox" data-cid="'+esc(t.id)+'" onclick="event.stopPropagation();updateTaskDeleteBtn()"/></td><td>'+esc(t.bot_name)+'</td><td>'+label+'</td><td><span class="pill '+sc+'">'+esc(t.status)+'</span></td><td>'+(t.scheduled_at?ago(t.scheduled_at):'&#x2014;')+'</td><td>'+ago(t.created_at)+'</td></tr>';
    }).join('');
    el.innerHTML='<table><thead><tr><th style="width:1.5rem"><input type="checkbox" id="task-chk-all" onclick="toggleAllTaskChecks(this)"/></th><th>Bot</th><th>Title / Instruction</th><th>Status</th><th>Sched</th><th>Created</th></tr></thead><tbody>'+rows+'</tbody></table>';
    el.querySelectorAll('tr[data-tid]').forEach(function(tr){
      tr.style.cursor='pointer';
      tr.onclick=function(ev){
        if(ev.target.type==='checkbox')return;
        var tid=tr.getAttribute('data-tid');
        var task=allTasksList.find(function(t){return t.id===tid});
        if(task)openTaskCtx(task,tr);
      };
    });
    updateTaskDeleteBtn();
  }

  function toggleAllTaskChecks(master){
    ge('tasks-list').querySelectorAll('input[data-cid]').forEach(function(cb){cb.checked=master.checked});
    updateTaskDeleteBtn();
  }

  function loadTasks(){
    if(!token){ge('tasks-list').innerHTML='<div class="empty-state">Sign in to view tasks</div>';return}
    api('GET','/api/v1/tasks')
      .then(function(tasks){allTasksList=tasks||[];renderTaskList()})
      .catch(function(){ge('tasks-list').innerHTML='<div class="empty-state">Failed to load tasks</div>'});
  }

  function deleteSelectedTasks(){
    var ids=[];
    ge('tasks-list').querySelectorAll('input[data-cid]:checked').forEach(function(cb){ids.push(cb.getAttribute('data-cid'))});
    if(!ids.length)return;
    if(!confirm('Delete '+ids.length+' task'+(ids.length>1?'s':'')+' and their log directories?'))return;
    Promise.all(ids.map(function(id){return api('DELETE','/api/v1/tasks/'+id,null)}))
      .then(function(){if(taskCtxTask&&ids.indexOf(taskCtxTask.id)>=0)closeTaskCtx();loadTasks()})
      .catch(function(e){alert('Delete failed: '+e.message);loadTasks()});
  }

  function runSelectedTasks(){
    var ids=[];
    ge('tasks-list').querySelectorAll('input[data-cid]:checked').forEach(function(cb){
      var id=cb.getAttribute('data-cid');
      var task=allTasksList.find(function(t){return t.id===id});
      if(task&&task.status!=='running')ids.push(id);
    });
    if(!ids.length){alert('No eligible tasks selected (already-running tasks are skipped).');return}
    Promise.all(ids.map(function(id){return api('POST','/api/v1/tasks/'+id+'/run',{})}))
      .then(function(){loadTasks()})
      .catch(function(e){alert('Run failed: '+e.message);loadTasks()});
  }

  function openAssignTask(botName){
    ge('at-bot').textContent=botName;
    ge('at-instr').value='';
    ge('at-now').checked=true;
    ge('at-sched-wrap').style.display='none';
    ge('at-sched').value='';
    ge('at-err').style.display='none';
    var sel=ge('at-workdir-sel');
    sel.innerHTML='<option value="">None</option>';
    (allWorkDirs||[]).forEach(function(d){
      var o=document.createElement('option');
      o.value=d;o.textContent=d;
      sel.appendChild(o);
    });
    ge('at-workdir-txt').style.display='none';
    ge('at-workdir-txt').value='';
    dlg('at-dlg');
  }

  function doDispatchTask(){
    var botName=ge('at-bot').textContent;
    var title=ge('at-title').value.trim();
    var instruction=ge('at-instr').value.trim();
    var isNow=ge('at-now').checked;
    var schedVal=ge('at-sched').value;
    var e=ge('at-err');
    e.style.display='none';
    if(!instruction){e.textContent='Instruction is required';e.style.display='block';return}
    var root=ge('at-workdir-sel').value,sub=ge('at-workdir-txt').value.trim();
    var workDir=root?(sub?root+'/'+sub:root):'';
    var body={instruction:instruction};
    if(title){body.title=title}
    if(!isNow&&schedVal){body.scheduled_at=new Date(schedVal).toISOString()}
    if(workDir){body.work_dir=workDir}
    api('POST','/api/v1/bots/'+botName+'/tasks',body)
      .then(function(){cls('at-dlg');ge('at-title').value='';ge('at-instr').value='';setTaskFilter('immediate');tab('tasks');loadTasks()})
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Chat ──────────────────────────────────────────────────────────────────────
  function loadThreads(){
    if(!token)return;
    api('GET','/api/v1/threads',null)
      .then(function(threads){
          allThreads=threads||[];
          renderThreadList();
          if(!activeThreadID&&allThreads.length){
              selectThread(allThreads[0].id);
          }
      })
      .catch(function(){});
  }

  function renderThreadList(){
    var el=ge('thread-list');
    if(!el)return;
    el.innerHTML='';
    if(!allThreads.length){
        el.innerHTML='<div class="nil" style="padding:.75rem;font-size:.75rem">No threads yet</div>';
        return;
    }
    allThreads.forEach(function(t){
        var d=document.createElement('div');
        d.className='thread-item'+(t.id===activeThreadID?' active':'');
        d.innerHTML=
            '<div class="thread-title">'+esc(t.title)+'</div>'+
            '<div class="thread-meta">'+(t.participants||[]).map(function(p){return esc(p)}).join(', ')+'</div>'+
            '<button class="thread-del" onclick="deleteThread(event,\''+esc(t.id)+'\')">&#x2715;</button>';
        d.onclick=function(){selectThread(t.id)};
        el.appendChild(d);
    });
  }

  function selectThread(id){
    activeThreadID=id;
    renderThreadList();
    var t=allThreads.find(function(x){return x.id===id});
    if(t&&t.participants&&t.participants.length){
        selectedBots=t.participants.slice();
        renderConvoBar();
    }
    loadChat();
  }

  function newThread(){
    if(!token){alert('Please sign in first');return}
    var title=prompt('Thread title (leave blank to auto-name):')||'';
    if(title===null)return;
    var participants=selectedBots.length?selectedBots:(allBots.length?[allBots[0].name]:[]);
    if(!title)title='Chat with '+participants.join(', ');
    api('POST','/api/v1/threads',{title:title,participants:participants})
      .then(function(t){
          loadThreads();
          selectThread(t.id);
      })
      .catch(function(e){alert('Failed: '+e.message)});
  }

  function deleteThread(ev,id){
    ev.stopPropagation();
    if(!confirm('Delete this thread and all its messages?'))return;
    api('DELETE','/api/v1/threads/'+id,null)
      .then(function(){
          if(activeThreadID===id){activeThreadID=null;ge('chat-hist').innerHTML=''}
          loadThreads();
      })
      .catch(function(e){alert('Failed: '+e.message)});
  }

  function loadChat(){
    var el=ge('chat-hist');
    if(!token){el.innerHTML='<div class="nil">Sign in to chat</div>';return}
    // Ensure selector is current (loadTeam populates it; this is a safety call
    // for the case where the user switches to Chat before loadTeam resolves).
    if(!ge('chat-bot-sel').options.length)populateBotSelectors();
    if(!activeThreadID){
        el.innerHTML='<div class="nil">Select or create a thread</div>';
        return;
    }
    api('GET','/api/v1/threads/'+activeThreadID+'/messages',null)
      .then(function(msgs){
        el.innerHTML='';
        if(!msgs||!msgs.length){el.innerHTML='<div class="nil">No messages yet</div>';return}
        // API returns newest-first; reverse to show oldest-first.
        var ordered=msgs.slice().reverse();
        ordered.forEach(function(m){el.appendChild(renderChatMsg(m))});
        el.scrollTop=el.scrollHeight;
        // Resolve any pending thinking indicators.
        var resolved=[];
        (msgs||[]).forEach(function(m){
          if(m.direction==='inbound'&&m.task_id&&pendingTasks[m.task_id]){
            resolved.push(m.task_id);
          }
        });
        resolved.forEach(function(id){hideThinking(id);delete pendingTasks[id]});
        if(!Object.keys(pendingTasks).length)stopFastPoll();
      })
      .catch(function(){el.innerHTML='<div class="nil">Failed to load messages</div>'});
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
    meta.textContent=esc(msg.bot_name)+' • '+ago(msg.created_at);
    wrap.appendChild(bubble);
    wrap.appendChild(meta);
    return wrap;
  }

  function sendChat(){
    if(!token){alert('Please sign in first');return}
    var content=ge('chat-input').value.trim();
    if(!content)return;
    if(!selectedBots.length){alert('Select a bot first');return}
    if(!activeThreadID){
        var participants=selectedBots.slice();
        var title='Chat with '+participants.join(', ');
        api('POST','/api/v1/threads',{title:title,participants:participants})
          .then(function(t){
              allThreads.unshift(t);
              activeThreadID=t.id;
              renderThreadList();
              doSend(content);
          })
          .catch(function(e){alert('Failed: '+e.message)});
        return;
    }
    doSend(content);
  }

  function doSend(content){
    ge('chat-input').value='';
    var promises=selectedBots.map(function(bot){
      return api('POST','/api/v1/chat/'+bot,{content:content,thread_id:activeThreadID})
        .then(function(msg){
          if(msg&&msg.task_id){
            pendingTasks[msg.task_id]=bot;
            showThinking(bot,msg.task_id);
          }
        })
        .catch(function(e){alert('Send to '+bot+' failed: '+e.message)});
    });
    Promise.all(promises).then(function(){startFastPoll();loadChat()});
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

  // ── Board context panel ───────────────────────────────────────────────────────
  var bctxH=280;
  function openBoardCtx(item,cardEl){
    boardCtxItem=item;
    boardCtxThread=null;
    var panel=ge('board-ctx');
    panel.style.display='flex';
    panel.style.height=bctxH+'px';
    ge('board-ctx-title').textContent=item.title;
    bctxTab(boardCtxTab);
    loadBoardCtx();
    if(cardEl){
      requestAnimationFrame(function(){
        cardEl.scrollIntoView({block:'nearest',behavior:'smooth'});
      });
    }
  }

  // ── Resize handle ─────────────────────────────────────────────────────────────
  (function(){
    var handle=ge('bctx-resize');
    var panel=ge('board-ctx');
    var startY=0,startH=0,active=false;
    handle.addEventListener('mousedown',function(e){
      active=true;startY=e.clientY;startH=bctxH;
      document.body.style.cursor='ns-resize';
      document.body.style.userSelect='none';
      e.preventDefault();
    });
    document.addEventListener('mousemove',function(e){
      if(!active)return;
      var dy=startY-e.clientY;
      bctxH=Math.max(120,Math.min(700,startH+dy));
      panel.style.height=bctxH+'px';
    });
    document.addEventListener('mouseup',function(){
      if(!active)return;
      active=false;
      document.body.style.cursor='';
      document.body.style.userSelect='';
    });
  })();

  function stopOutputPoll(){if(outputPollTimer){clearInterval(outputPollTimer);outputPollTimer=null;}}
  function stopAskPoll(){if(askPollTimer){clearInterval(askPollTimer);askPollTimer=null;}}

  function closeBoardCtx(){
    stopOutputPoll();
    stopAskPoll();
    var panel=ge('board-ctx');
    panel.style.height='0';
    panel.style.display='none';
    boardCtxItem=null;
  }

  function bctxTab(name){
    if(name!=='output')stopOutputPoll();
    if(name!=='ask')stopAskPoll();
    boardCtxTab=name;
    ['detail','output','ask','files'].forEach(function(t){
      var el=ge('bctx-t-'+t);if(el)el.classList.toggle('on',t===name);
    });
    ge('board-ctx-ask').style.display=name==='ask'?'flex':'none';
    if(boardCtxItem)loadBoardCtx();
  }

  function loadBoardCtx(){
    if(!boardCtxItem)return;
    var body=ge('board-ctx-body');
    if(boardCtxTab==='detail'){
      var it=boardCtxItem;
      var attCount=(it.attachments||[]).length;
      var isDone=it.status==='done';
      var isBacklog=it.status==='backlog';
      var canEdit=token&&(isBacklog);

      // Work dir row — editable picker in backlog; read-only elsewhere
      var workdirInput='';
      if(canEdit){
        if(allWorkDirs.length>0){
          var wdRoot='',wdSub='';
          if(it.work_dir){
            for(var wi=0;wi<allWorkDirs.length;wi++){
              if(it.work_dir===allWorkDirs[wi]){wdRoot=allWorkDirs[wi];break;}
              if(it.work_dir.indexOf(allWorkDirs[wi]+'/')===0){wdRoot=allWorkDirs[wi];wdSub=it.work_dir.slice(allWorkDirs[wi].length+1);break;}
            }
          }
          workdirInput='<div style="flex:1;display:flex;flex-direction:column;gap:.3rem">'+
            '<select id="bctx-workdir" style="width:100%;background:#0d1627;border:1px solid #1a2744;border-radius:4px;color:#e2e8f0;font-size:.85rem;padding:.45rem .6rem" onchange="ge(\'bctx-workdir-sub\').style.display=this.value?\'block\':\'none\';onBctxChange()">'+
            '<option value="">— none —</option>';
          allWorkDirs.forEach(function(d){workdirInput+='<option value="'+esc(d)+'"'+(wdRoot===d?' selected':'')+'>'+esc(d)+'</option>'});
          workdirInput+='</select>'+
            '<input id="bctx-workdir-sub" type="text" placeholder="sub/path/within/root (optional)" value="'+esc(wdSub)+'" oninput="onBctxChange()" style="width:100%;display:'+(wdRoot?'block':'none')+';background:#0d1627;border:1px solid #1a2744;border-radius:4px;color:#e2e8f0;font-size:.85rem;padding:.45rem .6rem"/>'+
            '</div>';
        } else {
          workdirInput='<input id="bctx-workdir" value="'+esc(it.work_dir||'')+'" placeholder="none" oninput="onBctxChange()" style="flex:1;background:#0d1627;border:1px solid #1a2744;border-radius:4px;color:#e2e8f0;font-size:.85rem;padding:.45rem .6rem"/>';
        }
      } else {
        workdirInput='<span>'+esc(it.work_dir||'&#x2014;')+'</span>';
      }

      // Bot selector for backlog editing
      var botRow='<div class="ctx-row"><span class="ctx-lbl">Assigned to</span><span class="ctx-val">'+
        (canEdit
          ? '<select id="bctx-bot" onchange="onBctxChange()" style="width:100%;background:#0d1627;border:1px solid #1a2744;border-radius:4px;color:#e2e8f0;font-size:.85rem;padding:.45rem .6rem">'+
            '<option value="">Unassigned</option>'+
            allBots.map(function(b){return'<option value="'+esc(b.name)+'"'+(it.assigned_to===b.name?' selected':'')+'>'+esc(b.name)+'</option>'}).join('')+
            '</select>'
          : (it.assigned_to||'&#x2014;'))+
        '</span></div>';

      // Description — editable in backlog
      var descRow='<div class="ctx-row"><span class="ctx-lbl">Description</span><span class="ctx-val">'+
        (canEdit
          ? '<textarea id="bctx-desc" oninput="onBctxChange()" style="flex:1;width:100%;background:#0d1627;border:1px solid #1a2744;border-radius:4px;color:#e2e8f0;font-size:.85rem;padding:.45rem .6rem;resize:vertical;min-height:10rem">'+esc(it.description||'')+'</textarea>'
          : (it.description?esc(it.description):'&#x2014;'))+
        '</span></div>';

      body.innerHTML=
        '<div class="ctx-row"><span class="ctx-lbl">Status</span><span class="ctx-val">'+esc(it.status)+'</span></div>'+
        botRow+
        descRow+
        '<div class="ctx-row"><span class="ctx-lbl">Work dir</span><span class="ctx-val" style="display:flex;align-items:center;gap:.5rem">'+
          workdirInput+
        '</span></div>'+
        (it.work_dir?'<div style="font-size:.7rem;color:#475569;padding:.1rem 0 .4rem 0">Attachments will be written to this directory.</div>':'')+
        '<div class="ctx-row"><span class="ctx-lbl">Attachments</span><span class="ctx-val"><a href="#" onclick="bctxTab(\'files\');return false" style="color:#60a5fa">'+attCount+' file'+(attCount!==1?'s':'')+'</a></span></div>'+
        '<div class="ctx-row"><span class="ctx-lbl">Created</span><span class="ctx-val">'+ago(it.created_at)+'</span></div>'+
        (it.active_task_id&&it.status==='in-progress'?'<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>':'')+
        (canEdit?'<div style="margin-top:.75rem"><button id="bctx-save-btn" class="btn btn-primary btn-sm" disabled onclick="saveBoardAllEdits()" style="opacity:.4;cursor:not-allowed">Save changes</button></div>':'')+
        (isDone&&token?'<div style="margin-top:.5rem"><button class="btn btn-danger btn-sm" onclick="deleteBoardItem()">Delete item</button></div>':'');
    } else if(boardCtxTab==='output'){
      body.innerHTML='<div style="color:#475569">Loading&#x2026;</div>';
      function renderOutput(resp){
        var html='';
        var output=(resp.task&&resp.task.output)||'';
        if(!output&&resp.item&&resp.item.last_result)output=resp.item.last_result;
        if(output){
          html+='<pre id="output-pre" class="viewer-pre" style="max-height:340px;overflow-y:auto;white-space:pre-wrap;word-break:break-all">'+esc(output)+'</pre>';
        } else if(resp.task&&(resp.task.status==='dispatched'||resp.task.status==='pending')){
          html+='<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>';
        } else {
          html+='<div style="color:#475569">No output yet</div>';
        }
        if(resp.task){
          html+='<div class="ctx-row" style="margin-top:.75rem"><span class="ctx-lbl">Task status</span><span class="ctx-val">'+esc(resp.task.status)+'</span></div>';
          if(resp.task.dispatched_at)html+='<div class="ctx-row"><span class="ctx-lbl">Dispatched</span><span class="ctx-val">'+ago(resp.task.dispatched_at)+'</span></div>';
          if(resp.task.completed_at)html+='<div class="ctx-row"><span class="ctx-lbl">Completed</span><span class="ctx-val">'+ago(resp.task.completed_at)+'</span></div>';
        }
        body.innerHTML=html;
        var pre=ge('output-pre');if(pre)pre.scrollTop=pre.scrollHeight;
        // Stop polling once the task has completed.
        if(resp.task&&resp.task.status==='completed')stopOutputPoll();
        if(resp.item&&!resp.item.active_task_id)stopOutputPoll();
      }
      var outputItemID=boardCtxItem.id;
      api('GET','/api/v1/board/'+outputItemID+'/activity',null)
        .then(function(resp){
          renderOutput(resp);
          // Start polling only while the item is still in-progress.
          if(boardCtxItem&&boardCtxItem.active_task_id&&!outputPollTimer){
            outputPollTimer=setInterval(function(){
              if(boardCtxTab!=='output'||!boardCtxItem||boardCtxItem.id!==outputItemID){stopOutputPoll();return;}
              api('GET','/api/v1/board/'+outputItemID+'/activity',null)
                .then(renderOutput)
                .catch(function(){});
            },2000);
          }
        })
        .catch(function(){body.innerHTML='<div style="color:#e74c3c">Failed to load activity</div>'});
    } else if(boardCtxTab==='ask'){
      body.innerHTML='<div style="color:#475569;font-size:.75rem">Loading conversation&#x2026;</div>';
      function renderAskMessages(msgs){
        if(!msgs||!msgs.length){
          body.innerHTML='<div style="color:#475569;font-size:.75rem">No messages yet. Ask the bot a question below.</div>';
          return;
        }
        var html='<div class="ask-thread">';
        msgs.forEach(function(m){
          var isUser=m.direction==='outbound';
          html+='<div class="ask-msg '+(isUser?'ask-msg-user':'ask-msg-bot')+'">'
            +'<div class="ask-msg-label">'+(isUser?'You':esc(m.bot_name||'Bot'))+'</div>'
            +'<div class="ask-msg-body">'+esc(m.content)+'</div>'
            +'</div>';
        });
        html+='</div>';
        body.innerHTML=html;
      }
      var askItemID=boardCtxItem.id;
      api('GET','/api/v1/board/'+askItemID+'/messages',null)
        .then(renderAskMessages)
        .catch(function(){body.innerHTML='<div style="color:#e74c3c">Failed to load messages</div>';});
      if(!askPollTimer&&boardCtxItem&&boardCtxItem.active_task_id){
        askPollTimer=setInterval(function(){
          if(boardCtxTab!=='ask'||!boardCtxItem||boardCtxItem.id!==askItemID){stopAskPoll();return;}
          api('GET','/api/v1/board/'+askItemID+'/messages',null)
            .then(renderAskMessages)
            .catch(function(){});
        },2000);
      }
    } else if(boardCtxTab==='files'){
      var it=boardCtxItem;
      var atts=(it.attachments||[]);
      var html='<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:.5rem">'
        +'<span style="font-size:.78rem;color:#475569">'+atts.length+' file'+(atts.length!==1?'s':'')+'</span>'
        +'<label class="upload-btn"><input type="file" multiple style="display:none" onchange="uploadFiles(this)">&#x2B; Attach files</label>'
        +'</div>';
      if(atts.length===0){
        html+='<div style="color:#475569;font-size:.78rem">No attachments yet</div>';
      } else {
        html+='<div class="att-list">';
        atts.forEach(function(a){
          html+='<div class="att-row">'
            +'<span class="att-name" title="'+esc(a.name)+'">'+esc(a.name)+'</span>'
            +'<span style="color:#475569;font-size:.72rem">'+fmtBytes(a.size)+'</span>'
            +'<div class="att-acts">'
            +'<button class="btn btn-secondary btn-sm" onclick="viewAttachment(\''+esc(it.id)+'\',\''+esc(a.id)+'\',\''+esc(a.name)+'\')">View</button>'
            +'<button class="btn btn-secondary btn-sm" onclick="deleteAttachment(\''+esc(it.id)+'\',\''+esc(a.id)+'\')">Del</button>'
            +'</div>'
            +'</div>';
        });
        html+='</div>';
      }
      body.innerHTML=html;
    }
  }

  function saveBoardWorkDir(){
    if(!boardCtxItem||!token)return;
    var root=(ge('bctx-workdir')||{}).value||'';
    var sub=((ge('bctx-workdir-sub')||{}).value||'').trim();
    var val=root?(sub?root+'/'+sub:root):'';
    api('PATCH','/api/v1/board/'+boardCtxItem.id,{work_dir:val})
      .then(function(item){boardCtxItem=item;loadBoard();})
      .catch(function(e){alert('Failed to save: '+e.message)});
  }

  function saveBoardBacklogEdits(){
    if(!boardCtxItem||!token)return;
    var update={};
    var desc=(ge('bctx-desc')||{}).value;
    var bot=(ge('bctx-bot')||{}).value;
    if(desc!==undefined)update.description=desc;
    if(bot!==undefined)update.assigned_to=bot;
    api('PATCH','/api/v1/board/'+boardCtxItem.id,update)
      .then(function(item){boardCtxItem=item;loadBoard();loadBoardCtx();})
      .catch(function(e){alert('Failed to save: '+e.message)});
  }

  function onBctxChange(){
    var btn=ge('bctx-save-btn');
    if(!btn)return;
    btn.disabled=false;
    btn.style.opacity='1';
    btn.style.cursor='pointer';
  }

  function saveBoardAllEdits(){
    if(!boardCtxItem||!token)return;
    var update={};
    var desc=(ge('bctx-desc')||{}).value;
    var bot=(ge('bctx-bot')||{}).value;
    if(desc!==undefined)update.description=desc;
    if(bot!==undefined)update.assigned_to=bot;
    var root=(ge('bctx-workdir')||{}).value||'';
    var sub=((ge('bctx-workdir-sub')||{}).value||'').trim();
    update.work_dir=root?(sub?root+'/'+sub:root):'';
    api('PATCH','/api/v1/board/'+boardCtxItem.id,update)
      .then(function(item){boardCtxItem=item;loadBoard();loadBoardCtx();})
      .catch(function(e){alert('Failed to save: '+e.message)});
  }

  function deleteBoardItem(){
    if(!boardCtxItem||!token)return;
    if(!confirm('Delete "'+boardCtxItem.title+'"? This cannot be undone.'))return;
    api('DELETE','/api/v1/board/'+boardCtxItem.id,null)
      .then(function(){closeBoardCtx();loadBoard()})
      .catch(function(e){alert('Delete failed: '+e.message)});
  }

  function boardAsk(){
    if(!boardCtxItem||!token)return;
    var content=ge('board-ctx-ask-input').value.trim();
    if(!content)return;
    ge('board-ctx-ask-input').value='';
    api('POST','/api/v1/board/'+boardCtxItem.id+'/ask',{content:content})
      .then(function(){
        // Reload messages immediately after sending and start polling for the reply.
        loadBoardCtx();
        if(!askPollTimer&&boardCtxItem&&boardCtxItem.active_task_id){
          var pollID=boardCtxItem.id;
          askPollTimer=setInterval(function(){
            if(boardCtxTab!=='ask'||!boardCtxItem||boardCtxItem.id!==pollID){stopAskPoll();return;}
            api('GET','/api/v1/board/'+pollID+'/messages',null)
              .then(function(msgs){
                var body=ge('board-ctx-body');
                if(!body)return;
                // Only re-render the thread, not the whole panel.
                if(!msgs||!msgs.length)return;
                var html='<div class="ask-thread">';
                msgs.forEach(function(m){
                  var isUser=m.direction==='outbound';
                  html+='<div class="ask-msg '+(isUser?'ask-msg-user':'ask-msg-bot')+'">'
                    +'<div class="ask-msg-label">'+(isUser?'You':esc(m.bot_name||'Bot'))+'</div>'
                    +'<div class="ask-msg-body">'+esc(m.content)+'</div>'
                    +'</div>';
                });
                html+='</div>';
                body.innerHTML=html;
              })
              .catch(function(){});
          },2000);
        }
      })
      .catch(function(e){alert('Failed: '+e.message)});
  }

  // ── Task context panel ────────────────────────────────────────────────────────
  var tctxH=260;
  function openTaskCtx(task,rowEl){
    taskCtxTask=task;
    var panel=ge('task-ctx');
    panel.style.display='flex';
    panel.style.height=tctxH+'px';
    ge('task-ctx-title').textContent='Task: '+esc(task.bot_name);
    tctxTab(taskCtxActiveTab);
    if(rowEl){requestAnimationFrame(function(){rowEl.scrollIntoView({block:'nearest',behavior:'smooth'})})}
  }

  function tctxTab(name){
    taskCtxActiveTab=name;
    ['detail','output'].forEach(function(t){var el=ge('tctx-t-'+t);if(el)el.classList.toggle('on',t===name)});
    loadTaskCtx();
  }

  function closeTaskCtx(){
    var panel=ge('task-ctx');
    panel.style.height='0';
    panel.style.display='none';
    taskCtxTask=null;
  }

  function loadTaskCtx(){
    if(!taskCtxTask)return;
    var body=ge('task-ctx-body');
    var t=taskCtxTask;
    if(taskCtxActiveTab==='output'){
      if(t.output){
        body.innerHTML='<div class="ctx-output" style="max-height:none">'+esc(t.output)+'</div>';
      } else if(t.status==='running'){
        body.innerHTML='<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>';
      } else {
        body.innerHTML='<div style="color:#475569;font-size:.78rem">No output recorded.</div>';
      }
      return;
    }
    body.innerHTML=
      (t.title?'<div class="ctx-row"><span class="ctx-lbl">Title</span><span class="ctx-val" style="font-weight:500">'+esc(t.title)+'</span></div>':'')+
      '<div class="ctx-row"><span class="ctx-lbl">Bot</span><span class="ctx-val">'+esc(t.bot_name)+'</span></div>'+
      '<div class="ctx-row"><span class="ctx-lbl">Status</span><span class="ctx-val">'+esc(t.status)+'</span></div>'+
      '<div class="ctx-row"><span class="ctx-lbl">Source</span><span class="ctx-val">'+(t.source||'&#x2014;')+'</span></div>'+
      '<div class="ctx-row"><span class="ctx-lbl">Created</span><span class="ctx-val">'+ago(t.created_at)+'</span></div>'+
      (t.dispatched_at?'<div class="ctx-row"><span class="ctx-lbl">Dispatched</span><span class="ctx-val">'+ago(t.dispatched_at)+'</span></div>':'')+
      (t.completed_at?'<div class="ctx-row"><span class="ctx-lbl">Completed</span><span class="ctx-val">'+ago(t.completed_at)+'</span></div>':'')+
      '<div class="ctx-row"><span class="ctx-lbl">Instruction</span><span class="ctx-val">'+esc(t.instruction)+'</span></div>';
  }

  // ── Task context resize ───────────────────────────────────────────────────────
  (function(){
    var handle=ge('tctx-resize');
    var panel=ge('task-ctx');
    if(!handle||!panel)return;
    var startY=0,startH=0,active=false;
    handle.addEventListener('mousedown',function(e){
      active=true;startY=e.clientY;startH=tctxH;
      document.body.style.cursor='ns-resize';
      document.body.style.userSelect='none';
      e.preventDefault();
    });
    document.addEventListener('mousemove',function(e){
      if(!active)return;
      tctxH=Math.max(120,Math.min(700,startH+(startY-e.clientY)));
      panel.style.height=tctxH+'px';
    });
    document.addEventListener('mouseup',function(){
      if(!active)return;
      active=false;
      document.body.style.cursor='';
      document.body.style.userSelect='';
    });
  })();

  function openTaskCtxById(id){
    var task=allTasksList.find(function(t){return t.id===id});
    if(task)openTaskCtx(task);
  }

  // ── Attachment helpers ─────────────────────────────────────────────────────────
  function fmtBytes(b){
    if(b<1024)return b+'B';
    if(b<1048576)return (b/1024).toFixed(1)+'KB';
    return (b/1048576).toFixed(1)+'MB';
  }

  function uploadFiles(input){
    if(!boardCtxItem||!token)return;
    var files=input.files;
    if(!files||files.length===0)return;
    var fd=new FormData();
    for(var i=0;i<files.length;i++)fd.append('files',files[i]);
    fetch('/api/v1/board/'+boardCtxItem.id+'/attachments',{
      method:'POST',
      headers:{Authorization:'Bearer '+token},
      body:fd
    }).then(function(r){return r.json()}).then(function(item){
      boardCtxItem=item;
      loadBoardCtx();
    }).catch(function(e){alert('Upload failed: '+e.message)});
  }

  function deleteAttachment(itemId,attId){
    if(!confirm('Remove this attachment?'))return;
    api('DELETE','/api/v1/board/'+itemId+'/attachments/'+attId,null)
      .then(function(){
        if(boardCtxItem&&boardCtxItem.id===itemId){
          boardCtxItem.attachments=(boardCtxItem.attachments||[]).filter(function(a){return a.id!==attId});
          loadBoardCtx();
        }
      }).catch(function(e){alert('Delete failed: '+e.message)});
  }

  function viewAttachment(itemId,attId,name){
    ge('viewer-title').textContent=name;
    ge('viewer-pre').textContent='Loading…';
    var url='/api/v1/board/'+itemId+'/attachments/'+attId;
    ge('viewer-dl').href=url;
    ge('viewer-dl').download=name;
    ge('viewer-overlay').style.display='flex';
    fetch(url,{headers:{Authorization:'Bearer '+token}})
      .then(function(r){
        var ct=r.headers.get('content-type')||'';
        if(ct.startsWith('text/')||ct==='application/json'||ct===''){
          return r.text().then(function(t){ge('viewer-pre').textContent=t});
        } else {
          return r.blob().then(function(){
            ge('viewer-pre').textContent='[Binary file — use Download button]';
          });
        }
      }).catch(function(){ge('viewer-pre').textContent='Failed to load'});
  }

  function closeViewer(){ge('viewer-overlay').style.display='none'}

  // ── Refresh loop ──────────────────────────────────────────────────────────────
  function loadWorkDirs(){
    api('GET','/api/v1/workdirs',null).then(function(dirs){allWorkDirs=dirs||[];}).catch(function(){allWorkDirs=[]});
  }

  function refreshAll(){
    loadBoard(); loadTeam(); loadThreads();
    if(activeTab==='tasks')loadTasks();
    if(activeTab==='chat')loadChat();
    if(activeTab==='plugins'){loadPlugins();loadRegistries();}
    if(activeTab==='dlq')loadDLQ();
    if(activeTab==='users')loadUsers();
    if(boardCtxItem){
      var cur=allItems.find(function(x){return x.id===boardCtxItem.id});
      if(cur){boardCtxItem=cur;if(boardCtxTab==='output')loadBoardCtx();}
    }
    if(taskCtxTask){
      api('GET','/api/v1/tasks/'+taskCtxTask.id,null).then(function(t){taskCtxTask=t;loadTaskCtx();}).catch(function(){});
    }
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

  loadWorkDirs();
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
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
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

// ── plugin registry handlers ──────────────────────────────────────────────────

func (s *Server) handleRegistriesList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginRegistry == nil {
		writeJSON(w, http.StatusOK, []domain.PluginRegistry{})
		return
	}
	regs, err := s.cfg.PluginRegistry.List(r.Context())
	if err != nil {
		writeInternalError(w, "registries list", err)
		return
	}
	if regs == nil {
		regs = []domain.PluginRegistry{}
	}
	writeJSON(w, http.StatusOK, regs)
}

func (s *Server) handleRegistriesAdd(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginRegistry == nil {
		writeError(w, http.StatusNotImplemented, "plugin registry not configured")
		return
	}
	var req domain.AddRegistryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !strings.HasPrefix(req.URL, "https://") {
		writeError(w, http.StatusBadRequest, "registry URL must use https://")
		return
	}
	reg := domain.PluginRegistry(req) //nolint:gocritic
	if err := s.cfg.PluginRegistry.Add(r.Context(), reg); err != nil {
		writeInternalError(w, "registries add", err)
		return
	}
	writeJSON(w, http.StatusCreated, reg)
}

func (s *Server) handleRegistriesRemove(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginRegistry == nil {
		writeError(w, http.StatusNotImplemented, "plugin registry not configured")
		return
	}
	name := r.PathValue("name")
	if err := s.cfg.PluginRegistry.Remove(r.Context(), name); err != nil {
		writeInternalError(w, "registries remove", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRegistriesFetchIndex(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginRegistry == nil {
		writeError(w, http.StatusNotImplemented, "plugin registry not configured")
		return
	}
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"
	idx, err := s.cfg.PluginRegistry.FetchIndex(r.Context(), name, force)
	if err != nil {
		writeInternalError(w, "registries fetch index", err)
		return
	}
	writeJSON(w, http.StatusOK, idx)
}

// ── plugin management handlers ────────────────────────────────────────────────

func (s *Server) handlePluginsList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeJSON(w, http.StatusOK, []domain.Plugin{})
		return
	}
	plugins, err := s.cfg.PluginManage.List(r.Context())
	if err != nil {
		writeInternalError(w, "plugins list", err)
		return
	}
	if plugins == nil {
		plugins = []domain.Plugin{}
	}
	writeJSON(w, http.StatusOK, plugins)
}

func (s *Server) handlePluginsGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	id := r.PathValue("id")
	p, err := s.cfg.PluginManage.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePluginsInstall(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginInstall == nil {
		writeError(w, http.StatusNotImplemented, "plugin installer not configured")
		return
	}
	var req domain.InstallPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	p, err := s.cfg.PluginInstall.Install(r.Context(), req.Registry, req.Name, req.Version, actor)
	if err != nil {
		writeInternalError(w, "plugins install", err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handlePluginsApprove(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Approve(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins approve", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginsReject(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Reject(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins reject", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginsEnable(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Enable(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins enable", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginsDisable(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Disable(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins disable", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginsReload(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Reload(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins reload", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginsRemove(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PluginManage == nil {
		writeError(w, http.StatusNotImplemented, "plugin manager not configured")
		return
	}
	id := r.PathValue("id")
	claims := claimsFromContext(r)
	actor := claims.Subject
	if actor == "" {
		actor = "system"
	}
	if err := s.cfg.PluginManage.Remove(r.Context(), id, actor); err != nil {
		if errors.Is(err, domain.ErrPluginNotFound) {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
		writeInternalError(w, "plugins remove", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Pool ──────────────────────────────────────────────────────────────────────

// handlePoolList returns the current tech-lead pool entries as JSON.
// If no Pool is configured (Pool == nil), returns an empty pool array.
func (s *Server) handlePoolList(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Pool []*domain.PoolEntry `json:"pool"`
	}

	var entries []*domain.PoolEntry
	if s.cfg.Pool != nil {
		var err error
		entries, err = s.cfg.Pool.ListEntries(r.Context())
		if err != nil {
			slog.Error("pool list error", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if entries == nil {
		entries = []*domain.PoolEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{Pool: entries})
}
