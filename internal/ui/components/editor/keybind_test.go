package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
)

func editorWithRegistry(t *testing.T, kb *config.KeybindRegistry) *SQLEditor {
	t.Helper()
	ed := NewSQLEditor(testStyles())
	ed.SetKeybinds(kb)
	ed.Focus()
	return ed
}

// Scenario: Editor shortcuts dispatch through the resolved action ID.
func TestSQLEditor_HistoryPrevDispatchesViaRegistry(t *testing.T) {
	ed := editorWithRegistry(t, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	ed.PushHistory("SELECT 1")

	cmd, handled := ed.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !handled || cmd != nil {
		t.Fatalf("ctrl+p = (cmd=%v, handled=%v), want (nil, true)", cmd, handled)
	}
	if got := ed.Content(); got != "SELECT 1" {
		t.Fatalf("content = %q, want the previous history entry", got)
	}
}

// Scenario: the editor no longer matches a shortcut against a raw key string,
// so rebinding history_prev moves the trigger with it.
func TestSQLEditor_RebindingRemovesRawBypass(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"history_prev": "ctrl+b"}})
	ed := editorWithRegistry(t, kb)
	ed.PushHistory("SELECT 1")

	// The old key no longer resolves and must not navigate.
	if _, handled := ed.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}); handled {
		t.Fatal("ctrl+p still handled after history_prev was rebound away")
	}
	if ed.Content() == "SELECT 1" {
		t.Fatal("ctrl+p navigated history despite being unbound")
	}

	// The rebound key does.
	if _, handled := ed.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}); !handled {
		t.Fatal("rebound ctrl+b did not dispatch history_prev")
	}
	if got := ed.Content(); got != "SELECT 1" {
		t.Fatalf("content = %q, want the previous history entry", got)
	}
}

// Scenario: Widget-local text entry in the editor is untouched.
func TestSQLEditor_TextKeysDoNotResolve(t *testing.T) {
	ed := editorWithRegistry(t, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	for _, key := range []string{"a", "1", "space", "backspace", "enter"} {
		if _, ok := ed.keybinds.Resolve(key, config.ContextEditor); ok {
			t.Errorf("editor context unexpectedly resolves text key %q", key)
		}
	}

	if _, handled := ed.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}); !handled {
		t.Fatal("letter was not handled as local text entry")
	}
	if got := ed.Content(); got != "a" {
		t.Fatalf("content = %q, want %q after typing", got, "a")
	}
}

// Scenario: tab keeps its dual behavior under the single autocomplete action.
func TestSQLEditor_TabAcceptsCompletionWhenVisible(t *testing.T) {
	ed := editorWithRegistry(t, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	ed.SetSchema(testExport())
	ed.SetContent("SELECT * FROM users WH")
	ed.SetCursorPos(0, len("SELECT * FROM users WH"))
	ed.triggerAutocomplete()
	if !ed.AutocompleteVisible() {
		t.Fatal("precondition failed: autocomplete popup is not visible")
	}
	for i := 0; i < 50; i++ {
		item := ed.autocomplete.SelectedItem()
		if item != nil && item.Name == "WHERE" {
			break
		}
		ed.autocomplete.SelectNext()
	}
	if item := ed.autocomplete.SelectedItem(); item == nil || item.Name != "WHERE" {
		t.Fatalf("could not select the WHERE completion, got %v", item)
	}

	if _, handled := ed.Update(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
		t.Fatal("tab was not handled while the completion popup was visible")
	}
	if got, want := ed.Content(), "SELECT * FROM users WHERE "; got != want {
		t.Fatalf("content = %q, want the accepted completion %q", got, want)
	}
}

func TestSQLEditor_TabIndentsWhenNoCompletion(t *testing.T) {
	ed := editorWithRegistry(t, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	ed.SetContent("x")
	ed.SetCursorPos(0, 0)

	if _, handled := ed.Update(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
		t.Fatal("tab was not handled when no completion was visible")
	}
	if got, want := ed.Content(), "    x"; got != want {
		t.Fatalf("content = %q, want the indented %q", got, want)
	}
}
