package acp

import (
	"context"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeWorker is a hand-written domain.Worker test double (AGENTS.md: "write
// them by hand for simple interfaces").
type fakeWorker struct {
	result       domain.TaskResult
	err          error
	delay        time.Duration // simulates a long-running turn
	progressFn   func(taskID, line string)
	receivedTask domain.Task
}

func (f *fakeWorker) Execute(ctx context.Context, task domain.Task) (domain.TaskResult, error) {
	f.receivedTask = task
	if f.progressFn != nil {
		f.progressFn(task.ID, "starting")
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return domain.TaskResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return domain.TaskResult{}, f.err
	}
	return f.result, nil
}

// WithProgressHandler makes *fakeWorker satisfy the acp package's optional
// progressReporter interface, mirroring
// application.ExecuteTaskUseCase.WithProgressHandler.
func (f *fakeWorker) WithProgressHandler(fn func(taskID, line string)) {
	f.progressFn = fn
}

type fakeWorkerFactory struct {
	worker domain.Worker
}

func (f *fakeWorkerFactory) New() domain.Worker {
	if f.worker == nil {
		f.worker = &fakeWorker{}
	}
	return f.worker
}
