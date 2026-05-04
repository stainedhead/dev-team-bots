package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/config"
)

func NewBoardCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board",
		Short: "Manage Kanban board work items",
	}
	cmd.AddCommand(
		newBoardListCmd(cfg),
		newBoardGetCmd(cfg),
		newBoardCreateCmd(cfg),
		newBoardUpdateCmd(cfg),
		newBoardAssignCmd(cfg),
		newBoardCloseCmd(cfg),
	)
	return cmd
}

func newBoardListCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all work items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			return fmt.Errorf("not implemented")
		},
	}
}

func newBoardGetCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newBoardCreateCmd(cfg config.Config) *cobra.Command {
	var title, description, assignTo string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new work item",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			_ = title
			_ = description
			_ = assignTo
			return fmt.Errorf("not implemented")
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "work item title")
	cmd.Flags().StringVar(&description, "description", "", "work item description")
	cmd.Flags().StringVar(&assignTo, "assign", "", "bot to assign the item to")
	return cmd
}

func newBoardUpdateCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "update <id>",
		Short: "Update a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}

func newBoardAssignCmd(cfg config.Config) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "assign <id>",
		Short: "Assign a work item to a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			_ = to
			return fmt.Errorf("not implemented")
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "bot name to assign to")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newBoardCloseCmd(cfg config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "close <id>",
		Short: "Close a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			_ = args[0]
			return fmt.Errorf("not implemented")
		},
	}
}
