package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// NewTaskCmd creates the task command group.
func NewTaskCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage direct bot tasks",
	}
	cmd.AddCommand(
		newTaskListCmd(c, w),
		newTaskGetCmd(c, w),
		newTaskCreateCmd(c, w),
	)
	return cmd
}

func newTaskListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var botName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks (all, or for a specific bot)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				tasks []domain.DirectTask
				err   error
			)
			if botName != "" {
				tasks, err = c.TaskListByBot(context.Background(), botName)
			} else {
				tasks, err = c.TaskList(context.Background())
			}
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tBOT\tSTATUS\tSOURCE\tCREATED")
			for _, t := range tasks {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					t.ID, t.BotName, t.Status, t.Source,
					t.CreatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&botName, "bot", "", "filter by bot name")
	return cmd
}

func newTaskGetCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a task and its output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := c.TaskGet(context.Background(), args[0])
			if err != nil {
				return err
			}
			printTask(w, t)
			return nil
		},
	}
}

func newTaskCreateCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var botName, instruction, schedAt string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Dispatch an instruction to a bot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var sat *time.Time
			if schedAt != "" {
				t, err := time.Parse(time.RFC3339, schedAt)
				if err != nil {
					return fmt.Errorf("invalid --at format (use RFC3339, e.g. 2026-05-06T15:00:00Z): %w", err)
				}
				sat = &t
			}
			task, err := c.TaskCreate(context.Background(), botName, instruction, sat)
			if err != nil {
				return err
			}
			printTask(w, task)
			return nil
		},
	}
	cmd.Flags().StringVar(&botName, "bot", "", "target bot name")
	cmd.Flags().StringVar(&instruction, "instruction", "", "instruction to send")
	cmd.Flags().StringVar(&schedAt, "at", "", "schedule time in RFC3339 (optional)")
	_ = cmd.MarkFlagRequired("bot")
	_ = cmd.MarkFlagRequired("instruction")
	return cmd
}

func printTask(w io.Writer, t domain.DirectTask) {
	fmt.Fprintf(w, "ID:          %s\n", t.ID)
	fmt.Fprintf(w, "Bot:         %s\n", t.BotName)
	fmt.Fprintf(w, "Status:      %s\n", t.Status)
	fmt.Fprintf(w, "Source:      %s\n", t.Source)
	fmt.Fprintf(w, "Created at:  %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
	if t.DispatchedAt != nil {
		fmt.Fprintf(w, "Dispatched:  %s\n", t.DispatchedAt.Format("2006-01-02 15:04:05"))
	}
	if t.CompletedAt != nil {
		fmt.Fprintf(w, "Completed:   %s\n", t.CompletedAt.Format("2006-01-02 15:04:05"))
	}
	if t.Instruction != "" {
		instr := t.Instruction
		if len(instr) > 300 {
			instr = instr[:300] + "… (truncated)"
		}
		fmt.Fprintf(w, "Instruction: %s\n", instr)
	}
	if t.Output != "" {
		fmt.Fprintf(w, "\n--- Output ---\n%s\n", t.Output)
	}
}
