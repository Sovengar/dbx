package palette

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
)

func TestDefaultCommands_IncludesRollback(t *testing.T) {
	cmds := DefaultCommands()
	for _, cmd := range cmds {
		if cmd.Action != "rollback" {
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
	t.Fatal("DefaultCommands() missing rollback command")
}

func TestBuildCommands_Rollback_IncludesKeybind(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"rollback": "U"}})
	cmds := BuildCommands(kb)
	for _, cmd := range cmds {
		if cmd.Action != "rollback" {
			continue
		}
		if cmd.Alias != "rollback (U)" {
			t.Fatalf("rollback alias = %q, want %q", cmd.Alias, "rollback (U)")
		}
		return
	}
	t.Fatal("BuildCommands() missing rollback command")
}

func TestDefaultCommands_IncludesCopySQL(t *testing.T) {
	cmds := DefaultCommands()
	found := false
	for _, cmd := range cmds {
		if cmd.Action == "copy_sql" {
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
		t.Fatal("DefaultCommands() missing copy_sql command")
	}
}

func TestBuildCommands_CopySQL_IncludesKeybind(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{})
	cmds := BuildCommands(kb)
	found := false
	for _, cmd := range cmds {
		if cmd.Action == "copy_sql" {
			found = true
			if cmd.Alias != "copy (ctrl+y)" {
				t.Fatalf("Copy SQL alias = %q, want %q", cmd.Alias, "copy (ctrl+y)")
			}
			break
		}
	}
	if !found {
		t.Fatal("BuildCommands() missing copy_sql command")
	}
}
