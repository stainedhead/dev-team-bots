package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestUserList_HappyPath(t *testing.T) {
	mc := &mockClient{
		userListResp: []domain.User{
			{Username: "alice", Role: "admin", Enabled: true},
			{Username: "bob", Role: "user", Enabled: false},
		},
	}
	var out bytes.Buffer
	cmd := commands.NewUserCmd(mc, &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Errorf("missing users in output: %q", got)
	}
}

func TestUserList_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewUserCmd(newErrClient("unauthorized"), &out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserAdd_HappyPath(t *testing.T) {
	mc := &mockClient{
		userCreateResp: domain.User{Username: "charlie", Role: "user", Enabled: true},
	}
	var out bytes.Buffer
	// inject a reader that provides the password for the prompt
	in := strings.NewReader("initial-password\n")
	cmd := commands.NewUserCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"add", "--username", "charlie", "--role", "user"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserCreateReq.Username != "charlie" {
		t.Errorf("expected username 'charlie', got %q", mc.lastUserCreateReq.Username)
	}
	got := out.String()
	if !strings.Contains(got, "charlie") {
		t.Errorf("missing username in output: %q", got)
	}
}

func TestUserAdd_Error(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("password\n")
	cmd := commands.NewUserCmdWithIO(newErrClient("create failed"), &out, in)
	cmd.SetArgs([]string{"add", "--username", "x", "--role", "user"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserRemove_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	// inject "y\n" to confirm removal
	in := strings.NewReader("y\n")
	cmd := commands.NewUserCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"remove", "bob"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserRemoveUser != "bob" {
		t.Errorf("expected remove for 'bob', got %q", mc.lastUserRemoveUser)
	}
}

func TestUserRemove_Cancelled(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	in := strings.NewReader("n\n")
	cmd := commands.NewUserCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"remove", "bob"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserRemoveUser != "" {
		t.Errorf("remove should not have been called when cancelled")
	}
}

func TestUserRemove_Error(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	cmd := commands.NewUserCmdWithIO(newErrClient("remove failed"), &out, in)
	cmd.SetArgs([]string{"remove", "bob"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserDisable_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewUserCmd(mc, &out)
	cmd.SetArgs([]string{"disable", "bob"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserDisableUser != "bob" {
		t.Errorf("expected disable for 'bob', got %q", mc.lastUserDisableUser)
	}
}

func TestUserDisable_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewUserCmd(newErrClient("disable failed"), &out)
	cmd.SetArgs([]string{"disable", "bob"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserSetPwd_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	in := strings.NewReader("new-password\n")
	cmd := commands.NewUserCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"set-pwd", "bob"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserSetPwdUser != "bob" {
		t.Errorf("expected set-pwd for 'bob', got %q", mc.lastUserSetPwdUser)
	}
}

func TestUserSetPwd_Error(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("new-password\n")
	cmd := commands.NewUserCmdWithIO(newErrClient("set-pwd failed"), &out, in)
	cmd.SetArgs([]string{"set-pwd", "bob"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserSetRole_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewUserCmd(mc, &out)
	cmd.SetArgs([]string{"set-role", "bob", "--role", "admin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastUserSetRoleUser != "bob" || mc.lastUserSetRoleRole != "admin" {
		t.Errorf("expected set-role bob admin, got user=%q role=%q", mc.lastUserSetRoleUser, mc.lastUserSetRoleRole)
	}
}

func TestUserSetRole_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewUserCmd(newErrClient("set-role failed"), &out)
	cmd.SetArgs([]string{"set-role", "bob", "--role", "admin"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
