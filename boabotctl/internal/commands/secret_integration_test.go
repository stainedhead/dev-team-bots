//go:build integration

// This file holds Phase I's (tasks.md I2/C5) `//go:build integration` stub
// for the Secret-storage PRD AC "boabotctl writes, checks presence of, and
// deletes a keystore secret on each of the three platforms" (line 587).
// Unlike boabot's own internal/infrastructure/secret/keystore package,
// this exercises the actual `secret set`/`secret get`/`secret delete`
// cobra commands end-to-end (piped stdin for set, exactly as an operator
// would invoke them) against the real OS keystore -- not the injected
// keystoreBackend fake the unit tests in secret_test.go use. It self-skips
// unless BUZZ_KEYSTORE_TEST_LIVE=1 is set, so `go test -tags integration
// ./...` is safe to run anywhere (including CI) without touching a real
// keystore. Running it for real, on each of macOS/Windows/Linux, is
// tracked on boabot's implementation-notes.md "Manual Verification
// Required" checklist.
package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestLiveKeystore_SecretSetGetDeleteRoundTrip(t *testing.T) {
	if os.Getenv("BUZZ_KEYSTORE_TEST_LIVE") != "1" {
		t.Skip("BUZZ_KEYSTORE_TEST_LIVE not set to 1; this test writes to the real OS keystore")
	}

	const botName = "integration-test-bot"
	const secretName = "integration_test_secret"
	const value = "integration-test-value"

	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := NewSecretCmdWithIO(&out, strings.NewReader(value+"\n"), libKeystoreBackend{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("secret %v: %v", args, err)
		}
		return out.String()
	}

	t.Cleanup(func() {
		_ = libKeystoreBackend{}.Delete(keystoreServiceName, secretAccount(botName, secretName))
	})

	run("set", secretName, "--bot", botName)

	getOut := run("get", secretName, "--bot", botName)
	if !strings.Contains(getOut, "present") && !strings.Contains(getOut, "found") {
		t.Fatalf("secret get output = %q, want it to report presence (not the value itself, per FR-049)", getOut)
	}
	if strings.Contains(getOut, value) {
		t.Fatalf("secret get output leaked the secret value: %q", getOut)
	}

	run("delete", secretName, "--bot", botName)

	got, err := libKeystoreBackend{}.Get(keystoreServiceName, secretAccount(botName, secretName))
	if err == nil {
		t.Fatalf("secret still present after delete: %q", got)
	}
}
