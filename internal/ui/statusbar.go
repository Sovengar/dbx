package ui

import (
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/theme"
)

type StatusBar struct {
	styles      *theme.Styles
	keybinds    map[string]string
	width       int
	focus       string
	tableName   string
	schema      string
	rows        int
	editorOpen  bool
	whereClause string
}

func NewStatusBar(styles *theme.Styles, keybinds map[string]string) *StatusBar {
	return &StatusBar{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (s *StatusBar) SetWidth(w int)              { s.width = w }
func (s *StatusBar) SetFocus(f string)           { s.focus = f }
func (s *StatusBar) SetEditorOpen(open bool)     { s.editorOpen = open }
func (s *StatusBar) SetFilter(where string)      { s.whereClause = where }
func (s *StatusBar) SetTable(schema, name string, rows int) {
	s.schema = schema
	s.tableName = name
	s.rows = rows
}

func (s *StatusBar) SetTableName(schema, name string) {
	s.schema = schema
	s.tableName = name
}

func (s *StatusBar) keyFor(action string) string {
	if k, ok := s.keybinds[action]; ok {
		return k
	}
	return "?"
}

func (s *StatusBar) Render() string {
	if s.width <= 0 {
		return ""
	}

	sep := strings.Repeat("─", s.width)
	line1 := s.renderActions()
	contextLines := s.renderContextual()

	var parts []string
	parts = append(parts, s.styles.Sep.Render(sep))
	parts = append(parts, s.styles.Help.Render("  "+line1))
	for _, cl := range contextLines {
		parts = append(parts, s.styles.Help.Render("  "+cl))
	}

	return strings.Join(parts, "\n")
}

func (s *StatusBar) RenderStatus() string {
	return s.styles.TextBright.Render("  " + s.renderStatus())
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
		lines = append(lines, "Ctrl+Enter execute · Ctrl+U clear · Ctrl+P/N history")
	} else {
		switch s.focus {
		case "explorer":
			lines = append(lines, "/ filter · n new · d drop · v DDL · Enter Open table data · Space View columns")
		case "grid":
			lines = append(lines, "1-5 tabs · / filter · n/p N/P page · F1-9 goto · s sort · f find column")
			lines = append(lines, "Enter edit · d delete · i insert · space select · y export · o FK nav · H go back")
		case "grid-preview":
			lines = append(lines, "Tab back · j/k scroll · g/G first/last · e explorer")
		}
	}

	return lines
}

func (s *StatusBar) renderStatus() string {
	var segments []string

	segments = append(segments, "dbx")

	if s.focus != "" {
		segments = append(segments, fmt.Sprintf("[%s]", s.focus))
	}

	if s.tableName != "" {
		if s.schema != "" {
			segments = append(segments, fmt.Sprintf("%s.%s", s.schema, s.tableName))
		} else {
			segments = append(segments, s.tableName)
		}
	}

	if s.rows > 0 {
		segments = append(segments, fmt.Sprintf("%d rows", s.rows))
	}

	if s.whereClause != "" {
		segments = append(segments, fmt.Sprintf("WHERE %s", s.whereClause))
	}

	return strings.Join(segments, " · ")
}
