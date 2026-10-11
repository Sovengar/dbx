package grid

// Scenario: Los supervivientes no-Boundary del sweep de table.go.
//
// Four survivors of this file are killed below: the two `cursorCol - scrollCol`
// subtractions that turn a VISIBLE index into a sum (the multi-selected branch and the
// edit branch), the edited row's own index under scroll, and the pending-rows loop's
// counter. Three more are proven equivalent in the allowlist: the wrap's `len(columns)-1`
// (syncScroll clamps any out-of-range cursor back onto the last column), the `>=` clause
// of the edit-row test (subsumed by the equality clause right before it), and the
// empty-table branch's counter (it feeds only the padding loop, whose trailing empty
// lines the body's own TrimRight removes).

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sweepRowLines is the row block of the records view, raw, sliced the way the contract
// tests slice it: after the header, exactly contentHeight lines.
func sweepRowLines(g *Grid) []string {
	out := resultOf(g.renderRecordsView())
	head := g.recordsHeaderOffset()
	return out[head+1 : head+1+g.contentHeight()]
}

// selectedSig is the Selected style's own opening escape, the byte signature every
// highlighted cell must carry.
func selectedSig(g *Grid) string {
	sel := g.styles.Selected.Render("x")
	return sel[:strings.Index(sel, "x")]
}

// Scenario: La fila en edición sigue siendo su fila de datos cuando la rejilla está
// desplazada.
//
// Pins the `visibleCount == (g.editRow-(offset+g.scrollRow))` clause of the edit-row test
// (line 2203): scrollRow=1 with editRow=2 is the fixture that distinguishes the
// subtraction (view index 1) from a sum (index 3). It is also the premise the allowlist
// proof for the line's second clause rests on — that equality, when it holds, already
// implies the row is inside the window.
func TestTheEditedRowStaysOnItsDataRowWhenScrolled(t *testing.T) {
	g := newGrid(t, 6, 2, 40, 14, 100)
	g.scrollRow = 1
	g.editing = true
	g.editRow = 2 // the row an unscrolled grid edits, now drawn at view index 1
	g.editCol = 0
	g.editValue = "wxyz"
	g.editCursor = len(g.editValue)

	row := ansi.Strip(sweepRowLines(g)[1])
	if !strings.Contains(row, "wxy") {
		t.Errorf("view row 1 does not carry the in-progress value: %q", row)
	}
	if strings.Contains(row, "r2c0") {
		t.Errorf("view row 1 still shows the committed value instead of the edit: %q", row)
	}
}

// Scenario: El resaltado de la fila del cursor seleccionada queda dentro de la ventana.
//
// Kills the ARITHMETIC_BASE/INVERT_NEGATIVES pair on 2228 (`g.cursorCol - g.scrollCol`
// -> `+` in the multi-selected branch: a VISIBLE index computed as a sum points past the
// visible window and no cell is highlighted). The fixture puts cursorCol+scrollCol
// outside the window: on dead code the last visible cell carries the Selected style, on
// the mutant no cell does.
func TestTheMultiSelectedCursorRowKeepsItsHighlightInsideTheWindow(t *testing.T) {
	g := newGrid(t, 3, 6, 120, 12, 100)
	g.cursorRow = 1
	g.selectedRows[1] = true
	g.scrollCol = 1
	g.cursorCol = 5 // visible index 4: cursorCol-scrollCol, the last visible column

	if row := sweepRowLines(g)[1]; !strings.Contains(row, selectedSig(g)) {
		t.Errorf("the multi-selected cursor row has no highlighted cell: %q", row)
	}
}

// Scenario: El cursor se resalta aunque su columna editada quede fuera de la ventana.
//
// Kills the ARITHMETIC_BASE/INVERT_NEGATIVES pair on 2224 (the same `cursorCol -
// scrollCol` -> `+`, in the edit branch that renders when the edited column itself is
// scrolled out of view). editCol=0 with scrollCol=2 puts the edited column off-window, so
// the row falls into that branch; cursorCol=3 makes the dead code highlight visible cell
// 1 while the sum (5) points past the four visible cells and highlights nothing.
func TestAnEditedRowKeepsTheCursorHighlightWhenItsColumnIsOffWindow(t *testing.T) {
	g := newGrid(t, 3, 6, 120, 12, 100)
	g.editing = true
	g.editRow = 1
	g.editCol = 0 // off-window: scrollCol=2 makes editColLocal negative
	g.editValue = "V"
	g.editCursor = 0
	g.cursorRow = 1
	g.cursorCol = 3
	g.scrollCol = 2

	if row := sweepRowLines(g)[1]; !strings.Contains(row, selectedSig(g)) {
		t.Errorf("the edited row has no highlighted cursor cell: %q", row)
	}
}

// Scenario: Las filas pendientes no desbordan el panel.
//
// Kills INCREMENT_DECREMENT 2277 (`visibleCount++` -> `--` in the pending-rows loop):
// with the counter decrementing, the loop's own guard `visibleCount < contentHeight`
// never trips, so all ten pending rows are appended instead of the five the panel holds.
// The pending rows carry content, so the body's TrimRight cannot swallow the overflow and
// the view outgrows its panel: 13 lines instead of the panel's 10.
func TestPendingRowsCannotOutgrowThePanel(t *testing.T) {
	const height = 12
	g := newGrid(t, 0, 2, 40, height, 100)
	g.pendingRows = make([][]interface{}, 10)
	for i := range g.pendingRows {
		g.pendingRows[i] = []interface{}{i, "p"}
	}

	if got := len(resultOf(g.renderRecordsView())); got != height-2 {
		t.Errorf("ten pending rows grew the records view to %d lines, want the panel's %d", got, height-2)
	}
}
