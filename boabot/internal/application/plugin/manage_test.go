package plugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/plugin"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestManageUseCase_List(t *testing.T) {
	expected := []domain.Plugin{{ID: "1", Name: "p1"}, {ID: "2", Name: "p2"}}
	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return expected, nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	plugins, err := uc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(plugins))
	}
}

func TestManageUseCase_Get(t *testing.T) {
	expected := domain.Plugin{ID: "abc", Name: "test"}
	store := &mocks.PluginStore{
		GetFn: func(_ context.Context, id string) (domain.Plugin, error) {
			if id != "abc" {
				return domain.Plugin{}, errors.New("not found")
			}
			return expected, nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	p, err := uc.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.ID != "abc" {
		t.Errorf("got ID %q, want abc", p.ID)
	}
}

func TestManageUseCase_Approve_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		ApproveFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Approve(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Approve called with xyz, got %q", calledID)
	}
}

func TestManageUseCase_Reject_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		RejectFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Reject(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Reject called with xyz, got %q", calledID)
	}
}

func TestManageUseCase_Enable_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		EnableFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Enable(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Enable called with xyz, got %q", calledID)
	}
}

func TestManageUseCase_Disable_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		DisableFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Disable(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Disable called with xyz, got %q", calledID)
	}
}

func TestManageUseCase_Reload_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		ReloadFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Reload(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Reload called with xyz, got %q", calledID)
	}
}

func TestManageUseCase_Remove_CallsStore(t *testing.T) {
	var calledID string
	store := &mocks.PluginStore{
		RemoveFn: func(_ context.Context, id string) error {
			calledID = id
			return nil
		},
	}
	uc := plugin.NewManageUseCase(store)
	if err := uc.Remove(context.Background(), "xyz", "admin"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if calledID != "xyz" {
		t.Errorf("expected Remove called with xyz, got %q", calledID)
	}
}
