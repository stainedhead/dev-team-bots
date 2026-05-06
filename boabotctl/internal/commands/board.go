package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// NewBoardCmd creates the board command group with injected client and writer.
// Pass os.Stdout as w and a real OrchestratorClient in production;
// tests inject a mockClient and a bytes.Buffer.
func NewBoardCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "board",
		Short: "Manage Kanban board work items",
	}
	cmd.AddCommand(
		newBoardListCmd(c, w),
		newBoardGetCmd(c, w),
		newBoardCreateCmd(c, w),
		newBoardUpdateCmd(c, w),
		newBoardAssignCmd(c, w),
		newBoardCloseCmd(c, w),
	)
	return cmd
}

func newBoardListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all work items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := c.BoardList(context.Background())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tASSIGNED")
			for _, item := range items {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.ID, item.Title, item.Status, item.AssignedTo)
			}
			return tw.Flush()
		},
	}
}

func newBoardGetCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := c.BoardGet(context.Background(), args[0])
			if err != nil {
				return err
			}
			printWorkItem(w, item)
			return nil
		},
	}
}

func newBoardCreateCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var title, description, assignTo string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new work item",
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := domain.CreateWorkItemRequest{
				Title:       title,
				Description: description,
				AssignedTo:  assignTo,
			}
			item, err := c.BoardCreate(context.Background(), req)
			if err != nil {
				return err
			}
			printWorkItem(w, item)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "work item title")
	cmd.Flags().StringVar(&description, "description", "", "work item description")
	cmd.Flags().StringVar(&assignTo, "assign", "", "bot to assign the item to")
	return cmd
}

func newBoardUpdateCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var title, description, status string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := domain.UpdateWorkItemRequest{}
			if cmd.Flags().Changed("title") {
				req.Title = &title
			}
			if cmd.Flags().Changed("description") {
				req.Description = &description
			}
			if cmd.Flags().Changed("status") {
				req.Status = &status
			}
			item, err := c.BoardUpdate(context.Background(), args[0], req)
			if err != nil {
				return err
			}
			printWorkItem(w, item)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	return cmd
}

func newBoardAssignCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "assign <id>",
		Short: "Assign a work item to a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := c.BoardAssign(context.Background(), args[0], to)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Assigned to %s\n", item.AssignedTo)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "bot name to assign to")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newBoardCloseCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "close <id>",
		Short: "Close a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.BoardClose(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(w, "Closed")
			return nil
		},
	}
}

func printWorkItem(w io.Writer, item domain.WorkItem) {
	fmt.Fprintf(w, "ID:          %s\n", item.ID)
	fmt.Fprintf(w, "Title:       %s\n", item.Title)
	fmt.Fprintf(w, "Description: %s\n", item.Description)
	fmt.Fprintf(w, "Status:      %s\n", item.Status)
	fmt.Fprintf(w, "Assigned to: %s\n", item.AssignedTo)
	fmt.Fprintf(w, "Created by:  %s\n", item.CreatedBy)
	fmt.Fprintf(w, "Created at:  %s\n", item.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Updated at:  %s\n", item.UpdatedAt.Format("2006-01-02 15:04:05"))
}
