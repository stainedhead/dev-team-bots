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

// NewDLQCmd creates the dlq command group with injected dependencies.
func NewDLQCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return NewDLQCmdWithIO(c, w, os.Stdin)
}

// NewDLQCmdWithIO creates the dlq command group with fully injected IO.
func NewDLQCmdWithIO(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "dlq",
		Short: "Inspect and manage the dead-letter queue",
	}
	cmd.AddCommand(
		newDLQListCmd(c, w),
		newDLQRetryCmd(c, w),
		newDLQDiscardCmd(c, w, r),
	)
	return cmd
}

func newDLQListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List dead-letter queue items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := c.DLQList(context.Background())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tQUEUE\tRECEIVED COUNT\tLAST RECEIVED")
			for _, item := range items {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
					item.ID, item.QueueName, item.ReceivedCount,
					item.LastReceived.Format("2006-01-02 15:04:05"))
			}
			return tw.Flush()
		},
	}
}

func newDLQRetryCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "retry <id>",
		Short: "Retry a dead-letter queue item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.DLQRetry(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(w, "Retried")
			return nil
		},
	}
}

func newDLQDiscardCmd(c client.OrchestratorClient, w io.Writer, r io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <id>",
		Short: "Discard a dead-letter queue item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			ok, err := confirm(w, r, fmt.Sprintf("Discard DLQ item %s? [y/N]: ", id))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(w, "Cancelled.")
				return nil
			}
			if err := c.DLQDiscard(context.Background(), id); err != nil {
				return err
			}
			fmt.Fprintln(w, "Discarded")
			return nil
		},
	}
}
