package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewTeamCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Inspect the running bot team",
	}
	cmd.AddCommand(
		newTeamListCmd(cfg),
		newTeamGetCmd(cfg),
		newTeamHealthCmd(cfg),
	)
	return cmd
}

func newTeamListCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered bots",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}

func newTeamGetCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get details for a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newTeamHealthCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Overall team health summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}
