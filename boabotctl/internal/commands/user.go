package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewUserCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users (admin only)",
	}
	cmd.AddCommand(
		newUserListCmd(cfg),
		newUserAddCmd(cfg),
		newUserRemoveCmd(cfg),
		newUserDisableCmd(cfg),
		newUserSetPwdCmd(cfg),
		newUserSetRoleCmd(cfg),
	)
	return cmd
}

func newUserListCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}

func newUserAddCmd(cfg config.Config) *cobra.Command {
	var username, role string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create a new user account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			_ = username
			_ = role
			return fmt.Errorf("not implemented")
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "username")
	cmd.Flags().StringVar(&role, "role", "user", "role (admin|user)")
	return cmd
}

func newUserRemoveCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Remove a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newUserDisableCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <username>",
		Short: "Disable a user account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newUserSetPwdCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "set-pwd <username>",
		Short: "Reset a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newUserSetRoleCmd(cfg config.Config) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "set-role <username>",
		Short: "Change a user's role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			_ = role
			return fmt.Errorf("not implemented")
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "role (admin|user)")
	_ = cmd.MarkFlagRequired("role")
	return cmd
}
