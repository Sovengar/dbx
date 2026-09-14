package config

import (
	"testing"
)

func TestDefaultKeybindings_IncludesCopySQL(t *testing.T) {
	kb := DefaultKeybindings()
	if _, ok := kb["editor.copy"]; !ok {
		t.Fatal("DefaultKeybindings() missing 'editor.copy'")
	}
	if kb["editor.copy"] != "ctrl+y" {
		t.Fatalf("editor.copy key = %q, want %q", kb["editor.copy"], "ctrl+y")
	}
}

func TestDefaultBindings_IncludesCopySQL(t *testing.T) {
	bindings := defaultBindings()
	if _, ok := bindings["editor.copy"]; !ok {
		t.Fatal("defaultBindings() missing 'editor.copy'")
	}
	found := false
	for _, k := range bindings["editor.copy"] {
		if k == "ctrl+y" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("editor.copy bindings = %v, want 'ctrl+y'", bindings["editor.copy"])
	}
}

func TestKeybindRegistry_MatchesCopySQL(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	action := r.Match("ctrl+y", "editor")
	if action != "editor.copy" {
		t.Fatalf("Match('ctrl+y', 'editor') = %q, want %q", action, "editor.copy")
	}
}

func TestKeybindRegistry_Flatten_IncludesCopySQL(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	flat := r.Flatten()
	if _, ok := flat["editor.copy"]; !ok {
		t.Fatal("Flatten() missing 'editor.copy'")
	}
}
