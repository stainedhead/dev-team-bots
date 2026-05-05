package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
)

// NewTeamCmd creates the team command group with injected dependencies.
func NewTeamCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Inspect the running bot team",
	}
	cmd.AddCommand(
		newTeamListCmd(c, w),
		newTeamGetCmd(c, w),
		newTeamHealthCmd(c, w),
	)
	return cmd
}

func newTeamListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered bots",
		RunE: func(cmd *cobra.Command, _ []string) error {
			bots, err := c.TeamList(context.Background())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTYPE\tSTATUS\tLAST SEEN")
			for _, b := range bots {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", b.Name, b.BotType, b.Status, b.LastSeen.Format("2006-01-02 15:04:05"))
			}
			return tw.Flush()
		},
	}
}

func newTeamGetCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get details for a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bot, err := c.TeamGet(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Name:      %s\n", bot.Name)
			fmt.Fprintf(w, "Type:      %s\n", bot.BotType)
			fmt.Fprintf(w, "Status:    %s\n", bot.Status)
			fmt.Fprintf(w, "Last seen: %s\n", bot.LastSeen.Format("2006-01-02 15:04:05"))
			return nil
		},
	}
}

func newTeamHealthCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Overall team health summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := c.TeamHealth(context.Background())
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Active:   %d\n", h.Active)
			fmt.Fprintf(w, "Inactive: %d\n", h.Inactive)
			fmt.Fprintf(w, "Total:    %d\n", h.Total)
			return nil
		},
	}
}
