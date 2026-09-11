package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/theme"
)

type SQLEditor struct {
	styles     *theme.Styles
	lines      []string
	cursorRow  int
	cursorCol  int
	width      int
	height     int
	focused    bool
	history    []string
	historyIdx int
modified   bool
}

func NewSQLEditor(styles *theme.Styles) *SQLEditor {
	return &SQLEditor{
		styles: styles,
		lines:  []string{""},
	}
}

func (e *SQLEditor) SetWidth(w int)  { e.width = w }
func (e *SQLEditor) SetHeight(h int) { e.height = h }
func (e *SQLEditor) Focus()          { e.focused = true }
func (e *SQLEditor) Blur()           { e.focused = false }

func (e *SQLEditor) Content() string {
	return strings.Join(e.lines, "\n")
}

func (e *SQLEditor) Clear() {
	e.lines = []string{""}
	e.cursorRow = 0
	e.cursorCol = 0
	e.modified = false
}

func (e *SQLEditor) SetContent(s string) {
	e.lines = strings.Split(s, "\n")
	if len(e.lines) == 0 {
		e.lines = []string{""}
	}
	e.cursorRow = 0
	e.cursorCol = 0
	e.modified = false
}

func (e *SQLEditor) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !e.focused {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return e.handleKey(msg)
	}

	return nil, false
}

func (e *SQLEditor) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	switch key {
	case "ctrl+enter":
		return nil, false // handled by parent

	case "ctrl+r":
		return nil, false // handled by parent

	case "ctrl+u":
		e.Clear()
		return nil, true

	case "ctrl+p":
		e.historyPrev()
		return nil, true

	case "ctrl+n":
		e.historyNext()
		return nil, true

	case "up":
		if e.cursorRow > 0 {
			e.cursorRow--
			e.clampCol()
		}
		return nil, true

	case "down":
		if e.cursorRow < len(e.lines)-1 {
			e.cursorRow++
			e.clampCol()
		}
		return nil, true

	case "left":
		e.moveLeft()
		return nil, true

	case "right":
		e.moveRight()
		return nil, true

	case "home", "0":
		e.cursorCol = 0
		return nil, true

	case "end", "$":
		e.cursorCol = len(e.lines[e.cursorRow])
		return nil, true

	case "enter":
		e.insertNewline()
		return nil, true

	case "backspace":
		e.deleteBackward()
		return nil, true

	case "delete":
		e.deleteForward()
		return nil, true

	case "space":
		e.insertText(" ")
		return nil, true

	case "tab":
		e.insertText("    ")
		return nil, true

	default:
		if len(msg.Text) > 0 {
			e.insertText(msg.Text)
			return nil, true
		}
	}

	return nil, false
}

func (e *SQLEditor) moveLeft() {
	if e.cursorCol > 0 {
		e.cursorCol--
	} else if e.cursorRow > 0 {
		e.cursorRow--
		e.cursorCol = len(e.lines[e.cursorRow])
	}
}

func (e *SQLEditor) moveRight() {
	if e.cursorCol < len(e.lines[e.cursorRow]) {
		e.cursorCol++
	} else if e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.cursorCol = 0
	}
}

func (e *SQLEditor) insertText(s string) {
	line := e.lines[e.cursorRow]
	e.lines[e.cursorRow] = line[:e.cursorCol] + s + line[e.cursorCol:]
	e.cursorCol += len(s)
	e.modified = true
}

func (e *SQLEditor) insertNewline() {
	line := e.lines[e.cursorRow]
	rest := line[e.cursorCol:]
	e.lines[e.cursorRow] = line[:e.cursorCol]
	e.cursorRow++
	e.lines = append(e.lines[:e.cursorRow], append([]string{rest}, e.lines[e.cursorRow:]...)...)
	e.cursorCol = 0
	e.modified = true
}

func (e *SQLEditor) deleteBackward() {
	if e.cursorCol > 0 {
		line := e.lines[e.cursorRow]
		e.lines[e.cursorRow] = line[:e.cursorCol-1] + line[e.cursorCol:]
		e.cursorCol--
		e.modified = true
	} else if e.cursorRow > 0 {
		prevLen := len(e.lines[e.cursorRow-1])
		e.lines[e.cursorRow-1] += e.lines[e.cursorRow]
		e.lines = append(e.lines[:e.cursorRow], e.lines[e.cursorRow+1:]...)
		e.cursorRow--
		e.cursorCol = prevLen
		e.modified = true
	}
}

func (e *SQLEditor) deleteForward() {
	line := e.lines[e.cursorRow]
	if e.cursorCol < len(line) {
		e.lines[e.cursorRow] = line[:e.cursorCol] + line[e.cursorCol+1:]
		e.modified = true
	} else if e.cursorRow < len(e.lines)-1 {
		e.lines[e.cursorRow] += e.lines[e.cursorRow+1]
		e.lines = append(e.lines[:e.cursorRow+1], e.lines[e.cursorRow+2:]...)
		e.modified = true
	}
}

func (e *SQLEditor) clampCol() {
	max := len(e.lines[e.cursorRow])
	if e.cursorCol > max {
		e.cursorCol = max
	}
}

func (e *SQLEditor) historyPrev() {
	if len(e.history) == 0 {
		return
	}
	if e.historyIdx < 0 {
		e.historyIdx = len(e.history) - 1
	} else if e.historyIdx > 0 {
		e.historyIdx--
	}
	if e.historyIdx >= 0 && e.historyIdx < len(e.history) {
		e.lines = strings.Split(e.history[e.historyIdx], "\n")
		e.cursorRow = 0
		e.cursorCol = 0
	}
}

func (e *SQLEditor) historyNext() {
	if len(e.history) == 0 || e.historyIdx < 0 {
		return
	}
	if e.historyIdx < len(e.history)-1 {
		e.historyIdx++
		e.lines = strings.Split(e.history[e.historyIdx], "\n")
		e.cursorRow = 0
		e.cursorCol = 0
	} else {
		e.historyIdx = len(e.history)
		e.lines = []string{""}
		e.cursorRow = 0
		e.cursorCol = 0
	}
}

func (e *SQLEditor) PushHistory(sql string) {
	if sql == "" {
		return
	}
	e.history = append(e.history, sql)
	if len(e.history) > 100 {
		e.history = e.history[len(e.history)-100:]
	}
	e.historyIdx = len(e.history)
}

func (e *SQLEditor) View() string {
	if e.width <= 0 || e.height <= 0 {
		return ""
	}

	var lines []string
	for i, line := range e.lines {
		rendered := HighlightSQL(line, e.styles)
		if i == e.cursorRow && e.focused {
			// Add cursor indicator
			if e.cursorCol <= len(line) {
				before := line[:e.cursorCol]
				cursor := " "
				after := ""
				if e.cursorCol < len(line) {
					after = line[e.cursorCol+1:]
					cursor = string(line[e.cursorCol])
				}
				rendered = HighlightSQL(before, e.styles) +
					e.styles.Selected.Render(cursor) +
					HighlightSQL(after, e.styles)
			}
		}
		lines = append(lines, rendered)
	}

	// Pad to fill height
	for len(lines) < e.height {
		lines = append(lines, "")
	}

	// Truncate to height
	if len(lines) > e.height {
		lines = lines[:e.height]
	}

	return strings.Join(lines, "\n")
}
