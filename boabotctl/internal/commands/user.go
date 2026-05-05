package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// NewUserCmd creates the user command group with injected dependencies.
// stdin defaults to os.Stdin.
func NewUserCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return NewUserCmdWithIO(c, w, os.Stdin)
}

// NewUserCmdWithIO creates the user command group with fully injected IO.
func NewUserCmdWithIO(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users (admin only)",
	}
	cmd.AddCommand(
		newUserListCmd(c, w),
		newUserAddCmd(c, w, r),
		newUserRemoveCmd(c, w, r),
		newUserDisableCmd(c, w),
		newUserSetPwdCmd(c, w, r),
		newUserSetRoleCmd(c, w),
	)
	return cmd
}

func newUserListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			users, err := c.UserList(context.Background())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "USERNAME\tROLE\tENABLED")
			for _, u := range users {
				enabled := "yes"
				if !u.Enabled {
					enabled = "no"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", u.Username, u.Role, enabled)
			}
			return tw.Flush()
		},
	}
}

func newUserAddCmd(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	var username, role string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create a new user account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			password, err := promptPassword(w, r, "Initial password: ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			req := domain.CreateUserRequest{
				Username: username,
				Role:     role,
				Password: password,
			}
			user, err := c.UserCreate(context.Background(), req)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Created user %s (role: %s)\n", user.Username, user.Role)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "username")
	cmd.Flags().StringVar(&role, "role", "user", "role (admin|user)")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func newUserRemoveCmd(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Remove a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			ok, err := confirm(w, r, fmt.Sprintf("Remove user %s? [y/N]: ", username))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(w, "Cancelled.")
				return nil
			}
			if err := c.UserRemove(context.Background(), username); err != nil {
				return err
			}
			fmt.Fprintf(w, "Removed user %s\n", username)
			return nil
		},
	}
}

func newUserDisableCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <username>",
		Short: "Disable a user account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.UserDisable(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(w, "Disabled user %s\n", args[0])
			return nil
		},
	}
}

func newUserSetPwdCmd(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:   "set-pwd <username>",
		Short: "Reset a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			newPwd, err := promptPassword(w, r, "New password: ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			if err := c.UserSetPassword(context.Background(), args[0], newPwd); err != nil {
				return err
			}
			fmt.Fprintf(w, "Password updated for %s\n", args[0])
			return nil
		},
	}
}

func newUserSetRoleCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "set-role <username>",
		Short: "Change a user's role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.UserSetRole(context.Background(), args[0], role); err != nil {
				return err
			}
			fmt.Fprintf(w, "Role updated for %s: %s\n", args[0], role)
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "role (admin|user)")
	_ = cmd.MarkFlagRequired("role")
	return cmd
}
