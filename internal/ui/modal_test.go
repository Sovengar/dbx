package ui

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
)

func testHelpModalKeybinds() map[string]string {
	return map[string]string{
		"editor.execute":      "ctrl+enter",
		"editor.clear":        "ctrl+u",
		"editor.copy":         "ctrl+y",
		"editor.autocomplete": "tab",
		"editor.history_prev": "ctrl+p",
		"editor.history_next": "ctrl+n",
	}
}

func TestHelpModal_EditorSection_IncludesCopySQL(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	modal := NewHelpModal(styles, testHelpModalKeybinds())
	modal.SetWidth(80)
	modal.SetHeight(200)
	modal.Show()

	view := modal.View()
	if !strings.Contains(view, "Copy SQL") {
		t.Fatal("Help modal does not contain 'Copy SQL' in Editor section")
	}
	if !strings.Contains(view, "ctrl+y") {
		t.Fatal("Help modal does not show 'ctrl+y' keybind for Copy SQL")
	}
}
