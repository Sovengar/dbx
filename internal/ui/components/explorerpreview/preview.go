package explorerpreview

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

type ExplorerPreviewTabChangeMsg struct {
	Tab int
}

type ERENavigateMsg struct {
	Schema string
	Table  string
}

type ExplorerPreview struct {
	styles            *theme.Styles
	tabBar            *TabBar
	activeTab         int
	focused           bool
	width             int
	height            int
	keybinds          config.Resolver
	tableName         string
	schema            string
	columns           []postgres.ColumnInfo
	constraints       []postgres.ConstraintInfo
	foreignKeys       []postgres.ForeignKeyInfo
	indexes           []postgres.IndexInfo
	overview          *postgres.TableOverview
	schemaForeignKeys map[string][]postgres.ForeignKeyInfo
	centerSchema      string
	centerTable       string
	ereNav            *ERDiagramNav
	ereViewport       *ERDiagramViewport
	ereDiagram        *ERDiagram
}

func New(styles *theme.Styles, keybinds config.Resolver) *ExplorerPreview {
	return &ExplorerPreview{
		styles:   styles,
		tabBar:   NewTabBar(styles, keybinds),
		keybinds: keybinds,
	}
}

func (e *ExplorerPreview) Focus()          { e.focused = true }
func (e *ExplorerPreview) Blur()           { e.focused = false }
func (e *ExplorerPreview) IsFocused() bool { return e.focused }

func (e *ExplorerPreview) SetWidth(w int)  { e.width = w }
func (e *ExplorerPreview) SetHeight(h int) { e.height = h }

func (e *ExplorerPreview) ActiveTab() int      { return e.activeTab }
func (e *ExplorerPreview) ActiveTabID() string { return e.tabBar.ActiveID() }

func (e *ExplorerPreview) SetData(
	schema, table string,
	columns []postgres.ColumnInfo,
	constraints []postgres.ConstraintInfo,
	foreignKeys []postgres.ForeignKeyInfo,
	indexes []postgres.IndexInfo,
	overview *postgres.TableOverview,
) {
	e.schema = schema
	e.tableName = table
	e.columns = columns
	e.constraints = constraints
	e.foreignKeys = foreignKeys
	e.indexes = indexes
	e.overview = overview
	e.centerSchema = schema
	e.centerTable = table
	e.buildEREDiagram()
}

func (e *ExplorerPreview) SetOverview(overview *postgres.TableOverview) {
	e.overview = overview
}

func (e *ExplorerPreview) SetColumns(columns []postgres.ColumnInfo) {
	e.columns = columns
}

func (e *ExplorerPreview) SetConstraints(constraints []postgres.ConstraintInfo) {
	e.constraints = constraints
}

func (e *ExplorerPreview) SetForeignKeys(foreignKeys []postgres.ForeignKeyInfo) {
	e.foreignKeys = foreignKeys
}

func (e *ExplorerPreview) SetIndexes(indexes []postgres.IndexInfo) {
	e.indexes = indexes
}

func (e *ExplorerPreview) SetSchemaForeignKeys(schemaFKs map[string][]postgres.ForeignKeyInfo) {
	e.schemaForeignKeys = schemaFKs
}

func (e *ExplorerPreview) CenterTable() string {
	if e.centerTable != "" {
		return e.centerTable
	}
	return e.tableName
}

func (e *ExplorerPreview) CenterSchema() string {
	if e.centerSchema != "" {
		return e.centerSchema
	}
	return e.schema
}

func (e *ExplorerPreview) HasData() bool {
	return e.tableName != ""
}

func (e *ExplorerPreview) TableName() string {
	return e.tableName
}

func (e *ExplorerPreview) buildEREDiagram() {
	if e.centerTable == "" || e.centerSchema == "" {
		return
	}
	diagram := BuildERDiagram(
		e.centerSchema,
		e.centerTable,
		e.columns,
		e.constraints,
		e.foreignKeys,
		e.schemaForeignKeys,
	)
	e.ereDiagram = &diagram
	e.ereNav = NewERDiagramNav(len(diagram.Incoming), len(diagram.Outgoing))
	// Content height: center box (6 lines) + spacing (1) + columns (each box ~5 lines + 1 spacing)
	colHeight := 0
	for _, count := range [2]int{len(diagram.Incoming), len(diagram.Outgoing)} {
		h := count * 6 // 5 lines per box + 1 spacing
		if h > colHeight {
			colHeight = h
		}
	}
	contentHeight := 7 + 1 + colHeight // center + spacing + columns
	e.ereViewport = NewERDiagramViewport(e.height, contentHeight)
}

func (e *ExplorerPreview) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !e.focused {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()

		// Tab keys — switch tabs within explorer-preview
		if e.keybinds != nil {
			if action, ok := e.keybinds.Resolve(key, config.ContextExplorerPreview); ok {
				switch action {
				case "overview_tab":
					e.setActiveTab(0)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 0} }, true
				case "columns_tab":
					e.setActiveTab(1)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 1} }, true
				case "constraints_tab":
					e.setActiveTab(2)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 2} }, true
				case "foreign_keys_tab":
					e.setActiveTab(3)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 3} }, true
				case "indexes_tab":
					e.setActiveTab(4)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 4} }, true
				case "ere_tab":
					e.setActiveTab(5)
					return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 5} }, true
				}
			}
		}

		// ERE navigation keys (only when ERE tab is active)
		if e.activeTab == 5 && e.ereNav != nil {
			switch key {
			case "up", "k":
				e.ereNav.MoveUp()
				e.ensureERESelectionVisible()
				return nil, true
			case "down", "j":
				e.ereNav.MoveDown()
				e.ensureERESelectionVisible()
				return nil, true
			case "left", "h":
				e.ereNav.MoveLeft()
				e.ensureERESelectionVisible()
				return nil, true
			case "right", "l":
				e.ereNav.MoveRight()
				e.ensureERESelectionVisible()
				return nil, true
			case "g":
				if e.ereViewport != nil {
					e.ereViewport.JumpTop()
				}
				return nil, true
			case "G":
				if e.ereViewport != nil {
					e.ereViewport.JumpBottom()
				}
				return nil, true
			case "enter":
				if e.ereNav.HasSelection() {
					if rel := e.ereNav.GetSelectedRelationship(e.ereDiagram); rel != nil {
						newTable := rel.ToTable
						newSchema := e.centerSchema
						if idx := strings.Index(newTable, "."); idx > 0 {
							newSchema = newTable[:idx]
							newTable = newTable[idx+1:]
						}
						e.centerSchema = newSchema
						e.centerTable = newTable
						e.buildEREDiagram()
						if e.ereViewport != nil {
							e.ereViewport.Reset()
						}
						return func() tea.Msg {
							return ERENavigateMsg{Schema: newSchema, Table: newTable}
						}, true
					}
				}
				return nil, true
			}
		}
	}

	return nil, false
}

func (e *ExplorerPreview) setActiveTab(tab int) {
	e.activeTab = tab
	e.tabBar.SetActive(tab)
}

func (e *ExplorerPreview) View() string {
	if !e.HasData() {
		return e.styles.Text.Render("  Select a table to view details")
	}

	tabBar := e.tabBar.Render()

	var content string
	switch e.activeTab {
	case 0:
		content = e.renderOverview()
	case 1:
		content = e.renderColumns()
	case 2:
		content = e.renderConstraints()
	case 3:
		content = e.renderForeignKeys()
	case 4:
		content = e.renderIndexes()
	case 5:
		content = e.renderERE()
	}

	return tabBar + "\n" + content
}

func (e *ExplorerPreview) renderERE() string {
	if e.ereDiagram == nil {
		return e.styles.TextMuted.Render("  No data available for ERE diagram")
	}

	fullOutput := RenderERDiagram(*e.ereDiagram, e.width, e.height, e.ereNav)

	// Apply scroll
	if e.ereViewport != nil {
		lines := strings.Split(fullOutput, "\n")
		start := e.ereViewport.ScrollOffset
		end := start + e.ereViewport.PaneHeight
		if end > len(lines) {
			end = len(lines)
		}
		if start > len(lines) {
			start = len(lines)
		}
		return strings.Join(lines[start:end], "\n")
	}

	return fullOutput
}

func (e *ExplorerPreview) ensureERESelectionVisible() {
	if e.ereViewport == nil || e.ereNav == nil || !e.ereNav.HasSelection() {
		return
	}

	// Estimate line of selected item: center (7) + spacing (1) + row * 6
	row := e.ereNav.ActiveRow()
	if row < 0 {
		return
	}
	selLine := 8 + row*6 // rough: center box ~7 lines + 1 spacing + row*6 per box

	// Adjust scroll to keep selection visible
	if selLine < e.ereViewport.ScrollOffset {
		e.ereViewport.ScrollOffset = selLine
	} else if selLine >= e.ereViewport.ScrollOffset+e.ereViewport.PaneHeight {
		e.ereViewport.ScrollOffset = selLine - e.ereViewport.PaneHeight + 1
	}
}

func (e *ExplorerPreview) renderOverview() string {
	if e.overview == nil {
		return e.styles.Text.Render("  Loading overview...")
	}

	var lines []string

	// Table info
	lines = append(lines, e.styles.Primary.Render("  Table"))
	lines = append(lines, fmt.Sprintf("    Name:     %s", e.styles.Text.Render(e.overview.TableName)))
	lines = append(lines, fmt.Sprintf("    Type:     %s", e.styles.Text.Render(e.overview.TableType)))
	if e.overview.Comment != nil {
		lines = append(lines, fmt.Sprintf("    Comment:  %s", e.styles.Text.Render(*e.overview.Comment)))
	}
	lines = append(lines, "")

	// Sizes
	lines = append(lines, e.styles.Primary.Render("  Storage"))
	lines = append(lines, fmt.Sprintf("    Total:    %s", e.styles.Info.Render(e.overview.TotalSize)))
	lines = append(lines, fmt.Sprintf("    Table:    %s", e.styles.Text.Render(e.overview.TableSize)))
	lines = append(lines, fmt.Sprintf("    Indexes:  %s", e.styles.Text.Render(e.overview.IndexSize)))
	lines = append(lines, "")

	// Statistics
	lines = append(lines, e.styles.Primary.Render("  Statistics"))
	if e.overview.LiveTuples >= 0 {
		lines = append(lines, fmt.Sprintf("    Live rows:  %s", e.styles.Success.Render(fmt.Sprintf("%d", e.overview.LiveTuples))))
		lines = append(lines, fmt.Sprintf("    Dead rows:  %s", e.styles.Warning.Render(fmt.Sprintf("%d", e.overview.DeadTuples))))
	} else {
		lines = append(lines, "    Live rows:  —")
		lines = append(lines, "    Dead rows:  —")
	}
	lines = append(lines, "")

	// Maintenance
	lines = append(lines, e.styles.Primary.Render("  Maintenance"))
	lines = append(lines, fmt.Sprintf("    Last vacuum:      %s", formatTime(e.overview.LastVacuum)))
	lines = append(lines, fmt.Sprintf("    Last autovacuum:  %s", formatTime(e.overview.LastAutovacuum)))
	lines = append(lines, fmt.Sprintf("    Last analyze:     %s", formatTime(e.overview.LastAnalyze)))
	lines = append(lines, fmt.Sprintf("    Last autoanalyze: %s", formatTime(e.overview.LastAutoanalyze)))
	lines = append(lines, "")

	// Summary counts
	lines = append(lines, e.styles.Primary.Render("  Summary"))
	lines = append(lines, fmt.Sprintf("    Columns:      %d", e.overview.ColumnCount))
	lines = append(lines, fmt.Sprintf("    Constraints:  %d", e.overview.ConstraintCount))
	lines = append(lines, fmt.Sprintf("    Indexes:      %d", e.overview.IndexCount))
	lines = append(lines, fmt.Sprintf("    Foreign Keys: %d", e.overview.FKCount))

	return strings.Join(lines, "\n")
}

func (e *ExplorerPreview) renderColumns() string {
	if len(e.columns) == 0 {
		return e.styles.Text.Render("  No columns loaded")
	}

	header := e.styles.Header.Render(fmt.Sprintf("  %-30s %-20s %-10s %s", strings.ToUpper("Name"), strings.ToUpper("Type"), strings.ToUpper("Nullable"), strings.ToUpper("Default")))

	var rows []string
	for _, col := range e.columns {
		defaultVal := "NULL"
		if col.Default != nil {
			defaultVal = *col.Default
		}
		line := e.styles.Text.Render(fmt.Sprintf("  %-30s %-20s %-10s %s", col.Name, col.DataType, col.IsNullable, defaultVal))
		rows = append(rows, line)
	}

	return header + "\n" + strings.Join(rows, "\n")
}

func (e *ExplorerPreview) renderConstraints() string {
	if len(e.constraints) == 0 {
		return e.styles.Text.Render("  No constraints loaded")
	}

	header := e.styles.Header.Render(fmt.Sprintf("  %-30s %-20s %s", strings.ToUpper("Name"), strings.ToUpper("Type"), strings.ToUpper("Columns")))

	var rows []string
	for _, c := range e.constraints {
		line := e.styles.Text.Render(fmt.Sprintf("  %-30s %-20s %s", c.Name, c.Type, c.Columns))
		rows = append(rows, line)
	}

	return header + "\n" + strings.Join(rows, "\n")
}

func (e *ExplorerPreview) renderForeignKeys() string {
	if len(e.foreignKeys) == 0 {
		return e.styles.Text.Render("  No foreign keys loaded")
	}

	header := e.styles.Header.Render(fmt.Sprintf("  %-30s %-20s %-20s %s", strings.ToUpper("Name"), strings.ToUpper("Column"), strings.ToUpper("Ref Table"), strings.ToUpper("Ref Column")))

	var rows []string
	for _, fk := range e.foreignKeys {
		line := e.styles.Text.Render(fmt.Sprintf("  %-30s %-20s %-20s %s", fk.Name, fk.Column, fk.RefTable, fk.RefColumn))
		rows = append(rows, line)
	}

	return header + "\n" + strings.Join(rows, "\n")
}

func (e *ExplorerPreview) renderIndexes() string {
	if len(e.indexes) == 0 {
		return e.styles.Text.Render("  No indexes loaded")
	}

	header := e.styles.Header.Render(fmt.Sprintf("  %-30s %-10s %s", strings.ToUpper("Name"), strings.ToUpper("Unique"), strings.ToUpper("Definition")))

	var rows []string
	for _, idx := range e.indexes {
		unique := "NO"
		if idx.Unique {
			unique = "YES"
		}
		line := e.styles.Text.Render(fmt.Sprintf("  %-30s %-10s %s", idx.Name, unique, idx.Def))
		rows = append(rows, line)
	}

	return header + "\n" + strings.Join(rows, "\n")
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Format("2006-01-02 15:04:05")
}

func debugLogERE(diagram *ERDiagram, selectedIdx int) {
	f, err := os.OpenFile("/tmp/dbx_ere_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	centerColsPKFK := 0
	for _, col := range diagram.Center.Columns {
		if col.IsPK || col.IsFK {
			centerColsPKFK++
		}
	}

	fmt.Fprintf(f, "ERE: center=%s centerCols=%d(out of all) pkfkCols=%d outgoing=%d incoming=%d selectedIdx=%d\n",
		diagram.Center.Name,
		len(diagram.Center.Columns),
		centerColsPKFK,
		len(diagram.Outgoing),
		len(diagram.Incoming),
		selectedIdx,
	)
}
