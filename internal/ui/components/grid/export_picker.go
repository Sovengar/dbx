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
	ExportMarkdown
	ExportCopyCSV
)

// noun is what the picker calls the format in its description line. It lives
// next to the enum so a new option cannot inherit the previous option's wording,
// which is what the old positional updateDescriptions did.
func (f ExportFormat) noun() string {
	switch f {
	case ExportSQL:
		return "INSERT"
	case ExportJSON:
		return "JSON"
	case ExportCSV:
		return "CSV"
	case ExportMarkdown:
		return "Markdown"
	case ExportCopyCSV:
		return "table via COPY"
	}
	return ""
}

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
	styles   *theme.Styles
	visible  bool
	cursor   int
	schema   string
	table    string
	width    int
	height   int
	options  []exportOption
	bulk     exportOption
	row      []interface{}   // single row for clipboard export
	rows     [][]interface{} // multiple rows for file export
	columns  []string
	fileMode bool // true = save to file, false = copy to clipboard
	yankMode bool // true = yank (always clipboard), false = normal export
	bulkMode bool // true = the whole database table, not only the loaded rows
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
			{label: "Markdown", description: "Copy Markdown to clipboard", format: ExportMarkdown},
		},
		bulk: exportOption{label: "COPY CSV", description: "Stream the entire table to file", format: ExportCopyCSV},
	}
}

// updateDescriptions rewrites every option's description for the mode the
// picker was opened in. The wording comes from each option's OWN format, not
// from its position: the positional version had one assignment per option, so a
// fourth option would have silently kept the constructor-era "Copy Markdown to
// clipboard" line in file mode.
func (ep *ExportPicker) updateDescriptions() {
	verb, target := "Copy", "to clipboard"
	if ep.fileMode {
		verb, target = "Save", "to file"
	}
	for i := range ep.options {
		ep.options[i].description = fmt.Sprintf("%s %s %s", verb, ep.options[i].format.noun(), target)
	}
	ep.bulk.description = "Stream the entire table to file"
}

func (ep *ExportPicker) visibleOptions() []exportOption {
	if ep.bulkMode && ep.fileMode && !ep.yankMode {
		return append(append([]exportOption{}, ep.options...), ep.bulk)
	}
	return ep.options
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
	ep.bulkMode = false
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
	ep.bulkMode = false
}

func (ep *ExportPicker) ShowBulkFileMode(schema, table string, rows [][]interface{}, columns []string) {
	ep.ShowFileMode(schema, table, nil, rows, columns)
	ep.bulkMode = true
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
	ep.bulkMode = false
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
	options := ep.visibleOptions()
	switch key {
	case "j", "down":
		if ep.cursor < len(options)-1 {
			ep.cursor++
		}
		return nil, true
	case "k", "up":
		if ep.cursor > 0 {
			ep.cursor--
		}
		return nil, true
	case "enter":
		selected := options[ep.cursor]
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
	for i, opt := range ep.visibleOptions() {
		arrow := "  "
		if i == ep.cursor {
			arrow = lipgloss.NewStyle().
				Foreground(ep.styles.Primary.GetForeground()).
				Bold(true).
				Render("▸")
		}

		label := lipgloss.NewStyle().
			Bold(true).
			Width(10).
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
