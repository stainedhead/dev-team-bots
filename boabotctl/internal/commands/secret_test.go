package commands_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
)

// fakeKeystoreBackend is an in-memory stand-in for zalando/go-keyring's
// package-level functions, so secret command tests never touch a real OS
// keystore.
type fakeKeystoreBackend struct {
	entries map[string]string // "service/user" -> password
	getErr  error
	setErr  error
	delErr  error
}

func newFakeKeystoreBackend() *fakeKeystoreBackend {
	return &fakeKeystoreBackend{entries: map[string]string{}}
}

func key(service, user string) string { return service + "/" + user }

func (f *fakeKeystoreBackend) Get(service, user string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.entries[key(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeystoreBackend) Set(service, user, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.entries[key(service, user)] = password
	return nil
}

func (f *fakeKeystoreBackend) Delete(service, user string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.entries[key(service, user)]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, key(service, user))
	return nil
}

// TestSecretSet_WritesToKeystoreUnderConvention verifies that "secret set"
// writes under the same service/account convention as boabot's own keystore
// provider (FR-045: service "boabot", account "<bot>/<name>" or bare
// "<name>"), so the same secret is reachable by boabot's SecretStore.
func TestSecretSet_WritesToKeystoreUnderConvention(t *testing.T) {
	b := newFakeKeystoreBackend()
	var out bytes.Buffer
	in := strings.NewReader("s3cr3t-value\n")

	cmd := commands.NewSecretCmdWithIO(&out, in, b)
	cmd.SetArgs([]string{"set", "buzz_private_key", "--bot", "buzzbot"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := b.entries[key("boabot", "buzzbot/buzz_private_key")]
	if !ok {
		t.Fatal("expected an entry under service=boabot, account=buzzbot/buzz_private_key")
	}
	if got != "s3cr3t-value" {
		t.Errorf("stored value: got %q, want s3cr3t-value", got)
	}
	if strings.Contains(out.String(), "s3cr3t-value") {
		t.Errorf("secret value must never be printed, got output: %q", out.String())
	}
}

// TestSecretSet_GlobalWhenNoBot verifies the bare "<name>" account
// convention when --bot is not given.
func TestSecretSet_GlobalWhenNoBot(t *testing.T) {
	b := newFakeKeystoreBackend()
	var out bytes.Buffer
	in := strings.NewReader("anthropic-key-value\n")

	cmd := commands.NewSecretCmdWithIO(&out, in, b)
	cmd.SetArgs([]string{"set", "anthropic_api_key"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := b.entries[key("boabot", "anthropic_api_key")]; !ok {
		t.Fatal("expected an entry under service=boabot, account=anthropic_api_key")
	}
}

// TestSecretGet_ReportsPresenceOnly verifies FR-049's exact wording:
// "read-presence-of (never the value)". The secret value must never appear
// in the command's output.
func TestSecretGet_ReportsPresenceOnly(t *testing.T) {
	b := newFakeKeystoreBackend()
	b.entries[key("boabot", "buzzbot/buzz_private_key")] = "TOP-SECRET-SENTINEL-abc123"
	var out bytes.Buffer

	cmd := commands.NewSecretCmdWithIO(&out, strings.NewReader(""), b)
	cmd.SetArgs([]string{"get", "buzz_private_key", "--bot", "buzzbot"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "TOP-SECRET-SENTINEL-abc123") {
		t.Fatalf("secret value leaked into output: %q", got)
	}
	if !strings.Contains(got, "present") {
		t.Errorf("expected output to report presence, got: %q", got)
	}
}

// TestSecretGet_NotPresent verifies the miss-path message names the secret
// as not present, still without ever touching a value.
func TestSecretGet_NotPresent(t *testing.T) {
	b := newFakeKeystoreBackend()
	var out bytes.Buffer

	cmd := commands.NewSecretCmdWithIO(&out, strings.NewReader(""), b)
	cmd.SetArgs([]string{"get", "missing_secret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "not present") {
		t.Errorf("expected output to report absence, got: %q", got)
	}
}

// TestSecretDelete_RemovesEntry verifies "secret delete" removes the
// namespaced entry.
func TestSecretDelete_RemovesEntry(t *testing.T) {
	b := newFakeKeystoreBackend()
	b.entries[key("boabot", "buzzbot/buzz_private_key")] = "value"
	var out bytes.Buffer

	cmd := commands.NewSecretCmdWithIO(&out, strings.NewReader(""), b)
	cmd.SetArgs([]string{"delete", "buzz_private_key", "--bot", "buzzbot"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := b.entries[key("boabot", "buzzbot/buzz_private_key")]; ok {
		t.Error("expected the entry to be removed")
	}
	if !strings.Contains(out.String(), "deleted") {
		t.Errorf("expected output to confirm deletion, got: %q", out.String())
	}
}

// TestSecretDelete_NotPresentIsNotAnError mirrors boabot's own keystore
// provider: deleting an absent entry is not an error.
func TestSecretDelete_NotPresentIsNotAnError(t *testing.T) {
	b := newFakeKeystoreBackend()
	var out bytes.Buffer

	cmd := commands.NewSecretCmdWithIO(&out, strings.NewReader(""), b)
	cmd.SetArgs([]string{"delete", "missing_secret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "not present") {
		t.Errorf("expected output to report absence, got: %q", out.String())
	}
}

// TestSecretSet_EmptyValueRejected guards against accidentally writing an
// empty secret (e.g. an accidental blank line at a prompt).
func TestSecretSet_EmptyValueRejected(t *testing.T) {
	b := newFakeKeystoreBackend()
	var out bytes.Buffer
	in := strings.NewReader("\n")

	cmd := commands.NewSecretCmdWithIO(&out, in, b)
	cmd.SetArgs([]string{"set", "some_secret"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an empty secret value")
	}
	if len(b.entries) != 0 {
		t.Error("expected no entry to be written for an empty value")
	}
}

// TestSecretGet_BackendErrorIsSurfaced verifies a genuine backend error
// (not a not-found miss) is returned as an error, distinctly from a miss.
func TestSecretGet_BackendErrorIsSurfaced(t *testing.T) {
	b := newFakeKeystoreBackend()
	b.getErr = errors.New("keystore unreachable")
	var out bytes.Buffer

	cmd := commands.NewSecretCmdWithIO(&out, strings.NewReader(""), b)
	cmd.SetArgs([]string{"get", "some_secret"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error to be returned")
	}
}
