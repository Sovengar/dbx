package grid

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
)

type ExportFormat int

const (
	ExportSQL ExportFormat = iota
	ExportJSON
	ExportCSV
)

type ExportSelectedMsg struct {
	Format ExportFormat
	Schema string
	Table  string
	Row    []interface{} // nil for mass export
	Rows   [][]interface{} // nil for single row export
	Columns []string
}

type ExportPicker struct {
	styles    *theme.Styles
	visible   bool
	cursor    int
	schema    string
	table     string
	width     int
	height    int
	options   []exportOption
	row       []interface{}   // single row for clipboard export
	rows      [][]interface{} // multiple rows for file export
	columns   []string
}

type exportOption struct {
	label       string
	description string
	format      ExportFormat
}

func NewExportPicker(styles *theme.Styles) *ExportPicker {
	return &ExportPicker{
		styles:  styles,
		visible: false,
		options: []exportOption{
			{label: "SQL", description: "Copy INSERT to clipboard", format: ExportSQL},
			{label: "JSON", description: "Copy JSON to clipboard", format: ExportJSON},
			{label: "CSV", description: "Copy CSV to clipboard", format: ExportCSV},
		},
	}
}

func (ep *ExportPicker) Show(schema, table string, row []interface{}, rows [][]interface{}, columns []string) {
	ep.visible = true
	ep.cursor = 0
	ep.schema = schema
	ep.table = table
	ep.row = row
	ep.rows = rows
	ep.columns = columns
}

func (ep *ExportPicker) Hide() {
	ep.visible = false
}

func (ep *ExportPicker) IsVisible() bool {
	return ep.visible
}

func (ep *ExportPicker) SetWidth(w int) {
	ep.width = w
}

func (ep *ExportPicker) SetHeight(h int) {
	ep.height = h
}

func (ep *ExportPicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !ep.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return ep.handleKey(msg)
	}
	return nil, false
}

func (ep *ExportPicker) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "j", "down":
		if ep.cursor < len(ep.options)-1 {
			ep.cursor++
		}
		return nil, true
	case "k", "up":
		if ep.cursor > 0 {
			ep.cursor--
		}
		return nil, true
	case "enter":
		selected := ep.options[ep.cursor]
		ep.visible = false
		return func() tea.Msg {
			return ExportSelectedMsg{
				Format:  selected.format,
				Schema:  ep.schema,
				Table:   ep.table,
				Row:     ep.row,
				Rows:    ep.rows,
				Columns: ep.columns,
			}
		}, true
	case "esc", "q":
		ep.visible = false
		return nil, true
	}
	return nil, false
}

func (ep *ExportPicker) View() string {
	if !ep.visible {
		return ""
	}

	var lines []string

	// Title
	title := ep.styles.Header.Render(fmt.Sprintf("Export %s.%s", ep.schema, ep.table))
	lines = append(lines, title)
	lines = append(lines, "")

	// Options
	for i, opt := range ep.options {
		arrow := "  "
		if i == ep.cursor {
			arrow = lipgloss.NewStyle().
				Foreground(ep.styles.Primary.GetForeground()).
				Bold(true).
				Render("▸")
		}

		label := lipgloss.NewStyle().
			Bold(true).
			Width(8).
			Render(opt.label)

		desc := ep.styles.TextMuted.Render(opt.description)

		line := fmt.Sprintf("  %s %s  %s", arrow, label, desc)
		lines = append(lines, line)
	}

	// Footer
	lines = append(lines, "")
	footer := ep.styles.Help.Render("j/k ↑↓   enter copy   esc cancel")
	lines = append(lines, footer)

	content := strings.Join(lines, "\n")

	// Build the box with border
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ep.styles.Primary.GetForeground()).
		Width(48).
		Padding(0, 1)

	box := borderStyle.Render(content)

	return box
}
