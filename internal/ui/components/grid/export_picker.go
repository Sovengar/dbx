package grid

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/keydisplay"
)

type ExportFormat int

const (
	ExportSQL ExportFormat = iota
	ExportJSON
	ExportCSV
)

type ExportSelectedMsg struct {
	Format   ExportFormat
	Schema   string
	Table    string
	Row      []interface{}   // nil for mass export
	Rows     [][]interface{} // nil for single row export
	Columns  []string
	YankMode bool // true = yank (clipboard), false = export (file or clipboard based on row count)
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
	fileMode  bool // true = save to file, false = copy to clipboard
	yankMode  bool // true = yank (always clipboard), false = normal export
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

func (ep *ExportPicker) updateDescriptions() {
	if ep.yankMode {
		ep.options[0].description = "Copy INSERT to clipboard"
		ep.options[1].description = "Copy JSON to clipboard"
		ep.options[2].description = "Copy CSV to clipboard"
	} else if ep.fileMode {
		ep.options[0].description = "Save INSERT to file"
		ep.options[1].description = "Save JSON to file"
		ep.options[2].description = "Save CSV to file"
	} else {
		ep.options[0].description = "Copy INSERT to clipboard"
		ep.options[1].description = "Copy JSON to clipboard"
		ep.options[2].description = "Copy CSV to clipboard"
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
	ep.fileMode = false
	ep.yankMode = false
}

func (ep *ExportPicker) ShowFileMode(schema, table string, row []interface{}, rows [][]interface{}, columns []string) {
	ep.visible = true
	ep.cursor = 0
	ep.schema = schema
	ep.table = table
	ep.row = row
	ep.rows = rows
	ep.columns = columns
	ep.fileMode = true
	ep.yankMode = false
}

func (ep *ExportPicker) ShowYankMode(schema, table string, row []interface{}, rows [][]interface{}, columns []string) {
	ep.visible = true
	ep.cursor = 0
	ep.schema = schema
	ep.table = table
	ep.row = row
	ep.rows = rows
	ep.columns = columns
	ep.fileMode = false
	ep.yankMode = true
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
				Format:   selected.format,
				Schema:   ep.schema,
				Table:    ep.table,
				Row:      ep.row,
				Rows:     ep.rows,
				Columns:  ep.columns,
				YankMode: ep.yankMode,
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

	ep.updateDescriptions()

	var lines []string

	// Title
	action := "Export"
	if ep.yankMode {
		action = "Yank"
	} else if ep.fileMode {
		action = "Export to file"
	}
	title := ep.styles.Header.Render(fmt.Sprintf("%s %s.%s", action, ep.schema, ep.table))
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
	footer := ep.styles.Help.Render(keydisplay.Key("j/k ↑↓   enter copy   esc cancel"))
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
