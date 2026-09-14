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
