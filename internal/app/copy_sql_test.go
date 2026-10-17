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

// stubClipboard replaces the clipboard seam for one case and returns a pointer to what it
// was given.
//
// The production seam resolves its helper through PATH, so a case that runs it for real
// hands the statement to the helper on the machine running the suite and the developer's
// own clipboard ends up holding "SELECT * FROM users WHERE id = 1". This file tests
// handleCopySQL's plumbing, not the clipboard, so the real helper must never be reached.
// The pointer is what lets the case assert the PAYLOAD and not just the message type.
func stubClipboard(t *testing.T) *string {
	t.Helper()
	var copied string
	old := copyToClipboard
	copyToClipboard = func(content string) error {
		copied = content
		return nil
	}
	t.Cleanup(func() { copyToClipboard = old })
	return &copied
}

func TestCopySQLMsg_Copied(t *testing.T) {
	copied := stubClipboard(t)

	m := newTestModel()
	const sql = "SELECT * FROM users WHERE id = 1"
	m.editor.SetContent(sql)
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
	if got := *copied; got != sql {
		t.Errorf("the clipboard received %q, want the statement %q", got, sql)
	}
}

func TestCopySQLMsg_EmptyEditor(t *testing.T) {
	stubClipboard(t)

	m := newTestModel()
	m.editor.SetContent("")
	m.editorOpen = true

	cmd := m.handleCopySQL()
	if cmd != nil {
		t.Fatal("Expected nil command for empty editor, got a command")
	}
}

func TestCopySQLMsg_PreservesContent(t *testing.T) {
	copied := stubClipboard(t)

	m := newTestModel()
	originalSQL := "DELETE FROM temp WHERE 1=1"
	m.editor.SetContent(originalSQL)
	m.editorOpen = true

	if cmd := m.handleCopySQL(); cmd != nil {
		cmd()
	}

	if m.editor.Content() != originalSQL {
		t.Fatalf("Editor content changed: got %q, want %q", m.editor.Content(), originalSQL)
	}
	if got := *copied; got != originalSQL {
		t.Errorf("the clipboard received %q, want the original statement %q", got, originalSQL)
	}
}
