package plugin_test

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/plugin"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestRegistryUseCase_List(t *testing.T) {
	expected := []domain.PluginRegistry{{Name: "official", URL: "https://r.example.com"}}
	mgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return expected, nil
		},
	}
	uc := plugin.NewRegistryUseCase(mgr)
	regs, err := uc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(regs) != 1 || regs[0].Name != "official" {
		t.Errorf("unexpected registries: %+v", regs)
	}
}

func TestRegistryUseCase_Add(t *testing.T) {
	var addedReg domain.PluginRegistry
	mgr := &mocks.RegistryManager{
		AddFn: func(_ context.Context, reg domain.PluginRegistry) error {
			addedReg = reg
			return nil
		},
	}
	uc := plugin.NewRegistryUseCase(mgr)
	reg := domain.PluginRegistry{Name: "new-reg", URL: "https://new.example.com", Trusted: true}
	if err := uc.Add(context.Background(), reg); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if addedReg.Name != reg.Name {
		t.Errorf("expected %q, got %q", reg.Name, addedReg.Name)
	}
}

func TestRegistryUseCase_Remove(t *testing.T) {
	var removedName string
	mgr := &mocks.RegistryManager{
		RemoveFn: func(_ context.Context, name string) error {
			removedName = name
			return nil
		},
	}
	uc := plugin.NewRegistryUseCase(mgr)
	if err := uc.Remove(context.Background(), "old-reg"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removedName != "old-reg" {
		t.Errorf("expected old-reg, got %q", removedName)
	}
}

func TestRegistryUseCase_FetchIndex_ByName(t *testing.T) {
	regs := []domain.PluginRegistry{
		{Name: "official", URL: "https://official.example.com", Trusted: true},
	}
	var fetchedURL string
	var fetchedForce bool
	expectedIdx := domain.RegistryIndex{Registry: "official"}

	mgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return regs, nil
		},
		FetchIndexFn: func(_ context.Context, url string, force bool) (domain.RegistryIndex, error) {
			fetchedURL = url
			fetchedForce = force
			return expectedIdx, nil
		},
	}

	uc := plugin.NewRegistryUseCase(mgr)
	idx, err := uc.FetchIndex(context.Background(), "official", true)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if idx.Registry != "official" {
		t.Errorf("unexpected index registry: %s", idx.Registry)
	}
	if fetchedURL != "https://official.example.com" {
		t.Errorf("expected URL https://official.example.com, got %q", fetchedURL)
	}
	if !fetchedForce {
		t.Error("expected force=true to be passed through")
	}
}

func TestRegistryUseCase_FetchIndex_NotFound(t *testing.T) {
	mgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{}, nil
		},
	}
	uc := plugin.NewRegistryUseCase(mgr)
	_, err := uc.FetchIndex(context.Background(), "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent registry, got nil")
	}
}
