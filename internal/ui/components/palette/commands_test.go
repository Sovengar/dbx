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

// Scenario: ASK command available in the palette
func TestDefaultCommands_IncludesAsk(t *testing.T) {
	cmds := DefaultCommands()
	for _, cmd := range cmds {
		if cmd.Action != "ask" {
			continue
		}
		if cmd.Name != "Ask AI" {
			t.Fatalf("ask command name = %q, want %q", cmd.Name, "Ask AI")
		}
		if cmd.Alias != "ask" {
			t.Fatalf("ask command alias = %q, want %q", cmd.Alias, "ask")
		}
		if cmd.Section != SectionQuery {
			t.Fatalf("ask command section = %q, want %q", cmd.Section, SectionQuery)
		}
		return
	}
	t.Fatal("DefaultCommands() missing ask command")
}

func TestBuildCommands_Ask_IncludesKeybind(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{})
	cmds := BuildCommands(kb)
	for _, cmd := range cmds {
		if cmd.Action != "ask" {
			continue
		}
		if cmd.Alias != "ask (a)" {
			t.Fatalf("ask alias = %q, want %q", cmd.Alias, "ask (a)")
		}
		return
	}
	t.Fatal("BuildCommands() missing ask command")
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
