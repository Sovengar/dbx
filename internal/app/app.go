package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/buble/dbx/internal/ui/components/picker"
	"github.com/buble/dbx/internal/ui/components/preview"
	"github.com/jackc/pgx/v5"
)

type AppState int

const (
	StatePicker AppState = iota
	StateLoading
	StateMain
	StateError
)

type Model struct {
	state           AppState
	config          *config.Config
	theme           *theme.Theme
	styles          *theme.Styles
	picker          *picker.Picker
	explorer        *explorer.Explorer
	grid            *grid.Grid
	editor          *editor.SQLEditor
	preview         *preview.Preview
	router          *Router
	keybindRegistry *config.KeybindRegistry
	keybinds        map[string]string
	palette         *palette.Palette
	helpModal       *ui.HelpModal
	toast           *ui.ToastManager
	statusbar       *ui.StatusBar
	project         *config.FoundProject
	conn            *pgx.Conn
	width           int
	height          int
	err             error
	editorOpen      bool
	queryExecuting  bool
	lastClickTime   time.Time
	lastClickX      int
	lastClickY      int
	navStack        []NavigationEntry
	prevSchema      string
	prevTable       string
}

func NewModel(cfg *config.Config) Model {
	t := theme.Resolve(cfg.Theme.Mode)
	kbr := config.NewKeybindRegistry(cfg.Keybindings)
	kbs := kbr.Flatten()
	pageSize := 100
	if cfg.UI.PageSize > 0 {
		pageSize = cfg.UI.PageSize
	}
	return Model{
		config:          cfg,
		theme:           t,
		styles:          t.Styles(),
		picker:          picker.New(t.Styles()),
		grid:            grid.New(t.Styles(), pageSize, kbs),
		editor:          editor.NewSQLEditor(t.Styles()),
		preview:         preview.New(t.Styles()),
		router:          NewRouter(kbs),
		keybindRegistry: kbr,
		keybinds:        kbs,
		palette:         palette.New(t.Styles(), kbs),
		helpModal:       ui.NewHelpModal(t.Styles(), kbs),
		toast:           ui.NewToastManager(t.Styles()),
		statusbar:       ui.NewStatusBar(t.Styles(), kbs),
		state:           StatePicker,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scanProjects(), tickToast())
}

func (m Model) scanProjects() tea.Cmd {
	return func() tea.Msg {
		homeDir, _ := config.GetHomeDir()
		rootDir := homeDir + "/dev"

		scanner := config.NewScanner(rootDir)
		projects := scanner.Scan()

		return projectsScannedMsg{projects: projects}
	}
}

type projectsScannedMsg struct {
	projects []config.FoundProject
}

type toastTickMsg struct{}

func tickToast() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return toastTickMsg{}
	})
}

func (m Model) connectToDB(project config.FoundProject) tea.Cmd {
	return func() tea.Msg {
		dsn := project.Connection.GetDSN()
		if dsn == "" {
			return dbConnectedMsg{err: fmt.Errorf("no DSN provided")}
		}

		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return dbConnectedMsg{err: fmt.Errorf("failed to connect: %w", err)}
		}

		if err := conn.Ping(ctx); err != nil {
			return dbConnectedMsg{err: fmt.Errorf("failed to ping: %w", err)}
		}

		return dbConnectedMsg{conn: conn, project: &project}
	}
}

type dbConnectedMsg struct {
	conn    *pgx.Conn
	project *config.FoundProject
	err     error
}

func (m Model) loadSchema(conn *pgx.Conn, project config.FoundProject) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(conn)

		dbName := project.Connection.DSN
		if idx := strings.LastIndex(dbName, "/"); idx >= 0 {
			dbName = dbName[idx+1:]
		}
		if idx := strings.Index(dbName, "?"); idx >= 0 {
			dbName = dbName[:idx]
		}

		rootNode, err := loader.LoadDatabase(ctx, dbName)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("failed to load schema: %w", err)}
		}

		return schemaLoadedMsg{root: rootNode}
	}
}

func (m Model) isDDL(sql string) bool {
	trimmed := strings.TrimSpace(strings.ToUpper(sql))
	ddlPrefixes := []string{"CREATE ", "DROP ", "ALTER ", "TRUNCATE "}
	for _, prefix := range ddlPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

type schemaLoadedMsg struct {
	root *explorer.Node
	err  error
}

type tableSelectedMsg struct {
	schema string
	table  string
}

type tableDataLoadedMsg struct {
	result *postgres.QueryResult
	schema string
	table  string
	where  string
	err    error
}

func (m Model) loadMetadata(schema, table string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		constraints, err := loader.ListConstraints(ctx, schema, table)
		if err != nil {
			return metadataLoadedMsg{err: err}
		}

		foreignKeys, err := loader.ListForeignKeys(ctx, schema, table)
		if err != nil {
			return metadataLoadedMsg{err: err}
		}

		indexes, err := loader.ListIndexes(ctx, schema, table)
		if err != nil {
			return metadataLoadedMsg{err: err}
		}

		return metadataLoadedMsg{
			schema:      schema,
			table:       table,
			constraints: toConstraintInfo(constraints),
			foreignKeys: toForeignKeyInfo(foreignKeys),
			indexes:     toIndexInfo(indexes),
		}
	}
}

func toConstraintInfo(data []postgres.ConstraintInfo) []constraintInfo {
	result := make([]constraintInfo, len(data))
	for i, c := range data {
		result[i] = constraintInfo{Name: c.Name, Type: c.Type, Columns: c.Columns}
	}
	return result
}

func toForeignKeyInfo(data []postgres.ForeignKeyInfo) []foreignKeyInfo {
	result := make([]foreignKeyInfo, len(data))
	for i, fk := range data {
		result[i] = foreignKeyInfo{Name: fk.Name, Column: fk.Column, RefSchema: fk.RefSchema, RefTable: fk.RefTable, RefColumn: fk.RefColumn}
	}
	return result
}

func toIndexInfo(data []postgres.IndexInfo) []indexInfo {
	result := make([]indexInfo, len(data))
	for i, idx := range data {
		result[i] = indexInfo{Name: idx.Name, Columns: idx.Columns, Unique: idx.Unique}
	}
	return result
}

type queryExecutedMsg struct {
	result *postgres.QueryResult
	sql    string
	err    error
}

func (m Model) loadTableData(schema, table string) tea.Cmd {
	return m.loadTableDataWithWhere(schema, table, "")
}

func (m Model) loadTableDataWithWhere(schema, table, where string) tea.Cmd {
	return m.loadTableDataWithSortAndWhere(schema, table, "1", "", where)
}

func (m Model) loadTableDataWithSortAndWhere(schema, table, orderBy, orderDir, where string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		opts := postgres.SelectOptions{
			Schema:   schema,
			Limit:    grid.MaxRows,
			OrderBy:  orderBy,
			OrderDir: orderDir,
		}
		if where != "" {
			opts.Where = strings.TrimRight(strings.TrimSpace(where), ";")
		}

		result, err := loader.Select(ctx, table, opts)

		if err != nil {
			return tableDataLoadedMsg{err: fmt.Errorf("SELECT failed: %w", err)}
		}

		return tableDataLoadedMsg{result: result, schema: schema, table: table, where: where}
	}
}

func formatFKValue(val interface{}) string {
	switch v := val.(type) {
	case nil:
		return "NULL"
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	case int64:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case float32:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("'%v'", v)
	}
}

func (m Model) executeQuery(sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		result, err := loader.ExecuteRaw(ctx, sql)

		if err != nil {
			return queryExecutedMsg{err: fmt.Errorf("query failed: %w", err), sql: sql}
		}

		return queryExecutedMsg{result: result, sql: sql}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.SetWidth(msg.Width)
		m.picker.SetHeight(msg.Height)
		m.toast.SetWidth(msg.Width)
		m.palette.SetWidth(msg.Width)
		m.palette.SetHeight(msg.Height)
		m.helpModal.SetWidth(msg.Width)
		m.helpModal.SetHeight(msg.Height)
		m.statusbar.SetWidth(msg.Width)
		if m.explorer != nil {
			m.explorer.SetWidth(msg.Width / 3)
			m.explorer.SetHeight(msg.Height - 2)
		}
		return m, tickToast()

	case toastTickMsg:
		m.toast.Update()
		return m, tickToast()

	case projectsScannedMsg:
		m.picker.SetProjects(msg.projects)
		if len(msg.projects) == 0 {
			m.state = StateError
			m.err = fmt.Errorf("no .dbx.toml files found in ~/dev")
		}
		return m, nil

	case dbConnectedMsg:
		if msg.err != nil {
			m.state = StateError
			m.err = msg.err
			m.toast.ShowError(fmt.Sprintf("Connection failed: %v", msg.err))
			return m, nil
		}
		m.conn = msg.conn
		m.state = StateLoading
		m.toast.ShowInfo("Connected to database")
		return m, m.loadSchema(msg.conn, *msg.project)

	case schemaLoadedMsg:
		if msg.err != nil {
			m.state = StateError
			m.err = msg.err
			m.toast.ShowError(fmt.Sprintf("Schema load failed: %v", msg.err))
			return m, nil
		}
		m.explorer = explorer.New(m.styles, nil, m.keybinds)
		m.explorer.SetWidth(m.width / 3)
		m.explorer.SetHeight(m.height - 2)
		m.explorer.SetNodes([]*explorer.Node{msg.root})
		m.grid.SetWidth(m.width * 2 / 3)
		m.grid.SetHeight(m.height - 4)
		m.state = StateMain
		m.toast.ShowSuccess("Schema loaded")
		return m, nil

	case tableSelectedMsg:
		if m.conn == nil || msg.schema == "" || msg.table == "" {
			return m, nil
		}
		m.prevSchema = msg.schema
		m.prevTable = msg.table
		return m, m.loadTableData(msg.schema, msg.table)

	case tableDataLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.toast.ShowError(fmt.Sprintf("Load failed: %v", msg.err))
			return m, nil
		}
		m.grid.SetData(msg.result, msg.schema, msg.table)
		m.prevSchema = msg.schema
		m.prevTable = msg.table
		m.statusbar.SetTable(msg.schema, msg.table, msg.result.Count)
		m.statusbar.SetFilter(msg.where)
		m.router.FocusPane(FocusGrid)
		if m.conn != nil {
			return m, m.loadMetadata(msg.schema, msg.table)
		}
		return m, nil

	case metadataLoadedMsg:
		if msg.err != nil {
			m.toast.ShowError(fmt.Sprintf("Metadata load failed: %v", msg.err))
			return m, nil
		}
		constraints := make([]postgres.ConstraintInfo, len(msg.constraints))
		for i, c := range msg.constraints {
			constraints[i] = postgres.ConstraintInfo{Name: c.Name, Type: c.Type, Columns: c.Columns}
		}
		foreignKeys := make([]postgres.ForeignKeyInfo, len(msg.foreignKeys))
		for i, fk := range msg.foreignKeys {
			foreignKeys[i] = postgres.ForeignKeyInfo{Name: fk.Name, Column: fk.Column, RefSchema: fk.RefSchema, RefTable: fk.RefTable, RefColumn: fk.RefColumn}
		}
		indexes := make([]postgres.IndexInfo, len(msg.indexes))
		for i, idx := range msg.indexes {
			indexes[i] = postgres.IndexInfo{Name: idx.Name, Columns: idx.Columns, Unique: idx.Unique}
		}
		m.grid.SetMetadata(constraints, foreignKeys, indexes)
		if row := m.grid.SelectedRow(); row != nil {
			m.preview.SetRow(m.grid.Columns(), row)
		}
		return m, nil

	case grid.CellEditCommitMsg:
		if m.conn == nil {
			return m, nil
		}
		_, err := m.conn.Exec(context.Background(), msg.Query, msg.Args...)
		if err != nil {
			m.toast.ShowError(fmt.Sprintf("Update failed: %v", err))
			return m, nil
		}
		m.toast.ShowSuccess("Row updated")
		return m, nil

	case grid.GridCommitPendingMsg:
		if m.conn == nil {
			return m, nil
		}
		var inserted int
		for i, query := range msg.Queries {
			_, err := m.conn.Exec(context.Background(), query, msg.Args[i]...)
			if err != nil {
				m.toast.ShowError(fmt.Sprintf("Insert failed: %v", err))
				return m, nil
			}
			inserted++
		}
		m.toast.ShowSuccess(fmt.Sprintf("%d row(s) inserted", inserted))
		return m, m.loadTableData(msg.Schema, msg.Table)

	case grid.GridDeleteRowMsg:
		if m.conn == nil {
			return m, nil
		}
		pkCols := m.grid.PrimaryKeyColumns()
		query, args := grid.BuildDeleteQuery(msg.Schema, msg.Table, msg.Columns, msg.Row, pkCols)
		_, err := m.conn.Exec(context.Background(), query, args...)
		if err != nil {
			m.toast.ShowError(fmt.Sprintf("Delete failed: %v", err))
			return m, nil
		}
		m.grid.ClearSelection()
		m.toast.ShowSuccess("Row deleted")
		return m, m.loadTableData(msg.Schema, msg.Table)

	case grid.GridBulkDeleteMsg:
		if m.conn == nil {
			return m, nil
		}
		pkCols := m.grid.PrimaryKeyColumns()
		var deleted int
		for _, row := range msg.Rows {
			query, args := grid.BuildDeleteQuery(msg.Schema, msg.Table, msg.Columns, row, pkCols)
			_, err := m.conn.Exec(context.Background(), query, args...)
			if err != nil {
				m.toast.ShowError(fmt.Sprintf("Delete failed at row %d: %v", deleted+1, err))
				m.grid.ClearSelection()
				return m, m.loadTableData(msg.Schema, msg.Table)
			}
			deleted++
		}
		m.grid.ClearSelection()
		m.toast.ShowSuccess(fmt.Sprintf("%d row(s) deleted", deleted))
		return m, m.loadTableData(msg.Schema, msg.Table)

	case grid.GridFilterApplyMsg:
		if m.conn == nil {
			return m, nil
		}
		return m, m.loadTableDataWithWhere(msg.Schema, msg.Table, msg.Where)

	case grid.GridSortApplyMsg:
		if m.conn == nil {
			return m, nil
		}
		return m, m.loadTableDataWithSortAndWhere(msg.Schema, msg.Table, msg.OrderBy, msg.OrderDir, msg.Where)

	case grid.GridCursorMovedMsg:
		if row := m.grid.SelectedRow(); row != nil {
			m.preview.SetRow(m.grid.Columns(), row)
		} else {
			m.preview.SetRow(nil, nil)
		}
		return m, nil

	case grid.ExportSelectedMsg:
		return m, m.handleExport(msg)

	case exportDoneMsg:
		if msg.err != nil {
			m.toast.ShowError(fmt.Sprintf("Export failed: %v", msg.err))
		} else if msg.clipboard {
			m.toast.ShowSuccess("Copied to clipboard")
		} else {
			m.toast.ShowSuccess(fmt.Sprintf("Exported to %s", msg.filename))
		}
		return m, nil

	case grid.GridTabChangeMsg:
		if row := m.grid.SelectedRow(); row != nil {
			m.preview.SetRow(m.grid.Columns(), row)
		} else {
			m.preview.SetRow(nil, nil)
		}
		return m, nil

	case grid.GridNavigateFKMsg:
		if msg.RefTable == "" {
			m.toast.ShowInfo("FK value is NULL — cannot navigate")
			return m, nil
		}
		// Push current state to navigation stack
		if m.prevSchema != "" && m.prevTable != "" {
			m.navStack = append(m.navStack, NavigationEntry{
				Schema:    m.prevSchema,
				Table:     m.prevTable,
				Where:     m.grid.WhereClause(),
				CursorRow: m.grid.CursorRow(),
				CursorCol: m.grid.CursorCol(),
				ScrollCol: m.grid.ScrollCol(),
			})
		}
		m.prevSchema = msg.RefSchema
		m.prevTable = msg.RefTable
		// Combine existing WHERE with FK condition
		fkWhere := fmt.Sprintf("%q = %s", msg.RefColumn, formatFKValue(msg.FKValue))
		existingWhere := m.grid.WhereClause()
		combinedWhere := fkWhere
		if existingWhere != "" {
			combinedWhere = fmt.Sprintf("%s AND (%s)", fkWhere, existingWhere)
		}
		// Sync explorer to the referenced table
		if m.explorer != nil {
			m.explorer.SelectTable(msg.RefSchema, msg.RefTable)
		}
		m.router.FocusPane(FocusGrid)
		return m, m.loadTableDataWithWhere(msg.RefSchema, msg.RefTable, combinedWhere)

	case grid.GridGoBackMsg:
		if len(m.navStack) == 0 {
			m.toast.ShowInfo("No navigation history")
			return m, nil
		}
		entry := m.navStack[len(m.navStack)-1]
		m.navStack = m.navStack[:len(m.navStack)-1]
		m.prevSchema = entry.Schema
		m.prevTable = entry.Table
		if m.explorer != nil {
			m.explorer.SelectTable(entry.Schema, entry.Table)
		}
		m.router.FocusPane(FocusGrid)
		return m, m.loadTableDataWithSortAndWhere(entry.Schema, entry.Table, "1", "", entry.Where)

	case queryExecutedMsg:
		m.queryExecuting = false
		if msg.err != nil {
			m.toast.ShowError(fmt.Sprintf("Query failed: %v", msg.err))
			return m, nil
		}
		m.grid.SetData(msg.result, "", "query")
		m.editor.PushHistory(msg.sql)
		m.statusbar.SetTable("", "query", msg.result.Count)
		m.router.FocusPane(FocusGrid)
		m.toast.ShowSuccess(fmt.Sprintf("Query returned %d rows", msg.result.Count))

		if m.isDDL(msg.sql) && m.conn != nil && m.project != nil {
			return m, m.loadSchema(m.conn, *m.project)
		}
		return m, nil

	case explorer.TableSelectedMsg:
		return m, m.loadTableData(msg.Schema, msg.Table)

	case explorer.NewTableMsg:
		schema := msg.Schema
		if schema == "" {
			schema = "public"
		}
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(fmt.Sprintf("CREATE TABLE %s.new_table (\n    id SERIAL PRIMARY KEY,\n    name VARCHAR(255) NOT NULL\n);", schema))
		m.statusbar.SetEditorOpen(true)
		return m, nil

	case explorer.DropTableMsg:
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(fmt.Sprintf("DROP TABLE %s.%s;", msg.Schema, msg.Table))
		m.statusbar.SetEditorOpen(true)
		return m, nil

	case explorer.ViewDDLMsg:
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(fmt.Sprintf("-- DDL for %s.%s\n-- Run this query to see the table definition:\nSELECT pg_get_tabledef('%s', '%s');", msg.Schema, msg.Table, msg.Schema, msg.Table))
		m.statusbar.SetEditorOpen(true)
		return m, nil

	case picker.ConnectionSelectedMsg:
		m.state = StateLoading
		m.project = &msg.Project
		m.toast.ShowInfo(fmt.Sprintf("Connecting to %s...", msg.Project.Name))
		return m, m.connectToDB(msg.Project)

	case palette.CommandSelectedMsg:
		return m.handlePaletteCommand(msg.Action)

	case tea.MouseWheelMsg:
		if m.state == StateMain {
			if m.router.Focus() == FocusExplorer && m.explorer != nil {
				mm := msg.Mouse()
				if mm.Button == tea.MouseWheelUp {
					m.explorer.Update(tea.KeyPressMsg{Code: 'k'})
				} else if mm.Button == tea.MouseWheelDown {
					m.explorer.Update(tea.KeyPressMsg{Code: 'j'})
				}
			}
			if m.router.Focus() == FocusGrid && m.grid != nil {
				mm := msg.Mouse()
				if mm.Button == tea.MouseWheelUp {
					m.grid.Update(tea.KeyPressMsg{Code: 'k'})
				} else if mm.Button == tea.MouseWheelDown {
					m.grid.Update(tea.KeyPressMsg{Code: 'j'})
				}
				if row := m.grid.SelectedRow(); row != nil {
					m.preview.SetRow(m.grid.Columns(), row)
				}
			}
			if m.preview != nil {
				mm := msg.Mouse()
				if mm.Button == tea.MouseWheelUp {
					m.preview.ScrollUp()
				} else if mm.Button == tea.MouseWheelDown {
					m.preview.ScrollDown()
				}
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.state == StateMain {
			mm := msg.Mouse()
			paneWidth := m.width / 3
			now := time.Now()

			isDoubleClick := !m.lastClickTime.IsZero() &&
				now.Sub(m.lastClickTime) < 300*time.Millisecond &&
				abs(mm.X-m.lastClickX) <= 2 &&
				abs(mm.Y-m.lastClickY) <= 2

			m.lastClickTime = now
			m.lastClickX = mm.X
			m.lastClickY = mm.Y

			if mm.X < paneWidth && m.explorer != nil {
				localY := mm.Y - 3
				if m.explorer.IsFiltering() {
					localY--
				}
				if isDoubleClick {
					m.explorer.HandleClick(localY)
					return m, m.explorer.ToggleExpand()
				}
				m.explorer.HandleClick(localY)
			} else if mm.X >= paneWidth && m.grid != nil {
				m.router.FocusPane(FocusGrid)
				localX := mm.X - paneWidth
				localY := mm.Y - 3
				if isDoubleClick {
					m.grid.HandleClick(localX, localY)
					if cmd, _ := m.grid.Update(tea.KeyPressMsg{Code: 13}); cmd != nil {
						return m, cmd
					}
					return m, nil
				}
				m.grid.HandleClick(localX, localY)
				if row := m.grid.SelectedRow(); row != nil {
					m.preview.SetRow(m.grid.Columns(), row)
				}
				if cmd := m.grid.HandleHeaderClick(localX); cmd != nil {
					return m, cmd
				}
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.helpModal.IsVisible() {
			if cmd, handled := m.helpModal.Update(msg); handled {
				return m, cmd
			}
			return m, nil
		}

		if m.palette.IsVisible() {
			if cmd, handled := m.palette.Update(msg); handled {
				return m, cmd
			}
			return m, nil
		}

		if m.state == StatePicker {
			if cmd, handled := m.picker.Update(msg); handled {
				return m, cmd
			}
			key := msg.String()
			if key == "q" || key == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.state == StateMain {
			key := msg.String()

			if m.editorOpen {
				if key == "esc" {
					m.editorOpen = false
					m.editor.Blur()
					return m, nil
				}

				if key == "ctrl+enter" || key == "ctrl+r" {
					sql := m.editor.Content()
					if sql != "" && m.conn != nil && !m.queryExecuting {
						m.queryExecuting = true
						return m, m.executeQuery(sql)
					}
					return m, nil
				}

				if cmd, handled := m.editor.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsFiltering() {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsWhereFiltering() {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if m.router.Focus() == FocusExplorer && m.explorer != nil && m.explorer.IsFiltering() {
				if cmd, handled := m.explorer.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsEditing() {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if key == m.keybinds["global.help"] {
				m.helpModal.Show()
				return m, nil
			}

			if key == m.keybinds["global.palette"] {
				m.palette.Show()
				return m, nil
			}

			if key == m.keybinds["global.quit"] {
				if m.conn != nil {
					m.conn.Close(context.Background())
				}
				return m, tea.Quit
			}

			if key == m.keybinds["grid.commit_pending"] && m.grid != nil && m.grid.HasPendingRows() {
				return m, m.grid.CommitPendingInserts()
			}

			if key == m.keybinds["global.cycle_focus"] {
				m.router.CycleFocus()
				return m, nil
			}

			if key == m.keybinds["global.focus_explorer"] {
				m.router.FocusPane(FocusExplorer)
				return m, nil
			}

			if key == m.keybinds["global.focus_grid"] {
				m.router.FocusPane(FocusGrid)
				return m, nil
			}

			if key == m.keybinds["global.focus_editor"] {
				m.editorOpen = !m.editorOpen
				if m.editorOpen {
					m.editor.Focus()
				} else {
					m.editor.Blur()
				}
				m.statusbar.SetEditorOpen(m.editorOpen)
				return m, nil
			}

			if m.router.Focus() == FocusExplorer && m.explorer != nil {
				if cmd, handled := m.explorer.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusGrid && m.grid != nil {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
			}

			return m, nil
		}
	}

	return m, nil
}

func (m Model) handlePaletteCommand(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "global.quit":
		if m.conn != nil {
			m.conn.Close(context.Background())
		}
		return m, tea.Quit
	case "global.cycle_focus":
		m.router.CycleFocus()
	case "global.focus_explorer":
		m.router.FocusPane(FocusExplorer)
	case "global.focus_grid":
		m.router.FocusPane(FocusGrid)
	case "global.focus_editor":
		m.editorOpen = !m.editorOpen
		if m.editorOpen {
			m.editor.Focus()
		} else {
			m.editor.Blur()
		}
		m.statusbar.SetEditorOpen(m.editorOpen)
	case "global.help":
		m.helpModal.Show()
	case "global.refresh":
		if m.project != nil && m.conn != nil {
			return m, m.loadSchema(m.conn, *m.project)
		}
	case "editor.execute":
		if !m.editorOpen {
			m.editorOpen = true
			m.editor.Focus()
			m.statusbar.SetEditorOpen(true)
		}
	case "editor.clear":
		m.editor.Clear()
		m.toast.ShowInfo("Editor cleared")
	default:
		m.toast.ShowInfo(fmt.Sprintf("Command: %s", action))
	}
	return m, nil
}

func (m Model) handleExport(msg grid.ExportSelectedMsg) tea.Cmd {
	return func() tea.Msg {
		// Single row: copy to clipboard
		if msg.Row != nil {
			var content string
			switch msg.Format {
			case grid.ExportSQL:
				content = exportRowAsSQL(msg.Schema, msg.Table, msg.Columns, msg.Row)
			case grid.ExportJSON:
				content = exportRowAsJSON(msg.Columns, msg.Row)
			case grid.ExportCSV:
				content = exportRowAsCSV(msg.Columns, msg.Row)
			}

			if err := copyToClipboard(content); err != nil {
				return exportDoneMsg{err: fmt.Errorf("failed to copy to clipboard: %w", err)}
			}
			return exportDoneMsg{clipboard: true}
		}

		// Multiple rows: save to file
		if msg.Rows != nil {
			result := &postgres.QueryResult{
				Columns: make([]postgres.ColumnInfo, len(msg.Columns)),
				Rows:    msg.Rows,
			}
			for i, name := range msg.Columns {
				result.Columns[i] = postgres.ColumnInfo{Name: name}
			}

			var content string
			var filename string

			switch msg.Format {
			case grid.ExportSQL:
				content = exportAsSQL(msg.Schema, msg.Table, result)
				filename = fmt.Sprintf("%s_%s.sql", msg.Schema, msg.Table)
			case grid.ExportJSON:
				content = exportAsJSON(result)
				filename = fmt.Sprintf("%s_%s.json", msg.Schema, msg.Table)
			case grid.ExportCSV:
				content = exportAsCSV(result)
				filename = fmt.Sprintf("%s_%s.csv", msg.Schema, msg.Table)
			}

			filePath := filepath.Join(m.project.Path, filename)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return exportDoneMsg{err: fmt.Errorf("failed to write file: %w", err)}
			}

			return exportDoneMsg{filename: filePath}
		}

		return exportDoneMsg{err: fmt.Errorf("no data to export")}
	}
}

func exportAsSQL(schema, table string, result *postgres.QueryResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("INSERT INTO %q.%q (%s) VALUES\n", schema, table, 
		strings.Join(quoteColumns(result.Columns), ", ")))

	for i, row := range result.Rows {
		values := make([]string, len(row))
		for j, val := range row {
			values[j] = formatSQLValue(val)
		}
		sb.WriteString(fmt.Sprintf("  (%s)", strings.Join(values, ", ")))
		if i < len(result.Rows)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(";\n")
	return sb.String()
}

func quoteColumns(columns []postgres.ColumnInfo) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf("%q", col.Name)
	}
	return quoted
}

func formatSQLValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("'%v'", v)
	}
}

func exportAsJSON(result *postgres.QueryResult) string {
	rows := make([]map[string]interface{}, len(result.Rows))
	for i, row := range result.Rows {
		rows[i] = make(map[string]interface{})
		for j, col := range result.Columns {
			rows[i][col.Name] = row[j]
		}
	}

	data, _ := json.MarshalIndent(rows, "", "  ")
	return string(data) + "\n"
}

func exportAsCSV(result *postgres.QueryResult) string {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	// Header
	headers := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		headers[i] = col.Name
	}
	writer.Write(headers)

	// Rows
	for _, row := range result.Rows {
		record := make([]string, len(row))
		for i, val := range row {
			record[i] = fmt.Sprintf("%v", val)
		}
		writer.Write(record)
	}

	writer.Flush()
	return sb.String()
}

func exportRowAsSQL(schema, table string, columns []string, row []interface{}) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("INSERT INTO %q.%q (%s) VALUES\n", schema, table, 
		strings.Join(quoteColumnNames(columns), ", ")))

	values := make([]string, len(row))
	for i, val := range row {
		values[i] = formatSQLValue(val)
	}
	sb.WriteString(fmt.Sprintf("  (%s)\n", strings.Join(values, ", ")))
	return sb.String()
}

func quoteColumnNames(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, name := range columns {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return quoted
}

func exportRowAsJSON(columns []string, row []interface{}) string {
	obj := make(map[string]interface{})
	for i, col := range columns {
		obj[col] = row[i]
	}
	data, _ := json.MarshalIndent(obj, "", "  ")
	return string(data) + "\n"
}

func exportRowAsCSV(columns []string, row []interface{}) string {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	writer.Write(columns)

	record := make([]string, len(row))
	for i, val := range row {
		record[i] = fmt.Sprintf("%v", val)
	}
	writer.Write(record)

	writer.Flush()
	return sb.String()
}

func copyToClipboard(content string) error {
	// Try platform-specific commands
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip.exe")
	default: // Linux
		// Wayland: try wl-copy first
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd = exec.Command("wl-copy")
			cmd.Stdin = strings.NewReader(content)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		// X11: try xclip, then xsel
		cmd = exec.Command("xclip", "-selection", "clipboard")
		if err := cmd.Run(); err == nil {
			return nil
		}
		cmd = exec.Command("xsel", "--clipboard", "--input")
	}
	
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

type exportDoneMsg struct {
	filename  string
	clipboard bool
	err       error
}

func (m Model) View() tea.View {
	var content string

	switch m.state {
	case StatePicker:
		content = m.picker.View()
	case StateLoading:
		content = m.styles.Text.Render("Connecting to database...")
	case StateError:
		content = m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err))
	case StateMain:
		content = m.renderMainView()
	}

	toastLines := m.toast.ViewLines()
	for i := len(toastLines) - 1; i >= 0; i-- {
		toastBox := m.styles.Border.Width(30).Render(toastLines[i])
		content = overlayBottomRight(content, toastBox, m.width, m.height, i)
	}

	if m.helpModal.IsVisible() {
		helpView := m.helpModal.View()
		if helpView != "" {
			content = overlay(content, helpView, m.width, m.height)
		}
	}

	if m.palette.IsVisible() {
		paletteView := m.palette.View()
		if paletteView != "" {
			content = overlay(content, paletteView, m.width, m.height)
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderMainView() string {
	if m.explorer != nil {
		if selected := m.explorer.Selected(); selected != nil && selected.Type == explorer.NodeTable {
			schema := ""
			if s, ok := selected.Metadata["schema"].(string); ok {
				schema = s
			}
			m.statusbar.SetTable(schema, selected.Name, selected.RowCount())
		}
	}

	m.statusbar.SetFocus(m.router.Context())
	statusLine := m.statusbar.RenderStatus()

	contentHeight := m.height - 4
	hasTableData := m.grid.HasData()
	showPreview := hasTableData && m.grid.ActiveTab() == 0 && m.width >= 100

	var panes []string
	if m.editorOpen || m.router.Focus() == FocusExplorer {
		panes = append(panes, m.renderExplorer(m.width, contentHeight))
	} else {
		if showPreview {
			gridW := m.width * 3 / 4
			panes = append(panes, m.renderGrid(gridW, contentHeight))
			panes = append(panes, m.renderPreview(m.width/4, contentHeight))
		} else {
			panes = append(panes, m.renderGrid(m.width, contentHeight))
		}
	}

	content := statusLine + "\n" + m.renderBreadcrumbs() + lipgloss.JoinHorizontal(lipgloss.Top, panes...)

	if m.editorOpen {
		modalW := m.width * 6 / 10
		modalH := contentHeight * 7 / 10
		modal := m.renderEditor(modalW, modalH)
		content = overlay(content, modal, m.width, contentHeight)
	}

	if m.grid.IsExporting() {
		exportView := m.grid.ExportPickerView()
		if exportView != "" {
			content = overlay(content, exportView, m.width, m.height)
		}
	}

	content += "\n" + m.statusbar.Render()

	return content
}

func (m Model) renderBreadcrumbs() string {
	if len(m.navStack) == 0 && m.prevTable == "" {
		return ""
	}

	var parts []string
	for _, entry := range m.navStack {
		parts = append(parts, m.styles.TextMuted.Render(fmt.Sprintf("%s.%s", entry.Schema, entry.Table)))
	}
	if m.prevTable != "" {
		parts = append(parts, m.styles.Text.Render(fmt.Sprintf("%s.%s", m.prevSchema, m.prevTable)))
	}

	bread := strings.Join(parts, m.styles.TextMuted.Render(" → "))
	return "  " + bread + "\n"
}

func (m Model) renderExplorer(w, h int) string {
	if m.explorer == nil {
		return m.styles.Border.
			Width(w - 2).
			Height(h - 2).
			Render(m.styles.TextMuted.Render("Explorer"))
	}

	m.explorer.SetWidth(w)
	m.explorer.SetHeight(h)

	if m.router.Focus() == FocusExplorer {
		m.explorer.Focus()
	} else {
		m.explorer.Blur()
	}

	return m.explorer.View()
}

func (m Model) renderGrid(w, h int) string {
	m.grid.SetWidth(w)
	m.grid.SetHeight(h)

	if m.router.Focus() == FocusGrid {
		m.grid.Focus()
	} else {
		m.grid.Blur()
	}

	border := m.styles.Border
	if m.router.Focus() == FocusGrid {
		border = m.styles.BorderActive
	}

	return border.
		Width(w - 2).
		Height(h - 2).
		Render(m.grid.View())
}

func (m Model) renderPreview(w, h int) string {
	m.preview.SetWidth(w)
	m.preview.SetHeight(h)

	return m.styles.Border.
		Width(w - 2).
		Height(h - 2).
		Render(m.preview.Render())
}

func (m Model) renderEditor(w, h int) string {
	m.editor.SetWidth(w - 4)
	m.editor.SetHeight(h - 6)
	m.editor.Focus()

	title := m.styles.Header.Render("SQL Editor")
	hint := m.styles.Help.Render("Ctrl+Enter: execute · Ctrl+U: clear · Ctrl+P/N: history · Esc: close")
	content := m.editor.View()

	return m.styles.BorderActive.
		Width(w - 2).
		Height(h - 2).
		Render(title + "\n" + content + "\n" + hint)
}

func overlay(base, box string, width, height int) string {
	lines := strings.Split(base, "\n")
	blocks := strings.Split(box, "\n")
	bw := lipgloss.Width(blocks[0])
	bh := len(blocks)
	if bh > height {
		bh = height
	}
	x := (width - bw) / 2
	if x < 0 {
		x = 0
	}
	y := (height - bh) / 2
	if y < 0 {
		y = 0
	}
	for j := 0; j < bh && y+j < len(lines); j++ {
		line := lines[y+j]
		lines[y+j] = ansi.Truncate(line, x, "") + blocks[j] + ansi.TruncateLeft(line, x+bw, "")
	}
	return strings.Join(lines, "\n")
}

func overlayBottomRight(base, box string, width, height, stackOffset int) string {
	lines := strings.Split(base, "\n")
	blocks := strings.Split(box, "\n")
	bw := lipgloss.Width(blocks[0])
	bh := len(blocks)
	if bh > height {
		bh = height
	}
	x := width - bw - 1
	if x < 0 {
		x = 0
	}
	y := height - bh - 5 - stackOffset*(bh+1)
	if y < 0 {
		y = 0
	}
	for j := 0; j < bh && y+j < len(lines); j++ {
		line := lines[y+j]
		lines[y+j] = ansi.Truncate(line, x, "") + blocks[j] + ansi.TruncateLeft(line, x+bw, "")
	}
	return strings.Join(lines, "\n")
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
