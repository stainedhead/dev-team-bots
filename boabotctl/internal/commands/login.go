package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/auth"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
)

// NewLoginCmd creates the login command with injected dependencies.
// Pass os.Stdin as r and os.Stdout as w in production;
// tests inject a strings.Reader and a bytes.Buffer.
func NewLoginCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return NewLoginCmdWithIO(c, w, os.Stdin)
}

// NewLoginCmdWithIO creates the login command with fully injected IO.
func NewLoginCmdWithIO(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store credentials locally",
		RunE: func(cmd *cobra.Command, _ []string) error {
			username, err := promptLine(w, r, "Username: ")
			if err != nil {
				return fmt.Errorf("read username: %w", err)
			}
			password, err := promptPassword(w, r, "Password: ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}

			resp, err := c.Login(context.Background(), username, password)
			if err != nil {
				return err
			}

			if resp.MustChangePassword {
				newPwd, err := promptPassword(w, r, "New password required. New password: ")
				if err != nil {
					return fmt.Errorf("read new password: %w", err)
				}
				if err := c.ProfileSetPassword(context.Background(), password, newPwd); err != nil {
					return fmt.Errorf("set new password: %w", err)
				}
			}

			if err := auth.Save(resp.Token); err != nil {
				return fmt.Errorf("save credentials: %w", err)
			}
			fmt.Fprintf(w, "Logged in as %s\n", username)
			return nil
		},
	}
}
