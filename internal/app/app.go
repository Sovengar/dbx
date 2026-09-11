package app

import (
	"context"
	"fmt"
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
		grid:            grid.New(t.Styles(), pageSize),
		editor:          editor.NewSQLEditor(t.Styles()),
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
		return m, m.loadTableData(msg.schema, msg.table)

	case tableDataLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.toast.ShowError(fmt.Sprintf("Load failed: %v", msg.err))
			return m, nil
		}
		m.grid.SetData(msg.result, msg.schema, msg.table)
		m.statusbar.SetTable(msg.schema, msg.table, msg.result.Count)
		m.statusbar.SetFilter(msg.where)
		m.router.FocusPane(FocusGrid)
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
		return m, m.loadTableData(msg.Schema, msg.Table)

	case grid.GridInsertRowMsg:
		if m.conn == nil {
			return m, nil
		}
		query := fmt.Sprintf("INSERT INTO %q.%q DEFAULT VALUES", msg.Schema, msg.Table)
		_, err := m.conn.Exec(context.Background(), query)
		if err != nil {
			m.toast.ShowError(fmt.Sprintf("Insert failed: %v", err))
			return m, nil
		}
		m.toast.ShowSuccess("Row inserted")
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
	paneWidth := m.width / 3
	focus := m.router.Focus()

	var panes []string

	if focus == FocusExplorer || m.editorOpen {
		panes = append(panes, m.renderExplorer(paneWidth, contentHeight))
		panes = append(panes, m.renderGrid(paneWidth*2, contentHeight))
	} else {
		panes = append(panes, m.renderGrid(m.width, contentHeight))
	}

	content := statusLine + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)

	if m.editorOpen {
		modalW := m.width * 6 / 10
		modalH := contentHeight * 7 / 10
		modal := m.renderEditor(modalW, modalH)
		content = overlay(content, modal, m.width, contentHeight)
	}

	content += "\n" + m.statusbar.Render()

	return content
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
