package config

import "testing"

func TestKeybindRegistry_ResolveCopySQLInEditor(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	if id, ok := r.Resolve("ctrl+y", ContextEditor); !ok || id != "copy_sql" {
		t.Fatalf("Resolve('ctrl+y','editor') = %q,%v want copy_sql,true", id, ok)
	}
	if _, ok := r.Resolve("ctrl+y", ContextGrid); ok {
		t.Fatal("copy_sql must not resolve in the grid context")
	}
}

func TestKeybindRegistry_ResolveRollback(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	if id, ok := r.Resolve("U", ContextGrid); !ok || id != "rollback" {
		t.Fatalf("Resolve('U','grid') = %q,%v want rollback,true", id, ok)
	}
}

func TestKeybindRegistry_QuitNotInEditor(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	if _, ok := r.Resolve("q", ContextEditor); ok {
		t.Fatal("quit must not resolve in the editor context")
	}
}

func TestKeybindRegistry_PrimaryKey(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	if got := r.PrimaryKey("copy_sql"); got != "ctrl+y" {
		t.Fatalf("PrimaryKey(copy_sql) = %q, want ctrl+y", got)
	}
	if got := r.PrimaryKey("focus_explorer"); got != "" {
		t.Fatalf("PrimaryKey(focus_explorer) = %q, want empty", got)
	}
}

func TestKeybindRegistry_ActionsForExplorer(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	var hasFilter bool
	for _, a := range r.ActionsFor(ContextExplorer) {
		if a.ID == "filter_tables" {
			hasFilter = true
		}
		if a.ID == "copy_sql" {
			t.Fatal("ActionsFor(explorer) leaked copy_sql")
		}
	}
	if !hasFilter {
		t.Fatal("ActionsFor(explorer) missing filter_tables")
	}
}

func TestKeybindRegistry_CustomOverrideReplacesAllKeys(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{Custom: map[string]string{"next_page": "X"}})
	if got := r.KeysFor("next_page"); len(got) != 1 || got[0] != "X" {
		t.Fatalf("KeysFor(next_page) = %v, want [X]", got)
	}
	if _, ok := r.Resolve("n", ContextGrid); ok {
		t.Fatal("old next_page key 'n' must stop resolving after override")
	}
	if id, ok := r.Resolve("X", ContextGrid); !ok || id != "next_page" {
		t.Fatalf("Resolve('X','grid') = %q,%v want next_page,true", id, ok)
	}
}

// Scenario: Toda acción declarada tiene sección y descripción.
func TestRegistry_EveryActionHasSectionAndDescription(t *testing.T) {
	for _, a := range NewKeybindRegistry(KeybindingsConfig{}).All() {
		if a.Section == "" {
			t.Errorf("action %q has an empty Section", a.ID)
		}
		if a.Description == "" {
			t.Errorf("action %q has an empty Description", a.ID)
		}
	}
}

func TestRegistry_ActionIDsAreUnique(t *testing.T) {
	seen := make(map[ActionID]bool)
	for _, a := range NewKeybindRegistry(KeybindingsConfig{}).All() {
		if seen[a.ID] {
			t.Errorf("duplicate action id %q", a.ID)
		}
		seen[a.ID] = true
	}
}

// Scenario: No hay colisiones de tecla dentro de una misma vista.
func TestRegistry_NoKeyCollisionsWithinContext(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	owner := make(map[string]ActionID)
	for _, a := range r.All() {
		for _, ctx := range a.Contexts {
			for _, key := range a.Keys {
				if key == "" {
					continue
				}
				slot := ctx + "\x00" + key
				if prev, ok := owner[slot]; ok && prev != a.ID {
					t.Errorf("collision in context %q on key %q between %q and %q", ctx, key, prev, a.ID)
				}
				owner[slot] = a.ID
			}
		}
	}
}

// Scenario: display == dispatch — the shown key must execute the shown action.
func TestRegistry_DisplayedKeyResolvesToSameAction(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	for _, a := range r.All() {
		for _, ctx := range a.Contexts {
			for _, key := range a.Keys {
				if key == "" {
					continue
				}
				id, ok := r.Resolve(key, ctx)
				if !ok || id != a.ID {
					t.Errorf("Resolve(%q,%q) = %q,%v; displayed action is %q", key, ctx, id, ok, a.ID)
				}
			}
		}
	}
}
