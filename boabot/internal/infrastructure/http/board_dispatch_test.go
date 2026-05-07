package httpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// TestBoardUpdate_DispatchesTaskOnInProgress verifies that moving a board item
// to in-progress with an assigned bot causes a task to be dispatched to that bot.
func TestBoardUpdate_DispatchesTaskOnInProgress(t *testing.T) {
	var dispatchedBot, dispatchedInstruction string

	board := &fakeBoardStore{
		getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
			return domain.WorkItem{
				ID:          id,
				Title:       "Implement feature X",
				Description: "Write the code for feature X",
				AssignedTo:  "dev-1",
				Status:      domain.WorkItemStatusBacklog,
			}, nil
		},
		updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			return item, nil
		},
	}
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, botName, instruction string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			dispatchedBot = botName
			dispatchedInstruction = instruction
			return domain.DirectTask{ID: "dispatched-task", BotName: botName, Status: domain.DirectTaskStatusDispatched}, nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"status":"in-progress"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/board/item-123", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if dispatchedBot != "dev-1" {
		t.Errorf("expected task dispatched to dev-1, got %q", dispatchedBot)
	}
	if !strings.Contains(dispatchedInstruction, "Implement feature X") {
		t.Errorf("expected instruction to contain item title, got: %q", dispatchedInstruction)
	}
}

// TestBoardUpdate_NoDispatch_WhenNotInProgress verifies that moving to other
// statuses does NOT trigger dispatch.
func TestBoardUpdate_NoDispatch_WhenNotInProgress(t *testing.T) {
	dispatchCount := 0
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			dispatchCount++
			return domain.DirectTask{}, nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      &fakeBoardStore{},
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for _, status := range []string{"backlog", "blocked", "done"} {
		body := `{"status":"` + status + `"}`
		req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/board/item-123", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer valid-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	if dispatchCount != 0 {
		t.Errorf("expected no dispatches for non-in-progress status changes, got %d", dispatchCount)
	}
}

// TestBoardUpdate_NoDispatch_WhenNoAssignedBot verifies that moving to
// in-progress without an assigned bot does NOT trigger dispatch.
func TestBoardUpdate_NoDispatch_WhenNoAssignedBot(t *testing.T) {
	dispatchCount := 0
	board := &fakeBoardStore{
		getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
			return domain.WorkItem{
				ID:         id,
				Title:      "Unassigned item",
				AssignedTo: "", // no bot assigned
				Status:     domain.WorkItemStatusBacklog,
			}, nil
		},
		updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			return item, nil
		},
	}
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			dispatchCount++
			return domain.DirectTask{}, nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"status":"in-progress"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/board/item-123", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if dispatchCount != 0 {
		t.Errorf("expected no dispatch when no bot assigned, got %d dispatches", dispatchCount)
	}
}

// TestBoardUpdate_DispatchFailure_DoesNotFailBoardUpdate verifies that a
// dispatch failure is non-fatal — the board update still succeeds.
func TestBoardUpdate_DispatchFailure_DoesNotFailBoardUpdate(t *testing.T) {
	board := &fakeBoardStore{
		getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
			return domain.WorkItem{
				ID:         id,
				Title:      "Feature Y",
				AssignedTo: "dev-1",
				Status:     domain.WorkItemStatusBacklog,
			}, nil
		},
		updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			return item, nil
		},
	}
	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			return domain.DirectTask{}, context.Canceled // simulate failure
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"status":"in-progress"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/board/item-123", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 even when dispatch fails, got %d", resp.StatusCode)
	}
}
