package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewProfileCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage your own profile",
	}
	cmd.AddCommand(
		newProfileGetCmd(cfg),
		newProfileSetNameCmd(cfg),
		newProfileSetPwdCmd(cfg),
	)
	return cmd
}

func newProfileGetCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "View your profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}

func newProfileSetNameCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "set-name <display-name>",
		Short: "Update your display name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newProfileSetPwdCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "set-pwd",
		Short: "Change your password",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}
