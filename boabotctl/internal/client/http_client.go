package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// APIError represents a structured error response from the orchestrator API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}

// HTTPClient implements OrchestratorClient over HTTP.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	tokenFn    func() string
}

// NewHTTPClient constructs a new HTTPClient.
func NewHTTPClient(baseURL string, tokenFn func() string) *HTTPClient {
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
		tokenFn:    tokenFn,
	}
}

func (c *HTTPClient) url(path string) string {
	return c.baseURL + "/api/v1/" + strings.TrimLeft(path, "/")
}

func (c *HTTPClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if tok := c.tokenFn(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	return c.httpClient.Do(req)
}

// checkResponse reads a response and returns an error for 4xx/5xx status codes.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var errBody struct {
			Error string `json:"error"`
		}
		data, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(data, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("server error: %d", resp.StatusCode)}
}

// decodeJSON decodes a JSON response body into v, checking for errors first.
func decodeJSON(resp *http.Response, v any) error {
	if err := checkResponse(resp); err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (c *HTTPClient) Login(ctx context.Context, username, password string) (domain.LoginResponse, error) {
	body := map[string]string{"username": username, "password": password}
	resp, err := c.do(ctx, http.MethodPost, "auth/login", body)
	if err != nil {
		return domain.LoginResponse{}, err
	}
	var out domain.LoginResponse
	return out, decodeJSON(resp, &out)
}

// ── Board ─────────────────────────────────────────────────────────────────────

func (c *HTTPClient) BoardList(ctx context.Context) ([]domain.WorkItem, error) {
	resp, err := c.do(ctx, http.MethodGet, "board", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardGet(ctx context.Context, id string) (domain.WorkItem, error) {
	resp, err := c.do(ctx, http.MethodGet, "board/"+id, nil)
	if err != nil {
		return domain.WorkItem{}, err
	}
	var out domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardCreate(ctx context.Context, req domain.CreateWorkItemRequest) (domain.WorkItem, error) {
	resp, err := c.do(ctx, http.MethodPost, "board", req)
	if err != nil {
		return domain.WorkItem{}, err
	}
	var out domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardUpdate(ctx context.Context, id string, req domain.UpdateWorkItemRequest) (domain.WorkItem, error) {
	resp, err := c.do(ctx, http.MethodPatch, "board/"+id, req)
	if err != nil {
		return domain.WorkItem{}, err
	}
	var out domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardAssign(ctx context.Context, id, botName string) (domain.WorkItem, error) {
	body := map[string]string{"bot_name": botName}
	resp, err := c.do(ctx, http.MethodPost, "board/"+id+"/assign", body)
	if err != nil {
		return domain.WorkItem{}, err
	}
	var out domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardClose(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "board/"+id+"/close", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) BoardDelete(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "board/"+id, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── Team ──────────────────────────────────────────────────────────────────────

func (c *HTTPClient) TeamList(ctx context.Context) ([]domain.BotEntry, error) {
	resp, err := c.do(ctx, http.MethodGet, "team", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.BotEntry
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) TeamGet(ctx context.Context, name string) (domain.BotEntry, error) {
	resp, err := c.do(ctx, http.MethodGet, "team/"+name, nil)
	if err != nil {
		return domain.BotEntry{}, err
	}
	var out domain.BotEntry
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) TeamHealth(ctx context.Context) (domain.TeamHealth, error) {
	resp, err := c.do(ctx, http.MethodGet, "team/health", nil)
	if err != nil {
		return domain.TeamHealth{}, err
	}
	var out domain.TeamHealth
	return out, decodeJSON(resp, &out)
}

// ── Skills ────────────────────────────────────────────────────────────────────

func (c *HTTPClient) SkillsList(ctx context.Context, botName string) ([]domain.Skill, error) {
	path := "skills"
	if botName != "" {
		path += "?bot=" + botName
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out []domain.Skill
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) SkillsApprove(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "skills/"+id+"/approve", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) SkillsReject(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "skills/"+id+"/reject", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) SkillsRevoke(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "skills/"+id, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── User ──────────────────────────────────────────────────────────────────────

func (c *HTTPClient) UserList(ctx context.Context) ([]domain.User, error) {
	resp, err := c.do(ctx, http.MethodGet, "users", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.User
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) UserCreate(ctx context.Context, req domain.CreateUserRequest) (domain.User, error) {
	resp, err := c.do(ctx, http.MethodPost, "users", req)
	if err != nil {
		return domain.User{}, err
	}
	var out domain.User
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) UserRemove(ctx context.Context, username string) error {
	resp, err := c.do(ctx, http.MethodDelete, "users/"+username, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) UserDisable(ctx context.Context, username string) error {
	resp, err := c.do(ctx, http.MethodPost, "users/"+username+"/disable", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) UserSetPassword(ctx context.Context, username, newPassword string) error {
	body := map[string]string{"password": newPassword}
	resp, err := c.do(ctx, http.MethodPost, "users/"+username+"/password", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) UserSetRole(ctx context.Context, username, role string) error {
	body := map[string]string{"role": role}
	resp, err := c.do(ctx, http.MethodPost, "users/"+username+"/role", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── Profile ───────────────────────────────────────────────────────────────────

func (c *HTTPClient) ProfileGet(ctx context.Context) (domain.User, error) {
	resp, err := c.do(ctx, http.MethodGet, "profile", nil)
	if err != nil {
		return domain.User{}, err
	}
	var out domain.User
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) ProfileSetName(ctx context.Context, displayName string) error {
	body := map[string]string{"display_name": displayName}
	resp, err := c.do(ctx, http.MethodPatch, "profile", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) ProfileSetPassword(ctx context.Context, currentPassword, newPassword string) error {
	body := map[string]string{"old_password": currentPassword, "new_password": newPassword}
	resp, err := c.do(ctx, http.MethodPost, "profile/password", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── DLQ ───────────────────────────────────────────────────────────────────────

func (c *HTTPClient) DLQList(ctx context.Context) ([]domain.DLQItem, error) {
	resp, err := c.do(ctx, http.MethodGet, "dlq", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.DLQItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) DLQRetry(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "dlq/"+id+"/retry", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) DLQDiscard(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "dlq/"+id, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── Memory backup ─────────────────────────────────────────────────────────────

func (c *HTTPClient) MemoryBackup(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, "memory/backup", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) MemoryRestore(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, "memory/restore", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) MemoryStatus(ctx context.Context) (domain.MemoryStatusResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "memory/status", nil)
	if err != nil {
		return domain.MemoryStatusResponse{}, err
	}
	var out domain.MemoryStatusResponse
	return out, decodeJSON(resp, &out)
}

// ── Board extensions ──────────────────────────────────────────────────────────

func (c *HTTPClient) BoardActivity(ctx context.Context, id string) (domain.ActivityResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "board/"+id+"/activity", nil)
	if err != nil {
		return domain.ActivityResponse{}, err
	}
	var out domain.ActivityResponse
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardAsk(ctx context.Context, id, content, threadID string) (domain.ChatMessage, error) {
	body := map[string]string{"content": content, "thread_id": threadID}
	resp, err := c.do(ctx, http.MethodPost, "board/"+id+"/ask", body)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	var out domain.ChatMessage
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardAttachmentUpload(ctx context.Context, id string, paths []string) (domain.WorkItem, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return domain.WorkItem{}, fmt.Errorf("open %s: %w", p, err)
		}
		part, err := mw.CreateFormFile("files", filepath.Base(p))
		if err != nil {
			_ = f.Close()
			return domain.WorkItem{}, fmt.Errorf("create form file: %w", err)
		}
		if _, err = io.Copy(part, f); err != nil {
			_ = f.Close()
			return domain.WorkItem{}, fmt.Errorf("copy %s: %w", p, err)
		}
		_ = f.Close()
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("board/"+id+"/attachments"), &buf)
	if err != nil {
		return domain.WorkItem{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tok := c.tokenFn(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.WorkItem{}, err
	}
	var out domain.WorkItem
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) BoardAttachmentGet(ctx context.Context, id, attID string) ([]byte, string, string, error) {
	resp, err := c.do(ctx, http.MethodGet, "board/"+id+"/attachments/"+attID, nil)
	if err != nil {
		return nil, "", "", err
	}
	if err := checkResponse(resp); err != nil {
		return nil, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("read body: %w", err)
	}
	ct := resp.Header.Get("Content-Type")
	cd := resp.Header.Get("Content-Disposition")
	name := ""
	if cd != "" {
		for _, part := range strings.Split(cd, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "filename=") {
				name = strings.Trim(strings.TrimPrefix(part, "filename="), `"`)
			}
		}
	}
	return data, ct, name, nil
}

func (c *HTTPClient) BoardAttachmentDelete(ctx context.Context, id, attID string) error {
	resp, err := c.do(ctx, http.MethodDelete, "board/"+id+"/attachments/"+attID, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (c *HTTPClient) TaskList(ctx context.Context) ([]domain.DirectTask, error) {
	resp, err := c.do(ctx, http.MethodGet, "tasks", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.DirectTask
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) TaskListByBot(ctx context.Context, botName string) ([]domain.DirectTask, error) {
	resp, err := c.do(ctx, http.MethodGet, "bots/"+botName+"/tasks", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.DirectTask
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) TaskCreate(ctx context.Context, botName, instruction string, scheduledAt *time.Time) (domain.DirectTask, error) {
	body := map[string]any{"instruction": instruction}
	if scheduledAt != nil {
		body["scheduled_at"] = scheduledAt.Format(time.RFC3339)
	}
	resp, err := c.do(ctx, http.MethodPost, "bots/"+botName+"/tasks", body)
	if err != nil {
		return domain.DirectTask{}, err
	}
	var out domain.DirectTask
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) TaskGet(ctx context.Context, id string) (domain.DirectTask, error) {
	resp, err := c.do(ctx, http.MethodGet, "tasks/"+id, nil)
	if err != nil {
		return domain.DirectTask{}, err
	}
	var out domain.DirectTask
	return out, decodeJSON(resp, &out)
}

// ── Chat / Threads ────────────────────────────────────────────────────────────

func (c *HTTPClient) ThreadList(ctx context.Context) ([]domain.ChatThread, error) {
	resp, err := c.do(ctx, http.MethodGet, "threads", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.ChatThread
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) ThreadCreate(ctx context.Context, title string, participants []string) (domain.ChatThread, error) {
	body := map[string]any{"title": title, "participants": participants}
	resp, err := c.do(ctx, http.MethodPost, "threads", body)
	if err != nil {
		return domain.ChatThread{}, err
	}
	var out domain.ChatThread
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) ThreadDelete(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "threads/"+id, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func (c *HTTPClient) ThreadMessages(ctx context.Context, id string) ([]domain.ChatMessage, error) {
	resp, err := c.do(ctx, http.MethodGet, "threads/"+id+"/messages", nil)
	if err != nil {
		return nil, err
	}
	var out []domain.ChatMessage
	return out, decodeJSON(resp, &out)
}

func (c *HTTPClient) ChatSend(ctx context.Context, botName, content, threadID string) (domain.ChatMessage, error) {
	body := map[string]string{"content": content, "thread_id": threadID}
	resp, err := c.do(ctx, http.MethodPost, "chat/"+botName, body)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	var out domain.ChatMessage
	return out, decodeJSON(resp, &out)
}
