package app

import (
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/explorerpreview"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
)

// coverageModel builds a model with every dispatch owner wired so the
// action↔handler coverage test can aggregate their handled actions.
func coverageModel(t *testing.T) Model {
	t.Helper()
	m := newRollbackTestModel()
	m.explorer = explorer.New(m.styles, nil, m.keybinds)
	m.grid = grid.New(m.styles, 100, m.keybinds)
	m.gridPreview = gridpreview.New(m.styles, m.keybinds)
	m.explorerPreview = explorerpreview.New(m.styles, m.keybinds)
	return m
}

func handledByOwner(m Model) map[string]map[config.ActionID]bool {
	owners := map[string]map[config.ActionID]bool{}
	add := func(owner string, ids []config.ActionID) {
		set := owners[owner]
		if set == nil {
			set = map[config.ActionID]bool{}
			owners[owner] = set
		}
		for _, id := range ids {
			set[id] = true
		}
	}
	add("app", m.HandledActions())
	if m.grid != nil {
		add("grid", m.grid.HandledActions())
	}
	if m.explorer != nil {
		add("explorer", m.explorer.HandledActions())
	}
	if m.gridPreview != nil {
		add("gridpreview", m.gridPreview.HandledActions())
	}
	if m.explorerPreview != nil {
		add("explorerpreview", m.explorerPreview.HandledActions())
	}
	if m.editor != nil {
		add("editor", m.editor.HandledActions())
	}
	return owners
}

// Scenario: Toda acción tiene handler, salvo las marcadas como pendientes.
func TestRegistry_EveryNonPendingActionHasHandler(t *testing.T) {
	m := coverageModel(t)
	owners := handledByOwner(m)

	registry := config.NewKeybindRegistry(config.KeybindingsConfig{})
	declared := map[config.ActionID]bool{}
	for _, a := range registry.All() {
		declared[a.ID] = true
		if a.Pending {
			continue
		}
		covered := false
		for _, set := range owners {
			if set[a.ID] {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("action %q (owner %q) has no dispatch handler", a.ID, a.Owner)
		}
	}

	// Rule 4: no handler without a declared action.
	for owner, set := range owners {
		for id := range set {
			if !declared[id] {
				t.Errorf("owner %q handles undeclared action %q", owner, id)
			}
		}
	}
}

// Rule 3: an action's declared owner must actually handle it.
func TestRegistry_OwnerHandlesItsActions(t *testing.T) {
	m := coverageModel(t)
	owners := handledByOwner(m)

	for _, a := range config.NewKeybindRegistry(config.KeybindingsConfig{}).All() {
		if a.Pending || a.Owner == "" {
			continue
		}
		if !owners[a.Owner][a.ID] {
			t.Errorf("action %q declares owner %q, but that owner does not handle it", a.ID, a.Owner)
		}
	}
}

// Scenario: The ASK action is wired — declared, no longer pending, handled by
// the app owner.
func TestRegistry_AskActionIsWired(t *testing.T) {
	m := coverageModel(t)
	owners := handledByOwner(m)

	registry := config.NewKeybindRegistry(config.KeybindingsConfig{})
	var ask *config.Action
	for _, a := range registry.All() {
		if a.ID == "ask" {
			cp := a
			ask = &cp
		}
	}
	if ask == nil {
		t.Fatal("registry is missing the 'ask' action")
	}
	if ask.Pending {
		t.Fatal("'ask' must not be declared Pending once its handler is wired")
	}
	if ask.Description != "Ask AI (NL→SQL)" {
		t.Fatalf("'ask' description = %q, want %q", ask.Description, "Ask AI (NL→SQL)")
	}
	if ask.Keys[0] != "a" {
		t.Fatalf("'ask' primary key = %q, want 'a'", ask.Keys[0])
	}
	if !owners["app"]["ask"] {
		t.Fatal("'ask' is not handled by the app owner")
	}
}
