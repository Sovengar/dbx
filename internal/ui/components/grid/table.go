package grid

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

type Grid struct {
	styles     *theme.Styles
	header     *Header
	cells      *CellRenderer
	pager      *Pager
	data       *postgres.QueryResult
	columns    []string
	widths     []int
	cursorRow  int
	cursorCol  int
	scrollCol  int
	width      int
	height     int
	focused    bool
	tableName  string
	schema     string
}

func New(styles *theme.Styles, pageSize int) *Grid {
	return &Grid{
		styles: styles,
		header: NewHeader(styles),
		cells:  NewCellRenderer(styles),
		pager:  NewPager(styles, pageSize),
		widths: make([]int, 0),
	}
}

func (g *Grid) SetData(result *postgres.QueryResult, schema, table string) {
	g.data = result
	g.tableName = table
	g.schema = schema
	g.cursorRow = 0
	g.cursorCol = 0
	g.scrollCol = 0

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

func (g *Grid) calculateWidths() {
	if g.width <= 0 || len(g.columns) == 0 {
		return
	}

	g.widths = make([]int, len(g.columns))
	for i, col := range g.columns {
		w := len(col) + 2
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

	g.syncScroll()
}

func (g *Grid) syncScroll() {
	available := g.width - 4
	if available <= 0 || len(g.widths) == 0 {
		return
	}

	for g.scrollCol > 0 {
		prevWidth := g.widths[g.scrollCol-1]
		if available+prevWidth > 0 {
			g.scrollCol--
			available += prevWidth
		} else {
			break
		}
	}

	total := 0
	for i := g.scrollCol; i < len(g.widths); i++ {
		if total+g.widths[i] > available {
			break
		}
		total += g.widths[i]
	}

	if g.cursorCol < g.scrollCol {
		g.scrollCol = g.cursorCol
	}

	cumWidth := 0
	for i := g.scrollCol; i <= g.cursorCol && i < len(g.widths); i++ {
		cumWidth += g.widths[i]
	}
	for cumWidth > available && g.scrollCol < g.cursorCol {
		cumWidth -= g.widths[g.scrollCol]
		g.scrollCol++
	}
}

func (g *Grid) visibleColumns() ([]string, []int) {
	if len(g.columns) == 0 || len(g.widths) == 0 {
		return nil, nil
	}

	available := g.width - 4
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
	g.width = w
	g.calculateWidths()
}

func (g *Grid) SetHeight(h int) {
	g.height = h
}

func (g *Grid) Focus() {
	g.focused = true
}

func (g *Grid) Blur() {
	g.focused = false
}

func (g *Grid) SelectedRow() []interface{} {
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

func (g *Grid) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !g.focused {
		return nil, false
	}

	if g.data == nil || len(g.data.Rows) == 0 {
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

	switch key {
	case "j", "down":
		g.moveDown()
		return nil, true
	case "k", "up":
		g.moveUp()
		return nil, true
	case "h", "left":
		g.moveLeft()
		return nil, true
	case "l", "right":
		g.moveRight()
		return nil, true
	case "g":
		g.moveToFirst()
		return nil, true
	case "G":
		g.moveToLast()
		return nil, true
	case "ctrl+u":
		g.halfPageUp()
		return nil, true
	case "ctrl+d":
		g.halfPageDown()
		return nil, true
	case "n":
		g.pager.NextPage()
		g.clampCursor()
		return nil, true
	case "p":
		g.pager.PrevPage()
		g.clampCursor()
		return nil, true
	case "s":
		g.toggleSort()
		return nil, true
	}

	return nil, false
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
		g.syncScroll()
	}
}

func (g *Grid) moveLeft() {
	if g.cursorCol > 0 {
		g.cursorCol--
		g.syncScroll()
	}
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

func (g *Grid) toggleSort() {
	if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
		return
	}
	g.header.ToggleSort(g.cursorCol)
}

func (g *Grid) visibleRows() int {
	if g.data == nil {
		return 0
	}
	total := len(g.data.Rows)
	if total == 0 {
		return 0
	}

	offset := g.pager.Offset()
	limit := g.pager.Limit()
	remaining := total - offset
	if remaining < limit {
		return remaining
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

func (g *Grid) View() string {
	// DEBUG: log view state
	f, _ := os.OpenFile("/tmp/dbx_grid_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "\nView called: g.data=%v g.columns=%d g.widths=%d g.height=%d scrollCol=%d cursorCol=%d\n", g.data != nil, len(g.columns), len(g.widths), g.height, g.scrollCol, g.cursorCol)
		if g.data != nil {
			fmt.Fprintf(f, "  g.data.Rows=%d g.data.Columns=%d\n", len(g.data.Rows), len(g.data.Columns))
		}
		f.Close()
	}

	if g.data == nil || len(g.columns) == 0 {
		return g.styles.Text.Render("  No data loaded")
	}

	title := g.styles.Header.Render(g.tableName)
	contentHeight := g.height - 6

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
		selected := visibleCount == g.cursorRow
		rendered := g.cells.RenderRow(visValues, visWidths, selected)
		rows = append(rows, rendered)
		visibleCount++
	}

	pager := g.pager.Render(g.width)

	result := title + "\n" + header + "\n" + strings.Join(rows, "\n") + "\n" + pager
	return result
}
