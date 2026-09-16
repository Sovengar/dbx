package palette

import (
	"strings"
	"testing"
)

func TestDefaultCommands_IncludesRollback(t *testing.T) {
	cmds := DefaultCommands()
	for _, cmd := range cmds {
		if cmd.Action != "global.rollback" {
			continue
		}
		if !strings.Contains(cmd.Name, "Rollback") {
			t.Fatalf("rollback command name = %q, want it to contain %q", cmd.Name, "Rollback")
		}
		if cmd.Alias != "rollback" {
			t.Fatalf("rollback command alias = %q, want %q", cmd.Alias, "rollback")
		}
		return
	}
	t.Fatal("DefaultCommands() missing global.rollback command")
}

func TestBuildCommands_Rollback_IncludesKeybind(t *testing.T) {
	cmds := BuildCommands(map[string]string{"global.rollback": "U"})
	for _, cmd := range cmds {
		if cmd.Action != "global.rollback" {
			continue
		}
		if cmd.Alias != "rollback (U)" {
			t.Fatalf("rollback alias = %q, want %q", cmd.Alias, "rollback (U)")
		}
		return
	}
	t.Fatal("BuildCommands() missing global.rollback command")
}

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
