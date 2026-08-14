package acp

import (
	"context"
	"sync"
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

// fakePublisher records Publish calls so tests can assert on fallback-publish
// behavior without shelling out to a real buzz CLI.
type fakePublisher struct {
	mu    sync.Mutex
	err   error
	calls []publishCall
}

type publishCall struct {
	channelID string
	content   string
}

func (f *fakePublisher) Publish(_ context.Context, channelID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, publishCall{channelID: channelID, content: content})
	return f.err
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePublisher) last() publishCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return publishCall{}
	}
	return f.calls[len(f.calls)-1]
}
