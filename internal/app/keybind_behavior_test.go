package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
