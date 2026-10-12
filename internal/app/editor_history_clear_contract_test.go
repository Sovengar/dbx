package app

// Scenario: Tras ejecutar con éxito, el buffer queda vacío y ctrl+p recuerda de inmediato.
//
// The editor history parks its "not browsing" sentinel at historyIdx = len(history), which
// assumes an empty prompt. A successful execution USED to keep the executed SQL in the
// buffer, so that assumption was false: the first ctrl+p loaded the newest entry — the same
// text already on screen — and looked like a no-op. The user had to press it a second time
// before anything happened. The clear on the success path is what makes the first press show
// the last executed query, which is the behaviour this pins.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
)

func TestSuccessfulExecutionEmptiesTheEditorSoHistoryRecallsAtOnce(t *testing.T) {
	m := newModelWithGrid(t)
	m.state = StateMain
	m.editorOpen = true
	m.editor.Focus()
	m.editor.SetContent("SELECT 42")

	// First successful run: the buffer must not survive it, or the sentinel collides.
	out, _ := fold(m, queryExecutedMsg{sql: "SELECT 42", result: &postgres.QueryResult{}})
	if out.editorOpen {
		t.Error("the editor stayed open after the query ran")
	}
	if got := out.editor.Content(); got != "" {
		t.Fatalf("the editor still holds %q after a successful run; the first ctrl+p would reload it", got)
	}

	// A second run, typed fresh, gives the history two entries to walk.
	out.editorOpen = true
	out.editor.Focus()
	out.editor.SetContent("SELECT 7")
	out, _ = fold(out, queryExecutedMsg{sql: "SELECT 7", result: &postgres.QueryResult{}})
	if got := out.editor.Content(); got != "" {
		t.Fatalf("the editor still holds %q after the second run", got)
	}

	// Reopen the way the user does, then recall. The first press is the whole point: it must
	// bring back the query that just ran, not repeat what is already on screen.
	out = reopenEditor(t, out)
	if got := out.editor.Content(); got != "" {
		t.Fatalf("the reopened editor holds %q, want an empty prompt", got)
	}

	first := pressEditorKey(t, out, "ctrl+p")
	if got := first.editor.Content(); got != "SELECT 7" {
		t.Fatalf("the first ctrl+p brought back %q, want the last executed query", got)
	}

	second := pressEditorKey(t, first, "ctrl+p")
	if got := second.editor.Content(); got != "SELECT 42" {
		t.Fatalf("the second ctrl+p brought back %q, want the previous query", got)
	}
}

// reopenEditor toggles the editor with the real key the user presses, so the fixture cannot
// quietly reopen it through a path the app does not offer.
func reopenEditor(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'E'})
	out, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(E) returned %T, want Model", updated)
	}
	if !out.editorOpen {
		t.Fatal("E did not reopen the editor")
	}
	return out
}

// pressEditorKey sends a shortcut straight to the focused editor, which is where the app
// forwards it once the editor owns the keyboard.
func pressEditorKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	msg := keyMsg(t, key)
	if _, handled := m.editor.Update(msg); !handled {
		t.Fatalf("the editor did not handle %q", key)
	}
	return m
}

// keyMsg builds the key press a shortcut names. Only the combos this file uses are needed.
func keyMsg(t *testing.T, key string) tea.KeyPressMsg {
	t.Helper()
	switch key {
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	default:
		t.Fatalf("keyMsg has no case for %q", key)
		return tea.KeyPressMsg{}
	}
}
