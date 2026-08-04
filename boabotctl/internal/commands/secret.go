package commands

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
)

// keystoreServiceName is the OS keystore "service" boabot's own
// internal/infrastructure/secret/keystore provider uses. It MUST stay in
// sync with that package's serviceName constant (FR-045: the key convention
// is the stable, documented contract shared across the module boundary —
// see architecture.md's "Cross-Module Constraint").
const keystoreServiceName = "boabot"

// keystoreBackend abstracts zalando/go-keyring's package-level functions so
// the secret commands are unit-testable against an in-memory fake, without
// ever touching a real OS keystore in tests.
type keystoreBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type libKeystoreBackend struct{}

func (libKeystoreBackend) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (libKeystoreBackend) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (libKeystoreBackend) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// secretAccount returns the keyring "user"/account name for name, namespaced
// by bot when set. This duplicates (does not import — see the module-level
// comment above) boabot's internal/infrastructure/secret/keystore
// convention: "<bot>/<name>" when bot is set, or the bare "<name>" for a
// global/shared secret. boabotctl cannot import boabot's internal/ packages
// across the module boundary (Go's internal/ visibility is enforced
// per-module-root even within this monorepo), so the convention is
// re-implemented here rather than shared as code; only the convention
// itself is shared, and it must stay stable and identical in both places.
func secretAccount(bot, name string) string {
	if bot != "" {
		return bot + "/" + name
	}
	return name
}

// NewSecretCmd creates the "secret" command group, operating on the local
// machine's OS keystore only (OQ-11 resolved local-only — no remote-host
// support). FR-049.
func NewSecretCmd(w io.Writer) *cobra.Command {
	return NewSecretCmdWithIO(w, os.Stdin, libKeystoreBackend{})
}

// NewSecretCmdWithIO creates the "secret" command group with fully injected
// IO and keystore backend, for testing.
func NewSecretCmdWithIO(w io.Writer, r io.Reader, b keystoreBackend) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage boabot secrets in this machine's OS keystore (local machine only)",
	}
	cmd.AddCommand(
		newSecretSetCmd(w, r, b),
		newSecretGetCmd(w, b),
		newSecretDeleteCmd(w, b),
	)
	return cmd
}

func newSecretSetCmd(w io.Writer, r io.Reader, b keystoreBackend) *cobra.Command {
	var bot string
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Write a secret to the local OS keystore",
		Long: "Write a secret to the local OS keystore, under the same service/account\n" +
			"convention boabot's SecretStore keystore provider reads from (FR-045).\n" +
			"The value is read from a prompt (or piped stdin), never from a command-\n" +
			"line argument or flag, since process arguments are world-readable.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			value, err := promptPassword(w, r, "Value: ")
			if err != nil {
				return fmt.Errorf("read value: %w", err)
			}
			if value == "" {
				return fmt.Errorf("secret value must not be empty")
			}
			if err := b.Set(keystoreServiceName, secretAccount(bot, name), value); err != nil {
				return fmt.Errorf("set secret %q: %w", name, err)
			}
			fmt.Fprintf(w, "secret %q set\n", name) //nolint:errcheck
			return nil
		},
	}
	cmd.Flags().StringVar(&bot, "bot", "", "bot name to namespace the secret under (optional; matches SecretRef.Bot)")
	return cmd
}

func newSecretGetCmd(w io.Writer, b keystoreBackend) *cobra.Command {
	var bot string
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Report whether a secret is present in the local OS keystore",
		Long: "Reports only whether a secret is present in the local OS keystore — the\n" +
			"value itself is never printed. FR-049 grants boabotctl the ability to\n" +
			"\"write, read-presence-of (never the value), and delete\" keystore\n" +
			"secrets; this command implements the presence-check, not a value dump.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			_, err := b.Get(keystoreServiceName, secretAccount(bot, name))
			if err != nil {
				if errors.Is(err, keyring.ErrNotFound) {
					fmt.Fprintf(w, "secret %q: not present\n", name) //nolint:errcheck
					return nil
				}
				return fmt.Errorf("check secret %q: %w", name, err)
			}
			fmt.Fprintf(w, "secret %q: present\n", name) //nolint:errcheck
			return nil
		},
	}
	cmd.Flags().StringVar(&bot, "bot", "", "bot name the secret is namespaced under (optional; matches SecretRef.Bot)")
	return cmd
}

func newSecretDeleteCmd(w io.Writer, b keystoreBackend) *cobra.Command {
	var bot string
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a secret from the local OS keystore",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := b.Delete(keystoreServiceName, secretAccount(bot, name)); err != nil {
				if errors.Is(err, keyring.ErrNotFound) {
					fmt.Fprintf(w, "secret %q: not present\n", name) //nolint:errcheck
					return nil
				}
				return fmt.Errorf("delete secret %q: %w", name, err)
			}
			fmt.Fprintf(w, "secret %q deleted\n", name) //nolint:errcheck
			return nil
		},
	}
	cmd.Flags().StringVar(&bot, "bot", "", "bot name the secret is namespaced under (optional; matches SecretRef.Bot)")
	return cmd
}
