package app

import (
	"testing"

	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/editor"
)

func testStyles() *theme.Styles {
	return theme.Resolve("dark").Styles()
}

func newTestModel() Model {
	styles := testStyles()
	ed := editor.NewSQLEditor(styles)
	toast := ui.NewToastManager(styles)
	return Model{
		editor: ed,
		toast:  toast,
	}
}

func TestCopySQLMsg_Copied(t *testing.T) {
	m := newTestModel()
	m.editor.SetContent("SELECT * FROM users WHERE id = 1")
	m.editorOpen = true

	cmd := m.handleCopySQL()
	if cmd == nil {
		t.Fatal("Expected a command from handleCopySQL, got nil")
	}

	// Execute the command
	msg := cmd()
	if _, ok := msg.(copySQLDoneMsg); !ok {
		t.Fatalf("Expected copySQLDoneMsg, got %T", msg)
	}
}

func TestCopySQLMsg_EmptyEditor(t *testing.T) {
	m := newTestModel()
	m.editor.SetContent("")
	m.editorOpen = true

	cmd := m.handleCopySQL()
	if cmd != nil {
		t.Fatal("Expected nil command for empty editor, got a command")
	}
}

func TestCopySQLMsg_PreservesContent(t *testing.T) {
	m := newTestModel()
	originalSQL := "DELETE FROM temp WHERE 1=1"
	m.editor.SetContent(originalSQL)
	m.editorOpen = true

	m.handleCopySQL()

	if m.editor.Content() != originalSQL {
		t.Fatalf("Editor content changed: got %q, want %q", m.editor.Content(), originalSQL)
	}
}
