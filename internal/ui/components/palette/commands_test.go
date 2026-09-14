package palette

import (
	"testing"
)

func TestDefaultCommands_IncludesCopySQL(t *testing.T) {
	cmds := DefaultCommands()
	found := false
	for _, cmd := range cmds {
		if cmd.Action == "editor.copy" {
			found = true
			if cmd.Name != "Copy SQL" {
				t.Fatalf("Copy SQL command name = %q, want %q", cmd.Name, "Copy SQL")
			}
			if cmd.Section != SectionQuery {
				t.Fatalf("Copy SQL section = %q, want %q", cmd.Section, SectionQuery)
			}
			break
		}
	}
	if !found {
		t.Fatal("DefaultCommands() missing editor.copy command")
	}
}

func TestBuildCommands_CopySQL_IncludesKeybind(t *testing.T) {
	kb := map[string]string{
		"editor.copy": "ctrl+y",
	}
	cmds := BuildCommands(kb)
	found := false
	for _, cmd := range cmds {
		if cmd.Action == "editor.copy" {
			found = true
			if cmd.Alias != "copy (ctrl+y)" {
				t.Fatalf("Copy SQL alias = %q, want %q", cmd.Alias, "copy (ctrl+y)")
			}
			break
		}
	}
	if !found {
		t.Fatal("BuildCommands() missing editor.copy command")
	}
}
