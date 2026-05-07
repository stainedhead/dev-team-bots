package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
		newBoardDeleteCmd(c, w),
		newBoardActivityCmd(c, w),
		newBoardAskCmd(c, w),
		newBoardAttachCmd(c, w),
		newBoardAttachmentsCmd(c, w),
		newBoardAttachmentGetCmd(c, w),
		newBoardAttachmentDeleteCmd(c, w),
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
	var title, description, assignTo, workDir string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new work item",
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := domain.CreateWorkItemRequest{
				Title:       title,
				Description: description,
				AssignedTo:  assignTo,
				WorkDir:     workDir,
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
	cmd.Flags().StringVar(&workDir, "workdir", "", "working directory for bot output (optional)")
	return cmd
}

func newBoardUpdateCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var title, description, status, workDir string
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
			if cmd.Flags().Changed("workdir") {
				req.WorkDir = &workDir
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
	cmd.Flags().StringVar(&workDir, "workdir", "", "working directory for bot output")
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

func newBoardDeleteCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Permanently delete a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.BoardDelete(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(w, "Deleted.")
			return nil
		},
	}
}

func printWorkItem(w io.Writer, item domain.WorkItem) {
	fmt.Fprintf(w, "ID:          %s\n", item.ID)
	fmt.Fprintf(w, "Title:       %s\n", item.Title)
	fmt.Fprintf(w, "Description: %s\n", item.Description)
	if item.WorkDir != "" {
		fmt.Fprintf(w, "Work dir:    %s\n", item.WorkDir)
	}
	fmt.Fprintf(w, "Status:      %s\n", item.Status)
	fmt.Fprintf(w, "Assigned to: %s\n", item.AssignedTo)
	if item.ActiveTaskID != "" {
		fmt.Fprintf(w, "Active task: %s\n", item.ActiveTaskID)
	}
	if item.LastResult != "" {
		result := item.LastResult
		if len(result) > 200 {
			result = result[:200] + "… (truncated, use 'board activity' for full output)"
		}
		fmt.Fprintf(w, "Last result: %s\n", result)
	}
	if len(item.Attachments) > 0 {
		names := make([]string, len(item.Attachments))
		for i, a := range item.Attachments {
			names[i] = fmt.Sprintf("%s (%s)", a.Name, fmtBytes(a.Size))
		}
		fmt.Fprintf(w, "Attachments: %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(w, "Created by:  %s\n", item.CreatedBy)
	fmt.Fprintf(w, "Created at:  %s\n", item.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Updated at:  %s\n", item.UpdatedAt.Format("2006-01-02 15:04:05"))
}

func fmtBytes(b int) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/1024/1024)
	}
}

func newBoardActivityCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "activity <id>",
		Short: "Show a work item's activity and last bot output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			act, err := c.BoardActivity(context.Background(), args[0])
			if err != nil {
				return err
			}
			printWorkItem(w, act.Item)
			if act.Task != nil {
				fmt.Fprintln(w)
				fmt.Fprintf(w, "Task ID:     %s\n", act.Task.ID)
				fmt.Fprintf(w, "Task status: %s\n", act.Task.Status)
				if act.Task.Output != "" {
					fmt.Fprintf(w, "\n--- Bot Output ---\n%s\n", act.Task.Output)
				}
			}
			return nil
		},
	}
}

func newBoardAskCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var threadID string
	cmd := &cobra.Command{
		Use:   "ask <id> <question>",
		Short: "Ask the assigned bot a question about this item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			msg, err := c.BoardAsk(context.Background(), args[0], args[1], threadID)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Sent (thread %s). Reply will appear in chat.\n", msg.ThreadID)
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "existing thread ID to continue (optional)")
	return cmd
}

func newBoardAttachCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id> <file> [file...]",
		Short: "Upload one or more files to a work item",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := c.BoardAttachmentUpload(context.Background(), args[0], args[1:])
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Uploaded. Item now has %d attachment(s).\n", len(item.Attachments))
			return nil
		},
	}
}

func newBoardAttachmentsCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "attachments <id>",
		Short: "List attachments on a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := c.BoardGet(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(item.Attachments) == 0 {
				fmt.Fprintln(w, "No attachments.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tSIZE\tUPLOADED")
			for _, a := range item.Attachments {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					a.ID, a.Name, fmtBytes(a.Size), a.UploadedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
}

func newBoardAttachmentGetCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "attachment-get <item-id> <att-id>",
		Short: "Download or print an attachment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, ct, name, err := c.BoardAttachmentGet(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			if outPath != "" {
				return os.WriteFile(outPath, data, 0o644)
			}
			// Print text content inline; prompt to use --output for binary.
			if strings.HasPrefix(ct, "text/") || ct == "application/json" || ct == "" {
				fmt.Fprintf(w, "--- %s ---\n", name)
				_, err = w.Write(data)
				return err
			}
			fmt.Fprintf(w, "Binary file (%s). Use --output <path> to save it.\n", ct)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "output", "", "write to file instead of stdout")
	return cmd
}

func newBoardAttachmentDeleteCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "attachment-delete <item-id> <att-id>",
		Short: "Remove an attachment from a work item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.BoardAttachmentDelete(context.Background(), args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintln(w, "Removed.")
			return nil
		},
	}
}
