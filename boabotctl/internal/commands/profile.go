package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
)

// NewProfileCmd creates the profile command group with injected dependencies.
func NewProfileCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return NewProfileCmdWithIO(c, w, os.Stdin)
}

// NewProfileCmdWithIO creates the profile command group with fully injected IO.
func NewProfileCmdWithIO(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage your own profile",
	}
	cmd.AddCommand(
		newProfileGetCmd(c, w),
		newProfileSetNameCmd(c, w),
		newProfileSetPwdCmd(c, w, r),
	)
	return cmd
}

func newProfileGetCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "View your profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			user, err := c.ProfileGet(context.Background())
			if err != nil {
				return err
			}
			enabled := "yes"
			if !user.Enabled {
				enabled = "no"
			}
			fmt.Fprintf(w, "Username:     %s\n", user.Username)
			fmt.Fprintf(w, "Display name: %s\n", user.DisplayName)
			fmt.Fprintf(w, "Role:         %s\n", user.Role)
			fmt.Fprintf(w, "Enabled:      %s\n", enabled)
			return nil
		},
	}
}

func newProfileSetNameCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "set-name <display-name>",
		Short: "Update your display name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.ProfileSetName(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(w, "Display name updated to: %s\n", args[0])
			return nil
		},
	}
}

func newProfileSetPwdCmd(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:   "set-pwd",
		Short: "Change your password",
		RunE: func(cmd *cobra.Command, _ []string) error {
			oldPwd, err := promptPassword(w, r, "Current password: ")
			if err != nil {
				return fmt.Errorf("read current password: %w", err)
			}
			newPwd, err := promptPassword(w, r, "New password: ")
			if err != nil {
				return fmt.Errorf("read new password: %w", err)
			}
			confirmPwd, err := promptPassword(w, r, "Confirm new password: ")
			if err != nil {
				return fmt.Errorf("read confirm password: %w", err)
			}
			if newPwd != confirmPwd {
				return fmt.Errorf("passwords do not match")
			}
			if err := c.ProfileSetPassword(context.Background(), oldPwd, newPwd); err != nil {
				return err
			}
			fmt.Fprintln(w, "Password changed successfully")
			return nil
		},
	}
}
