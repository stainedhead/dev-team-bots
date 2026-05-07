package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const defaultRetentionDays = 10

// RunRetentionCleanup deletes done board items and all tasks older than retentionDays.
// A retentionDays value of 0 uses the default (10 days).
func RunRetentionCleanup(ctx context.Context, board domain.BoardStore, tasks domain.DirectTaskStore, retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = defaultRetentionDays
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	cleanupBoard(ctx, board, cutoff)
	cleanupTasks(ctx, tasks, cutoff)
}

func cleanupBoard(ctx context.Context, board domain.BoardStore, cutoff time.Time) {
	items, err := board.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusDone})
	if err != nil {
		slog.Warn("retention cleanup: list done board items", "err", err)
		return
	}
	deleted := 0
	for _, item := range items {
		if item.UpdatedAt.Before(cutoff) {
			if delErr := board.Delete(ctx, item.ID); delErr != nil {
				slog.Warn("retention cleanup: delete board item", "id", item.ID, "err", delErr)
			} else {
				deleted++
			}
		}
	}
	if deleted > 0 {
		slog.Info("retention cleanup: removed done board items", "count", deleted)
	}
}

func cleanupTasks(ctx context.Context, tasks domain.DirectTaskStore, cutoff time.Time) {
	all, err := tasks.ListAll(ctx)
	if err != nil {
		slog.Warn("retention cleanup: list tasks", "err", err)
		return
	}
	deleted := 0
	for _, t := range all {
		if t.CreatedAt.Before(cutoff) {
			if delErr := tasks.Delete(ctx, t.ID); delErr != nil {
				slog.Warn("retention cleanup: delete task", "id", t.ID, "err", delErr)
			} else {
				deleted++
			}
		}
	}
	if deleted > 0 {
		slog.Info("retention cleanup: removed old tasks", "count", deleted)
	}
}
