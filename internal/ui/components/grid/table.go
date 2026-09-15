package grid

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
)

type CellEditCommitMsg struct {
	Schema string
	Table  string
	Query  string
	Args   []interface{}
}

type GridCommitPendingMsg struct {
	Schema  string
	Table   string
	Queries []string
	Args    [][]interface{}
}

type GridCommitAllMsg struct {
	Schema  string
	Table   string
	Queries []string
	Args    [][]interface{}
}

type GridUndoRowMsg struct {
	Count int
}

type PendingUpdate struct {
	RowIdx   int
	ColIdx   int
	OldValue interface{}
	NewValue interface{}
	OldRow   []interface{}
}

type PendingDelete struct {
	RowIdx int
	Row    []interface{}
}

type GridFilterApplyMsg struct {
	Schema     string
	Table      string
	Where      string
	ClientSide bool
}

type GridSortApplyMsg struct {
	Schema   string
	Table    string
	OrderBy  string
	OrderDir string
	Where    string
}

type GridDeleteRowMsg struct {
	Schema  string
	Table   string
	Row     []interface{}
	Columns []string
}

type GridBulkDeleteMsg struct {
	Schema  string
	Table   string
	Rows    [][]interface{}
	Columns []string
}

type GridCursorMovedMsg struct{}

type GridNavigateFKMsg struct {
	Schema    string
	Table     string
	FKColumn  string
	FKValue   interface{}
	RefSchema string
	RefTable  string
	RefColumn string
}

type GridGoBackMsg struct{}

type GridRefreshConfirmMsg struct {
	Schema   string
	Table    string
	OrderBy  string
	OrderDir string
	Where    string
}

type GridTabChangeMsg struct {
	Tab int
}

type Grid struct {
	styles      *theme.Styles
	header      *Header
	cells       *CellRenderer
	pager       *Pager
	mouse       *MouseHandler
	exportPicker *ExportPicker
	data        *postgres.QueryResult
	columns     []string
	widths      []int
	cursorRow   int
	cursorCol   int
	scrollCol   int
	width       int
	height      int
	focused     bool
	tableName   string
	schema      string
	editing     bool
	editRow     int
	editCol     int
	editValue   string
	editStartValue string
	editCursor  int
	editOrigWidth int
	displayDraftCount int
	filtering   bool
	filter      string
	whereFilter  *WhereFilter
	whereClause  string
	pendingDigits string
	keybinds     map[string]string
	constraintsData []postgres.ConstraintInfo
	foreignKeysData []postgres.ForeignKeyInfo
	indexesData     []postgres.IndexInfo
	keyIcons        map[int]KeyIcon
	selectedRows    map[int]bool
	pendingRows     [][]interface{}
	inserting       bool
	pendingRow      int
	pendingCol      int
	pendingUpdates  []PendingUpdate
	pendingDeletes  []PendingDelete
	discardPending  bool
	commitPending   bool
	refreshPending  bool
	activeTab       int
	yankMaxRows     int
}

func New(styles *theme.Styles, pageSize int, keybinds map[string]string) *Grid {
	g := &Grid{
		styles:      styles,
		header:      NewHeader(styles),
		cells:       NewCellRenderer(styles),
		pager:       NewPager(styles, pageSize),
		exportPicker: NewExportPicker(styles),
		keybinds:    keybinds,
		widths:      make([]int, 0),
		selectedRows: make(map[int]bool),
		yankMaxRows: 10,
	}
	g.mouse = NewMouseHandler(g)
	return g
}

func (g *Grid) SetData(result *postgres.QueryResult, schema, table string) {
	isNewTable := g.tableName != table
	g.data = result
	g.tableName = table
	g.schema = schema
	g.cursorRow = 0
	g.cursorCol = 0
	g.scrollCol = 0
	g.editing = false
	g.editValue = ""
	g.editCursor = 0
	g.whereFilter = nil
	if isNewTable {
		g.whereClause = ""
	}
	g.pendingRows = nil
	g.inserting = false
	g.pendingRow = 0
	g.pendingCol = 0
	g.pendingUpdates = nil
	g.pendingDeletes = nil
	g.discardPending = false
	g.commitPending = false
	g.pager.SetPendingCount(0)
	if isNewTable {
		g.header.ClearSort()
		g.constraintsData = nil
		g.foreignKeysData = nil
		g.indexesData = nil
	}

	if result == nil || len(result.Columns) == 0 {
		g.columns = nil
		return
	}

	g.columns = make([]string, len(result.Columns))
	for i, col := range result.Columns {
		g.columns[i] = col.Name
	}

	g.header.SetColumns(g.columns)
	g.calculateWidths()
	g.pager.SetTotalRows(len(result.Rows))

	// DEBUG
	f, _ := os.Create("/tmp/dbx_grid_debug.log")
	defer f.Close()
	fmt.Fprintf(f, "SetData: schema=%q table=%q\n", schema, table)
	fmt.Fprintf(f, "  Columns count: %d\n", len(result.Columns))
	for i, col := range result.Columns {
		fmt.Fprintf(f, "  Column[%d]: %s\n", i, col.Name)
	}
	fmt.Fprintf(f, "  Rows count: %d\n", len(result.Rows))
	if len(result.Rows) > 0 {
		fmt.Fprintf(f, "  First row values count: %d\n", len(result.Rows[0]))
		for i, v := range result.Rows[0] {
			fmt.Fprintf(f, "  Row[0][%d]: %v (type: %T)\n", i, v, v)
		}
	}
	fmt.Fprintf(f, "  g.widths count: %d\n", len(g.widths))
	fmt.Fprintf(f, "  g.width: %d\n", g.width)
}

func (g *Grid) SetMetadata(constraints []postgres.ConstraintInfo, foreignKeys []postgres.ForeignKeyInfo, indexes []postgres.IndexInfo) {
	g.constraintsData = constraints
	g.foreignKeysData = foreignKeys
	g.indexesData = indexes

	g.updateKeyIcons()
}

func (g *Grid) updateKeyIcons() {
	icons := make(map[int]KeyIcon)
	colIndex := make(map[string]int, len(g.columns))
	for i, c := range g.columns {
		colIndex[c] = i
	}

	// Mark PK columns from constraints
	for _, c := range g.constraintsData {
		if c.Type != "PRIMARY KEY" {
			continue
		}
		for _, name := range splitColumns(c.Columns) {
			if idx, ok := colIndex[name]; ok {
				icons[idx] = KeyPK
			}
		}
	}

	// Mark FK columns
	for _, fk := range g.foreignKeysData {
		if idx, ok := colIndex[fk.Column]; ok {
			icons[idx] = KeyFK
		}
	}

	g.keyIcons = icons
	g.header.SetKeyIcons(icons)
	g.calculateWidths()
}

func splitColumns(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func (g *Grid) HasData() bool {
	return g.data != nil && len(g.data.Columns) > 0
}

func (g *Grid) ActiveTab() int {
	return g.activeTab
}

func (g *Grid) Columns() []string {
	return g.columns
}

func (g *Grid) Data() *postgres.QueryResult {
	return g.data
}

func (g *Grid) calculateWidths() {
	if g.width <= 0 || len(g.columns) == 0 {
		return
	}

	g.widths = make([]int, len(g.columns))
	for i, col := range g.columns {
		w := len(col) + 2
		// Add space for key icon prefix (* or →)
		if _, isKey := g.keyIcons[i]; isKey {
			w += 2
		}
		if w < 6 {
			w = 6
		}
		g.widths[i] = w
	}

	if g.data != nil && len(g.data.Rows) > 0 {
		for _, row := range g.data.Rows {
			for i, val := range row {
				if i >= len(g.widths) {
					break
				}
				w := len(fmt.Sprintf("%v", val)) + 2
				if w > g.widths[i] {
					g.widths[i] = w
				}
			}
		}
	}

	// Cap individual column widths so multiple columns fit in viewport
	available := g.width - 2 // border takes 1 col per side
	if available > 0 && len(g.widths) > 0 {
		minCols := 5
		if len(g.widths) < minCols {
			minCols = len(g.widths)
		}
		maxColWidth := available / minCols
		if maxColWidth < 10 {
			maxColWidth = 10
		}
		for i := range g.widths {
			if g.widths[i] > maxColWidth {
				g.widths[i] = maxColWidth
			}
		}
	}

	g.syncScroll()
}

func (g *Grid) syncScroll() {
	available := g.width - 2 // border takes 1 col per side
	if available <= 0 || len(g.widths) == 0 {
		return
	}

	// Phase 1: Expand left — bring columns into view from the left side
	for g.scrollCol > 0 {
		prevWidth := g.widths[g.scrollCol-1]
		if available >= prevWidth {
			g.scrollCol--
			available += prevWidth
		} else {
			break
		}
	}

	// Phase 2: Ensure cursorCol is visible — scroll right if cursor is beyond visible area
	// Calculate cumulative width from scrollCol to cursorCol
	if g.cursorCol >= g.scrollCol {
		cumWidth := 0
		for i := g.scrollCol; i <= g.cursorCol && i < len(g.widths); i++ {
			cumWidth += g.widths[i]
		}
		// If cursor column doesn't fit, scroll right until it does
		for cumWidth > available && g.scrollCol < g.cursorCol {
			cumWidth -= g.widths[g.scrollCol]
			g.scrollCol++
		}
	}

	// Phase 3: If cursor is left of scroll window, scroll left
	if g.cursorCol < g.scrollCol {
		g.scrollCol = g.cursorCol
	}

}

func (g *Grid) visibleColumns() ([]string, []int) {
	if len(g.columns) == 0 || len(g.widths) == 0 {
		return nil, nil
	}

	available := g.width - 2 // border takes 1 col per side
	var visCols []string
	var visWidths []int
	total := 0

	for i := g.scrollCol; i < len(g.columns); i++ {
		w := g.widths[i]
		if total+w > available {
			break
		}
		visCols = append(visCols, g.columns[i])
		visWidths = append(visWidths, w)
		total += w
	}

	return visCols, visWidths
}

func (g *Grid) SetWidth(w int) {
	if w == g.width {
		return
	}
	g.width = w
	g.calculateWidths()
}

func (g *Grid) SetHeight(h int) {
	if h == g.height {
		return
	}
	g.height = h
}

func (g *Grid) Focus() {
	g.focused = true
}

func (g *Grid) Blur() {
	g.focused = false
}

func (g *Grid) SelectedRow() []interface{} {
	if g.activeTab != 0 {
		return nil
	}
	if g.data == nil || len(g.data.Rows) == 0 {
		return nil
	}
	if g.cursorRow >= 0 && g.cursorRow < len(g.data.Rows) {
		return g.data.Rows[g.cursorRow]
	}
	return nil
}

func (g *Grid) TableName() string {
	return g.tableName
}

func (g *Grid) CursorRow() int {
	return g.cursorRow
}

func (g *Grid) CursorCol() int {
	return g.cursorCol
}

func (g *Grid) ScrollCol() int {
	return g.scrollCol
}

func (g *Grid) ForeignKeys() []postgres.ForeignKeyInfo {
	return g.foreignKeysData
}

func (g *Grid) GetResult() *postgres.QueryResult {
	return g.data
}

func (g *Grid) HandleClick(x, y int) bool {
	return g.mouse.HandleClick(x, y)
}

func (g *Grid) HandleHeaderClick(x int) tea.Cmd {
	return g.mouse.HandleHeaderClick(x)
}

func (g *Grid) HandleScrollUp() bool {
	return g.mouse.HandleScrollUp()
}

func (g *Grid) HandleScrollDown() bool {
	return g.mouse.HandleScrollDown()
}

func (g *Grid) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !g.focused {
		return nil, false
	}

	// Handle export picker if visible
	if g.exportPicker.IsVisible() {
		if cmd, handled := g.exportPicker.Update(msg); handled {
			return cmd, true
		}
		return nil, false
	}

	if g.data == nil {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return g.handleKey(msg)
	}

	return nil, false
}

func (g *Grid) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	if g.editing {
		return g.handleEditKey(msg)
	}

	if g.filtering {
		return g.handleFilterKey(msg)
	}

	if g.whereFilter != nil && g.whereFilter.Visible() {
		handled, _ := g.whereFilter.HandleKey(msg)
		if !handled {
			return nil, false
		}
		if g.whereFilter.ShouldApply() {
			where := g.whereFilter.Input()
			g.whereClause = where
			g.whereFilter = nil
			clientSide := len(g.data.Rows) <= MaxRows
			return func() tea.Msg {
				return GridFilterApplyMsg{
					Schema:     g.schema,
					Table:      g.tableName,
					Where:      where,
					ClientSide: clientSide,
				}
			}, true
		}
		if !g.whereFilter.Visible() {
			return nil, true
		}
		return nil, true
	}

	if key == "esc" && len(g.selectedRows) > 0 {
		g.ClearSelection()
		return func() tea.Msg { return GridCursorMovedMsg{} }, true
	}

	// Keys that work regardless of whether there are rows
	switch key {
	case "i":
		return g.startInsertRow()
	case g.keybinds["grid.go_back"]:
		return func() tea.Msg { return GridGoBackMsg{} }, true
	case "s":
		return g.toggleSort(), true
	case "/":
		g.startWhereFilter()
		return nil, true
	case "f":
		g.startColumnFind()
		return nil, true
	}

	// Cancel discard pending on any key except D
	if key != "D" {
		g.discardPending = false
	}

	// Cancel commit pending on any key except ctrl+s
	if key != g.keybinds["grid.commit_pending"] {
		g.commitPending = false
	}

	// Cancel refresh pending on any key except r
	if key != g.keybinds["grid.refresh"] {
		g.refreshPending = false
	}

	// Keys that require existing rows
	hasRows := len(g.data.Rows) > 0
	if !hasRows {
		return nil, false
	}

	// Goto page F1-F9 — must be before 0-9 digit handler
	for i := 1; i <= 9; i++ {
		action := fmt.Sprintf("grid.goto_page_%d", i)
		if key == g.keybinds[action] {
			g.pager.GoToPage(i)
			g.clampCursor()
			return nil, true
		}
	}

	if key >= "0" && key <= "9" {
		g.pendingDigits += key
		n := 0
		for _, ch := range g.pendingDigits {
			n = n*10 + int(ch-'0')
		}
		g.pager.GoToPage(n)
		g.clampCursor()
		return nil, true
	}
	g.pendingDigits = ""

	switch key {
	case "j", "down":
		g.moveDown()
		return g.cursorMovedCmd(), true
	case "k", "up":
		g.moveUp()
		return g.cursorMovedCmd(), true
	case "h", "left":
		g.moveLeft()
		return g.cursorMovedCmd(), true
	case "l", "right":
		g.moveRight()
		return g.cursorMovedCmd(), true
	case "g":
		g.moveToFirst()
		return g.cursorMovedCmd(), true
	case "G":
		g.moveToLast()
		return g.cursorMovedCmd(), true
	case "ctrl+u":
		g.halfPageUp()
		return g.cursorMovedCmd(), true
	case "ctrl+d":
		g.halfPageDown()
		return g.cursorMovedCmd(), true
	case "N":
		g.pager.LastPage()
		g.clampCursor()
		return g.cursorMovedCmd(), true
	case "P":
		g.pager.FirstPage()
		g.clampCursor()
		return g.cursorMovedCmd(), true
	case "n", "]", "ctrl+right":
		g.pager.NextPage()
		g.clampCursor()
		return g.cursorMovedCmd(), true
	case "p", "[", "ctrl+left":
		g.pager.PrevPage()
		g.clampCursor()
		return g.cursorMovedCmd(), true
	case "enter":
		if g.cursorRowType() == "insert" {
			idx := g.pendingInsertIndex()
			if idx >= 0 {
				g.restoreEditWidth()
				g.inserting = true
				g.pendingRow = idx
				g.pendingCol = g.cursorCol
				g.editing = true
				g.editRow = len(g.data.Rows) + idx
				g.editCol = g.cursorCol
				val := g.pendingRows[idx][g.cursorCol]
				if val == nil {
					g.editValue = ""
				} else {
					g.editValue = fmt.Sprintf("%v", val)
				}
				g.editStartValue = g.editValue
				g.editCursor = len(g.editValue)
				g.expandEditCol()
				return nil, true
			}
		}
		if g.inserting {
			g.restoreEditWidth()
			g.editing = true
			g.editCol = g.pendingCol
			val := g.pendingRows[g.pendingRow][g.pendingCol]
			if val == nil {
				g.editValue = ""
			} else {
				g.editValue = fmt.Sprintf("%v", val)
			}
			g.editCursor = len(g.editValue)
			g.expandEditCol()
			return nil, true
		}
		g.startEdit()
		return nil, true
	case g.keybinds["grid.navigate_fk"]:
		return g.navigateFK()
	}

	// Handle yank via keybinding
	if key == g.keybinds["grid.yank"] {
		return g.startYank()
	}

	// Handle export via keybinding
	if key == g.keybinds["grid.export"] {
		return g.startExport()
	}

	// Handle delete via keybinding
	if key == g.keybinds["grid.delete_row"] {
		return g.startDelete()
	}

	// Handle commit pending drafts
	if key == g.keybinds["grid.commit_pending"] {
		return g.handleCommitPendingKey()
	}

	// Handle select_row keybinding
	if key == g.keybinds["grid.select_row"] {
		g.toggleRowSelection()
		return func() tea.Msg { return GridCursorMovedMsg{} }, true
	}

	// Handle discard drafts keybinding
	if key == "D" {
		return g.handleDiscardKey()
	}

	// Handle undo row drafts keybinding
	if key == g.keybinds["grid.undo"] {
		count := g.UndoRowDrafts()
		if count > 0 {
			return func() tea.Msg { return GridUndoRowMsg{Count: count} }, true
		}
		return nil, true
	}

	// Handle refresh keybinding
	if key == g.keybinds["grid.refresh"] {
		return g.handleRefreshKey()
	}

	return nil, false
}

func (g *Grid) startWhereFilter() {
	if g.data == nil || len(g.data.Columns) == 0 {
		return
	}
	g.whereFilter = NewWhereFilter(g.styles, g.data.Columns, g.width)
	g.whereFilter.Show()
	if g.whereClause != "" {
		g.whereFilter.SetInput(g.whereClause)
	}
}

func (g *Grid) startColumnFind() {
	g.filtering = true
	g.filter = ""
}

func (g *Grid) handleFilterKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "esc":
		g.filtering = false
		g.filter = ""
		return nil, true
	case "enter":
		g.jumpToBestMatch()
		g.filtering = false
		g.filter = ""
		return nil, true
	case "backspace":
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
		return nil, true
	default:
		if len(key) == 1 {
			g.filter += key
		}
		return nil, true
	}
}

func (g *Grid) jumpToBestMatch() {
	if g.filter == "" || len(g.columns) == 0 {
		return
	}
	query := strings.ToLower(g.filter)
	for i, col := range g.columns {
		if strings.Contains(strings.ToLower(col), query) {
			g.jumpToColumn(i)
			return
		}
	}
}

func (g *Grid) jumpToColumn(colIndex int) {
	scrollStart := colIndex - 2
	if scrollStart < 0 {
		scrollStart = 0
	}
	g.scrollCol = scrollStart
	g.cursorCol = colIndex
}

func (g *Grid) IsFiltering() bool {
	return g.filtering
}

func (g *Grid) IsExporting() bool {
	return g.exportPicker.IsVisible()
}

func (g *Grid) ExportPickerView() string {
	return g.exportPicker.View()
}

func (g *Grid) IsWhereFiltering() bool {
	return g.whereFilter != nil && g.whereFilter.Visible()
}

func (g *Grid) FilterText() string {
	return g.filter
}

func (g *Grid) WhereClause() string {
	return g.whereClause
}

func (g *Grid) SortColumn() string {
	return g.header.SortColumn()
}

func (g *Grid) SortDirection() string {
	return g.header.SortDirection()
}

func (g *Grid) TotalRows() int {
	if g.data == nil {
		return 0
	}
	return g.data.Count
}

func (g *Grid) startEdit() {
	if g.data == nil || len(g.data.Rows) == 0 {
		return
	}
	if g.cursorRow < 0 || g.cursorRow >= len(g.data.Rows) {
		return
	}
	if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
		return
	}

	g.displayDraftCount = g.DraftCount()
	g.editing = true
	g.editRow = g.cursorRow
	g.editCol = g.cursorCol
	g.editValue = fmt.Sprintf("%v", g.data.Rows[g.editRow][g.editCol])
	g.editStartValue = g.editValue
	g.editCursor = len(g.editValue)
	g.expandEditCol()
}

func (g *Grid) startInsertRow() (tea.Cmd, bool) {
	if g.data == nil || len(g.columns) == 0 {
		return nil, false
	}

	g.displayDraftCount = g.DraftCount()
	newRow := make([]interface{}, len(g.columns))
	g.pendingRows = append(g.pendingRows, newRow)
	g.pager.SetPendingCount(len(g.pendingRows))

	g.inserting = true
	g.pendingRow = len(g.pendingRows) - 1
	g.pendingCol = 0

	g.editing = true
	g.editRow = len(g.data.Rows) + g.pendingRow
	g.editCol = 0
	g.editValue = ""
	g.editStartValue = ""
	g.editCursor = 0

	// DEBUG
	f, _ := os.OpenFile("/tmp/dbx_mode_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "startInsertRow: inserting=true pendingRows=%d editRow=%d\n", len(g.pendingRows), g.editRow)
		f.Close()
	}

	return func() tea.Msg { return nil }, true
}

func (g *Grid) navigateFK() (tea.Cmd, bool) {
	if g.data == nil || len(g.data.Rows) == 0 {
		return nil, false
	}
	if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
		return nil, false
	}
	icon, ok := g.keyIcons[g.cursorCol]
	if !ok || icon != KeyFK {
		return nil, false
	}

	fkValue := g.data.Rows[g.cursorRow+g.pager.Offset()][g.cursorCol]
	if fkValue == nil {
		return nil, false
	}

	var fkInfo *postgres.ForeignKeyInfo
	for i := range g.foreignKeysData {
		if g.foreignKeysData[i].Column == g.columns[g.cursorCol] {
			fkInfo = &g.foreignKeysData[i]
			break
		}
	}
	if fkInfo == nil {
		return nil, false
	}

	refSchema := fkInfo.RefSchema
	if refSchema == "" {
		refSchema = g.schema
	}

	return func() tea.Msg {
		return GridNavigateFKMsg{
			Schema:    g.schema,
			Table:     g.tableName,
			FKColumn:  g.columns[g.cursorCol],
			FKValue:   fkValue,
			RefSchema: refSchema,
			RefTable:  fkInfo.RefTable,
			RefColumn: fkInfo.RefColumn,
		}
	}, true
}

func (g *Grid) startYank() (tea.Cmd, bool) {
	if g.data == nil || len(g.columns) == 0 {
		return nil, false
	}

	count := len(g.selectedRows)
	fileMode := count >= g.yankMaxRows

	var row []interface{}
	var rows [][]interface{}

	if count > 1 {
		// Multiple selected rows
		for i := range g.selectedRows {
			if i >= 0 && i < len(g.data.Rows) {
				rows = append(rows, g.data.Rows[i])
			}
		}
	} else if count == 1 {
		// Single selected row
		for i := range g.selectedRows {
			if i >= 0 && i < len(g.data.Rows) {
				row = g.data.Rows[i]
			}
		}
	} else {
		// Cursor row
		if g.cursorRow >= 0 && g.cursorRow < len(g.data.Rows) {
			row = g.data.Rows[g.cursorRow]
		}
	}

	if fileMode {
		g.exportPicker.ShowFileMode(g.schema, g.tableName, row, rows, g.columns)
	} else {
		g.exportPicker.ShowYankMode(g.schema, g.tableName, row, rows, g.columns)
	}
	return nil, true
}

func (g *Grid) StartExport() (tea.Cmd, bool) {
	return g.startExport()
}

func (g *Grid) startExport() (tea.Cmd, bool) {
	if g.data == nil || len(g.columns) == 0 {
		return nil, false
	}

	var row []interface{}
	var rows [][]interface{}

	if len(g.selectedRows) > 1 {
		// Export selected rows to file via picker
		for i := range g.selectedRows {
			if i >= 0 && i < len(g.data.Rows) {
				rows = append(rows, g.data.Rows[i])
			}
		}
		g.exportPicker.ShowFileMode(g.schema, g.tableName, nil, rows, g.columns)
	} else if len(g.selectedRows) == 1 {
		// Single selected row: export via picker (clipboard or file)
		for i := range g.selectedRows {
			if i >= 0 && i < len(g.data.Rows) {
				row = g.data.Rows[i]
			}
		}
		g.exportPicker.Show(g.schema, g.tableName, row, nil, g.columns)
	} else {
		// No selection: export ALL rows to file
		g.exportPicker.ShowFileMode(g.schema, g.tableName, nil, g.data.Rows, g.columns)
	}

	return nil, true
}

func (g *Grid) startDelete() (tea.Cmd, bool) {
	if g.data == nil || len(g.columns) == 0 {
		return nil, false
	}

	if len(g.selectedRows) > 0 {
		for i := range g.selectedRows {
			if i >= 0 && i < len(g.data.Rows) {
				rowCopy := make([]interface{}, len(g.data.Rows[i]))
				copy(rowCopy, g.data.Rows[i])
				g.pendingDeletes = append(g.pendingDeletes, PendingDelete{
					RowIdx: i,
					Row:    rowCopy,
				})
			}
		}
		g.selectedRows = make(map[int]bool)
		return nil, true
	}

	row := g.SelectedRow()
	if row == nil {
		return nil, false
	}
	rowCopy := make([]interface{}, len(row))
	copy(rowCopy, row)
	g.pendingDeletes = append(g.pendingDeletes, PendingDelete{
		RowIdx: g.cursorRow + g.pager.Offset(),
		Row:    rowCopy,
	})
	return nil, true
}

func (g *Grid) HasDrafts() bool {
	return len(g.pendingUpdates) > 0 || len(g.pendingDeletes) > 0 || len(g.pendingRows) > 0
}

func (g *Grid) DraftCount() int {
	return len(g.pendingUpdates) + len(g.pendingDeletes) + len(g.pendingRows)
}

func (g *Grid) HasPendingDeletes() bool {
	return len(g.pendingDeletes) > 0
}

func (g *Grid) IsCommitPending() bool {
	return g.commitPending
}

func (g *Grid) SetCommitPending(v bool) {
	g.commitPending = v
}

func (g *Grid) handleCommitPendingKey() (tea.Cmd, bool) {
	if !g.HasDrafts() {
		return nil, false
	}
	if g.HasPendingDeletes() && !g.commitPending {
		g.commitPending = true
		return nil, true
	}
	g.commitPending = false
	return g.CommitAllDrafts(), true
}

func (g *Grid) handleDiscardKey() (tea.Cmd, bool) {
	if !g.HasDrafts() {
		return nil, false
	}

	if g.discardPending {
		g.discardPending = false
		g.DiscardAllDrafts()
		return nil, true
	}

	g.discardPending = true
	return nil, true
}

func (g *Grid) DiscardAllDrafts() {
	for _, update := range g.pendingUpdates {
		if update.RowIdx >= 0 && update.RowIdx < len(g.data.Rows) {
			g.data.Rows[update.RowIdx][update.ColIdx] = update.OldValue
		}
	}

	g.pendingUpdates = nil
	g.pendingDeletes = nil
	g.pendingRows = nil
	g.inserting = false
	g.discardPending = false
	g.selectedRows = make(map[int]bool)
	g.pager.SetPendingCount(0)
}

func (g *Grid) IsDiscardPending() bool {
	return g.discardPending
}

// UndoRowDrafts reverts all draft changes on the currently selected row.
// For pending inserts, removes the insert row entirely.
// For real rows, reverts updates and deletes.
func (g *Grid) UndoRowDrafts() int {
	// Handle pending insert row
	if idx := g.pendingInsertIndex(); idx >= 0 {
		g.pendingRows = append(g.pendingRows[:idx], g.pendingRows[idx+1:]...)
		g.pager.SetPendingCount(len(g.pendingRows))
		g.displayDraftCount = g.DraftCount()
		g.clampCursor()
		return 1
	}

	// Handle real rows (updates + deletes)
	targetRowIdx := g.cursorRow + g.pager.Offset()

	count := 0

	// Restore cell values for updates on this row
	filtered := g.pendingUpdates[:0]
	for _, u := range g.pendingUpdates {
		if u.RowIdx == targetRowIdx {
			if u.RowIdx >= 0 && u.RowIdx < len(g.data.Rows) {
				g.data.Rows[u.RowIdx][u.ColIdx] = u.OldValue
			}
			count++
		} else {
			filtered = append(filtered, u)
		}
	}
	g.pendingUpdates = filtered

	// Remove delete for this row
	filteredDel := g.pendingDeletes[:0]
	for _, d := range g.pendingDeletes {
		if d.RowIdx == targetRowIdx {
			count++
		} else {
			filteredDel = append(filteredDel, d)
		}
	}
	g.pendingDeletes = filteredDel

	g.displayDraftCount = g.DraftCount()
	return count
}

func (g *Grid) handleRefreshKey() (tea.Cmd, bool) {
	if g.data == nil || len(g.columns) == 0 {
		return nil, false
	}

	if g.refreshPending {
		g.refreshPending = false
		return func() tea.Msg {
			return GridRefreshConfirmMsg{
				Schema:   g.schema,
				Table:    g.tableName,
				OrderBy:  g.SortColumn(),
				OrderDir: g.SortDirection(),
				Where:    g.whereClause,
			}
		}, true
	}

	if g.HasDrafts() {
		g.refreshPending = true
		return nil, true
	}

	return func() tea.Msg {
		return GridRefreshConfirmMsg{
			Schema:   g.schema,
			Table:    g.tableName,
			OrderBy:  g.SortColumn(),
			OrderDir: g.SortDirection(),
			Where:    g.whereClause,
		}
	}, true
}

func (g *Grid) IsRefreshPending() bool {
	return g.refreshPending
}

func (g *Grid) SetRefreshPending(v bool) {
	g.refreshPending = v
}

func (g *Grid) isRowDeleted(rowIdx int) bool {
	for _, d := range g.pendingDeletes {
		if d.RowIdx == rowIdx {
			return true
		}
	}
	return false
}

func (g *Grid) isCellModified(rowIdx, colIdx int) bool {
	for _, u := range g.pendingUpdates {
		if u.RowIdx == rowIdx && u.ColIdx == colIdx {
			return true
		}
	}
	return false
}

func (g *Grid) isRowPendingInsert(rowIdx int) bool {
	return rowIdx >= len(g.data.Rows)
}

func (g *Grid) PrimaryKeyColumns() []string {
	colIndex := make(map[string]int, len(g.columns))
	for i, c := range g.columns {
		colIndex[c] = i
	}

	var pkCols []string
	for _, c := range g.constraintsData {
		if c.Type != "PRIMARY KEY" {
			continue
		}
		for _, name := range splitColumns(c.Columns) {
			if _, ok := colIndex[name]; ok {
				pkCols = append(pkCols, name)
			}
		}
	}
	return pkCols
}

func BuildDeleteQuery(schema, table string, columns []string, row []interface{}, pkCols []string) (string, []interface{}) {
	var whereParts []string
	var args []interface{}
	argIdx := 1

	if len(pkCols) > 0 {
		pkIndex := make(map[string]int, len(columns))
		for i, c := range columns {
			pkIndex[c] = i
		}
		for _, pk := range pkCols {
			whereParts = append(whereParts, fmt.Sprintf("%q = $%d", pk, argIdx))
			args = append(args, row[pkIndex[pk]])
			argIdx++
		}
	} else {
		for i, val := range row {
			whereParts = append(whereParts, fmt.Sprintf("%q = $%d", columns[i], argIdx))
			args = append(args, val)
			argIdx++
		}
	}

	query := fmt.Sprintf("DELETE FROM %q.%q WHERE %s",
		schema, table, strings.Join(whereParts, " AND "))
	return query, args
}

func (g *Grid) toggleRowSelection() {
	if g.data == nil || len(g.data.Rows) == 0 {
		return
	}
	
	// Calculate the actual row index considering pagination
	actualRow := g.cursorRow + g.pager.Offset()
	
	if actualRow >= 0 && actualRow < len(g.data.Rows) {
		if g.selectedRows[actualRow] {
			delete(g.selectedRows, actualRow)
		} else {
			g.selectedRows[actualRow] = true
		}
	}
}

func (g *Grid) ClearSelection() {
	g.selectedRows = make(map[int]bool)
}

func (g *Grid) SelectedRows() [][]interface{} {
	if g.data == nil || len(g.selectedRows) == 0 {
		return nil
	}
	
	var rows [][]interface{}
	for i := range g.selectedRows {
		if i >= 0 && i < len(g.data.Rows) {
			rows = append(rows, g.data.Rows[i])
		}
	}
	return rows
}

func (g *Grid) SelectionCount() int {
	return len(g.selectedRows)
}

func (g *Grid) SetYankMaxRows(n int) {
	if n > 0 {
		g.yankMaxRows = n
	}
}

func (g *Grid) AllRows() [][]interface{} {
	if g.data == nil {
		return nil
	}
	return g.data.Rows
}

func (g *Grid) IsInserting() bool    { return g.inserting }
func (g *Grid) HasPendingRows() bool { return len(g.pendingRows) > 0 }
func (g *Grid) PendingCount() int    { return len(g.pendingRows) }
func (g *Grid) IsEditing() bool      { return g.editing }

func (g *Grid) handleEditKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	switch key {
	case "esc":
		g.restoreEditWidth()
		if g.editValue != g.editStartValue {
			g.commitEdit()
			g.editing = false
			g.editValue = ""
			g.inserting = false
			return nil, true
		}
		g.editing = false
		g.editValue = ""
		if g.inserting {
			g.inserting = false
			if g.pendingRow >= 0 && g.pendingRow < len(g.pendingRows) {
				// Remove only the current pending insert row
				g.pendingRows = append(g.pendingRows[:g.pendingRow], g.pendingRows[g.pendingRow+1:]...)
				g.pager.SetPendingCount(len(g.pendingRows))
			}
			if len(g.pendingRows) == 0 {
				g.pendingRows = nil
			}
			g.clampCursor()
		}
		return nil, true
	case "tab":
		g.restoreEditWidth()
		cmd := g.commitEdit()
		g.editValue = ""
		nextCol := (g.editCol + 1) % len(g.columns)
		g.editCol = nextCol
		if g.inserting {
			g.pendingCol = nextCol
			val := g.pendingRows[g.pendingRow][nextCol]
			if val == nil {
				g.editValue = ""
			} else {
				g.editValue = fmt.Sprintf("%v", val)
			}
		} else {
			g.editValue = fmt.Sprintf("%v", g.data.Rows[g.editRow][nextCol])
		}
		g.editCursor = len(g.editValue)
		g.expandEditCol()
		return cmd, true
	case "enter":
		g.restoreEditWidth()
		cmd := g.commitEdit()
		g.editValue = ""
		nextCol := (g.editCol + 1) % len(g.columns)
		g.editCol = nextCol
		if g.inserting {
			g.pendingCol = nextCol
			val := g.pendingRows[g.pendingRow][nextCol]
			if val == nil {
				g.editValue = ""
			} else {
				g.editValue = fmt.Sprintf("%v", val)
			}
		} else {
			g.editValue = fmt.Sprintf("%v", g.data.Rows[g.editRow][nextCol])
		}
		g.editCursor = len(g.editValue)
		g.expandEditCol()
		return cmd, true
	case "up":
		return nil, true
	case "down":
		return nil, true
	case "left":
		if g.editCursor > 0 {
			g.editCursor--
		}
		return nil, true
	case "right":
		if g.editCursor < len(g.editValue) {
			g.editCursor++
		}
		return nil, true
	case "home", "ctrl+a":
		g.editCursor = 0
		return nil, true
	case "end", "ctrl+e":
		g.editCursor = len(g.editValue)
		return nil, true
	case "backspace":
		if g.editCursor > 0 {
			g.editValue = g.editValue[:g.editCursor-1] + g.editValue[g.editCursor:]
			g.editCursor--
		}
		return nil, true
	case "delete":
		if g.editCursor < len(g.editValue) {
			g.editValue = g.editValue[:g.editCursor] + g.editValue[g.editCursor+1:]
		}
		return nil, true
	default:
		if msg.Text != "" && !isControlKey(msg) {
			g.editValue = g.editValue[:g.editCursor] + msg.Text + g.editValue[g.editCursor:]
			g.editCursor += len(msg.Text)
			if len(g.editValue)+4 > g.widths[g.editCol] {
				g.expandEditCol()
			}
			return nil, true
		}
	}

	return nil, false
}

func isControlKey(msg tea.KeyPressMsg) bool {
	return msg.Mod > 0
}

func (g *Grid) restoreEditWidth() {
	if g.editOrigWidth > 0 && g.editCol >= 0 && g.editCol < len(g.widths) {
		g.widths[g.editCol] = g.editOrigWidth
		g.editOrigWidth = 0
	}
}

func (g *Grid) expandEditCol() {
	if g.editCol < 0 || g.editCol >= len(g.widths) {
		return
	}
	g.editOrigWidth = g.widths[g.editCol]
	colNameWidth := len(g.columns[g.editCol]) + 4
	editValWidth := len(g.editValue) + 4
	expandedWidth := g.editOrigWidth
	if colNameWidth > expandedWidth {
		expandedWidth = colNameWidth
	}
	if editValWidth > expandedWidth {
		expandedWidth = editValWidth
	}
	if expandedWidth > 80 {
		expandedWidth = 80
	}
	g.widths[g.editCol] = expandedWidth
}

func (g *Grid) commitEdit() tea.Cmd {
	if g.inserting && g.pendingRow >= 0 && g.pendingRow < len(g.pendingRows) {
		var newVal interface{}
		if g.editValue == "" {
			newVal = nil
		} else {
			newVal = g.editValue
		}
		g.pendingRows[g.pendingRow][g.editCol] = newVal
		return nil
	}
	if g.editRow < 0 || g.editRow >= len(g.data.Rows) {
		return nil
	}
	row := g.data.Rows[g.editRow]

	newVal := g.parseEditValue(g.editValue, row[g.editCol])

	found := false
	for i, u := range g.pendingUpdates {
		if u.RowIdx == g.editRow && u.ColIdx == g.editCol {
			g.pendingUpdates[i].NewValue = newVal
			found = true
			break
		}
	}
	if !found {
		// Check if there's already an update for this row — reuse its OldRow
		var oldRow []interface{}
		for _, u := range g.pendingUpdates {
			if u.RowIdx == g.editRow && u.OldRow != nil {
				oldRow = u.OldRow
				break
			}
		}
		if oldRow == nil {
			oldRow = make([]interface{}, len(row))
			copy(oldRow, row)
		}
		g.pendingUpdates = append(g.pendingUpdates, PendingUpdate{
			RowIdx:   g.editRow,
			ColIdx:   g.editCol,
			OldValue: row[g.editCol],
			NewValue: newVal,
			OldRow:   oldRow,
		})
	}
	g.data.Rows[g.editRow][g.editCol] = newVal
	return nil
}

// DraftSQL generates SQL from pending drafts without clearing them.
// Uses real values instead of parameterized placeholders.
func (g *Grid) DraftSQL() string {
	if !g.HasDrafts() {
		return ""
	}

	var queries []string

	for _, row := range g.pendingRows {
		var cols []string
		var vals []string
		for i, val := range row {
			cols = append(cols, g.columns[i])
			vals = append(vals, formatSQLValue(val))
		}
		q := fmt.Sprintf("INSERT INTO %q.%q (%s) VALUES (%s)",
			g.schema, g.tableName,
			strings.Join(cols, ", "),
			strings.Join(vals, ", "))
		queries = append(queries, q)
	}

	for _, update := range g.pendingUpdates {
		if update.RowIdx < 0 || update.RowIdx >= len(g.data.Rows) {
			continue
		}
		colName := g.columns[update.ColIdx]
		whereRow := update.OldRow
		if whereRow == nil {
			whereRow = g.data.Rows[update.RowIdx]
		}
		var whereParts []string
		for i, val := range whereRow {
			whereParts = append(whereParts, fmt.Sprintf("%q = %s", g.columns[i], formatSQLValue(val)))
		}
		q := fmt.Sprintf("UPDATE %q.%q SET %q = %s WHERE %s",
			g.schema, g.tableName, colName, formatSQLValue(update.NewValue), strings.Join(whereParts, " AND "))
		queries = append(queries, q)
	}

	pkCols := g.PrimaryKeyColumns()
	for _, del := range g.pendingDeletes {
		q, _ := BuildDeleteQuery(g.schema, g.tableName, g.columns, del.Row, pkCols)
		queries = append(queries, q)
	}

	return strings.Join(queries, ";\n")
}

// formatSQLValue formats a value for display in SQL.
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

func (g *Grid) CommitAllDrafts() tea.Cmd {
	var queries []string
	var allArgs [][]interface{}

	for _, row := range g.pendingRows {
		var cols []string
		var placeholders []string
		var args []interface{}
		argIdx := 1
		for i, val := range row {
			cols = append(cols, g.columns[i])
			if val == nil {
				placeholders = append(placeholders, "NULL")
			} else {
				placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
				args = append(args, val)
				argIdx++
			}
		}
		q := fmt.Sprintf("INSERT INTO %q.%q (%s) VALUES (%s)",
			g.schema, g.tableName,
			strings.Join(cols, ", "),
			strings.Join(placeholders, ", "))
		queries = append(queries, q)
		allArgs = append(allArgs, args)
	}

	for _, update := range g.pendingUpdates {
		if update.RowIdx < 0 || update.RowIdx >= len(g.data.Rows) {
			continue
		}
		whereRow := update.OldRow
		if whereRow == nil {
			whereRow = g.data.Rows[update.RowIdx]
		}
		colName := g.columns[update.ColIdx]
		var whereParts []string
		var args []interface{}
		argIdx := 1
		for i, val := range whereRow {
			whereParts = append(whereParts, fmt.Sprintf("%q = $%d", g.columns[i], argIdx))
			args = append(args, val)
			argIdx++
		}
		args = append(args, update.NewValue)
		q := fmt.Sprintf("UPDATE %q.%q SET %q = $%d WHERE %s",
			g.schema, g.tableName, colName, argIdx, strings.Join(whereParts, " AND "))
		queries = append(queries, q)
		allArgs = append(allArgs, args)
	}

	pkCols := g.PrimaryKeyColumns()
	for _, del := range g.pendingDeletes {
		q, args := BuildDeleteQuery(g.schema, g.tableName, g.columns, del.Row, pkCols)
		queries = append(queries, q)
		allArgs = append(allArgs, args)
	}

	if len(queries) == 0 {
		return nil
	}

	schema := g.schema
	table := g.tableName
	q := queries
	a := allArgs
	g.pendingRows = nil
	g.pendingUpdates = nil
	g.pendingDeletes = nil
	g.inserting = false
	g.discardPending = false
	g.pager.SetPendingCount(0)

	f, _ := os.OpenFile("/tmp/dbx_grid_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		for i, query := range q {
			fmt.Fprintf(f, "CommitAll[%d]: %s args=%v\n", i, query, a[i])
		}
		f.Close()
	}

	return func() tea.Msg {
		return GridCommitAllMsg{Schema: schema, Table: table, Queries: q, Args: a}
	}
}

func (g *Grid) parseEditValue(s string, original interface{}) interface{} {
	switch original.(type) {
	case int64:
		var v int64
		fmt.Sscanf(s, "%d", &v)
		return v
	case float64:
		var v float64
		fmt.Sscanf(s, "%g", &v)
		return v
	case bool:
		return s == "true" || s == "t" || s == "1"
	default:
		return s
	}
}

func (g *Grid) moveDown() {
	maxRow := g.visibleRows() - 1
	if g.cursorRow < maxRow {
		g.cursorRow++
	}
}

func (g *Grid) moveUp() {
	if g.cursorRow > 0 {
		g.cursorRow--
	}
}

func (g *Grid) moveRight() {
	if g.cursorCol < len(g.columns)-1 {
		g.cursorCol++
	} else {
		// Wrap to first column
		g.cursorCol = 0
		g.scrollCol = 0
	}
	g.syncScroll()
}

func (g *Grid) moveLeft() {
	if g.cursorCol > 0 {
		g.cursorCol--
	} else {
		// Wrap to last column
		g.cursorCol = len(g.columns) - 1
	}
	g.syncScroll()
}

func (g *Grid) moveToFirst() {
	g.cursorRow = 0
}

func (g *Grid) moveToLast() {
	g.cursorRow = g.visibleRows() - 1
	if g.cursorRow < 0 {
		g.cursorRow = 0
	}
}

func (g *Grid) halfPageUp() {
	half := g.visibleRows() / 2
	g.cursorRow -= half
	if g.cursorRow < 0 {
		g.cursorRow = 0
	}
}

func (g *Grid) halfPageDown() {
	half := g.visibleRows() / 2
	g.cursorRow += half
	maxRow := g.visibleRows() - 1
	if g.cursorRow > maxRow {
		g.cursorRow = maxRow
	}
}

func (g *Grid) setActiveTab(tab int) {
	g.cursorRow = 0
	g.cursorCol = 0
	g.scrollCol = 0
}

func (g *Grid) cursorMovedCmd() tea.Cmd {
	return func() tea.Msg { return GridCursorMovedMsg{} }
}

func (g *Grid) toggleSort() tea.Cmd {
	if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
		return nil
	}
	g.header.ToggleSort(g.cursorCol)

	sortCol := g.header.SortColumn()
	sortDir := g.header.SortDirection()

	return func() tea.Msg {
		return GridSortApplyMsg{
			Schema:   g.schema,
			Table:    g.tableName,
			OrderBy:  sortCol,
			OrderDir: sortDir,
			Where:    g.whereClause,
		}
	}
}

func (g *Grid) visibleRows() int {
	if g.data == nil {
		return 0
	}
	total := len(g.data.Rows)
	if total == 0 && len(g.pendingRows) == 0 {
		return 0
	}

	offset := g.pager.Offset()
	limit := g.pager.Limit()
	remaining := total - offset
	if remaining < 0 {
		remaining = 0
	}
	totalVisible := remaining + len(g.pendingRows)
	if totalVisible < limit {
		return totalVisible
	}
	return limit
}

func (g *Grid) clampCursor() {
	max := g.visibleRows() - 1
	if max < 0 {
		max = 0
	}
	if g.cursorRow > max {
		g.cursorRow = max
	}
}

func (g *Grid) cursorRowType() string {
	offset := g.pager.Offset()
	remaining := len(g.data.Rows) - offset
	if remaining < 0 {
		remaining = 0
	}
	if g.cursorRow >= remaining {
		return "insert"
	}
	return "real"
}

func (g *Grid) pendingInsertIndex() int {
	offset := g.pager.Offset()
	remaining := len(g.data.Rows) - offset
	if remaining < 0 {
		remaining = 0
	}
	if g.cursorRow >= remaining {
		idx := g.cursorRow - remaining
		if idx >= 0 && idx < len(g.pendingRows) {
			return idx
		}
	}
	return -1
}

func (g *Grid) View() string {
	if g.data == nil || len(g.columns) == 0 {
		return g.styles.Text.Render("  No data loaded")
	}

	output := g.renderRecordsView()

	// Choose border style and color based on focus
	border := lipgloss.ThickBorder()
	var borderFg color.Color
	if g.focused {
		borderFg = g.styles.BorderActive.GetBorderTopForeground()
	} else {
		borderFg = g.styles.Border.GetBorderTopForeground()
	}

	// Pagination footer — right-aligned in bottom border
	footer := g.pager.RenderFooter()

	return bordered.RenderWithTitleAndFooterEx(border, borderFg, bordered.AlignLeft, " Grid ", footer, output, g.width)
}

func (g *Grid) renderRecordsView() string {
	// Calculate dynamic overhead: header(1) + mode(1) + buffer(2) = 4 base
	// Plus 2 for border (top + bottom lines)
	overhead := 6
	if g.commitPending || g.refreshPending || g.discardPending || g.HasDrafts() {
		overhead++ // prefix message line
	}
	if g.whereFilter != nil && g.whereFilter.Visible() {
		overhead++ // whereFilter input line
	} else if g.whereClause != "" {
		overhead++ // active WHERE indicator line
	} else if g.filtering {
		overhead++ // column filter line
	}
	contentHeight := g.height - overhead
	if contentHeight < 1 {
		contentHeight = 1
	}

	visCols, visWidths := g.visibleColumns()

	g.header.SetColumns(visCols)
	g.header.SetWidths(visWidths)
	header := g.header.Render()

	var rows []string
	offset := g.pager.Offset()
	limit := g.pager.Limit()
	endRow := offset + limit
	if endRow > len(g.data.Rows) {
		endRow = len(g.data.Rows)
	}

	visibleCount := 0
	for i := offset; i < endRow && visibleCount < contentHeight; i++ {
		row := g.data.Rows[i]
		visValues := make([]interface{}, len(visCols))
		for j := 0; j < len(visCols); j++ {
			colIdx := g.scrollCol + j
			if colIdx < len(row) {
				visValues[j] = row[colIdx]
			}
		}
		isSelected := visibleCount == g.cursorRow
		isMultiSelected := g.selectedRows[i]
		isEditing := g.editing && visibleCount == (g.editRow-offset) && g.editRow >= offset && g.editRow < endRow
		isDraftDelete := g.isRowDeleted(i)
		draftCols := make(map[int]bool)
		if !isDraftDelete {
			for j := range visCols {
				actualColIdx := g.scrollCol + j
				draftCols[j] = g.isCellModified(i, actualColIdx)
			}
		}
		var rendered string
		if isDraftDelete {
			activeCol := -1
			if isSelected {
				activeCol = g.cursorCol - g.scrollCol
			}
			rendered = g.cells.RenderDraftDeleteRow(visValues, visWidths, activeCol)
		} else if isEditing {
			editColLocal := g.editCol - g.scrollCol
			if editColLocal >= 0 && editColLocal < len(visWidths) {
				rendered = g.cells.RenderEditRow(visValues, visWidths, editColLocal, g.editValue, g.editCursor)
			} else {
				activeCol := g.cursorCol - g.scrollCol
				rendered = g.cells.RenderRow(visValues, visWidths, activeCol)
			}
		} else if isSelected && isMultiSelected {
			activeCol := g.cursorCol - g.scrollCol
			rendered = g.cells.RenderCursorSelectedRow(visValues, visWidths, activeCol)
		} else if isSelected {
			activeCol := g.cursorCol - g.scrollCol
			hasDrafts := len(g.pendingUpdates) > 0
			if hasDrafts {
				rendered = g.cells.RenderDraftUpdateRow(visValues, visWidths, draftCols, activeCol)
			} else {
				rendered = g.cells.RenderRow(visValues, visWidths, activeCol)
			}
		} else if isMultiSelected {
			rendered = g.cells.RenderSelectedRow(visValues, visWidths, -1)
		} else {
			hasDrafts := len(g.pendingUpdates) > 0
			if hasDrafts {
				rendered = g.cells.RenderDraftUpdateRow(visValues, visWidths, draftCols, -1)
			} else {
				rendered = g.cells.RenderRow(visValues, visWidths, -1)
			}
		}
		rows = append(rows, rendered)
		visibleCount++
	}

	for pi := 0; pi < len(g.pendingRows) && visibleCount < contentHeight; pi++ {
		row := g.pendingRows[pi]
		visValues := make([]interface{}, len(visCols))
		for j := 0; j < len(visCols); j++ {
			colIdx := g.scrollCol + j
			if colIdx < len(row) {
				visValues[j] = row[colIdx]
			}
		}
		isPendingEditing := g.inserting && pi == g.pendingRow && g.editing
		isPendingSelected := g.cursorRowType() == "insert" && g.pendingInsertIndex() == pi
		var rendered string
		if isPendingEditing {
			editColLocal := g.pendingCol - g.scrollCol
			if editColLocal >= 0 && editColLocal < len(visWidths) {
				rendered = g.cells.RenderDraftInsertEditRow(visValues, visWidths, editColLocal, g.editValue, g.editCursor)
			} else {
				rendered = g.cells.RenderDraftInsertRow(visValues, visWidths)
			}
		} else if isPendingSelected {
			rendered = g.cells.RenderDraftInsertSelectedRow(visValues, visWidths)
		} else {
			rendered = g.cells.RenderDraftInsertRow(visValues, visWidths)
		}
		rows = append(rows, rendered)
		visibleCount++
	}

	if visibleCount == 0 && contentHeight > 0 {
		emptyMsg := g.styles.Help.Render("  Empty table — press i to insert a row")
		rows = append(rows, emptyMsg)
		visibleCount++
	}

	for i := visibleCount; i < contentHeight; i++ {
		rows = append(rows, "")
	}

	var modeIndicator string
	if g.editing {
		modeIndicator = g.styles.ModeEdit.Render(" EDIT ")
	} else {
		modeIndicator = g.styles.ModeNormal.Render(" NORMAL ")
	}

	// DEBUG
	f, _ := os.OpenFile("/tmp/dbx_mode_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "renderRecordsView: inserting=%v pendingRows=%d editing=%v\n", g.inserting, len(g.pendingRows), g.editing)
		f.Close()
	}

	var prefix string
	if g.commitPending {
		prefix = g.styles.Help.Render(fmt.Sprintf("  Press Ctrl+S again to confirm %d change(s) including deletes", g.DraftCount())) + "\n"
	} else if g.refreshPending {
		prefix = g.styles.Help.Render("  Press r again to refresh (unsaved changes will be lost)") + "\n"
	} else if g.discardPending {
		prefix = g.styles.Help.Render(fmt.Sprintf("  Press D again to discard %d change(s)", g.DraftCount())) + "\n"
	} else if g.HasDrafts() {
		count := g.DraftCount()
		if g.editing {
			count = g.displayDraftCount
		}
		prefix = g.styles.Help.Render(fmt.Sprintf("  %d pending change(s) — Ctrl+S to save, D to discard", count)) + "\n"
	}

	var result string
	if g.whereFilter != nil && g.whereFilter.Visible() {
		filterBar := g.whereFilter.RenderInput()
		popup := g.whereFilter.RenderPopup()
		result = prefix + filterBar + "\n"
		if popup != "" {
			result += popup + "\n"
		}
		result += header + "\n" + strings.Join(rows, "\n") + "\n" + modeIndicator
	} else if g.whereClause != "" {
		activeFilter := g.styles.FilterIndicator.
			Width(g.width - 2).
			Render("  WHERE " + g.whereClause)
		result = prefix + activeFilter + "\n" + header + "\n" + strings.Join(rows, "\n") + "\n" + modeIndicator
	} else if g.filtering {
		filterLine := g.styles.Text.Render("Column: " + g.filter + "_")
		result = prefix + filterLine + "\n" + header + "\n" + strings.Join(rows, "\n") + "\n" + modeIndicator
	} else {
		result = prefix + header + "\n" + strings.Join(rows, "\n") + "\n" + modeIndicator
	}
	return result
}
