package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	body := map[string]string{"bot_id": botName}
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
