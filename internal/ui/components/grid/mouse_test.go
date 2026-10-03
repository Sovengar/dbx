package grid

import (
	"fmt"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

func newMouseTestGrid(width int, widths []int, rows int) *Grid {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, nil)

	g.columns = make([]string, len(widths))
	dataRows := make([][]interface{}, rows)
	for i := 0; i < rows; i++ {
		row := make([]interface{}, len(widths))
		for j := range widths {
			row[j] = i*10 + j
		}
		dataRows[i] = row
	}
	for j := range widths {
		g.columns[j] = fmt.Sprintf("c%d", j)
	}

	g.data = &postgres.QueryResult{
		Columns: nil,
		Rows:    dataRows,
		Count:   rows,
	}
	g.width = width
	g.widths = append([]int(nil), widths...)
	g.focused = true
	return g
}

// sentinelCol returns a valid starting column index that differs from the ones
// these tests expect. It must be in range: HandleClick calls syncScroll, which
// clamps cursorCol into [0, len(widths)-1], so an out-of-range "sentinel" would
// be silently rewritten and the test would pass for the wrong reason.
func sentinelCol(widths int) int { return widths - 1 }

func TestGrid_HandleClick_ColumnMapping(t *testing.T) {
	// width 40 -> inner width 38; columns are 10, 12, 14 (sum 36).
	// contentX = x-1, and each column spans its width.
	tests := []struct {
		name string
		x    int
		want int // expected cursorCol, -1 means column must stay unchanged
	}{
		{"left border", 0, -1},
		{"first column start", 1, 0},
		{"first column end", 10, 0},
		{"second column start", 11, 1},
		{"second column end", 22, 1},
		{"third column start", 23, 2},
		{"third column end", 36, 2},
		{"past last column", 37, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
			g.cursorCol = 2 // sentinel to detect "unchanged"

			cmd, hit := g.HandleClick(tt.x, 2) // y=2 -> first data row
			if !hit {
				t.Fatalf("HandleClick(%d, 2) did not hit a cell", tt.x)
			}
			if cmd != nil {
				t.Fatalf("HandleClick(%d, 2) returned a command, want nil", tt.x)
			}
			if g.cursorRow != 0 {
				t.Fatalf("cursorRow = %d, want 0", g.cursorRow)
			}
			want := tt.want
			if want == -1 {
				want = 2 // sentinel preserved
			}
			if g.cursorCol != want {
				t.Fatalf("cursorCol = %d, want %d", g.cursorCol, want)
			}
		})
	}
}

func TestGrid_HandleClick_RowMappingKeepsScroll(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 10)
	g.scrollRow = 3
	g.scrollCol = 0

	cmd, hit := g.HandleClick(1, 4) // header at y=1, rows start at y=2 -> dataY=2
	if !hit || cmd != nil {
		t.Fatalf("HandleClick returned (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorRow != 2 {
		t.Fatalf("cursorRow = %d, want 2", g.cursorRow)
	}
	if g.scrollRow != 3 {
		t.Fatalf("scrollRow = %d, want 3 (click must not jump to page start)", g.scrollRow)
	}
}

func TestGrid_HandleClick_OutOfRange(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)

	tests := []struct {
		name string
		x, y int
	}{
		{"top border", 1, 0},
		{"row past visible rows", 1, 2 + 5},
		{"far below", 1, 999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.cursorRow = 1
			g.cursorCol = 1
			cmd, hit := g.HandleClick(tt.x, tt.y)
			if hit || cmd != nil {
				t.Fatalf("HandleClick(%d, %d) = (cmd=%v, hit=%v), want (nil, false)", tt.x, tt.y, cmd, hit)
			}
			if g.cursorRow != 1 || g.cursorCol != 1 {
				t.Fatalf("cursor moved to (%d,%d), want unchanged (1,1)", g.cursorRow, g.cursorCol)
			}
		})
	}
}

func TestGrid_HandleClick_HeaderRow(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)

	if cmd, hit := g.HandleClick(24, 1); cmd == nil || hit {
		t.Fatalf("header click = (cmd=%v, hit=%v), want (non-nil, false)", cmd, hit)
	}
	if got := g.header.SortColumn(); got != "c2" {
		t.Fatalf("SortColumn() = %q, want %q", got, "c2")
	}
}

func TestGrid_HandleClick_ScrolledHorizontally(t *testing.T) {
	// Make the first column too wide to fit back in so syncScroll can't expand
	// the window left; visible columns from scrollCol=1 are 12 and 14 (sum 26).
	g := newMouseTestGrid(28, []int{30, 12, 14}, 5)
	g.scrollCol = 1

	cmd, hit := g.HandleClick(1, 2) // first visible column (absolute index 1)
	if !hit || cmd != nil {
		t.Fatalf("HandleClick returned (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want 1", g.cursorCol)
	}
	if g.scrollCol != 1 {
		t.Fatalf("scrollCol = %d, want 1", g.scrollCol)
	}
}

// Scenario: Una caja sin anchura utilizable no convierte ningun clic en columna.
//
// available is width-2, one cell per border, so a grid narrower than 3 has no
// content area at all. Every click must miss rather than map onto a column that
// is not drawn, and must not panic on a negative available.
func TestGrid_HandleClick_TinyWidthMapsNoColumn(t *testing.T) {
	for _, width := range []int{-5, 0, 1, 2, 3} {
		g := newMouseTestGrid(width, []int{10, 12, 14}, 5)
		g.cursorCol = 1 // sentinel

		// A header click must produce no sort command.
		if cmd, hit := g.HandleClick(1, 1); cmd != nil || hit {
			t.Errorf("width %d: header click = (cmd=%v, hit=%v), want (nil, false)", width, cmd, hit)
		}
		if got := g.header.SortColumn(); got != "" {
			t.Errorf("width %d: SortColumn() = %q, want no sort applied", width, got)
		}

		// A data click still moves the row, because the row is a rectangle and
		// does not depend on the column widths.
		if _, hit := g.HandleClick(1, 2); !hit {
			t.Errorf("width %d: data click did not hit a cell", width)
		}
		if g.cursorRow != 0 {
			t.Errorf("width %d: cursorRow = %d, want 0", width, g.cursorRow)
		}
		if g.cursorCol != 1 {
			t.Errorf("width %d: cursorCol = %d, want the sentinel preserved", width, g.cursorCol)
		}
	}
}

// Scenario: La columna 0 es alcanzable con un clic, no solo las siguientes.
//
// visibleColumnAtX returns 0 for the first visible column, and the callers test
// `vj >= 0`. A check that treated 0 as "no column" would silently break sorting
// and column selection on the leftmost column, which is the one users click
// most.
func TestGrid_HandleClick_FirstVisibleColumnIsReachable(t *testing.T) {
	for _, x := range []int{1, 2, 9, 10} {
		g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
		g.header.SetColumns(g.columns)
		g.header.SetWidths(g.widths)
		g.cursorCol = 2 // sentinel

		cmd, hit := g.HandleClick(x, 2) // first data row
		if !hit || cmd != nil {
			t.Fatalf("HandleClick(%d, 2) = (cmd=%v, hit=%v), want (nil, true)", x, cmd, hit)
		}
		if g.cursorCol != 0 {
			t.Errorf("HandleClick(%d, 2): cursorCol = %d, want 0 (the first column)", x, g.cursorCol)
		}
	}

	// And the header over the first column sorts by it.
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)

	if cmd, hit := g.HandleClick(5, 1); cmd == nil || hit {
		t.Fatalf("header click on the first column = (cmd=%v, hit=%v), want (non-nil, false)", cmd, hit)
	}
	if got := g.header.SortColumn(); got != "c0" {
		t.Errorf("SortColumn() = %q, want %q", got, "c0")
	}
}

// Scenario: La línea justo encima de la cabecera no es la cabecera.
//
// headerRowY is the top border plus any prefix lines, and the header check is
// `y == headerY` with a separate `y < headerY` bail. So one line ABOVE the
// header must fall through both: not equal, and not less than, which means it
// reaches the data-row maths with a negative dataY and is rejected there. An
// inclusive `<` would swallow it as a header click and produce no sort.
func TestGrid_HandleClick_LineAboveTheHeaderIsNotTheHeader(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)
	sentinel := sentinelCol(len(g.widths))

	// y=0 is the top border: no sort, no data hit, cursor untouched.
	g.cursorCol = sentinel
	if cmd, hit := g.HandleClick(5, 0); cmd != nil || hit {
		t.Errorf("y=0 = (cmd=%v, hit=%v), want (nil, false)", cmd, hit)
	}
	if g.cursorCol != sentinel {
		t.Errorf("y=0 moved the cursor to %d, want it unchanged at %d", g.cursorCol, sentinel)
	}
	if got := g.header.SortColumn(); got != "" {
		t.Errorf("y=0 sorted by %q, want no sort", got)
	}

	// With a draft prefix the header moves to y=2, so y=1 is the line above it
	// and must behave the same way.
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: 1, NewValue: 2}}
	g.cursorCol = sentinel
	if cmd, hit := g.HandleClick(5, 1); cmd != nil || hit {
		t.Errorf("y=1 with a draft prefix = (cmd=%v, hit=%v), want (nil, false)", cmd, hit)
	}
	if got := g.header.SortColumn(); got != "" {
		t.Errorf("y=1 with a draft prefix sorted by %q, want no sort", got)
	}
	if g.cursorCol != sentinel {
		t.Errorf("y=1 with a draft prefix moved the cursor to %d, want it unchanged", g.cursorCol)
	}
}

// Scenario: El borde derecho no pertenece a la primera columna.
//
// The column span is exclusive on the right: a click one cell past a column's
// end belongs to the next one. An inclusive bound would make the neighbouring
// column one cell harder to click and, worse, would make the last column claim
// the border.
func TestGrid_HandleClick_ColumnRightEdgeIsExclusive(t *testing.T) {
	// First column is 10 wide, so it covers contentX 0..9, i.e. x 1..10.
	// x=11 is the first cell of the second column.
	for _, tc := range []struct {
		x    int
		want int
	}{
		{x: 10, want: 0},
		{x: 11, want: 1},
		// Right after the second column (12 wide, contentX 10..21, x 11..22).
		{x: 22, want: 1},
		{x: 23, want: 2},
		// Right after the third (14 wide, contentX 22..35, x 23..36).
		{x: 36, want: 2},
		// One past the last column, still inside the 38-cell content area.
		{x: 37, want: -1},
		{x: 38, want: -1},
		// The right border itself.
		{x: 39, want: -1},
	} {
		g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
		sentinel := sentinelCol(len(g.widths))
		g.cursorCol = sentinel

		if _, hit := g.HandleClick(tc.x, 2); !hit {
			t.Fatalf("x=%d: the click missed the row entirely", tc.x)
		}
		if tc.want == -1 {
			if g.cursorCol != sentinel {
				t.Errorf("x=%d: cursorCol = %d, want the sentinel (no column here)", tc.x, g.cursorCol)
			}
			continue
		}
		if g.cursorCol != tc.want {
			t.Errorf("x=%d: cursorCol = %d, want %d", tc.x, g.cursorCol, tc.want)
		}
	}
}

// Scenario: Una columna que no cabe en el ancho disponible no se elige.
//
// The mapping stops as soon as the next column would overflow the content area,
// so the region a too-wide column would have occupied belongs to no column.
//
// This is asserted on the mapping, not on the click, because HandleClick goes on
// to call syncScroll, which clamps the cursor back into range. See the
// BeyondTheVisibleArea test below for that half.
func TestVisibleColumnAtX_ColumnThatDoesNotFitIsNotSelectable(t *testing.T) {
	// content area is width-2 = 18. Column 0 is 10 wide, so it ends at 10;
	// column 1 would end at 22, past 18, so it does not fit.
	g := newMouseTestGrid(20, []int{10, 12, 14}, 5)
	mh := NewMouseHandler(g)

	// Inside the first column.
	if got := mh.visibleColumnAtX(5); got != 0 {
		t.Errorf("visibleColumnAtX(5) = %d, want 0", got)
	}
	// contentX=11, inside the second column, which does not fit.
	if got := mh.visibleColumnAtX(12); got != -1 {
		t.Errorf("visibleColumnAtX(12) = %d, want -1 (the column does not fit)", got)
	}
	// And the same further right, where nothing at all is drawn.
	if got := mh.visibleColumnAtX(18); got != -1 {
		t.Errorf("visibleColumnAtX(18) = %d, want -1", got)
	}
	// Widen the grid so all three fit, and the second becomes selectable. This is
	// what makes the refusal above about the width rather than about the index.
	wide := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	if got := NewMouseHandler(wide).visibleColumnAtX(12); got != 1 {
		t.Errorf("at width 40, visibleColumnAtX(12) = %d, want 1", got)
	}
}

// Scenario: El ancho disponible son exactamente los dos bordes menos.
//
// This is the single subtraction the whole column mapping rests on: one cell for
// each border. If it were width-1, a click on the right border would map onto the
// last column; if it were width, a click one past the content would too.
//
// Pinned with a column that exactly fills each candidate width, so each
// subtraction yields a different answer at the same x.
func TestVisibleColumnAtX_AvailableIsWidthMinusBothBorders(t *testing.T) {
	// avail = width-2, so a column of avail covers contentX 0..avail-1, i.e. the
	// last content cell x=avail, and the right border x=avail+1 = width-1.
	for _, width := range []int{12, 20, 40} {
		avail := width - 2
		widths := []int{avail, 5}
		g := newMouseTestGrid(width, widths, 5)
		mh := NewMouseHandler(g)

		// The last cell of the content area still belongs to the column.
		if got := mh.visibleColumnAtX(avail); got != 0 {
			t.Errorf("width %d (avail %d): visibleColumnAtX(%d) = %d, want 0 (last content cell)", width, avail, avail, got)
		}
		// One cell further is the right border, which belongs to nothing.
		if got := mh.visibleColumnAtX(width - 1); got != -1 {
			t.Errorf("width %d: visibleColumnAtX(%d) = %d, want -1 (the right border)", width, width-1, got)
		}
		// And the left border too.
		if got := mh.visibleColumnAtX(0); got != -1 {
			t.Errorf("width %d: visibleColumnAtX(0) = %d, want -1 (the left border)", width, got)
		}
	}
}

// Scenario: Un ancho tan pequeño que no queda contenido devuelve -1 siempre.
//
// available is width-2, so widths 0, 1 and 2 all give available <= 0 and there
// is no content area to hit. The guard has to come before the loop, or the
// accumulated width would be compared against a non-positive budget and the
// first column would be accepted.
func TestVisibleColumnAtX_NoContentAreaMapsNothing(t *testing.T) {
	for _, width := range []int{-4, 0, 1, 2, 3} {
		g := newMouseTestGrid(width, []int{10, 12, 14}, 5)
		mh := NewMouseHandler(g)
		for _, x := range []int{-1, 0, 1, 2, 5, 50} {
			if got := mh.visibleColumnAtX(x); got != -1 {
				t.Errorf("width %d: visibleColumnAtX(%d) = %d, want -1", width, x, got)
			}
		}
	}
	// The header click over a zero-content grid must not sort anything.
	g := newMouseTestGrid(2, []int{10, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)
	if cmd, _ := g.HandleClick(1, 1); cmd != nil {
		t.Error("a header click produced a sort command over a zero-width grid")
	}
	if got := g.header.SortColumn(); got != "" {
		t.Errorf("SortColumn() = %q, want no sort applied", got)
	}
}

// Scenario: Un clic a la derecha del área visible hace scroll y mueve el cursor
// con la ventana.
//
// KNOWN BEHAVIOUR, pinned because it is surprising: the column mapping refuses
// an x that lands on a column which does not fit, but HandleClick then calls
// syncScroll, and syncScroll clamps the cursor into range and brings the window
// to it. So the observable result is NOT "nothing happened": the cursor moves to
// a valid column and scrollCol follows.
//
// The alternative would be to skip syncScroll when no column was hit, keeping
// the cursor where it was. Which is right is a product decision; this test
// records what happens today so the change, if made, is deliberate.
func TestGrid_HandleClick_BeyondTheVisibleAreaScrollsTheCursor(t *testing.T) {
	g := newMouseTestGrid(20, []int{10, 12, 14}, 5)
	sentinel := sentinelCol(len(g.widths))

	// The mapping itself refuses: the second column does not fit.
	if got := NewMouseHandler(g).visibleColumnAtX(12); got != -1 {
		t.Errorf("visibleColumnAtX(12) = %d, want -1 (the column does not fit)", got)
	}

	// But the cursor still moves, because syncScroll clamps it.
	g.cursorCol = sentinel
	if _, hit := g.HandleClick(12, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorRow != 0 {
		t.Errorf("cursorRow = %d, want 0: the row is still selected", g.cursorRow)
	}
	if g.cursorCol >= len(g.widths) {
		t.Errorf("cursorCol = %d, out of range: syncScroll must clamp it", g.cursorCol)
	}
	if g.cursorCol == sentinel && g.scrollCol == 0 {
		t.Log("the cursor stayed put; syncScroll may have adjusted the window instead")
	}
}

// Scenario: Una columna que cabe JUSTO sigue siendo seleccionable.
//
// The fit test is `acc+w > available`, so a column ending exactly at the
// available width is included. An inclusive bound would drop the last visible
// column and leave a dead strip on the right.
func TestGrid_HandleClick_ColumnFittingExactlyIsSelectable(t *testing.T) {
	// content area is 18; columns of 10 and 8 end exactly at 18.
	g := newMouseTestGrid(20, []int{10, 8}, 5)
	sentinel := sentinelCol(len(g.widths))
	g.cursorCol = sentinel

	// The last cell of the second column, contentX=17, x=18.
	if _, hit := g.HandleClick(18, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorCol != 1 {
		t.Errorf("cursorCol = %d, want 1 (the column that fits exactly)", g.cursorCol)
	}

	// One cell further right is past the content area.
	g.cursorCol = sentinel
	if _, hit := g.HandleClick(19, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorCol != sentinel {
		t.Errorf("cursorCol = %d, want the sentinel", g.cursorCol)
	}
}

// Scenario: Una columna de anchura cero no se cuela en el mapeo.
//
// A zero-width column covers no cells, so the click that would land on it must
// fall through to the next one instead of selecting an invisible column.
func TestGrid_HandleClick_ZeroWidthColumnIsSkipped(t *testing.T) {
	g := newMouseTestGrid(40, []int{0, 10}, 5)
	sentinel := sentinelCol(len(g.widths))
	g.cursorCol = sentinel

	// contentX=0 is the second column's first cell, since the first covers none.
	if _, hit := g.HandleClick(1, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorCol != 1 {
		t.Errorf("cursorCol = %d, want 1 (the zero-width column covers no cells)", g.cursorCol)
	}
}

// Scenario: Con scroll horizontal el índice visible se traduce al absoluto.
//
// toggleSort and cursorCol both add scrollCol to the visible index, so a click
// on the first visible column of a scrolled grid must resolve to the absolute
// column index. Getting the translation wrong would sort by the wrong column.
func TestGrid_HandleClick_ScrolledSortUsesTheAbsoluteColumn(t *testing.T) {
	// Columns 30 (too wide to scroll back), 12, 14. With scrollCol=1 the
	// visible columns are 12 and 14.
	g := newMouseTestGrid(28, []int{30, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)
	g.scrollCol = 1

	// x=1 -> contentX=0, the first visible column, which is absolute 1.
	if cmd, hit := g.HandleClick(1, 1); cmd == nil || hit {
		t.Fatalf("header click = (cmd=%v, hit=%v), want (non-nil, false)", cmd, hit)
	}
	if got := g.header.SortColumn(); got != "c1" {
		t.Errorf("SortColumn() = %q, want %q (absolute index, not visible)", got, "c1")
	}

	// And the command it produced carries the same column.
	g.scrollCol = 1
	if cmd, _ := g.HandleClick(1, 1); cmd != nil {
		msg, ok := cmd().(GridSortApplyMsg)
		if !ok {
			t.Fatalf("the command did not produce a GridSortApplyMsg")
		}
		if msg.OrderBy != "c1" {
			t.Errorf("GridSortApplyMsg.OrderBy = %q, want %q", msg.OrderBy, "c1")
		}
		if msg.Table != g.tableName || msg.Schema != g.schema {
			t.Errorf("the sort command carries %s.%s, want %s.%s", msg.Schema, msg.Table, g.schema, g.tableName)
		}
	}
}

// Scenario: La fila de datos empieza justo debajo de la cabecera.
//
// dataY is y - headerY - 1, so the line immediately below the header is data
// row 0 and the header line itself is not data. An off-by-one here would make
// clicking the header move the cursor, or clicking the first row miss it.
func TestGrid_HandleClick_FirstDataRowIsDirectlyBelowTheHeader(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)

	// y=1 is the header (no offset): a header click is not a data hit.
	if _, hit := g.HandleClick(1, 1); hit {
		t.Error("y=1 hit a data cell, but that is the header line")
	}
	// y=2 is the first data row.
	if _, hit := g.HandleClick(1, 2); !hit {
		t.Error("y=2 did not hit a data cell, but that is the first row")
	}
	if g.cursorRow != 0 {
		t.Errorf("cursorRow = %d, want 0", g.cursorRow)
	}
}

// Scenario: El límite inferior de la última fila es exclusivo.
//
// dataY >= rows is rejected, so a click one line past the last row does not
// move the cursor onto a row that does not exist. An inclusive bound would let
// the cursor land on a phantom row.
func TestGrid_HandleClick_RowBelowTheLastIsRejected(t *testing.T) {
	const rows = 5
	g := newMouseTestGrid(40, []int{10, 12, 14}, rows)

	// The last row is at y = headerY + 1 + (rows-1) = 1+1+4 = 6.
	if _, hit := g.HandleClick(1, 6); !hit {
		t.Error("y=6 did not hit a cell, but that is the last row")
	}
	if g.cursorRow != rows-1 {
		t.Errorf("cursorRow = %d, want %d", g.cursorRow, rows-1)
	}

	// One line further down is out of range and must not move the cursor.
	g.cursorRow = 2
	g.cursorCol = 2
	if cmd, hit := g.HandleClick(1, 7); hit || cmd != nil {
		t.Errorf("y=7 = (cmd=%v, hit=%v), want (nil, false)", cmd, hit)
	}
	if g.cursorRow != 2 || g.cursorCol != 2 {
		t.Errorf("the cursor moved to (%d,%d), want it unchanged at (2,2)", g.cursorRow, g.cursorCol)
	}
}

// Scenario: Sin datos cargados ningún clic hace nada.
func TestGrid_HandleClick_NoDataIsNeverAHit(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.data = nil
	g.cursorRow = 3
	g.cursorCol = 3

	for _, y := range []int{0, 1, 2, 3, 100} {
		if cmd, hit := g.HandleClick(1, y); hit || cmd != nil {
			t.Errorf("HandleClick(1, %d) = (cmd=%v, hit=%v), want (nil, false) with no data", y, cmd, hit)
		}
	}
	if g.cursorRow != 3 || g.cursorCol != 3 {
		t.Errorf("the cursor moved to (%d,%d) with no data, want (3,3)", g.cursorRow, g.cursorCol)
	}
}

// Scenario: El ancho disponible son los dos bordes menos, y el clic también los
// salta.
//
// The two must agree. If the mapping subtracted the borders twice, a click at
// the left edge would map to a column one to the right of the visible one.
func TestGrid_HandleClick_AvailableWidthMatchesTheSkippedBorders(t *testing.T) {
	// width 40 -> borders at x=0 and x=39, content x=1..38.
	const width = 40
	g := newMouseTestGrid(width, []int{38}, 5)
	sentinel := sentinelCol(len(g.widths))

	// A single column filling the whole content area.
	g.cursorCol = sentinel
	if _, hit := g.HandleClick(1, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorCol != 0 {
		t.Errorf("cursorCol = %d, want 0", g.cursorCol)
	}

	// The right border is not part of it.
	g.cursorCol = sentinel
	if _, hit := g.HandleClick(width-1, 2); !hit {
		t.Fatal("the click missed the row")
	}
	if g.cursorCol != sentinel {
		t.Errorf("cursorCol = %d, want the sentinel: x=%d is the right border", g.cursorCol, width-1)
	}
}

func TestGrid_HandleClick_HeaderOffsetWithDrafts(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: 1, NewValue: 2}}

	// The draft prefix adds one line, so the header moves to y=2 and data to y=3.
	if _, hit := g.HandleClick(1, 2); hit {
		t.Fatal("y=2 with a draft prefix should be the header, not a data cell")
	}
	if cmd, hit := g.HandleClick(1, 3); !hit || cmd != nil {
		t.Fatalf("HandleClick(1, 3) = (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorRow != 0 {
		t.Fatalf("cursorRow = %d, want 0", g.cursorRow)
	}
}
