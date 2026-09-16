package ui

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
)

func testStatusBarKeybinds() map[string]string {
	return map[string]string{
		"editor.execute":      "ctrl+enter",
		"editor.clear":        "ctrl+u",
		"editor.copy":         "ctrl+y",
		"editor.autocomplete": "tab",
		"editor.history_prev": "ctrl+p",
		"editor.history_next": "ctrl+n",
	}
}

func TestStatusBar_EditorOpen_IncludesCopyKeybind(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	s := NewStatusBar(styles, testStatusBarKeybinds())
	s.SetEditorOpen(true)
	s.SetAutocompleteReady(true)

	lines := s.renderContextual()
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Ctrl+Y copy") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("renderContextual() does not contain 'Ctrl+Y copy'. Lines: %v", lines)
	}
}

func TestStatusBar_EditorOpen_NoAutocomplete_IncludesCopyKeybind(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	s := NewStatusBar(styles, testStatusBarKeybinds())
	s.SetEditorOpen(true)
	s.SetAutocompleteReady(false)

	lines := s.renderContextual()
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Ctrl+Y copy") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("renderContextual() does not contain 'Ctrl+Y copy'. Lines: %v", lines)
	}
}

// Scenario: statusbar shows the rollback keybind when a transaction is pending.
func TestStatusBar_TxPending_ShowsRollbackKeybind(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	kbs := testStatusBarKeybinds()
	kbs["global.rollback"] = "U"

	s := NewStatusBar(styles, kbs)
	s.SetTxPending(true)

	actions := s.renderActions()
	if !strings.Contains(actions, "U rollback") {
		t.Fatalf("renderActions() does not contain 'U rollback': %q", actions)
	}
	if !strings.Contains(actions, "tx pending") {
		t.Fatalf("renderActions() does not contain 'tx pending': %q", actions)
	}
}

// Scenario: statusbar hides the rollback indicator when there is no transaction.
func TestStatusBar_NoTx_HidesRollbackIndicator(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	kbs := testStatusBarKeybinds()
	kbs["global.rollback"] = "U"

	s := NewStatusBar(styles, kbs)
	s.SetTxPending(false)

	actions := s.renderActions()
	if strings.Contains(actions, "tx pending") {
		t.Fatalf("renderActions() shows 'tx pending' with no pending transaction: %q", actions)
	}
	if strings.Contains(actions, "rollback") {
		t.Fatalf("renderActions() shows a rollback keybind with no pending transaction: %q", actions)
	}
}
