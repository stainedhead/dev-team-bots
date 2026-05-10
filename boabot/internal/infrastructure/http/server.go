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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	apporchestrator "github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
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
	Auth             AuthProvider
	Board            domain.BoardStore
	Team             domain.ControlPlane
	Users            domain.UserStore
	Skills           domain.SkillRegistry
	DLQ              domain.DLQStore
	Tasks            domain.DirectTaskStore
	Dispatcher       domain.TaskDispatcher
	Chat             domain.ChatStore
	AskRouter        domain.AskRouter           // optional; routes mid-task questions to running bots
	Pool             domain.TechLeadPool        // optional; nil means pool endpoint returns empty
	AllowedWorkDirs  []string                   // whitelisted base directories for item working directories
	TaskLogBase      string                     // base directory for per-task log directories (optional)
	IconPNG          []byte                     // raw icon served at /imgs/boabot-icon-raw.png
	ProcessedIconPNG []byte                     // dark-pixels-transparent variant served at /imgs/boabot-icon.png
	FaviconIconPNG   []byte                     // blue/white-filter variant served at /imgs/boabot-favicon.png
	BoardDispatcher  domain.BoardItemDispatcher // use-case for dispatching board items to bots
	MaxConcurrent    int                        // max items in-progress simultaneously (0 = unlimited)
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
	// Auto-wire BoardDispatcher from Dispatcher when not explicitly provided.
	if cfg.BoardDispatcher == nil && cfg.Dispatcher != nil {
		cfg.BoardDispatcher = apporchestrator.NewBoardDispatch(apporchestrator.BoardDispatchConfig{
			Dispatcher:      cfg.Dispatcher,
			Board:           cfg.Board,
			AllowedWorkDirs: cfg.AllowedWorkDirs,
		})
	}
	return &Server{cfg: cfg}
}

// Handler returns the root http.Handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	if len(s.cfg.ProcessedIconPNG) > 0 || len(s.cfg.IconPNG) > 0 {
		mux.HandleFunc("GET /imgs/boabot-icon.png", s.handleIcon)
		mux.HandleFunc("GET /imgs/boabot-icon-raw.png", s.handleIconRaw)
		mux.HandleFunc("GET /imgs/boabot-favicon.png", s.handleFavicon)
	}

	// Public
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Work directory roots (public read — UI needs these before login)
	mux.HandleFunc("GET /api/v1/workdirs", s.handleWorkDirList)
	mux.HandleFunc("GET /api/v1/files", s.auth(s.handleListFiles))

	// Board — read endpoints are public; write endpoints require auth
	mux.HandleFunc("GET /api/v1/board", s.handleBoardList)
	mux.HandleFunc("GET /api/v1/board/{id}", s.handleBoardGet)
	mux.HandleFunc("POST /api/v1/board", s.auth(s.handleBoardCreate))
	mux.HandleFunc("PATCH /api/v1/board/{id}", s.auth(s.handleBoardUpdate))
	mux.HandleFunc("DELETE /api/v1/board/{id}", s.auth(s.handleBoardDelete))
	mux.HandleFunc("POST /api/v1/board/{id}/assign", s.auth(s.handleBoardAssign))
	mux.HandleFunc("POST /api/v1/board/{id}/close", s.auth(s.handleBoardClose))
	mux.HandleFunc("POST /api/v1/board/reorder", s.auth(s.handleBoardReorder))
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
	mux.HandleFunc("POST /api/v1/tasks/{id}/ask", s.auth(s.handleTaskAsk))
	mux.HandleFunc("GET /api/v1/tasks/{id}/messages", s.auth(s.handleTaskMessages))
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

	// Shell (bash mode in chat UI)
	mux.HandleFunc("POST /api/v1/shell", s.auth(s.handleShell))

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

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "dir required")
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !isAllowedWorkDir(abs, s.cfg.AllowedWorkDirs) {
		writeError(w, http.StatusForbidden, "path not within an allowed work directory")
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, "directory not found")
		return
	}
	type fileEntry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	items := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			items = append(items, fileEntry{Name: e.Name(), IsDir: e.IsDir()})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		WorkDir string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command required")
		return
	}
	// WorkDir is optional; default to first allowed dir or temp.
	workDir := req.WorkDir
	if workDir == "" && len(s.cfg.AllowedWorkDirs) > 0 {
		workDir = s.cfg.AllowedWorkDirs[0]
	}
	if workDir == "" {
		workDir = os.TempDir()
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid work_dir")
		return
	}
	// Security: work_dir must be within an allowed directory (if any are configured).
	if len(s.cfg.AllowedWorkDirs) > 0 && !isAllowedWorkDir(abs, s.cfg.AllowedWorkDirs) {
		writeError(w, http.StatusForbidden, "work_dir not within an allowed work directory")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	cmd.Dir = abs
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	output := buf.String()

	type shellResp struct {
		Output  string `json:"output"`
		IsError bool   `json:"is_error"`
	}
	writeJSON(w, http.StatusOK, shellResp{Output: output, IsError: runErr != nil})
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
	oldStatus := existing.Status
	var req struct {
		Title               *string    `json:"title"`
		Description         *string    `json:"description"`
		Status              *string    `json:"status"`
		AssignedTo          *string    `json:"assigned_to"`
		WorkDir             *string    `json:"work_dir"`
		QueueMode           *string    `json:"queue_mode"`
		QueueRunAt          *time.Time `json:"queue_run_at"`
		QueueAfterItemID    *string    `json:"queue_after_item_id"`
		QueueRequireSuccess *bool      `json:"queue_require_success"`
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
		newStatus := domain.WorkItemStatus(*req.Status)

		// Capacity check: reject in-progress transitions when at the concurrent limit.
		if newStatus == domain.WorkItemStatusInProgress &&
			oldStatus != domain.WorkItemStatusInProgress &&
			s.cfg.MaxConcurrent > 0 {
			inProg, listErr := s.cfg.Board.List(r.Context(), domain.WorkItemFilter{Status: domain.WorkItemStatusInProgress})
			if listErr == nil && len(inProg) >= s.cfg.MaxConcurrent {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":          "at_capacity",
					"max_concurrent": s.cfg.MaxConcurrent,
					"current":        len(inProg),
				})
				return
			}
		}

		existing.Status = newStatus
		if existing.Status != domain.WorkItemStatusInProgress {
			existing.ActiveTaskID = ""
		}

		// Record when an item enters the queued state.
		if existing.Status == domain.WorkItemStatusQueued && oldStatus != domain.WorkItemStatusQueued {
			now := time.Now().UTC()
			existing.QueuedAt = &now
		}
		// Clear queue config when leaving queued state.
		if existing.Status != domain.WorkItemStatusQueued {
			existing.QueueMode = ""
			existing.QueueRunAt = nil
			existing.QueueAfterItemID = ""
			existing.QueueRequireSuccess = false
			existing.QueuedAt = nil
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
	// Apply queue config fields (only meaningful while status is queued).
	if req.QueueMode != nil {
		existing.QueueMode = *req.QueueMode
	}
	if req.QueueRunAt != nil {
		existing.QueueRunAt = req.QueueRunAt
	}
	if req.QueueAfterItemID != nil {
		existing.QueueAfterItemID = *req.QueueAfterItemID
	}
	if req.QueueRequireSuccess != nil {
		existing.QueueRequireSuccess = *req.QueueRequireSuccess
	}

	existing.UpdatedAt = time.Now().UTC()
	updated, err := s.cfg.Board.Update(r.Context(), existing)
	if err != nil {
		writeInternalError(w, "board update", err)
		return
	}

	// When the status changes, append the moved item to the end of the new column.
	if req.Status != nil && updated.Status != oldStatus {
		colItems, listErr := s.cfg.Board.List(r.Context(), domain.WorkItemFilter{Status: updated.Status})
		if listErr == nil {
			ids := make([]string, 0, len(colItems))
			for _, it := range colItems {
				if it.ID != updated.ID {
					ids = append(ids, it.ID)
				}
			}
			ids = append(ids, updated.ID)
			if reorderErr := s.cfg.Board.Reorder(r.Context(), ids); reorderErr != nil {
				slog.Warn("board reorder after status change failed", "err", reorderErr)
			}
		}
	}

	// If status moved to in-progress and a bot is assigned, dispatch via BoardDispatcher.
	if updated.Status == domain.WorkItemStatusInProgress &&
		updated.AssignedTo != "" &&
		oldStatus != domain.WorkItemStatusInProgress &&
		s.cfg.BoardDispatcher != nil {
		if fresh, dispErr := s.cfg.BoardDispatcher.DispatchBoardItem(r.Context(), updated); dispErr != nil {
			slog.Warn("board→bot dispatch failed", "bot", updated.AssignedTo, "item", updated.ID, "err", dispErr)
		} else {
			updated = fresh
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

func (s *Server) handleBoardReorder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids required")
		return
	}
	if err := s.cfg.Board.Reorder(r.Context(), req.IDs); err != nil {
		writeInternalError(w, "board reorder", err)
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
		if t.Source == domain.DirectTaskSourceOperator {
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
		if t.Source == domain.DirectTaskSourceOperator {
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

	// AskRouter delivers questions between tool-call iterations of an active
	// Execute loop. It only makes sense when the item is in-progress; for any
	// other status the bot's Execute loop is not running and the channel is
	// never drained.
	if item.Status == domain.WorkItemStatusInProgress && s.cfg.AskRouter != nil {
		midTask := fmt.Sprintf("Regarding board item '%s': %s", item.Title, req.Content)
		s.cfg.AskRouter.Enqueue(item.AssignedTo, domain.AskRequest{
			Question: midTask,
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
	} else if s.cfg.Dispatcher != nil {
		// Bot is idle — dispatch a Q&A task with full item context so the bot
		// can answer questions about the work item without taking active actions.
		chatInstruction := buildBoardAskInstruction(item, req.Content, func() []domain.ChatMessage {
			msgs, _ := s.cfg.Chat.List(ctx, threadID)
			return msgs
		})
		_, _ = s.cfg.Dispatcher.Dispatch(ctx, item.AssignedTo, chatInstruction, nil,
			domain.DirectTaskSourceChat, threadID, item.WorkDir)
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

// handleTaskAsk routes a question to the bot that ran a task and records the
// conversation in a per-task private thread (thread ID "task-<id>").
func (s *Server) handleTaskAsk(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	task, err := s.cfg.Tasks.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}

	ctx := r.Context()
	threadID := "task-" + id
	userMsg := domain.ChatMessage{
		ThreadID:  threadID,
		BotName:   task.BotName,
		Direction: domain.ChatDirectionOutbound,
		Content:   req.Content,
	}
	_ = s.cfg.Chat.Append(ctx, userMsg)

	replyFn := func(reply string) {
		botMsg := domain.ChatMessage{
			ThreadID:  threadID,
			BotName:   task.BotName,
			Direction: domain.ChatDirectionInbound,
			Content:   reply,
		}
		_ = s.cfg.Chat.Append(context.Background(), botMsg)
	}

	title := task.Title
	if title == "" {
		title = "(no title)"
	}
	question := fmt.Sprintf("Regarding task '%s': %s", title, req.Content)

	// Try mid-task interrupt first; if the bot is not actively running fall back
	// to dispatching a new task so the question is always answered.
	routed := s.cfg.AskRouter != nil && s.cfg.AskRouter.Enqueue(task.BotName, domain.AskRequest{
		Question: question,
		ReplyFn:  replyFn,
	})
	if !routed && s.cfg.Dispatcher != nil {
		// Build instruction with task context and any prior conversation history.
		instruction := buildTaskAskInstruction(task, req.Content, func() []domain.ChatMessage {
			msgs, _ := s.cfg.Chat.List(ctx, threadID)
			return msgs
		})
		dispatched, dispErr := s.cfg.Dispatcher.Dispatch(ctx, task.BotName, instruction, nil,
			domain.DirectTaskSourceChat, threadID, task.WorkDir)
		if dispErr == nil {
			userMsg.TaskID = dispatched.ID
		}
	}

	writeJSON(w, http.StatusCreated, userMsg)
}

// handleTaskMessages returns the private per-task conversation thread.
func (s *Server) handleTaskMessages(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat not available")
		return
	}
	id := r.PathValue("id")
	msgs, err := s.cfg.Chat.List(r.Context(), "task-"+id)
	if err != nil {
		writeInternalError(w, "task messages", err)
		return
	}
	if msgs == nil {
		msgs = []domain.ChatMessage{}
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	writeJSON(w, http.StatusOK, msgs)
}

// buildTaskAskInstruction assembles the prompt for a fallback task-ask dispatch.
// It includes the original task context followed by conversation history so the
// bot can answer questions about a completed task.
func buildTaskAskInstruction(task domain.DirectTask, question string, history func() []domain.ChatMessage) string {
	var sb strings.Builder
	title := task.Title
	if title == "" {
		title = "(no title)"
	}
	fmt.Fprintf(&sb, "You are being asked a follow-up question about a task you previously completed.\n\nTask title: %s\n\nOriginal task instruction:\n%s\n\n", title, task.Instruction)

	msgs := history()
	// msgs is newest-first; reverse for chronological, skip the just-appended outbound message.
	var prior []domain.ChatMessage
	for i := len(msgs) - 1; i >= 1 && len(prior) < 10; i-- {
		prior = append(prior, msgs[i])
	}
	if len(prior) > 0 {
		sb.WriteString("Prior conversation:\n")
		for _, m := range prior {
			who := "Operator"
			if m.Direction == domain.ChatDirectionInbound {
				who = task.BotName
			}
			fmt.Fprintf(&sb, "%s: %s\n", who, m.Content)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "Operator: %s", question)
	return sb.String()
}

// buildBoardAskInstruction assembles a Q&A prompt for dispatching when the
// operator asks about a board item that is not currently in-progress. It
// frames the bot as an answerer (not an actor), and includes the item's
// description, status, last output, and prior conversation history.
func buildBoardAskInstruction(item domain.WorkItem, question string, history func() []domain.ChatMessage) string {
	var sb strings.Builder
	sb.WriteString("You are in a conversation with an operator about a work item on the team board. ")
	sb.WriteString("Your role is to answer questions, explain your previous actions, and help the operator understand the current state of the work. ")
	sb.WriteString("Do not take active actions — this is a Q&A context.\n\n")

	fmt.Fprintf(&sb, "Work item: %s\n", item.Title)
	if item.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", item.Description)
	}
	fmt.Fprintf(&sb, "Status: %s\n", string(item.Status))
	if item.LastResult != "" {
		fmt.Fprintf(&sb, "\nLast output:\n%s\n", item.LastResult)
	}

	msgs := history()
	// msgs is newest-first; reverse for chronological, skip the just-appended outbound message.
	var prior []domain.ChatMessage
	for i := len(msgs) - 1; i >= 1 && len(prior) < 10; i-- {
		prior = append(prior, msgs[i])
	}
	if len(prior) > 0 {
		sb.WriteString("\nPrior conversation:\n")
		for _, m := range prior {
			who := "Operator"
			if m.Direction == domain.ChatDirectionInbound {
				who = item.AssignedTo
			}
			fmt.Fprintf(&sb, "%s: %s\n", who, m.Content)
		}
	}
	fmt.Fprintf(&sb, "\nOperator: %s", question)
	return sb.String()
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
				fmt.Fprintf(&sb, "%s: %s\n", who, m.Content)
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
	case domain.WorkItemStatusBacklog, domain.WorkItemStatusQueued,
		domain.WorkItemStatusInProgress, domain.WorkItemStatusBlocked,
		domain.WorkItemStatusDone, domain.WorkItemStatusErrored:
		return true
	}
	return false
}

func (s *Server) handleIcon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(s.cfg.ProcessedIconPNG)
}

func (s *Server) handleIconRaw(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(s.cfg.IconPNG)
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(s.cfg.FaviconIconPNG)
}

// ── Kanban web UI ─────────────────────────────────────────────────────────────

const kanbanHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>BaoBot Control</title>
  <link rel="icon" type="image/png" href="/imgs/boabot-favicon.png">
  <style>
    *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
    body{font-family:system-ui,-apple-system,sans-serif;background:#080e1a;color:#e2e8f0;height:100vh;display:flex;flex-direction:column;overflow:hidden}

    /* ── Header ── */
    header{padding:.6rem 1.25rem;background:#0d1424;border-bottom:1px solid #1a2744;display:flex;align-items:center;gap:.75rem;flex-shrink:0;z-index:10}
    .logo{font-size:1rem;font-weight:700;color:#60a5fa;letter-spacing:-.02em;white-space:nowrap;display:flex;align-items:center;gap:.5rem}
    .logo img{width:1.75rem;height:1.75rem;border-radius:.3rem;object-fit:cover}
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
    .sb-icon-bg{flex-shrink:0;height:160px;background:url('/imgs/boabot-icon.png') center/130px no-repeat;opacity:.46;pointer-events:none;filter:invert(1) sepia(1) saturate(4) hue-rotate(190deg)}
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
    .btype-pill{font-size:.58rem;padding:.05rem .3rem;background:#172032;color:#475569;border:1px solid #1a2744;border-radius:3px;flex-shrink:0;white-space:nowrap}
    .bmeta{margin-top:.25rem;font-size:.62rem;color:#334155;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .binfo-link{cursor:pointer;color:#475569;font-size:.65rem;margin-left:.3rem;opacity:.7;flex-shrink:0}
    .binfo-link:hover{color:#93c5fd;opacity:1}
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
    .col{background:#0f1829;border:1px solid #1a2744;border-radius:.5rem;flex:1;min-width:240px;display:flex;flex-direction:column;max-height:100%}
    .col.over{border-color:#3b82f6;background:#0d1d35}
    .col-hdr{padding:.6rem .75rem;font-size:.65rem;font-weight:700;text-transform:uppercase;letter-spacing:.07em;color:#64748b;border-bottom:1px solid #1a2744;display:flex;align-items:center;gap:.4rem;flex-shrink:0}
    .col-cnt{padding:.05rem .35rem;border-radius:9999px;background:#1a2744;color:#475569;font-size:.6rem;font-weight:600}
    .col-body{flex:1;overflow-y:auto;padding:.375rem;min-height:60px}
    .card{background:#080e1a;border:1px solid #1a2744;border-radius:.35rem;padding:.715rem .65rem;margin-bottom:.3rem;cursor:grab;user-select:none;transition:border-color .15s,opacity .15s;position:relative}
    .card:hover{border-color:#2d3e5a}
    .card.card-sel{border-color:#3b82f6;background:#0a1628;box-shadow:inset 3px 0 0 #3b82f6}
    .card.dragging{opacity:.35;cursor:grabbing}
    .card-close{position:absolute;top:.3rem;right:.3rem;width:1.1rem;height:1.1rem;display:flex;align-items:center;justify-content:center;background:transparent;border:none;border-radius:.2rem;color:#475569;font-size:.7rem;line-height:1;cursor:pointer;opacity:0;transition:opacity .1s,color .1s,background .1s;padding:0}
    .card:hover .card-close{opacity:1}
    .card-close:hover{color:#f87171;background:#1a0808}
    .card.drag-above{border-top:2px solid #3b82f6;border-top-left-radius:0;border-top-right-radius:0}
    .card.drag-below{border-bottom:2px solid #3b82f6;border-bottom-left-radius:0;border-bottom-right-radius:0}
    .card-title{font-size:.78rem;font-weight:500;line-height:1.35}
    .card-desc{font-size:.68rem;color:#475569;margin-top:.2rem;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
    .card-foot{display:flex;align-items:center;gap:.3rem;margin-top:.4rem}
    .card-who{font-size:.62rem;color:#60a5fa;background:#1e3a5f22;padding:.08rem .35rem;border-radius:9999px;border:1px solid #1e3a5f44}
    .card-age{font-size:.62rem;color:#334155;margin-left:auto}
    .card-queue-info{font-size:.65rem;color:#93c5fd;margin-top:.25rem;opacity:.85}
    .card-queue-edit{margin-top:.25rem}
    .card-status-pill{display:inline-block;padding:.06rem .4rem;border-radius:9999px;font-size:.6rem;font-weight:700;margin-right:.3rem;vertical-align:middle}
    .card-status-done{background:#166534;color:#bbf7d0}
    .card-status-errored{background:#7f1d1d;color:#fecaca}
    .col-backlog .col-hdr,.col-queued .col-hdr,.col-inprogress .col-hdr,.col-blocked .col-hdr,.col-done .col-hdr{color:#93c5fd}
    .col-errored .col-hdr{color:#f87171}
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
    /* ── Plugin & Skills panels ── */
    #pane-plugins.on{overflow-y:auto}
    .plugin-panel{display:flex;flex-direction:column;min-width:0}
    .plugin-panel-body{height:240px;min-height:60px;overflow:auto;flex-shrink:0}
    .plugin-resize-handle{height:8px;cursor:row-resize;flex-shrink:0;border-top:2px solid #1a2744;border-bottom:2px solid #1a2744;margin:2px 0;transition:background .15s;position:relative}
    .plugin-resize-handle:hover,.plugin-resize-handle.prd-active{background:#1e3a5f}
    /* ── Plugin detail slide-in ── */
    #plugin-detail-panel h3{margin:0 0 1rem;font-size:.95rem;color:#93c5fd;padding-bottom:.6rem;border-bottom:1px solid #1a2744}
    #plugin-detail-panel p{margin:.4rem 0;font-size:.82rem;color:#cbd5e1}
    #plugin-detail-panel b{color:#94a3b8;font-weight:600}
    #plugin-detail-panel hr{border:none;border-top:1px solid #1a2744;margin:.75rem 0}
    #plugin-detail-panel ul{margin:.35rem 0;padding-left:1.25rem;font-size:.82rem;color:#94a3b8}
    #plugin-detail-panel li{margin:.2rem 0}
    #plugin-detail-panel a{color:#60a5fa;text-decoration:none}
    #plugin-detail-panel a:hover{text-decoration:underline}
    #plugin-detail-panel pre{background:#080e1a;border:1px solid #1a2744;border-radius:.35rem;padding:.6rem .75rem;color:#86efac;font-size:.7rem;overflow:auto;margin:.35rem 0}
    #plugin-detail-panel .pd-section-title{font-size:.7rem;font-weight:700;text-transform:uppercase;letter-spacing:.08em;color:#475569;margin:.9rem 0 .4rem}
    /* ── Registry browser ── */
    .reg-toolbar-input{padding:.3rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.78rem}
    .reg-toolbar-input::placeholder{color:#475569}
    #registry-cards{display:flex;flex-wrap:wrap;gap:12px;padding:12px;align-content:flex-start}
    .reg-card{background:#0f1829;border:1px solid #1a2744;border-radius:.45rem;padding:14px;min-width:220px;max-width:260px;display:flex;flex-direction:column;gap:6px}
    .reg-card-name{font-weight:600;font-size:.85rem;color:#93c5fd}
    .reg-card-version{font-size:.72rem;color:#475569}
    .reg-card-desc{font-size:.78rem;color:#94a3b8;flex:1}
    .reg-card-tags{display:flex;flex-wrap:wrap;gap:4px}
    .reg-tag{background:#1e3a5f;color:#93c5fd;font-size:.67rem;padding:.15rem .45rem;border-radius:.75rem;white-space:nowrap}
    /* ── Registry modal ── */
    .reg-modal-backdrop{display:none;position:fixed;inset:0;background:rgba(0,0,0,.6);z-index:1000;align-items:center;justify-content:center}
    .reg-modal-box{background:#0f1829;border:1px solid #253a5e;border-radius:.5rem;padding:24px;min-width:380px;box-shadow:0 12px 40px rgba(0,0,0,.8)}
    .reg-modal-box h3{margin:0 0 16px;font-size:.95rem;color:#e2e8f0}
    .reg-modal-box label{display:block;font-size:.78rem;color:#94a3b8;margin-bottom:10px}
    .reg-modal-box input[type=text]{display:block;width:100%;box-sizing:border-box;margin-top:4px;padding:.35rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.82rem}
    .reg-modal-box .reg-modal-check{font-size:.78rem;color:#94a3b8;display:flex;align-items:center;gap:.4rem;margin-bottom:16px}
    .reg-modal-box .reg-modal-acts{display:flex;gap:8px}
    /* ── Command / file mention popup ── */
    .mp-pop{position:fixed;inset:auto;margin:0;padding:0;border:1px solid #253a5e;z-index:9999;background:#0f1829;border-radius:.4rem;overflow-y:auto;max-height:230px;min-width:320px;max-width:540px;box-shadow:0 6px 24px rgba(0,0,0,.65);display:none}
    .mp-pop:popover-open{display:block}
    .mp-item{display:flex;align-items:baseline;padding:.28rem .65rem;cursor:pointer;gap:.6rem;line-height:1.4}
    .mp-item:hover,.mp-sel{background:#1e3a5f}
    .mp-name{color:#e2e8f0;font-family:monospace;font-size:.74rem;flex-shrink:0;white-space:nowrap}
    .mp-dir{color:#60a5fa}
    .mp-desc{color:#64748b;font-size:.67rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1;text-align:right}
    .acts{display:flex;gap:.3rem;align-items:center}
    .empty-state{text-align:center;padding:3rem;color:#1e2d4a;font-style:italic;font-size:.8rem}

    /* ── Dialogs ── */
    dialog{background:#0f1829;color:#e2e8f0;border:1px solid #1a2744;border-radius:.625rem;padding:1.375rem;min-width:min(560px,95vw);box-shadow:0 20px 60px #000a;position:fixed;margin:0;top:50%;left:50%;transform:translate(-50%,-50%)}
    dialog::backdrop{background:#000b}
    dialog h2{font-size:.95rem;font-weight:600;margin-bottom:1rem;cursor:move;user-select:none;padding-bottom:.5rem;margin-bottom:.75rem;border-bottom:1px solid #1a2744}
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
    .chat-bubble{max-width:80%;padding:.5rem .75rem;border-radius:.5rem;font-size:.8rem;line-height:1.5}
    .chat-out{background:#1e3a5f;color:#e2e8f0;align-self:flex-end;border-bottom-right-radius:.125rem}
    .chat-in{background:#1e293b;color:#e2e8f0;align-self:flex-start;border-bottom-left-radius:.125rem}
    .chat-meta{font-size:.62rem;color:#475569;margin-top:.2rem}
    .chat-bubble h1,.chat-bubble h2,.chat-bubble h3,.chat-bubble h4,.ask-msg-body h1,.ask-msg-body h2,.ask-msg-body h3,.ask-msg-body h4{margin:.6rem 0 .25rem;font-weight:700;line-height:1.2}
    .chat-bubble h1,.ask-msg-body h1{font-size:1.1em}.chat-bubble h2,.ask-msg-body h2{font-size:1em}.chat-bubble h3,.ask-msg-body h3{font-size:.95em}.chat-bubble h4,.ask-msg-body h4{font-size:.9em}
    .chat-bubble p,.ask-msg-body p{margin:.25rem 0}
    .chat-bubble ul,.chat-bubble ol,.ask-msg-body ul,.ask-msg-body ol{margin:.25rem 0 .25rem 1.2rem;padding:0}
    .chat-bubble li,.ask-msg-body li{margin:.1rem 0}
    .chat-bubble code,.ask-msg-body code{background:#0a1020;border:1px solid #1a2744;border-radius:.2rem;padding:.05em .3em;font-family:monospace;font-size:.9em}
    .chat-bubble pre,.ask-msg-body pre{background:#0a1020;border:1px solid #1a2744;border-radius:.3rem;padding:.5rem .65rem;overflow-x:auto;margin:.35rem 0}
    .chat-bubble pre code,.ask-msg-body pre code{background:none;border:none;padding:0;font-size:.82em}
    .chat-bubble blockquote,.ask-msg-body blockquote{border-left:3px solid #334155;margin:.25rem 0;padding:.1rem .6rem;color:#94a3b8}
    .chat-bubble hr,.ask-msg-body hr{border:none;border-top:1px solid #1a2744;margin:.4rem 0}
    .chat-bubble strong,.ask-msg-body strong{font-weight:700}.chat-bubble em,.ask-msg-body em{font-style:italic}
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
    .chat-input-wrap{position:relative;flex:1;display:flex}
    .chat-input-wrap textarea{flex:1;padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.82rem;resize:none;height:56px;width:100%;box-sizing:border-box}
    .chat-input-wrap textarea.bash-mode{border-color:#b45309;background:#120e00;color:#fcd34d;padding-left:1.4rem}
    #chat-bash-prefix{position:absolute;left:.45rem;top:.42rem;color:#ef4444;font-weight:700;font-size:.95rem;line-height:1;pointer-events:none;display:none;font-family:monospace}
    .chat-input-row select{padding:.45rem .6rem;background:#0a1020;border:1px solid #1a2744;border-radius:.35rem;color:#e2e8f0;font-size:.78rem}
    .bctx-meta{display:inline-flex;align-items:center;gap:.3rem;flex-shrink:0}
    .bctx-badge{display:inline-flex;align-items:center;font-size:.68rem;padding:.1rem .4rem;border-radius:999px;border:1px solid #1a2744;color:#94a3b8;white-space:nowrap;background:#0d1627}
    /* ── Bash result overlay ── */
    .bash-overlay{position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:10000;display:flex;align-items:center;justify-content:center}
    .bash-box{background:#0a1020;border:1px solid #253a5e;border-radius:.5rem;width:min(780px,94vw);max-height:70vh;display:flex;flex-direction:column;box-shadow:0 12px 40px rgba(0,0,0,.8)}
    .bash-hdr{display:flex;align-items:center;gap:.5rem;padding:.5rem .75rem;border-bottom:1px solid #1a2744;flex-shrink:0}
    .bash-cmd{flex:1;font-family:monospace;font-size:.8rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .bash-close{background:none;border:none;color:#64748b;cursor:pointer;font-size:1.1rem;line-height:1;padding:.1rem .3rem}
    .bash-close:hover{color:#e2e8f0}
    .bash-body{flex:1;overflow-y:auto;padding:.75rem 1rem;font-family:monospace;font-size:.78rem;white-space:pre-wrap;word-break:break-all;color:#e2e8f0;user-select:text;-webkit-user-select:text}
    .bash-body.bash-err{color:#fca5a5}
    .bash-loading{color:#64748b;font-style:italic}

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
    .ctx-hdr{display:flex;align-items:center;gap:.5rem;padding:.5rem 1rem;border-bottom:1px solid #1a2744;flex-shrink:0}
    .ctx-hdr-left{display:flex;align-items:center;gap:.4rem;flex:1;min-width:0;overflow:hidden}
    .ctx-title{font-size:.82rem;color:#e2e8f0;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;flex-shrink:1;min-width:0}
    .ctx-tabs{display:flex;gap:.25rem}
    .ctx-tab{background:none;border:none;color:#64748b;cursor:pointer;font-size:.72rem;padding:.2rem .5rem;border-radius:.25rem}
    .ctx-tab.on{background:#1e3a5f;color:#93c5fd}
    .ctx-body{flex:1;overflow-y:auto;padding:.75rem 1rem;font-size:.78rem;color:#cbd5e1}
    .ctx-row{display:flex;gap:.5rem;margin-bottom:.4rem}
    .ctx-lbl{color:#475569;min-width:80px;flex-shrink:0}
    .ctx-val{color:#e2e8f0;word-break:break-word;flex:1;min-width:0}
    .tctx-meta{display:flex;flex-wrap:wrap;align-items:center;gap:.35rem .5rem;margin-bottom:.6rem}
    .tctx-pill{background:#1e293b;border:1px solid #1a2744;border-radius:999px;padding:.1rem .55rem;font-size:.72rem;color:#94a3b8}
    .tctx-pill.ok{background:#052e16;border-color:#166534;color:#4ade80}
    .tctx-pill.run{background:#1e1b4b;border-color:#3730a3;color:#a5b4fc}
    .tctx-pill.fail{background:#450a0a;border-color:#7f1d1d;color:#f87171}
    .tctx-time{font-size:.7rem;color:#475569}
    .tctx-section-title{font-size:.95rem;font-weight:600;color:#e2e8f0;margin-bottom:.4rem}
    .tctx-instr{font-size:.78rem;color:#cbd5e1;line-height:1.5;margin-bottom:.5rem;white-space:pre-wrap;word-break:break-word}
    .tctx-prompt-link{font-size:.7rem;color:#60a5fa;cursor:pointer;text-decoration:none;display:inline-block;margin-bottom:.35rem}
    .tctx-prompt-link:hover{text-decoration:underline}
    .tctx-body-text{font-size:.85rem;color:#cbd5e1;line-height:1.55;white-space:pre-wrap;word-break:break-word;margin-bottom:.6rem}
    .tctx-prompt{background:#050d1a;border:1px solid #1a2744;border-radius:.3rem;padding:.5rem .65rem;font-size:.72rem;color:#64748b;line-height:1.45;white-space:pre-wrap;word-break:break-word;max-height:200px;overflow-y:auto;margin-bottom:.5rem}
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
    .rt-footer{display:flex;align-items:center;gap:.5rem;margin-top:.8rem;padding-top:.5rem;border-top:1px solid #1a2744;font-size:.72rem}
    .rt-footer .rt-lbl{color:#475569}
    .rt-footer .rt-val{color:#94a3b8;font-variant-numeric:tabular-nums;margin-left:auto}

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
    <div class="sb-icon-bg"></div>
  </aside>

  <main>
    <div class="tabbar">
      <button class="tab on" onclick="tab('board')">Board</button>
      <button class="tab" onclick="tab('tasks')" id="t-tasks">Tasks</button>
      <button class="tab" onclick="tab('chat')" id="t-chat">Chat</button>
      <div style="flex:1"></div>
      <button class="tab" onclick="tab('plugins')" id="t-plugins">Plugins &amp; Skills</button>
      <button class="tab" onclick="tab('dlq')" id="t-dlq">Dead Letter Queue</button>
      <button class="tab" onclick="tab('users')" id="t-users" style="display:none">Users</button>
    </div>

    <!-- Board -->
    <div class="pane on" id="pane-board" style="overflow:hidden">
      <div class="board" id="board" style="flex:1;overflow:auto;min-height:0;min-width:0">
        <div class="col col-backlog" id="col-backlog" data-status="backlog" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Backlog <span class="col-cnt" id="n-backlog">0</span></div>
          <div class="col-body" id="b-backlog"><div class="nil">No items</div></div>
        </div>
        <div class="col col-queued" id="col-queued" data-status="queued" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Queued <span class="col-cnt" id="n-queued">0</span></div>
          <div class="col-body" id="b-queued"><div class="nil">No items</div></div>
        </div>
        <div class="col col-inprogress" id="col-inprogress" data-status="in-progress" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">In Progress <span class="col-cnt" id="n-inprogress">0</span></div>
          <div class="col-body" id="b-inprogress"><div class="nil">No items</div></div>
        </div>
        <div class="col col-blocked" id="col-blocked" data-status="blocked" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Blocked <span class="col-cnt" id="n-blocked">0</span></div>
          <div class="col-body" id="b-blocked"><div class="nil">No items</div></div>
        </div>
        <div class="col col-done" id="col-done" data-status="done" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Done <span class="col-cnt" id="n-done">0</span></div>
          <div class="col-body" id="b-done"><div class="nil">No items</div></div>
        </div>
        <div class="col col-errored" id="col-errored" data-status="errored" ondragover="ov(event)" ondragleave="ol(event)" ondrop="dp(event)">
          <div class="col-hdr">Errored <span class="col-cnt" id="n-errored">0</span></div>
          <div class="col-body" id="b-errored"><div class="nil">No items</div></div>
        </div>
      </div>
      <div class="ctx-panel" id="board-ctx" style="display:none">
        <div class="ctx-resize-handle" id="bctx-resize"></div>
        <div class="ctx-hdr">
          <div class="ctx-hdr-left">
            <span class="ctx-title" id="board-ctx-title">Select an item</span>
            <span class="bctx-meta" id="board-ctx-meta" style="display:none"></span>
          </div>
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
          <input id="board-ctx-ask-input" placeholder="Ask the assigned bot&#x2026;"/>
          <button class="btn btn-primary btn-sm" onclick="boardAsk()">Ask</button>
        </div>
      </div>
    </div>

    <!-- Tasks -->
    <div class="pane" id="pane-tasks" style="overflow:hidden">
      <div class="sec-hdr">
        <div style="display:flex;gap:.4rem;align-items:center;flex-wrap:wrap">
          <button class="btn btn-sm task-filter-btn active" id="tf-all"       onclick="setTaskFilter('all')">All</button>
          <button class="btn btn-sm task-filter-btn"        id="tf-immediate" onclick="setTaskFilter('immediate')">Immediate</button>
          <button class="btn btn-sm task-filter-btn"        id="tf-scheduled" onclick="setTaskFilter('scheduled')">Scheduled</button>
          <div style="width:1px;background:#1a2744;height:1.25rem;margin:0 .2rem"></div>
          <select id="tf-bot" onchange="renderTaskList()" style="background:#0f1829;border:1px solid #1a2744;color:#94a3b8;font-size:.72rem;padding:.25rem .5rem;border-radius:4px">
            <option value="">All bots</option>
          </select>
          <input id="tf-text" type="text" placeholder="Search title&hellip;" oninput="renderTaskList()" style="background:#0f1829;border:1px solid #1a2744;color:#e2e8f0;font-size:.72rem;padding:.25rem .5rem;border-radius:4px;width:10rem"/>
        </div>
        <div class="sec-acts">
          <button id="task-run-btn" class="btn btn-primary btn-sm" disabled onclick="runSelectedTasks()" style="opacity:.35;cursor:not-allowed">Run Selected</button>
          <button id="task-del-btn" class="btn btn-danger btn-sm" disabled onclick="deleteSelectedTasks()" style="opacity:.35;cursor:not-allowed">Delete Selected</button>
          <button class="btn btn-secondary btn-sm" onclick="loadTasks()">Refresh</button>
        </div>
      </div>
      <div id="tasks-list" style="flex:1;overflow:auto;min-width:0"><div class="empty-state">Loading&#x2026;</div></div>
      <div class="ctx-panel" id="task-ctx" style="display:none">
        <div class="ctx-resize-handle" id="tctx-resize"></div>
        <div class="ctx-hdr">
          <span class="ctx-title" id="task-ctx-title">Select a task</span>
          <div class="ctx-tabs">
            <button class="ctx-tab on" id="tctx-t-detail" onclick="tctxTab('detail')">Details</button>
            <button class="ctx-tab" id="tctx-t-ask"    onclick="tctxTab('ask')">Ask</button>
            <button class="ctx-tab" id="tctx-t-output" onclick="tctxTab('output')">Output</button>
          </div>
          <button class="ctx-close" onclick="closeTaskCtx()">&#x2715;</button>
        </div>
        <div class="ctx-body" id="task-ctx-body"></div>
        <div class="ctx-ask-row" id="task-ctx-ask" style="display:none">
          <input id="task-ctx-ask-input" placeholder="Ask the assigned bot&#x2026;"/>
          <button class="btn btn-primary btn-sm" onclick="taskAsk()">Ask</button>
        </div>
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
            <div class="chat-input-wrap">
              <span id="chat-bash-prefix">!</span>
              <textarea id="chat-input" placeholder="Message… (Enter to send, Shift+Enter for newline)"></textarea>
            </div>
            <button class="btn btn-primary" onclick="chatSendOrBash()">Send</button>
          </div>
        </div>
      </div>
      <select id="chat-bot-sel" style="display:none"></select>
    </div>

    <!-- Plugins & Skills -->
    <div class="pane" id="pane-plugins">

      <!-- Registry Browser -->
      <div class="plugin-panel">
        <div class="sec-hdr">
          <div class="sec-title">Registry Browser</div>
          <div class="sec-acts">
            <select id="registry-select" class="reg-toolbar-input" onchange="onRegistryChange()" style="margin-right:8px"></select>
            <input type="text" id="registry-search" class="reg-toolbar-input" placeholder="Search plugins…" oninput="filterRegistryCards()" style="margin-right:8px" />
            <button class="btn btn-secondary btn-sm" onclick="refreshRegistry()">Refresh</button>
            <button id="registry-delete-btn" class="btn btn-sm" style="display:none;background:#7f1d1d;color:#fca5a5;margin-right:4px" onclick="deleteRegistry()">Delete</button>
            <button class="btn btn-secondary btn-sm" onclick="showAddRegistryModal()">Add Registry</button>
          </div>
        </div>
        <div class="plugin-panel-body">
          <div id="registry-cards"><div class="empty-state">Select a registry above</div></div>
        </div>
      </div>

      <!-- Add Registry Modal -->
      <div id="add-registry-modal" class="reg-modal-backdrop">
        <div class="reg-modal-box">
          <h3>Add Registry</h3>
          <label>Name<input id="reg-name" type="text" placeholder="my-registry" /></label>
          <label>URL (https://)<input id="reg-url" type="text" placeholder="https://github.com/owner/repo" /></label>
          <div class="reg-modal-check"><input id="reg-trusted" type="checkbox" /><span>Trusted registry</span></div>
          <div class="reg-modal-acts">
            <button class="btn btn-primary btn-sm" onclick="addRegistry()">Add</button>
            <button class="btn btn-secondary btn-sm" onclick="ge('add-registry-modal').style.display='none'">Cancel</button>
          </div>
        </div>
      </div>

      <div class="plugin-resize-handle"></div>

      <!-- Installed Plugins -->
      <div class="plugin-panel">
        <div class="sec-hdr">
          <div class="sec-title">Installed Plugins</div>
          <div class="sec-acts"><button class="btn btn-secondary btn-sm" onclick="loadPlugins()">Refresh</button></div>
        </div>
        <div class="plugin-panel-body">
          <div id="plugins-body"><div class="empty-state">Loading…</div></div>
        </div>
      </div>

      <!-- Plugin Detail Side Panel -->
      <div id="plugin-detail-panel" style="display:none;position:fixed;top:0;right:0;width:380px;height:100%;background:#0d1424;border-left:1px solid #1a2744;overflow:auto;padding:1.25rem;z-index:500;color:#e2e8f0">
        <button onclick="ge('plugin-detail-panel').style.display='none'" style="float:right;background:none;border:none;font-size:1.1rem;cursor:pointer;color:#475569;line-height:1;padding:.2rem .4rem;border-radius:.25rem" onmouseover="this.style.color='#e2e8f0'" onmouseout="this.style.color='#475569'">✕</button>
        <h3 id="plugin-detail-name"></h3>
        <div id="plugin-detail-content"></div>
      </div>

      <div class="plugin-resize-handle"></div>

      <!-- Uploaded Skills (Legacy) -->
      <div class="plugin-panel">
        <div class="sec-hdr">
          <div class="sec-title">Manually Uploaded Skills (Legacy)</div>
          <div class="sec-acts">
            <button class="btn btn-secondary btn-sm" onclick="ge('skill-upload-inp').click()">Upload Skill</button>
            <button class="btn btn-secondary btn-sm" onclick="loadSkills()">Refresh</button>
          </div>
        </div>
        <input type="file" id="skill-upload-inp" accept=".md,.zip" style="display:none" onchange="uploadSkill(this)"/>
        <div class="plugin-panel-body">
          <div id="skills-body"><div class="empty-state">Loading…</div></div>
        </div>
      </div>

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
  <div style="display:flex;align-items:center;gap:.75rem;margin-bottom:1.25rem">
    <img src="/imgs/boabot-icon-raw.png" alt="BaoBot" style="width:52px;height:52px;border-radius:.5rem;flex-shrink:0"/>
    <h2 style="margin:0">Sign In</h2>
  </div>
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
  <div class="da"><button class="btn btn-secondary" onclick="cls('ni-dlg')">Cancel</button><button class="btn btn-secondary" onclick="doCreateItem(true)">Create and add another</button><button class="btn btn-primary" onclick="doCreateItem()">Create</button></div>
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
  <div class="da"><button class="btn btn-secondary" onclick="cls('at-dlg')">Cancel</button><button class="btn btn-secondary" onclick="doDispatchTask(true)">Create and add another</button><button class="btn btn-primary" onclick="doDispatchTask()">Create</button></div>
</dialog>

<!-- Change Own Password -->
<dialog id="cp-dlg">
  <h2>Change Password</h2>
  <div class="fg"><label class="fl">Current Password</label><input class="fi" id="cp-old" type="password"/></div>
  <div class="fg"><label class="fl">New Password</label><input class="fi" id="cp-new" type="password" autocomplete="new-password"/></div>
  <div class="errmsg" id="cp-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cls('cp-dlg')">Cancel</button><button class="btn btn-primary" onclick="doChangePw()">Update</button></div>
</dialog>

<dialog id="qcfg-dlg" style="min-width:24rem">
  <h2 id="qcfg-title">Queue Configuration</h2>
  <div class="fg" style="gap:.5rem">
    <label style="display:flex;align-items:center;gap:.5rem;cursor:pointer">
      <input type="radio" name="qcfg-mode" value="asap" checked onchange="onQcfgMode()"/> ASAP &mdash; run when a slot is available
    </label>
    <label style="display:flex;align-items:center;gap:.5rem;cursor:pointer">
      <input type="radio" name="qcfg-mode" value="run_when" onchange="onQcfgMode()"/> Run When &mdash; at a time and/or after another item
    </label>
    <div id="qcfg-run-when-wrap" style="display:none;padding-left:1.5rem">
      <div style="font-size:.75rem;color:#64748b;margin-bottom:.35rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em">Run At (optional)</div>
      <div style="display:flex;gap:.4rem;align-items:center;margin-bottom:.75rem">
        <input id="qcfg-run-at-when" type="datetime-local" class="fi" style="flex:1"/>
        <button type="button" title="Open calendar" onclick="try{ge('qcfg-run-at-when').showPicker()}catch(e){ge('qcfg-run-at-when').focus()}" style="background:#1a2744;border:1px solid #253a5e;border-radius:4px;color:#94a3b8;cursor:pointer;font-size:1rem;padding:.3rem .5rem;line-height:1">&#x1F4C5;</button>
      </div>
      <div style="font-size:.75rem;color:#64748b;margin-bottom:.35rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em">Run After (optional)</div>
      <select id="qcfg-after-item" class="fi" style="width:100%;margin-bottom:.4rem">
        <option value="">Select predecessor item&hellip;</option>
      </select>
      <label style="display:flex;align-items:center;gap:.4rem;cursor:pointer;font-size:.8rem">
        <input type="checkbox" id="qcfg-require-success" checked/> Require success (only run if predecessor completes without error)
      </label>
    </div>
  </div>
  <div class="errmsg" id="qcfg-err" style="display:none"></div>
  <div class="da"><button class="btn btn-secondary" onclick="cancelQcfg()">Cancel</button><button class="btn btn-primary" onclick="confirmQcfg()">Queue Item</button></div>
</dialog>

<dialog id="cap-dlg" style="min-width:22rem">
  <h2>At Capacity</h2>
  <p id="cap-msg" style="color:#94a3b8;font-size:.85rem;margin:.25rem 0 .75rem"></p>
  <div class="da">
    <button class="btn btn-secondary" onclick="cls('cap-dlg')">Cancel</button>
    <button class="btn btn-primary" onclick="queueAsASAP()">Queue as ASAP</button>
  </div>
</dialog>

<!-- Bot info dialog -->
<dialog id="bot-info-dlg" style="min-width:24rem;max-width:32rem" onclick="if(event.target===this)this.close()">
  <h2 id="bot-info-title" style="margin-bottom:.5rem"></h2>
  <div id="bot-info-body" style="color:#94a3b8;font-size:.82rem;line-height:1.6"></div>
  <div class="da" style="margin-top:1rem">
    <button type="button" class="btn btn-secondary" onclick="cls('bot-info-dlg')">Close</button>
  </div>
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
  var dropTargetId=null, dropPos=null;
  var activeTab='board', countdown=30, tickTimer=null;
  var activeThreadID=null, allThreads=[];
  var boardCtxItem=null, boardCtxThread=null, boardCtxTab='detail', boardCtxActivity=null, outputPollTimer=null, askPollTimer=null, boardAskPending=false;
  var qcfgPendingItemId=null, qcfgPendingStatus=null, capPendingItemId=null;
  var currentTaskFilter='all', taskBotFilter='', taskTextFilter='';
  var allTasksList=[];

  function renderBoardAskMsgs(msgs){
    var body=ge('board-ctx-body');
    if(!body)return;
    if(!msgs||!msgs.length){
      body.innerHTML='<div style="color:#475569;font-size:.75rem">No messages yet. Ask the bot a question below.</div>';
    } else {
      var html='<div class="ask-thread">';
      msgs.forEach(function(m){
        var isUser=m.direction==='outbound';
        html+='<div class="ask-msg '+(isUser?'ask-msg-user':'ask-msg-bot')+'">'
          +'<div class="ask-msg-label">'+(isUser?'You':esc(m.bot_name||'Bot'))+'</div>'
          +'<div class="ask-msg-body">'+renderMd(m.content)+'</div>'
          +'</div>';
      });
      html+='</div>';
      body.innerHTML=html;
      // Bot has replied — clear pending state.
      if(boardAskPending&&msgs[msgs.length-1].direction==='inbound')boardAskPending=false;
    }
    if(boardAskPending){
      var td=document.createElement('div');
      td.style.cssText='display:flex;align-items:center;gap:.5rem;padding:.35rem .65rem';
      td.innerHTML='<span style="font-size:.65rem;color:#64748b">thinking…</span><div class="chat-thinking"><span></span><span></span><span></span></div>';
      body.appendChild(td);
    }
    var bAt=boardCtxActivity&&boardCtxActivity.task;
    var ftrHtml=bAt?runTimeFooter(bAt.dispatched_at,bAt.completed_at):'';
    if(ftrHtml){var ftrEl=document.createElement('div');ftrEl.innerHTML=ftrHtml;if(ftrEl.firstElementChild)body.appendChild(ftrEl.firstElementChild);}
    body.scrollTop=body.scrollHeight;
  }
  var taskCtxTask=null, taskCtxActiveTab='detail', taskOutputPollTimer=null, elapsedTimer=null;
  var dragging=false;

  // ── Util ────────────────────────────────────────────────────────────────────
  function esc(s){if(!s)return'';return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}

  function renderMd(raw){
    if(!raw)return'';
    function inl(s){
      s=s.replace(/\x60([^\x60]+)\x60/g,'<code>$1</code>');
      s=s.replace(/\*\*\*([^*]+)\*\*\*/g,'<strong><em>$1</em></strong>');
      s=s.replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
      s=s.replace(/\*([^*\n]+)\*/g,'<em>$1</em>');
      s=s.replace(/_([^_\n]+)_/g,'<em>$1</em>');
      s=s.replace(/~~([^~]+)~~/g,'<del>$1</del>');
      return s;
    }
    var fence='\x60\x60\x60';
    var lines=raw.split('\n'),out='',inCode=false,codeBuf='',inList='',listBuf='';
    function flushList(){
      if(!inList)return;
      out+='<'+(inList==='ul'?'ul':'ol')+'>'+listBuf+'</'+(inList==='ul'?'ul':'ol')+'>';
      inList='';listBuf='';
    }
    for(var i=0;i<lines.length;i++){
      var line=lines[i];
      if(line.indexOf(fence)===0){
        if(!inCode){flushList();codeBuf='';inCode=true;}
        else{out+='<pre><code>'+esc(codeBuf.replace(/\n$/,''))+'</code></pre>';inCode=false;}
        continue;
      }
      if(inCode){codeBuf+=line+'\n';continue;}
      if(/^([-*_] *){3,}$/.test(line.trim())){flushList();out+='<hr>';continue;}
      var hm=line.match(/^(#{1,6}) +(.*)/);
      if(hm){flushList();var lv=hm[1].length;out+='<h'+lv+'>'+inl(esc(hm[2]))+'</h'+lv+'>';continue;}
      if(line.indexOf('> ')===0){flushList();out+='<blockquote>'+inl(esc(line.slice(2)))+'</blockquote>';continue;}
      var ulm=line.match(/^[*-] +(.*)/);
      if(ulm){if(inList==='ol')flushList();inList='ul';listBuf+='<li>'+inl(esc(ulm[1]))+'</li>';continue;}
      var olm=line.match(/^\d+\. +(.*)/);
      if(olm){if(inList==='ul')flushList();inList='ol';listBuf+='<li>'+inl(esc(olm[1]))+'</li>';continue;}
      if(!line.trim()){flushList();out+='<br>';continue;}
      flushList();out+='<p>'+inl(esc(line))+'</p>';
    }
    flushList();
    if(inCode)out+='<pre><code>'+esc(codeBuf)+'</code></pre>';
    return out;
  }
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
      return r.json().then(function(d){
        if(!r.ok){var e=new Error(d.error||r.statusText);e.status=r.status;e.data=d;throw e;}
        return d;
      });
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
    var itemId=dragId;
    dragId=null;
    if(status==='queued'){
      openQcfg(itemId,'queued');
      return;
    }
    if(status==='in-progress'){
      api('PATCH','/api/v1/board/'+itemId,{status:'in-progress'})
        .then(function(){loadBoard()})
        .catch(function(e){
          if(e.status===409&&e.data&&e.data.error==='at_capacity'){
            capPendingItemId=itemId;
            ge('cap-msg').textContent='All '+e.data.max_concurrent+' concurrent slots are in use ('+e.data.current+' items in progress). Queue as ASAP to run when a slot opens?';
            dlg('cap-dlg');
          } else {
            alert('Move failed: '+e.message);
          }
        });
      return;
    }
    api('PATCH','/api/v1/board/'+itemId,{status:status})
      .then(function(){loadBoard()})
      .catch(function(e){alert('Move failed: '+e.message)});
  }

  // ── Board ────────────────────────────────────────────────────────────────────
  var colCfg=[
    {status:'backlog',     hdr:'b-backlog',    cnt:'n-backlog'},
    {status:'queued',      hdr:'b-queued',     cnt:'n-queued'},
    {status:'in-progress', hdr:'b-inprogress', cnt:'n-inprogress'},
    {status:'blocked',     hdr:'b-blocked',    cnt:'n-blocked'},
    {status:'done',        hdr:'b-done',       cnt:'n-done'},
    {status:'errored',     hdr:'b-errored',    cnt:'n-errored'},
  ];

  function fmtQueueAt(iso){
    if(!iso)return '';
    var d=new Date(iso);
    return d.toLocaleDateString(undefined,{month:'short',day:'numeric'})+' '+d.toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit'});
  }

  function leafDir(path){
    if(!path)return '';
    var p=path.replace(/\/+$/,'');
    var i=p.lastIndexOf('/');
    return i>=0?p.slice(i+1):p;
  }

  function queueInfoHtml(it){
    if(it.status!=='queued')return '';
    var mode=it.queue_mode||'asap';
    if(mode==='run_at'&&it.queue_run_at){
      return '<div class="card-queue-info">&#x23F0; Run at '+esc(fmtQueueAt(it.queue_run_at))+'</div>';
    }
    if(mode==='run_after'&&it.queue_after_item_id){
      var pred=allItems.find(function(x){return x.id===it.queue_after_item_id});
      var predTitle=pred?esc(pred.title.substring(0,30)+(pred.title.length>30?'…':'')):esc(it.queue_after_item_id.substring(0,8));
      return '<div class="card-queue-info">&#x23ED; After: '+predTitle+(it.queue_require_success?' (requires success)':'')+'</div>';
    }
    if(mode==='run_when'){
      var parts=[];
      if(it.queue_run_at)parts.push('&#x23F0; '+esc(fmtQueueAt(it.queue_run_at)));
      if(it.queue_after_item_id){
        var p2=allItems.find(function(x){return x.id===it.queue_after_item_id});
        var t2=p2?esc(p2.title.substring(0,25)+(p2.title.length>25?'…':'')):esc(it.queue_after_item_id.substring(0,8));
        parts.push('&#x23ED; '+t2);
      }
      return '<div class="card-queue-info">'+parts.join(' &amp; ')+'</div>';
    }
    return '<div class="card-queue-info">&#x26A1; ASAP</div>';
  }

  function statusPillHtml(it){
    if(it.status==='done')return '<span class="card-status-pill card-status-done">Done</span>';
    if(it.status==='errored')return '<span class="card-status-pill card-status-errored">Errored</span>';
    return '';
  }

  function markSelectedCard(){
    document.querySelectorAll('.card.card-sel').forEach(function(el){el.classList.remove('card-sel');});
    if(boardCtxItem){
      document.querySelectorAll('.card[data-item-id="'+boardCtxItem.id+'"]').forEach(function(el){el.classList.add('card-sel');});
    }
  }

  function makeCard(it){
    var d=document.createElement('div');
    d.className='card';
    d.setAttribute('data-item-id',it.id);
    d.draggable=!!token;
    d.style.cursor=token?'grab':'default';
    d.innerHTML=
      (token?'<button class="card-close" title="Delete" onclick="event.stopPropagation();deleteCardItem(\''+esc(it.id)+'\',\''+esc(it.title)+'\',\''+esc(it.status)+'\')">&#x2715;</button>':'')+
      '<div class="card-title">'+statusPillHtml(it)+esc(it.title)+'</div>'+
      (it.status==='in-progress'&&it.active_task_id?'<div class="card-working">&#x2699; working&hellip;</div>':'')+
      queueInfoHtml(it)+
      (it.description?'<div class="card-desc">'+esc(it.description)+'</div>':'')+
      '<div class="card-foot">'+
        (it.assigned_to?'<span class="card-who">'+esc(it.assigned_to)+'</span>':'')+
        (it.work_dir?'<span class="card-who" style="color:#475569;font-style:normal">&#x1F4C1; '+esc(leafDir(it.work_dir))+'</span>':'')+
        '<span class="card-age">'+ago(it.updated_at)+'</span>'+
      '</div>'+
      (it.status==='queued'&&token?'<div class="card-queue-edit"><button class="btn btn-ghost btn-sm" onclick="event.stopPropagation();editQueueConfig(\''+esc(it.id)+'\')">&#x270E; Queue config</button></div>':'');
    d.addEventListener('dragstart',function(ev){dragging=true;dragId=it.id;d.classList.add('dragging');ev.dataTransfer.effectAllowed='move'});
    d.addEventListener('dragend',function(){dragging=false;d.classList.remove('dragging')});
    d.addEventListener('dragover',function(ev){
      if(!dragId||dragId===it.id||!dragging)return;
      ev.preventDefault();ev.stopPropagation();
      var mid=d.getBoundingClientRect().top+d.getBoundingClientRect().height/2;
      if(ev.clientY<mid){d.classList.add('drag-above');d.classList.remove('drag-below');dropPos='before';}
      else{d.classList.remove('drag-above');d.classList.add('drag-below');dropPos='after';}
      dropTargetId=it.id;
    });
    d.addEventListener('dragleave',function(){
      d.classList.remove('drag-above','drag-below');
      if(dropTargetId===it.id){dropTargetId=null;dropPos=null;}
    });
    d.addEventListener('drop',function(ev){
      ev.preventDefault();ev.stopPropagation();
      d.classList.remove('drag-above','drag-below');
      if(!dragId||dragId===it.id)return;
      var srcId=dragId;dragId=null;
      var src=allItems.find(function(x){return x.id===srcId});
      if(src&&src.status===it.status){
        var col=allItems.filter(function(x){return x.status===it.status}).slice()
          .sort(function(a,b){return (a.sort_position||999999)-(b.sort_position||999999)});
        col=col.filter(function(x){return x.id!==srcId});
        var ti=col.findIndex(function(x){return x.id===it.id});
        col.splice(dropPos==='before'?ti:ti+1,0,src);
        api('POST','/api/v1/board/reorder',{ids:col.map(function(x){return x.id;})})
          .then(function(){dropTargetId=null;loadBoard();})
          .catch(function(e){alert('Reorder failed: '+e.message);});
      } else if(src&&it.status==='queued'){
        openQcfg(srcId,'queued');
      } else if(src&&it.status==='in-progress'){
        api('PATCH','/api/v1/board/'+srcId,{status:'in-progress'})
          .then(function(){dropTargetId=null;loadBoard();})
          .catch(function(e){
            if(e.status===409&&e.data&&e.data.error==='at_capacity'){
              capPendingItemId=srcId;
              ge('cap-msg').textContent='All '+e.data.max_concurrent+' concurrent slots are in use ('+e.data.current+' items in progress). Queue as ASAP to run when a slot opens?';
              dlg('cap-dlg');
            } else {
              alert('Move failed: '+e.message);
            }
          });
      } else if(src){
        api('PATCH','/api/v1/board/'+srcId,{status:it.status})
          .then(function(){dropTargetId=null;loadBoard();})
          .catch(function(e){alert('Move failed: '+e.message);});
      }
    });
    d.onclick=(function(item,el){return function(){if(!dragging&&token)openBoardCtx(item,el)}})(it,d);
    return d;
  }

  function renderBoard(){
    var buckets={backlog:[],queued:[],'in-progress':[],blocked:[],done:[],errored:[]};
    allItems.forEach(function(it){(buckets[it.status]||(buckets[it.status]=[])).push(it)});
    colCfg.forEach(function(c){
      var body=ge(c.hdr),cnt=ge(c.cnt),list=buckets[c.status]||[];
      cnt.textContent=list.length;
      body.innerHTML='';
      if(!list.length){body.innerHTML='<div class="nil">No items</div>';return;}
      // Done/Errored columns: sort by last_result_at ASC when no explicit positions set
      if((c.status==='done'||c.status==='errored')&&list.every(function(x){return !x.sort_position})){
        list=list.slice().sort(function(a,b){
          var ta=a.last_result_at?new Date(a.last_result_at).getTime():Infinity;
          var tb=b.last_result_at?new Date(b.last_result_at).getTime():Infinity;
          return ta-tb;
        });
      }
      // Queued column: ASAP first (by queued_at), then run_at/run_after (by queued_at)
      if(c.status==='queued'){
        list=list.slice().sort(function(a,b){
          var aAsap=(a.queue_mode==='asap'||!a.queue_mode),bAsap=(b.queue_mode==='asap'||!b.queue_mode);
          if(aAsap&&!bAsap)return -1;
          if(!aAsap&&bAsap)return 1;
          var ta=a.queued_at?new Date(a.queued_at).getTime():new Date(a.created_at).getTime();
          var tb=b.queued_at?new Date(b.queued_at).getTime():new Date(b.created_at).getTime();
          return ta-tb;
        });
      }
      list.forEach(function(it){body.appendChild(makeCard(it));});
    });
    markSelectedCard();
  }

  function loadBoard(){
    api('GET','/api/v1/board',null)
      .then(function(items){
        allItems=items||[];
        if(boardCtxItem){
          var fresh=allItems.find(function(x){return x.id===boardCtxItem.id});
          if(fresh){var prev=boardCtxItem.status;boardCtxItem=fresh;updateBoardCtxMeta(fresh);if(fresh.status!==prev)loadBoardCtx();}
        }
        renderBoard();renderRoster();
      })
      .catch(function(){});
  }

  // ── Queue config dialog ──────────────────────────────────────────────────────
  function qcfgFillAfterSel(itemId,selectedId){
    var sel=ge('qcfg-after-item');
    sel.innerHTML='<option value="">Select predecessor item…</option>';
    // Only show items that are in-progress or queued (active candidates).
    allItems.filter(function(x){
      return x.id!==itemId&&(x.status==='in-progress'||x.status==='queued');
    }).forEach(function(x){
      var o=document.createElement('option');
      o.value=x.id;
      o.textContent=x.title.substring(0,60)+(x.title.length>60?'…':'')+' ['+x.status+']';
      if(x.id===selectedId)o.selected=true;
      sel.appendChild(o);
    });
  }

  function openQcfg(itemId,targetStatus){
    qcfgPendingItemId=itemId;
    qcfgPendingStatus=targetStatus||'queued';
    ge('qcfg-err').style.display='none';
    document.querySelectorAll('input[name="qcfg-mode"]').forEach(function(r){r.checked=r.value==='asap';});
    ge('qcfg-run-when-wrap').style.display='none';
    ge('qcfg-run-at-when').value='';
    ge('qcfg-require-success').checked=true;
    qcfgFillAfterSel(itemId,'');
    dlg('qcfg-dlg');
  }

  function editQueueConfig(itemId){
    var it=allItems.find(function(x){return x.id===itemId});
    if(!it)return;
    qcfgPendingItemId=itemId;
    qcfgPendingStatus='queued';
    ge('qcfg-title').textContent='Edit Queue Configuration';
    ge('qcfg-err').style.display='none';
    // run_at is a legacy mode; treat it as run_when with a time and no predecessor.
    var mode=it.queue_mode||'asap';
    if(mode==='run_at')mode='run_when';
    document.querySelectorAll('input[name="qcfg-mode"]').forEach(function(r){r.checked=r.value===mode;});
    ge('qcfg-run-when-wrap').style.display=mode==='run_when'?'block':'none';
    var pad=function(n){return n<10?'0'+n:String(n)};
    if(it.queue_run_at){
      var d=new Date(it.queue_run_at);
      var iso=d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+'T'+pad(d.getHours())+':'+pad(d.getMinutes());
      ge('qcfg-run-at-when').value=iso;
    }
    ge('qcfg-require-success').checked=it.queue_require_success!==false;
    qcfgFillAfterSel(itemId,it.queue_after_item_id||'');
    dlg('qcfg-dlg');
  }

  function onQcfgMode(){
    var mode=document.querySelector('input[name="qcfg-mode"]:checked');
    if(!mode)return;
    ge('qcfg-run-when-wrap').style.display=mode.value==='run_when'?'block':'none';
  }

  function cancelQcfg(){
    cls('qcfg-dlg');
    ge('qcfg-title').textContent='Queue Configuration';
    // If item was being dragged into queued, move it back to backlog
    if(qcfgPendingItemId){
      var it=allItems.find(function(x){return x.id===qcfgPendingItemId});
      if(it&&it.status!=='queued'){
        qcfgPendingItemId=null;qcfgPendingStatus=null;
        return; // already not queued
      }
      if(it&&it.status==='queued'){
        api('PATCH','/api/v1/board/'+qcfgPendingItemId,{status:'backlog'})
          .then(function(){loadBoard()}).catch(function(){loadBoard()});
      }
    }
    qcfgPendingItemId=null;qcfgPendingStatus=null;
  }

  function confirmQcfg(){
    if(!qcfgPendingItemId)return;
    var modeEl=document.querySelector('input[name="qcfg-mode"]:checked');
    var mode=modeEl?modeEl.value:'asap';
    var body={status:'queued',queue_mode:mode};
    var errEl=ge('qcfg-err');
    errEl.style.display='none';
    if(mode==='run_when'){
      var rawWhen=ge('qcfg-run-at-when').value;
      var predWhen=ge('qcfg-after-item').value;
      if(!rawWhen&&!predWhen){errEl.textContent='Please set a time, a predecessor item, or both.';errEl.style.display='block';return;}
      if(rawWhen)body.queue_run_at=new Date(rawWhen).toISOString();
      if(predWhen){body.queue_after_item_id=predWhen;body.queue_require_success=ge('qcfg-require-success').checked;}
    }
    var id=qcfgPendingItemId;
    qcfgPendingItemId=null;qcfgPendingStatus=null;
    cls('qcfg-dlg');
    ge('qcfg-title').textContent='Queue Configuration';
    api('PATCH','/api/v1/board/'+id,body)
      .then(function(){loadBoard()})
      .catch(function(e){alert('Queue failed: '+e.message);loadBoard();});
  }

  function queueAsASAP(){
    cls('cap-dlg');
    if(!capPendingItemId)return;
    var id=capPendingItemId;capPendingItemId=null;
    api('PATCH','/api/v1/board/'+id,{status:'queued',queue_mode:'asap'})
      .then(function(){loadBoard()})
      .catch(function(e){alert('Queue failed: '+e.message);loadBoard();});
  }

  // ── Roster ───────────────────────────────────────────────────────────────────
  var botTypeSummaries={
    'orchestrator':'Control plane &amp; task routing',
    'tech-lead':'Full-stack dev, code review &amp; planning',
    'developer':'Code implementation &amp; debugging',
    'implementer':'Code implementation &amp; debugging',
    'reviewer':'Code review &amp; quality feedback',
    'qa':'Testing, bug analysis &amp; quality gates',
    'devops':'CI/CD, infra &amp; deployments',
    'designer':'UI/UX design &amp; prototyping',
    'analyst':'Data analysis &amp; reporting',
    'security':'Security review &amp; threat modelling',
    'architect':'System design &amp; architecture planning',
    'maintainer':'Maintenance, patches &amp; dependency updates',
  };
  var botTypeDetails={
    'orchestrator':['Routes tasks to the right specialist bot','Manages the Kanban board and work queue','Monitors team health and bot availability','Handles triage, scheduling, and escalation'],
    'tech-lead':['Breaks down features into delegatable tasks','Coordinates multi-bot workflows end-to-end','Reviews completed work before integration','Acts as the main point of contact for complex dev tasks'],
    'developer':['Implements features from specs or task descriptions','Fixes bugs and investigates regressions','Writes unit, integration, and E2E tests','Refactors and optimises existing code'],
    'implementer':['Implements features from specs or task descriptions','Fixes bugs and investigates regressions','Writes unit, integration, and E2E tests','Refactors and optimises existing code'],
    'reviewer':['Reviews pull requests for correctness and style','Identifies bugs, security issues, and design flaws','Provides actionable feedback with severity levels','Validates that implementation meets acceptance criteria'],
    'qa':['Designs and executes test plans','Analyses bug reports and reproduction steps','Validates acceptance criteria against implementations','Enforces quality gates before release'],
    'devops':['Manages CI/CD pipelines and build systems','Provisions and configures infrastructure','Handles deployments, rollbacks, and runbooks','Monitors system health and on-call tooling'],
    'designer':['Creates UI mockups and interactive prototypes','Defines and maintains design systems','Reviews frontend implementations for fidelity','Conducts usability assessments and A/B tests'],
    'analyst':['Queries and analyses data from multiple sources','Produces reports, dashboards, and summaries','Identifies patterns, anomalies, and insights','Supports data-driven product and engineering decisions'],
    'security':['Reviews code and configs for vulnerabilities','Performs threat modelling and risk assessment','Audits third-party dependencies and supply chain','Enforces security policies and compliance controls'],
    'architect':['Designs system architecture and component boundaries','Documents architectural decisions and trade-offs','Reviews proposed changes for systemic impact','Guides the team on scalability and reliability patterns'],
    'maintainer':['Applies patches and resolves dependency updates','Monitors for deprecations and breaking changes','Keeps CI green and build tooling up to date','Handles housekeeping tasks that keep the codebase healthy'],
  };
  function botSkillSummary(botType){
    if(!botType)return '';
    var t=botType.toLowerCase();
    for(var k in botTypeSummaries){if(t===k||t.indexOf(k)>=0)return botTypeSummaries[k];}
    return '';
  }
  function openBotInfo(botName,botType){
    var t=(botType||'').toLowerCase();
    var details=null;
    for(var k in botTypeDetails){if(t===k||t.indexOf(k)>=0){details=botTypeDetails[k];break;}}
    var summary=botSkillSummary(botType);
    ge('bot-info-title').textContent=botName+(botType&&botType!==botName?' ('+botType+')':'');
    var html='';
    if(summary)html+='<div style="color:#e2e8f0;margin-bottom:.75rem">'+esc(summary)+'</div>';
    if(details&&details.length){
      html+='<ul style="margin:0;padding-left:1.25rem;color:#94a3b8">';
      details.forEach(function(s){html+='<li style="margin-bottom:.2rem">'+esc(s)+'</li>';});
      html+='</ul>';
    }else{
      html+='<div style="color:#475569;font-style:italic">No additional details available for this bot type.</div>';
    }
    ge('bot-info-body').innerHTML=html;
    dlg('bot-info-dlg');
  }

  function renderRoster(){
    var el=ge('roster');
    if(!allBots.length){el.innerHTML='<div class="nil" style="padding:1rem;font-size:.7rem">No bots registered</div>';return}
    var active={};
    allItems.forEach(function(it){if(it.active_task_id&&it.assigned_to)active[it.assigned_to]=(active[it.assigned_to]||0)+1});
    el.innerHTML='';
    // Sort: orchestrator pinned first, remaining alphabetically by name.
    var sorted=allBots.slice().sort(function(a,b){
      var ao=a.bot_type==='orchestrator',bo=b.bot_type==='orchestrator';
      if(ao&&!bo)return -1;
      if(!ao&&bo)return 1;
      return a.name<b.name?-1:a.name>b.name?1:0;
    });
    var hasOrch=sorted.length>0&&sorted[0].bot_type==='orchestrator';
    sorted.forEach(function(b,idx){
      var on=b.status==='active',n=active[b.name]||0;
      var pct=Math.min(n/6*100,100);
      var fc=n===0?'bfill-none':n<=2?'bfill-lo':n<=5?'bfill-md':'bfill-hi';
      var typeLabel=b.bot_type&&b.bot_type!==b.name?esc(b.bot_type):'';
      var skillSummary=botSkillSummary(b.bot_type);
      var infoBtn=b.bot_type?'<span class="binfo-link" title="What can this bot do?" onclick="event.stopPropagation();openBotInfo(\''+esc(b.name)+'\',\''+esc(b.bot_type||'')+'\')">&#x24D8;</span>':'';
      var c=document.createElement('div');c.className='bcard';
      c.innerHTML=
        '<div class="brow">'+
          '<div class="bdot '+(on?'bdot-on':'bdot-off')+'"></div>'+
          '<div class="bname">'+esc(b.name)+'</div>'+
          (typeLabel?'<div class="btype-pill">'+typeLabel+'</div>':'')+
          (n?'<div class="bbadge">'+n+'</div>':'')+
          (on&&token?'<button class="btn btn-ghost btn-sm" onclick="openAssignTask(\''+esc(b.name)+'\')">&#x26A1; Task</button>':'')+
        '</div>'+
        '<div class="bmeta" style="display:flex;align-items:center;gap:.2rem">'+
          '<span style="flex:1;overflow:hidden;text-overflow:ellipsis">'+esc(skillSummary||(b.bot_type||''))+(on?' &bull; online':' &bull; inactive')+'</span>'+
          infoBtn+
        '</div>'+
        (on?'<div class="bbar"><div class="bfill '+fc+'" style="width:'+pct+'%"></div></div>':'');
      el.appendChild(c);
      // Separator between orchestrator and the rest.
      if(hasOrch&&idx===0&&sorted.length>1){
        var sep=document.createElement('div');
        sep.style.cssText='height:1px;background:#1a2744;margin:.25rem 0';
        el.appendChild(sep);
      }
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
    // Default to tech-lead bot if one exists, otherwise first active bot
    var techLead=allBots.find(function(b){return b.bot_type&&b.bot_type.toLowerCase().indexOf('tech-lead')>=0&&b.status==='active'});
    if(!techLead)techLead=allBots.find(function(b){return b.bot_type&&b.bot_type.toLowerCase().indexOf('tech-lead')>=0});
    if(techLead){sel.value=techLead.name;}
    var wsel=ge('ni-workdir-sel');
    wsel.innerHTML='<option value="">None</option>';
    (allWorkDirs||[]).forEach(function(d){var o=document.createElement('option');o.value=d;o.textContent=d;wsel.appendChild(o);});
    ge('ni-workdir-txt').style.display='none';
    ge('ni-workdir-txt').value='';
    ge('ni-err').style.display='none';
    dlg('ni-dlg');
  }

  function doCreateItem(addAnother){
    var title=ge('ni-title').value.trim(),desc=ge('ni-desc').value.trim(),bot=ge('ni-bot').value,e=ge('ni-err');
    e.style.display='none';
    if(!title){e.textContent='Title is required';e.style.display='block';return}
    var root=ge('ni-workdir-sel').value,sub=ge('ni-workdir-txt').value.trim().replace(/^@/,'').trimEnd();
    var workdir=root?(sub?root+'/'+sub:root):'';
    var body={title:title,description:desc,assigned_to:bot};
    if(workdir)body.work_dir=workdir;
    api('POST','/api/v1/board',body)
      .then(function(){
        ge('ni-title').value='';ge('ni-desc').value='';ge('ni-workdir-sel').value='';ge('ni-workdir-txt').value='';ge('ni-workdir-txt').style.display='none';
        loadBoard();
        if(addAnother){ge('ni-title').focus()}else{cls('ni-dlg')}
      })
      .catch(function(err){e.textContent=err.message||'Failed';e.style.display='block'});
  }

  // ── Plugins & Registry ──────────────────────────────────────────────────────
  var registryData=[];

  function loadRegistries(){
    api('GET','/api/v1/registries',null).then(function(regs){
      regs=(regs||[]).slice().sort(function(a,b){return a.name.localeCompare(b.name);});
      registryData=regs;
      var sel=ge('registry-select');
      if(!sel)return;
      var prev=sel.value;
      sel.innerHTML='<option value="">-- select registry --</option>';
      regs.forEach(function(r){
        var opt=document.createElement('option');
        opt.value=r.name;opt.textContent=r.name+(r.trusted?' (trusted)':'');
        sel.appendChild(opt);
      });
      // Restore previous selection, fall back to first registry, or clear.
      if(prev&&regs.some(function(r){return r.name===prev;})){
        sel.value=prev;
      } else if(regs.length){
        sel.value=regs[0].name;
      }
      updateRegistryDeleteBtn();
      if(sel.value){loadRegistryIndex();}
      else{ge('registry-cards').innerHTML='<div class="empty-state">Select a registry above</div>';}
    }).catch(function(){});
  }

  function onRegistryChange(){
    updateRegistryDeleteBtn();
    loadRegistryIndex();
  }

  function updateRegistryDeleteBtn(){
    var btn=ge('registry-delete-btn');
    if(!btn)return;
    btn.style.display=ge('registry-select').value?'inline-flex':'none';
  }

  function refreshRegistry(){
    var name=ge('registry-select').value;
    if(name){
      // Force-fetch the index from the remote, then reload the list.
      ge('registry-cards').innerHTML='<div class="empty-state">Refreshing…</div>';
      api('GET','/api/v1/registries/'+encodeURIComponent(name)+'/index?force=true',null)
        .then(function(idx){renderRegistryCards(idx.plugins||[]);})
        .catch(function(e){ge('registry-cards').innerHTML='<div class="empty-state">Error: '+e.message+'</div>';});
    } else {
      loadRegistries();
    }
  }

  function deleteRegistry(){
    var name=ge('registry-select').value;
    if(!name)return;
    if(!confirm('Remove registry "'+name+'"?'))return;
    api('DELETE','/api/v1/registries/'+encodeURIComponent(name),null)
      .then(function(){
        ge('registry-select').value='';
        updateRegistryDeleteBtn();
        ge('registry-cards').innerHTML='<div class="empty-state">Select a registry above</div>';
        loadRegistries();
      }).catch(function(e){alert('Delete failed: '+e.message);});
  }

  function loadRegistryIndex(){
    var name=ge('registry-select').value;
    if(!name){ge('registry-cards').innerHTML='<div class="empty-state">Select a registry above</div>';return;}
    ge('registry-cards').innerHTML='<div class="empty-state">Loading…</div>';
    api('GET','/api/v1/registries/'+encodeURIComponent(name)+'/index',null).then(function(idx){
      renderRegistryCards(idx.plugins||[]);
    }).catch(function(e){ge('registry-cards').innerHTML='<div class="empty-state">Error: '+e.message+'</div>';});
  }

  var installedNames={};
  var installedById={};

  function setInstallButtonState(btn,installed){
    if(installed){
      btn.className='btn btn-secondary btn-sm';
      btn.disabled=true;
      btn.textContent='✓ Installed';
      btn.removeAttribute('onclick');
    }else{
      var name=btn.getAttribute('data-install-name');
      var ver=btn.getAttribute('data-install-version');
      var reg=ge('registry-select').value;
      btn.className='btn btn-primary btn-sm';
      btn.disabled=false;
      btn.textContent='Install';
      btn.setAttribute('onclick','installPlugin(\''+esc(reg)+'\',\''+esc(name)+'\',\''+esc(ver)+'\')');
    }
  }

  function syncInstallButtons(){
    document.querySelectorAll('[data-install-name]').forEach(function(btn){
      setInstallButtonState(btn,!!installedNames[btn.getAttribute('data-install-name')]);
    });
  }

  function renderRegistryCards(plugins){
    var q=(ge('registry-search').value||'').toLowerCase();
    var reg=ge('registry-select').value;
    var filtered=plugins.filter(function(p){return !q||p.name.toLowerCase().includes(q)||(p.description||'').toLowerCase().includes(q);});
    if(!filtered.length){ge('registry-cards').innerHTML='<div class="empty-state">No plugins found</div>';return;}
    ge('registry-cards').innerHTML=filtered.map(function(p){
      var tags=p.tags&&p.tags.length
        ?'<div class="reg-card-tags">'+p.tags.map(function(t){return '<span class="reg-tag">'+esc(t)+'</span>';}).join('')+'</div>'
        :'';
      var instd=!!installedNames[p.name];
      var installBtn=instd
        ?'<button class="btn btn-secondary btn-sm" disabled data-install-name="'+esc(p.name)+'" data-install-version="'+esc(p.latest_version)+'">✓ Installed</button>'
        :'<button class="btn btn-primary btn-sm" data-install-name="'+esc(p.name)+'" data-install-version="'+esc(p.latest_version)+'" onclick="installPlugin(\''+esc(reg)+'\',\''+esc(p.name)+'\',\''+esc(p.latest_version)+'\')">Install</button>';
      return '<div class="reg-card">'+
        '<div class="reg-card-name">'+esc(p.name)+'</div>'+
        '<div class="reg-card-version">'+esc(p.latest_version)+'</div>'+
        '<div class="reg-card-desc">'+esc(p.description)+'</div>'+
        tags+
        '<div>'+installBtn+'</div>'+
        '</div>';
    }).join('');
  }

  function filterRegistryCards(){
    var name=ge('registry-select').value;
    if(name)loadRegistryIndex();
  }

  function showAddRegistryModal(){
    var m=ge('add-registry-modal');if(m)m.style.display='flex';
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
      installedNames[p.name]=true;
      installedById[p.id]=p.name;
      var btn=document.querySelector('[data-install-name="'+name+'"]');
      if(btn)setInstallButtonState(btn,true);
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
    installedNames={};installedById={};
    (plugins||[]).forEach(function(p){
      if(p.status!=='rejected'){installedNames[p.name]=true;installedById[p.id]=p.name;}
    });
    syncInstallButtons();
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
    api(method,path,null).then(function(){
      if(action==='remove'){
        var name=installedById[id];
        if(name){
          delete installedNames[name];
          delete installedById[id];
          var btn=document.querySelector('[data-install-name="'+name+'"]');
          if(btn)setInstallButtonState(btn,false);
        }
      }
      loadPlugins();
    }).catch(function(e){alert('Error: '+e.message);});
  }

  function showPluginDetail(id){
    api('GET','/api/v1/plugins/'+id,null).then(function(p){
      ge('plugin-detail-name').textContent=p.name+' '+p.version;
      var m=p.manifest||{};
      var tools=(m.provides&&m.provides.tools)||[];
      var perms=m.permissions||{};
      var noPerms=!perms.filesystem&&!(perms.network||[]).length&&!(perms.env_vars||[]).length;
      ge('plugin-detail-content').innerHTML=
        '<p><b>Author:</b> '+esc(m.author||'—')+'</p>'+
        '<p><b>Description:</b> '+esc(m.description||'—')+'</p>'+
        '<p><b>Status:</b> <span style="color:'+statusColor(p.status)+'">'+esc(p.status)+'</span></p>'+
        '<p><b>Registry:</b> '+esc(p.registry||'—')+'</p>'+
        '<p><b>Entrypoint:</b> <span style="font-family:monospace;font-size:.78rem">'+esc(m.entrypoint||'—')+'</span></p>'+
        (m.homepage?'<p><b>Homepage:</b> <a href="'+esc(m.homepage)+'" target="_blank">'+esc(m.homepage)+'</a></p>':'')+
        '<hr/>'+
        '<div class="pd-section-title">Tools provided</div>'+
        (tools.length
          ?'<ul>'+tools.map(function(t){return '<li><span style="color:#e2e8f0;font-weight:500">'+esc(t.Name||t.name)+'</span><span style="color:#475569"> — </span>'+esc(t.Description||t.description||'')+'</li>';}).join('')+'</ul>'
          :'<p style="color:#475569">None</p>')+
        '<hr/>'+
        '<div class="pd-section-title">Permissions</div>'+
        (noPerms
          ?'<p style="color:#475569">None declared</p>'
          :'<ul>'+
            (perms.filesystem?'<li>Filesystem access</li>':'')+
            ((perms.network||[]).map(function(n){return '<li>Network: <span style="font-family:monospace;font-size:.78rem">'+esc(n)+'</span></li>';}).join(''))+
            ((perms.env_vars||[]).map(function(ev){return '<li>Env var: <span style="font-family:monospace;font-size:.78rem">'+esc(ev)+'</span></li>';}).join(''))+
            '</ul>')+
        (m.checksums?'<hr/><div class="pd-section-title">Checksums</div><pre>'+esc(JSON.stringify(m.checksums,null,2))+'</pre>':'');
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
    var list=allTasksList;
    if(currentTaskFilter==='immediate')list=list.filter(function(t){return!t.scheduled_at});
    if(currentTaskFilter==='scheduled')list=list.filter(function(t){return!!t.scheduled_at});
    var bot=(ge('tf-bot')||{}).value||'';
    if(bot)list=list.filter(function(t){return t.bot_name===bot});
    var txt=((ge('tf-text')||{}).value||'').toLowerCase().trim();
    if(txt)list=list.filter(function(t){
      var title=(t.title||'').toLowerCase();
      var instr=(t.instruction||'').toLowerCase();
      return title.indexOf(txt)>=0||instr.indexOf(txt)>=0;
    });
    return list;
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
      var tDir=t.work_dir?leafDir(t.work_dir):'';
      return'<tr data-tid="'+esc(t.id)+'"><td style="width:1.5rem;text-align:center"><input type="checkbox" data-cid="'+esc(t.id)+'" onclick="event.stopPropagation();updateTaskDeleteBtn()"/></td><td>'+esc(t.bot_name)+'</td><td>'+label+'</td><td style="font-size:.68rem;color:#475569;white-space:nowrap">'+(tDir?esc(tDir):'&#x2014;')+'</td><td><span class="pill '+sc+'">'+esc(t.status)+'</span></td><td>'+(t.scheduled_at?ago(t.scheduled_at):'&#x2014;')+'</td><td>'+ago(t.created_at)+'</td></tr>';
    }).join('');
    el.innerHTML='<table style="min-width:600px"><thead><tr><th style="width:1.5rem"><input type="checkbox" id="task-chk-all" onclick="toggleAllTaskChecks(this)"/></th><th>Bot</th><th>Title / Instruction</th><th>Dir</th><th>Status</th><th>Sched</th><th>Created</th></tr></thead><tbody>'+rows+'</tbody></table>';
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
      .then(function(tasks){
        allTasksList=tasks||[];
        // Rebuild bot filter dropdown from current task list
        var sel=ge('tf-bot');
        if(sel){
          var prev=sel.value;
          sel.innerHTML='<option value="">All bots</option>';
          var bots={};
          allTasksList.forEach(function(t){if(t.bot_name)bots[t.bot_name]=1});
          Object.keys(bots).sort().forEach(function(bn){
            var o=document.createElement('option');o.value=bn;o.textContent=bn;sel.appendChild(o);
          });
          if(prev&&bots[prev])sel.value=prev;
        }
        renderTaskList();
      })
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

  function doDispatchTask(addAnother){
    var botName=ge('at-bot').textContent;
    var title=ge('at-title').value.trim();
    var instruction=ge('at-instr').value.trim();
    var isNow=ge('at-now').checked;
    var schedVal=ge('at-sched').value;
    var e=ge('at-err');
    e.style.display='none';
    if(!instruction){e.textContent='Instruction is required';e.style.display='block';return}
    var root=ge('at-workdir-sel').value,sub=ge('at-workdir-txt').value.trim().replace(/^@/,'').trimEnd();
    var workDir=root?(sub?root+'/'+sub:root):'';
    var body={instruction:instruction};
    if(title){body.title=title}
    if(!isNow&&schedVal){body.scheduled_at=new Date(schedVal).toISOString()}
    if(workDir){body.work_dir=workDir}
    api('POST','/api/v1/bots/'+botName+'/tasks',body)
      .then(function(){
        ge('at-title').value='';ge('at-instr').value='';ge('at-now').checked=true;ge('at-sched-wrap').style.display='none';ge('at-sched').value='';ge('at-workdir-sel').value='';ge('at-workdir-txt').value='';ge('at-workdir-txt').style.display='none';
        setTaskFilter('immediate');loadTasks();
        if(addAnother){ge('at-instr').focus()}else{cls('at-dlg');tab('tasks')}
      })
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
        if(!Object.keys(pendingTasks).length){stopFastPoll();}
        else{Object.keys(pendingTasks).forEach(function(tid){showThinking(pendingTasks[tid],tid);});}
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
    bubble.innerHTML=renderMd(msg.content||'');
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

  // ── Bash mode (chat-input only) ──────────────────────────────────────────────
  function isBashMode(el){
    return el&&el.id==='chat-input'&&el.value.length>0&&el.value[0]==='!';
  }

  function checkBashMode(el){
    if(!el)return;
    var pfx=ge('chat-bash-prefix');
    if(isBashMode(el)){
      el.classList.add('bash-mode');
      el.placeholder='bash command… (Enter to run, @ for file)';
      if(pfx)pfx.style.display='block';
    } else {
      el.classList.remove('bash-mode');
      el.placeholder='Message… (Enter to send, Shift+Enter for newline)';
      if(pfx)pfx.style.display='none';
    }
  }

  var bashOverlay=null;
  function showBashResult(cmd,output,isErr,loading){
    if(!bashOverlay){
      bashOverlay=document.createElement('div');
      bashOverlay.className='bash-overlay';
      bashOverlay.innerHTML='<div class="bash-box"><div class="bash-hdr"><span class="bash-cmd"></span><button class="bash-close" title="Close">✕</button></div><div class="bash-body"></div></div>';
      bashOverlay.querySelector('.bash-close').addEventListener('click',closeBashResult);
      bashOverlay.addEventListener('mousedown',function(e){if(e.target===bashOverlay)closeBashResult();});
      document.body.appendChild(bashOverlay);
    }
    var cmdEl=bashOverlay.querySelector('.bash-cmd');
    var bodyEl=bashOverlay.querySelector('.bash-body');
    cmdEl.textContent='$ '+cmd;
    if(loading){
      bodyEl.className='bash-body bash-loading';
      bodyEl.textContent='Running…';
    } else {
      bodyEl.className='bash-body'+(isErr?' bash-err':'');
      bodyEl.textContent=output||'(no output)';
    }
    bashOverlay.style.display='flex';
  }

  function closeBashResult(){
    if(bashOverlay)bashOverlay.style.display='none';
  }

  function runBashCmd(el){
    var raw=el.value;
    // Strip leading '!' and trim.
    var cmd=raw.slice(1).trim();
    if(!cmd)return;
    var workDir='';
    // Try to get the current work dir from the board context item.
    if(boardCtxItem&&boardCtxItem.work_dir)workDir=boardCtxItem.work_dir;
    showBashResult(cmd,'',false,true);
    api('POST','/api/v1/shell',{command:cmd,work_dir:workDir})
      .then(function(res){
        showBashResult(cmd,res.output,res.is_error,false);
      })
      .catch(function(e){
        showBashResult(cmd,'Error: '+e.message,true,false);
      });
    el.value='';
    checkBashMode(el);
  }

  function chatSendOrBash(){
    var el=ge('chat-input');
    if(!el)return;
    if(isBashMode(el)){runBashCmd(el);}
    else{sendChat();}
  }

  // ── Command & file mention popup ─────────────────────────────────────────────
  var mpMode=null,mpEl=null,mpPos=0,mpText='',mpItems=[],mpIdx=0,mpWorkDir='',mpPop=null;
  var pluginCmds=[];

  // Whether the browser supports the Popover API — used to put mpPop into the
  // top layer independently of any showModal() dialog, giving truly
  // viewport-relative position:fixed coordinates.
  var mpPopoverOK='showPopover' in HTMLElement.prototype;

  function mpInit(){
    if(mpPop)return;
    mpPop=document.createElement('div');
    mpPop.className='mp-pop';
    if(mpPopoverOK)mpPop.setAttribute('popover','manual');
    document.body.appendChild(mpPop);
    document.addEventListener('mousedown',function(e){if(mpPop&&!mpPop.contains(e.target))mpClose();});
  }

  function mpHide(){
    if(!mpPop)return;
    if(mpPopoverOK)try{mpPop.hidePopover();}catch(e){}
    else mpPop.style.display='none';
  }

  function mpVisible(){
    if(!mpPop)return false;
    if(mpPopoverOK)return mpPop.matches(':popover-open');
    return mpPop.style.display!=='none';
  }

  function mpClose(){
    mpMode=null;mpEl=null;mpItems=[];mpIdx=0;mpHide();
  }

  function mpPosition(){
    if(!mpEl||!mpPop)return;
    var r=mpEl.getBoundingClientRect();
    // Reset any UA popover defaults that could interfere with explicit positioning.
    mpPop.style.right='auto';mpPop.style.margin='0';
    var left=Math.max(4,r.left);
    if(left+320>window.innerWidth)left=Math.max(4,window.innerWidth-324);
    mpPop.style.left=left+'px';
    if(window.innerHeight-r.bottom<240&&r.top>240){
      mpPop.style.top='auto';mpPop.style.bottom=(window.innerHeight-r.top+2)+'px';
    } else {
      mpPop.style.bottom='auto';mpPop.style.top=(r.bottom+2)+'px';
    }
    if(mpPopoverOK)try{mpPop.showPopover();}catch(e){mpPop.style.display='block';}
    else mpPop.style.display='block';
  }

  function mpRender(items){
    mpItems=items;mpIdx=0;
    if(!items.length){mpHide();return;}
    if(!mpPop)mpInit();
    mpPop.innerHTML='';
    items.forEach(function(it,i){
      var el=document.createElement('div');
      el.className='mp-item'+(i===0?' mp-sel':'');
      el.innerHTML='<span class="mp-name'+(it.isDir?' mp-dir':'')+'">'+esc(it.label)+(it.isDir?'/':'')+'</span>'
        +(it.desc?'<span class="mp-desc">'+esc(it.desc)+'</span>':'');
      el.addEventListener('mousedown',function(e){e.preventDefault();mpPick(i);});
      mpPop.appendChild(el);
    });
    mpPosition();
  }

  function mpMove(d){
    if(!mpItems.length)return;
    var els=mpPop.querySelectorAll('.mp-item');
    if(els[mpIdx])els[mpIdx].classList.remove('mp-sel');
    mpIdx=(mpIdx+d+mpItems.length)%mpItems.length;
    if(els[mpIdx]){els[mpIdx].classList.add('mp-sel');els[mpIdx].scrollIntoView({block:'nearest'});}
  }

  function mpPick(idx){
    if(idx==null)idx=mpIdx;
    var it=mpItems[idx];if(!it||!mpEl)return;
    var v=mpEl.value;
    // Path-field mode (mpPos===-1): the whole field is the path — replace it entirely.
    if(mpMode==='file'&&mpPos===-1){
      var newVal=it.path+(it.isDir?'/':'');
      mpEl.value=newVal;mpText=newVal;
      mpEl.selectionStart=mpEl.selectionEnd=newVal.length;
      if(it.isDir){mpLoadFile(newVal);mpEl.focus();}else{mpClose();mpEl.focus();}
      return;
    }
    var after=v.slice(mpPos+1+mpText.length);
    var before=v.slice(0,mpPos);
    if(mpMode==='file'&&it.isDir){
      var newText=it.path+'/';
      mpText=newText;
      // Insert without @ — adjust mpPos so mpPos+1 still points to start of path text.
      mpEl.value=before+newText+after;
      mpPos=before.length-1;
      mpEl.selectionStart=mpEl.selectionEnd=before.length+newText.length;
      mpLoadFile(newText);mpEl.focus();
      return;
    }
    if(mpMode==='file'){
      // File selected — insert bare path, no @ prefix.
      mpEl.value=before+it.path+' '+after;
      mpEl.selectionStart=mpEl.selectionEnd=before.length+it.path.length+1;
      mpClose();mpEl.focus();
      return;
    }
    var insert='/'+it.path;
    mpEl.value=before+insert+' '+after;
    mpEl.selectionStart=mpEl.selectionEnd=before.length+insert.length+1;
    mpClose();mpEl.focus();
  }

  function mpLoadCmd(text){
    var q=text.toLowerCase();
    var list=q?pluginCmds.filter(function(c){
      return c.name.toLowerCase().indexOf(q)>=0||c.desc.toLowerCase().indexOf(q)>=0;
    }):pluginCmds;
    mpRender(list.slice(0,24).map(function(c){return{label:c.name,path:c.name,desc:c.desc,isDir:false};}));
  }

  function mpLoadFile(text){
    if(!mpWorkDir){mpClose();return;}
    var lastSlash=text.lastIndexOf('/');
    var dirPart=lastSlash>=0?text.slice(0,lastSlash+1):'';
    var filterPart=lastSlash>=0?text.slice(lastSlash+1):text;
    var reqDir=mpWorkDir.replace(/\/+$/,'')+(dirPart?'/'+dirPart.replace(/^\/+/,'').replace(/\/+$/,''):'');
    api('GET','/api/v1/files?dir='+encodeURIComponent(reqDir),null)
      .then(function(entries){
        if(!entries)entries=[];
        var q=filterPart.toLowerCase();
        if(q)entries=entries.filter(function(e){return e.name.toLowerCase().indexOf(q)===0;});
        entries.sort(function(a,b){return(b.is_dir-a.is_dir)||a.name.localeCompare(b.name);});
        mpRender(entries.slice(0,20).map(function(e){
          return{label:e.name,path:dirPart+e.name,desc:e.is_dir?'directory':'',isDir:e.is_dir};
        }));
      })
      .catch(function(){mpClose();});
  }

  function mpOnInput(el,workDirFn){
    var v=el.value;
    var cursor=typeof el.selectionStart==='number'?el.selectionStart:v.length;
    if(mpMode&&mpEl===el){
      if(cursor<=mpPos){mpClose();return;}
      mpText=v.slice(mpPos+1,cursor);
      if(mpMode==='cmd')mpLoadCmd(mpText);else mpLoadFile(mpText);
      return;
    }
    if(cursor<1)return;
    var ch=v[cursor-1];
    if(ch!=='/'&&ch!=='@')return;
    var prevCh=cursor>1?v[cursor-2]:'';
    if(prevCh&&!/[\s\n\r\t]/.test(prevCh))return;
    if(ch==='/'){
      if(!pluginCmds.length)return;
      if(isBashMode(el))return; // bash mode: no command picker
      mpMode='cmd';mpEl=el;mpPos=cursor-1;mpText='';mpIdx=0;mpWorkDir='';
      if(!mpPop)mpInit();mpLoadCmd('');
    } else {
      var wd=typeof workDirFn==='function'?workDirFn():'';
      if(!wd)return;
      mpMode='file';mpEl=el;mpPos=cursor-1;mpText='';mpIdx=0;mpWorkDir=wd;
      if(!mpPop)mpInit();mpLoadFile('');
    }
  }

  function mpOnKeydown(e){
    if(!mpMode||!mpPop||!mpVisible()||mpEl!==e.target)return;
    if(e.key==='ArrowDown'){e.preventDefault();mpMove(1);}
    else if(e.key==='ArrowUp'){e.preventDefault();mpMove(-1);}
    else if(e.key==='Tab'){
      e.preventDefault();e.stopImmediatePropagation();
      if(mpItems.length)mpPick();else mpClose();
    } else if(e.key==='Enter'&&mpItems.length){
      e.preventDefault();e.stopImmediatePropagation();mpPick();
    } else if(e.key==='Escape'){e.preventDefault();mpClose();}
  }

  function attachMention(el,workDirFn){
    if(!el)return;
    el.addEventListener('input',function(){mpOnInput(el,workDirFn);});
    el.addEventListener('keydown',mpOnKeydown);
    el.addEventListener('blur',function(){setTimeout(mpClose,160);});
  }

  // attachPathMention wires a dedicated path-input field so that typing any
  // character triggers file/directory completion (the whole field value is the
  // path). The '/' key never opens the skill-command popup in these fields.
  function attachPathMention(el,workDirFn){
    if(!el)return;
    el.addEventListener('input',function(){
      var v=el.value;
      var cursor=typeof el.selectionStart==='number'?el.selectionStart:v.length;
      var wd=typeof workDirFn==='function'?workDirFn():'';
      if(!wd){if(mpEl===el)mpClose();return;}
      if(mpMode==='file'&&mpEl===el){
        if(cursor===0){mpClose();return;}
        mpText=v.slice(0,cursor);mpLoadFile(mpText);return;
      }
      if(!v.length)return;
      mpMode='file';mpEl=el;mpPos=-1;mpText=v.slice(0,cursor);mpIdx=0;mpWorkDir=wd;
      if(!mpPop)mpInit();mpLoadFile(mpText);
    });
    el.addEventListener('keydown',mpOnKeydown);
    el.addEventListener('blur',function(){setTimeout(mpClose,160);});
  }

  function loadPluginCmds(){
    var cmds=[];
    var p1=api('GET','/api/v1/plugins',null).then(function(plugins){
      (plugins||[]).forEach(function(p){
        if(p.status==='active'&&p.manifest&&p.manifest.provides&&p.manifest.provides.tools){
          p.manifest.provides.tools.forEach(function(t){
            cmds.push({name:p.name+':'+t.name,desc:t.description||''});
          });
        }
      });
    }).catch(function(){});
    var p2=token?api('GET','/api/v1/skills',null).then(function(skills){
      (skills||[]).forEach(function(s){
        if(s.status==='active'){cmds.push({name:s.name,desc:s.summary||''});}
      });
    }).catch(function(){}):Promise.resolve();
    Promise.all([p1,p2]).then(function(){pluginCmds=cmds;});
  }

  // ── Draggable dialogs ─────────────────────────────────────────────────────────
  function makeDraggable(dlgEl){
    var handle=dlgEl.querySelector('h2');
    if(!handle)return;
    var startX=0,startY=0,startL=0,startT=0,active=false;
    handle.addEventListener('mousedown',function(e){
      if(e.button!==0)return;
      var r=dlgEl.getBoundingClientRect();
      dlgEl.style.transform='none';
      dlgEl.style.left=r.left+'px';
      dlgEl.style.top=r.top+'px';
      startX=e.clientX;startY=e.clientY;startL=r.left;startT=r.top;
      active=true;e.preventDefault();
    });
    document.addEventListener('mousemove',function(e){
      if(!active)return;
      var l=startL+(e.clientX-startX);
      var t=startT+(e.clientY-startY);
      // clamp so dialog can't be dragged fully off-screen
      l=Math.max(0,Math.min(l,window.innerWidth-dlgEl.offsetWidth));
      t=Math.max(0,Math.min(t,window.innerHeight-dlgEl.offsetHeight));
      dlgEl.style.left=l+'px';dlgEl.style.top=t+'px';
    });
    document.addEventListener('mouseup',function(){active=false;});
    dlgEl.addEventListener('close',function(){
      dlgEl.style.left='';dlgEl.style.top='';dlgEl.style.transform='';
    });
  }

  // Enter sends; Shift+Enter inserts newline.
  document.addEventListener('DOMContentLoaded',function(){
    mpInit();loadPluginCmds();
    document.querySelectorAll('dialog').forEach(makeDraggable);
    function firstSelOpt(sel){
      if(!sel)return '';
      for(var i=0;i<sel.options.length;i++){if(sel.options[i].value)return sel.options[i].value;}
      return '';
    }
    function getAtWorkDir(){
      var sel=ge('at-workdir-sel'),txt=ge('at-workdir-txt');
      var base=(sel&&sel.value)||firstSelOpt(sel);
      var sub=txt&&txt.style.display!=='none'?txt.value.trim().replace(/^@/,'').trimEnd():'';
      return base?(base+(sub?'/'+sub.replace(/^\/+/,''):'')):'';
    }
    function getNiWorkDir(){
      var sel=ge('ni-workdir-sel'),txt=ge('ni-workdir-txt');
      var base=(sel&&sel.value)||firstSelOpt(sel);
      var sub=txt&&txt.style.display!=='none'?txt.value.trim().replace(/^@/,'').trimEnd():'';
      return base?(base+(sub?'/'+sub.replace(/^\/+/,''):'')):'';
    }
    function getNiRootDir(){var sel=ge('ni-workdir-sel');return(sel&&sel.value)||firstSelOpt(sel);}
    function getAtRootDir(){var sel=ge('at-workdir-sel');return(sel&&sel.value)||firstSelOpt(sel);}
    var bCtxDir=function(){
      if(boardCtxItem&&boardCtxItem.work_dir)return boardCtxItem.work_dir;
      return allWorkDirs&&allWorkDirs.length?allWorkDirs[0]:'';
    };
    // Register mention keydown handlers FIRST so mpOnKeydown fires before send handlers.
    // Note: bctx-desc is dynamically recreated by loadBoardCtx() so it is attached there instead.
    attachMention(ge('chat-input'),bCtxDir);
    attachMention(ge('board-ctx-ask-input'),bCtxDir);
    var tCtxDir=function(){
      if(taskCtxTask&&taskCtxTask.work_dir)return taskCtxTask.work_dir;
      return allWorkDirs&&allWorkDirs.length?allWorkDirs[0]:'';
    };
    attachMention(ge('task-ctx-ask-input'),tCtxDir);
    attachMention(ge('ni-title'),getNiWorkDir);
    attachMention(ge('ni-desc'),getNiWorkDir);
    attachPathMention(ge('ni-workdir-txt'),getNiRootDir);
    attachMention(ge('at-title'),getAtWorkDir);
    attachMention(ge('at-instr'),getAtWorkDir);
    attachPathMention(ge('at-workdir-txt'),getAtRootDir);
    // Ask input: Enter triggers boardAsk only when mention popup is not active.
    var askIn=ge('board-ctx-ask-input');
    if(askIn)askIn.addEventListener('keydown',function(e){
      if(e.key==='Enter'&&(!mpMode||!mpItems.length))boardAsk();
    });
    var tAskIn=ge('task-ctx-ask-input');
    if(tAskIn)tAskIn.addEventListener('keydown',function(e){
      if(e.key==='Enter'&&(!mpMode||!mpItems.length))taskAsk();
    });
    // Chat: Enter sends or runs bash; Shift+Enter newline.
    var ta=ge('chat-input');
    if(ta){
      ta.addEventListener('input',function(){checkBashMode(ta);});
      ta.addEventListener('keydown',function(e){
        if(e.key==='Escape'&&isBashMode(ta)){ta.value='';checkBashMode(ta);return;}
        if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();chatSendOrBash();}
      });
    }
  });

  // ── Board context panel ───────────────────────────────────────────────────────
  var bctxH=280;
  function updateBoardCtxMeta(item){
    var meta=ge('board-ctx-meta');
    if(!meta||!item)return;
    var html='<span class="bctx-badge">'+esc(item.status)+'</span>'+
      '<span class="bctx-badge">'+esc(item.assigned_to||'unassigned')+'</span>';
    if(item.work_dir)html+='<span class="bctx-badge">&#x1F4C1; '+esc(leafDir(item.work_dir))+'</span>';
    meta.innerHTML=html;
  }

  function openBoardCtx(item,cardEl){
    boardCtxItem=item;
    boardCtxThread=null;
    boardCtxActivity=null;
    var panel=ge('board-ctx');
    panel.style.display='flex';
    panel.style.height=bctxH+'px';
    ge('board-ctx-title').textContent=item.title;
    updateBoardCtxMeta(item);
    markSelectedCard();
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
  function stopTaskOutputPoll(){if(taskOutputPollTimer){clearInterval(taskOutputPollTimer);taskOutputPollTimer=null;}}
  function stopElapsedTimer(){if(elapsedTimer){clearInterval(elapsedTimer);elapsedTimer=null;}}
  function startElapsedTimer(since){
    stopElapsedTimer();
    elapsedTimer=setInterval(function(){
      var el=ge('rt-elapsed');if(!el){stopElapsedTimer();return;}
      el.textContent=fmtDur(Date.now()-new Date(since).getTime());
    },1000);
  }
  function fmtDur(ms){
    var s=Math.floor(ms/1000);
    if(s<60)return s+'s';
    var m=Math.floor(s/60);return m+'m '+(s%60)+'s';
  }
  function runTimeFooter(dispAt,compAt){
    if(!dispAt)return '';
    if(compAt){
      var dur=new Date(compAt).getTime()-new Date(dispAt).getTime();
      return '<div class="rt-footer"><span class="rt-lbl">Run time</span><span class="rt-val">'+fmtDur(dur)+'</span></div>';
    }
    var init=fmtDur(Date.now()-new Date(dispAt).getTime());
    setTimeout(function(){startElapsedTimer(dispAt);},0);
    return '<div class="rt-footer"><span class="rt-lbl">Running for</span><span class="rt-val"><span id="rt-elapsed">'+init+'</span></span></div>';
  }

  function closeBoardCtx(){
    stopOutputPoll();
    stopAskPoll();
    stopElapsedTimer();
    var panel=ge('board-ctx');
    panel.style.height='0';
    panel.style.display='none';
    boardCtxItem=null;
    boardCtxActivity=null;
    markSelectedCard();
  }

  function bctxTab(name){
    if(name!=='output')stopOutputPoll();
    if(name!=='ask'){stopAskPoll();boardAskPending=false;}
    stopElapsedTimer();
    boardCtxTab=name;
    ['detail','output','ask','files'].forEach(function(t){
      var el=ge('bctx-t-'+t);if(el)el.classList.toggle('on',t===name);
    });
    ge('board-ctx-ask').style.display=name==='ask'?'flex':'none';
    var meta=ge('board-ctx-meta');
    if(meta)meta.style.display=(name!=='detail'&&boardCtxItem)?'inline-flex':'none';
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

      var isQueued=it.status==='queued';
      var isErrored=it.status==='errored';
      var queueRow='';
      if(isQueued){
        var qmode=it.queue_mode||'asap';
        var qdetail='';
        if(qmode==='run_at'&&it.queue_run_at)qdetail=' at '+esc(fmtQueueAt(it.queue_run_at));
        else if(qmode==='run_after'&&it.queue_after_item_id){
          var pred=allItems.find(function(x){return x.id===it.queue_after_item_id});
          qdetail=' after "'+esc(pred?pred.title.substring(0,40):it.queue_after_item_id)+'"'+(it.queue_require_success?' (success required)':'');
        }
        queueRow='<div class="ctx-row"><span class="ctx-lbl">Queue</span><span class="ctx-val">'+
          esc(qmode)+esc(qdetail)+
          (token?' <button class="btn btn-ghost btn-sm" onclick="editQueueConfig(\''+esc(it.id)+'\')">&#x270E; Edit</button>':'')+
          '</span></div>';
      }
      var bActTask=boardCtxActivity&&boardCtxActivity.task;
      body.innerHTML=
        '<div class="ctx-row"><span class="ctx-lbl">Status</span><span class="ctx-val">'+esc(it.status)+'</span></div>'+
        queueRow+
        botRow+
        descRow+
        '<div class="ctx-row"><span class="ctx-lbl">Work dir</span><span class="ctx-val" style="display:flex;align-items:center;gap:.5rem">'+
          workdirInput+
        '</span></div>'+
        (it.work_dir?'<div style="font-size:.7rem;color:#475569;padding:.1rem 0 .4rem 0">Attachments will be written to this directory.</div>':'')+
        '<div class="ctx-row"><span class="ctx-lbl">Attachments</span><span class="ctx-val"><a href="#" onclick="bctxTab(\'files\');return false" style="color:#60a5fa">'+attCount+' file'+(attCount!==1?'s':'')+'</a></span></div>'+
        '<div class="ctx-row"><span class="ctx-lbl">Created</span><span class="ctx-val">'+ago(it.created_at)+'</span></div>'+
        (it.active_task_id&&it.status==='in-progress'?'<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>':'')+
        (it.last_result&&(isDone||isErrored)?'<div class="ctx-row"><span class="ctx-lbl">Last result</span><span class="ctx-val" style="font-size:.78rem;white-space:pre-wrap;word-break:break-word;max-height:8rem;overflow:auto">'+esc(it.last_result.substring(0,500))+'</span></div>':'')+
        (canEdit?'<div style="margin-top:.75rem"><button id="bctx-save-btn" class="btn btn-primary btn-sm" disabled onclick="saveBoardAllEdits()" style="opacity:.4;cursor:not-allowed">Save changes</button></div>':'')+
        ((isDone||isErrored)&&token?'<div style="margin-top:.5rem"><button class="btn btn-danger btn-sm" onclick="deleteBoardItem()">Delete item</button></div>':'')+
        (bActTask?runTimeFooter(bActTask.dispatched_at,bActTask.completed_at):'');
      // If we don't have activity timing yet, fetch it once to populate the footer.
      if(!boardCtxActivity){
        var _detailItemID=it.id;
        api('GET','/api/v1/board/'+_detailItemID+'/activity',null)
          .then(function(resp){boardCtxActivity=resp;if(boardCtxTab==='detail'&&boardCtxItem&&boardCtxItem.id===_detailItemID)loadBoardCtx();})
          .catch(function(){});
      }
      // Re-attach mention pickers to dynamically-created editable fields.
      if(canEdit){
        var _bwd=function(){return boardCtxItem&&boardCtxItem.work_dir?boardCtxItem.work_dir:'';};
        attachMention(ge('bctx-desc'),_bwd);
        attachMention(ge('bctx-workdir-sub'),_bwd);
      }
    } else if(boardCtxTab==='output'){
      body.innerHTML='<div style="color:#475569">Loading&#x2026;</div>';
      function renderOutput(resp){
        boardCtxActivity=resp;
        stopElapsedTimer();
        var html='';
        var output=(resp.task&&resp.task.output)||'';
        if(!output&&resp.item&&resp.item.last_result)output=resp.item.last_result;
        if(output){
          html+='<pre id="output-pre" class="viewer-pre" style="max-height:340px;overflow-y:auto;white-space:pre-wrap;word-break:break-all">'+esc(output)+'</pre>';
        } else if(resp.task&&(resp.task.status==='dispatched'||resp.task.status==='pending'||resp.task.status==='running')){
          html+='<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>';
        } else {
          html+='<div style="color:#475569">No output yet</div>';
        }
        if(resp.task){
          html+='<div class="ctx-row" style="margin-top:.75rem"><span class="ctx-lbl">Task status</span><span class="ctx-val">'+esc(resp.task.status)+'</span></div>';
          html+=runTimeFooter(resp.task.dispatched_at,resp.task.completed_at);
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
      var askItemID=boardCtxItem.id;
      api('GET','/api/v1/board/'+askItemID+'/messages',null)
        .then(renderBoardAskMsgs)
        .catch(function(){body.innerHTML='<div style="color:#e74c3c">Failed to load messages</div>';});
      if(!askPollTimer&&boardCtxItem&&boardCtxItem.active_task_id){
        askPollTimer=setInterval(function(){
          if(boardCtxTab!=='ask'||!boardCtxItem||boardCtxItem.id!==askItemID){stopAskPoll();return;}
          api('GET','/api/v1/board/'+askItemID+'/messages',null)
            .then(renderBoardAskMsgs)
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
      var fAt=boardCtxActivity&&boardCtxActivity.task;
      body.innerHTML=html+(fAt?runTimeFooter(fAt.dispatched_at,fAt.completed_at):'');
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

  function deleteCardItem(id,title,status){
    if(!token)return;
    if(status==='in-progress'){
      if(!confirm('"'+title+'" is currently running.\nDeleting it will stop the bot mid-task. Continue?'))return;
    }
    api('DELETE','/api/v1/board/'+id,null)
      .then(function(){
        if(boardCtxItem&&boardCtxItem.id===id)closeBoardCtx();
        loadBoard();
      })
      .catch(function(e){alert('Delete failed: '+e.message)});
  }

  function boardAsk(){
    if(!boardCtxItem||!token)return;
    var content=ge('board-ctx-ask-input').value.trim();
    if(!content)return;
    ge('board-ctx-ask-input').value='';
    api('POST','/api/v1/board/'+boardCtxItem.id+'/ask',{content:content})
      .then(function(){
        boardAskPending=true;
        // Reload messages immediately after sending (shows the user's outbound message).
        loadBoardCtx();
        // Also start a poll so the thinking dots update when the bot replies.
        if(!askPollTimer){
          var pollID=boardCtxItem.id;
          askPollTimer=setInterval(function(){
            if(boardCtxTab!=='ask'||!boardCtxItem||boardCtxItem.id!==pollID){stopAskPoll();return;}
            api('GET','/api/v1/board/'+pollID+'/messages',null)
              .then(renderBoardAskMsgs)
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

  var taskAskPending=false, taskAskPollTimer=null;

  function tctxTab(name){
    taskCtxActiveTab=name;
    ['detail','ask','output'].forEach(function(t){var el=ge('tctx-t-'+t);if(el)el.classList.toggle('on',t===name)});
    var askRow=ge('task-ctx-ask');
    if(askRow)askRow.style.display=name==='ask'?'flex':'none';
    if(name!=='ask')stopTaskAskPoll();
    if(name!=='output')stopTaskOutputPoll();
    stopElapsedTimer();
    loadTaskCtx();
  }

  function stopTaskAskPoll(){
    if(taskAskPollTimer){clearInterval(taskAskPollTimer);taskAskPollTimer=null;}
  }

  function closeTaskCtx(){
    stopTaskAskPoll();
    stopTaskOutputPoll();
    stopElapsedTimer();
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
      stopElapsedTimer();
      if(t.output){
        body.innerHTML='<div class="ctx-output" style="max-height:none">'+esc(t.output)+'</div>'+runTimeFooter(t.dispatched_at,t.completed_at);
        stopTaskOutputPoll();
      } else if(t.status==='running'){
        body.innerHTML='<div class="ctx-working">&#x2699; Bot is working&#x2026;</div>'+runTimeFooter(t.dispatched_at,null);
        if(!taskOutputPollTimer){
          var _tPollID=t.id;
          taskOutputPollTimer=setInterval(function(){
            if(taskCtxActiveTab!=='output'||!taskCtxTask||taskCtxTask.id!==_tPollID){stopTaskOutputPoll();return;}
            api('GET','/api/v1/tasks/'+_tPollID,null).then(function(fresh){taskCtxTask=fresh;loadTaskCtx();}).catch(function(){});
          },2000);
        }
      } else {
        body.innerHTML='<div style="color:#475569;font-size:.78rem">No output recorded.</div>'+runTimeFooter(t.dispatched_at,t.completed_at);
        stopTaskOutputPoll();
      }
      return;
    }

    if(taskCtxActiveTab==='ask'){loadTaskAsk();return;}

    // ── Details tab ──────────────────────────────────────────────────────────
    var statusClass=t.status==='succeeded'?'ok':t.status==='running'?'run':t.status==='failed'?'fail':'';
    var meta='<div class="tctx-meta">'
      +'<span class="tctx-pill">'+esc(t.bot_name)+'</span>'
      +'<span class="tctx-pill '+statusClass+'">'+esc(t.status)+'</span>'
      +(t.source?'<span class="tctx-pill">'+esc(t.source)+'</span>':'')
      +'<span class="tctx-time">Created '+ago(t.created_at)+'</span>'
      +(t.dispatched_at?'<span class="tctx-time">· Dispatched '+ago(t.dispatched_at)+'</span>':'')
      +(t.completed_at?'<span class="tctx-time">· Completed '+ago(t.completed_at)+'</span>':'')
      +'</div>';
    // Derive a human-readable title and body from the stored fields.
    // For board-dispatched tasks the Title field is empty; extract it from the instruction.
    var dispTitle=t.title||'';
    var dispBody='';
    if(t.instruction){
      if(!dispTitle||t.source==='board'){
        var tm=/\nTitle: ([^\n]+)/.exec(t.instruction);
        if(tm)dispTitle=tm[1].trim();
      }
      if(t.source==='board'){
        var dm=/\nDescription: ([\s\S]+?)\n\nItem ID:/.exec(t.instruction);
        if(dm)dispBody=dm[1].trim();
      } else {
        // Operator / chat tasks: the instruction IS what was entered.
        dispBody=t.instruction;
      }
    }
    var titleBlock='<div class="tctx-section-title">'+esc(dispTitle||'(no title)')+'</div>';
    var bodyBlock=dispBody?'<div class="tctx-body-text">'+esc(dispBody)+'</div>':'';
    var promptBlock=t.instruction
      ?'<a class="tctx-prompt-link" onclick="toggleTctxPrompt(this)">Review prompt &#x25BE;</a>'
       +'<div class="tctx-prompt" style="display:none">'+esc(t.instruction)+'</div>'
      :'';
    body.innerHTML=meta+titleBlock+bodyBlock+promptBlock+runTimeFooter(t.dispatched_at,t.completed_at);
  }

  function toggleTctxPrompt(link){
    var p=link.nextElementSibling;
    var open=p.style.display!=='none';
    p.style.display=open?'none':'block';
    link.innerHTML=open?'Review prompt &#x25BE;':'Hide prompt &#x25B4;';
  }

  function loadTaskAsk(){
    if(!taskCtxTask)return;
    var body=ge('task-ctx-body');
    api('GET','/api/v1/tasks/'+taskCtxTask.id+'/messages',null)
      .then(function(msgs){
        if(!msgs||!msgs.length){
          body.innerHTML='<div style="color:#475569;font-size:.75rem">No messages yet. Ask the assigned bot a question below.</div>';
        } else {
          var html='<div class="ask-thread">';
          msgs.forEach(function(m){
            var isUser=m.direction==='outbound';
            html+='<div class="ask-msg '+(isUser?'ask-msg-user':'ask-msg-bot')+'">'
              +'<div class="ask-msg-label">'+(isUser?'You':esc(m.bot_name||'Bot'))+'</div>'
              +'<div class="ask-msg-body">'+renderMd(m.content)+'</div>'
              +'</div>';
          });
          html+='</div>';
          body.innerHTML=html;
          if(taskAskPending&&msgs[msgs.length-1].direction==='inbound')taskAskPending=false;
        }
        if(taskAskPending){
          var td=document.createElement('div');
          td.style.cssText='display:flex;align-items:center;gap:.5rem;padding:.35rem .65rem';
          td.innerHTML='<span style="font-size:.65rem;color:#64748b">thinking&#x2026;</span><div class="chat-thinking"><span></span><span></span><span></span></div>';
          body.appendChild(td);
        }
        var _tAsk=taskCtxTask;
        var ftrHtml=_tAsk?runTimeFooter(_tAsk.dispatched_at,_tAsk.completed_at):'';
        if(ftrHtml){var ftrEl=document.createElement('div');ftrEl.innerHTML=ftrHtml;if(ftrEl.firstElementChild)body.appendChild(ftrEl.firstElementChild);}
        body.scrollTop=body.scrollHeight;
      }).catch(function(){});
  }

  function taskAsk(){
    if(!taskCtxTask||!token)return;
    var inp=ge('task-ctx-ask-input');
    var content=inp?inp.value.trim():'';
    if(!content)return;
    inp.value='';
    api('POST','/api/v1/tasks/'+taskCtxTask.id+'/ask',{content:content})
      .then(function(){
        taskAskPending=true;
        loadTaskAsk();
        if(!taskAskPollTimer){
          var taskID=taskCtxTask.id;
          taskAskPollTimer=setInterval(function(){
            if(taskCtxActiveTab!=='ask'||!taskCtxTask||taskCtxTask.id!==taskID){stopTaskAskPoll();return;}
            api('GET','/api/v1/tasks/'+taskID+'/messages',null)
              .then(function(msgs){
                loadTaskAsk();
                if(!taskAskPending)stopTaskAskPoll();
              }).catch(function(){});
          },2000);
        }
      }).catch(function(e){alert('Failed: '+e.message)});
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
    loadBoard(); loadTeam(); loadThreads(); loadPluginCmds();
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

  // ── Plugin panel drag-to-resize ───────────────────────────────────────────
  (function(){
    var dragBody=null,startY=0,startH=0,activeHandle=null;
    document.addEventListener('mousedown',function(e){
      var h=e.target.closest('.plugin-resize-handle');
      if(!h)return;
      // Walk back through siblings to find the preceding .plugin-panel
      var prev=h.previousElementSibling;
      while(prev&&!prev.classList.contains('plugin-panel')){prev=prev.previousElementSibling;}
      if(!prev)return;
      dragBody=prev.querySelector('.plugin-panel-body');
      if(!dragBody)return;
      activeHandle=h;
      startY=e.clientY;
      startH=dragBody.offsetHeight;
      h.classList.add('prd-active');
      document.body.style.userSelect='none';
      e.preventDefault();
    });
    document.addEventListener('mousemove',function(e){
      if(!dragBody)return;
      dragBody.style.height=Math.max(60,startH+(e.clientY-startY))+'px';
    });
    document.addEventListener('mouseup',function(){
      if(!dragBody)return;
      if(activeHandle)activeHandle.classList.remove('prd-active');
      document.body.style.userSelect='';
      dragBody=null;activeHandle=null;
    });
  })();
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
		if strings.Contains(err.Error(), "no download URL available") {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
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
