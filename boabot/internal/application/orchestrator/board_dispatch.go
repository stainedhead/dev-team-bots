package orchestrator

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// BoardDispatchConfig holds the dependencies needed to dispatch a board item.
type BoardDispatchConfig struct {
	Dispatcher      domain.TaskDispatcher
	Board           domain.BoardStore
	AllowedWorkDirs []string
}

// BoardDispatch implements domain.BoardItemDispatcher: it builds a task
// instruction from the item and dispatches it to the assigned bot.
type BoardDispatch struct {
	cfg BoardDispatchConfig
}

// NewBoardDispatch creates a BoardDispatch with the supplied config.
func NewBoardDispatch(cfg BoardDispatchConfig) *BoardDispatch {
	return &BoardDispatch{cfg: cfg}
}

// DispatchBoardItem builds an instruction from the item's fields, dispatches
// it via the TaskDispatcher, and stores the resulting task ID back on the item.
func (d *BoardDispatch) DispatchBoardItem(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	if item.AssignedTo == "" || d.cfg.Dispatcher == nil {
		return item, nil
	}
	instruction := buildBoardInstruction(item, d.cfg.AllowedWorkDirs)
	task, err := d.cfg.Dispatcher.Dispatch(ctx, item.AssignedTo, instruction, nil,
		domain.DirectTaskSourceBoard, "", item.WorkDir)
	if err != nil {
		return item, fmt.Errorf("dispatch board item %s: %w", item.ID, err)
	}
	item.ActiveTaskID = task.ID
	updated, updateErr := d.cfg.Board.Update(ctx, item)
	if updateErr != nil {
		return item, fmt.Errorf("store active_task_id for board item %s: %w", item.ID, updateErr)
	}
	return updated, nil
}

// buildBoardInstruction assembles the prompt sent to the bot for a board item.
func buildBoardInstruction(item domain.WorkItem, allowedWorkDirs []string) string {
	var instruction string
	if cmd := extractSlashCommand(item.Title); cmd != "" {
		instruction = fmt.Sprintf(
			"Run the following skill command: /%s\n\nBoard item context:\n\nTitle: %s\n\nDescription: %s\n\nItem ID: %s",
			cmd, item.Title, item.Description, item.ID)
	} else if cmd := extractSlashCommand(item.Description); cmd != "" {
		instruction = fmt.Sprintf(
			"Run the following skill command: /%s\n\nBoard item context:\n\nTitle: %s\n\nDescription: %s\n\nItem ID: %s",
			cmd, item.Title, item.Description, item.ID)
	} else {
		instruction = fmt.Sprintf(
			"Board item assigned to you:\n\nTitle: %s\n\nDescription: %s\n\nItem ID: %s",
			item.Title, item.Description, item.ID)
	}
	if item.WorkDir != "" {
		instruction += fmt.Sprintf(
			"\n\nWorking directory: %s\nYou may read and write files in this directory to complete your work. If it is a git repository you may also use git commands.",
			item.WorkDir)
	}
	if len(allowedWorkDirs) > 0 {
		instruction += fmt.Sprintf(
			"\n\nSECURITY CONSTRAINT: You are only permitted to access files within these directories: %s\nDo not read, write, or execute files outside these paths.",
			strings.Join(allowedWorkDirs, ", "))
	}
	for _, att := range item.Attachments {
		raw, decErr := base64.StdEncoding.DecodeString(att.Content)
		if decErr != nil {
			continue
		}
		ct := att.ContentType
		if ct == "" || strings.HasPrefix(ct, "text/") || ct == "application/json" ||
			strings.HasSuffix(att.Name, ".md") || strings.HasSuffix(att.Name, ".yaml") ||
			strings.HasSuffix(att.Name, ".yml") || strings.HasSuffix(att.Name, ".go") ||
			strings.HasSuffix(att.Name, ".txt") {
			instruction += fmt.Sprintf("\n\n--- Attachment: %s ---\n%s", att.Name, string(raw))
		}
	}
	return instruction
}

func extractSlashCommand(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "/") {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimPrefix(parts[0], "/")
}
