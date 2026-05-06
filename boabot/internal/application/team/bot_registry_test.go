package team_test

import (
	"sort"
	"sync"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestBotRegistry_RegisterGetList(t *testing.T) {
	t.Parallel()
	r := team.NewBotRegistry()

	// Empty registry.
	if _, ok := r.Get("x"); ok {
		t.Fatal("expected Get on empty registry to return false")
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty List, got %d items", len(got))
	}

	// Register two bots.
	alice := domain.BotIdentity{Name: "alice", BotType: "orchestrator", QueueURL: "alice"}
	bob := domain.BotIdentity{Name: "bob", BotType: "implementer", QueueURL: "bob"}
	r.Register(alice)
	r.Register(bob)

	if id, ok := r.Get("alice"); !ok || id != alice {
		t.Errorf("Get(alice) = %v, %v; want %v, true", id, ok, alice)
	}
	if id, ok := r.Get("bob"); !ok || id != bob {
		t.Errorf("Get(bob) = %v, %v; want %v, true", id, ok, bob)
	}
	if _, ok := r.Get("unknown"); ok {
		t.Error("Get(unknown) should return false")
	}

	// List should contain both.
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list))
	}
	names := []string{list[0].Name, list[1].Name}
	sort.Strings(names)
	if names[0] != "alice" || names[1] != "bob" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestBotRegistry_RegisterOverwrites(t *testing.T) {
	t.Parallel()
	r := team.NewBotRegistry()

	original := domain.BotIdentity{Name: "alice", BotType: "orchestrator", QueueURL: "old-url"}
	updated := domain.BotIdentity{Name: "alice", BotType: "orchestrator", QueueURL: "new-url"}

	r.Register(original)
	r.Register(updated)

	id, ok := r.Get("alice")
	if !ok {
		t.Fatal("expected Get to return true after overwrite")
	}
	if id.QueueURL != "new-url" {
		t.Errorf("expected QueueURL=new-url, got %s", id.QueueURL)
	}
	if len(r.List()) != 1 {
		t.Errorf("expected 1 item after overwrite, got %d", len(r.List()))
	}
}

func TestBotRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	r := team.NewBotRegistry()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		i := i
		go func() {
			defer wg.Done()
			name := "bot"
			if i%2 == 0 {
				name = "bot-even"
			}
			r.Register(domain.BotIdentity{Name: name, BotType: "worker", QueueURL: name})
			r.Get(name)
			r.List()
		}()
	}
	wg.Wait()
}
