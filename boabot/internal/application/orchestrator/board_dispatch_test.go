package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── fakes ──────────────────────────────────────────────────────────────────

type fakeDispatcher struct {
	dispatchFn func(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource, threadID, workDir string) (domain.DirectTask, error)
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource, threadID, workDir string) (domain.DirectTask, error) {
	if f.dispatchFn != nil {
		return f.dispatchFn(ctx, botName, instruction, scheduledAt, source, threadID, workDir)
	}
	return domain.DirectTask{ID: "task-1", Status: domain.DirectTaskStatusRunning}, nil
}

func (f *fakeDispatcher) RunNow(ctx context.Context, id string) (domain.DirectTask, error) {
	return domain.DirectTask{}, errors.New("not implemented")
}

type fakeBoardStore struct {
	updateFn func(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error)
}

func (f *fakeBoardStore) Create(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	return item, nil
}
func (f *fakeBoardStore) Update(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, item)
	}
	return item, nil
}
func (f *fakeBoardStore) Get(ctx context.Context, id string) (domain.WorkItem, error) {
	return domain.WorkItem{}, errors.New("not implemented")
}
func (f *fakeBoardStore) List(ctx context.Context, filter domain.WorkItemFilter) ([]domain.WorkItem, error) {
	return nil, nil
}
func (f *fakeBoardStore) Delete(ctx context.Context, id string) error     { return nil }
func (f *fakeBoardStore) Reorder(ctx context.Context, ids []string) error { return nil }

// ── tests ──────────────────────────────────────────────────────────────────

func TestBoardDispatch_DispatchBoardItem_Success(t *testing.T) {
	var gotInstruction string
	var gotBot string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, botName, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		gotBot = botName
		gotInstruction = instruction
		return domain.DirectTask{ID: "task-42"}, nil
	}}
	var gotUpdate domain.WorkItem
	board := &fakeBoardStore{updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
		gotUpdate = item
		return item, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      board,
	})

	item := domain.WorkItem{
		ID:         "item-1",
		Title:      "Fix the login bug",
		AssignedTo: "dev-bot",
	}
	updated, err := bd.DispatchBoardItem(context.Background(), item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBot != "dev-bot" {
		t.Errorf("dispatched to wrong bot: got %q", gotBot)
	}
	if gotUpdate.ActiveTaskID != "task-42" {
		t.Errorf("active_task_id not stored: got %q", gotUpdate.ActiveTaskID)
	}
	if updated.ActiveTaskID != "task-42" {
		t.Errorf("returned item missing active_task_id")
	}
	if gotInstruction == "" {
		t.Error("instruction was empty")
	}
}

func TestBoardDispatch_SlashCommandInTitle(t *testing.T) {
	var gotInstruction string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		gotInstruction = instruction
		return domain.DirectTask{ID: "t1"}, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{ID: "i1", Title: "/devflow:implm-frm-prd", AssignedTo: "bot"}
	if _, err := bd.DispatchBoardItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !contains(gotInstruction, "Run the following skill command") {
		t.Errorf("expected slash command instruction, got: %q", gotInstruction)
	}
}

func TestBoardDispatch_NoBot_Noop(t *testing.T) {
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: &fakeDispatcher{},
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{ID: "i1", Title: "task"}
	updated, err := bd.DispatchBoardItem(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActiveTaskID != "" {
		t.Error("should not dispatch without assigned bot")
	}
}

func TestBoardDispatch_DispatchError(t *testing.T) {
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		return domain.DirectTask{}, errors.New("queue full")
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{ID: "i1", Title: "task", AssignedTo: "bot"}
	_, err := bd.DispatchBoardItem(context.Background(), item)
	if err == nil {
		t.Error("expected error")
	}
}

func TestBoardDispatch_WorkDir_IncludedInInstruction(t *testing.T) {
	var gotInstruction string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, workDir string) (domain.DirectTask, error) {
		gotInstruction = instruction
		return domain.DirectTask{ID: "t1"}, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{ID: "i1", Title: "Deploy app", AssignedTo: "bot", WorkDir: "/home/user/project"}
	if _, err := bd.DispatchBoardItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !contains(gotInstruction, "Working directory") {
		t.Errorf("expected working directory in instruction, got: %q", gotInstruction)
	}
	if !contains(gotInstruction, "/home/user/project") {
		t.Errorf("expected work dir path in instruction, got: %q", gotInstruction)
	}
}

func TestBoardDispatch_AllowedWorkDirs_IncludedInInstruction(t *testing.T) {
	var gotInstruction string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		gotInstruction = instruction
		return domain.DirectTask{ID: "t1"}, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher:      disp,
		Board:           &fakeBoardStore{},
		AllowedWorkDirs: []string{"/safe/dir", "/other/dir"},
	})
	item := domain.WorkItem{ID: "i1", Title: "Run analysis", AssignedTo: "bot"}
	if _, err := bd.DispatchBoardItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !contains(gotInstruction, "SECURITY CONSTRAINT") {
		t.Errorf("expected security constraint in instruction, got: %q", gotInstruction)
	}
}

func TestBoardDispatch_SlashCommandInDescription(t *testing.T) {
	var gotInstruction string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		gotInstruction = instruction
		return domain.DirectTask{ID: "t1"}, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{ID: "i1", Title: "Normal title", Description: "/devflow:review-code", AssignedTo: "bot"}
	if _, err := bd.DispatchBoardItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !contains(gotInstruction, "Run the following skill command") {
		t.Errorf("expected slash command instruction from description, got: %q", gotInstruction)
	}
}

func TestBoardDispatch_TextAttachment_IncludedInInstruction(t *testing.T) {
	var gotInstruction string
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		gotInstruction = instruction
		return domain.DirectTask{ID: "t1"}, nil
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      &fakeBoardStore{},
	})
	item := domain.WorkItem{
		ID:         "i1",
		Title:      "Review this",
		AssignedTo: "bot",
		Attachments: []domain.Attachment{
			{
				ID:          "a1",
				Name:        "notes.txt",
				ContentType: "text/plain",
				Content:     "aGVsbG8gd29ybGQ=", // base64("hello world")
			},
		},
	}
	if _, err := bd.DispatchBoardItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !contains(gotInstruction, "Attachment: notes.txt") {
		t.Errorf("expected attachment in instruction, got: %q", gotInstruction)
	}
	if !contains(gotInstruction, "hello world") {
		t.Errorf("expected attachment content in instruction, got: %q", gotInstruction)
	}
}

func TestBoardDispatch_BoardUpdateError_ReturnsError(t *testing.T) {
	disp := &fakeDispatcher{dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: "t1"}, nil
	}}
	board := &fakeBoardStore{updateFn: func(_ context.Context, _ domain.WorkItem) (domain.WorkItem, error) {
		return domain.WorkItem{}, errors.New("update failed")
	}}
	bd := orchestrator.NewBoardDispatch(orchestrator.BoardDispatchConfig{
		Dispatcher: disp,
		Board:      board,
	})
	item := domain.WorkItem{ID: "i1", Title: "task", AssignedTo: "bot"}
	_, err := bd.DispatchBoardItem(context.Background(), item)
	if err == nil {
		t.Error("expected error when board update fails")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
