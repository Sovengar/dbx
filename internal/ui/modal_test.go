package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func teaKey(key string) tea.KeyPressMsg {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	default:
		return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
}

func modalWith(registry *config.KeybindRegistry) *HelpModal {
	m := NewHelpModal(theme.Resolve("dark").Styles(), registry)
	m.SetWidth(80)
	m.SetHeight(400)
	m.Show()
	return m
}

func TestHelpModal_IncludesRollback(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	if !strings.Contains(view, "Rollback Last Transaction") {
		t.Fatal("Help modal does not contain 'Rollback Last Transaction'")
	}
}

// Scenario: ASK action shown in the help modal
func TestHelpModal_GlobalSection_IncludesAsk(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	stripped := ansi.Strip(view)
	if !strings.Contains(stripped, "Ask AI (NL→SQL)") {
		t.Fatal("Help modal does not contain 'Ask AI (NL→SQL)' in Global section")
	}
	want := "  " + fmt.Sprintf("%-14s", "a") + "Ask AI (NL→SQL)"
	if !strings.Contains(stripped, want) {
		t.Fatalf("Help modal does not render key 'a' next to 'Ask AI (NL→SQL)'; want substring %q", want)
	}
}

func TestHelpModal_EditorSection_IncludesCopySQL(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	if !strings.Contains(view, "Copy SQL") {
		t.Fatal("Help modal does not contain 'Copy SQL' in Editor section")
	}
	if !strings.Contains(view, "ctrl+y") {
		t.Fatal("Help modal does not show 'ctrl+y' keybind for Copy SQL")
	}
}

// Scenario: El modal de ayuda cubre todas las secciones operables.
func TestHelpModal_CoversAllOperableSections(t *testing.T) {
	view := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{})).View()
	for _, header := range []string{
		"Explorer", "Grid", "Grid Preview", "Explorer Preview",
		"Editor", "Query Browser", "Connection Picker",
		"EDIT Mode", "FILTER Mode", "WHERE FILTER Mode", "Mouse",
	} {
		if !strings.Contains(view, header) {
			t.Fatalf("Help modal is missing the %q section", header)
		}
	}
}

// Scenario: El modal de ayuda es navegable y se cierra.
func TestHelpModal_ClosesOnEscOrQuestion(t *testing.T) {
	for _, key := range []string{"esc", "?"} {
		styles := theme.Resolve("dark").Styles()
		modal := NewHelpModal(styles, config.NewKeybindRegistry(config.KeybindingsConfig{}))
		modal.SetWidth(80)
		modal.SetHeight(40)
		modal.Show()

		_, handled := modal.Update(teaKey(key))
		if !handled {
			t.Fatalf("Update(%q) not handled", key)
		}
		if modal.IsVisible() {
			t.Fatalf("modal still visible after %q", key)
		}
	}
}

// Scenario: Custom key override changes the modal key too.
func TestHelpModal_CustomKeyOverride(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"rollback": "ctrl+z"}})
	view := modalWith(kb).View()
	if !strings.Contains(view, "ctrl+z") {
		t.Fatalf("modal does not show the custom rollback key: %q", view)
	}
	if strings.Contains(view, "U Rollback Last Transaction") {
		t.Fatalf("modal still shows the default rollback key after override")
	}
}

// Scenario: The help modal shows every key, including aliases, and groups by
// the same registry field the pane uses.
func TestHelpModal_GridShowsAliasesAndGroups(t *testing.T) {
	view := ansi.Strip(modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{})).View())

	// next_page is ungrouped, so every alias must be listed on its line.
	wantLine := "  " + fmt.Sprintf("%-14s", "n, ], ctrl+right") + "Next Page"
	if !strings.Contains(view, wantLine) {
		t.Errorf("Grid section is missing the full next_page alias line; want %q", wantLine)
	}

	// Sibling sets collapse under their shared group label.
	for _, label := range []string{"Navigate", "First/Last", "Half Page", "Go to Page"} {
		if !strings.Contains(view, label) {
			t.Errorf("Grid section is missing the group label %q", label)
		}
	}
}
