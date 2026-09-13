package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
)

type HelpModal struct {
	styles    *theme.Styles
	keybinds  map[string]string
	visible   bool
	scroll    int
	width     int
	height    int
}

func NewHelpModal(styles *theme.Styles, keybinds map[string]string) *HelpModal {
	return &HelpModal{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (m *HelpModal) Show() {
	m.visible = true
	m.scroll = 0
}

func (m *HelpModal) Hide() {
	m.visible = false
}

func (m *HelpModal) IsVisible() bool {
	return m.visible
}

func (m *HelpModal) SetWidth(w int)  { m.width = w }
func (m *HelpModal) SetHeight(h int) { m.height = h }

func (m *HelpModal) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !m.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		switch key {
		case "esc", "?", "q":
			m.Hide()
			return nil, true
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
			return nil, true
		case "down", "j":
			m.scroll++
			return nil, true
		case "ctrl+u":
			m.scroll -= 10
			if m.scroll < 0 {
				m.scroll = 0
			}
			return nil, true
		case "ctrl+d":
			m.scroll += 10
			return nil, true
		case "g":
			m.scroll = 0
			return nil, true
		case "G":
			m.scroll = 1000
			return nil, true
		}
	case tea.MouseWheelMsg:
		if msg.Y < 0 {
			m.scroll--
			if m.scroll < 0 {
				m.scroll = 0
			}
		} else {
			m.scroll++
		}
		return nil, true
	}

	return nil, false
}

func (m *HelpModal) View() string {
	if !m.visible {
		return ""
	}

	modalW := m.width * 7 / 10
	if modalW < 50 {
		modalW = 50
	}
	if modalW > 90 {
		modalW = 90
	}

	contentW := modalW - 4

	var lines []string

	lines = append(lines, m.styles.Header.Render("Global"))
	lines = append(lines, m.renderKeybind("global.quit", "Quit"))
	lines = append(lines, m.renderKeybind("global.help", "Help"))
	lines = append(lines, m.renderKeybind("global.palette", "Command Palette"))
	lines = append(lines, m.renderKeybind("global.cycle_focus", "Toggle Explorer"))
	lines = append(lines, m.renderKeybind("global.focus_editor", "Toggle Editor"))
	lines = append(lines, m.renderKeybind("global.export", "Export"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Explorer"))
	lines = append(lines, m.renderKeybind("explorer.down", "Navigate Down"))
	lines = append(lines, m.renderKeybind("explorer.up", "Navigate Up"))
	lines = append(lines, m.renderKeybind("explorer.expand", "Load Table"))
	lines = append(lines, m.renderKeybind("explorer.toggle_columns", "Collapse Schema"))
	lines = append(lines, m.renderKeybind("explorer.collapse", "Collapse / Go to Parent"))
	lines = append(lines, m.renderKeybind("explorer.first", "First Node"))
	lines = append(lines, m.renderKeybind("explorer.last", "Last Node"))
	lines = append(lines, m.renderKeybind("explorer.filter", "Filter Tables"))
	lines = append(lines, m.renderKeybind("explorer.new", "New Table"))
	lines = append(lines, m.renderKeybind("explorer.drop", "Drop Table"))
	lines = append(lines, m.renderKeybind("explorer.view_ddl", "View DDL"))
	lines = append(lines, m.renderKeybind("explorer.refresh", "Refresh Schema"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Grid"))
	lines = append(lines, m.renderKeybind("grid.down", "Row Down"))
	lines = append(lines, m.renderKeybind("grid.up", "Row Up"))
	lines = append(lines, m.renderKeybind("grid.left", "Column Left"))
	lines = append(lines, m.renderKeybind("grid.right", "Column Right"))
	lines = append(lines, m.renderKeybind("grid.first", "First Row"))
	lines = append(lines, m.renderKeybind("grid.last", "Last Row"))
	lines = append(lines, m.renderKeybind("grid.half_up", "Half Page Up"))
	lines = append(lines, m.renderKeybind("grid.half_down", "Half Page Down"))
	lines = append(lines, m.renderKeybind("grid.next_page", "Next Page"))
	lines = append(lines, m.renderKeybind("grid.prev_page", "Previous Page"))
	lines = append(lines, m.renderKeybind("grid.first_page", "First Page"))
	lines = append(lines, m.renderKeybind("grid.last_page", "Last Page"))
	lines = append(lines, m.renderKeybind("grid.goto_page", "Go to Page"))
	lines = append(lines, m.renderKeybind("grid.edit_cell", "Edit Cell"))
	lines = append(lines, m.renderKeybind("grid.delete_row", "Delete Row(s)"))
	lines = append(lines, m.renderKeybind("grid.insert_row", "Insert Row"))
	lines = append(lines, m.renderKeybind("grid.select_row", "Select Row"))
	lines = append(lines, m.renderKeybind("grid.yank", "Export (SQL/JSON/CSV)"))
	lines = append(lines, m.renderKeybind("grid.sort", "Sort Column"))
	lines = append(lines, m.renderKeybind("grid.find_and_jump_to_column", "Find & Jump to Column"))
	lines = append(lines, m.renderKeybind("grid.filter", "Filter Rows"))
	lines = append(lines, m.renderKeybind("grid.focus_preview", "Focus Preview"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Explorer Preview"))
	lines = append(lines, m.renderKeybind("explorer.tab_overview", "Overview Tab"))
	lines = append(lines, m.renderKeybind("explorer.tab_columns", "Columns Tab"))
	lines = append(lines, m.renderKeybind("explorer.tab_constraints", "Constraints Tab"))
	lines = append(lines, m.renderKeybind("explorer.tab_foreign_keys", "Foreign Keys Tab"))
	lines = append(lines, m.renderKeybind("explorer.tab_indexes", "Indexes Tab"))
	lines = append(lines, m.renderKeybind("explorer.tab_ere", "ERE Diagram Tab"))
	lines = append(lines, "  tab         Toggle Explorer/Preview")
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Grid Preview"))
	lines = append(lines, m.renderKeybind("grid-preview.cursor_up", "Navigate Up"))
	lines = append(lines, m.renderKeybind("grid-preview.cursor_down", "Navigate Down"))
	lines = append(lines, m.renderKeybind("grid-preview.expand", "Expand FK / Collapse"))
	lines = append(lines, m.renderKeybind("grid-preview.first", "First Line"))
	lines = append(lines, m.renderKeybind("grid-preview.last", "Last Line"))
	lines = append(lines, m.renderKeybind("grid-preview.half_up", "Half Page Up"))
	lines = append(lines, m.renderKeybind("grid-preview.half_down", "Half Page Down"))
	lines = append(lines, m.renderKeybind("grid-preview.toggle_explorer", "Focus Explorer"))
	lines = append(lines, m.renderKeybind("grid-preview.jq_filter", "JQ Filter"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Editor"))
	lines = append(lines, m.renderKeybind("editor.execute", "Execute Query"))
	lines = append(lines, m.renderKeybind("editor.clear", "Clear Editor"))
	lines = append(lines, m.renderKeybind("editor.autocomplete", "Autocomplete"))
	lines = append(lines, m.renderKeybind("editor.history_prev", "History Previous"))
	lines = append(lines, m.renderKeybind("editor.history_next", "History Next"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Help.Render("  j/k scroll · Esc close"))

	maxVisible := m.height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}

	if m.scroll > len(lines)-maxVisible {
		m.scroll = len(lines) - maxVisible
	}
	if m.scroll < 0 {
		m.scroll = 0
	}

	end := m.scroll + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	visibleLines := lines[m.scroll:end]
	content := strings.Join(visibleLines, "\n")

	modal := m.styles.BorderActive.
		Width(contentW).
		Render(m.styles.Header.Render("Help") + "\n" + content)

	return overlayModal(m.width, m.height, modal)
}

func (m *HelpModal) renderKeybind(action, description string) string {
	key := m.keybinds[action]
	if key == "" {
		key = "?"
	}

	keyStyled := m.styles.Primary.Render(fmt.Sprintf("%-12s", key))
	descStyled := m.styles.Text.Render(description)

	return "  " + keyStyled + descStyled
}

func overlayModal(width, height int, modal string) string {
	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
