package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/grid"
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
	state          AppState
	config         *config.Config
	theme          *theme.Theme
	styles         *theme.Styles
	picker         *picker.Picker
	explorer       *explorer.Explorer
	grid           *grid.Grid
	editor         *editor.SQLEditor
	router         *Router
	keybindRegistry *config.KeybindRegistry
	project        *config.FoundProject
	conn           *pgx.Conn
	width          int
	height         int
	err            error
	editorOpen     bool
	queryExecuting bool
}

func NewModel(cfg *config.Config) Model {
	t := theme.Resolve(cfg.Theme.Mode)
	kbr := config.NewKeybindRegistry(cfg.Keybindings)
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
		router:          NewRouter(kbr.Flatten()),
		keybindRegistry: kbr,
		state:           StatePicker,
	}
}

func (m Model) Init() tea.Cmd {
	return m.scanProjects()
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
	err    error
}

type queryExecutedMsg struct {
	result *postgres.QueryResult
	sql    string
	err    error
}

func (m Model) loadTableData(schema, table string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		result, err := loader.Select(ctx, table, postgres.SelectOptions{
			Schema:  schema,
			Limit:   1000,
			OrderBy: "1",
		})

		if err != nil {
			return tableDataLoadedMsg{err: fmt.Errorf("SELECT failed: %w", err)}
		}

		// DEBUG
		f, _ := os.Create("/tmp/dbx_load_debug.log")
		if f != nil {
			fmt.Fprintf(f, "loadTableData: schema=%q table=%q\n", schema, table)
			fmt.Fprintf(f, "  result.Columns=%d result.Rows=%d\n", len(result.Columns), len(result.Rows))
			if len(result.Columns) > 0 {
				for i, c := range result.Columns {
					fmt.Fprintf(f, "  Column[%d]: %s\n", i, c.Name)
				}
			}
			if len(result.Rows) > 0 {
				fmt.Fprintf(f, "  First row len=%d\n", len(result.Rows[0]))
			}
			f.Close()
		}

		return tableDataLoadedMsg{result: result, schema: schema, table: table}
	}
}

func (m Model) executeQuery(sql string) tea.Cmd {
	return func() tea.Msg {
		// DEBUG
		if f, err := os.OpenFile("/tmp/dbx_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintf(f, "EXECUTING: %s\n", sql)
			f.Close()
		}

		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		result, err := loader.ExecuteRaw(ctx, sql)

		// DEBUG
		if f, err := os.OpenFile("/tmp/dbx_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			if err != nil {
				fmt.Fprintf(f, "EXEC ERROR: %v\n", err)
			} else {
				fmt.Fprintf(f, "EXEC OK: %d rows, %d cols\n", result.Count, len(result.Columns))
			}
			f.Close()
		}

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
		if m.explorer != nil {
			m.explorer.SetWidth(msg.Width / 3)
			m.explorer.SetHeight(msg.Height - 2)
		}
		return m, nil

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
			return m, nil
		}
		m.conn = msg.conn
		m.state = StateLoading
		return m, m.loadSchema(msg.conn, *msg.project)

	case schemaLoadedMsg:
		if msg.err != nil {
			m.state = StateError
			m.err = msg.err
			return m, nil
		}
		m.explorer = explorer.New(m.styles, nil)
		m.explorer.SetWidth(m.width / 3)
		m.explorer.SetHeight(m.height - 2)
		m.explorer.SetNodes([]*explorer.Node{msg.root})
		m.grid.SetWidth(m.width * 2 / 3)
		m.grid.SetHeight(m.height - 4)
		m.state = StateMain
		return m, nil

	case tableSelectedMsg:
		if m.conn == nil || msg.schema == "" || msg.table == "" {
			return m, nil
		}
		return m, m.loadTableData(msg.schema, msg.table)

	case tableDataLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// DEBUG
		f, _ := os.Create("/tmp/dbx_app_debug.log")
		if f != nil {
			fmt.Fprintf(f, "tableDataLoadedMsg: schema=%q table=%q\n", msg.schema, msg.table)
			fmt.Fprintf(f, "  result.Columns=%d result.Rows=%d\n", len(msg.result.Columns), len(msg.result.Rows))
			f.Close()
		}
		m.grid.SetData(msg.result, msg.schema, msg.table)
		m.router.FocusPane(FocusGrid)
		return m, nil

	case queryExecutedMsg:
		m.queryExecuting = false

		// DEBUG
		if f, err := os.OpenFile("/tmp/dbx_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			if msg.err != nil {
				fmt.Fprintf(f, "HANDLER ERROR: %v\n", msg.err)
			} else {
				fmt.Fprintf(f, "HANDLER OK: %d rows\n", msg.result.Count)
			}
			f.Close()
		}

		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.grid.SetData(msg.result, "", "query")
		m.editor.PushHistory(msg.sql)
		m.router.FocusPane(FocusGrid)
		return m, nil

	case explorer.TableSelectedMsg:
		return m, m.loadTableData(msg.Schema, msg.Table)

	case picker.ConnectionSelectedMsg:
		m.state = StateLoading
		m.project = &msg.Project
		return m, m.connectToDB(msg.Project)

	case tea.KeyPressMsg:
		// Let picker handle keys first when in picker state
		if m.state == StatePicker {
			if cmd, handled := m.picker.Update(msg); handled {
				return m, cmd
			}
			// Also handle quit in picker state
			key := msg.String()
			if key == "q" || key == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		// Handle keys in main state
		if m.state == StateMain {
			key := msg.String()

			// Editor modal captures keys when open
			if m.editorOpen {
				// DEBUG: Log all keys to file
				if f, err := os.OpenFile("/tmp/dbx_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
					fmt.Fprintf(f, "key=%q code=%d mod=%d\n", key, msg.Code, msg.Mod)
					f.Close()
				}

				if key == "esc" {
					m.editorOpen = false
					m.editor.Blur()
					return m, nil
				}

				// Handle editor-specific keys
				if key == "ctrl+enter" || key == "ctrl+r" {
					sql := m.editor.Content()
					// DEBUG: Log conditions
					if f, err := os.OpenFile("/tmp/dbx_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
						fmt.Fprintf(f, "EXEC: sql=%q conn=%v queryExec=%v\n", sql, m.conn != nil, m.queryExecuting)
						f.Close()
					}
					if sql != "" && m.conn != nil && !m.queryExecuting {
						m.queryExecuting = true
						return m, m.executeQuery(sql)
					}
					return m, nil
				}

				// Forward to editor component
				if cmd, handled := m.editor.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			ctx := m.router.Context()

			action := m.router.Match(key, ctx)
			if action == "" {
				action = m.router.Match(key, "global")
			}

			switch action {
			case "global.quit":
				if m.conn != nil {
					m.conn.Close(context.Background())
				}
				return m, tea.Quit
			case "global.cycle_focus":
				m.router.CycleFocus()
				return m, nil
			case "global.focus_explorer":
				m.router.FocusPane(FocusExplorer)
				return m, nil
			case "global.focus_grid":
				m.router.FocusPane(FocusGrid)
				return m, nil
			case "global.focus_editor":
				m.editorOpen = !m.editorOpen
				if m.editorOpen {
					m.editor.Focus()
				} else {
					m.editor.Blur()
				}
				return m, nil
			case "explorer.filter":
				if m.router.Focus() == FocusExplorer && m.explorer != nil {
					m.explorer.StartFilter()
				}
				return m, nil
			}

		// Forward to focused component
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

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderMainView() string {
	titleText := "dbx"
	if m.explorer != nil {
		if selected := m.explorer.Selected(); selected != nil {
			titleText = selected.Name
		}
	}
	title := m.styles.Title.Render(titleText)

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

	content := title + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)

	if m.editorOpen {
		modalW := m.width * 6 / 10
		modalH := contentHeight * 7 / 10
		modal := m.renderEditor(modalW, modalH)
		content = overlay(content, modal, m.width, contentHeight)
	}

	content += "\n" + m.renderHelp()

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

func (m Model) renderHelp() string {
	help1 := m.helpLine1()
	help2 := m.helpLine2()

	sep := m.styles.Sep.Render(strings.Repeat("─", m.width))

	return sep + "\n" +
		m.styles.Help.Render("  "+help1) + "\n" +
		"\n" +
		m.styles.Help.Render("  "+help2)
}

func (m Model) helpLine1() string {
	r := m.router
	segments := []string{
		"/ filter",
		"Tab cycle",
		r.KeyFor("global.help") + " help",
		r.KeyFor("global.quit") + " quit",
	}

	if m.editorOpen {
		segments = append(segments, "esc close")
	} else {
		segments = append(segments, r.KeyFor("global.focus_editor")+" editor")
	}

	segments = append(segments, ": palette")

	return strings.Join(segments, " · ")
}

func (m Model) helpLine2() string {
	r := m.router
	ctx := m.router.Context()

	var segments []string

	if ctx == "explorer" {
		segments = []string{
			"j/k navigate",
			r.KeyFor("explorer.expand") + " expand",
			r.KeyFor("explorer.collapse") + " collapse",
			r.KeyFor("explorer.new") + " new",
			r.KeyFor("explorer.drop") + " drop",
			r.KeyFor("explorer.view_ddl") + " ddl",
		}
	} else if ctx == "grid" {
		segments = []string{
			"j/k navigate",
			r.KeyFor("grid.edit_cell") + " edit",
			r.KeyFor("grid.delete_row") + " delete",
			r.KeyFor("grid.insert_row") + " insert",
			r.KeyFor("grid.yank") + " yank",
			r.KeyFor("grid.sort") + " sort",
		}
	} else {
		segments = []string{
			r.KeyFor("global.refresh") + " refresh",
			r.KeyFor("global.export") + " export",
		}
	}

	return strings.Join(segments, " · ")
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

func (m *Router) Match(key, context string) string {
	_ = context
	switch key {
	case "q", "ctrl+c":
		return "global.quit"
	case "tab":
		return "global.cycle_focus"
	case "1":
		return "global.focus_explorer"
	case "2":
		return "global.focus_grid"
	case "E":
		return "global.focus_editor"
	case "/":
		return "explorer.filter"
	}
	return ""
}
