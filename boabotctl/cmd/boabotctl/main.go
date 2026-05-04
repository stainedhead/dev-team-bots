package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cfg, _ := config.Load()

	root := &cobra.Command{
		Use:     "baobotctl",
		Short:   "BaoBot operator CLI",
		Version: version,
	}

	root.AddCommand(
		commands.NewLoginCmd(cfg),
		commands.NewBoardCmd(cfg),
		commands.NewTeamCmd(cfg),
		commands.NewUserCmd(cfg),
		commands.NewProfileCmd(cfg),
		commands.NewConfigCmd(),
	)

	return root
}
