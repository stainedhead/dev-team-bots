// Package backup provides the scheduled memory backup use case.
// It runs periodic backups of the agent's memory directory using a
// domain.MemoryBackup implementation on a configurable cron schedule.
package backup

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const defaultSchedule = "*/30 * * * *"

// cronRunner is the subset of *cron.Cron used by ScheduledBackupUseCase.
// Injected in tests to control scheduling.
type cronRunner interface {
	AddFunc(spec string, cmd func()) (cron.EntryID, error)
	Start()
	Stop() context.Context
}

// ScheduledBackupUseCase runs a periodic backup of the memory directory.
type ScheduledBackupUseCase struct {
	backup   domain.MemoryBackup
	schedule string
	newCron  func() cronRunner
}

// New constructs a ScheduledBackupUseCase. If schedule is empty, the default
// cron expression "*/30 * * * *" (every 30 minutes) is used.
func New(backup domain.MemoryBackup, schedule string) *ScheduledBackupUseCase {
	s := schedule
	if s == "" {
		s = defaultSchedule
	}
	return &ScheduledBackupUseCase{
		backup:   backup,
		schedule: s,
		newCron:  func() cronRunner { return cron.New() },
	}
}

// Run starts the scheduled backup loop and blocks until ctx is cancelled.
// Each tick calls Backup(ctx). Errors are logged but do not stop the loop.
// Returns the context error when ctx is cancelled.
func (u *ScheduledBackupUseCase) Run(ctx context.Context) error {
	c := u.newCron()

	_, err := c.AddFunc(u.schedule, func() {
		start := time.Now()
		if err := u.backup.Backup(ctx); err != nil {
			slog.ErrorContext(ctx, "scheduled memory backup failed",
				"error", err,
				"duration", time.Since(start).String(),
			)
			return
		}
		slog.InfoContext(ctx, "scheduled memory backup succeeded",
			"duration", time.Since(start).String(),
		)
	})
	if err != nil {
		return err
	}

	c.Start()
	<-ctx.Done()
	stopCtx := c.Stop()
	<-stopCtx.Done()
	return ctx.Err()
}
