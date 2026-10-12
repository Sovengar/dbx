package grid

// CONDITIONALS_BOUNDARY sweep of table.go (group gridtable).
//
// Every survivor below is either killed or proven equivalent, with the
// reasoning in each scenario comment:
//
//   - Clamps that assign the value the variable already holds at the
//     boundary (`w < 6` with w == 6, `maxColWidth < 10` with 10, the
//     `x >= y` -> `x > y` boundary rewrites, ...): the extra branch fires
//     only on the exact boundary and writes back the same number.
//   - Guards implied by a neighbouring invariant: `len(g.widths) > 0`
//     (widths is built 1:1 with columns, and the empty-columns return
//     sits above it), `i < len(g.widths)` in the used-sum loop (syncScroll
//     clamps cursorCol to len-1 first, and the && short-circuits before
//     the second operand is ever evaluated at i == len).
//   - Loops whose body is a no-op on the boundary: `len(g.data.Rows) > 0`
//     with zero rows iterates nothing.
//   - Divergent pairs that converge: the shrink loop's extra pass at
//     `used == available` is always undone by the expand loop right
//     below it (used - w + w == used <= available), so both reach the
//     same fixed point; the padding loop's extra empty line at
//     `visibleCount == contentHeight` is absorbed by the body gap
//     computed underneath (gap = target - bodyLines - 1), and every
//     line the shift moves is an indistinguishable empty string.
//   - Dead ranges: contentHeight has a floor of 1, so `contentHeight > 0`
//     never sees 0; RenderDraftUpdateRow with zero pendingUpdates gets a
//     draftCols map that is false everywhere and falls into the exact
//     Selected/Text branches RenderRow already takes; the isEditing
//     equality clause can never hold when editRow == endRow inside the
//     loop (visibleCount ranges over [0, limit-scrollRow), the equality
//     needs exactly limit-scrollRow).

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

// ── calculateWidths (table.go:310) ─────────────────────────────────────────

// Scenario: el ancho minimo de una columna es exactamente 6.
//
// Pins `w < 6` (table.go:323): a four-char column header lands on w == 6,
// the boundary. The mutant (`w <= 6`) assigns 6 there too — the clamp body
// writes back the value the variable already holds. The row-value pass and
// the shared cap below use the same shape.
func TestGridTableCalculateWidthsAppliesTheMinimumColumnWidth(t *testing.T) {
	g := newGrid(t, 1, 1, 40, 12, 100)
	if g.widths[0] != 6 {
		t.Fatalf("a four-char header is %d cells wide, want the 6-cell minimum", g.widths[0])
	}
}

// Scenario: sin filas de datos el pase de valores no hace nada.
//
// Pins `len(g.data.Rows) > 0` (table.go:329): with zero rows the body of
// the guard is a range over an empty slice, so `> 0` and `>= 0` run the
// exact same (zero) iterations. The widths come out of the header pass
// alone, identically.
func TestGridTableCalculateWidthsSkipsTheRowLoopOnEmptyData(t *testing.T) {
	g := newGrid(t, 0, 2, 40, 12, 100)
	g.calculateWidths() // must not panic; the loop never runs
	if len(g.widths) != 2 || g.widths[0] != 6 {
		t.Fatalf("widths = %v, want the two 6-cell header widths", g.widths)
	}
}

// Scenario: un valor que ya iguala el ancho de la columna no lo ensancha.
//
// Pins `w > g.widths[i]` (table.go:336): a value whose padded width equals
// the current width is the boundary. `>=` assigns w over widths[i] — the
// same number that is already there.
func TestGridTableCalculateWidthsKeepsTheWidthWhenTheValueMatches(t *testing.T) {
	g := newGrid(t, 1, 1, 40, 12, 100)
	base := g.widths[0]
	g.data.Rows[0][0] = strings.Repeat("x", base-2) // padded width == base
	g.calculateWidths()
	if g.widths[0] != base {
		t.Fatalf("a matching value widened the column to %d, want %d", g.widths[0], base)
	}
}

// Scenario: sin cols, calculateWidths vuelve antes del bloque de caps.
//
// IN-ALLOWLIST line 345, `len(g.widths) > 0`. The guard below it divides by
// minCols = min(5, len(widths)); with an empty widths slice that is a
// division by zero, so the entry was accepted as unreachable. It is:
// widths is created as make([]int, len(g.columns)) (table.go:316) and the
// function returns at table.go:311 when columns is empty, so every path
// that reaches line 345 has len(widths) == len(columns) > 0. The mutant
// (`>=`) would only diverge on a state no caller can produce.
func TestGridTableCalculateWidthsReturnsEarlyWithoutColumns(t *testing.T) {
	empty := newGrid(t, 0, 0, 40, 12, 100)
	empty.calculateWidths() // the early return at table.go:311
	if len(empty.widths) != 0 {
		t.Fatalf("a grid without columns kept %d widths, want none", len(empty.widths))
	}
	withCols := newGrid(t, 2, 3, 40, 12, 100)
	if len(withCols.widths) != len(withCols.columns) {
		t.Fatalf("widths and columns disagree: %d widths for %d columns", len(withCols.widths), len(withCols.columns))
	}
}

// Scenario: cinco cols reparten el ancho disponible a partes iguales.
//
// Pins `len(g.widths) < minCols` (table.go:347) and `maxColWidth < 10`
// (table.go:351). Five columns is the first boundary of the first guard
// (5 < 5 is false; the mutant's <= takes the branch and assigns minCols =
// 5, the value it already holds). The second boundary is maxColWidth == 10:
// available = 52-2 = 50 over minCols = 5 gives exactly 10, and the
// mutant's <= clamps 10 to 10.
func TestGridTableCalculateWidthsSharesTheAvailableWidthEvenly(t *testing.T) {
	g := newGrid(t, 1, 5, 52, 12, 100)
	for i := range g.columns {
		g.data.Rows[0][i] = strings.Repeat("y", 15) // padded width 17 > 10
	}
	g.calculateWidths()
	for i, w := range g.widths {
		if w != 10 {
			t.Errorf("column %d is %d cells wide, want the shared cap of 10", i, w)
		}
	}
}

// Scenario: una columna que ya esta en el maximo compartido no se recorta dos veces.
//
// Pins `g.widths[i] > maxColWidth` (table.go:355): a value whose padded
// width equals maxColWidth (8 chars + 2 == 10) is the boundary. The
// mutant's >= writes maxColWidth over widths[i] — the same number.
func TestGridTableCalculateWidthsCapsAtTheSharedMaximum(t *testing.T) {
	g := newGrid(t, 1, 5, 52, 12, 100)
	for i := range g.columns {
		g.data.Rows[0][i] = strings.Repeat("z", 8) // padded width 10 == maxColWidth
	}
	g.calculateWidths()
	for i, w := range g.widths {
		if w != 10 {
			t.Errorf("column %d is %d cells wide, want 10", i, w)
		}
	}
}

// ── syncScroll (table.go:366) ───────────────────────────────────────────────

// Scenario: un cursorCol negativo o cero se clampa a cero.
//
// Pins `g.cursorCol < 0` (table.go:379). The boundary is cursorCol == 0:
// the original does not enter, the mutant's <= enters and assigns 0 —
// the value the variable already holds.
func TestGridTableSyncScrollClampsANegativeCursorColumn(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	g.cursorCol = 0
	g.syncScroll()
	if g.cursorCol != 0 {
		t.Errorf("cursorCol stayed at %d, want 0", g.cursorCol)
	}
	g.cursorCol = -3
	g.syncScroll()
	if g.cursorCol != 0 {
		t.Errorf("a negative cursorCol landed on %d, want 0", g.cursorCol)
	}
}

// Scenario: un scrollCol negativo o cero se clampa a cero.
//
// Pins `g.scrollCol < 0` (table.go:385); same boundary shape as above.
func TestGridTableSyncScrollClampsANegativeScrollColumn(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	g.scrollCol = 0
	g.syncScroll()
	if g.scrollCol != 0 {
		t.Errorf("scrollCol stayed at %d, want 0", g.scrollCol)
	}
	g.scrollCol = -2
	g.syncScroll()
	if g.scrollCol != 0 {
		t.Errorf("a negative scrollCol landed on %d, want 0", g.scrollCol)
	}
}

// Scenario: cursor y ventana coincidentes no se tocan.
//
// Pins `g.cursorCol < g.scrollCol` (table.go:394). With both at 2 the
// original leaves the window alone; the mutant's <= rewrites scrollCol
// with cursorCol's value — the same 2. The ordinary case (cursor left of
// the window) still drags the window to the cursor on both variants.
func TestGridTableSyncScrollBringsTheWindowToACoincidentCursor(t *testing.T) {
	g := newGrid(t, 2, 3, 22, 12, 100)
	g.widths = []int{20, 20} // available is 20: the span never fits a column more
	g.cursorCol = 1
	g.scrollCol = 1
	g.syncScroll()
	if g.scrollCol != 1 {
		t.Errorf("a coincident window moved to %d, want 1", g.scrollCol)
	}
	g.cursorCol = 0
	g.scrollCol = 1
	g.syncScroll()
	if g.scrollCol != 0 {
		t.Errorf("a window right of the cursor stayed at %d, want the cursor's 0", g.scrollCol)
	}
}

// Scenario: la suma del usado nunca avanza mas alla de la ultima col.
//
// Pins `i < len(g.widths)` (table.go:402). The second operand of the loop
// condition is dead: syncScroll clamps cursorCol to len(widths)-1 above
// (table.go:382), and the first operand `i <= g.cursorCol` is already
// false by the time i reaches len(widths), so the && never evaluates the
// second one at the only value (<=) would add. The mutant would index
// widths[len] — a panic — on a state the clamp makes unreachable. This is
// the same dead-clause shape the allowlist records for table.go:2203.
func TestGridTableSyncScrollNeverCountsPastTheLastColumn(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	g.widths = []int{8, 10}
	g.cursorCol = 1 // the last column: the boundary the clamp allows
	g.scrollCol = 0
	g.syncScroll() // must not panic on either variant
	if g.scrollCol != 0 {
		t.Errorf("the window scrolled to %d, want 0", g.scrollCol)
	}
}

// Scenario: un usado que cabe justo no encoge la ventana.
//
// Pins `used > available` (table.go:408). With widths [8, 10] and a
// 20-cell grid, available is 18 and used is exactly 18: the original
// stops at the boundary, the mutant's >= takes one shrink step. The
// expand loop directly below undoes that step without thinking: it adds
// widths[scrollCol-1] back while used + w <= available, and here
// (used - w) + w == used == available. Both variants converge on the same
// fixed point — the smallest scrollCol whose span fits — so the extra
// pass changes nothing observable.
func TestGridTableSyncScrollKeepsTheWindowWhenUsedEqualsAvailable(t *testing.T) {
	g := newGrid(t, 2, 3, 20, 12, 100)
	g.widths = []int{8, 10}
	g.cursorCol = 1
	g.scrollCol = 0
	g.syncScroll()
	if g.scrollCol != 0 {
		t.Errorf("a fitting window shrank to %d, want 0", g.scrollCol)
	}
}

// ── UndoRowDrafts (table.go:1182) ───────────────────────────────────────────

// Scenario: deshacer restaura tambien la primera fila de datos.
//
// Kills `u.RowIdx >= 0` (table.go:1201) mutated to `>`. RowIdx == 0 is the
// boundary and the one the old guard excluded: the original restores
// data.Rows[0][0] from the pending update, the mutant skips the write
// entirely and the row keeps the edited value.
func TestGridTableUndoRowDraftsRestoresTheFirstRow(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	g.cursorRow = 0
	g.scrollRow = 0
	g.data.Rows[0][0] = "CHANGED"
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: marker(0, 0)}}

	if n := g.UndoRowDrafts(); n != 1 {
		t.Fatalf("UndoRowDrafts reverted %d updates, want 1", n)
	}
	if g.data.Rows[0][0] != marker(0, 0) {
		t.Errorf("the first row kept %q after the undo, want %q", g.data.Rows[0][0], marker(0, 0))
	}
}

// ── handleEditKey / runeAt / expandEditCol (table.go:1405-1608) ─────────────

// Scenario: una columna de edicion fuera del listado de anchos se ignora.
//
// Kills `g.editCol < len(g.widths)` (table.go:1520) mutated to `<=`. The
// boundary editCol == len(widths) is not reachable through the key path
// today — every writer clamps (cursorCol via syncScroll, tab/enter via
// the modulo) — but it is a state the struct allows, and there the
// mutant evaluates widths[len] and panics where the original returned
// cleanly. The test drives that boundary directly: one keystroke, no
// panic, value inserted.
func TestGridTableEditKeyRefusesAnOutOfRangeColumn(t *testing.T) {
	g := editing(t, 1, 1)
	g.editCol = len(g.widths)
	g.editValue = ""
	g.editCursor = 0

	if _, handled := g.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); !handled {
		t.Fatal("the keystroke was not handled")
	}
	if g.editValue != "x" {
		t.Errorf("the keystroke landed in %q, want %q", g.editValue, "x")
	}
}

// Scenario: la columna se ensancha solo cuando el texto supera su ancho.
//
// Kills `len(g.editValue)+4 > g.widths[g.editCol]` (table.go:1500, the
// second operand of the same line) mutated to >=. The editor is emptied
// and the column fixed at 6 cells, so the second typed character lands
// exactly on len+4 == widths. The original does not expand; the mutant
// does, and the wide column name carries the width from 6 to 19.
func TestGridTableEditKeyExpandsOnlyPastTheColumnWidth(t *testing.T) {
	g := editing(t, 1, 1)
	g.columns[1] = "columnnumberone" // name width 19, above the 6-cell fix
	g.widths[1] = 6
	g.editValue = ""
	g.editCursor = 0

	typeIn(t, g, "qq") // len 2 + 4 == 6 == widths[1]: the boundary

	if g.widths[1] != 6 {
		t.Errorf("a value that exactly fits expanded the column to %d cells, want 6", g.widths[1])
	}
}

// Scenario: runeAt devuelve RuneError en ambos extremos.
//
// Pins `i >= len(s)` (table.go:1535) mutated to >. At i == len(s) the
// original already returns RuneError, and utf8.DecodeRuneInString on the
// empty slice returns (RuneError, 0) anyway, so the mutant's extra path
// produces the identical zero value.
func TestGridTableRuneAtReportsRuneErrorAtBothEnds(t *testing.T) {
	if r := runeAt("abc", 3); r != utf8.RuneError {
		t.Errorf("runeAt at the end returned %q, want RuneError", r)
	}
	if r := runeAt("abc", 9); r != utf8.RuneError {
		t.Errorf("runeAt past the end returned %q, want RuneError", r)
	}
	if r := runeAt("abc", 0); r != 'a' {
		t.Errorf("runeAt at the start returned %q, want 'a'", r)
	}
}

// Scenario: runeAtBefore devuelve RuneError en ambos extremos.
//
// Pins `i <= 0` (table.go:1559) mutated to <. At i == 0 the original
// already returns RuneError; the mutant's extra path decodes the empty
// prefix s[:0], and DecodeLastRuneInString("") is (RuneError, 0) too.
func TestGridTableRuneAtBeforeReportsRuneErrorAtBothEnds(t *testing.T) {
	if r := runeAtBefore("abc", 0); r != utf8.RuneError {
		t.Errorf("runeAtBefore at 0 returned %q, want RuneError", r)
	}
	if r := runeAtBefore("abc", -2); r != utf8.RuneError {
		t.Errorf("runeAtBefore below 0 returned %q, want RuneError", r)
	}
	if r := runeAtBefore("abc", 1); r != 'a' {
		t.Errorf("runeAtBefore at 1 returned %q, want 'a'", r)
	}
}

// Scenario: expandEditCol no hace nada cuando el nombre ya iguala el ancho.
//
// Pins `colNameWidth > expandedWidth` (table.go:1598) mutated to >=. A
// 16-char column name against a 20-cell width gives colNameWidth == 20 ==
// expandedWidth: the boundary. The mutant assigns colNameWidth over
// expandedWidth — the same 20.
func TestGridTableExpandEditColKeepsTheWidthWhenTheNameMatches(t *testing.T) {
	g := newGrid(t, 1, 2, 200, 16, 100)
	g.columns[0] = "sixteencharsxx" // name width 20
	g.widths[0] = 20
	g.editCol = 0
	g.editOrigWidth = 0
	g.editValue = ""
	g.expandEditCol()
	if g.widths[0] != 20 {
		t.Errorf("the column grew to %d cells, want the untouched 20", g.widths[0])
	}
}

// Scenario: expandEditCol no hace nada cuando el texto ya iguala el ancho.
//
// Pins `editValWidth > expandedWidth` (table.go:1601) mutated to >=. A
// 16-character value against a 20-cell width: the boundary again, and the
// assignment the mutant adds writes back 20.
func TestGridTableExpandEditColKeepsTheWidthWhenTheValueMatches(t *testing.T) {
	g := newGrid(t, 1, 2, 200, 16, 100)
	g.columns[0] = "c0"
	g.widths[0] = 20
	g.editCol = 0
	g.editOrigWidth = 0
	g.editValue = strings.Repeat("q", 16) // value width 20
	g.expandEditCol()
	if g.widths[0] != 20 {
		t.Errorf("the column grew to %d cells, want the untouched 20", g.widths[0])
	}
}

// Scenario: expandEditCol nunca pasa de 80 celdas.
//
// Pins `expandedWidth > 80` (table.go:1604) mutated to >=. A 76-char
// value against an 80-cell width lands exactly on 80: the mutant clamps
// 80 to 80.
func TestGridTableExpandEditColCapsTheWidthAtEighty(t *testing.T) {
	g := newGrid(t, 1, 2, 200, 16, 100)
	g.columns[0] = "c0"
	g.widths[0] = 80
	g.editCol = 0
	g.editOrigWidth = 0
	g.editValue = strings.Repeat("q", 76) // value width 80
	g.expandEditCol()
	if g.widths[0] != 80 {
		t.Errorf("the column grew to %d cells, want the cap of 80", g.widths[0])
	}
}

// ── scroll arithmetic (table.go:1943-2082) ───────────────────────────────────

// Scenario: pinToLastRow clampa un scrollRow negativo y uno exactamente cero.
//
// Pins `g.scrollRow < 0` (table.go:1945) mutated to <=. totalRows == ch
// gives scrollRow == 0: the boundary where the mutant's extra branch
// assigns 0. The negative case (a content height above the row count) is
// the guard's real work and behaves the same on both variants.
func TestGridTablePinToLastRowClampsTheNegativeScroll(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 100)
	g.pinToLastRow(2, 5) // scrollRow = -3
	if g.scrollRow != 0 || g.cursorRow != 1 {
		t.Errorf("negative scroll landed on (scrollRow %d, cursorRow %d), want (0, 1)", g.scrollRow, g.cursorRow)
	}
	g.pinToLastRow(5, 5) // scrollRow = 0 exactly
	if g.scrollRow != 0 || g.cursorRow != 4 {
		t.Errorf("zero scroll landed on (scrollRow %d, cursorRow %d), want (0, 4)", g.scrollRow, g.cursorRow)
	}
}

// Scenario: halfPageUp nunca pasa de la primera fila.
//
// Pins `g.cursorRow < 0` (table.go:1962) mutated to <= and the nested
// `g.scrollRow < 0` (table.go:1965) mutated to <=. A cursorRow of exactly
// half the content height is the first boundary: cursorRow - half == 0,
// the original leaves the window alone, the mutant enters and adds 0 to
// scrollRow and rewrites cursorRow with 0. The negative case clamps a
// negative scrollRow to 0 on both variants.
func TestGridTableHalfPageUpStopsOnTheFirstRow(t *testing.T) {
	g := newGrid(t, 10, 2, 40, 16, 100) // contentHeight 10, half 5
	g.cursorRow = 5                     // 5 - 5 == 0: the boundary
	g.scrollRow = 2
	g.halfPageUp()
	if g.cursorRow != 0 || g.scrollRow != 2 {
		t.Errorf("half page up from the boundary moved to (scrollRow %d, cursorRow %d), want (2, 0)", g.scrollRow, g.cursorRow)
	}

	g.cursorRow = 2 // 2 - 5 = -3: scrollRow 0 + -3 clamps to 0
	g.scrollRow = 0
	g.halfPageUp()
	if g.cursorRow != 0 || g.scrollRow != 0 {
		t.Errorf("half page up from the top moved to (scrollRow %d, cursorRow %d), want (0, 0)", g.scrollRow, g.cursorRow)
	}
}

// Scenario: rowsAfterOffset clampa a cero tambien en el limite exacto.
//
// Pins `remaining < 0` (table.go:2046) mutated to <=. With the pager on
// the page that starts past the last row, remaining == 0: the mutant's
// extra branch returns 0, which is what the function returns anyway. The
// same clamp catches a fully-past-the-end offset (-1) on both variants.
func TestGridTableRowsAfterOffsetClampsAtZero(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 1) // pageSize 1
	g.pager.page = 4                 // offset 3 == len(Rows): remaining == 0
	if got := g.rowsAfterOffset(); got != 0 {
		t.Errorf("offset == row count gave %d rows left, want 0", got)
	}
	g.pager.page = 5 // offset 4 > len(Rows): remaining -1
	if got := g.rowsAfterOffset(); got != 0 {
		t.Errorf("offset past the end gave %d rows left, want 0", got)
	}
}

// Scenario: visibleRows coincide con el tamano de pagina en el limite.
//
// Pins `totalVisible < g.pager.Limit()` (table.go:2059) mutated to <=.
// A page holding exactly pageSize rows makes totalVisible == Limit: the
// original falls through to `return g.pager.Limit()`, the mutant returns
// totalVisible — the same number, by definition of the boundary.
func TestGridTableVisibleRowsMatchesThePageSizeAtTheLimit(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 3) // exactly pageSize rows
	if got := g.visibleRows(); got != 3 {
		t.Errorf("a full page reported %d visible rows, want the limit of 3", got)
	}
}

// Scenario: contentHeight nunca baja de 1.
//
// Pins `h < 1` (table.go:2078) mutated to <=. A window one row above the
// overhead gives h == 1 exactly: the mutant's floor assigns 1. Smaller
// windows hit the floor on both variants.
func TestGridTableContentHeightHasAFloorOfOne(t *testing.T) {
	g := newGrid(t, 2, 2, 40, 7, 100) // height 7 - overhead 6 == 1: the boundary
	if got := g.contentHeight(); got != 1 {
		t.Errorf("a one-row window reported a content height of %d, want 1", got)
	}
	g = newGrid(t, 2, 2, 40, 4, 100)
	if got := g.contentHeight(); got != 1 {
		t.Errorf("a too-short window reported a content height of %d, want 1", got)
	}
}

// ── renderRecordsView (table.go:2162-2351) ──────────────────────────────────

// Scenario: un startRow negativo se clampa a la primera fila.
//
// Pins `startRow < 0` (table.go:2182) mutated to <=. A scrollRow of -1
// against offset 0 gives startRow == -1 and renders row 0; the boundary
// startRow == 0 (no scroll) leaves the first row first on both variants.
func TestGridTableRenderClampsANegativeStartRow(t *testing.T) {
	g := newGrid(t, 2, 2, 40, 12, 100)
	g.scrollRow = -1
	rows := sweepRowLines(g)
	if !strings.Contains(rows[0], marker(0, 0)) {
		t.Errorf("a negative start row rendered %q first, want row 0", rows[0])
	}
}

// Scenario: endRow nunca pasa de los datos, ni justo en el limite.
//
// Pins `endRow > len(g.data.Rows)` (table.go:2187) mutated to >=. With
// pageSize == len(Rows) and offset 0, endRow == len(Rows) == 3: the
// mutant clamps 3 to 3. The render shows every row on both variants.
func TestGridTableRenderKeepsTheEndRowInsideTheData(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 3)
	view := g.View()
	for r := 0; r < 3; r++ {
		if !strings.Contains(view, marker(r, 0)) {
			t.Errorf("row %d is missing from the render with endRow == len(Rows)", r)
		}
	}
}

// Scenario: el panel muestra, como mucho, contentHeight filas.
//
// Kills `visibleCount < contentHeight` (table.go:2192) mutated to <=. A
// nine-cell window holds a content height of 3, and the data has six rows:
// the original stops after the third, the mutant's <= lets the loop take
// a fourth step and render row 3 too. The row markers are greppable, so
// the over-render is a one-line assertion.
func TestGridTableRenderShowsAtMostContentHeightRows(t *testing.T) {
	g := newGrid(t, 6, 2, 40, 9, 100) // contentHeight 3
	if strings.Contains(g.View(), marker(3, 0)) {
		t.Errorf("a 3-row panel rendered a fourth row: %q", marker(3, 0))
	}
}

// Scenario: una fila de datos corta no rompe el render.
//
// Kills `colIdx < len(row)` (table.go:2197) mutated to <=. Row 0 carries
// one value against two visible columns: colIdx == len(row) == 1 is the
// boundary the original declines and the mutant enters, indexing row[1]
// on a length-1 slice. The render must complete and still show the value
// that is there.
func TestGridTableRenderSurvivesAShortRow(t *testing.T) {
	g := newGrid(t, 2, 2, 40, 12, 100)
	g.data.Rows[0] = []interface{}{"solo"}
	view := g.View() // must not panic
	if !strings.Contains(view, "solo") {
		t.Errorf("the surviving value is missing from the render")
	}
}

// Scenario: una edicion fuera de la ventana no se dibuja.
//
// Pins `g.editRow < endRow` (table.go:2203, last clause) mutated to <=.
// The window is scrolled one row and editRow sits at endRow == len(Rows),
// the value the mutant newly accepts. It is unreachable: the first
// clause of the same && demands visibleCount == editRow-(offset+scrollRow),
// and inside the loop visibleCount only ever ranges over
// [0, limit-scrollRow) — the value the equality needs is the one just past
// the last iteration. Both variants render the grid with no edit cell.
func TestGridTableRenderIgnoresAnEditPastTheWindow(t *testing.T) {
	g := editing(t, 1, 1)
	g.scrollRow = 1 // the window opens on row 1, so row 6 is its endRow
	g.editRow = 6   // == endRow: the boundary the mutant would accept
	g.editValue = "ZZZ"
	g.editCursor = 0

	view := g.View() // must not panic
	if strings.Contains(view, "█") {
		t.Errorf("a row outside the window rendered as an edit row")
	}
}

// Scenario: la columna editada se dibuja en la primera columna visible.
//
// Pins `editColLocal >= 0` (table.go:2221) mutated to >. editCol == 1
// with scrollCol == 1 puts the edited column at local index 0: the
// boundary the original renders through RenderEditCell and the mutant
// drops into the plain row branch. The edit cell paints the block cursor
// "█", which no plain row carries.
func TestGridTableRenderShowsTheEditCellOnTheFirstVisibleColumn(t *testing.T) {
	g := editing(t, 1, 1)
	g.scrollCol = 1 // editCol 1 - scrollCol 1 == 0: the boundary

	if view := g.View(); !strings.Contains(view, "█") {
		t.Errorf("the edited cell at local index 0 lost its edit cell")
	}
}

// Scenario: una fila seleccionada sin pendientes se resalta igual.
//
// Pins `len(g.pendingUpdates) > 0` (table.go:2232) mutated to >=. With
// zero pending updates the boundary the mutant newly enters, but the
// draftCols map is then false everywhere, and RenderDraftUpdateRow falls
// into the exact Selected/Text branches RenderRow takes through
// RenderCell(..., i == activeCol). Byte-identical output.
func TestGridTableRenderSelectedRowWithoutDrafts(t *testing.T) {
	g := newGrid(t, 2, 2, 40, 12, 100)
	g.cursorRow = 1
	if row := sweepRowLines(g)[1]; !strings.Contains(row, selectedSig(g)) {
		t.Errorf("the cursor row carries no highlighted cell without drafts")
	}
}

// Scenario: una fila plana sin pendientes no se resalta.
//
// Pins `len(g.pendingUpdates) > 0` (table.go:2241) mutated to >=. The
// not-selected row of the same boundary: no draft is pending, so
// RenderDraftUpdateRow's empty draftCols leaves every cell in the plain
// Text branch, exactly as RenderRow with no active column already did.
func TestGridTableRenderPlainRowWithoutDrafts(t *testing.T) {
	g := newGrid(t, 2, 2, 40, 12, 100)
	g.cursorRow = 1
	if row := sweepRowLines(g)[0]; strings.Contains(row, selectedSig(g)) {
		t.Errorf("a plain row carries a highlighted cell without drafts")
	}
}

// Scenario: las filas pendientes tampoco desbordan el panel.
//
// Kills `visibleCount < contentHeight` (table.go:2252, pending-rows
// loop) mutated to <=. A one-row window (height 7) shows its single data
// row and fills the counter exactly; the original stops, the mutant's <=
// renders the pending row on top of a full panel.
func TestGridTableRenderShowsAtMostContentHeightPendingRows(t *testing.T) {
	g := newGrid(t, 1, 2, 40, 7, 100) // contentHeight 1
	g.pendingRows = [][]interface{}{{"PEND", "x"}}

	if strings.Contains(g.View(), "PEND") {
		t.Errorf("a pending row was rendered on top of a full panel")
	}
}

// Scenario: una fila pendiente corta no rompe el render.
//
// Kills `colIdx < len(row)` (table.go:2257, pending-rows loop) mutated to
// <=. The pending row carries one value against the two visible columns:
// colIdx == len(row) is the boundary that indexes one past the end on the
// mutant. The render must complete.
func TestGridTableRenderSurvivesAShortPendingRow(t *testing.T) {
	g := newGrid(t, 1, 2, 40, 9, 100) // contentHeight 2 (overhead 7: the pending-drafts prefix): the pending row is on screen
	g.pendingRows = [][]interface{}{{"x"}}

	view := g.View() // must not panic
	if !strings.Contains(view, "x") {
		t.Errorf("the pending row's surviving value is missing from the render")
	}
}

// Scenario: el resaltado del cursor sobrevive a una columna editada fuera de la ventana.
//
// Kills `editColLocal < len(visWidths)` (table.go:2266) mutated to <=.
// editCol == 3 with no scroll puts the edited column at local index 3 ==
// len(visWidths): the original falls to the plain-row branch, which still
// highlights the cursor's own cell; the mutant's <= routes the row
// through RenderEditRow, and with no local index matching editCol that
// renderer highlights nothing at all.
// Scenario: la fila de insercion pendiente editada justo fuera de la ventana
// se pinta como fila plana, sin celda de edicion.
//
// Pins table.go:2266 `editColLocal < len(visWidths)` mutated to `<=`.
// At editColLocal == len(visWidths) the mutant routes through
// RenderDraftInsertEditRow with editCol == len(widths): the loop
// `for i := range values` never reaches i == editCol, so every cell takes
// the else arm, which is byte-identical to RenderDraftInsertRow. Neither
// variant can show the edit value, and the row renders the same — the
// boundary is observable only as "still the plain draft row".
func TestGridTablePendingInsertEditOutOfWindowKeepsThePlainRow(t *testing.T) {
	g := newGrid(t, 6, 3, 60, 16, 100)
	if _, handled := g.HandleAction("insert_row"); !handled {
		t.Fatal("insert_row was not handled")
	}
	g.pendingRows[0] = []interface{}{"p0", "p1", "p2"}
	_, visWidths := g.visibleColumns()
	g.pendingCol = g.scrollCol + len(visWidths) // exactly at the boundary
	g.editValue = "BND-EDIT-VALUE"
	g.editCursor = 0

	view := g.View()
	if !strings.Contains(view, "p0") {
		t.Fatalf("the pending insert row did not render at all: %q", view)
	}
	if strings.Contains(view, "BND-EDIT-VALUE") {
		t.Errorf("the pending edit cell rendered past the window edge: %q", view)
	}
}

func TestGridTableRenderHighlightsTheCursorWhenTheEditedColumnIsOffWindow(t *testing.T) {
	g := editing(t, 1, 1)
	g.editCol = 3 // local index 3 == len(visWidths): the boundary

	if view := g.View(); !strings.Contains(view, selectedSig(g)) {
		t.Errorf("the cursor's cell lost its highlight off-window")
	}
}

// Scenario: una tabla vacia dibuja su mensaje, nunca un hueco.
//
// Pins `contentHeight > 0` (table.go:2280) mutated to >=. The boundary
// contentHeight == 0 is unreachable — contentHeight floors at 1
// (table.go:2078) — so both variants take the branch on exactly the same
// states. A one-row window is the tightest case the floor allows.
func TestGridTableRenderDrawsTheEmptyMessage(t *testing.T) {
	g := newGrid(t, 0, 2, 40, 7, 100) // contentHeight 1
	if view := g.View(); !strings.Contains(view, "Empty table") {
		t.Errorf("the empty-table message is missing from the render")
	}
}

// Scenario: el relleno inferior deja el panel exactamente en su altura.
//
// Pins `i < contentHeight` (table.go:2286, padding loop) mutated to <=.
// The mutant appends one more empty line than the rows block needs — and
// the body gap computed right below (target - bodyLines - 1) shrinks by
// exactly one to compensate. Every line the shift moves is an
// indistinguishable empty string, so the render is byte-identical: the
// panel keeps height-2 lines.
func TestGridTableRenderPadsExactlyToTheContentHeight(t *testing.T) {
	g := newGrid(t, 0, 2, 40, 12, 100)
	if got := len(resultOf(g.renderRecordsView())); got != 10 {
		t.Errorf("an empty panel rendered %d lines, want the 10 the box holds", got)
	}
}

// Scenario: el indicador de modo queda pegado al borde inferior.
//
// Pins `target < 1` (table.go:2342) mutated to <= and `gap < 0`
// (table.go:2346) mutated to <=. A three-cell window gives target == 1,
// the first boundary (the mutant assigns 1). The second boundary,
// gap == 0, is reachable through the filter popup: a two-line popup over
// a five-line content height fills the body to exactly target-1 lines,
// and the mutant's <= rewrites gap with the 0 it already holds.
func TestGridTableRenderKeepsTheIndicatorAtTheBottom(t *testing.T) {
	tiny := newGrid(t, 0, 2, 40, 3, 100) // target == 1: the boundary
	if got := len(resultOf(tiny.View())); got != 3 {
		t.Errorf("a three-line box rendered %d lines, want the 3 the border holds", got)
	}

	g := newGrid(t, 0, 2, 40, 12, 100) // contentHeight 6, no filter
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), g.data.Columns, 40)
	wf.Show()
	wf.showPopup = true
	wf.suggestions = []Suggestion{{Label: "aa"}, {Label: "bb"}} // a two-line popup: gap == 0
	g.whereFilter = wf

	if got := len(resultOf(g.View())); got != 12 {
		t.Errorf("a body that exactly fills the box rendered %d lines, want the 12 the border holds", got)
	}
}
