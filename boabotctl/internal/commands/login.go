package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/auth"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewLoginCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store credentials locally",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// TODO: prompt username/password, call client.Login, handle must_change_password
			_ = cfg
			_ = auth.Save
			return fmt.Errorf("not implemented")
		},
	}
	return cmd
}
