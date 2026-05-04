package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage baobotctl configuration",
	}
	cmd.AddCommand(newConfigSetCmd(), newConfigGetCmd())
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.Set(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("set %s = %s\n", args[0], args[1])
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Printf("endpoint: %s\n", cfg.Endpoint)
			return nil
		},
	}
}
