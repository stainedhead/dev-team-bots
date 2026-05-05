package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestProfileGet_HappyPath(t *testing.T) {
	mc := &mockClient{
		profileGetResp: domain.User{Username: "alice", DisplayName: "Alice Smith", Role: "admin", Enabled: true},
	}
	var out bytes.Buffer
	cmd := commands.NewProfileCmd(mc, &out)
	cmd.SetArgs([]string{"get"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "alice") || !strings.Contains(got, "Alice Smith") {
		t.Errorf("missing profile data in output: %q", got)
	}
}

func TestProfileGet_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewProfileCmd(newErrClient("profile unavailable"), &out)
	cmd.SetArgs([]string{"get"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestProfileSetName_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	cmd := commands.NewProfileCmd(mc, &out)
	cmd.SetArgs([]string{"set-name", "Alicia"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastProfileSetName != "Alicia" {
		t.Errorf("expected set-name 'Alicia', got %q", mc.lastProfileSetName)
	}
}

func TestProfileSetName_Error(t *testing.T) {
	var out bytes.Buffer
	cmd := commands.NewProfileCmd(newErrClient("set-name failed"), &out)
	cmd.SetArgs([]string{"set-name", "Alicia"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestProfileSetPwd_HappyPath(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	// old password, new password, new password (confirmation)
	in := strings.NewReader("oldpw\nnewpw\nnewpw\n")
	cmd := commands.NewProfileCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"set-pwd"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastProfileOldPwd != "oldpw" || mc.lastProfileNewPwd != "newpw" {
		t.Errorf("wrong passwords: old=%q new=%q", mc.lastProfileOldPwd, mc.lastProfileNewPwd)
	}
}

func TestProfileSetPwd_Mismatch(t *testing.T) {
	mc := &mockClient{}
	var out bytes.Buffer
	// old password, then two mismatched new passwords
	in := strings.NewReader("oldpw\nnewpw\ndifferent\n")
	cmd := commands.NewProfileCmdWithIO(mc, &out, in)
	cmd.SetArgs([]string{"set-pwd"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for password mismatch")
	}
}

func TestProfileSetPwd_Error(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("old\nnew\nnew\n")
	cmd := commands.NewProfileCmdWithIO(newErrClient("change failed"), &out, in)
	cmd.SetArgs([]string{"set-pwd"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
