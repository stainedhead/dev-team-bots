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
)

// NewChatCmd creates the chat command group.
func NewChatCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Chat with bots and manage conversation threads",
	}
	cmd.AddCommand(
		newChatThreadsCmd(c, w),
		newChatSendCmd(c, w),
		newChatMessagesCmd(c, w),
		newChatDeleteThreadCmd(c, w),
	)
	return cmd
}

func newChatThreadsCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "threads",
		Short: "List conversation threads",
		RunE: func(cmd *cobra.Command, _ []string) error {
			threads, err := c.ThreadList(context.Background())
			if err != nil {
				return err
			}
			if len(threads) == 0 {
				fmt.Fprintln(w, "No threads.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTITLE\tPARTICIPANTS\tUPDATED")
			for _, t := range threads {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					t.ID, t.Title,
					strings.Join(t.Participants, ","),
					t.UpdatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
}

func newChatSendCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var threadID string
	cmd := &cobra.Command{
		Use:   "send <bot> <message>",
		Short: "Send a chat message to a bot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			msg, err := c.ChatSend(context.Background(), args[0], args[1], threadID)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Sent to %s (thread %s). Use 'chat messages %s' to see the reply.\n",
				args[0], msg.ThreadID, msg.ThreadID)
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "continue an existing thread (optional)")
	return cmd
}

func newChatMessagesCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "messages <thread-id>",
		Short: "Print messages in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			msgs, err := c.ThreadMessages(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(msgs) == 0 {
				fmt.Fprintln(w, "No messages.")
				return nil
			}
			// Messages come newest-first; reverse for chronological display.
			for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
				msgs[i], msgs[j] = msgs[j], msgs[i]
			}
			for _, m := range msgs {
				who := "you"
				if m.Direction == "inbound" {
					who = m.BotName
				}
				fmt.Fprintf(w, "[%s] %s: %s\n",
					m.CreatedAt.Format("15:04:05"), who, m.Content)
			}
			return nil
		},
	}
}

func newChatDeleteThreadCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <thread-id>",
		Short: "Delete a conversation thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.ThreadDelete(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(w, "Deleted.")
			return nil
		},
	}
}
