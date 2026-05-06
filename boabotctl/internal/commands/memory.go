package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
)

// NewMemoryCmd creates the memory command group with injected dependencies.
func NewMemoryCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage agent memory backup and restore",
	}
	cmd.AddCommand(
		newMemoryBackupCmd(c, w),
		newMemoryRestoreCmd(c, w),
		newMemoryStatusCmd(c, w),
	)
	return cmd
}

func newMemoryBackupCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Trigger an immediate memory backup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := c.MemoryBackup(context.Background()); err != nil {
				return err
			}
			fmt.Fprintln(w, "Backup triggered.")
			return nil
		},
	}
}

func newMemoryRestoreCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "restore",
		Short: "Trigger a memory restore from the remote backup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := c.MemoryRestore(context.Background()); err != nil {
				return err
			}
			fmt.Fprintln(w, "Restore triggered.")
			return nil
		},
	}
}

func newMemoryStatusCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current memory backup status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := c.MemoryStatus(context.Background())
			if err != nil {
				return err
			}
			if st.LastBackupAt.IsZero() {
				fmt.Fprintln(w, "Last backup:     never")
			} else {
				fmt.Fprintf(w, "Last backup:     %s\n", st.LastBackupAt.Format("2006-01-02 15:04:05 UTC"))
			}
			fmt.Fprintf(w, "Pending changes: %d\n", st.PendingChanges)
			fmt.Fprintf(w, "Remote URL:      %s\n", st.RemoteURL)
			return nil
		},
	}
}
