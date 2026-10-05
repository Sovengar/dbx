package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/debuglog"
	"github.com/charmbracelet/x/ansi"

	aiContext "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/ai/nl2sql"
	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/store"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/bordered"
	"github.com/buble/dbx/internal/ui/components/ask"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/explorerpreview"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
	"github.com/buble/dbx/internal/ui/components/gridsidebarpreview"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/buble/dbx/internal/ui/components/picker"
	"github.com/buble/dbx/internal/ui/components/querybrowser"
	"github.com/buble/dbx/internal/ui/keydisplay"
	"github.com/jackc/pgx/v5"
)

type AppState int

const (
	StatePicker AppState = iota
	StateLoading
	StateMain
	StateError
)

// Mouse zones registered on each rendered pane. Only the panes that are
// currently visible get marked, so hit-testing naturally follows the active
// layout/focus instead of assuming a fixed split.
const (
	zonePaneExplorer    = "pane-explorer"
	zonePaneGrid        = "pane-grid"
	zonePaneGridSidebar = "pane-grid-sidebar"
)

type Model struct {
	state              AppState
	config             *config.Config
	theme              *theme.Theme
	styles             *theme.Styles
	picker             *picker.Picker
	explorer           *explorer.Explorer
	grid               *grid.Grid
	editor             *editor.SQLEditor
	gridSidebarPreview *gridsidebarpreview.Preview
	gridPreview        *gridpreview.GridPreview
	explorerPreview    *explorerpreview.ExplorerPreview
	router             *Router
	keybinds           config.Resolver
	palette            *palette.Palette
	helpModal          *ui.HelpModal
	toast              *ui.ToastManager
	keybindsPane       *ui.KeybindsPane
	project            *config.FoundProject
	// The INTERFACE, not *pgx.Conn. Everything this model does over a connection goes
	// through postgres.Conn — Query, QueryRow, Exec, Close — and depending on the
	// interface is what lets a test make one statement fail and the next succeed, which
	// is the only way to reach the arms that handle a rejected metadata query or a
	// refused write. A live database cannot be told to fail on demand, so while this was
	// *pgx.Conn the error handling in this file was only reachable by breaking a
	// container mid-test.
	conn                       postgres.Conn
	width                      int
	height                     int
	err                        error
	editorOpen                 bool
	queryExecuting             bool
	lastClickTime              time.Time
	lastClickX                 int
	lastClickY                 int
	navStack                   []NavigationEntry
	prevSchema                 string
	prevTable                  string
	gridSidebarFKPreviewCache  map[string][]gridSidebarFKPreviewCacheEntry
	gridSidebarFKPreviewCursor int
	schemaDetail               []postgres.SchemaDetail
	dbName                     string
	yankMaxRows                int
	spinnerActive              bool
	spinnerFrame               int
	schemaForeignKeys          map[string][]postgres.ForeignKeyInfo
	queryStore                 *store.QueryStore
	// sessionLogger records the SQL that ran, for `dbx pipe`. Nil when
	// session.enabled is false or the log could not be opened, so every call site
	// nil-checks rather than testing a flag.
	sessionLogger    *session.Logger
	queryBrowser     *querybrowser.QueryBrowser
	queryBrowserOpen bool
	ask              *ask.Ask
	askOpen          bool
	aiProvider       nl2sql.Provider
	aiProviderErr    error
	askSchemaText    string
	askContextHint   string
	askGenSeq        int
	runner           *statementRunner
	connectCancelled bool
	forcePicker      bool
}

var spinnerChars = [9]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇"}

func NewModel(cfg *config.Config) Model {
	keydisplay.SetNerdFont(cfg.UI.NerdFont)
	t := theme.Resolve(cfg.Theme.Mode)
	kbr := config.NewKeybindRegistry(cfg.Keybindings)
	pageSize := 100
	if cfg.UI.PageSize > 0 {
		pageSize = cfg.UI.PageSize
	}
	yankMaxRows := 10
	if cfg.UI.YankMaxRows > 0 {
		yankMaxRows = cfg.UI.YankMaxRows
	}
	// ui.statusbar_help is the on/off switch for the keybind pane. It was
	// declared with a default of true and read by nothing, so a user who turned
	// the help off kept getting it.
	helpVisible := cfg.UI.StatusBarHelp
	g := grid.New(t.Styles(), pageSize, kbr)
	g.SetYankMaxRows(yankMaxRows)
	// QueryStore is initialized later in initQueryStore after project selection
	qs := &store.QueryStore{}
	qb := querybrowser.New(t.Styles(), qs)
	ed := editor.NewSQLEditor(t.Styles())
	ed.SetKeybinds(kbr)
	ed.SetAutocompleteConfig(cfg.Editor.Autocomplete, cfg.Editor.AutocompleteTrigger)
	// Resolve the NL→SQL provider once; a nil provider keeps ASK unavailable.
	// The resolution error is kept so the user sees the real reason (e.g. a
	// missing API key) instead of a generic "not configured".
	aiProvider, aiProviderErr := nl2sql.Resolve(nl2sql.Config{
		Provider:  cfg.AI.Provider,
		Model:     cfg.AI.Model,
		Providers: cfg.AI.Nl2sqlProviders(),
	})
	kbp := ui.NewKeybindsPane(t.Styles(), kbr)
	kbp.SetVisible(helpVisible)

	// session.enabled and session.retention_days. session.NewLogger took a
	// directory and a retention all along and was never called by anything, so
	// both keys were declared with defaults and read by nothing — and dbx wrote no
	// session log at all, which is why `session.dir` appeared to work (only the
	// READER in the CLI used it) while the writer did not exist.
	//
	// A failure to open the log must not stop the TUI, so the error is dropped and
	// the logger stays nil: logging is the one feature here that is allowed to be
	// unavailable without the user finding out the hard way.
	var sessionLogger *session.Logger
	if cfg.Session.Enabled {
		if l, err := session.NewLogger(cfg.Session.Dir, cfg.Session.RetentionDays); err == nil {
			sessionLogger = l
		}
	}

	return Model{
		config:             cfg,
		theme:              t,
		styles:             t.Styles(),
		picker:             picker.New(t.Styles()),
		grid:               g,
		editor:             ed,
		gridSidebarPreview: gridsidebarpreview.New(t.Styles()),
		gridPreview:        gridpreview.New(t.Styles(), kbr),
		explorerPreview:    explorerpreview.New(t.Styles(), kbr),
		router:             NewRouter(kbr),
		keybinds:           kbr,
		palette:            palette.New(t.Styles(), kbr),
		helpModal:          ui.NewHelpModal(t.Styles(), kbr),
		toast:              ui.NewToastManager(t.Styles()),
		keybindsPane:       kbp,
		state:              StatePicker,
		yankMaxRows:        yankMaxRows,
		queryStore:         qs,
		queryBrowser:       qb,
		sessionLogger:      sessionLogger,
		ask:                ask.New(t.Styles()),
		aiProvider:         aiProvider,
		aiProviderErr:      aiProviderErr,
	}
}

// logSessionQuery, logSessionError and logSessionConnect are the three writes the
// session log gets. All three are no-ops without a logger, which is the state
// session.enabled = false produces — and also the state a failed log open produces,
// because a TUI that refuses to start over a log file would be a worse bug than a
// missing log.
//
// They swallow their own errors on purpose. The log is a record of what happened, not a
// channel the user is waiting on, so a full disk must not turn a successful query into a
// reported failure.
func (m *Model) logSessionQuery(sql string, d time.Duration, rows int) {
	if m.sessionLogger == nil {
		return
	}
	_ = m.sessionLogger.LogQuery(sql, d, rows)
}

func (m *Model) logSessionError(sql string, err error) {
	if m.sessionLogger == nil || err == nil {
		return
	}
	_ = m.sessionLogger.LogError(sql, err)
}

func (m *Model) logSessionConnect(name string) {
	if m.sessionLogger == nil {
		return
	}
	_ = m.sessionLogger.LogConnect(name)
}

// cleanupSessions applies session.retention_days. It runs once at startup, in its own
// command rather than inline, because it is a directory walk: on a laptop with a year of
// daily logs that is thousands of stat calls, and doing it in Init would delay the first
// frame by however long the disk takes.
func (m Model) cleanupSessions() tea.Cmd {
	logger := m.sessionLogger
	if logger == nil {
		return nil
	}
	return func() tea.Msg {
		_ = logger.Cleanup()
		return nil
	}
}

// initQueryStore initializes the QueryStore for the given project.
// It uses a default stateDir if not provided (for tests).
func (m *Model) initQueryStore(projectName string, stateDir string) {
	if stateDir == "" && m.config != nil {
		// ui.state_dir. The explicit argument still wins so a caller — and the
		// tests — can point the store somewhere disposable without touching the
		// user's real state. The nil guard is not defensive padding: a model
		// built by the test harness has no config, and reading through it
		// panicked the first time a test passed an empty stateDir.
		stateDir = m.config.UI.StateDir
	}
	if stateDir == "" {
		stateDir = config.StateDir()
	}
	projectDir := filepath.Join(stateDir, "projects", projectName)

	// Migrate global history if it exists and project file doesn't
	_ = store.MigrateGlobalHistory(stateDir, projectDir)

	// ui.history_size, which used to default to 100 while the store kept 500 and
	// read neither. A limit of zero means the store's own default rather than
	// "keep nothing", so a model built without a config is not left with an empty
	// history.
	maxEntries := 0
	if m.config != nil {
		maxEntries = m.config.UI.HistorySize
	}
	m.queryStore = store.NewQueryStoreLimited(projectDir, maxEntries)
	m.queryBrowser = querybrowser.New(m.styles, m.queryStore)
	// Apply current window dimensions to the new QueryBrowser
	if m.width > 0 && m.height > 0 {
		m.queryBrowser.SetWidth(m.width)
		m.queryBrowser.SetHeight(m.height)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scanProjects(), m.cleanupSessions(), tickToast())
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

// actionSurvivesTextInput reports whether an app-wide action stays available while a
// widget owns the keyboard — the explorer's table filter, the jq expression line.
//
// This is ONE list because it was TWO, and they drifted. The jq line got a carve-out
// and the explorer's table filter did not, so `?` typed a literal `?` into the filter
// instead of opening help: the one key a user reaches for when they do not know what
// else to press, which is exactly the state they are in while filtering.
//
// `quit` is deliberately ABSENT, and that is a real tension rather than an oversight.
// It is bound to `q`, and `q` is a legal character in a table filter, so carving it out
// would make the filter untypeable. Its other binding, `ctrl+c`, is not separable from
// `q` at this layer — the carve-out is by ACTION, not by key — so quitting from inside
// a text input stays the business of the key that means "quit" everywhere.
func actionSurvivesTextInput(action config.ActionID) bool {
	switch action {
	case "help", "palette", "rollback":
		return true
	}
	return false
}

func tickToast() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return toastTickMsg{}
	})
}

type spinnerTickMsg struct{}

func tickSpinner() tea.Cmd {
	return tea.Every(time.Millisecond*100, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// pgxConnect opens a database connection, and it is a VARIABLE so a test can supply one.
//
// This is the app's only call that opens a socket, and the ping that follows it is an error
// arm no test could reach: pgx.Connect succeeding means the DSN parsed and something answered
// on the far end, so the only way for the PING to fail is a server that accepts the connection
// and then stalls — which is a socket this repository has no server for.
//
// So the connect is injected and the arm is driven with a connection that reports itself
// unusable. Production passes pgx.Connect and nothing else can tell the difference.
//
// It is the same trade the CLI makes with its own pgxConnect, and it is the same shape as
// copyToClipboard below: a package variable for the one call a test cannot make for itself.
var pgxConnect = func(ctx context.Context, dsn string) (postgres.Conn, error) {
	return pgx.Connect(ctx, dsn)
}

func (m Model) connectToDB(project config.FoundProject) tea.Cmd {
	return func() tea.Msg {
		dsn := project.Connection.GetDSN()
		if dsn == "" {
			return dbConnectedMsg{err: fmt.Errorf("no DSN provided")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgxConnect(ctx, dsn)
		if err != nil {
			return dbConnectedMsg{err: fmt.Errorf("failed to connect: %w", err)}
		}

		if err := conn.Ping(ctx); err != nil {
			// Close it. The connection is already open — pgx.Connect returned it — and
			// returning without closing leaked a socket and a backend process for every
			// database that accepted the TCP connection and then failed to answer. The error
			// is returned rather than the connection, because a connection that cannot answer
			// a ping cannot answer a query either.
			_ = conn.Close(ctx)
			return dbConnectedMsg{err: fmt.Errorf("failed to ping: %w", err)}
		}

		return dbConnectedMsg{conn: conn, project: &project}
	}
}

type dbConnectedMsg struct {
	conn    postgres.Conn
	project *config.FoundProject
	err     error
}

func (m Model) loadSchema(conn postgres.Conn, project config.FoundProject) tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(conn)

		dbName := deriveDBName(project.Connection.DSN)

		result, err := loader.LoadDatabase(ctx, dbName)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("failed to load schema: %w", err)}
		}

		return schemaLoadedMsg{root: result.Root, dbName: dbName, schemaDetail: result.Schemas}
	}
}

// deriveDBName is the database name inside a DSN: the part after the last slash and before
// the first question mark.
//
// It was written inline twice — in loadSchema and in loadSchemaWithTarget — with no shared
// helper, which is the shape that has produced the most bugs in this repository: the same
// string operation in two places, left to drift. The name it produces is what the schema
// pane and the window title show, so a divergence between the copies would mean the app
// names the database one thing on the first load and another on the reload after a DDL
// statement.
//
// The order of the two steps matters and is not interchangeable: the question mark is
// stripped AFTER the slash, because a DSN's query string can itself contain a slash
// (`?options=-c%20search_path%3Dpublic`).
func deriveDBName(dsn string) string {
	name := dsn
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.Index(name, "?"); idx >= 0 {
		name = name[:idx]
	}
	return name
}

func (m Model) loadSchemaWithTarget(conn postgres.Conn, project config.FoundProject, targetSchema, targetTable string) tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(conn)

		dbName := deriveDBName(project.Connection.DSN)

		result, err := loader.LoadDatabase(ctx, dbName)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("failed to load schema: %w", err)}
		}

		return schemaLoadedMsg{
			root:         result.Root,
			dbName:       dbName,
			schemaDetail: result.Schemas,
			targetSchema: targetSchema,
			targetTable:  targetTable,
		}
	}
}

func (m Model) loadAutocompleteData() tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		indexesBySchema := make(map[string]map[string][]postgres.IndexInfoFull)
		fksBySchema := make(map[string]map[string][]postgres.ForeignKeyInfo)

		for _, sd := range m.schemaDetail {
			idx, _ := loader.ListIndexesFullBySchema(ctx, sd.Name)
			fks, _ := loader.ListForeignKeysBySchema(ctx, sd.Name)
			indexesBySchema[sd.Name] = idx
			fksBySchema[sd.Name] = fks
		}

		export := buildSchemaExportFull(m.dbName, m.schemaDetail, indexesBySchema, fksBySchema)

		// Flatten schema FKs for ERE diagram: table -> FKs across all schemas
		allSchemaFKs := make(map[string][]postgres.ForeignKeyInfo)
		for _, fksMap := range fksBySchema {
			for table, fks := range fksMap {
				allSchemaFKs[table] = append(allSchemaFKs[table], fks...)
			}
		}

		return autocompleteDataLoadedMsg{schemaExport: export, schemaForeignKeys: allSchemaFKs}
	}
}

type autocompleteDataLoadedMsg struct {
	schemaExport      *aiContext.SchemaExport
	schemaForeignKeys map[string][]postgres.ForeignKeyInfo
}

func buildSchemaExportFull(
	dbName string,
	schemas []postgres.SchemaDetail,
	indexesBySchema map[string]map[string][]postgres.IndexInfoFull,
	fksBySchema map[string]map[string][]postgres.ForeignKeyInfo,
) *aiContext.SchemaExport {
	export := &aiContext.SchemaExport{Database: dbName}

	for _, sd := range schemas {
		si := aiContext.SchemaInfo{Name: sd.Name}

		idxMap := indexesBySchema[sd.Name]
		fksMap := fksBySchema[sd.Name]

		for _, td := range sd.Tables {
			ti := aiContext.TableInfo{
				Name:     td.Name,
				Type:     td.Type,
				RowCount: int64(td.RowCount),
			}

			for _, c := range td.Columns {
				ti.Columns = append(ti.Columns, aiContext.ColumnInfo{
					Name:         c.Name,
					DataType:     c.DataType,
					IsNullable:   c.IsNullable == "YES",
					DefaultValue: c.Default,
				})
			}

			if idxMap != nil {
				for _, idx := range idxMap[td.Name] {
					ti.Indexes = append(ti.Indexes, aiContext.IndexInfo{
						Name:      idx.Name,
						Columns:   idx.Columns,
						IsUnique:  idx.IsUnique,
						IsPrimary: idx.IsPrimary,
					})
				}
			}

			if fksMap != nil {
				for _, fk := range fksMap[td.Name] {
					ti.FKs = append(ti.FKs, aiContext.FKInfo{
						Name:       fk.Name,
						Columns:    fk.Column,
						RefSchema:  fk.RefSchema,
						RefTable:   fk.RefTable,
						RefColumns: fk.RefColumn,
					})
				}
			}

			si.Tables = append(si.Tables, ti)
		}

		export.Schemas = append(export.Schemas, si)
	}

	return export
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

// hasWordPrefix reports whether s begins with word followed by whitespace or the end of
// s. A bare strings.HasPrefix is not enough: it matched "CREATE TABLEX" — and
// "CREATE TABLESPACE" — as CREATE TABLE, so `CREATE TABLESPACE foo LOCATION '/x'`
// reported the table "SPACE".
func hasWordPrefix(s, word string) bool {
	if !strings.HasPrefix(s, word) {
		return false
	}
	rest := s[len(word):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r'
}

// takeName reads one unquoted identifier off the front of rest, returning it and what is
// left (already trimmed).
//
// The characters that END an identifier are written here, once. They used to be written
// twice and the two copies disagreed: the reader that takes the table after an explicit
// schema ended the name at a semicolon, and the reader that takes an unqualified name did
// not. So `CREATE TABLE users;` — the punctuated form, which is how most people write it,
// and what the editor is left holding after a previous statement — navigated to a table
// named "users;", while `CREATE TABLE s.users;` navigated correctly. Same parser, same
// statement, different answer depending on whether a schema happened to be spelled out.
//
// dotEnds says whether a dot terminates the name. It does for the FIRST token, which is
// either the schema or the whole name, and not for the second, which is a table inside a
// quoted schema and may legitimately contain one.
func takeName(rest string, dotEnds bool) (name, remainder string) {
	end := 0
	for end < len(rest) {
		switch c := rest[end]; c {
		case ' ', '\t', '\n', '\r', '(', ';':
			return rest[:end], strings.TrimSpace(rest[end:])
		case '"':
			// A double quote ENDS the name immediately, which for a name that starts with
			// one means the name is empty. Quoted identifiers are only ever read through
			// unquoteName, so reaching a quote here means either the statement is malformed
			// or the quoting is unterminated — and returning the opening quote as part of
			// the name produced a table called `"users`, which does not exist.
			//
			// A quote cannot appear inside an unquoted identifier (Postgres escapes it by
			// doubling it, which only exists in the quoted form), so this cannot truncate
			// a legitimate name.
			return rest[:end], strings.TrimSpace(rest[end:])
		case '.':
			if dotEnds {
				return rest[:end], strings.TrimSpace(rest[end:])
			}
		}
		end++
	}
	return rest, ""
}

func extractDDLTableName(sql string) (schema, table string) {
	// TRIM ONCE. `upper` was TrimSpace'd while `sql` was not, so the slice below
	// counted the keyword's bytes from the start of the UNTRIMMED text and cut
	// into the middle of it: "  DROP TABLE IF EXISTS public.users" sliced at 10
	// gave "LE IF EXISTS public.users", and the answer was schema "public", table
	// "LE". The result drives loadSchemaWithTarget, so that navigated the
	// explorer to a table that does not exist — and the leading whitespace is not
	// exotic, because the statement arrives from the editor and the history, and
	// the TrimSpace at the top of this function PROMISED to handle it.
	//
	// The table name still comes from the UN-uppercased text below, which is why
	// this cannot simply slice `upper`.
	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)

	keyword := ""
	switch {
	case hasWordPrefix(upper, "CREATE TABLE"):
		keyword = "CREATE TABLE"
	case hasWordPrefix(upper, "DROP TABLE"):
		keyword = "DROP TABLE"
	case hasWordPrefix(upper, "ALTER TABLE"):
		keyword = "ALTER TABLE"
	case hasWordPrefix(upper, "TRUNCATE TABLE"):
		keyword = "TRUNCATE TABLE"
	}

	if keyword == "" {
		return "", ""
	}

	rest := strings.TrimSpace(trimmed[len(keyword):])

	upperRest := strings.ToUpper(rest)
	switch {
	case hasWordPrefix(upperRest, "IF NOT EXISTS"):
		rest = rest[len("IF NOT EXISTS"):]
	case hasWordPrefix(upperRest, "IF EXISTS"):
		rest = rest[len("IF EXISTS"):]
	}

	rest = strings.TrimSpace(rest)

	if len(rest) > 0 && rest[0] == '"' {
		if quoted, ok := unquoteName(rest); ok {
			// Read by unquoteName so the quote handling lives in ONE place. It used to be
			// hand-sliced here, and the two copies disagreed in two ways:
			//
			//   after a quoted SCHEMA an unquoted table was never read at all, so
			//   `CREATE TABLE "MySchema".users` came back with an empty table and
			//   navigated nowhere
			//
			//   after an unquoted schema a quoted table kept its QUOTES, so
			//   `sales."MyTable"` came back as the name `"MyTable"` — with the quote
			//   characters in it, which is not a table that exists, while the identical
			//   statement without a schema came back as `MyTable`.
			//
			// Two copies of one decision, disagreeing. Eleventh instance of this family in
			// this repo.
			table = quoted
			rest = strings.TrimSpace(rest[len(quoted)+2:])
			if len(rest) > 0 && rest[0] == '.' {
				schema = table
				table = nameAfterSchema(strings.TrimSpace(rest[1:]))
			}

			// The `public` default, applied HERE as well as at the end of the function
			// because this arm returns early. Quoting a table name is mandatory in
			// Postgres for any name with a capital in it, so without the default here
			// `CREATE TABLE "MyTable"` navigated nowhere while the identical statement
			// unquoted navigated to public.MyTable.
			if schema == "" {
				schema = "public"
			}
			return schema, table
		}
	}

	first, rest := takeName(rest, true)

	if len(rest) > 0 && rest[0] == '.' {
		schema = first
		table = nameAfterSchema(strings.TrimSpace(rest[1:]))
	} else {
		table = first
	}

	if schema == "" {
		schema = "public"
	}

	return schema, table
}

// nameAfterSchema reads the table name that follows a dot, quoted or not.
//
// Both arms of extractDDLTableName go through it, which is what makes `sales."MyTable"`
// and `sales.MyTable` answer identically — they used not to, and the quoted one carried
// its quote characters into a lookup for a table that does not exist.
func nameAfterSchema(rest string) string {
	if quoted, ok := unquoteName(rest); ok {
		return quoted
	}
	name, _ := takeName(rest, false)
	return name
}

// unquoteName reads a double-quoted identifier off the front of s and returns it with the
// quotes removed, plus whether there was one.
//
// It is the single answer to "is this name quoted", and it refuses an UNTERMINATED quote
// rather than returning the fragment: slicing s[1 : strings.Index(s[1], quote)+1] over a
// string with no closing quote asks for s[1:0], which is a slice bounds panic. The panic
// was avoided by a branch that instead returned the name WITH its opening quote still
// attached, so `DROP TABLE "users` navigated to a table called `"users`.
func unquoteName(s string) (string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", false
	}
	end := strings.Index(s[1:], `"`)
	if end < 0 {
		return "", false
	}
	return s[1 : end+1], true
}

type schemaLoadedMsg struct {
	root         *explorer.Node
	dbName       string
	schemaDetail []postgres.SchemaDetail
	err          error
	targetSchema string
	targetTable  string
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
	if m.refuseIfBusy() {
		return nil
	}
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

		overview, err := loader.GetTableOverview(ctx, schema, table)
		if err != nil {
			overview = nil
		}

		return metadataLoadedMsg{
			schema:      schema,
			table:       table,
			constraints: toConstraintInfo(constraints),
			foreignKeys: toForeignKeyInfo(foreignKeys),
			indexes:     toIndexInfo(indexes),
			overview:    overview,
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

func (m Model) loadExplorerPreviewData(schema, table string) tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		columns, err := loader.ListColumns(ctx, schema, table)
		if err != nil {
			columns = nil
		}

		constraints, err := loader.ListConstraints(ctx, schema, table)
		if err != nil {
			constraints = nil
		}

		foreignKeys, err := loader.ListForeignKeys(ctx, schema, table)
		if err != nil {
			foreignKeys = nil
		}

		indexes, err := loader.ListIndexes(ctx, schema, table)
		if err != nil {
			indexes = nil
		}

		overview, err := loader.GetTableOverview(ctx, schema, table)
		if err != nil {
			overview = nil
		}

		return explorerPreviewDataMsg{
			schema:      schema,
			table:       table,
			columns:     columns,
			constraints: constraints,
			foreignKeys: foreignKeys,
			indexes:     indexes,
			overview:    overview,
		}
	}
}

type queryExecutedMsg struct {
	result *postgres.QueryResult
	sql    string
	err    error
	// duration is how long the statement took, measured where it ran. It is
	// carried rather than recomputed because the handler that writes the session
	// log runs later, on a different turn of the event loop.
	duration time.Duration
	// commitFailed reports that the COMMIT of a pending DML transaction failed,
	// which is not the same as the statement failing: the statement ran, and the
	// question is whether its effects are stored. It is carried separately because
	// the answer needs the connection's, not the statement's — see
	// handleCommitFailure.
	commitFailed bool
	// committedTx reports that a pending DML transaction was committed
	// before running this execution.
	committedTx bool
}

// askGeneratedMsg carries the result of an async NL→SQL generation. seq tags
// the request so a stale result cannot attach to a newer turn.
type askGeneratedMsg struct {
	sql string
	err error
	seq int
}

// askQueryExecutedMsg carries the result of an ASK query run in a READ ONLY
// transaction. seq tags the request so a stale result cannot attach to a newer
// turn.
type askQueryExecutedMsg struct {
	result *postgres.QueryResult
	sql    string
	err    error
	seq    int
}

func (m Model) loadTableData(schema, table string) tea.Cmd {
	return m.loadTableDataWithWhere(schema, table, "")
}

func (m Model) loadTableDataWithWhere(schema, table, where string) tea.Cmd {
	return m.loadTableDataWithSortAndWhere(schema, table, "1", "", where)
}

func (m Model) loadTableDataWithSortAndWhere(schema, table, orderBy, orderDir, where string) tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
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

const maxGridSidebarFKPreviewCacheSize = 50

type gridSidebarFKPreviewCacheEntry struct {
	value   interface{}
	columns []string
	row     []interface{}
}

func gridSidebarFKPreviewCacheKey(schema, table, column string) string {
	return schema + "." + table + "." + column
}

func (m *Model) lookupGridSidebarFKPreviewCache(key string, value interface{}) *gridSidebarFKPreviewCacheEntry {
	entries, ok := m.gridSidebarFKPreviewCache[key]
	if !ok {
		return nil
	}
	for i := range entries {
		if fmt.Sprintf("%v", entries[i].value) == fmt.Sprintf("%v", value) {
			return &entries[i]
		}
	}
	return nil
}

func (m *Model) storeGridSidebarFKPreviewCache(key string, value interface{}, columns []string, row []interface{}) {
	if m.gridSidebarFKPreviewCache == nil {
		m.gridSidebarFKPreviewCache = make(map[string][]gridSidebarFKPreviewCacheEntry)
	}
	entry := gridSidebarFKPreviewCacheEntry{value: value, columns: columns, row: row}
	m.gridSidebarFKPreviewCache[key] = append(m.gridSidebarFKPreviewCache[key], entry)
	if len(m.gridSidebarFKPreviewCache[key]) > maxGridSidebarFKPreviewCacheSize {
		m.gridSidebarFKPreviewCache[key] = m.gridSidebarFKPreviewCache[key][len(m.gridSidebarFKPreviewCache[key])-maxGridSidebarFKPreviewCacheSize:]
	}
}

func (m Model) findFKForColumn(colName string) *postgres.ForeignKeyInfo {
	fks := m.grid.ForeignKeys()
	for i := range fks {
		if fks[i].Column == colName {
			return &fks[i]
		}
	}
	return nil
}

func (m Model) fetchGridSidebarFKPreview(fkInfo *postgres.ForeignKeyInfo, fkValue interface{}, token int) tea.Cmd {
	if m.refuseIfBusy() {
		return nil
	}
	refSchema := fkInfo.RefSchema
	if refSchema == "" {
		refSchema = m.prevSchema
	}
	fkWhere := fmt.Sprintf("%q = %s", fkInfo.RefColumn, formatFKValue(fkValue))
	refTable := fkInfo.RefTable
	cacheKey := gridSidebarFKPreviewCacheKey(m.prevSchema, m.grid.TableName(), fkInfo.Column)
	return func() tea.Msg {
		loader := postgres.NewSchemaLoader(m.conn)
		opts := postgres.SelectOptions{
			Schema: refSchema,
			Where:  fkWhere,
			Limit:  1,
		}
		result, err := loader.Select(context.Background(), refTable, opts)
		if err != nil {
			return GridSidebarFKPreviewLookupResultMsg{Err: err, Token: token}
		}
		if result == nil || len(result.Rows) == 0 {
			return GridSidebarFKPreviewLookupResultMsg{Err: fmt.Errorf("referenced row not found"), Token: token}
		}
		columns := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			columns[i] = col.Name
		}
		return GridSidebarFKPreviewLookupResultMsg{
			Columns:  columns,
			Row:      result.Rows[0],
			CacheKey: cacheKey,
			CacheVal: fkValue,
			RefTable: refTable,
			Token:    token,
		}
	}
}

func (m *Model) syncGridSidebarPreviewForCursor() tea.Cmd {
	if m.conn == nil || m.grid == nil {
		return nil
	}
	row := m.grid.SelectedRow()
	if row == nil {
		m.gridSidebarPreview.SetRow(nil, nil)
		m.syncGridPreview()
		return nil
	}
	columns := m.grid.Columns()
	cursorCol := m.grid.CursorCol()

	if cursorCol >= 0 && cursorCol < len(columns) {
		fkInfo := m.findFKForColumn(columns[cursorCol])
		if fkInfo != nil && cursorCol < len(row) && row[cursorCol] != nil {
			fkValue := row[cursorCol]
			key := gridSidebarFKPreviewCacheKey(m.prevSchema, m.grid.TableName(), fkInfo.Column)
			if cached := m.lookupGridSidebarFKPreviewCache(key, fkValue); cached != nil {
				m.gridSidebarPreview.SetFKRow(cached.columns, cached.row, fkInfo.RefTable)
				if m.gridPreview != nil && m.gridPreview.IsFocused() {
					m.gridPreview.SetRow(cached.columns, cached.row)
				}
				return nil
			}
			m.gridSidebarFKPreviewCursor++
			return m.fetchGridSidebarFKPreview(fkInfo, fkValue, m.gridSidebarFKPreviewCursor)
		}
	}

	m.gridSidebarPreview.SetRow(columns, row)
	m.syncGridPreview()
	return nil
}

func preprocessSQL(sql string) string {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		// Nothing to run. This used to fall through to `return sql` at the bottom,
		// handing back the untrimmed whitespace — which is not empty, so execute_query's
		// `sql != ""` check passed and pressing run on an empty editor sent whitespace
		// to PostgreSQL. The answer came back as "empty query string", which is a
		// confusing way to learn you pressed the wrong key.
		return ""
	}
	upper := strings.ToUpper(trimmed)

	if strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "WITH ") ||
		strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "UPDATE ") ||
		strings.HasPrefix(upper, "DELETE ") || strings.HasPrefix(upper, "EXPLAIN ") ||
		strings.HasPrefix(upper, "ALTER ") || strings.HasPrefix(upper, "CREATE ") ||
		strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(upper, "GRANT ") ||
		strings.HasPrefix(upper, "REVOKE ") {
		return sql
	}

	if idx := findLastTopLevelSELECT(trimmed); idx > 0 {
		selectClause := strings.TrimSpace(trimmed[idx:])
		remainder := strings.TrimSpace(trimmed[:idx])
		return selectClause + " " + remainder
	}

	if strings.HasPrefix(upper, "FROM ") {
		return "SELECT * " + trimmed
	}

	return sql
}

func findLastTopLevelSELECT(s string) int {
	upper := strings.ToUpper(s)
	depth := 0
	lastPos := -1
	for i := 0; i < len(s)-5; i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && i+6 <= len(s) && upper[i:i+6] == "SELECT" {
				if i > 0 && isASCIILetter(s[i-1]) {
					continue
				}
				if i+6 < len(s) && isASCIILetter(s[i+6]) {
					continue
				}
				lastPos = i
			}
		}
	}
	return lastPos
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

// dbBusy reports whether an ASK read-only transaction is still in flight on
// the shared connection. While busy, no other DB command may start: pgx cannot
// multiplex transactions on one connection, so a concurrent query would fail
// read-only or be rolled back with the ASK transaction.
func (m Model) dbBusy() bool {
	return m.runner != nil && m.runner.readOnlyBusy()
}

// refuseIfBusy shows a clear message and reports whether a DB command must be
// refused because an ASK read-only transaction is still running.
func (m Model) refuseIfBusy() bool {
	if !m.dbBusy() {
		return false
	}
	if m.toast != nil {
		m.toast.ShowError("a read-only ASK query is still running")
	}
	appDebugLog("DB: refused, a read-only ASK query is still running")
	return true
}

func (m Model) executeQuery(sql string) tea.Cmd {
	// Capture the intent at dispatch time: the grid sets commit-on-run when it
	// dumps draft SQL into the editor, so executing it also commits the
	// transaction the statement opens.
	commitOnRun := m.editor != nil && m.editor.CommitOnRun()

	return func() tea.Msg {
		if m.runner == nil {
			return queryExecutedMsg{err: fmt.Errorf("not connected to a database"), sql: sql}
		}

		ctx := context.Background()
		started := time.Now()
		result, committed, err := m.runner.execute(ctx, sql)
		commitFailed := false
		if err == nil && commitOnRun {
			committedNow, commitErr := m.runner.commitPending(ctx)
			if commitErr != nil {
				err = commitErr
				commitFailed = true
			}
			committed = committed || committedNow
		}
		// The duration and row count are measured HERE, inside the command, because
		// the handler that receives the message cannot: by then the runner is free
		// again and any clock it started would be measuring the wrong thing.
		return queryExecutedMsg{
			result:       result,
			sql:          sql,
			err:          err,
			commitFailed: commitFailed,
			committedTx:  committed,
			duration:     time.Since(started),
		}
	}
}

// handleAskOpen opens the ASK overlay, refusing when no AI provider is
// configured so the user gets a clear message instead of a dead pane.
func (m Model) handleAskOpen() (tea.Model, tea.Cmd) {
	if m.aiProvider == nil {
		reason := "No AI provider configured"
		if m.aiProviderErr != nil {
			reason = fmt.Sprintf("No AI provider configured: %v", m.aiProviderErr)
		}
		appDebugLog("Ask: provider unavailable: %s", reason)
		m.toast.ShowError(reason)
		return m, nil
	}
	if m.ask == nil {
		return m, nil
	}
	schema, table, where := m.gridContextHint()
	m.askContextHint = buildAskContextHint(schema, table, where)
	m.askOpen = true
	m.ask.Show()
	appDebugLog("Ask: opened context=%q", m.askContextHint)
	return m, nil
}

// gridContextHint returns the loaded table and WHERE clause, if any.
func (m Model) gridContextHint() (schema, table, where string) {
	if m.grid == nil {
		return "", "", ""
	}
	return m.grid.ContextHint()
}

// buildAskContextHint formats the grid context for the prompt. It is a hint,
// never a restriction: ASK stays globally available. Query results (synthetic
// table "query") and empty schemas carry no hint.
func buildAskContextHint(schema, table, where string) string {
	if schema == "" || table == "" || table == "query" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The grid is currently showing table %s.%s.\n", schema, table)
	if where != "" {
		fmt.Fprintf(&b, "The grid's current WHERE clause is: %s\n", where)
	}
	return b.String()
}

// generateAskSQL runs the NL→SQL provider asynchronously so the UI stays
// responsive while the model generates. seq identifies the request so a stale
// result (the pane was closed and reopened) is ignored by the caller.
func (m Model) generateAskSQL(question string, seq int) tea.Cmd {
	provider := m.aiProvider
	schema := m.askSchemaText
	if schema == "" {
		schema = postgres.SchemaText(m.schemaDetail)
	}
	contextHint := m.askContextHint
	return func() tea.Msg {
		if provider == nil {
			return askGeneratedMsg{err: fmt.Errorf("no AI provider configured"), seq: seq}
		}
		prompt := question
		if contextHint != "" {
			prompt = question + "\n\nContext (hint, not a restriction):\n" + contextHint
		}
		appDebugLog("Ask: generating seq=%d prompt=%q schemaLen=%d", seq, prompt, len(schema))
		sql, err := provider.Generate(context.Background(), prompt, schema)
		return askGeneratedMsg{sql: sql, err: err, seq: seq}
	}
}

// executeAskSQL validates the generated SQL and runs it inside a READ ONLY
// transaction, reporting the result as an askQueryExecutedMsg. seq identifies
// the request so a stale result is ignored by the caller.
func (m Model) executeAskSQL(sql string, seq int) tea.Cmd {
	if m.ask == nil {
		return nil
	}
	if reason := selectOnlyViolation(sql); reason != "" {
		appDebugLog("Ask: rejected statement %q: %s", sql, reason)
		m.ask.SetError(fmt.Errorf("%s", reason))
		return nil
	}
	if m.runner == nil {
		m.ask.SetError(fmt.Errorf("not connected to a database"))
		return nil
	}
	if m.runner.pending() {
		appDebugLog("Ask: refused, a DML transaction is pending")
		m.ask.SetError(fmt.Errorf("a DML transaction is pending: commit or roll back first"))
		return nil
	}
	if m.runner.readOnlyBusy() {
		appDebugLog("Ask: refused, a read-only query is already running")
		m.ask.SetError(fmt.Errorf("a read-only query is already running"))
		return nil
	}
	runner := m.runner
	return func() tea.Msg {
		result, err := runner.executeReadOnly(context.Background(), sql)
		return askQueryExecutedMsg{result: result, sql: sql, err: err, seq: seq}
	}
}

// splitSQL splits a SQL string by top-level semicolons, ignoring semicolons
// inside string literals, dollar-quoted strings, quoted identifiers and
// comments.
func splitSQL(sql string) []string {
	masked := maskNonCode(sql)
	var parts []string
	start := 0
	for i := 0; i < len(sql); i++ {
		if masked[i] == ';' {
			parts = append(parts, sql[start:i])
			start = i + 1
		}
	}
	if start < len(sql) {
		parts = append(parts, sql[start:])
	}
	return parts
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.SetWidth(msg.Width)
		m.toast.SetWidth(msg.Width)
		m.palette.SetWidth(msg.Width)
		m.palette.SetHeight(msg.Height)
		m.helpModal.SetWidth(msg.Width)
		m.helpModal.SetHeight(msg.Height)
		m.keybindsPane.SetWidth(msg.Width)
		m.keybindsPane.SetHeight(msg.Height)
		// The grid was sized ONLY from renderGrid and from schemaLoadedMsg, so its width
		// was a side effect of painting: a model that had been resized but not yet
		// drawn had an unsized grid, and an unsized grid has no column widths, and the
		// cell editor indexes those widths. Every other component is sized here, and
		// the grid not being sized here is the odd one out.
		m.grid.SetWidth(msg.Width)
		m.grid.SetHeight(msg.Height - 4)
		if m.explorer != nil {
			m.explorer.SetWidth(msg.Width)
			m.explorer.SetHeight(msg.Height - 2)
		}
		if m.queryBrowser != nil {
			m.queryBrowser.SetWidth(msg.Width)
			m.queryBrowser.SetHeight(msg.Height)
		}
		if m.ask != nil {
			m.ask.SetWidth(msg.Width)
			m.ask.SetHeight(msg.Height)
		}
		return m, tickToast()

	case toastTickMsg:
		m.toast.Update()
		return m, tickToast()

	case spinnerTickMsg:
		if m.spinnerActive {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerChars)
		}
		return m, tickSpinner()

	case projectsScannedMsg:
		if len(msg.projects) == 0 {
			m.state = StateError
			m.err = fmt.Errorf("no .dbx.toml files found in ~/dev")
			return m, nil
		}
		// Count active projects
		activeCount := 0
		hasInactive := false
		for _, p := range msg.projects {
			if p.Active {
				activeCount++
			} else {
				hasInactive = true
			}
		}
		// Auto-connect: only if 1 active AND no inactive projects to manage
		// Skip auto-connect if user explicitly asked for the picker (e.g. from error)
		if activeCount == 1 && !hasInactive && !m.forcePicker {
			for _, p := range msg.projects {
				if p.Active {
					m.state = StateLoading
					m.project = &p
					m.toast.ShowInfo(fmt.Sprintf("Connecting to %s...", p.Name))
					return m, m.connectToDB(p)
				}
			}
		}
		// Show picker: multiple active, or inactive projects exist, or user forced it
		m.forcePicker = false
		m.picker.SetProjects(msg.projects)
		return m, nil

	case dbConnectedMsg:
		if m.connectCancelled {
			m.connectCancelled = false
			return m, nil
		}
		// Logged BEFORE the returns, and by PROJECT NAME rather than by DSN.
		//
		// A DSN carries the password — that is what password_env puts in it — so
		// writing one into a log file would undo the whole point of that knob. The
		// name is what a reader of the log actually wants anyway.
		if msg.err != nil {
			m.logSessionError("", msg.err)
			m.state = StateError
			m.err = msg.err
			// The spinner is stopped here for the same reason the schema-load error
			// stops it: this arm used not to, so a failed connection left the app
			// sitting on the error screen with a spinner still ticking and still
			// reissuing its command forever. Same decision, written once here and once
			// twenty lines down, and the copy that was missing is the one that fires
			// first.
			m.spinnerActive = false
			m.toast.ShowError(fmt.Sprintf("Connection failed: %v", msg.err))
			return m, nil
		}
		// The project the MESSAGE names becomes the model's project, not just the
		// argument to the schema load.
		//
		// The handler has always read msg.project here rather than m.project — which is
		// right, because connectToDB stamps the project it actually connected to onto the
		// reply — but it never wrote it back. So a model whose own project was changed
		// while the connection was in flight loaded the schema of the database it reached
		// while still calling itself STALE: the schema pane, the window title and every
		// later per-project lookup used a project that was never connected to.
		//
		// The nil case is not reachable from connectToDB, which always sets it, so this
		// keeps m.project rather than inventing an error path nothing takes.
		if msg.project != nil {
			m.project = msg.project
		}
		if m.project != nil {
			m.logSessionConnect(m.project.Name)
		}
		m.conn = msg.conn
		// The runner needs to open transactions and the message carries the wider
		// postgres.Conn, so the two methods are recovered here. connectToDB hands over a
		// *pgx.Conn, which satisfies both, so this cannot fail in the app — and if it ever
		// did, the honest answer is a visible error rather than a nil runner, because a
		// nil runner means every statement refuses with "not connected" while the app
		// displays "Connected to database".
		beginner, ok := msg.conn.(postgres.TxBeginner)
		if !ok {
			m.state = StateError
			m.err = fmt.Errorf("connection cannot open transactions")
			m.spinnerActive = false
			m.toast.ShowError("Connected, but this connection cannot open transactions")
			return m, nil
		}
		m.runner = newStatementRunner(beginner)
		m.syncTxStatus()
		m.state = StateLoading
		m.toast.ShowInfo("Connected to database")
		return m, m.loadSchema(msg.conn, *msg.project)

	case schemaLoadedMsg:
		if m.connectCancelled {
			m.connectCancelled = false
			return m, nil
		}
		if msg.err != nil {
			m.state = StateError
			m.err = msg.err
			m.spinnerActive = false
			m.toast.ShowError(fmt.Sprintf("Schema load failed: %v", msg.err))
			return m, nil
		}
		m.explorer = explorer.New(m.styles, nil, m.keybinds)
		m.explorer.SetWidth(m.width)
		m.explorer.SetHeight(m.height - 2)
		m.explorer.SetNodes([]*explorer.Node{msg.root})
		m.grid.SetWidth(m.width)
		m.grid.SetHeight(m.height - 4)
		m.schemaDetail = msg.schemaDetail
		m.dbName = msg.dbName
		m.askSchemaText = postgres.SchemaText(msg.schemaDetail)
		// Initialize per-project query store after project selection
		if m.project != nil {
			m.initQueryStore(m.project.Name, "")
		}
		m.state = StateMain
		m.toast.ShowSuccess("Schema loaded")
		m.spinnerActive = true

		if msg.targetTable != "" {
			m.router.FocusPane(FocusExplorer)
			m.explorer.SelectTable(msg.targetSchema, msg.targetTable)
		}

		return m, tea.Batch(m.loadAutocompleteData(), tickSpinner())

	case autocompleteDataLoadedMsg:
		m.spinnerActive = false
		if msg.schemaExport != nil {
			m.editor.SetSchema(msg.schemaExport)
			m.keybindsPane.SetAutocompleteReady(true)
			m.toast.ShowSuccess("Autocomplete ready")
		}
		if msg.schemaForeignKeys != nil {
			m.schemaForeignKeys = msg.schemaForeignKeys
			m.explorerPreview.SetSchemaForeignKeys(msg.schemaForeignKeys)
		}
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
		m.explorerPreview.SetData(msg.schema, msg.table, nil, nil, nil, nil, nil)
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
		var gridColumns []postgres.ColumnInfo
		if gridData := m.grid.Data(); gridData != nil {
			gridColumns = gridData.Columns
		}
		m.explorerPreview.SetData(msg.schema, msg.table, gridColumns, constraints, foreignKeys, indexes, msg.overview)
		if row := m.grid.SelectedRow(); row != nil {
			cmd := m.syncGridSidebarPreviewForCursor()
			return m, cmd
		}
		return m, nil

	case explorerPreviewDataMsg:
		m.explorerPreview.SetSchemaForeignKeys(m.schemaForeignKeys)
		m.explorerPreview.SetData(msg.schema, msg.table, msg.columns, msg.constraints, msg.foreignKeys, msg.indexes, msg.overview)
		return m, nil

	case explorerpreview.ERENavigateMsg:
		if m.explorer != nil {
			m.explorer.SelectTable(msg.Schema, msg.Table)
		}
		// Load data for the new table
		return m, m.loadExplorerPreviewData(msg.Schema, msg.Table)

	case grid.GridCommitPendingMsg:
		if m.conn == nil {
			return m, nil
		}
		if m.refuseIfBusy() {
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

	case grid.GridCommitAllMsg:
		if m.conn == nil {
			return m, nil
		}
		if m.refuseIfBusy() {
			return m, nil
		}
		var executed int
		for i, query := range msg.Queries {
			_, err := m.conn.Exec(context.Background(), query, msg.Args[i]...)
			if err != nil {
				m.toast.ShowError(fmt.Sprintf("Commit failed at query %d: %v", i+1, err))
				return m, nil
			}
			executed++
		}
		m.toast.ShowSuccess(fmt.Sprintf("%d change(s) committed", executed))
		return m, m.loadTableData(msg.Schema, msg.Table)

	case grid.GridUndoRowMsg:
		if msg.Count > 0 {
			m.toast.ShowSuccess(fmt.Sprintf("Undid %d draft change(s) on row", msg.Count))
		} else {
			m.toast.ShowInfo("No drafts on this row")
		}

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
		cmd := m.syncGridSidebarPreviewForCursor()
		return m, cmd

	case grid.ExportSelectedMsg:
		return m, m.handleExport(msg)

	case exportDoneMsg:
		if msg.err != nil {
			m.toast.ShowError(fmt.Sprintf("Export failed: %v", msg.err))
		} else if msg.clipboard {
			if msg.yankCount > 0 {
				m.toast.ShowSuccess(fmt.Sprintf("Copied %d rows to clipboard", msg.yankCount))
			} else {
				m.toast.ShowSuccess("Copied to clipboard")
			}
		} else {
			m.toast.ShowSuccess(fmt.Sprintf("Exported to %s", msg.filename))
		}
		return m, nil

	case editor.CopySQLMsg:
		appDebugLog("CopySQLMsg received")
		return m, m.handleCopySQL()

	case copySQLDoneMsg:
		appDebugLog("copySQLDoneMsg: err=%v", msg.err)
		if msg.err != nil {
			m.toast.ShowError("Clipboard not available")
		} else {
			m.toast.ShowSuccess("SQL copied to clipboard")
		}
		return m, nil

	case grid.GridTabChangeMsg:
		cmd := m.syncGridSidebarPreviewForCursor()
		return m, cmd

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
				ScrollRow: m.grid.ScrollRow(),
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

	case gridpreview.GridPreviewExpandFKMsg:
		if msg.RefTable == "" || m.conn == nil {
			return m, nil
		}
		refSchema := msg.RefSchema
		if refSchema == "" {
			refSchema = m.prevSchema
		}
		fkWhere := fmt.Sprintf("%q = %s", msg.RefColumn, formatFKValue(msg.Value))
		return m, func() tea.Msg {
			loader := postgres.NewSchemaLoader(m.conn)
			opts := postgres.SelectOptions{
				Schema: refSchema,
				Where:  fkWhere,
				Limit:  1,
			}
			result, err := loader.Select(context.Background(), msg.RefTable, opts)
			if err != nil {
				return gridpreview.GridPreviewExpandFKResultMsg{Err: err}
			}
			if result == nil || len(result.Rows) == 0 {
				return gridpreview.GridPreviewExpandFKResultMsg{Err: fmt.Errorf("no row found")}
			}
			rowMap := make(map[string]interface{})
			for i, col := range result.Columns {
				if i < len(result.Rows[0]) {
					rowMap[col.Name] = result.Rows[0][i]
				}
			}
			refFKs, _ := loader.ListForeignKeys(context.Background(), refSchema, msg.RefTable)
			return gridpreview.GridPreviewExpandFKResultMsg{
				Column:      msg.Column,
				Path:        msg.Path,
				Row:         rowMap,
				ForeignKeys: refFKs,
			}
		}

	case gridpreview.GridPreviewExpandFKResultMsg:
		if msg.Err != nil {
			m.toast.ShowError(fmt.Sprintf("FK expand: %v", msg.Err))
			return m, nil
		}
		if m.gridPreview != nil {
			m.gridPreview.ExpandFK(msg.Column, msg.Path, msg.Row, msg.ForeignKeys)
		}
		return m, nil

	case GridSidebarFKPreviewLookupResultMsg:
		if msg.Token != m.gridSidebarFKPreviewCursor {
			return m, nil
		}
		if msg.Err != nil {
			if row := m.grid.SelectedRow(); row != nil {
				m.gridSidebarPreview.SetRow(m.grid.Columns(), row)
				m.syncGridPreview()
			}
			return m, nil
		}
		if msg.CacheKey != "" {
			m.storeGridSidebarFKPreviewCache(msg.CacheKey, msg.CacheVal, msg.Columns, msg.Row)
		}
		m.gridSidebarPreview.SetFKRow(msg.Columns, msg.Row, msg.RefTable)
		if m.gridPreview != nil && m.gridPreview.IsFocused() {
			m.gridPreview.SetRow(msg.Columns, msg.Row)
		}
		return m, nil

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

	case grid.GridRefreshConfirmMsg:
		m.toast.ShowSuccess("Query refreshed")
		return m, m.loadTableDataWithSortAndWhere(msg.Schema, msg.Table, msg.OrderBy, msg.OrderDir, msg.Where)

	case queryExecutedMsg:
		m.queryExecuting = false
		if m.editor != nil {
			m.editor.SetCommitOnRun(false)
		}
		m.syncTxStatus()
		// The error is answered FIRST. It used to be answered second, with the success
		// toast in front of it, so any message carrying both a committedTx and an error
		// announced "Transaction committed" and then "Query failed" for the same statement.
		// The boolean no longer says that any more — commitPending returns false when the
		// commit fails — but the ORDER is the defect, and an ordering that can contradict
		// itself is worth not having regardless of what feeds it.
		if msg.err != nil {
			m.logSessionError(msg.sql, msg.err)
			if msg.commitFailed {
				return m.handleCommitFailure(msg.err)
			}
			m.toast.ShowError(fmt.Sprintf("Query failed: %v", msg.err))
			return m, nil
		}
		if msg.committedTx {
			m.toast.ShowSuccess("Transaction committed")
		}
		m.logSessionQuery(msg.sql, msg.duration, len(msg.result.Rows))
		m.editorOpen = false
		m.editor.Blur()
		m.keybindsPane.SetEditorOpen(false)
		m.editor.PushHistory(msg.sql)
		m.queryStore.Add(msg.sql)

		if m.isDDL(msg.sql) && m.conn != nil && m.project != nil {
			schema, table := extractDDLTableName(msg.sql)
			m.router.FocusPane(FocusExplorer)
			m.toast.ShowSuccess("DDL executed")
			return m, m.loadSchemaWithTarget(m.conn, *m.project, schema, table)
		}

		m.grid.SetData(msg.result, "", "query")
		m.router.FocusPane(FocusGrid)
		m.toast.ShowSuccess(fmt.Sprintf("Query returned %d rows", msg.result.Count))
		return m, nil

	case querybrowser.QuerySelectedMsg:
		m.queryBrowserOpen = false
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(msg.SQL)
		m.keybindsPane.SetEditorOpen(true)
		m.keybindsPane.SetQueryBrowserOpen(false)
		return m, nil

	case ask.AskSubmittedMsg:
		m.askGenSeq++
		return m, m.generateAskSQL(msg.Question, m.askGenSeq)

	case ask.AskConfirmMsg:
		return m, m.executeAskSQL(msg.SQL, m.askGenSeq)

	case ask.AskClosedMsg:
		m.askOpen = false
		return m, nil

	case askGeneratedMsg:
		if m.ask == nil {
			return m, nil
		}
		if msg.seq != m.askGenSeq {
			appDebugLog("Ask: ignoring stale generation seq=%d (current=%d)", msg.seq, m.askGenSeq)
			return m, nil
		}
		if msg.err != nil {
			m.ask.SetError(msg.err)
			return m, nil
		}
		m.ask.SetGeneratedSQL(msg.sql)
		return m, nil

	case askQueryExecutedMsg:
		if m.ask == nil {
			return m, nil
		}
		if msg.seq != m.askGenSeq {
			appDebugLog("Ask: ignoring stale execution seq=%d (current=%d)", msg.seq, m.askGenSeq)
			return m, nil
		}
		if msg.err != nil {
			m.ask.SetError(msg.err)
			return m, nil
		}
		m.ask.MarkExecuted()
		m.askOpen = false
		m.ask.Hide()
		// Record ASK queries in history like editor queries, so they can be
		// recalled from the query browser and the editor.
		if m.editor != nil {
			m.editor.PushHistory(msg.sql)
		}
		if m.queryStore != nil {
			m.queryStore.Add(msg.sql)
		}
		m.grid.SetData(msg.result, "", "query")
		m.router.FocusPane(FocusGrid)
		m.toast.ShowSuccess(fmt.Sprintf("Query returned %d rows", msg.result.Count))
		return m, nil

	case querybrowser.QueryBrowserClosedMsg:
		m.queryBrowserOpen = false
		m.keybindsPane.SetQueryBrowserOpen(false)
		return m, nil

	case explorer.TableSelectedMsg:
		return m, m.loadTableData(msg.Schema, msg.Table)

	case explorer.NewTableMsg:
		schema := msg.Schema
		if schema == "" {
			schema = "public"
		}
		prefix := fmt.Sprintf("CREATE TABLE %s.", schema)
		content := prefix + "new_table (\n    id SERIAL PRIMARY KEY\n);"
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(content)
		m.editor.SetCursorPos(0, len(prefix))
		m.keybindsPane.SetEditorOpen(true)
		return m, nil

	case explorer.DropTableMsg:
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(fmt.Sprintf("DROP TABLE %s.%s;", msg.Schema, msg.Table))
		m.keybindsPane.SetEditorOpen(true)
		return m, nil

	case explorer.ViewDDLMsg:
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent(fmt.Sprintf("-- DDL for %s.%s\n-- Run this query to see the table definition:\nSELECT pg_get_tabledef('%s', '%s');", msg.Schema, msg.Table, msg.Schema, msg.Table))
		m.keybindsPane.SetEditorOpen(true)
		return m, nil

	case explorer.ExplorerRefreshMsg:
		if m.project != nil && m.conn != nil {
			m.toast.ShowSuccess("Schema refreshed")
			return m, m.loadSchema(m.conn, *m.project)
		}
		return m, nil

	case picker.ConnectionSelectedMsg:
		m.state = StateLoading
		m.project = &msg.Project
		m.connectCancelled = false
		m.toast.ShowInfo(fmt.Sprintf("Connecting to %s...", msg.Project.Name))
		return m, m.connectToDB(msg.Project)

	case picker.ProjectToggledMsg:
		// Persist the active/inactive state
		state, err := config.LoadProjectState()
		if err == nil {
			if msg.Active {
				state.SetActive(msg.Project.Path)
			} else {
				state.SetInactive(msg.Project.Path)
			}
			_ = state.Save()
		}
		if msg.Active {
			m.toast.ShowInfo(fmt.Sprintf("Enabled %s", msg.Project.Name))
		} else {
			m.toast.ShowInfo(fmt.Sprintf("Disabled %s", msg.Project.Name))
		}
		return m, nil

	case palette.CommandSelectedMsg:
		return m.handlePaletteCommand(msg.Action)

	case tea.MouseWheelMsg:
		if m.state != StateMain {
			return m, nil
		}
		// The help modal scrolls itself; do not scroll panes behind it.
		// The help modal, offered the wheel FIRST: a visible modal scrolls itself, so the pane
		// behind it must not.
		//
		// There is no "and if the modal declines, do nothing" second return, and there used to
		// be one. HelpModal.Update handles every tea.MouseWheelMsg — up, down, and anything
		// else as a down — so `handled` is true on every path and the branch was a dead
		// statement that read as if the modal might not want the wheel.
		if m.helpModal.IsVisible() {
			if cmd, handled := m.helpModal.Update(msg); handled {
				return m, cmd
			}
		}
		if m.editorOpen || m.palette.IsVisible() ||
			(m.queryBrowserOpen && m.queryBrowser != nil) || m.grid.IsExporting() ||
			(m.askOpen && m.ask != nil) {
			return m, nil
		}

		mm := msg.Mouse()
		direction := 0 // -1 = up, +1 = down
		switch mm.Button {
		case tea.MouseWheelUp:
			direction = -1
		case tea.MouseWheelDown:
			direction = 1
		}
		if direction == 0 {
			return m, nil
		}

		// Scroll the pane under the cursor instead of the focused one. The
		// sidebar has its own zone so hovering the grid no longer scrolls it.
		switch {
		case ui.InBounds(zonePaneGridSidebar, msg) && m.gridSidebarPreview != nil:
			if direction < 0 {
				m.gridSidebarPreview.ScrollUp()
			} else {
				m.gridSidebarPreview.ScrollDown()
			}
		case ui.InBounds(zonePaneGrid, msg) && m.grid != nil:
			if direction < 0 {
				m.grid.HandleAction("navigate_up")
			} else {
				m.grid.HandleAction("navigate_down")
			}
			if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
				return m, cmd
			}
		case ui.InBounds(zonePaneExplorer, msg) && m.explorer != nil:
			if direction < 0 {
				m.explorer.HandleAction("navigate_up")
			} else {
				m.explorer.HandleAction("navigate_down")
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.state != StateMain {
			return m, nil
		}
		// Ignore clicks while a modal-like overlay is on top; these used to
		// swallow keys only, so a click behind them could act on a hidden pane.
		if m.editorOpen || m.helpModal.IsVisible() || m.palette.IsVisible() ||
			(m.queryBrowserOpen && m.queryBrowser != nil) || m.grid.IsExporting() ||
			(m.askOpen && m.ask != nil) {
			return m, nil
		}

		mm := msg.Mouse()
		now := time.Now()
		isDoubleClick := !m.lastClickTime.IsZero() &&
			now.Sub(m.lastClickTime) < 300*time.Millisecond &&
			abs(mm.X-m.lastClickX) <= 2 &&
			abs(mm.Y-m.lastClickY) <= 2

		m.lastClickTime = now
		m.lastClickX = mm.X
		m.lastClickY = mm.Y

		// Route by the pane that is actually rendered. Only the visible pane
		// gets a zone, so this respects the current focus/layout without
		// assuming a fixed explorer/grid split.
		if ui.InBounds(zonePaneGrid, msg) && m.grid != nil {
			localX, localY := ui.Pos(zonePaneGrid, msg)
			cmd, hitCell := m.grid.HandleClick(localX, localY)
			if isDoubleClick && hitCell {
				enterCmd, _ := m.grid.HandleAction("edit_cell")
				return m, tea.Batch(cmd, enterCmd)
			}
			var sidebarCmd tea.Cmd
			if hitCell {
				sidebarCmd = m.syncGridSidebarPreviewForCursor()
			}
			return m, tea.Batch(cmd, sidebarCmd)
		}

		if ui.InBounds(zonePaneExplorer, msg) && m.explorer != nil {
			_, localY := ui.Pos(zonePaneExplorer, msg)
			m.explorer.HandleClick(localY)
			if isDoubleClick {
				return m, m.explorer.ToggleExpand()
			}
			return m, nil
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

		if m.queryBrowserOpen && m.queryBrowser != nil {
			if cmd, handled := m.queryBrowser.Update(msg); handled {
				return m, cmd
			}
			return m, nil
		}

		if m.askOpen && m.ask != nil {
			if cmd, handled := m.ask.Update(msg); handled {
				return m, cmd
			}
			return m, nil
		}

		if m.state == StatePicker {
			// NO `if key == "q" || key == "ctrl+c"` fallback, and there used to be one. It
			// could not run: the picker is offered the key FIRST and it binds q and ctrl+c
			// itself — to tea.Quit, which is the same thing this branch was doing. So the
			// picker never declines them and the branch below was a second copy of a decision
			// that had already been made, written as if it might not have been.
			//
			// Which is worth stating, because the two copies LOOKED like different decisions:
			// "close the picker" and "quit the app" are different answers to the same key. The
			// picker's is the live one, and it quits — so a user pressing q in the project
			// picker quits dbx, and that is what has always happened.
			if cmd, handled := m.picker.Update(msg); handled {
				return m, cmd
			}
			return m, nil
		}

		if m.state == StateError {
			key := msg.String()
			if key == "q" || key == "ctrl+c" {
				return m, tea.Quit
			}
			if key == "r" || key == "enter" {
				if m.project != nil {
					m.state = StateLoading
					m.connectCancelled = false
					m.toast.ShowInfo(fmt.Sprintf("Retrying connection to %s...", m.project.Name))
					return m, m.connectToDB(*m.project)
				}
			}
			if key == "esc" {
				m.connectCancelled = true
				m.forcePicker = true
				m.state = StatePicker
				return m, m.scanProjects()
			}
			return m, nil
		}

		if m.state == StateLoading {
			key := msg.String()
			if key == "esc" || key == "q" || key == "ctrl+c" {
				m.connectCancelled = true
				m.forcePicker = true
				m.state = StatePicker
				return m, m.scanProjects()
			}
			return m, nil
		}

		if m.state == StateMain {
			key := msg.String()
			// Declared here, not three blocks down. The carve-out below is what a text input
			// consults to decide whether a key is still the app's, and it needs the router
			// context to answer. It used to be declared at the explorer filter, which meant
			// every block ABOVE it had no way to ask — and the column filter, the one input
			// where the question matters most, therefore asked nothing.
			context := m.router.Context()
			appDebugLog("KeyPress: key=%q editorOpen=%v focus=%q", key, m.editorOpen, m.router.Focus())

			if m.editorOpen {
				if action, ok := m.keybinds.Resolve(key, config.ContextEditor); ok && m.hasAppAction(action) {
					return m.dispatchAction(action)
				}

				if cmd, handled := m.editor.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			// The column filter gets the carve-out, which it did not have, and the reason
			// it matters is spelled out in the comment four blocks below: `?` is the key a
			// user reaches for when confused, and a filter is what you open when you cannot
			// find the column you want. Two of the three filters that look like this one —
			// the explorer's table filter and jq input — already carve `?` out. This one did
			// not, so `?` while hunting for a column typed a literal `?` into the filter:
			// the one moment help is needed was the one moment help could not be opened.
			//
			// The list also takes `U` (rollback) out of the filter, and that costs nothing:
			// jumpToBestMatch lowercases both the column name and the query, so the filter is
			// case-insensitive and `u` does the same job. Verified, not assumed.
			// NO `return m, nil` after the column filter declines, and there used to be one. It
			// could not run: Grid.handleFilterKey ends in a `default` that appends any single
			// character to the filter and returns handled=true, so it declines NOTHING — not a
			// function key, not ctrl+c, not a modifier combo. A text filter swallows every key
			// on purpose, which is why ctrl+c does nothing inside one, so the grid handles the
			// key on every path and there is no fall-through to write.
			//
			// The WHERE filter below is the contrast: its handler ends in a
			// `if len(key) == 1` insert, so a two-character key name like "f1" IS declined,
			// and its `return m, nil` is the live one.
			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsFiltering() {
				if action, ok := m.keybinds.Resolve(key, context); ok && actionSurvivesTextInput(action) {
					return m.dispatchAction(action)
				}
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsWhereFiltering() {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			// A widget that owns the keyboard gets the app-wide keys carved out
			// first, from ONE list. This block used to hand every key straight
			// to the filter, so `?` typed a literal `?` instead of opening help
			// — the one key a user reaches for when confused, which is exactly
			// when they are filtering. The jq line below had the carve-out and
			// this did not; actionSurvivesTextInput is now what both read.
			//
			// THREE inputs look like this one and only two of them may have the list, which
			// is worth writing down because the shape invites the wrong fix:
			//
			//	the column filter above: yes. It matches COLUMN NAMES, which contain no
			//	  `?` and no `:`, and it is case-insensitive so losing `U` is free.
			//
			//	the where filter just below: NO, and `?` is the reason. It builds a
			//	  WHERE clause, and `?` is PostgreSQL's bind parameter — carving help
			//	  out of it would make a parameterised filter untypable. The
			//	  "consistency" fix breaks the feature.
			//
			//	the cell editor: not settled. `?` is a legal character of a cell VALUE and
			//	  case is significant there, so unlike the filters, `U` is not free. Whether
			//	  help should win over a `?` in a data value is a UX judgement, not a
			//	  defect, and this does not make it. Pinned with the two callers above named.

			// No `return m, nil` here either, for the same reason as the column filter above:
			// Explorer's filter handler also ends in a default that swallows the key and
			// reports handled. Both text filters claim every key, and both mean it.
			if m.router.Focus() == FocusExplorer && m.explorer != nil && m.explorer.IsFiltering() {
				if action, ok := m.keybinds.Resolve(key, context); ok && actionSurvivesTextInput(action) {
					return m.dispatchAction(action)
				}
				if cmd, handled := m.explorer.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.IsEditing() {
				if cmd, handled := m.grid.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			// JQ input mode swallows every key except the few app-wide ones
			// that stay available while typing a filter — the same carve-out,
			// from the same list, as the explorer's table filter above.
			// No `return m, nil` here either, and the reason is different from the two filters
			// above: this whole block sits inside `case tea.KeyPressMsg:`, so msg IS a key
			// press — and the jq handler's ONLY way to decline is
			// `if _, ok := msg.(tea.KeyPressMsg); !ok`, which cannot be true for a message
			// that was type-switched as one. A jq input swallows every key it is given, which
			// is exactly what makes it a text field.
			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil && m.gridPreview.IsJQMode() {
				if action, ok := m.keybinds.Resolve(key, context); ok && actionSurvivesTextInput(action) {
					return m.dispatchAction(action)
				}
				if cmd, handled := m.gridPreview.Update(msg); handled {
					return m, cmd
				}
			}

			if action, ok := m.keybinds.Resolve(key, context); ok && m.hasAppAction(action) {
				return m.dispatchAction(action)
			}

			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				if cmd, handled := m.gridPreview.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusExplorer && m.explorer != nil {
				if cmd, handled := m.explorer.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusExplorerPreview && m.explorerPreview != nil {
				if cmd, handled := m.explorerPreview.Update(msg); handled {
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

func (m Model) handlePaletteCommand(action config.ActionID) (tea.Model, tea.Cmd) {
	return m.dispatchAction(action)
}

// hasAppAction reports whether the app dispatches this action itself (as
// opposed to a focused component handling it).
func (m Model) hasAppAction(id config.ActionID) bool {
	_, ok := m.appActions()[id]
	return ok
}

// dispatchAction invokes the single app-level handler for an action. This is
// the one table both key dispatch and the palette go through.
func (m Model) dispatchAction(id config.ActionID) (tea.Model, tea.Cmd) {
	if handler, ok := m.appActions()[id]; ok {
		return handler(m)
	}
	m.toast.ShowInfo(fmt.Sprintf("Command: %s", id))
	return m, nil
}

func (m Model) appActions() map[config.ActionID]func(Model) (tea.Model, tea.Cmd) {
	return map[config.ActionID]func(Model) (tea.Model, tea.Cmd){
		"quit": func(m Model) (tea.Model, tea.Cmd) {
			m.rollbackOnExit()
			return m, tea.Quit
		},
		"help": func(m Model) (tea.Model, tea.Cmd) {
			m.helpModal.Show()
			return m, nil
		},
		"palette": func(m Model) (tea.Model, tea.Cmd) {
			m.palette.Show()
			return m, nil
		},
		"ask": func(m Model) (tea.Model, tea.Cmd) {
			return m.handleAskOpen()
		},
		"toggle_explorer_focus": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				m.router.FocusPane(FocusExplorer)
				m.gridPreview.Blur()
				return m, nil
			}
			if m.router.Focus() == FocusExplorerPreview && m.explorerPreview != nil {
				m.router.FocusPane(FocusGrid)
				m.explorerPreview.Blur()
				return m, nil
			}
			m.router.CycleFocus()
			return m, nil
		},
		"focus_explorer": func(m Model) (tea.Model, tea.Cmd) {
			m.router.FocusPane(FocusExplorer)
			return m, nil
		},
		"focus_grid": func(m Model) (tea.Model, tea.Cmd) {
			m.router.FocusPane(FocusGrid)
			return m, nil
		},
		"focus_editor": func(m Model) (tea.Model, tea.Cmd) {
			m.editorOpen = !m.editorOpen
			if m.editorOpen {
				m.editor.Focus()
			} else {
				m.editor.Blur()
			}
			m.keybindsPane.SetEditorOpen(m.editorOpen)
			return m, nil
		},
		"rollback": func(m Model) (tea.Model, tea.Cmd) {
			return m.handleRollback()
		},
		"switch_connection": func(m Model) (tea.Model, tea.Cmd) {
			m.state = StatePicker
			return m, m.scanProjects()
		},
		"query_browser": func(m Model) (tea.Model, tea.Cmd) {
			if !m.queryBrowserOpen && !m.editorOpen {
				m.queryBrowserOpen = true
				m.queryBrowser.Show()
				m.keybindsPane.SetQueryBrowserOpen(true)
			}
			return m, nil
		},
		"focus_preview": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.HasData() &&
				!m.grid.IsEditing() && !m.grid.IsWhereFiltering() {
				m.router.FocusPane(FocusGridPreview)
				if m.gridPreview != nil {
					m.gridPreview.Focus()
					m.syncGridPreview()
				}
			}
			return m, nil
		},
		"preview_back": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				m.router.FocusPane(FocusGrid)
				m.gridPreview.Blur()
				return m, nil
			}
			if m.router.Focus() == FocusExplorerPreview && m.explorerPreview != nil {
				m.router.FocusPane(FocusExplorer)
				m.explorerPreview.Blur()
			}
			return m, nil
		},
		"explorer_open_preview": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() != FocusExplorer || m.explorer == nil {
				return m, nil
			}
			selected := m.explorer.Selected()
			if selected == nil || selected.Type != explorer.NodeTable {
				return m, nil
			}
			schema := ""
			if s, ok := selected.Metadata["schema"].(string); ok {
				schema = s
			}
			m.router.FocusPane(FocusExplorerPreview)
			m.explorerPreview.Focus()
			return m, m.loadExplorerPreviewData(schema, selected.Name)
		},
		"preview_cursor_up": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				m.gridPreview.HandleAction("navigate_up")
			}
			return m, nil
		},
		"preview_cursor_down": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				m.gridPreview.HandleAction("navigate_down")
			}
			return m, nil
		},
		"refresh_schema": func(m Model) (tea.Model, tea.Cmd) {
			if m.project != nil && m.conn != nil {
				m.toast.ShowSuccess("Schema refreshed")
				return m, m.loadSchema(m.conn, *m.project)
			}
			return m, nil
		},
		"refresh_data": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() != FocusGrid || m.grid == nil {
				return m, nil
			}
			if cmd, handled := m.grid.Refresh(); handled {
				return m, cmd
			}
			return m, nil
		},
		"export": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.HasData() {
				if cmd, handled := m.grid.StartExport(); handled {
					return m, cmd
				}
			}
			return m, nil
		},
		"undo_drafts": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() == FocusGrid && m.grid != nil && m.grid.HasData() {
				count := m.grid.UndoRowDrafts()
				if count > 0 {
					m.toast.ShowSuccess(fmt.Sprintf("Undid %d draft change(s) on row", count))
				} else {
					m.toast.ShowInfo("No drafts on this row")
				}
			}
			return m, nil
		},
		"execute_query": func(m Model) (tea.Model, tea.Cmd) {
			if !m.editorOpen {
				m.editorOpen = true
				m.editor.Focus()
				m.keybindsPane.SetEditorOpen(true)
				return m, nil
			}
			sql := preprocessSQL(m.editor.Content())
			if sql != "" && m.conn != nil && !m.queryExecuting {
				m.queryExecuting = true
				return m, m.executeQuery(sql)
			}
			return m, nil
		},
		"clear_editor": func(m Model) (tea.Model, tea.Cmd) {
			m.editor.Clear()
			m.toast.ShowInfo("Editor cleared")
			return m, nil
		},
		"copy_sql": func(m Model) (tea.Model, tea.Cmd) {
			if !m.editorOpen {
				m.editorOpen = true
				m.editor.Focus()
				m.keybindsPane.SetEditorOpen(true)
			}
			return m, m.handleCopySQL()
		},
		"close_editor": func(m Model) (tea.Model, tea.Cmd) {
			// Esc first dismisses an open autocomplete popup; the editor only
			// closes when there is nothing left to cancel.
			if m.editor.CancelAutocomplete() {
				return m, nil
			}
			m.editorOpen = false
			m.editor.Blur()
			m.editor.SetCommitOnRun(false)
			m.keybindsPane.SetEditorOpen(false)
			return m, nil
		},
		"commit_drafts": func(m Model) (tea.Model, tea.Cmd) {
			if m.router.Focus() != FocusGrid || m.grid == nil {
				return m, nil
			}
			appDebugLog("Ctrl+S: grid.commit_pending, hasDrafts=%v", m.grid.HasDrafts())
			if m.grid.HasDrafts() {
				sql := m.grid.DraftSQL()
				appDebugLog("Ctrl+S: draft SQL=%q", sql)
				if sql != "" {
					m.editorOpen = true
					m.editor.Focus()
					m.editor.SetContent(sql)
					m.editor.SetCommitOnRun(true)
					m.keybindsPane.SetEditorOpen(true)
				}
			}
			return m, nil
		},
	}
}

// HandledActions lists the app-level actions that have a dispatch handler.
func (m Model) HandledActions() []config.ActionID {
	handlers := m.appActions()
	ids := make([]config.ActionID, 0, len(handlers))
	for id := range handlers {
		ids = append(ids, id)
	}
	return ids
}

func (m Model) handleRollback() (tea.Model, tea.Cmd) {
	if m.runner == nil || !m.runner.pending() {
		m.toast.ShowInfo("No pending transaction")
		return m, nil
	}

	if _, err := m.runner.rollback(context.Background()); err != nil {
		appDebugLog("Rollback: error=%v", err)
		m.toast.ShowError(fmt.Sprintf("Rollback failed: %v", err))
		m.syncTxStatus()
		return m, nil
	}

	appDebugLog("Rollback: transaction rolled back")
	m.toast.ShowSuccess("Transaction rolled back")
	m.syncTxStatus()
	return m, nil
}

// rollbackOnExit discards a pending transaction before the connection is
// closed, so quitting never leaves changes half-applied.
func (m Model) rollbackOnExit() {
	if m.runner != nil && m.runner.pending() {
		if _, err := m.runner.rollback(context.Background()); err != nil {
			appDebugLog("Exit: rollback failed: %v", err)
		}
	}
	if m.conn != nil {
		if err := m.conn.Close(context.Background()); err != nil {
			appDebugLog("Exit: connection close failed: %v", err)
		}
	}
}

// handleCommitFailure answers a COMMIT that failed, and it is the only place in the app
// that asks the connection whether it survived.
//
// A failed commit has two endings and the error does not say which:
//
//	the usual one — pgx closes the whole connection when the server's transaction status
//	  is not IDLE, which it hands back to a model that still holds it. Every later
//	  statement then fails with "conn closed" and the user is told a query failed, over
//	  and over, with nothing anywhere saying the connection is gone. That was the bug.
//
//	the unlucky one — the commit landed and only the acknowledgement was lost, so the
//	  connection is fine and the DML IS stored. Reporting "reconnect" here would be
//	  false, and so would reporting "rolled back".
//
// So the connection is asked rather than assumed, and each answer gets the message it
// deserves. Dropping the connection is not done on the strength of the error: the project
// is kept, so `switch_connection` is one keypress away and the user does not lose their
// place.
func (m Model) handleCommitFailure(commitErr error) (tea.Model, tea.Cmd) {
	if m.runner == nil || m.runner.usable(context.Background()) {
		m.toast.ShowError(fmt.Sprintf(
			"Commit failed: %v — the transaction may or may not have been committed", commitErr))
		return m, nil
	}

	appDebugLog("Commit failed and the connection is gone: %v", commitErr)
	m.conn = nil
	// The runner held the connection and its hooks; keeping it would mean a runner whose
	// commands fail with a nil querier, which reads as "not connected" for a connection
	// the status bar still names.
	m.runner = nil
	// syncTxStatus with a nil runner clears the pending-transaction indicator, which is the
	// right answer: the transaction lived inside the connection pgx closed, so there is
	// nothing left to commit or roll back and claiming otherwise would offer the user a
	// rollback that cannot work.
	m.syncTxStatus()
	m.toast.ShowError(
		"Commit failed and the connection was lost. Press the switch-connection key to reconnect.")
	return m, nil
}

// syncTxStatus mirrors the pending transaction state into the statusbar.
func (m *Model) syncTxStatus() {
	if m.keybindsPane == nil || m.runner == nil {
		return
	}
	m.keybindsPane.SetTxPending(m.runner.pending())
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

		// Multiple rows: copy to clipboard or save to file
		if msg.Rows != nil {
			result := &postgres.QueryResult{
				Columns: make([]postgres.ColumnInfo, len(msg.Columns)),
				Rows:    msg.Rows,
			}
			for i, name := range msg.Columns {
				result.Columns[i] = postgres.ColumnInfo{Name: name}
			}

			var content string
			switch msg.Format {
			case grid.ExportSQL:
				content = exportAsSQL(msg.Schema, msg.Table, result)
			case grid.ExportJSON:
				content = exportAsJSON(result)
			case grid.ExportCSV:
				content = exportAsCSV(result)
			}

			// YankMode: always clipboard
			if msg.YankMode {
				if err := copyToClipboard(content); err != nil {
					return exportDoneMsg{err: fmt.Errorf("failed to copy to clipboard: %w", err)}
				}
				return exportDoneMsg{clipboard: true, yankCount: len(msg.Rows)}
			}

			// Export mode: save to file
			var filename string
			switch msg.Format {
			case grid.ExportSQL:
				filename = fmt.Sprintf("%s_%s.sql", msg.Schema, msg.Table)
			case grid.ExportJSON:
				filename = fmt.Sprintf("%s_%s.json", msg.Schema, msg.Table)
			case grid.ExportCSV:
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

// usableWidth is how many columns a result can honestly be exported as.
//
// The number of columns EVERY row has, capped at the number of columns that were
// selected. For anything PostgreSQL returns this is the column count and nothing changes;
// it exists for the ragged case, and all four exporters had a different answer to it:
//
//	CSV    wrote a short record, which encoding/csv and every other CSV reader REJECTS
//	JSON   indexed the row by column index and PANICKED
//	SQL    wrote fewer values than the header named, so the statement does not parse
//
// One assumption — a row has one value per column — written four times, and every copy
// broken in a different direction. Every exporter now asks this function first, so a
// ragged result exports something all four formats accept.
//
// Truncating is the honest answer rather than padding with NULLs: a column a row does not
// have was never read from the database, and inventing a NULL for it would put a value in
// the exported data that the query never produced.
func usableWidth(columns int, rows [][]interface{}) int {
	width := columns
	for _, row := range rows {
		if len(row) < width {
			width = len(row)
		}
	}
	if width < 0 {
		width = 0
	}
	return width
}

// rowWidth is usableWidth for the single-row exporters.
func rowWidth(columns []string, row []interface{}) int {
	return usableWidth(len(columns), [][]interface{}{row})
}

func exportAsSQL(schema, table string, result *postgres.QueryResult) string {
	var sb strings.Builder
	width := usableWidth(len(result.Columns), result.Rows)
	_, _ = fmt.Fprintf(&sb, "INSERT INTO %q.%q (%s) VALUES\n", schema, table,
		strings.Join(quoteColumns(result.Columns[:width]), ", "))

	for i, row := range result.Rows {
		values := make([]string, width)
		for j, val := range row[:width] {
			values[j] = formatSQLValue(val)
		}
		_, _ = fmt.Fprintf(&sb, "  (%s)", strings.Join(values, ", "))
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
	width := usableWidth(len(result.Columns), result.Rows)
	columns := result.Columns[:width]

	rows := make([]map[string]interface{}, len(result.Rows))
	for i, row := range result.Rows {
		rows[i] = make(map[string]interface{})
		// Over `row`, not over `columns`: the width is the minimum row length, so
		// indexing by column position is safe only if the row is at least that long,
		// which by construction it is. Iterating the row makes that structural.
		for j, val := range row[:width] {
			rows[i][columns[j].Name] = val
		}
	}

	data, _ := json.MarshalIndent(rows, "", "  ")
	return string(data) + "\n"
}

func exportAsCSV(result *postgres.QueryResult) string {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	width := usableWidth(len(result.Columns), result.Rows)

	// Header
	headers := make([]string, width)
	for i, col := range result.Columns[:width] {
		headers[i] = col.Name
	}
	// Writes to a strings.Builder never fail, so the errors are ignored.
	_ = writer.Write(headers)

	// Rows
	for _, row := range result.Rows {
		record := make([]string, width)
		for i, val := range row[:width] {
			record[i] = fmt.Sprintf("%v", val)
		}
		_ = writer.Write(record)
	}

	writer.Flush()
	return sb.String()
}

func exportRowAsSQL(schema, table string, columns []string, row []interface{}) string {
	var sb strings.Builder
	width := rowWidth(columns, row)
	_, _ = fmt.Fprintf(&sb, "INSERT INTO %q.%q (%s) VALUES\n", schema, table,
		strings.Join(quoteColumnNames(columns[:width]), ", "))

	values := make([]string, width)
	for i, val := range row[:width] {
		values[i] = formatSQLValue(val)
	}
	_, _ = fmt.Fprintf(&sb, "  (%s)\n", strings.Join(values, ", "))
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
	width := rowWidth(columns, row)
	obj := make(map[string]interface{})
	for i, col := range columns[:width] {
		obj[col] = row[i]
	}
	data, _ := json.MarshalIndent(obj, "", "  ")
	return string(data) + "\n"
}

func exportRowAsCSV(columns []string, row []interface{}) string {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	// Writes to a strings.Builder never fail, so the errors are ignored.
	_ = writer.Write(columns[:rowWidth(columns, row)])

	record := make([]string, rowWidth(columns, row))
	for i, val := range row[:len(record)] {
		record[i] = fmt.Sprintf("%v", val)
	}
	_ = writer.Write(record)

	writer.Flush()
	return sb.String()
}

// clipVia runs one clipboard helper with the content ALREADY attached to its stdin, and
// reports whether it worked.
//
// Assigning stdin before the run is the whole point of this function existing. It used to
// be assigned once, AFTER a loop that had already run two commands: xclip was invoked with
// a nil Stdin, which exec gives the child as the null device, so xclip read EOF, copied
// nothing and exited 0 — and the caller reported success over an EMPTY clipboard. The
// user saw a success toast and pasted an empty string.
//
// Every attempt now goes through here, so "give the child the content" is written once.
func clipVia(name string, args []string, content string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(content)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard: %s: %w", name, err)
	}
	return nil
}

// clipAttempt is one clipboard helper to try: the program and the arguments it needs.
type clipAttempt struct {
	name string
	args []string
}

// clipboardAttempts is the ordered list of helpers to try on a given platform.
//
// It was a switch on runtime.GOOS inside copyToClipboard, which meant the darwin and
// windows branches could only run on those platforms — and the ORDER, which is the entire
// content of the function, could not be checked at all from a Linux test run. Taking the
// platform and the wayland flag as arguments makes the decision data, so all three
// platforms are testable from anywhere.
//
// The order is deliberate and is what the app has always used: on Wayland, wl-copy is the
// native tool and the X11 helpers are emulated by XWayland, which sometimes work and
// sometimes lose the selection. On X11, xclip first because xsel's clipboard support is
// flakier under some window managers.
func clipboardAttempts(goos string, wayland bool) []clipAttempt {
	switch goos {
	case "darwin":
		return []clipAttempt{{name: "pbcopy"}}
	case "windows":
		return []clipAttempt{{name: "clip.exe"}}
	}

	attempts := make([]clipAttempt, 0, 3)
	if wayland {
		attempts = append(attempts, clipAttempt{name: "wl-copy"})
	}
	return append(attempts,
		clipAttempt{name: "xclip", args: []string{"-selection", "clipboard"}},
		clipAttempt{name: "xsel", args: []string{"--clipboard", "--input"}},
	)
}

// copyToClipboard puts content on the system clipboard, trying the helpers the running
// platform is likely to have and reporting the last failure if none of them worked.
//
// It is a VARIABLE so a test can replace it, for the same reason the CLI's loadConfig is one:
// the three callers are the export path and the copy-SQL action, and both report a clipboard
// failure with a toast — behaviour that is invisible on a machine with a working clipboard and
// unreachable on one without, because the only way to make the call fail is to have no helper
// installed. Which means the failure branch was never executed anywhere: on a developer machine
// it is never taken, and in CI it is taken for the wrong reason (no clipboard at all, so every
// assertion about it would be about the environment).
//
// So the name is a seam and the function behind it is the production value.
var copyToClipboard = func(content string) error {
	return clipViaAny(clipboardAttempts(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY") != ""), content)
}

// clipViaAny tries each helper in order and returns nil at the first success.
func clipViaAny(attempts []clipAttempt, content string) error {
	var lastErr error
	for _, a := range attempts {
		if err := clipVia(a.name, a.args, content); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	// Every helper is missing or failed. Say so with the LAST error, which names the
	// program the user would have to install, rather than a bare failure with no clue.
	//
	// The nil case is an empty list, which no current platform produces — Linux always
	// appends xclip and xsel. It is here so a future platform with no helpers gets a
	// message rather than a nil error that a caller would read as success.
	if lastErr == nil {
		lastErr = fmt.Errorf("clipboard: no helper available")
	}
	return lastErr
}

type exportDoneMsg struct {
	filename  string
	clipboard bool
	yankCount int
	err       error
}

type copySQLDoneMsg struct {
	err error
}

func (m Model) handleCopySQL() tea.Cmd {
	content := m.editor.Content()
	if content == "" {
		return nil
	}
	return func() tea.Msg {
		if err := copyToClipboard(content); err != nil {
			return copySQLDoneMsg{err: err}
		}
		return copySQLDoneMsg{}
	}
}

func (m Model) View() tea.View {
	var content string

	switch m.state {
	case StatePicker:
		content = m.picker.View()
	case StateLoading:
		content = m.styles.Text.Render("Connecting to database...") + "\n\n" +
			m.styles.Text.Render(keydisplay.Key("  esc cancel"))
	case StateError:
		content = m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)) + "\n\n" +
			m.styles.Text.Render(keydisplay.Key("  r retry  ·  esc connections  ·  q quit"))
	case StateMain:
		content = m.renderMainView()
	}

	renderedToasts := m.toast.RenderedToasts()
	for i := len(renderedToasts) - 1; i >= 0; i-- {
		content = overlayBottomRight(content, renderedToasts[i], m.width, m.height, i)
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

	if m.queryBrowserOpen && m.queryBrowser != nil {
		browserView := m.queryBrowser.View()
		if browserView != "" {
			appDebugLog("QueryBrowser View: browserView length=%d, overlaying on content length=%d", len(browserView), len(content))
			content = overlay(content, browserView, m.width, m.height)
			appDebugLog("QueryBrowser View: overlay done, content length=%d", len(content))
		} else {
			appDebugLog("QueryBrowser View: browserView is empty!")
		}
	}

	if m.askOpen && m.ask != nil {
		askView := m.ask.View()
		if askView != "" {
			content = overlay(content, askView, m.width, m.height)
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderMainView() string {
	var selSchema, selTable string
	if m.explorer != nil {
		if selected := m.explorer.Selected(); selected != nil && selected.Type == explorer.NodeTable {
			if s, ok := selected.Metadata["schema"].(string); ok {
				selSchema = s
			}
			selTable = selected.Name
		}
	}

	m.keybindsPane.SetFocus(m.router.Context())
	m.keybindsPane.SetHeight(m.height)
	m.keybindsPane.SetCanGoBack(len(m.navStack) > 0)
	topLine := m.renderTopLine(selSchema, selTable)

	countLines := func(s string) int {
		if s == "" {
			return 0
		}
		// Every non-empty shape renderTopLine can return ends in "\n" — the two bordered
		// branches append it explicitly, and the spinner branch writes it in the literal — so
		// the last element after a split is always the empty string the newline created. There
		// is no `return len(lines)` here, and there used to be one: it could not fire, because
		// the empty string is already handled by the `s == ""` return above it, which is the
		// only way a top line arrives without a trailing newline.
		lines := strings.Split(s, "\n")
		return len(lines) - 1
	}
	// The pane's OWN height, not a constant. It used to reserve 7 rows while the
	// pane drew a variable number of wrapped lines, so the content was short by
	// the difference on every window whose focused view needed more or fewer
	// than 5 segments. Fourth time in this repository that the same quantity was
	// written in two places and left to drift.
	statusBarLines := m.keybindsPane.Height() + countLines(topLine)
	contentHeight := m.height - statusBarLines
	if contentHeight < 1 {
		contentHeight = 1
	}
	hasTableData := m.grid.HasData()
	showPreview := hasTableData && m.grid.ActiveTab() == 0 && m.width >= 100

	var panes []string
	if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
		panes = append(panes, m.renderGridPreview(m.width, contentHeight))
	} else if m.router.Focus() == FocusExplorerPreview && m.explorerPreview != nil {
		panes = append(panes, m.renderExplorerPreview(m.width, contentHeight))
	} else if m.editorOpen || m.router.Focus() == FocusExplorer {
		panes = append(panes, ui.Mark(zonePaneExplorer, m.renderExplorer(m.width, contentHeight)))
	} else {
		if showPreview {
			gridW := m.width * 3 / 4
			panes = append(panes, ui.Mark(zonePaneGrid, m.renderGrid(gridW, contentHeight)))
			panes = append(panes, ui.Mark(zonePaneGridSidebar, m.renderGridSidebarPreview(m.width/4, contentHeight)))
		} else {
			panes = append(panes, ui.Mark(zonePaneGrid, m.renderGrid(m.width, contentHeight)))
		}
	}

	// Register pane bounds from the freshly rendered output and strip the zone
	// markers before layering overlays on top.
	//
	// "Previews are left unmarked" is still true — the grid preview and the explorer
	// preview have no zone and no click handling. The GRID SIDEBAR is not in that
	// category and this comment used to say it was, one line above the Mark call that
	// registers it: the sidebar exists to be hovered, and the wheel routes to the pane
	// under the cursor, which cannot happen without a zone. The comment and the code
	// disagreed, and the code is the one the wheel depends on.
	content := ui.Zones.Scan(topLine + lipgloss.JoinHorizontal(lipgloss.Top, panes...))

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

	content += "\n" + m.keybindsPane.View()

	return content
}

func (m Model) renderBreadcrumbs(selSchema, selTable string) string {
	if len(m.navStack) == 0 && selTable == "" {
		return ""
	}

	var parts []string
	for _, entry := range m.navStack {
		parts = append(parts, m.styles.TextMuted.Render(fmt.Sprintf("%s.%s", entry.Schema, entry.Table)))
	}
	if selTable != "" {
		entry := fmt.Sprintf("%s.%s", selSchema, selTable)
		parts = append(parts, m.styles.Text.Render(entry))
	}

	bread := strings.Join(parts, m.styles.TextMuted.Render(" → "))
	return "  " + bread + "\n"
}

func (m Model) renderTopLine(selSchema, selTable string) string {
	breadcrumb := m.renderBreadcrumbs(selSchema, selTable)

	var content string
	if m.spinnerActive {
		spinner := m.styles.Info.Render(spinnerChars[m.spinnerFrame])
		if breadcrumb == "" {
			return "  " + spinner + "\n"
		}
		breadInline := strings.TrimRight(breadcrumb, "\n")
		content = spinner + m.styles.TextMuted.Render(" · ") + breadInline
	} else {
		if breadcrumb == "" {
			return ""
		}
		content = strings.TrimRight(breadcrumb, "\n")
	}

	border := lipgloss.RoundedBorder()
	borderFg := m.styles.Border.GetBorderTopForeground()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Breadcrumbs ", content, m.width, 0) + "\n"
}

func (m Model) renderExplorer(w, h int) string {
	if m.explorer == nil {
		return m.styles.Border.
			Width(w - 2).
			Height(h - 2).
			MaxHeight(h - 2).
			Render(m.styles.TextMuted.Render("Explorer"))
	}

	m.explorer.SetWidth(w)
	m.explorer.SetHeight(h)

	if m.router.Focus() == FocusExplorer || m.router.Focus() == FocusExplorerPreview {
		m.explorer.Focus()
	} else {
		m.explorer.Blur()
	}

	return m.explorer.View()
}

func (m Model) renderExplorerPreview(w, h int) string {
	m.explorerPreview.SetWidth(w)
	m.explorerPreview.SetHeight(h)

	border := lipgloss.ThickBorder()
	var borderFg color.Color
	if m.router.Focus() == FocusExplorerPreview {
		borderFg = m.styles.BorderActive.GetBorderTopForeground()
	} else {
		borderFg = m.styles.Border.GetBorderTopForeground()
	}

	content := m.explorerPreview.View()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Explorer Preview ", content, w, h)
}

func (m Model) renderGrid(w, h int) string {
	m.grid.SetWidth(w)
	m.grid.SetHeight(h)

	if m.router.Focus() == FocusGrid {
		m.grid.Focus()
	} else {
		m.grid.Blur()
	}

	return m.grid.View()
}

func (m Model) renderGridSidebarPreview(w, h int) string {
	m.gridSidebarPreview.SetWidth(w)
	m.gridSidebarPreview.SetHeight(h)

	border := lipgloss.ThickBorder()
	borderFg := m.styles.Border.GetBorderTopForeground()
	content := m.gridSidebarPreview.Render()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Sidebar ", content, w, h)
}

func (m Model) renderGridPreview(w, h int) string {
	m.gridPreview.SetWidth(w)
	m.gridPreview.SetHeight(h)

	m.grid.Blur()
	m.gridPreview.Focus()

	border := lipgloss.ThickBorder()
	borderFg := m.styles.BorderActive.GetBorderTopForeground()
	content := m.gridPreview.Render()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Grid Preview ", content, w, h)
}

func (m Model) syncGridPreview() {
	if m.gridPreview == nil || !m.gridPreview.IsFocused() {
		return
	}
	columns := m.grid.Columns()
	row := m.grid.SelectedRow()
	if columns != nil && row != nil {
		m.gridPreview.SetRow(columns, row)
	}
	m.gridPreview.SetForeignKeys(m.grid.ForeignKeys())
}

func (m Model) renderEditor(w, h int) string {
	popupLines := 0
	if m.editor.AutocompleteVisible() {
		count := m.editor.AutocompleteItemCount()
		if count > 15 {
			count = 15
		}
		popupLines = count + 2
	}

	m.editor.SetWidth(w - 4)
	m.editor.SetHeight(h - 4 - popupLines)
	m.editor.Focus()

	content := m.editor.View()

	title := " SQL Editor "
	if m.editor.CommitOnRun() {
		title = " SQL Editor · commit on run "
	}

	border := lipgloss.RoundedBorder()
	borderFg := m.styles.BorderActive.GetBorderTopForeground()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, title, content, w, h)
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
	// No clamp on y, and there used to be one. bh was clamped to height three lines above, so
	// height-bh is never negative and the division is never negative. The x clamp above is NOT
	// redundant — bw is measured from the box's first line and nothing clamps it to the width —
	// and the pair read as if they were the same situation.
	y := (height - bh) / 2
	for len(lines) < y+bh {
		lines = append(lines, strings.Repeat(" ", width))
	}
	appDebugLog("overlay: baseLines=%d boxLines=%d bw=%d bh=%d width=%d height=%d x=%d y=%d",
		len(lines), len(blocks), bw, bh, width, height, x, y)
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
	y := height - bh - 7 - stackOffset*(bh+1)
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

func appDebugLog(format string, args ...interface{}) {
	debuglog.Write("app", "App", format, args...)
}
