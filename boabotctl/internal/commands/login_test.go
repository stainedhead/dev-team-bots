package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestLogin_HappyPath(t *testing.T) {
	mc := &mockClient{
		loginResp: domain.LoginResponse{Token: "jwt-xyz", MustChangePassword: false},
	}
	// stdin: username\npassword\n
	in := strings.NewReader("alice\nsecret\n")
	var out bytes.Buffer

	// Use a temp dir so we don't write to real credentials
	t.Setenv("HOME", t.TempDir())

	cmd := commands.NewLoginCmdWithIO(mc, &out, in)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "alice") {
		t.Errorf("output missing username: %q", got)
	}
}

func TestLogin_MustChangePassword(t *testing.T) {
	mc := &mockClient{
		loginResp: domain.LoginResponse{Token: "jwt-mcp", MustChangePassword: true},
	}
	// stdin: username\npassword\nnew-password\n
	in := strings.NewReader("alice\nsecret\nnewpassword\n")
	var out bytes.Buffer

	t.Setenv("HOME", t.TempDir())

	cmd := commands.NewLoginCmdWithIO(mc, &out, in)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// should have called ProfileSetPassword
	if mc.lastProfileNewPwd != "newpassword" {
		t.Errorf("expected new password 'newpassword', got %q", mc.lastProfileNewPwd)
	}
}

func TestLogin_Error(t *testing.T) {
	in := strings.NewReader("alice\nwrong\n")
	var out bytes.Buffer

	t.Setenv("HOME", t.TempDir())

	cmd := commands.NewLoginCmdWithIO(newErrClient("invalid credentials"), &out, in)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
