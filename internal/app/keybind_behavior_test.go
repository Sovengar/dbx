package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	aiContext "github.com/buble/dbx/internal/ai/context"
)

// Scenario: "q" es un carácter literal en el editor, no un atajo de salida.
func TestEditor_QInsertsCharacterInsteadOfQuitting(t *testing.T) {
	m := newRollbackTestModel()
	m.editorOpen = true
	m.editor.Focus()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(q) returned %T, want Model", updated)
	}
	if cmd != nil {
		// tea.Quit would be returned as a non-nil command; a plain insert
		// must not request the program to quit.
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Fatal("pressing q in the editor quit the application")
		}
	}
	if !strings.Contains(model.editor.Content(), "q") {
		t.Fatalf("editor content %q does not contain the typed 'q'", model.editor.Content())
	}
}

// Scenario: Esc cancels an open autocomplete popup before closing the editor.
func TestEditor_EscCancelsAutocompleteBeforeClosing(t *testing.T) {
	m := newRollbackTestModel()
	m.editorOpen = true
	m.editor.Focus()
	m.editor.SetSchema(&aiContext.SchemaExport{
		Schemas: []aiContext.SchemaInfo{{
			Name:   "public",
			Tables: []aiContext.TableInfo{{Name: "users"}},
		}},
	})
	m.editor.SetContent("SELECT * FROM u")
	m.editor.SetCursorPos(0, len("SELECT * FROM u"))
	// Typing a character triggers the popup through the editor's own handler.
	m.editor.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !m.editor.AutocompleteVisible() {
		t.Fatal("precondition failed: autocomplete popup is not visible")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model := updated.(Model)
	if !model.editorOpen {
		t.Fatal("Esc closed the editor while an autocomplete popup was open")
	}
	if model.editor.AutocompleteVisible() {
		t.Fatal("Esc did not cancel the autocomplete popup")
	}

	// A second Esc, with nothing left to cancel, closes the editor.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(Model)
	if model.editorOpen {
		t.Fatal("Esc did not close the editor after the popup was dismissed")
	}
}

// Scenario: "q" cierra la app desde las vistas de navegación.
func TestQuit_FromExplorerQuits(t *testing.T) {
	m := newRollbackTestModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("pressing q in the explorer did not return a command")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatal("pressing q in the explorer did not quit")
	}
}
