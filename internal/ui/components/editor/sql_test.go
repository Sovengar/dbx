package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/theme"
)

func testStyles() *theme.Styles {
	return theme.Resolve("dark").Styles()
}

func TestSQLEditor_CtrlY_WithContent(t *testing.T) {
	styles := testStyles()
	ed := NewSQLEditor(styles)
	ed.SetContent("SELECT * FROM users")
	ed.Focus()

	msg := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	cmd, handled := ed.Update(msg)

	if !handled {
		t.Fatal("Expected ctrl+y to be handled when editor has content")
	}
	if cmd == nil {
		t.Fatal("Expected a command from ctrl+y")
	}

	resultMsg := cmd()
	if _, ok := resultMsg.(CopySQLMsg); !ok {
		t.Fatalf("Expected CopySQLMsg, got %T", resultMsg)
	}
}

func TestSQLEditor_CtrlY_Empty(t *testing.T) {
	styles := testStyles()
	ed := NewSQLEditor(styles)
	ed.SetContent("")
	ed.Focus()

	msg := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	_, handled := ed.Update(msg)

	if handled {
		t.Fatal("Expected ctrl+y to NOT be handled when editor is empty")
	}
}

func TestSQLEditor_CtrlY_Multiline(t *testing.T) {
	styles := testStyles()
	ed := NewSQLEditor(styles)
	ed.SetContent("SELECT u.name, o.total\nFROM users u\nJOIN orders o ON o.user_id = u.id\nWHERE o.total > 100")
	ed.Focus()

	msg := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	cmd, handled := ed.Update(msg)

	if !handled {
		t.Fatal("Expected ctrl+y to be handled with multiline content")
	}
	if cmd == nil {
		t.Fatal("Expected a command from ctrl+y")
	}

	resultMsg := cmd()
	if _, ok := resultMsg.(CopySQLMsg); !ok {
		t.Fatalf("Expected CopySQLMsg, got %T", resultMsg)
	}
}

func TestSQLEditor_CtrlY_PreservesContent(t *testing.T) {
	styles := testStyles()
	ed := NewSQLEditor(styles)
	originalSQL := "DELETE FROM temp WHERE 1=1"
	ed.SetContent(originalSQL)
	ed.Focus()

	msg := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	ed.Update(msg)

	if ed.Content() != originalSQL {
		t.Fatalf("Editor content changed after copy: got %q, want %q", ed.Content(), originalSQL)
	}
}
