package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

type StatusBar struct {
	styles            *theme.Styles
	keybinds          map[string]string
	width             int
	height            int
	focused           bool
	focus             string
	editorOpen        bool
	autocompleteReady bool
}

func NewStatusBar(styles *theme.Styles, keybinds map[string]string) *StatusBar {
	return &StatusBar{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (s *StatusBar) SetWidth(w int)              { s.width = w }
func (s *StatusBar) SetHeight(h int)             { s.height = h }
func (s *StatusBar) SetFocus(f string)           { s.focus = f }
func (s *StatusBar) SetEditorOpen(open bool)     { s.editorOpen = open }
func (s *StatusBar) SetAutocompleteReady(r bool) { s.autocompleteReady = r }
func (s *StatusBar) Focus()                      { s.focused = true }
func (s *StatusBar) Blur()                       { s.focused = false }

func (s *StatusBar) Update(msg tea.Msg) (tea.Cmd, bool) {
	return nil, false
}

func (s *StatusBar) keyFor(action string) string {
	if k, ok := s.keybinds[action]; ok {
		return k
	}
	return "?"
}

func (s *StatusBar) View() string {
	if s.width <= 0 {
		return ""
	}

	innerW := s.width - 2
	line1 := s.renderActions()
	contextLines := s.renderContextual()

	var parts []string
	parts = append(parts, s.styles.Help.Render(line1))
	for _, cl := range contextLines {
		parts = append(parts, s.styles.Help.Render(cl))
	}

	content := strings.Join(parts, "\n")

	return s.styles.BorderActive.
		Width(innerW).
		Height(3).
		MaxHeight(3).
		Render(content)
}

func (s *StatusBar) renderActions() string {
	var segments []string

	segments = append(segments, s.keyFor("global.cycle_focus")+" Toggle explorer")
	segments = append(segments, s.keyFor("global.help")+" help")
	segments = append(segments, s.keyFor("global.quit")+" quit")
	segments = append(segments, s.keyFor("global.focus_editor")+" editor")
	segments = append(segments, ": palette")

	return strings.Join(segments, " · ")
}

func (s *StatusBar) renderContextual() []string {
	var lines []string

	if s.editorOpen {
		if s.autocompleteReady {
			lines = append(lines, "Ctrl+Enter execute · Tab autocomplete · Ctrl+U clear · Ctrl+Y copy · Ctrl+P/N history")
		} else {
			lines = append(lines, "Ctrl+Enter execute · Ctrl+U clear · Ctrl+Y copy · Ctrl+P/N history")
		}
	} else {
		switch s.focus {
		case "explorer":
			lines = append(lines, "/ filter · n new · d drop · v DDL · Enter Open table data · Space Collapse schema · Tab Preview")
		case "grid":
			lines = append(lines, "/ filter · r refresh · n/p N/P page · F1-9 goto · s sort · f find column")
			lines = append(lines, "Enter edit · d delete · i insert · space select · y yank · x export · o FK nav")
			lines = append(lines, "Ctrl+S save · D discard · H go back")
		case "grid-preview":
			lines = append(lines, "Tab/Esc back · j/k navigate · Enter expand FK · g/G first/last · e explorer · / jq")
		case "explorer-preview":
			lines = append(lines, "1-6 tabs · Tab/Esc back to explorer")
		}
	}

	for len(lines) < 2 {
		lines = append(lines, "")
	}

	return lines
}
