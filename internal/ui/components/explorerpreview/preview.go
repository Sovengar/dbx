package explorerpreview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

type ExplorerPreviewTabChangeMsg struct {
	Tab int
}

type ExplorerPreview struct {
	styles       *theme.Styles
	tabBar       *TabBar
	activeTab    int
	focused      bool
	width        int
	height       int
	keybinds     map[string]string
	tableName    string
	schema       string
	columns      []postgres.ColumnInfo
	constraints  []postgres.ConstraintInfo
	foreignKeys  []postgres.ForeignKeyInfo
	indexes      []postgres.IndexInfo
	overview     *postgres.TableOverview
}

func New(styles *theme.Styles, keybinds map[string]string) *ExplorerPreview {
	return &ExplorerPreview{
		styles:   styles,
		tabBar:   NewTabBar(styles, keybinds),
		keybinds: keybinds,
	}
}

func (e *ExplorerPreview) Focus()  { e.focused = true }
func (e *ExplorerPreview) Blur()   { e.focused = false }
func (e *ExplorerPreview) IsFocused() bool { return e.focused }

func (e *ExplorerPreview) SetWidth(w int)  { e.width = w }
func (e *ExplorerPreview) SetHeight(h int) { e.height = h }

func (e *ExplorerPreview) ActiveTab() int    { return e.activeTab }
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

func (e *ExplorerPreview) HasData() bool {
	return e.tableName != ""
}

func (e *ExplorerPreview) TableName() string {
	return e.tableName
}

func (e *ExplorerPreview) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !e.focused {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()

		// Tab keys — switch tabs within explorer-preview
		if key == e.keybinds["explorer.tab_overview"] {
			e.setActiveTab(0)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 0} }, true
		}
		if key == e.keybinds["explorer.tab_columns"] {
			e.setActiveTab(1)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 1} }, true
		}
		if key == e.keybinds["explorer.tab_constraints"] {
			e.setActiveTab(2)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 2} }, true
		}
		if key == e.keybinds["explorer.tab_foreign_keys"] {
			e.setActiveTab(3)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 3} }, true
		}
		if key == e.keybinds["explorer.tab_indexes"] {
			e.setActiveTab(4)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 4} }, true
		}
		if key == e.keybinds["explorer.tab_ere"] {
			e.setActiveTab(5)
			return func() tea.Msg { return ExplorerPreviewTabChangeMsg{Tab: 5} }, true
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
	return e.styles.TextMuted.Render("  Entity-Relationship diagram — coming soon")
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
