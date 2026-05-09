package domain_test

import (
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestRulesUpdate_HasChanges_Empty(t *testing.T) {
	u := domain.RulesUpdate{}
	if u.HasChanges() {
		t.Error("empty RulesUpdate should have no changes")
	}
}

func TestRulesUpdate_HasChanges_WithAdd(t *testing.T) {
	u := domain.RulesUpdate{
		Add: []domain.RulesEntry{{Dir: "/foo", File: "AGENTS.md", Content: "x"}},
	}
	if !u.HasChanges() {
		t.Error("RulesUpdate with Add should have changes")
	}
}

func TestRulesUpdate_HasChanges_WithRemove(t *testing.T) {
	u := domain.RulesUpdate{
		Remove: []domain.RulesEntry{{Dir: "/foo", File: "AGENTS.md"}},
	}
	if !u.HasChanges() {
		t.Error("RulesUpdate with Remove should have changes")
	}
}
