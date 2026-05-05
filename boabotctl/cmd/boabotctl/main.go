package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/auth"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
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
	token, _ := auth.Load()
	c := client.NewHTTPClient(cfg.Endpoint, func() string { return token })

	root := &cobra.Command{
		Use:     "baobotctl",
		Short:   "BaoBot operator CLI",
		Version: version,
	}

	root.AddCommand(
		commands.NewLoginCmd(c, os.Stdout),
		commands.NewBoardCmd(c, os.Stdout),
		commands.NewTeamCmd(c, os.Stdout),
		commands.NewUserCmd(c, os.Stdout),
		commands.NewProfileCmd(c, os.Stdout),
		commands.NewDLQCmd(c, os.Stdout),
		commands.NewConfigCmd(),
	)

	return root
}
