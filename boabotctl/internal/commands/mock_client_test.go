package commands_test

import (
	"context"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// mockClient implements client.OrchestratorClient for tests.
type mockClient struct {
	// Login
	loginResp domain.LoginResponse
	loginErr  error

	// Board
	boardListResp   []domain.WorkItem
	boardListErr    error
	boardGetResp    domain.WorkItem
	boardGetErr     error
	boardCreateResp domain.WorkItem
	boardCreateErr  error
	boardUpdateResp domain.WorkItem
	boardUpdateErr  error
	boardAssignResp domain.WorkItem
	boardAssignErr  error
	boardCloseErr   error

	// Team
	teamListResp   []domain.BotEntry
	teamListErr    error
	teamGetResp    domain.BotEntry
	teamGetErr     error
	teamHealthResp domain.TeamHealth
	teamHealthErr  error

	// Skills
	skillsListResp   []domain.Skill
	skillsListErr    error
	skillsApproveErr error
	skillsRejectErr  error
	skillsRevokeErr  error

	// User
	userListResp    []domain.User
	userListErr     error
	userCreateResp  domain.User
	userCreateErr   error
	userRemoveErr   error
	userDisableErr  error
	userSetPwdErr   error
	userSetRoleErr  error

	// Profile
	profileGetResp     domain.User
	profileGetErr      error
	profileSetNameErr  error
	profileSetPwdErr   error

	// DLQ
	dlqListResp   []domain.DLQItem
	dlqListErr    error
	dlqRetryErr   error
	dlqDiscardErr error

	// recorded calls
	lastBoardCreateReq domain.CreateWorkItemRequest
	lastBoardUpdateID  string
	lastBoardUpdateReq domain.UpdateWorkItemRequest
	lastBoardAssignID  string
	lastBoardAssignBot string
	lastBoardCloseID   string
	lastTeamGetName    string
	lastUserCreateReq  domain.CreateUserRequest
	lastUserRemoveUser string
	lastUserDisableUser string
	lastUserSetPwdUser  string
	lastUserSetPwdPw    string
	lastUserSetRoleUser string
	lastUserSetRoleRole string
	lastProfileSetName  string
	lastProfileOldPwd   string
	lastProfileNewPwd   string
	lastDLQRetryID      string
	lastDLQDiscardID    string
}

func (m *mockClient) Login(_ context.Context, _, _ string) (domain.LoginResponse, error) {
	return m.loginResp, m.loginErr
}
func (m *mockClient) BoardList(_ context.Context) ([]domain.WorkItem, error) {
	return m.boardListResp, m.boardListErr
}
func (m *mockClient) BoardGet(_ context.Context, _ string) (domain.WorkItem, error) {
	return m.boardGetResp, m.boardGetErr
}
func (m *mockClient) BoardCreate(_ context.Context, req domain.CreateWorkItemRequest) (domain.WorkItem, error) {
	m.lastBoardCreateReq = req
	return m.boardCreateResp, m.boardCreateErr
}
func (m *mockClient) BoardUpdate(_ context.Context, id string, req domain.UpdateWorkItemRequest) (domain.WorkItem, error) {
	m.lastBoardUpdateID = id
	m.lastBoardUpdateReq = req
	return m.boardUpdateResp, m.boardUpdateErr
}
func (m *mockClient) BoardAssign(_ context.Context, id, bot string) (domain.WorkItem, error) {
	m.lastBoardAssignID = id
	m.lastBoardAssignBot = bot
	return m.boardAssignResp, m.boardAssignErr
}
func (m *mockClient) BoardClose(_ context.Context, id string) error {
	m.lastBoardCloseID = id
	return m.boardCloseErr
}
func (m *mockClient) TeamList(_ context.Context) ([]domain.BotEntry, error) {
	return m.teamListResp, m.teamListErr
}
func (m *mockClient) TeamGet(_ context.Context, name string) (domain.BotEntry, error) {
	m.lastTeamGetName = name
	return m.teamGetResp, m.teamGetErr
}
func (m *mockClient) TeamHealth(_ context.Context) (domain.TeamHealth, error) {
	return m.teamHealthResp, m.teamHealthErr
}
func (m *mockClient) SkillsList(_ context.Context, _ string) ([]domain.Skill, error) {
	return m.skillsListResp, m.skillsListErr
}
func (m *mockClient) SkillsApprove(_ context.Context, _ string) error {
	return m.skillsApproveErr
}
func (m *mockClient) SkillsReject(_ context.Context, _ string) error {
	return m.skillsRejectErr
}
func (m *mockClient) SkillsRevoke(_ context.Context, _ string) error {
	return m.skillsRevokeErr
}
func (m *mockClient) UserList(_ context.Context) ([]domain.User, error) {
	return m.userListResp, m.userListErr
}
func (m *mockClient) UserCreate(_ context.Context, req domain.CreateUserRequest) (domain.User, error) {
	m.lastUserCreateReq = req
	return m.userCreateResp, m.userCreateErr
}
func (m *mockClient) UserRemove(_ context.Context, username string) error {
	m.lastUserRemoveUser = username
	return m.userRemoveErr
}
func (m *mockClient) UserDisable(_ context.Context, username string) error {
	m.lastUserDisableUser = username
	return m.userDisableErr
}
func (m *mockClient) UserSetPassword(_ context.Context, username, pw string) error {
	m.lastUserSetPwdUser = username
	m.lastUserSetPwdPw = pw
	return m.userSetPwdErr
}
func (m *mockClient) UserSetRole(_ context.Context, username, role string) error {
	m.lastUserSetRoleUser = username
	m.lastUserSetRoleRole = role
	return m.userSetRoleErr
}
func (m *mockClient) ProfileGet(_ context.Context) (domain.User, error) {
	return m.profileGetResp, m.profileGetErr
}
func (m *mockClient) ProfileSetName(_ context.Context, name string) error {
	m.lastProfileSetName = name
	return m.profileSetNameErr
}
func (m *mockClient) ProfileSetPassword(_ context.Context, old, newPwd string) error {
	m.lastProfileOldPwd = old
	m.lastProfileNewPwd = newPwd
	return m.profileSetPwdErr
}
func (m *mockClient) DLQList(_ context.Context) ([]domain.DLQItem, error) {
	return m.dlqListResp, m.dlqListErr
}
func (m *mockClient) DLQRetry(_ context.Context, id string) error {
	m.lastDLQRetryID = id
	return m.dlqRetryErr
}
func (m *mockClient) DLQDiscard(_ context.Context, id string) error {
	m.lastDLQDiscardID = id
	return m.dlqDiscardErr
}

// errClient is a simple client that returns an error for all calls.
type errClient struct{ err error }

func newErrClient(msg string) *errClient { return &errClient{err: fmt.Errorf(msg)} }
func (e *errClient) Login(_ context.Context, _, _ string) (domain.LoginResponse, error) {
	return domain.LoginResponse{}, e.err
}
func (e *errClient) BoardList(_ context.Context) ([]domain.WorkItem, error)  { return nil, e.err }
func (e *errClient) BoardGet(_ context.Context, _ string) (domain.WorkItem, error) {
	return domain.WorkItem{}, e.err
}
func (e *errClient) BoardCreate(_ context.Context, _ domain.CreateWorkItemRequest) (domain.WorkItem, error) {
	return domain.WorkItem{}, e.err
}
func (e *errClient) BoardUpdate(_ context.Context, _ string, _ domain.UpdateWorkItemRequest) (domain.WorkItem, error) {
	return domain.WorkItem{}, e.err
}
func (e *errClient) BoardAssign(_ context.Context, _, _ string) (domain.WorkItem, error) {
	return domain.WorkItem{}, e.err
}
func (e *errClient) BoardClose(_ context.Context, _ string) error             { return e.err }
func (e *errClient) TeamList(_ context.Context) ([]domain.BotEntry, error)    { return nil, e.err }
func (e *errClient) TeamGet(_ context.Context, _ string) (domain.BotEntry, error) {
	return domain.BotEntry{}, e.err
}
func (e *errClient) TeamHealth(_ context.Context) (domain.TeamHealth, error) {
	return domain.TeamHealth{}, e.err
}
func (e *errClient) SkillsList(_ context.Context, _ string) ([]domain.Skill, error) {
	return nil, e.err
}
func (e *errClient) SkillsApprove(_ context.Context, _ string) error { return e.err }
func (e *errClient) SkillsReject(_ context.Context, _ string) error  { return e.err }
func (e *errClient) SkillsRevoke(_ context.Context, _ string) error  { return e.err }
func (e *errClient) UserList(_ context.Context) ([]domain.User, error) { return nil, e.err }
func (e *errClient) UserCreate(_ context.Context, _ domain.CreateUserRequest) (domain.User, error) {
	return domain.User{}, e.err
}
func (e *errClient) UserRemove(_ context.Context, _ string) error   { return e.err }
func (e *errClient) UserDisable(_ context.Context, _ string) error  { return e.err }
func (e *errClient) UserSetPassword(_ context.Context, _, _ string) error { return e.err }
func (e *errClient) UserSetRole(_ context.Context, _, _ string) error { return e.err }
func (e *errClient) ProfileGet(_ context.Context) (domain.User, error) {
	return domain.User{}, e.err
}
func (e *errClient) ProfileSetName(_ context.Context, _ string) error        { return e.err }
func (e *errClient) ProfileSetPassword(_ context.Context, _, _ string) error { return e.err }
func (e *errClient) DLQList(_ context.Context) ([]domain.DLQItem, error)     { return nil, e.err }
func (e *errClient) DLQRetry(_ context.Context, _ string) error               { return e.err }
func (e *errClient) DLQDiscard(_ context.Context, _ string) error             { return e.err }
