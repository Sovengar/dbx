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
	"github.com/charmbracelet/x/ansi"

	aiContext "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/bordered"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/explorerpreview"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/buble/dbx/internal/ui/components/picker"
	"github.com/buble/dbx/internal/ui/components/gridsidebarpreview"
	"github.com/buble/dbx/internal/ui/components/querybrowser"
	"github.com/buble/dbx/internal/store"
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
	gridSidebarPreview *gridsidebarpreview.Preview
	gridPreview     *gridpreview.GridPreview
	explorerPreview *explorerpreview.ExplorerPreview
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
	gridSidebarFKPreviewCache  map[string][]gridSidebarFKPreviewCacheEntry
	gridSidebarFKPreviewCursor int
	schemaDetail               []postgres.SchemaDetail
	dbName                     string
	yankMaxRows                int
	spinnerActive              bool
	spinnerFrame               int
	schemaForeignKeys          map[string][]postgres.ForeignKeyInfo
	queryStore                 *store.QueryStore
	queryBrowser               *querybrowser.QueryBrowser
	queryBrowserOpen           bool
}

var spinnerChars = [9]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇"}

func NewModel(cfg *config.Config) Model {
	t := theme.Resolve(cfg.Theme.Mode)
	kbr := config.NewKeybindRegistry(cfg.Keybindings)
	kbs := kbr.Flatten()
	pageSize := 100
	if cfg.UI.PageSize > 0 {
		pageSize = cfg.UI.PageSize
	}
	yankMaxRows := 10
	if cfg.UI.YankMaxRows > 0 {
		yankMaxRows = cfg.UI.YankMaxRows
	}
	g := grid.New(t.Styles(), pageSize, kbs)
	g.SetYankMaxRows(yankMaxRows)
	qs := store.NewQueryStore(cfg.UI.QueryHistoryPath)
	qb := querybrowser.New(t.Styles(), qs)
	return Model{
		config:          cfg,
		theme:           t,
		styles:          t.Styles(),
		picker:          picker.New(t.Styles()),
		grid:            g,
		editor:          editor.NewSQLEditor(t.Styles()),
		gridSidebarPreview: gridsidebarpreview.New(t.Styles()),
		gridPreview:     gridpreview.New(t.Styles(), kbs),
		explorerPreview: explorerpreview.New(t.Styles(), kbs),
		router:          NewRouter(kbs),
		keybindRegistry: kbr,
		keybinds:        kbs,
		palette:         palette.New(t.Styles(), kbs),
		helpModal:       ui.NewHelpModal(t.Styles(), kbs),
		toast:           ui.NewToastManager(t.Styles()),
		statusbar:       ui.NewStatusBar(t.Styles(), kbs),
		state:           StatePicker,
		yankMaxRows:     yankMaxRows,
		queryStore:      qs,
		queryBrowser:    qb,
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

type spinnerTickMsg struct{}

func tickSpinner() tea.Cmd {
	return tea.Every(time.Millisecond*100, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
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

		result, err := loader.LoadDatabase(ctx, dbName)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("failed to load schema: %w", err)}
		}

		return schemaLoadedMsg{root: result.Root, dbName: dbName, schemaDetail: result.Schemas}
	}
}

func (m Model) loadSchemaWithTarget(conn *pgx.Conn, project config.FoundProject, targetSchema, targetTable string) tea.Cmd {
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
	schemaExport     *aiContext.SchemaExport
	schemaForeignKeys map[string][]postgres.ForeignKeyInfo
}

func buildSchemaExport(dbName string, schemas []postgres.SchemaDetail) *aiContext.SchemaExport {
	export := &aiContext.SchemaExport{Database: dbName}

	for _, sd := range schemas {
		si := aiContext.SchemaInfo{Name: sd.Name}

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

			for _, idx := range td.Indexes {
				ti.Indexes = append(ti.Indexes, aiContext.IndexInfo{
					Name:      idx.Name,
					Columns:   idx.Columns,
					IsUnique:  idx.IsUnique,
					IsPrimary: idx.IsPrimary,
				})
			}

			for _, fk := range td.FKs {
				ti.FKs = append(ti.FKs, aiContext.FKInfo{
					Name:       fk.Name,
					Columns:    fk.Column,
					RefSchema:  fk.RefSchema,
					RefTable:   fk.RefTable,
					RefColumns: fk.RefColumn,
				})
			}

			si.Tables = append(si.Tables, ti)
		}

		export.Schemas = append(export.Schemas, si)
	}

	return export
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

func extractDDLTableName(sql string) (schema, table string) {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	keyword := ""
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"):
		keyword = "CREATE TABLE"
	case strings.HasPrefix(upper, "DROP TABLE"):
		keyword = "DROP TABLE"
	case strings.HasPrefix(upper, "ALTER TABLE"):
		keyword = "ALTER TABLE"
	case strings.HasPrefix(upper, "TRUNCATE TABLE"):
		keyword = "TRUNCATE TABLE"
	}

	if keyword == "" {
		return "", ""
	}

	rest := strings.TrimSpace(sql[len(keyword):])

	upperRest := strings.ToUpper(rest)
	switch {
	case strings.HasPrefix(upperRest, "IF NOT EXISTS "):
		rest = rest[len("IF NOT EXISTS "):]
	case strings.HasPrefix(upperRest, "IF EXISTS "):
		rest = rest[len("IF EXISTS "):]
	}

	rest = strings.TrimSpace(rest)

	if len(rest) > 0 && rest[0] == '"' {
		end := strings.Index(rest[1:], "\"")
		if end >= 0 {
			table = rest[1 : end+1]
			rest = strings.TrimSpace(rest[end+2:])
			if len(rest) > 0 && rest[0] == '.' {
				schema = table
				table = ""
				rest = strings.TrimSpace(rest[1:])
				if len(rest) > 0 && rest[0] == '"' {
					end2 := strings.Index(rest[1:], "\"")
					if end2 >= 0 {
						table = rest[1 : end2+1]
					}
				}
			}
			return schema, table
		}
	}

	endIdx := 0
	for endIdx < len(rest) {
		c := rest[endIdx]
		if c == ' ' || c == '.' || c == '(' || c == '\t' || c == '\n' {
			break
		}
		endIdx++
	}
	first := rest[:endIdx]
	rest = strings.TrimSpace(rest[endIdx:])

	if len(rest) > 0 && rest[0] == '.' {
		schema = first
		rest = strings.TrimSpace(rest[1:])
		endIdx = 0
		for endIdx < len(rest) {
			c := rest[endIdx]
			if c == ' ' || c == '(' || c == '\t' || c == '\n' || c == ';' {
				break
			}
			endIdx++
		}
		table = rest[:endIdx]
	} else {
		table = first
	}

	if schema == "" {
		schema = "public"
	}

	return schema, table
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

func (m Model) executeQuery(sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		loader := postgres.NewSchemaLoader(m.conn)

		statements := splitSQL(sql)
		if len(statements) == 0 {
			return queryExecutedMsg{err: fmt.Errorf("no statements to execute")}
		}

		var lastResult *postgres.QueryResult
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			result, err := loader.ExecuteRaw(ctx, stmt)
			if err != nil {
				return queryExecutedMsg{err: err, sql: stmt}
			}
			lastResult = result
		}

		if lastResult == nil {
			lastResult = &postgres.QueryResult{}
		}
		return queryExecutedMsg{result: lastResult, sql: sql}
	}
}

// splitSQL splits a SQL string by top-level semicolons, ignoring semicolons inside strings.
func splitSQL(sql string) []string {
	var parts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]

		if ch == '\'' && !inDoubleQuote {
			if i+1 < len(sql) && sql[i+1] == '\'' {
				current.WriteByte(ch)
				current.WriteByte(ch)
				i++
				continue
			}
			inSingleQuote = !inSingleQuote
			current.WriteByte(ch)
			continue
		}

		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(ch)
			continue
		}

		if ch == ';' && !inSingleQuote && !inDoubleQuote {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
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
		m.statusbar.SetHeight(msg.Height)
		if m.explorer != nil {
			m.explorer.SetWidth(msg.Width / 3)
			m.explorer.SetHeight(msg.Height - 2)
		}
		if m.queryBrowser != nil {
			m.queryBrowser.SetWidth(msg.Width)
			m.queryBrowser.SetHeight(msg.Height)
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
		if len(msg.projects) == 1 {
			m.state = StateLoading
			m.project = &msg.projects[0]
			m.toast.ShowInfo(fmt.Sprintf("Connecting to %s...", msg.projects[0].Name))
			return m, m.connectToDB(msg.projects[0])
		}
		m.picker.SetProjects(msg.projects)
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
			m.spinnerActive = false
			m.toast.ShowError(fmt.Sprintf("Schema load failed: %v", msg.err))
			return m, nil
		}
		m.explorer = explorer.New(m.styles, nil, m.keybinds)
		m.explorer.SetWidth(m.width / 3)
		m.explorer.SetHeight(m.height - 2)
		m.explorer.SetNodes([]*explorer.Node{msg.root})
		m.grid.SetWidth(m.width * 2 / 3)
		m.grid.SetHeight(m.height - 4)
		m.schemaDetail = msg.schemaDetail
		m.dbName = msg.dbName
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
			m.statusbar.SetAutocompleteReady(true)
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
		if msg.err != nil {
			m.toast.ShowError(fmt.Sprintf("Query failed: %v", msg.err))
			return m, nil
		}
		m.editorOpen = false
		m.editor.Blur()
		m.statusbar.SetEditorOpen(false)
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
		m.statusbar.SetEditorOpen(true)
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

	case explorer.ExplorerRefreshMsg:
		if m.project != nil && m.conn != nil {
			m.toast.ShowSuccess("Schema refreshed")
			return m, m.loadSchema(m.conn, *m.project)
		}
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
				cmd := m.syncGridSidebarPreviewForCursor()
				if cmd != nil {
					return m, cmd
				}
			}
			if m.gridSidebarPreview != nil {
				mm := msg.Mouse()
				if mm.Button == tea.MouseWheelUp {
					m.gridSidebarPreview.ScrollUp()
				} else if mm.Button == tea.MouseWheelDown {
					m.gridSidebarPreview.ScrollDown()
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
				cmd := m.syncGridSidebarPreviewForCursor()
				if cmd != nil {
					return m, cmd
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

		if m.queryBrowserOpen && m.queryBrowser != nil {
			if cmd, handled := m.queryBrowser.Update(msg); handled {
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
			appDebugLog("KeyPress: key=%q editorOpen=%v focus=%q", key, m.editorOpen, m.router.Focus())

			if m.editorOpen {
				if key == "esc" {
					m.editorOpen = false
					m.editor.Blur()
					m.statusbar.SetEditorOpen(false)
					return m, nil
				}

			if key == "ctrl+enter" || key == "ctrl+r" {
				sql := preprocessSQL(m.editor.Content())
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

			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil && m.gridPreview.IsJQMode() {
				if cmd, handled := m.gridPreview.Update(msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if key == m.keybinds["global.quit"] {
				if m.conn != nil {
					m.conn.Close(context.Background())
				}
				return m, tea.Quit
			}

			if key == m.keybinds["grid.commit_pending"] && m.router.Focus() == FocusGrid && m.grid != nil {
				appDebugLog("Ctrl+S: grid.commit_pending, hasDrafts=%v", m.grid.HasDrafts())
				if m.grid.HasDrafts() {
					sql := m.grid.DraftSQL()
					appDebugLog("Ctrl+S: draft SQL=%q", sql)
					if sql != "" {
						m.editorOpen = true
						m.editor.Focus()
						m.editor.SetContent(sql)
						m.statusbar.SetEditorOpen(true)
						return m, nil
					}
				}
			}

			if key == m.keybinds["global.cycle_focus"] {
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

			if key == m.keybinds["global.query_browser"] {
				if !m.queryBrowserOpen && !m.editorOpen {
					m.queryBrowserOpen = true
					m.queryBrowser.Show()
				}
				return m, nil
			}

			if key == m.keybinds["grid.focus_preview"] && m.router.Focus() == FocusGrid && !m.grid.IsEditing() && !m.grid.IsWhereFiltering() {
				m.router.FocusPane(FocusGridPreview)
				if m.gridPreview != nil {
					m.gridPreview.Focus()
					m.syncGridPreview()
				}
				return m, nil
			}

			if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
				if key == m.keybinds["grid.focus_preview"] || key == "esc" {
					m.router.FocusPane(FocusGrid)
					m.gridPreview.Blur()
					return m, nil
				}
				if cmd, handled := m.gridPreview.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusExplorer && m.explorer != nil {
				if key == "tab" {
					if selected := m.explorer.Selected(); selected != nil && selected.Type == explorer.NodeTable {
						schema := ""
						if s, ok := selected.Metadata["schema"].(string); ok {
							schema = s
						}
						m.router.FocusPane(FocusExplorerPreview)
						m.explorerPreview.Focus()
						return m, m.loadExplorerPreviewData(schema, selected.Name)
					}
				}
				if cmd, handled := m.explorer.Update(msg); handled {
					return m, cmd
				}
			}

			if m.router.Focus() == FocusExplorerPreview && m.explorerPreview != nil {
				if key == "tab" || key == "esc" {
					m.router.FocusPane(FocusExplorer)
					m.explorerPreview.Blur()
					return m, nil
				}
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
	case "global.query_browser":
		if !m.queryBrowserOpen && !m.editorOpen {
			m.queryBrowserOpen = true
			m.queryBrowser.Show()
		}
	case "grid.focus_preview":
		if m.router.Focus() == FocusGrid && m.grid.HasData() {
			m.router.FocusPane(FocusGridPreview)
			if m.gridPreview != nil {
				m.gridPreview.Focus()
				m.syncGridPreview()
			}
		}
	case "grid-preview.cursor_up":
		if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
			m.gridPreview.Update(tea.KeyPressMsg{Code: 'k'})
		}
	case "grid-preview.cursor_down":
		if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
			m.gridPreview.Update(tea.KeyPressMsg{Code: 'j'})
		}
	case "grid-preview.toggle_explorer":
		if m.router.Focus() == FocusGridPreview && m.gridPreview != nil {
			m.router.FocusPane(FocusExplorer)
			m.gridPreview.Blur()
		}
	case "explorer.refresh":
		if m.project != nil && m.conn != nil {
			m.toast.ShowSuccess("Schema refreshed")
			return m, m.loadSchema(m.conn, *m.project)
		}
	case "grid.refresh":
		if m.router.Focus() == FocusGrid && m.grid.HasData() {
			if m.grid.HasDrafts() {
				m.grid.SetRefreshPending(true)
				return m, nil
			}
			m.toast.ShowSuccess("Query refreshed")
			return m, m.loadTableDataWithSortAndWhere(
				m.prevSchema, m.prevTable,
				m.grid.SortColumn(), m.grid.SortDirection(),
				m.grid.WhereClause(),
			)
		}
	case "global.export", "grid.export":
		if m.router.Focus() == FocusGrid && m.grid.HasData() {
			if cmd, handled := m.grid.StartExport(); handled {
				return m, cmd
			}
		}
	case "grid.undo":
		if m.router.Focus() == FocusGrid && m.grid.HasData() {
			count := m.grid.UndoRowDrafts()
			if count > 0 {
				m.toast.ShowSuccess(fmt.Sprintf("Undid %d draft change(s) on row", count))
			} else {
				m.toast.ShowInfo("No drafts on this row")
			}
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
	case "editor.copy":
		if !m.editorOpen {
			m.editorOpen = true
			m.editor.Focus()
			m.statusbar.SetEditorOpen(true)
		}
		return m, m.handleCopySQL()
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
		content = m.styles.Text.Render("Connecting to database...")
	case StateError:
		content = m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err))
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
			content = overlay(content, browserView, m.width, m.height)
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

	m.statusbar.SetFocus(m.router.Context())
	m.statusbar.SetHeight(m.height)
	topLine := m.renderTopLine(selSchema, selTable)

	countLines := func(s string) int {
		if s == "" {
			return 0
		}
		lines := strings.Split(s, "\n")
		if lines[len(lines)-1] == "" {
			return len(lines) - 1
		}
		return len(lines)
	}
	statusBarLines := 7 + countLines(topLine)
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
		panes = append(panes, m.renderExplorer(m.width, contentHeight))
	} else {
		if showPreview {
			gridW := m.width * 3 / 4
			panes = append(panes, m.renderGrid(gridW, contentHeight))
			panes = append(panes, m.renderGridSidebarPreview(m.width/4, contentHeight))
		} else {
			panes = append(panes, m.renderGrid(m.width, contentHeight))
		}
	}

	content := topLine + lipgloss.JoinHorizontal(lipgloss.Top, panes...)

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

	content += "\n" + m.statusbar.View()

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

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Breadcrumbs ", content, m.width) + "\n"
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

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Explorer Preview ", content, w)
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

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Sidebar ", content, w)
}

func (m Model) renderGridPreview(w, h int) string {
	m.gridPreview.SetWidth(w)
	m.gridPreview.SetHeight(h)

	m.grid.Blur()
	m.gridPreview.Focus()

	border := lipgloss.ThickBorder()
	borderFg := m.styles.BorderActive.GetBorderTopForeground()
	content := m.gridPreview.Render()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Grid Preview ", content, w)
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

	title := m.styles.Header.Render("SQL Editor")
	content := m.editor.View()

	return m.styles.BorderActive.
		Width(w - 2).
		Render(title + "\n" + content)
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
	f, err := os.OpenFile("/tmp/dbx_app_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "App: "+format+"\n", args...)
}
