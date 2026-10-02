// The contracts of the records grid, asserted as properties rather than one
// scenario at a time.
//
// table_test.go walks DraftSQL and CommitAllDrafts: given a pending change, what
// statement comes out. What is here is everything else, and what is here is what
// actually kills mutants — invariants held across a matrix of sizes and states,
// so a change to a clamp, a bound or an index cannot pass.
//
// THE FIXTURE IS THE POINT. Every pre-existing fixture in this package built its
// grid as New(styles, 0, nil), which is wrong in two independent ways:
//
//	pageSize 0 makes Pager.Limit() return 0, so renderRecordsView computes
//	endRow == 0 and the row loop `for i := startRow; i < endRow` never runs.
//	Not one row was ever rendered by the whole suite.
//
//	a nil keybinds Resolver panics the moment handleKey resolves a key, so
//	nothing that goes through the key path could be tested at all.
//
// Between them those two account for the large majority of this file's NOT
// COVERED mutants. Every fixture below uses newGrid, which has a real page size
// and a real registry. Do not add a fixture that does not.
//
// The invariants, in one place:
//
//	I1  every cursor movement leaves the cursor on a row that exists
//	I2  a column wider than its share is capped; the sum of the visible
//	    columns never exceeds the pane
//	I3  the scroll window always contains the cursor column
//	I4  the rendered view has exactly height-2 content lines, the last of
//	    which is the mode indicator, and the row block is exactly
//	    contentHeight() lines
package grid

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

// newGrid is the only supported way to build a Grid in a test here. See the file
// comment for why pageSize 0 and a nil Resolver are both wrong.
func newGrid(t *testing.T, nRows, nCols, width, height, pageSize int) *Grid {
	t.Helper()
	styles := theme.Resolve("dark").Styles()
	g := New(styles, pageSize, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(width)
	g.SetHeight(height)
	// Update drops everything for an unfocused grid, so a focused fixture is
	// what makes the key path reachable at all. HandleAction deliberately
	// skips that guard, which is why the pre-existing action tests could get
	// away without it.
	g.Focus()

	if nCols > 0 {
		cols := make([]postgres.ColumnInfo, nCols)
		for i := range nCols {
			cols[i] = postgres.ColumnInfo{Name: fmt.Sprintf("c%d", i), DataType: "text"}
		}
		rows := make([][]interface{}, nRows)
		for r := range nRows {
			row := make([]interface{}, nCols)
			for c := range nCols {
				// Short, distinct, and greppable: the cell renderer
				// truncates long values, so a fixture whose marker does
				// not fit would be measuring truncation instead of
				// placement.
				row[c] = fmt.Sprintf("r%dc%d", r, c)
			}
			rows[r] = row
		}
		g.SetData(&postgres.QueryResult{Columns: cols, Rows: rows, Count: nRows}, "public", "t")
	} else {
		g.SetData(&postgres.QueryResult{}, "public", "t")
	}
	return g
}

// resultOf reads a rendered grid, splitting into lines. Named so a failure that
// prints it reads as what it is.
func resultOf(render string) []string { return strings.Split(render, "\n") }

// marker is the value newGrid put at (row, col).
func marker(row, col int) string { return fmt.Sprintf("r%dc%d", row, col) }

// ---------------------------------------------------------------------------
// I1: the cursor
// ---------------------------------------------------------------------------

// Scenario: Ningun movimiento deja el cursor fuera del rango.
//
// moveDown, moveToLast, halfPageDown and clampCursor all end in the same
// arithmetic — cursorRow is derived from scrollRow and the row count — so a
// change to any one of them shows up as a cursor pointing at a row that does not
// exist. That is not a rendering bug, it is a panic or a wrong-row edit one
// keystroke later, which is why it is worth a property rather than a scenario.
func TestGrid_TheCursorIsAlwaysOnARowThatExists(t *testing.T) {
	// Rows, columns and height are crossed rather than fixed: the cursor
	// arithmetic has three independent inputs and a fixture that varies
	// only one of them pins only one branch.
	for _, nRows := range []int{1, 2, 5, 12, 40} {
		for _, height := range []int{8, 12, 20, 30} {
			for _, pageSize := range []int{3, 10, 100} {
				t.Run(fmt.Sprintf("%d rows, height %d, page %d", nRows, height, pageSize), func(t *testing.T) {
					g := newGrid(t, nRows, 3, 80, height, pageSize)

					check := func(what string) {
						t.Helper()
						total := g.visibleRows()
						if total == 0 {
							if g.cursorRow != 0 || g.scrollRow != 0 {
								t.Fatalf("%s left the cursor at row %d scroll %d with nothing to point at", what, g.cursorRow, g.scrollRow)
							}
							return
						}
						if g.cursorRow < 0 || g.scrollRow < 0 {
							t.Fatalf("%s produced a negative cursor: row %d scroll %d", what, g.cursorRow, g.scrollRow)
						}
						if got := g.scrollRow + g.cursorRow; got >= total {
							t.Fatalf("%s left the cursor on row %d of %d visible rows", what, got, total)
						}
						// And the cursor has to be inside the drawn
						// block, or the highlight lands on a blank line.
						if g.cursorRow >= g.contentHeight() {
							t.Fatalf("%s left the cursor at row %d but only %d rows are drawn", what, g.cursorRow, g.contentHeight())
						}
					}

					check("the initial state")
					for range nRows + height {
						g.moveDown()
						check("moveDown")
					}
					for range nRows + height + 5 {
						g.moveUp()
						check("moveUp")
					}
					g.moveToLast()
					check("moveToLast")
					for range height {
						g.halfPageDown()
						check("halfPageDown")
					}
					for range height {
						g.halfPageUp()
						check("halfPageUp")
					}
					g.moveToFirst()
					check("moveToFirst")
					g.clampCursor()
					check("clampCursor")
				})
			}
		}
	}
}

// Scenario: Bajar en la ultima fila NO la mueve, y subir en la primera tampoco.
//
// This is the property that makes navigation terminate. The asymmetry matters:
// moveDown refuses when the cursor is on the LAST row, and moveUp refuses when it
// is on the FIRST — and when the cursor is on the first row moveUp may still move
// the scroll, which is how a scrolled window walks back to the top.
func TestGrid_VerticalMovementStopsAtBothEnds(t *testing.T) {
	for _, nRows := range []int{1, 3, 10} {
		for _, height := range []int{8, 20} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 80, height, 100)

				// Down at the last row is a no-op.
				for range nRows * 3 {
					g.moveDown()
				}
				atBottom := fmt.Sprintf("%d/%d", g.scrollRow, g.cursorRow)
				g.moveDown()
				if got := fmt.Sprintf("%d/%d", g.scrollRow, g.cursorRow); got != atBottom {
					t.Errorf("moveDown at the last row moved %s to %s", atBottom, got)
				}

				// Up at the top is a no-op, and the walk ends at zero.
				for range nRows*3 + 10 {
					g.moveUp()
				}
				if g.scrollRow != 0 || g.cursorRow != 0 {
					t.Errorf("after moving up past the top the cursor is at %d/%d, want 0/0", g.scrollRow, g.cursorRow)
				}
				before := g.scrollRow
				g.moveUp()
				if g.scrollRow != before || g.cursorRow != 0 {
					t.Errorf("moveUp at the top moved the cursor to %d/%d", g.scrollRow, g.cursorRow)
				}
			})
		}
	}
}

// Scenario: moveToLast deja el cursor en la ULTIMA fila, no en la ultima dibujada.
//
// The two differ when the content is taller than the pane: the cursor row is the
// offset within the drawn block, so the last row means total-1 in absolute terms,
// which lives at cursorRow == ch-1 once the window has been scrolled to its end.
// Testing "cursorRow == ch-1" instead would pass a grid that jumped to the
// bottom of the window without counting the scroll.
func TestGrid_MoveToLastLandsOnTheLastRowNotTheLastLine(t *testing.T) {
	for _, nRows := range []int{1, 4, 9, 30, 100} {
		for _, height := range []int{8, 14, 25} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 80, height, 100)
				g.moveToLast()

				total := g.visibleRows()
				if got := g.scrollRow + g.cursorRow; got != total-1 {
					t.Errorf("moveToLast put the cursor on row %d of %d rows", got, total)
				}
				ch := g.contentHeight()
				if total <= ch {
					if g.scrollRow != 0 {
						t.Errorf("a content of %d rows in a %d-line window scrolled to %d, want 0", total, ch, g.scrollRow)
					}
				} else {
					// The window is pushed down as far as it goes and the
					// cursor sits on the final drawn line.
					if g.cursorRow != ch-1 {
						t.Errorf("the cursor is on drawn line %d of a %d-line window", g.cursorRow, ch)
					}
					if g.scrollRow != total-ch {
						t.Errorf("scrollRow is %d, want %d (the last full window)", g.scrollRow, total-ch)
					}
				}
				// moveToFirst undoes all of it.
				g.moveToFirst()
				if g.scrollRow != 0 || g.cursorRow != 0 {
					t.Errorf("moveToFirst left the cursor at %d/%d", g.scrollRow, g.cursorRow)
				}
			})
		}
	}
}

// Scenario: Media pagina mueve la MITAD del alto, y no mas.
//
// halfPageDown adds half the window and then pulls the cursor back to the last
// drawn line, so the cursor's ABSOLUTE position moves by at least half and at
// most half plus the cursor's previous offset. A change to the divisor, or to the
// `ch - 1` clamp, shows up here as a jump that is not the right size.
func TestGrid_HalfPageMovesHalfTheWindow(t *testing.T) {
	for _, nRows := range []int{60, 100} {
		for _, height := range []int{10, 14, 24} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 80, height, 100)
				ch := g.contentHeight()
				half := ch / 2

				// Start on the last drawn line with the window at the top,
				// so a half page down has room to move without the clamp
				// at the end of the data taking over.
				g.scrollRow = 0
				g.cursorRow = ch - 1
				start := g.cursorRow
				if start+half >= nRows {
					t.Skipf("the fixture has %d rows and a half page from line %d needs %d", nRows, start, start+half)
				}

				g.halfPageDown()
				got := g.scrollRow + g.cursorRow
				if want := start + half; got != want {
					t.Errorf("halfPageDown moved the cursor from %d to %d, want %d (half a window is %d)", start, got, want, half)
				}

				// And back up returns to where it started.
				g.halfPageUp()
				if got := g.scrollRow + g.cursorRow; got != start {
					t.Errorf("halfPageUp returned to %d, want %d", got, start)
				}
				// Near the end of the data the CLAMP wins over the half
				// page, and the cursor lands on the last row rather than
				// half a window past it. That is the same arithmetic as
				// moveToLast, reached a different way.
				edge := newGrid(t, nRows, 2, 80, height, 100)
				for range nRows {
					edge.halfPageDown()
				}
				if got := edge.scrollRow + edge.cursorRow; got != edge.visibleRows()-1 {
					t.Errorf("half-pageing to the end left the cursor on row %d, want the last of %d", got, edge.visibleRows())
				}

				// And a half page on a one-line window is a no-op, not a
				// negative move.
				tiny := newGrid(t, nRows, 2, 80, 7, 100)
				if tiny.contentHeight() != 1 {
					t.Fatalf("the fixture's window is %d lines, want 1", tiny.contentHeight())
				}
				tiny.halfPageDown()
				if tiny.scrollRow != 0 || tiny.cursorRow != 0 {
					t.Errorf("halfPageDown on a one-line window moved to %d/%d", tiny.scrollRow, tiny.cursorRow)
				}
				tiny.halfPageUp()
				if tiny.scrollRow != 0 || tiny.cursorRow != 0 {
					t.Errorf("halfPageUp on a one-line window moved to %d/%d", tiny.scrollRow, tiny.cursorRow)
				}
			})
		}
	}
}

// Scenario: El scroll horizontal SIEMPRE contiene la columna del cursor.
//
// This is the property that justifies tracking `used` separately from the
// viewport capacity: a cursor that scrolls out of view is a cursor whose cell is
// drawn with no highlight, which looks like a stuck selection. Every horizontal
// move and every width change must preserve it.
func TestGrid_TheColumnWindowAlwaysContainsTheCursorColumn(t *testing.T) {
	for _, width := range []int{20, 40, 60, 80, 120, 200} {
		for _, nCols := range []int{1, 2, 3, 7, 15} {
			t.Run(fmt.Sprintf("pane %d, %d columns", width, nCols), func(t *testing.T) {
				g := newGrid(t, 3, nCols, width, 14, 100)

				check := func(what string) {
					t.Helper()
					if g.scrollCol < 0 || g.scrollCol > g.cursorCol {
						t.Fatalf("%s: the window starts at column %d and the cursor is at %d, so the cursor is outside it",
							what, g.scrollCol, g.cursorCol)
					}
					// The span from the window start to the cursor must
					// fit inside the pane, or the cell is clipped away.
					used := 0
					for i := g.scrollCol; i <= g.cursorCol; i++ {
						used += g.widths[i]
					}
					if used > width-2 {
						t.Fatalf("%s: columns %d..%d need %d cells in a %d-cell pane, so the cursor cell is clipped",
							what, g.scrollCol, g.cursorCol, used, width-2)
					}
				}

				check("the initial state")
				for range nCols * 3 {
					g.moveRight()
					check("moveRight")
				}
				for range nCols * 3 {
					g.moveLeft()
					check("moveLeft")
				}
				// And the width changes underneath the cursor, which is
				// the case a window computed only on column movement
				// gets wrong.
				for _, w := range []int{200, 30, 100, 20, 80} {
					g.SetWidth(w)
					g.syncScroll()
					check(fmt.Sprintf("SetWidth(%d)", w))
				}
			})
		}
	}
}

// Scenario: Las columnas horizontalmente ENVUELTAN, no se detienen.
//
// Right from the last column goes to the first AND resets the window; left from
// the first goes to the last. So a cursor on a one-column grid moves right and
// stays put — which is the case where a wrap that also moved scrollCol would
// show up as a window that has nothing in it.
func TestGrid_HorizontalMovementWraps(t *testing.T) {
	for _, nCols := range []int{1, 2, 5} {
		t.Run(fmt.Sprintf("%d columns", nCols), func(t *testing.T) {
			g := newGrid(t, 2, nCols, 30, 12, 100)

			// Right off the end wraps to the first column.
			for range nCols {
				g.moveRight()
			}
			if g.cursorCol != 0 {
				t.Errorf("moving right %d times over %d columns left the cursor at %d, want 0", nCols, nCols, g.cursorCol)
			}
			if g.scrollCol != 0 {
				t.Errorf("wrapping to the first column left the window at %d, want 0", g.scrollCol)
			}

			// Left off the front wraps to the last.
			g.moveLeft()
			if g.cursorCol != nCols-1 {
				t.Errorf("moving left from the first column left the cursor at %d, want %d", g.cursorCol, nCols-1)
			}

			// One column: both wraps are identities.
			one := newGrid(t, 2, 1, 30, 12, 100)
			one.moveRight()
			if one.cursorCol != 0 {
				t.Errorf("moving right on a one-column grid left the cursor at %d", one.cursorCol)
			}
			one.moveLeft()
			if one.cursorCol != 0 {
				t.Errorf("moving left on a one-column grid left the cursor at %d", one.cursorCol)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// I2 + I3: widths and the visible window
// ---------------------------------------------------------------------------

// Scenario: Una columna se acota, y la suma de las visibles no pasa del panel.
//
// Two separate bounds, both of which a change would break visibly: the per-column
// cap (so one long column cannot eat the pane) and the accumulate-and-break (so
// the visible set is a prefix of the columns from the window start, and its total
// fits). The minimum column width is 6 for a short name and at least 10 after the
// cap, so a grid that dropped the floor would render unreadably thin columns.
func TestGrid_ColumnWidthsAreCappedAndTheVisibleSetFits(t *testing.T) {
	for _, width := range []int{10, 24, 40, 80, 160} {
		for _, nCols := range []int{1, 3, 6, 12} {
			t.Run(fmt.Sprintf("pane %d, %d columns", width, nCols), func(t *testing.T) {
				g := newGrid(t, 2, nCols, width, 12, 100)

				// Every column has a width, they are all at least the
				// floor, and none is wider than the cap.
				cap := (width - 2) / max(min(nCols, 5), 1)
				cap = max(cap, 10)
				for i, w := range g.widths {
					if w < 6 {
						t.Errorf("column %d is %d cells wide, want at least 6", i, w)
					}
					if w > cap {
						t.Errorf("column %d is %d cells wide, over the cap of %d for %d columns in a %d-cell pane", i, w, cap, nCols, width)
					}
				}

				// The visible set starts at the window, is a contiguous
				// run, and fits.
				cols, widths := g.visibleColumns()
				if len(cols) != len(widths) {
					t.Fatalf("%d visible names for %d widths", len(cols), len(widths))
				}
				for i, c := range cols {
					if c != g.columns[g.scrollCol+i] {
						t.Errorf("visible column %d is %q, want %q — the set must be a contiguous run from the window start",
							i, c, g.columns[g.scrollCol+i])
					}
				}
				total := 0
				for _, w := range widths {
					total += w
				}
				if total > width-2 {
					t.Errorf("the visible columns total %d cells in a %d-cell pane", total, width-2)
				}
				// One more column must NOT fit, or the loop stopped early.
				if g.scrollCol+len(cols) < len(g.columns) {
					next := g.widths[g.scrollCol+len(cols)]
					if total+next <= width-2 {
						t.Errorf("the window stopped at %d of %d columns although column %d would fit (%d+%d <= %d)",
							len(cols), nCols, g.scrollCol+len(cols), total, next, width-2)
					}
				}
			})
		}
	}
}

// Scenario: El ancho de una columna es el nombre, el valor mas largo, y 6 como minimo.
//
// The floor matters on its own: "c0" plus two padding cells is four, so without a
// floor every one-column grid would render a 4-cell column and the type would be
// cut. The value scan matters because a column's width follows the LONGEST value
// in it, not the last one.
func TestGrid_ColumnWidthFollowsTheWidestValueNotTheLast(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(200)
	g.SetHeight(12)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "k"}, {Name: "v"}, {Name: "w"}},
		Rows: [][]interface{}{
			{"a", "a-very-long-value-that-widens-the-column", "b"},
			{"c", "d", "e"},
		},
		Count: 2,
	}, "public", "t")

	if len(g.widths) != 3 {
		t.Fatalf("widths has %d entries, want 3", len(g.widths))
	}
	// The long value is 38 characters plus two padding cells.
	want := len("a-very-long-value-that-widens-the-column") + 2
	if g.widths[1] != want {
		t.Errorf("the column holding the long value is %d cells, want %d — the width follows the widest value", g.widths[1], want)
	}
	// The third column's last value is "e", but its first is "b": both are
	// one character, so the floor is what comes back.
	if g.widths[2] != 6 {
		t.Errorf("a column of one-character values is %d cells, want the floor of 6", g.widths[2])
	}
	// And the value scan stops at the column count rather than reading past
	// a short row.
	short := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	short.SetWidth(200)
	short.SetHeight(12)
	short.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}},
		Rows:    [][]interface{}{{"only-one-value"}},
		Count:   1,
	}, "public", "t")
	if len(short.widths) != 2 {
		t.Fatalf("a row with fewer values than columns produced %d widths", len(short.widths))
	}
}

// Scenario: La altura de contenido descuenta la UI de encima, nunca baja de 1.
//
// contentHeight is the row budget the renderer and every movement function share,
// so a change to it is not a rendering change — it is a change to where the cursor
// may go. The extra line appears once for the pending-changes prefix and once for
// whichever of the filter bar, the WHERE banner and the column finder is showing,
// and those three are mutually exclusive.
func TestGrid_ContentHeightAccountsForTheSurroundingUI(t *testing.T) {
	g := newGrid(t, 5, 3, 80, 20, 100)
	base := g.contentHeight()
	if want := 20 - 6; base != want {
		t.Errorf("a bare grid's content height is %d, want %d (height minus six lines of chrome)", base, want)
	}

	// One extra line for the draft banner.
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "x", NewValue: "y"}}
	withDrafts := g.contentHeight()
	if withDrafts != base-1 {
		t.Errorf("a grid with drafts has content height %d, want one less than %d", withDrafts, base)
	}
	g.pendingUpdates = nil

	// One extra for a WHERE clause.
	g.whereClause = "id = 1"
	if got := g.contentHeight(); got != base-1 {
		t.Errorf("a grid with a WHERE clause has content height %d, want %d", got, base-1)
	}
	// And the column finder, not both.
	g.filtering = true
	if got := g.contentHeight(); got != base-1 {
		t.Errorf("a grid with a WHERE clause AND the column finder has content height %d, want %d: the two are alternatives, not additions", got, base-1)
	}
	g.whereClause = ""
	g.filtering = false
	if got := g.contentHeight(); got != base {
		t.Errorf("clearing the filter lines restored content height %d, want %d", got, base)
	}

	// The floor: a grid too short for its own chrome still draws one row,
	// because zero rows means the empty message and the header alone.
	tiny := newGrid(t, 2, 2, 40, 3, 100)
	if got := tiny.contentHeight(); got != 1 {
		t.Errorf("a 3-line grid has content height %d, want the floor of 1", got)
	}

	// And the header offset mirrors that, because click hit-testing walks
	// down by it.
	if got := g.recordsHeaderOffset(); got != 0 {
		t.Errorf("a bare grid's header offset is %d, want 0", got)
	}
	g.whereClause = "id = 1"
	if got := g.recordsHeaderOffset(); got != 1 {
		t.Errorf("with a WHERE clause the header offset is %d, want 1", got)
	}
	g.whereClause = ""
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "x", NewValue: "y"}}
	if got := g.recordsHeaderOffset(); got != 1 {
		t.Errorf("with drafts the header offset is %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// I4: the rendered view
// ---------------------------------------------------------------------------

// Scenario: La vista tiene EXACTAMENTE height-2 lineas de contenido, y la ultima
// es el indicador de modo.
//
// The grid box spends two lines on borders, so the content has height-2 to fill,
// and the renderer pads the gap itself rather than letting the bordered box do it.
// That matters because the mode indicator has to be the LAST content line: the
// bordered box pads below its content, so a gap added there would put the
// indicator in the middle of a blank region instead of against the bottom border.
//
// The padding is computed from the body it actually built, so the count is a
// function of the height and not of how much content there was — which is what
// makes it a property rather than a coincidence.
func TestGrid_TheViewFillsItsHeightAndEndsWithTheModeIndicator(t *testing.T) {
	for _, height := range []int{8, 10, 14, 20, 31} {
		for _, nRows := range []int{0, 1, 5, 60} {
			for _, drafts := range []bool{false, true} {
				t.Run(fmt.Sprintf("height %d, %d rows, drafts=%v", height, nRows, drafts), func(t *testing.T) {
					g := newGrid(t, nRows, 3, 40, height, 100)
					if drafts {
						g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
					}

					out := resultOf(g.renderRecordsView())
					if len(out) != height-2 {
						t.Fatalf("the view has %d content lines, want %d (the box spends two on borders):\n%s",
							len(out), height-2, strings.Join(out, "\n"))
					}
					last := ansi.Strip(out[len(out)-1])
					if last != "  NORMAL  " {
						t.Errorf("the last line is %q, want the NORMAL indicator", last)
					}
					if drafts {
						if first := ansi.Strip(out[0]); !strings.Contains(first, "pending change") {
							t.Errorf("with drafts the first line is %q, want the pending-changes banner", first)
						}
					} else if strings.Contains(ansi.Strip(out[0]), "pending change") {
						t.Errorf("without drafts the first line is still the banner: %q", ansi.Strip(out[0]))
					}

					// And the box itself is exactly as wide as it was
					// told, every line, borders included.
					boxed := resultOf(g.View())
					for i, l := range boxed {
						if got := ansi.StringWidth(l); got != 40 {
							t.Errorf("boxed line %d is %d cells wide, want 40", i, got)
						}
					}
					if len(boxed) != height {
						t.Errorf("the boxed view has %d lines, want %d", len(boxed), height)
					}
				})
			}
		}
	}
}

// Scenario: El indicador de modo cambia con el modo, y solo con el modo.
//
// EDIT is not a hint — it is the only thing telling the user that their next
// keystrokes go into a cell rather than moving the cursor. So it has to track
// `editing` exactly, and it has to be last, because the bordered box pads below.
func TestGrid_TheModeIndicatorTracksEditingAndStaysLast(t *testing.T) {
	for _, height := range []int{9, 14, 22} {
		g := newGrid(t, 4, 2, 40, height, 100)

		out := resultOf(g.renderRecordsView())
		if last := ansi.Strip(out[len(out)-1]); last != "  NORMAL  " {
			t.Errorf("a grid that is not editing ends with %q", last)
		}

		g.editing = true
		out = resultOf(g.renderRecordsView())
		if last := ansi.Strip(out[len(out)-1]); last != "  EDIT  " {
			t.Errorf("a grid that is editing ends with %q, want the EDIT indicator", last)
		}

		g.editing = false
		out = resultOf(g.renderRecordsView())
		if last := ansi.Strip(out[len(out)-1]); last != "  NORMAL  " {
			t.Errorf("back to normal ends with %q", last)
		}
	}
}

// Scenario: Las filas salen EN ORDEN, y son exactamente las que caben.
//
// The row block is a window onto the data: start at the page offset plus the
// scroll, stop at contentHeight rows or the end of the data, whichever comes
// first, then the pending inserts below. So the number of row lines is a function
// of three inputs and a mutation to any of them either drops a row, invents one or
// reorders them.
//
// Which is why the assertion is positional: each row line has to carry the marker
// of the row the window says it should, and the pending inserts have to come
// AFTER the real ones, because they are appended to the same result and rendered
// below.
func TestGrid_RowsAreTheWindowOntoTheDataInOrder(t *testing.T) {
	for _, nRows := range []int{1, 4, 9} {
		for _, height := range []int{9, 13, 20} {
			for _, scrollRow := range []int{0, 1, 3} {
				t.Run(fmt.Sprintf("%d rows, height %d, scroll %d", nRows, height, scrollRow), func(t *testing.T) {
					g := newGrid(t, nRows, 2, 40, height, 100)

					// The pending inserts go in BEFORE the height is
					// read, because having drafts adds the banner line
					// and therefore takes a row slot. Reading the budget
					// first and adding rows afterwards would size the block
					// against a height that is no longer the one in effect.
					g.pendingRows = [][]interface{}{{"p0c0", "p0c1"}, {"p1c0", "p1c1"}}
					ch := g.contentHeight()

					// Walk the cursor down so the scroll window really is
					// at `scrollRow`, rather than setting it behind
					// the movement logic.
					for range scrollRow {
						g.moveDown()
					}
					if g.scrollRow != scrollRow {
						t.Skipf("the cursor movement reached scroll %d, not %d", g.scrollRow, scrollRow)
					}

					out := resultOf(g.renderRecordsView())
					head := g.recordsHeaderOffset()
					// Exactly the row budget. Slicing to the end of the
					// output would also take in the trailing blanks and
					// the mode indicator, so a length check on it would be
					// measuring the wrong thing.
					block := out[head+1 : head+1+ch]

					// Real rows first, from the window, capped by the
					// height.
					real := min(nRows-scrollRow, ch)
					for i := range real {
						want := marker(scrollRow+i, 0)
						if !strings.Contains(ansi.Strip(block[i]), want) {
							t.Errorf("row line %d carries %q, want the first column of row %d (%q):\n%s",
								i, ansi.Strip(block[i]), scrollRow+i, want, strings.Join(out, "\n"))
						}
					}
					// Then the inserts, while there is room.
					pend := min(2, max(ch-real, 0))
					for i := range pend {
						want := fmt.Sprintf("p%dc0", i)
						if !strings.Contains(ansi.Strip(block[real+i]), want) {
							t.Errorf("insert line %d carries %q, want %q: pending inserts are drawn BELOW the real rows:\n%s",
								i, ansi.Strip(block[real+i]), want, strings.Join(out, "\n"))
						}
					}
					// The block is exactly the height: rows, then
					// blanks, never more.
					if len(block) != ch {
						t.Errorf("the row block has %d lines, want the content height of %d", len(block), ch)
					}
					// A blank line where a row should be would be an
					// iteration that advanced without drawing.
					for i := range real + pend {
						if ansi.Strip(block[i]) == "" {
							t.Errorf("row line %d is blank although %d rows should be drawn", i, real+pend)
						}
					}
				})
			}
		}
	}
}

// Scenario: Una tabla sin filas lo DICE, en vez de pintar un marco vacio.
//
// Zero rows is a real state — a fresh table, or a filter that matched nothing —
// and an empty block of blanks is indistinguishable from a rendering failure. The
// message is one line, and it takes a row slot: contentHeight is a budget, not a
// suggestion.
func TestGrid_AnEmptyTableSaysSo(t *testing.T) {
	g := newGrid(t, 0, 3, 40, 12, 100)
	ch := g.contentHeight()

	out := resultOf(g.renderRecordsView())
	head := g.recordsHeaderOffset()
	block := out[head+1 : head+1+ch]

	if len(block) != ch {
		t.Fatalf("the row block has %d lines, want %d", len(block), ch)
	}
	if msg := ansi.Strip(block[0]); !strings.Contains(msg, "Empty table") {
		t.Errorf("the first row line is %q, want the empty-table message", msg)
	}
	// It is one line, not one per missing row.
	for i := 1; i < len(block); i++ {
		if strings.Contains(ansi.Strip(block[i]), "Empty table") {
			t.Errorf("the empty message is repeated on row line %d", i)
		}
	}
	// And View() says something other than the empty message: the box with
	// its title is the answer.
	if boxed := ansi.Strip(g.View()); !strings.Contains(boxed, "Empty table") {
		t.Error("the boxed view does not carry the empty-table message")
	}
}

// Scenario: La caja central solo aparece con datos; sin ellos hay otra cosa.
//
// View() has a sentence of its own for the case where the grid has been given
// nothing at all — before SetData, or a result with no columns. That is a
// different state from "a table with no rows", and the two sentences are
// different on purpose: "no data loaded" means nothing to show, "empty table" means
// there is a table and it is empty.
func TestGrid_NoDataLoadedIsItsOwnState(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(40)
	g.SetHeight(12)

	if got := ansi.Strip(g.View()); got != "  No data loaded" {
		t.Errorf("a grid with no data renders %q, want %q", got, "  No data loaded")
	}
	if g.HasData() {
		t.Error("HasData() is true for a grid that has never been given data")
	}
	if g.TotalRows() != 0 {
		t.Errorf("TotalRows() is %d for a grid with no data", g.TotalRows())
	}
	if g.SelectedRow() != nil {
		t.Error("SelectedRow() returned a row for a grid with no data")
	}

	// A result with columns but no rows has data, and is not this state.
	empty := newGrid(t, 0, 2, 40, 12, 100)
	if !empty.HasData() {
		t.Error("HasData() is false for a result that has columns")
	}
	if got := ansi.Strip(empty.View()); strings.Contains(got, "No data loaded") {
		t.Errorf("a grid with columns but no rows renders %q, which is the no-data sentence", got)
	}

	// And no columns is the no-data state again, even with rows present,
	// because there is nothing to draw a row of.
	g.SetData(&postgres.QueryResult{Rows: [][]interface{}{{"x"}}}, "public", "t")
	if g.HasData() {
		t.Error("HasData() is true for a result with no columns")
	}
}

// Scenario: Solo el estado del borrador cambia la fila; los datos no.
//
// A row's rendered bytes are a function of the DATA and of which draft state that
// row is in — selected, multi-selected, being edited, marked deleted, carrying an
// update. Not one of those changes the row's VALUES. So the stripped text of a
// data row is invariant across every draft state, and any change to it is a
// renderer picking the wrong data, which is a different bug entirely.
//
// And the raw bytes must differ between the states, or one of the branches is
// unreachable. That direction is the one that kills a branch-selection mutant.
func TestGrid_DraftStateChangesTheRowRenderingButNotItsValues(t *testing.T) {
	const height = 14
	base := func() *Grid {
		g := newGrid(t, 4, 2, 40, height, 100)
		g.cursorRow = 1
		return g
	}
	// rowLines returns the row block of the render, raw.
	rowLines := func(g *Grid) []string {
		out := resultOf(g.renderRecordsView())
		head := g.recordsHeaderOffset()
		return out[head+1 : head+1+g.contentHeight()]
	}

	plain := base()
	plainRows := rowLines(plain)

	// 1. The cursor row and the rows around it.
	if strings.Contains(ansi.Strip(plainRows[1]), marker(1, 0)) != true {
		t.Fatalf("the cursor row does not carry its own data:\n%s", strings.Join(plainRows, "\n"))
	}

	// 2. Multi-selection of the cursor row: same values, different bytes.
	multi := base()
	multi.selectedRows[1] = true
	multiRows := rowLines(multi)
	if ansi.Strip(multiRows[1]) != ansi.Strip(plainRows[1]) {
		t.Errorf("selecting a row changed its values: %q became %q", ansi.Strip(plainRows[1]), ansi.Strip(multiRows[1]))
	}
	if multiRows[1] == plainRows[1] {
		t.Error("a multi-selected cursor row renders identically to a plain one, so the cursor-on-selected branch is unreachable")
	}
	// And a NON-cursor row that is multi-selected renders differently from
	// one that is not.
	multiOff := base()
	multiOff.selectedRows[3] = true
	offRows := rowLines(multiOff)
	if ansi.Strip(offRows[3]) != ansi.Strip(plainRows[3]) {
		t.Errorf("selecting row 3 changed its values: %q became %q", ansi.Strip(plainRows[3]), ansi.Strip(offRows[3]))
	}
	if offRows[3] == plainRows[3] {
		t.Error("a multi-selected row off the cursor renders identically to a plain one")
	}

	// 3. A pending update: same values, different bytes.
	upd := base()
	upd.pendingUpdates = []PendingUpdate{{RowIdx: 2, ColIdx: 1, OldValue: "r2c1", NewValue: "changed"}}
	updRows := rowLines(upd)
	if ansi.Strip(updRows[2]) != ansi.Strip(plainRows[2]) {
		t.Errorf("a pending update changed the drawn values: %q became %q", ansi.Strip(plainRows[2]), ansi.Strip(updRows[2]))
	}
	if updRows[2] == plainRows[2] {
		t.Error("a row with a pending update renders identically to a plain one")
	}
	// And a row with NO update is unaffected by another row having one,
	// which is what the per-column map is for.
	if updRows[0] != plainRows[0] {
		t.Error("a pending update on row 2 changed the rendering of row 0")
	}

	// 4. A pending delete: the row is still drawn, and still carries its
	// values. A delete is a draft, not an erasure — the user has to be able
	// to see what they are about to discard.
	del := base()
	del.pendingDeletes = []PendingDelete{{RowIdx: 2, Row: []interface{}{"r2c0", "r2c1"}}}
	delRows := rowLines(del)
	if !strings.Contains(ansi.Strip(delRows[2]), marker(2, 0)) {
		t.Errorf("a row marked for deletion stopped carrying its data: %q", ansi.Strip(delRows[2]))
	}
	if delRows[2] == plainRows[2] {
		t.Error("a row marked for deletion renders identically to a plain one")
	}
	if !strings.Contains(ansi.Strip(delRows[3]), marker(3, 0)) {
		t.Error("deleting row 2 disturbed row 3")
	}

	// 5. Editing: the edited cell shows the IN-PROGRESS value, which is the
	// one thing here that is supposed to differ.
	ed := base()
	ed.editing = true
	ed.editRow = 1
	ed.editCol = 1
	ed.editValue = "wxyz"
	ed.editCursor = len(ed.editValue)
	edRows := rowLines(ed)
	// The in-progress value is drawn INSIDE the cell, so it has to fit it:
	// the column is six cells, the renderer gives three to text and spends
	// the fourth on the cursor block, so four characters are shown as three
	// plus the cursor. The assertion is therefore on the PREFIX and, more
	// importantly, on the old value being GONE — which is what "being edited"
	// means. A longer fixture value would be measuring truncation instead.
	stripped := ansi.Strip(edRows[1])
	if !strings.Contains(stripped, "wxy") {
		t.Errorf("the edited row does not carry the in-progress value: %q", stripped)
	}
	if strings.Contains(stripped, marker(1, 1)) {
		t.Errorf("the edited cell still shows the committed value %q: %q", marker(1, 1), stripped)
	}
	if !strings.Contains(stripped, marker(1, 0)) {
		t.Errorf("editing column 1 disturbed column 0 of the same row: %q", stripped)
	}
	if ansi.Strip(edRows[2]) != ansi.Strip(plainRows[2]) {
		t.Errorf("editing row 1 changed row 2: %q became %q", ansi.Strip(plainRows[2]), ansi.Strip(edRows[2]))
	}
}

// Scenario: El borrador de borrado NO borra, y el de insercion NO inserta en los datos.
//
// The pending changes are drafts: the data set is untouched until a commit, and
// discarding puts it back exactly as it was. So a row marked deleted is still in
// g.data, and a pending insert is not in g.data either — it is in a separate list
// until it is committed. Anything else would mean the undo path had nothing to
// undo.
func TestGrid_DraftsNeverTouchTheLoadedData(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 100)
	before := len(g.data.Rows)

	if _, ok := g.startDelete(); !ok {
		t.Fatal("startDelete on a populated grid refused")
	}
	if len(g.data.Rows) != before {
		t.Errorf("a pending delete changed the loaded row count from %d to %d", before, len(g.data.Rows))
	}
	if !strings.Contains(ansi.Strip(g.renderRecordsView()), marker(0, 0)) {
		t.Error("the row marked for deletion vanished from the view")
	}

	g.DiscardAllDrafts()
	if g.HasDrafts() {
		t.Error("DiscardAllDrafts left drafts behind")
	}
	if len(g.data.Rows) != before {
		t.Errorf("discarding changed the loaded row count to %d", len(g.data.Rows))
	}
	if !strings.Contains(ansi.Strip(g.renderRecordsView()), marker(0, 0)) {
		t.Error("discarding a delete did not bring the row back")
	}
}

// Scenario: La linea de aviso dice QUE pendiente hay y CUANTOS, y solo existe si hay.
//
// Three mutually exclusive banners, in a priority order: a pending commit
// confirmation outranks a pending refresh outranks a pending discard outranks the
// plain draft count. They are exclusive because they are the same line of screen —
// a user pressing Ctrl+S twice needs one instruction, not two.
//
// The count is the point: "Ctrl+S to save" tells the user nothing about the blast
// radius of the save.
func TestGrid_TheBannerNamesWhatIsPendingAndHowMuch(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 14, 100)

	// The banner is the line ABOVE the header, and when there is none the
	// header is the first line — so probing line 0 would read the header as
	// if it were a banner. The banner is whatever sits above the header,
	// which is what recordsHeaderOffset says it is.
	bannerOf := func(grid *Grid) string {
		out := resultOf(grid.renderRecordsView())
		n := grid.recordsHeaderOffset()
		if n == 0 {
			return ""
		}
		return ansi.Strip(out[0])
	}
	banner := func() string { return bannerOf(g) }

	// All three kinds of draft at once, so the count has to add all three. A
	// grid with no pending INSERTS cannot tell a sum that forgets them from
	// one that has them, because adding nothing and subtracting nothing are
	// the same number — and the banner is where the blast radius is read.
	all := newGrid(t, 3, 2, 40, 14, 100)
	all.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
	all.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{"a", "b"}}}
	all.pendingRows = [][]interface{}{{"c", "d"}, {"e", "f"}}
	if got := all.DraftCount(); got != 4 {
		t.Errorf("DraftCount() with one update, one delete and two inserts is %d, want 4", got)
	}
	if got := bannerOf(all); !strings.Contains(got, "4 pending change(s)") {
		t.Errorf("the banner says %q, want it to count all four changes", got)
	}

	if banner() != "" {
		t.Errorf("a clean grid shows the banner %q, want none", banner())
	}

	// Plain drafts.
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
	g.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{"r1c0", "r1c1"}}}
	if got := banner(); !strings.Contains(got, "2 pending change(s)") || !strings.Contains(got, "Ctrl+S") {
		t.Errorf("the draft banner is %q, want it to count 2 and name the save key", got)
	}

	// A pending discard outranks it.
	g.discardPending = true
	if got := banner(); !strings.Contains(got, "Press D again to discard") {
		t.Errorf("with a discard armed the banner is %q", got)
	}
	if strings.Contains(banner(), "Ctrl+S") {
		t.Errorf("the discard banner also shows the save banner: %q", banner())
	}
	g.discardPending = false

	// A pending refresh outranks the drafts.
	g.refreshPending = true
	if got := banner(); !strings.Contains(got, "Press r again to refresh") {
		t.Errorf("with a refresh armed the banner is %q", got)
	}
	if strings.Contains(banner(), "Ctrl+S") {
		t.Errorf("the refresh banner also shows the save banner: %q", banner())
	}

	// And a commit confirmation outranks everything, because the user is one
	// keystroke from writing to the database.
	g.commitPending = true
	if got := banner(); !strings.Contains(got, "Press Ctrl+S again to confirm") {
		t.Errorf("with a commit armed the banner is %q", got)
	}
	if strings.Contains(banner(), "refresh") || strings.Contains(banner(), "discard") {
		t.Errorf("the commit banner shows another banner too: %q", banner())
	}

	// An armed commit with NO drafts still shows the line, because the flag
	// is what arms it and the flag is public.
	g.commitPending = true
	g.pendingUpdates = nil
	g.pendingDeletes = nil
	if got := banner(); !strings.Contains(got, "confirm 0 change") {
		t.Errorf("with an armed commit and no drafts the banner is %q; the flag alone has to be enough", got)
	}
}

// Scenario: El filtro de columna encuentra una columna, y solo la primera.
//
// The finder is a shortcut: type a fragment, press enter, and the cursor lands on
// the first column whose name contains it, case-insensitively. First, because a
// jump to the best of several is a guess the user cannot see; and case-
// insensitive, because nobody remembers whether they typed the column name or
// what they think it is.
func TestGrid_TheColumnFinderJumpsToTheFirstMatch(t *testing.T) {
	g := newGrid(t, 3, 4, 40, 14, 100)
	g.columns = []string{"id", "user_id", "created_at", "updated_at"}
	g.calculateWidths()

	jump := func(fragment string) int {
		g.filtering = true
		g.filter = ""
		for _, r := range fragment {
			g.handleFilterKey(typing(string(r)))
		}
		g.jumpToBestMatch()
		return g.cursorCol
	}

	if got := jump("cre"); got != 2 {
		t.Errorf("finding %q landed on column %d, want 2 (created_at)", "cre", got)
	}
	if got := jump("USER"); got != 1 {
		t.Errorf("finding %q landed on column %d, want 1: the search is case-insensitive", "USER", got)
	}
	// The FIRST of two matches: "at" is in created_at and updated_at.
	if got := jump("at"); got != 2 {
		t.Errorf("finding %q landed on column %d, want 2 (created_at is the first match)", "at", got)
	}
	// A fragment that matches nothing leaves the cursor alone.
	before := g.cursorCol
	if got := jump("zzzz"); got != before {
		t.Errorf("finding %q moved the cursor from %d to %d, want no move", "zzzz", before, got)
	}
	// An empty fragment does nothing at all, rather than jumping to 0.
	if got := jump(""); got != before {
		t.Errorf("an empty fragment moved the cursor to %d, want %d", got, before)
	}

	// The finder is a mode with its own keys: enter takes the jump and
	// leaves, escape leaves without jumping, backspace eats one character,
	// and any single character is appended.
	f := newGrid(t, 3, 3, 40, 14, 100)
	f.startColumnFind()
	if !f.IsFiltering() || f.FilterText() != "" {
		t.Fatalf("startColumnFind left filtering=%v text=%q", f.IsFiltering(), f.FilterText())
	}
	f.handleFilterKey(typing("a"))
	f.handleFilterKey(typing("b"))
	f.handleFilterKey(named(tea.KeyBackspace))
	if f.FilterText() != "a" {
		t.Errorf("after typing ab and one backspace the finder holds %q, want %q", f.FilterText(), "a")
	}
	// Backspace on an empty finder does not go negative.
	for range 5 {
		f.handleFilterKey(named(tea.KeyBackspace))
	}
	if f.FilterText() != "" {
		t.Errorf("backspacing an empty finder left %q", f.FilterText())
	}
	f.handleFilterKey(typing("c"))
	f.handleFilterKey(named(tea.KeyEnter))
	if f.IsFiltering() {
		t.Error("enter did not leave the finder")
	}
	if f.FilterText() != "" {
		t.Errorf("enter left the finder holding %q", f.FilterText())
	}

	// And escape discards.
	e := newGrid(t, 3, 3, 40, 14, 100)
	e.startColumnFind()
	e.handleFilterKey(typing("q"))
	e.handleFilterKey(named(tea.KeyEscape))
	if e.IsFiltering() || e.FilterText() != "" {
		t.Errorf("escape left filtering=%v text=%q", e.IsFiltering(), e.FilterText())
	}
}

// typing sends a single printable character into the grid's key path.
func typing(ch string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(ch[0]), Text: ch}
}

// named sends one of the named keys the grid reads as a word: the finder and the
// cell editor both switch on the key's String(), which for a special key is its
// name rather than a character.
func named(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// ---------------------------------------------------------------------------
// SQL generation: pure functions
// ---------------------------------------------------------------------------

// Scenario: Un valor se escribe como SU TIPO, y las comillas se duplican.
//
// This is the text a user reads before running anything against their database,
// so a wrong literal is not cosmetic: a string with an unescaped quote produces
// SQL that parses as something else. Each branch of the type switch is a
// different rendering, and nil is not the same as the four-character string
// "NULL".
func TestFormatSQLValue_RendersEachTypeAsItsOwnLiteral(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   interface{}
		want string
	}{
		{"nil is NULL, not the word in quotes", nil, "NULL"},
		{"a string is quoted", "hello", "'hello'"},
		// The escaping is the whole point of the branch: one quote in,
		// one quote out, doubled.
		{"an embedded quote is doubled", "O'Brien", "'O''Brien'"},
		{"two embedded quotes are both doubled", "a''b", "'a''''b'"},
		{"an empty string is two quotes", "", "''"},
		{"an int64 is bare", int64(42), "42"},
		{"a negative int64 keeps its sign", int64(-7), "-7"},
		{"a zero int64 is not empty", int64(0), "0"},
		{"a float64 is bare and short", 1.5, "1.5"},
		{"a whole float64 has no decimal point", float64(3), "3"},
		{"a large float64 keeps its digits", float64(1e21), "1e+21"},
		{"true is the keyword", true, "TRUE"},
		{"false is the keyword", false, "FALSE"},
		// Anything else falls through to a quoted default, which is what
		// keeps an unexpected type from producing bare SQL.
		{"an int falls through to the quoted default", 42, "'42'"},
		{"a time is quoted", "2020-01-01", "'2020-01-01'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSQLValue(tc.in); got != tc.want {
				t.Errorf("formatSQLValue(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Scenario: Lo tecleado se convierte al TIPO del valor original.
//
// The grid shows `r1c1` for an int64 column, so typing `2` has to stay an int64
// or the UPDATE writes a string into a numeric column and PostgreSQL rejects the
// whole transaction. The types are read off the ORIGINAL value, not off the text.
//
// The bool branch is the awkward one: PostgreSQL accepts true, t and 1, and
// nothing else, so all three of those have to be true and everything else has to
// be false. That asymmetry is worth pinning — a check that only tests "true" and
// "false" passes an implementation that compares against "1" alone.
func TestGrid_ParseEditValueUsesTheOriginalTypes(t *testing.T) {
	g := newGrid(t, 1, 2, 40, 10, 100)

	for _, tc := range []struct {
		name     string
		text     string
		original interface{}
		want     interface{}
	}{
		{"an int stays an int", "42", int64(0), int64(42)},
		{"a negative int parses", "-42", int64(0), int64(-42)},
		{"a zero int parses", "0", int64(0), int64(0)},
		// Unparseable text leaves the destination at its zero value
		// rather than failing: Sscanf's error is discarded.
		{"an unparseable int becomes zero", "abc", int64(7), int64(0)},
		{"a float stays a float", "1.5", float64(0), 1.5},
		{"a whole float parses", "3", float64(0), float64(3)},
		{"an unparseable float becomes zero", "zz", float64(9), float64(0)},
		// The three accepted spellings of true.
		{"true is true", "true", true, true},
		{"t is true", "t", true, true},
		{"1 is true", "1", true, true},
		// And everything else is false, including the near misses.
		{"false is false", "false", false, false},
		{"f is false", "f", false, false},
		{"0 is false", "0", false, false},
		{"TRUE upper case is false, because the check is exact", "TRUE", true, false},
		{"yes is false", "yes", false, false},
		{"empty is false", "", false, false},
		// A string column keeps exactly what was typed.
		{"a string column keeps the text", "anything at all", "x", "anything at all"},
		{"a nil column keeps the text", "42", nil, "42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := g.parseEditValue(tc.text, tc.original)
			if got != tc.want {
				t.Errorf("parseEditValue(%q, %#v) = %#v, want %#v", tc.text, tc.original, got, tc.want)
			}
		})
	}
}

// Scenario: Un DELETE se ancla en la clave primaria, o en la fila entera.
//
// The difference is the blast radius. With a primary key the statement says which
// row; without one it has to match every column, which fails outright the moment
// any column is NULL — `col = NULL` is never true. So the key path is not an
// optimisation, it is the only one that works for a nullable table.
//
// The argument order is the other half: the placeholders are numbered in the order
// the WHERE terms appear, and the args are pushed in that same order. A mismatch
// there is a statement that silently updates or deletes the wrong row.
func TestBuildDeleteQuery_KeysOnThePrimaryKeyWhenThereIsOne(t *testing.T) {
	cols := []string{"id", "tenant", "name", "note"}

	t.Run("with a primary key", func(t *testing.T) {
		row := []interface{}{int64(7), "acme", "Alice", nil}
		q, args := BuildDeleteQuery("public", "users", cols, row, []string{"id"})
		want := `DELETE FROM "public"."users" WHERE "id" = $1`
		if q != want {
			t.Errorf("query = %q, want %q", q, want)
		}
		if len(args) != 1 || args[0] != int64(7) {
			t.Errorf("args = %#v, want just the key value", args)
		}
		// The NULL note is not in the predicate, which is the whole point.
		if len(args) > 1 {
			t.Errorf("a keyed delete carries %d args, so it still matches on the nullable columns", len(args))
		}
	})

	t.Run("with a composite key", func(t *testing.T) {
		row := []interface{}{int64(7), "acme", "Alice", "note"}
		q, args := BuildDeleteQuery("public", "users", cols, row, []string{"id", "tenant"})
		want := `DELETE FROM "public"."users" WHERE "id" = $1 AND "tenant" = $2`
		if q != want {
			t.Errorf("query = %q, want %q", q, want)
		}
		// The order of the args has to be the order of the predicates:
		// $1 is `id` and $2 is `tenant`, and the values come from the
		// named columns.
		if len(args) != 2 || args[0] != int64(7) || args[1] != "acme" {
			t.Errorf("args = %#v, want [7 acme] in predicate order", args)
		}
	})

	t.Run("without a key, every column is in the predicate", func(t *testing.T) {
		row := []interface{}{int64(7), "acme", "Alice", nil}
		q, args := BuildDeleteQuery("public", "users", cols, row, nil)
		want := `DELETE FROM "public"."users" WHERE "id" = $1 AND "tenant" = $2 AND "name" = $3 AND "note" = $4`
		if q != want {
			t.Errorf("query = %q, want %q", q, want)
		}
		if len(args) != 4 {
			t.Errorf("args = %#v, want one per column", args)
		}
	})

	t.Run("the key columns are looked up by NAME, not position", func(t *testing.T) {
		// The key here is the THIRD column. Reading it by position would
		// take the wrong value and delete the wrong row.
		row := []interface{}{int64(7), "acme", "Alice", nil}
		q, args := BuildDeleteQuery("public", "users", cols, row, []string{"name"})
		if q != `DELETE FROM "public"."users" WHERE "name" = $1` {
			t.Errorf("query = %q", q)
		}
		if len(args) != 1 || args[0] != "Alice" {
			t.Errorf("args = %#v, want the value of the named column", args)
		}
	})
}

// Scenario: Las columnas de clave primaria se SACAN de las restricciones.
//
// Three filters in one function, and all three are silent when wrong: the
// constraint TYPE, the composite split, and the requirement that the name is a
// column of this result at all. A constraint naming a column the query did not
// return would produce a DELETE referencing a column that does not exist.
//
// The order is the constraint order, not sorted and not reversed, because the
// WHERE has to match the args.
func TestGrid_PrimaryKeyColumnsComeFromTheConstraints(t *testing.T) {
	g := newGrid(t, 1, 4, 40, 10, 100)
	g.columns = []string{"a", "b", "c", "d"}

	g.constraintsData = []postgres.ConstraintInfo{
		{Type: "PRIMARY KEY", Columns: "a"},
		{Type: "UNIQUE", Columns: "b"},
		{Type: "FOREIGN KEY", Columns: "c"},
	}
	if got := g.PrimaryKeyColumns(); len(got) != 1 || got[0] != "a" {
		t.Errorf("with one key constraint the key columns are %v, want [a]", got)
	}

	// A composite key contributes every column, trimmed, in order.
	g.constraintsData = []postgres.ConstraintInfo{
		{Type: "PRIMARY KEY", Columns: " a , c "},
	}
	if got := g.PrimaryKeyColumns(); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("a composite key gave %v, want [a c]", got)
	}

	// A name that is not a column of this result is dropped.
	g.constraintsData = []postgres.ConstraintInfo{
		{Type: "PRIMARY KEY", Columns: "a, not_here, c"},
	}
	if got := g.PrimaryKeyColumns(); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("an unknown column in the key gave %v, want it dropped and [a c] left", got)
	}

	// Nothing at all.
	g.constraintsData = nil
	if got := g.PrimaryKeyColumns(); len(got) != 0 {
		t.Errorf("with no constraints the key columns are %v, want none", got)
	}
}

// Scenario: Una lista de columnas se parte por comas, sin huecos.
//
// The column list comes from information_schema and has its own formatting, so
// the splitter has to tolerate the spaces and the trailing comma that shows up
// there. An empty piece would otherwise become a constraint column named "".
func TestSplitColumns_ToleratesTheFormattingOfAColumnList(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"one column", "id", []string{"id"}},
		{"two columns", "a,b", []string{"a", "b"}},
		{"spaces around the separator", "a , b", []string{"a", "b"}},
		{"a trailing separator is dropped", "a,b,", []string{"a", "b"}},
		{"a leading separator is dropped", ",a,b", []string{"a", "b"}},
		// An empty piece would become a constraint column named "",
		// which matches no column and silently drops out of the key.
		{"nothing but separators", ",,", nil},
		{"an empty list", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitColumns(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitColumns(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("splitColumns(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The cell editor
// ---------------------------------------------------------------------------

// editing puts the grid into cell-editing mode on the cell the cursor is on.
func editing(t *testing.T, row, col int) *Grid {
	t.Helper()
	g := newGrid(t, 6, 3, 60, 16, 100)
	g.cursorRow = row
	g.cursorCol = col
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatalf("edit_cell was not handled")
	}
	if !g.IsEditing() {
		t.Fatal("edit_cell did not start editing")
	}
	return g
}

// typeIn sends printable text through the real key path.
func typeIn(t *testing.T, g *Grid, text string) {
	t.Helper()
	for _, r := range text {
		if _, handled := g.Update(tea.KeyPressMsg{Code: r, Text: string(r)}); !handled {
			t.Fatalf("typing %q into the cell editor was not handled", string(r))
		}
	}
}

// tap sends one named key through the real key path.
func tap(t *testing.T, g *Grid, code rune) bool {
	t.Helper()
	_, handled := g.Update(tea.KeyPressMsg{Code: code})
	return handled
}

// Scenario: El cursor de edicion NUNCA sale del valor.
//
// A cursor outside the value is not a rendering nit: the editor slices the value
// at the cursor to insert and to delete, so a cursor past the end would panic on
// the next keystroke. Every one of the four ways to move it is therefore clamped,
// and the clamps are checked by pressing the key twice rather than once — a single
// press cannot tell a clamp from a move.
func TestGrid_TheEditCursorNeverLeavesTheValue(t *testing.T) {
	for _, initial := range []string{"", "a", "abc"} {
		t.Run(fmt.Sprintf("starting from %q", initial), func(t *testing.T) {
			g := editing(t, 1, 1)
			g.editValue = initial
			g.editStartValue = initial
			g.editCursor = len(initial)

			tap(t, g, tea.KeyHome)
			tap(t, g, tea.KeyLeft)
			if g.editCursor != 0 {
				t.Fatalf("left at the start moved the cursor to %d", g.editCursor)
			}
			// Right walks to the end and stops there.
			for range len(initial) + 3 {
				tap(t, g, tea.KeyRight)
				if g.editCursor > len(g.editValue) {
					t.Fatalf("right moved the cursor to %d in a %d-character value", g.editCursor, len(g.editValue))
				}
			}
			if g.editCursor != len(initial) {
				t.Errorf("the cursor stopped at %d, want the end of a %d-character value (%d)", g.editCursor, len(initial), len(initial))
			}
			// End and home are absolute, and home is idempotent.
			tap(t, g, tea.KeyHome)
			tap(t, g, tea.KeyHome)
			if g.editCursor != 0 {
				t.Errorf("home twice left the cursor at %d", g.editCursor)
			}
			tap(t, g, tea.KeyEnd)
			tap(t, g, tea.KeyEnd)
			if g.editCursor != len(initial) {
				t.Errorf("end twice left the cursor at %d, want %d", g.editCursor, len(initial))
			}
			// And the ctrl spellings are the same keys.
			g.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
			if g.editCursor != 0 {
				t.Errorf("ctrl+a left the cursor at %d", g.editCursor)
			}
			g.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			if g.editCursor != len(initial) {
				t.Errorf("ctrl+e left the cursor at %d, want %d", g.editCursor, len(initial))
			}
		})
	}
}

// Scenario: Escribir INSERTA en el cursor, y el cursor avanza DETRAS de lo escrito.
//
// Insertion is at the cursor rather than at the end, which is what makes the
// editor usable at all — a user fixing a typo in the middle of a value must be
// able to. And the cursor advances by the length of what was typed, not by one, or
// multi-key input would land backwards.
func TestGrid_TypingInsertsAtTheCursorAndAdvancesPastIt(t *testing.T) {
	g := editing(t, 1, 1)
	g.editValue = "abcd"
	g.editStartValue = "abcd"
	g.editCursor = 2

	typeIn(t, g, "XY")

	if g.editValue != "abXYcd" {
		t.Errorf("after typing XY at offset 2 the value is %q, want %q", g.editValue, "abXYcd")
	}
	if g.editCursor != 4 {
		t.Errorf("the cursor is at %d, want 4 — just past what was typed", g.editCursor)
	}
	// And typing continues from where the cursor ended up, not at the end
	// of the original: "abXYcd" with the cursor at 4 takes a Z between the
	// Y and the c.
	typeIn(t, g, "Z")
	if g.editValue != "abXYZcd" {
		t.Errorf("typing at the end gave %q, want %q", g.editValue, "abXYZcd")
	}
}

// Scenario: Borrar quita UN CARACTER, no un byte.
//
// backspace deletes the character before the cursor and delete the one at it, and
// both are character operations. A byte operation on a multi-byte character — an
// accented letter, an emoji, a CJK glyph — leaves the value holding invalid UTF-8,
// and that value is the question text or the cell content the user is about to
// commit. It also leaves the cursor mid-character, so the next insertion lands
// inside the broken sequence.
//
// This is asserted on a multi-byte fixture on purpose. The same trap was found and
// fixed in the ASK pane's editor earlier, and the shape is identical: a byte index
// used as a character index.
func TestGrid_BackspaceAndDeleteRemoveOneCharacter(t *testing.T) {
	g := editing(t, 1, 1)
	// "á" is two bytes, "日" is three. A byte-wise delete would leave a
	// fragment that is not valid UTF-8.
	start := "aá日b"
	g.editValue = start
	g.editStartValue = start
	// The editor's cursor is a BYTE offset — that is what lets insertion be a
	// two-slice concatenation. Setting it to the CHARACTER count would put
	// the cursor in the middle of a multi-byte character, which is the very
	// defect the rest of this test is about, and the fixture would be the
	// bug rather than the detector.
	g.editCursor = len(start)

	tap(t, g, tea.KeyBackspace)
	if g.editValue != "aá日" {
		t.Errorf("backspace at the end gave %q, want %q", g.editValue, "aá日")
	}
	if !utf8.ValidString(g.editValue) {
		t.Errorf("backspace left invalid UTF-8: %q", g.editValue)
	}
	if got := utf8.RuneCountInString(g.editValue); got != 3 {
		t.Errorf("the value is %d characters, want 3: one character went, not one byte", got)
	}

	// Delete at the cursor removes the character AT it.
	g.editCursor = 1
	tap(t, g, tea.KeyDelete)
	if g.editValue != "a日" {
		t.Errorf("delete at offset 1 gave %q, want %q", g.editValue, "a日")
	}
	if !utf8.ValidString(g.editValue) {
		t.Errorf("delete left invalid UTF-8: %q", g.editValue)
	}

	// Both refuse at the ends rather than producing a negative index.
	tap(t, g, tea.KeyHome)
	tap(t, g, tea.KeyBackspace)
	if g.editValue != "a日" {
		t.Errorf("backspace at the start changed the value to %q", g.editValue)
	}
	tap(t, g, tea.KeyEnd)
	tap(t, g, tea.KeyDelete)
	if g.editValue != "a日" {
		t.Errorf("delete at the end changed the value to %q", g.editValue)
	}
}

// Scenario: Las flechas de arriba y abajo NO mueven la fila mientras se edita.
//
// They are consumed rather than ignored so they cannot scroll the grid out from
// under the cell being edited. The distinction matters: "not handled" would let
// them fall through to navigation.
func TestGrid_UpAndDownAreSwallowedWhileEditing(t *testing.T) {
	g := editing(t, 2, 1)
	before := fmt.Sprintf("%d/%d", g.scrollRow, g.cursorRow)

	for range 4 {
		if !tap(t, g, tea.KeyUp) {
			t.Fatal("up was not handled while editing")
		}
		if !tap(t, g, tea.KeyDown) {
			t.Fatal("down was not handled while editing")
		}
	}
	if got := fmt.Sprintf("%d/%d", g.scrollRow, g.cursorRow); got != before {
		t.Errorf("up and down moved the cursor from %s to %s while editing", before, got)
	}
}

// Scenario: Un atajo de teclado sin texto NO se escribe en la celda.
//
// Everything with a modifier is either a named editing key or must not reach the
// value at all. A shortcut that typed its own name into the cell would be silent
// data corruption — the user's next commit would contain "ctrl+w".
func TestGrid_AModifiedKeyNeverReachesTheCellValue(t *testing.T) {
	g := editing(t, 1, 1)
	g.editValue = "keep"
	g.editStartValue = "keep"
	g.editCursor = 4

	// A modifier with no text and a code that is not an editing key.
	if _, handled := g.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+w was reported as handled, so it fell through to the grid instead of being ignored")
	}
	if g.editValue != "keep" {
		t.Errorf("ctrl+w changed the cell value to %q", g.editValue)
	}
	// A key with no text and no modifier falls through instead: the editor
	// has nothing to do with it, so the grid above gets its chance. What it
	// must never do is write anything.
	if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyF5}); handled {
		t.Error("a key with no text was reported as handled by the editor")
	}
	if g.editValue != "keep" {
		t.Errorf("a key with no text changed the cell value to %q", g.editValue)
	}
}

// Scenario: Tab y enter GUARDAN y saltan a la siguiente columna, dando la vuelta.
//
// Both keys mean "save this cell and move on", and both wrap: the last column
// leads back to the first. The value loaded into the editor is the NEXT column's
// committed value, which is why a grid showing `r0c0` then `r0c1` lands on
// `r0c1` — if it loaded the edited value instead, moving right would appear to do
// nothing.
//
// And the guard is real: pressing tab has to produce a pending update, because
// that update is the only record of the edit.
func TestGrid_TabAndEnterSaveTheCellAndAdvanceTheColumn(t *testing.T) {
	for _, key := range []struct {
		name string
		code rune
	}{
		{"tab", tea.KeyTab},
		{"enter", tea.KeyEnter},
	} {
		t.Run(key.name, func(t *testing.T) {
			// Start on the LAST column of a three-column grid, so one
			// press wraps to the first. Starting in the middle could not
			// tell a wrap from a plain advance.
			g := editing(t, 1, 2)
			typeIn(t, g, "Z")

			if !tap(t, g, key.code) {
				t.Fatalf("%s was not handled while editing", key.name)
			}
			// The EDITOR moves, not the grid cursor: what the next
			// keystroke applies to is editCol, and the grid cursor only
			// follows when editing ends.
			if g.editCol != 0 {
				t.Errorf("after %s the editor is on column %d, want 0 — the last column wraps to the first", key.name, g.editCol)
			}
			if g.CursorCol() != 2 {
				t.Errorf("after %s the grid cursor moved to column %d; it should not move until editing ends", key.name, g.CursorCol())
			}
			if !g.IsEditing() {
				t.Errorf("after %s the grid left edit mode", key.name)
			}
			// The edit was recorded, and the value loaded next is the
			// next column's own, not the one just typed.
			if len(g.pendingUpdates) != 1 {
				t.Fatalf("%s recorded %d updates, want 1", key.name, len(g.pendingUpdates))
			}
			if got := g.pendingUpdates[0]; got.ColIdx != 2 || got.RowIdx != 1 {
				t.Errorf("the pending update is at row %d column %d, want row 1 column 2 — the column that was edited, not the one now shown", got.RowIdx, got.ColIdx)
			}
			if g.editValue != marker(1, 0) {
				t.Errorf("the editor now holds %q, want the next column's value %q", g.editValue, marker(1, 0))
			}
			if g.editCursor != len(g.editValue) {
				t.Errorf("the cursor is at %d, want the end of the loaded value (%d)", g.editCursor, len(g.editValue))
			}

			// And on from the first it advances to the second, which
			// is what a wrap rather than a pin looks like.
			tap(t, g, key.code)
			if g.editCol != 1 {
				t.Errorf("after wrapping the editor is on column %d, want 1", g.editCol)
			}
		})
	}
}

// Scenario: Escape GUARDA lo editado, y no guarda lo intacto.
//
// This is the one genuinely surprising rule in the editor, and it is worth stating
// plainly: esc COMMITS a changed value and CANCELS an unchanged one. So "escape to
// back out" does not discard typing — the user has to retype the original value to
// throw it away. It is a deliberate choice (the data is local, so committing a
// draft is cheap and losing an edit is not), and the test says so, because a
// reader would otherwise assume the opposite.
//
// The nil-value branch is also here: an unchanged pending insert is REMOVED, since
// an insert row with no value typed into it is not a row the user wants.
func TestGrid_EscapeCommitsAChangeAndCancelsAnUnchangedValue(t *testing.T) {
	t.Run("a changed value is committed", func(t *testing.T) {
		g := editing(t, 1, 1)
		typeIn(t, g, "Q")
		tap(t, g, tea.KeyEscape)

		if g.IsEditing() {
			t.Error("escape left the grid editing")
		}
		if len(g.pendingUpdates) != 1 {
			t.Fatalf("escape recorded %d updates, want 1: a changed value is committed, not discarded", len(g.pendingUpdates))
		}
		if g.editValue != "" {
			t.Errorf("escape left the editor holding %q", g.editValue)
		}
	})

	t.Run("an unchanged value is not", func(t *testing.T) {
		g := editing(t, 1, 1)
		tap(t, g, tea.KeyEscape)

		if g.IsEditing() {
			t.Error("escape left the grid editing")
		}
		if len(g.pendingUpdates) != 0 {
			t.Errorf("escape recorded %d updates for an unchanged value, want none", len(g.pendingUpdates))
		}
	})

	t.Run("an unchanged pending insert is removed", func(t *testing.T) {
		g := newGrid(t, 3, 2, 60, 16, 100)
		if _, ok := g.startInsertRow(); !ok {
			t.Fatal("startInsertRow refused")
		}
		if g.PendingCount() != 1 {
			t.Fatalf("the insert added %d pending rows, want 1", g.PendingCount())
		}
		tap(t, g, tea.KeyEscape)
		if g.PendingCount() != 0 {
			t.Errorf("escaping an untouched insert left %d pending rows", g.PendingCount())
		}
		// Nil rather than an empty slice: both report zero rows, and only
		// one of them makes a later `len(...) == 0` check mean "nothing
		// is pending".
		if g.pendingRows != nil {
			t.Errorf("after removing the last pending insert the list is %#v, want nil", g.pendingRows)
		}
		if g.IsInserting() {
			t.Error("escaping an insert left the grid in insert mode")
		}
	})

	t.Run("a changed pending insert is kept", func(t *testing.T) {
		g := newGrid(t, 3, 2, 60, 16, 100)
		g.startInsertRow()
		typeIn(t, g, "V")
		tap(t, g, tea.KeyEscape)
		if g.PendingCount() != 1 {
			t.Errorf("escaping a filled-in insert left %d pending rows", g.PendingCount())
		}
		if got := g.pendingRows[0][0]; got != "V" {
			t.Errorf("the pending row holds %#v in column 0, want %q", got, "V")
		}
	})
}

// Scenario: La columna editada se ENSANCHA para que quepa, y vuelve a su ancho.
//
// Editing a cell whose value is wider than the column is useless if the column
// does not grow: the user types into a window that shows three characters of it.
// So the edited column widens to fit the column name or the value, whichever is
// wider, and comes back to its original width when the edit ends.
//
// The cap matters too: a pathological value must not eat the whole pane, so the
// expansion stops at eighty cells.
func TestGrid_EditingWidensTheColumnAndRestoresIt(t *testing.T) {
	g := newGrid(t, 3, 2, 200, 16, 100)
	g.cursorCol = 1 // the editor starts on the cursor's column, so aim it
	base := g.widths[1]
	before := append([]int(nil), g.widths...)

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	// Entering the editor already widens: the rule is "as wide as the column
	// NAME or the VALUE, plus four", and this cell's value is wider than
	// its name.
	wantEntering := len(marker(0, 1)) + 4
	if g.widths[1] != wantEntering {
		t.Errorf("entering the editor left the column at %d cells, want %d (the value plus four)", g.widths[1], wantEntering)
	}

	// Typing follows the same rule, measured against the value as it now is.
	typeIn(t, g, strings.Repeat("q", 30))
	want := len(g.editValue) + 4
	if g.widths[1] != want {
		t.Errorf("the edited column is %d cells, want %d (the value plus four)", g.widths[1], want)
	}

	// The cap is eighty, and it applies once the value plus four passes it.
	typeIn(t, g, strings.Repeat("q", 100))
	if g.widths[1] != 80 {
		t.Errorf("a %d-character value left the column at %d cells, want the cap of 80", len(g.editValue), g.widths[1])
	}

	// Leaving restores the width it had before.
	g.restoreEditWidth()
	if g.widths[1] != base {
		t.Errorf("leaving the editor left the column at %d cells, want the original %d", g.widths[1], base)
	}
	if g.editOrigWidth != 0 {
		t.Errorf("editOrigWidth is %d after restoring, want 0: restoring twice would double-apply it", g.editOrigWidth)
	}

	// Restoring with nothing to restore is a no-op rather than a panic or a
	// zero-width column.
	g.restoreEditWidth()
	// Every column, not just the edited one: a restore that fires with
	// nothing saved would zero the column it looks at, and checking only
	// the edited column would look at the wrong index.
	for i, w := range g.widths {
		if w != before[i] {
			t.Errorf("a second restore changed column %d from %d to %d cells", i, before[i], w)
		}
	}

	// Restoring the FIRST column works too. The guard on the column index
	// is `>= 0`, and zero is the one value a comparison written the other
	// way round would exclude — and the first column is the one a user
	// reaches first.
	first := newGrid(t, 3, 2, 200, 16, 100)
	first.cursorCol = 0
	// The current width differs from the saved one, or restoring would be
	// a no-op and would prove nothing.
	saved := first.widths[0]
	first.editOrigWidth = saved
	first.widths[0] = 60
	first.editCol = 0
	first.restoreEditWidth()
	if first.widths[0] != saved {
		t.Errorf("restoring the first column left it at %d cells, want the saved %d", first.widths[0], saved)
	}
	if first.editOrigWidth != 0 {
		t.Errorf("restoring the first column left the saved width at %d", first.editOrigWidth)
	}

	// A column index outside the width list is refused by both directions.
	g.editCol = -1
	g.expandEditCol()
	g.editCol = len(g.widths)
	g.expandEditCol()
	g.editOrigWidth = 5
	g.editCol = len(g.widths)
	g.restoreEditWidth()
	if g.editOrigWidth != 5 {
		t.Errorf("restoring with an out-of-range column cleared the saved width anyway")
	}
}

// Scenario: El ancho ORIGINAL se toma UNA vez, aunque se teclee durante toda la edicion.
//
// REGRESSION. expandEditCol used to save the column's CURRENT width into
// editOrigWidth every time it ran, and it runs once per typed character from
// handleEditKey's insertion branch. So the "original" tracked the previous
// expansion: typing 20 characters into a 16-cell column left it permanently at 24,
// for the rest of the session, because restoring put back the last expanded width
// rather than the first.
//
// It is worth a test rather than a comment because the trigger is a COUNT — one
// keystroke works, two do not — so any test that types once passes against the
// broken code. This one types in three bursts, which is what makes the difference
// visible.
func TestGrid_TheEditColumnRemembersItsWidthAcrossManyKeystrokes(t *testing.T) {
	g := newGrid(t, 2, 2, 200, 16, 100)
	g.cursorCol = 1
	base := g.widths[1]

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	// Three bursts, so the saved original gets several chances to be
	// overwritten by an expansion.
	for _, n := range []int{8, 12, 20} {
		typeIn(t, g, strings.Repeat("q", n))
		if g.editOrigWidth != base {
			t.Fatalf("after typing %d characters editOrigWidth is %d, want the original %d", n, g.editOrigWidth, base)
		}
	}
	if g.widths[1] <= base {
		t.Fatalf("the column never widened: %d cells, was %d", g.widths[1], base)
	}

	g.restoreEditWidth()
	if g.widths[1] != base {
		t.Errorf("after typing 20 characters the column rests at %d cells, want the original %d", g.widths[1], base)
	}
}

// ---------------------------------------------------------------------------
// Drafts: the pending insert, update and delete
// ---------------------------------------------------------------------------

// Scenario: Una edicion sobre otra CONSOLIDA: una entrada por celda, con la fila
// original.
//
// Three rules, and each of them is a correctness rule rather than a tidy-up:
// editing the same cell twice must not leave two pending updates (the second would
// carry a stale OldValue and the discard would restore the wrong thing); a second
// edit to the SAME ROW must reuse the first edit's original row snapshot (otherwise
// the WHERE of the second statement matches nothing, because the first statement
// already changed that column); and the snapshot has to be taken before any write.
func TestGrid_EditingTheSameCellTwiceConsolidates(t *testing.T) {
	g := newGrid(t, 4, 3, 60, 16, 100)
	g.columns = []string{"id", "name", "age"}
	g.data.Rows[1] = []interface{}{"1", "Alice", "30"}
	// The editor opens on the CURSOR's cell, so aim it at row 1 column 1 —
	// "name", the one with a readable original value.
	g.cursorRow = 1
	g.cursorCol = 1

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	typeIn(t, g, "Ann")
	tap(t, g, tea.KeyEscape)

	if len(g.pendingUpdates) != 1 {
		t.Fatalf("after one edit there are %d pending updates, want 1", len(g.pendingUpdates))
	}
	if g.pendingUpdates[0].OldValue != "Alice" {
		t.Errorf("the snapshot has OldValue %#v, want the value as it was", g.pendingUpdates[0].OldValue)
	}
	first := g.pendingUpdates[0]

	// Edit the same cell again.
	g.cursorCol = 1
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("the second edit_cell was not handled")
	}
	typeIn(t, g, "Anna")
	tap(t, g, tea.KeyEscape)

	if len(g.pendingUpdates) != 1 {
		t.Fatalf("after editing the same cell twice there are %d pending updates, want 1: a second entry would carry a stale OldValue", len(g.pendingUpdates))
	}
	// The second edit opens on the cell's CURRENT value, which is what the
	// first edit already wrote there, and appends to it. So the consolidated
	// value is the whole history, not just the second keystroke burst.
	if got := g.pendingUpdates[0].NewValue; got != "AliceAnnAnna" {
		t.Errorf("the consolidated update has NewValue %#v, want %q", got, "AliceAnnAnna")
	}
	if g.pendingUpdates[0].OldValue != "Alice" {
		t.Errorf("the consolidated update's OldValue is %#v, want the ORIGINAL %q", g.pendingUpdates[0].OldValue, "Alice")
	}
	// The snapshot is the same slice, not a fresh one — a second edit to the
	// same row must reuse it or the WHERE stops matching.
	// The single entry is the ORIGINAL one, carrying the new value — not a
	// fresh entry, which would have the current value as its OldValue.
	if g.pendingUpdates[0].OldValue != first.OldValue {
		t.Errorf("the entry was rebuilt: OldValue went from %#v to %#v", first.OldValue, g.pendingUpdates[0].OldValue)
	}

	// A different column of the same row ADDS an entry and reuses the
	// snapshot, which is the case the single-statement UPDATE depends on.
	g.cursorCol = 2
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("the third edit_cell was not handled")
	}
	typeIn(t, g, "X")
	tap(t, g, tea.KeyEscape)

	if len(g.pendingUpdates) != 2 {
		t.Fatalf("editing a second column of the same row left %d updates, want 2", len(g.pendingUpdates))
	}
	firstRow := g.pendingUpdates[0].OldRow
	secondRow := g.pendingUpdates[1].OldRow
	if len(firstRow) == 0 || len(secondRow) == 0 {
		t.Fatalf("a pending update has no row snapshot: %v", g.pendingUpdates)
	}
	if firstRow[1] != "Alice" || secondRow[1] != "Alice" {
		t.Errorf("the two snapshots of the same row differ in the edited column: %#v and %#v", firstRow, secondRow)
	}
	// And it is the same snapshot, not a second copy: a copy taken after the
	// first edit would carry the edited value in this column. Slices cannot
	// be compared, so the check is on the backing array.
	if &firstRow[0] != &secondRow[0] {
		t.Error("the second edit to the row took a fresh snapshot instead of reusing the first")
	}
	// The whole row becomes ONE UPDATE, which is the documented reason for
	// grouping: one statement per column would reuse the same WHERE.
	groups := g.groupUpdatesByRow()
	if len(groups) != 1 {
		t.Fatalf("two edits to one row produced %d groups, want 1", len(groups))
	}
	if len(groups[0].updates) != 2 {
		t.Errorf("the group holds %d updates, want both", len(groups[0].updates))
	}
	if got := g.whereRowFor(groups[0]); got[1] != "Alice" {
		t.Errorf("the group's WHERE row has %q in the edited column, want the original", got[1])
	}
}

// Scenario: La fila ORIGINAL se busca una vez, y se reutiliza.
//
// whereRowFor is the WHERE of every statement built for a row, so it has to be
// the row as it was BEFORE any of the edits. It is found by scanning the group's
// updates for the first one that carries a snapshot, because only the first edit
// of a row can have taken one — the rest reuse it.
func TestGrid_TheWhereRowIsTheFirstSnapshotOrTheCurrentRow(t *testing.T) {
	g := newGrid(t, 2, 2, 60, 14, 100)
	g.data.Rows[0] = []interface{}{"0", "zero"}

	// With a snapshot, it wins over the current row even when the current row
	// has since been written to.
	snapshot := []interface{}{"0", "original"}
	group := rowUpdateGroup{rowIdx: 0, updates: []PendingUpdate{
		{ColIdx: 0, OldRow: snapshot},
	}}
	if got := g.whereRowFor(group); got[1] != "original" {
		t.Errorf("with a snapshot the WHERE row is %v, want the snapshot", got)
	}

	// With none, it falls back to the current row rather than producing an
	// empty predicate.
	noSnapshot := rowUpdateGroup{rowIdx: 0, updates: []PendingUpdate{{ColIdx: 0}}}
	if got := g.whereRowFor(noSnapshot); got[1] != "zero" {
		t.Errorf("with no snapshot the WHERE row is %v, want the current row", got)
	}
	if got := g.whereRowFor(rowUpdateGroup{}); got == nil {
		t.Error("with no updates at all the WHERE row is nil")
	}
}

// Scenario: Las actualizaciones de una fila FUERA del rango se ignoran.
//
// A pending update naming a row the result no longer has cannot be turned into a
// statement: there is nothing to UPDATE. The row can go missing between an edit
// and a save, because a refresh or a re-query replaces the data. Skipping is the
// only safe thing, and it is silent on purpose — there is nothing to tell the
// user that they cannot do anything about.
func TestGrid_UpdatesForRowsThatNoLongerExistAreDropped(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 14, 100)

	for _, tc := range []struct {
		name string
		row  int
	}{
		{"a row past the end", 9},
		{"a negative row", -1},
		{"the first row", 0},
		{"the last row", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g.pendingUpdates = []PendingUpdate{{RowIdx: tc.row, ColIdx: 0, OldValue: "x", NewValue: "y"}}
			groups := g.groupUpdatesByRow()
			if tc.row < 0 || tc.row >= 3 {
				if len(groups) != 0 {
					t.Errorf("an update for row %d produced %d groups, want none", tc.row, len(groups))
				}
				return
			}
			if len(groups) != 1 || groups[0].rowIdx != tc.row {
				t.Errorf("an update for row %d produced %v, want one group for that row", tc.row, groups)
			}
		})
	}
}

// Scenario: Descartar devuelve los valores, y SOLO a las filas que siguen ahi.
//
// Discard is the undo of the whole draft set, and it writes back into the loaded
// data — so the index check is load-bearing. A pending update for a row that has
// since gone would otherwise write into whatever row now occupies that index, and
// discarding would silently corrupt a row the user never touched.
func TestGrid_DiscardRestoresTheOriginalValuesAndNothingElse(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 14, 100)
	g.data.Rows[0] = []interface{}{"0", "zero"}
	g.data.Rows[1] = []interface{}{"1", "one"}

	// The pending values are applied to the data first, because that is the
	// state discarding has to undo. Writing the ORIGINAL over a cell that
	// still holds the ORIGINAL proves nothing — which is exactly what an
	// earlier version of this fixture did, and it is why the guard for row
	// ZERO survived: the only row whose index a `>= 0` test could mistake.
	g.data.Rows[0][1] = "changed"
	g.data.Rows[1][1] = "changed"
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 1, OldValue: "zero", NewValue: "changed"},
		{RowIdx: 1, ColIdx: 1, OldValue: "one", NewValue: "changed"},
		// Out of range: must not be written anywhere.
		{RowIdx: 9, ColIdx: 0, OldValue: "nope", NewValue: "nope"},
	}
	g.pendingDeletes = []PendingDelete{{RowIdx: 0, Row: []interface{}{"0", "zero"}}}
	g.pendingRows = [][]interface{}{{"x", "y"}}
	g.selectedRows[2] = true

	g.DiscardAllDrafts()

	if got := g.data.Rows[0][1]; got != "zero" {
		t.Errorf("after discarding, row 0 column 1 is %#v, want the original", got)
	}
	if got := g.data.Rows[1][1]; got != "one" {
		t.Errorf("after discarding, row 1 column 1 is %#v, want the original", got)
	}
	// The out-of-range update wrote into nothing, so row 0's first column —
	// which is what it named — is untouched.
	if got := g.data.Rows[0][0]; got != "0" {
		t.Errorf("an out-of-range update wrote %q into row 0 column 0", got)
	}
	if g.HasDrafts() {
		t.Errorf("discarding left %d drafts behind", g.DraftCount())
	}
	if g.PendingCount() != 0 {
		t.Errorf("discarding left %d pending inserts", g.PendingCount())
	}
	if g.IsInserting() {
		t.Error("discarding left the grid in insert mode")
	}
	if g.SelectionCount() != 0 {
		t.Error("discarding left rows selected")
	}
}

// Scenario: Deshacer una fila devuelve SUS cambios, y deja las demas.
//
// Undo is per-row: the user has edited three rows and wants the second one back.
// So the filter is by row index, and it covers both an update and a delete of that
// row — one row, one undo — while every other row's drafts survive untouched.
//
// A pending insert is a different shape: there is nothing to restore, so the insert
// row is removed from the list entirely.
func TestGrid_UndoRevertsOneRowAndLeavesTheOthers(t *testing.T) {
	t.Run("updates and a delete on the cursor row", func(t *testing.T) {
		g := newGrid(t, 4, 2, 60, 14, 100)
		g.data.Rows = [][]interface{}{
			{"0", "zero"}, {"1", "one"}, {"2", "two"}, {"3", "three"},
		}
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: 0, ColIdx: 1, OldValue: "zero", NewValue: "changed0"},
			{RowIdx: 2, ColIdx: 1, OldValue: "two", NewValue: "changed2a"},
			{RowIdx: 2, ColIdx: 0, OldValue: "2", NewValue: "changed2b"},
		}
		g.pendingDeletes = []PendingDelete{{RowIdx: 0, Row: []interface{}{"0", "zero"}}, {RowIdx: 2, Row: []interface{}{"2", "two"}}}
		// The edits are applied to the data, because undo has to put
		// them back — see the note on the discard fixture.
		g.data.Rows[2][1] = "changed2a"
		g.data.Rows[2][0] = "changed2b"
		g.cursorRow = 2

		if got := g.UndoRowDrafts(); got != 3 {
			t.Errorf("undoing row 2 reported %d changes, want 3 (two updates and one delete)", got)
		}
		if got := g.data.Rows[2][1]; got != "two" {
			t.Errorf("row 2 column 1 is %#v after undo, want the original", got)
		}
		if got := g.data.Rows[2][0]; got != "2" {
			t.Errorf("row 2 column 0 is %#v after undo, want the original", got)
		}
		// Row 0 keeps everything.
		if len(g.pendingUpdates) != 1 {
			t.Errorf("undoing row 2 left %d pending updates, want only the 1 belonging to row 0", len(g.pendingUpdates))
		}
		if len(g.pendingDeletes) != 1 {
			t.Errorf("undoing row 2 left %d pending deletes, want the 1 belonging to row 0", len(g.pendingDeletes))
		} else if got := g.pendingDeletes[0].RowIdx; got != 0 {
			// WHICH delete survived matters: a filter that dropped the
			// other one leaves the same COUNT and the wrong row.
			t.Errorf("the surviving pending delete is for row %d, want row 0", got)
		}
		if g.data.Rows[0][1] != "zero" {
			t.Errorf("undoing row 2 also restored row 0")
		}
	})

	t.Run("a row with no drafts reports nothing", func(t *testing.T) {
		g := newGrid(t, 3, 2, 60, 14, 100)
		g.pendingUpdates = []PendingUpdate{{RowIdx: 1, ColIdx: 0, OldValue: "a", NewValue: "b"}}
		g.cursorRow = 0
		if got := g.UndoRowDrafts(); got != 0 {
			t.Errorf("undoing an untouched row reported %d changes, want 0", got)
		}
		if len(g.pendingUpdates) != 1 {
			t.Errorf("undoing an untouched row dropped %d updates", 1-len(g.pendingUpdates))
		}
	})

	t.Run("a pending insert is removed instead", func(t *testing.T) {
		g := newGrid(t, 3, 2, 60, 14, 100)
		g.pendingRows = [][]interface{}{{"a0", "a1"}, {"a2", "a3"}, {"a4", "a5"}}
		g.cursorRow = 3 // past the real rows: the third insert
		if got := g.cursorRowType(); got != "insert" {
			t.Fatalf("the cursor row type is %q, want insert", got)
		}
		if got := g.pendingInsertIndex(); got != 0 {
			t.Fatalf("the pending insert index is %d, want 0: the cursor sits on the FIRST insert", got)
		}
		if got := g.UndoRowDrafts(); got != 1 {
			t.Errorf("undoing a pending insert reported %d changes, want 1", got)
		}
		if g.PendingCount() != 2 {
			t.Errorf("after undoing an insert there are %d pending rows, want 2", g.PendingCount())
		}
	})
}

// Scenario: El tipo de fila del cursor distingue REAL de INSERT.
//
// Every navigation decision downstream reads this: the cursor past the last
// committed row is on a draft insert, and editing or undoing it means something
// different. Getting it wrong means an edit lands on the row below, or an undo
// removes the wrong thing.
//
// The boundary is inclusive on the real side: absRow == remaining-1 is still a
// real row.
func TestGrid_TheCursorRowTypeAndPendingIndexAgree(t *testing.T) {
	for _, nRows := range []int{1, 3, 7} {
		for _, nPending := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("%d rows, %d pending", nRows, nPending), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 60, 14, 100)
				g.pendingRows = make([][]interface{}, nPending)
				for i := range nPending {
					g.pendingRows[i] = []interface{}{fmt.Sprintf("p%d", i), "x"}
				}

				for absRow := range nRows + nPending {
					g.scrollRow = 0
					g.cursorRow = absRow

					wantType := "real"
					wantIdx := -1
					if absRow >= nRows {
						wantType = "insert"
						wantIdx = absRow - nRows
					}
					if got := g.cursorRowType(); got != wantType {
						t.Errorf("at row %d the type is %q, want %q", absRow, got, wantType)
					}
					if got := g.pendingInsertIndex(); got != wantIdx {
						t.Errorf("at row %d the pending insert index is %d, want %d", absRow, got, wantIdx)
					}
				}

				// Past the end of everything there is no insert to name.
				g.cursorRow = nRows + nPending + 4
				if got := g.pendingInsertIndex(); got != -1 {
					t.Errorf("past the last insert the index is %d, want -1", got)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

// Scenario: Seleccionar usa el indice ABSOLUTO de la fila, cuenta la pagina y el scroll.
//
// Three offsets are added together to name the row the user is looking at: the
// page offset, the scroll within the page, and the cursor within the drawn block.
// Getting the sum wrong selects a different row than the one highlighted — and the
// count still looks right, which is why this needs a case where all three offsets
// are non-zero and different from each other.
func TestGrid_SelectionNamesTheRowUnderTheCursor(t *testing.T) {
	// 5 pages of 3 rows, a 4-line window: the cursor has to be on the second
	// drawn line of the third page, which is row 3 + 6 = 9.
	g := newGrid(t, 15, 2, 40, 10, 3)
	g.pager.GoToPage(3)
	g.scrollRow = 1
	g.cursorRow = 1

	// The page offset is (page-1)*pageSize, so page 3 of size 3 starts at
	// row 6; add the scroll and the cursor within the drawn block.
	want := 2*3 + 1 + 1
	if got := g.SelectedRow(); got == nil || got[0] != marker(want, 0) {
		t.Fatalf("SelectedRow() is %v, want the row for absolute index %d (%s)", got, want, marker(want, 0))
	}

	if _, handled := g.HandleAction("select_row"); !handled {
		t.Fatal("select_row was not handled")
	}
	if g.SelectionCount() != 1 {
		t.Fatalf("selecting left %d rows selected, want 1", g.SelectionCount())
	}
	sel := g.SelectedRows()
	if len(sel) != 1 || sel[0][0] != marker(want, 0) {
		t.Errorf("SelectedRows() is %v, want the row for absolute index %d", sel, want)
	}

	// Selecting the same row again deselects it, and the toggle is keyed on
	// the absolute index rather than on the cursor position.
	if _, handled := g.HandleAction("select_row"); !handled {
		t.Fatal("the second select_row was not handled")
	}
	if g.SelectionCount() != 0 {
		t.Errorf("toggling the same row left %d selected, want 0", g.SelectionCount())
	}

	// And the clear is total.
	g.ClearSelection()
	if g.SelectionCount() != 0 {
		t.Errorf("ClearSelection left %d selected", g.SelectionCount())
	}
	if g.SelectedRows() != nil {
		t.Error("SelectedRows() returned rows after a clear")
	}
}

// Scenario: La seleccion no se aplica cuando no hay nada que seleccionar.
//
// Two separate guards, and the second one is the interesting one: a grid with a
// result but no rows has no row to select, so pressing the key is not an error but
// also does nothing at all — including reporting that it handled the key.
func TestGrid_SelectionNeedsAGridWithRows(t *testing.T) {
	empty := newGrid(t, 0, 2, 40, 12, 100)
	if _, handled := empty.HandleAction("select_row"); handled {
		t.Error("select_row reported as handled on an empty grid")
	}
	if empty.SelectionCount() != 0 {
		t.Errorf("selecting on an empty grid left %d selected", empty.SelectionCount())
	}

	// And escape clears an existing selection, reporting the move so the
	// surrounding pane knows the cursor region changed.
	g := newGrid(t, 3, 2, 40, 12, 100)
	if _, handled := g.HandleAction("select_row"); !handled {
		t.Fatal("select_row was not handled")
	}
	cmd, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled {
		t.Fatal("escape with a selection was not handled")
	}
	if msg, ok := cmd().(GridCursorMovedMsg); !ok {
		t.Errorf("escape produced %T, want GridCursorMovedMsg", cmd())
	} else if msg != (GridCursorMovedMsg{}) {
		t.Errorf("escape produced %#v, want the zero message", msg)
	}
	if g.SelectionCount() != 0 {
		t.Errorf("escape left %d rows selected", g.SelectionCount())
	}

	// Escape with nothing selected falls through to the keybind registry,
	// which does not bind it — so nothing happens.
	if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); handled {
		t.Error("escape with no selection was reported as handled")
	}
}

// ---------------------------------------------------------------------------
// Deleting, yanking and exporting: what the picker is handed
// ---------------------------------------------------------------------------

// Scenario: Borrar sin seleccion borra la fila del cursor; con seleccion, todas.
//
// Two paths with different shapes. Without a selection the row is the one under
// the cursor — and it is COPIED, not referenced, because the data set is still
// live and a later edit would otherwise change what gets deleted. With a selection
// every selected row is copied, and the selection is dropped afterwards so the user
// cannot delete the same row twice by pressing the key again.
func TestGrid_DeleteStagesTheRowsAndClearsTheSelection(t *testing.T) {
	g := newGrid(t, 4, 2, 40, 12, 100)
	g.scrollRow = 1
	g.cursorRow = 1
	absolute := 1 + 1

	if _, ok := g.startDelete(); !ok {
		t.Fatal("startDelete refused with no selection")
	}
	if !g.HasPendingDeletes() {
		t.Fatal("startDelete staged nothing")
	}
	if len(g.pendingDeletes) != 1 {
		t.Fatalf("one press staged %d deletes, want 1", len(g.pendingDeletes))
	}
	if got := g.pendingDeletes[0].RowIdx; got != absolute {
		t.Errorf("the staged delete names row %d, want %d (scroll plus cursor)", got, absolute)
	}
	if got := g.pendingDeletes[0].Row[0]; got != marker(absolute, 0) {
		t.Errorf("the staged row is %v, want %q", got, marker(absolute, 0))
	}
	// The staged row is a COPY: writing into the grid afterwards must not
	// change what gets deleted.
	g.data.Rows[absolute][0] = "changed after staging"
	if got := g.pendingDeletes[0].Row[0]; got != marker(absolute, 0) {
		t.Errorf("the staged row changed with the data: %#v", got)
	}

	// With a selection, every selected row goes, and the selection is cleared.
	m := newGrid(t, 4, 2, 40, 12, 100)
	m.selectedRows[0] = true
	m.selectedRows[2] = true
	if _, ok := m.startDelete(); !ok {
		t.Fatal("startDelete refused with a selection")
	}
	if len(m.pendingDeletes) != 2 {
		t.Fatalf("two selected rows staged %d deletes, want 2", len(m.pendingDeletes))
	}
	if m.SelectionCount() != 0 {
		t.Errorf("deleting left %d rows selected, so a second press would stage them again", m.SelectionCount())
	}

	// A selection index outside the result stages nothing for it.
	bad := newGrid(t, 2, 2, 40, 12, 100)
	bad.selectedRows[0] = true
	bad.selectedRows[9] = true
	bad.startDelete()
	if len(bad.pendingDeletes) != 1 {
		t.Errorf("a selection naming a row that does not exist staged %d deletes, want just the real one", len(bad.pendingDeletes))
	}
}

// Scenario: Yank ofrece el portapapeles, y pasa a ARCHIVO cuando hay muchas filas.
//
// The threshold exists because a selection that large is a deliberate extraction
// rather than a spot check, and the app writes it to a file instead of putting a
// megabyte on the clipboard. It is `>=`, so the boundary is the threshold itself,
// and both sides of it are worth pinning.
func TestGrid_YankSwitchesToFileModeAtTheThreshold(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected int
		wantFile bool
	}{
		{"no selection yanks the cursor row to the clipboard", 0, false},
		{"one row is a clipboard yank", 1, false},
		{"just under the threshold stays on the clipboard", 2, false},
		{"exactly at the threshold goes to a file", 3, true},
		{"over the threshold goes to a file", 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, 20, 2, 40, 12, 100)
			// The threshold is set explicitly rather than left at the
			// default, so this table is about the comparison and not about
			// whatever the default happens to be.
			g.SetYankMaxRows(3)
			for i := range tc.selected {
				g.selectedRows[i] = true
			}
			if _, ok := g.startYank(); !ok {
				t.Fatal("startYank refused")
			}
			if !g.IsExporting() {
				t.Fatal("startYank did not show the picker")
			}
			if g.exportPicker.fileMode != tc.wantFile {
				t.Errorf("fileMode is %v, want %v", g.exportPicker.fileMode, tc.wantFile)
			}
			// Under the threshold it is a YANK: yank mode is on and file
			// mode is off, so the picker offers only the clipboard. At or
			// over it, a plain file export is shown instead — yank mode is
			// deliberately off there, so a large extraction does not look
			// like a yank with a file option bolted on.
			if g.exportPicker.yankMode != !tc.wantFile {
				t.Errorf("yankMode is %v, want %v for a selection of %d", g.exportPicker.yankMode, !tc.wantFile, tc.selected)
			}
			// More than one selected row arrives as a LIST; one or none
			// arrives as a single row.
			if tc.selected > 1 && len(g.exportPicker.rows) != tc.selected {
				t.Errorf("the picker got %d rows, want %d", len(g.exportPicker.rows), tc.selected)
			}
			if tc.selected <= 1 && len(g.exportPicker.row) == 0 {
				t.Error("the picker got no single row")
			}
		})
	}

	// The default is ten rows, which is a product decision rather than a
	// consequence: a spot check goes to the clipboard and a deliberate
	// extraction does not.
	if d := newGrid(t, 5, 2, 40, 12, 100); d.yankMaxRows != 10 {
		t.Errorf("the default yank threshold is %d rows, want 10", d.yankMaxRows)
	}

	// The threshold is configurable, and a non-positive value is refused
	// rather than turning every yank into a file.
	g := newGrid(t, 5, 2, 40, 12, 100)
	g.SetYankMaxRows(2)
	g.selectedRows[0] = true
	g.selectedRows[1] = true
	g.startYank()
	if !g.exportPicker.fileMode {
		t.Error("two rows with a threshold of two did not switch to file mode")
	}
	g.SetYankMaxRows(0)
	if g.yankMaxRows != 2 {
		t.Errorf("SetYankMaxRows(0) left the threshold at %d, want the previous 2", g.yankMaxRows)
	}
	g.SetYankMaxRows(5)
	if g.yankMaxRows != 5 {
		t.Errorf("SetYankMaxRows(5) left the threshold at %d", g.yankMaxRows)
	}
}

// Scenario: Exportar SIN seleccion manda la tabla entera; con una fila, esa fila.
//
// Export and yank are the same picker with different defaults, and the difference
// matters: "export" with nothing selected means "give me this table", so it goes
// straight to a file. With one row selected there is a choice — clipboard or file
// — so the picker offers it rather than deciding.
func TestGrid_ExportOffersTheWholeTableWhenNothingIsSelected(t *testing.T) {
	all := newGrid(t, 5, 2, 40, 12, 100)
	if _, ok := all.startExport(); !ok {
		t.Fatal("startExport refused with no selection")
	}
	if !all.exportPicker.fileMode {
		t.Error("exporting with no selection did not go straight to a file")
	}
	if len(all.exportPicker.rows) != 5 {
		t.Errorf("the picker got %d rows, want the whole table's 5", len(all.exportPicker.rows))
	}

	one := newGrid(t, 5, 2, 40, 12, 100)
	one.selectedRows[3] = true
	one.startExport()
	if one.exportPicker.fileMode {
		t.Error("exporting a single row went straight to a file, so the user was not offered the clipboard")
	}
	if len(one.exportPicker.row) == 0 {
		t.Error("the picker got no single row")
	}
	if len(one.exportPicker.rows) != 0 {
		t.Errorf("the picker also got %d rows, want none for a single-row export", len(one.exportPicker.rows))
	}

	many := newGrid(t, 5, 2, 40, 12, 100)
	many.selectedRows[1] = true
	many.selectedRows[3] = true
	many.startExport()
	if !many.exportPicker.fileMode {
		t.Error("exporting several rows did not go to a file")
	}
	if len(many.exportPicker.rows) != 2 {
		t.Errorf("the picker got %d rows, want the 2 selected", len(many.exportPicker.rows))
	}
}

// Scenario: Navegar a la clave AJENA pide la fila referenciada, con su esquema.
//
// The message is the whole feature: the app opens the referenced table filtered to
// this row. So the four fields that make it a lookup — column, value, table and
// column — all have to be right, and the SCHEMA has a default: a reference with no
// schema of its own is in the current one, so it is filled in from the grid rather
// than sent empty.
//
// It also has to REFUSE: only a column marked as a foreign key can be followed, and
// a NULL value has nothing to follow.
func TestGrid_ForeignKeyNavigationNamesTheReferencedRow(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 100)
	g.SetMetadata(
		[]postgres.ConstraintInfo{{Type: "PRIMARY KEY", Columns: "c0"}},
		[]postgres.ForeignKeyInfo{
			{Column: "c1", RefTable: "users", RefColumn: "id"},
			{Column: "c1", RefSchema: "auth", RefTable: "accounts", RefColumn: "uuid"},
		},
		nil,
	)
	g.cursorRow = 1
	g.cursorCol = 1

	cmd, ok := g.startFK()
	if !ok {
		t.Fatal("startFK refused a column marked as a foreign key")
	}
	msg, isFK := cmd().(GridNavigateFKMsg)
	if !isFK {
		t.Fatalf("startFK produced %T, want GridNavigateFKMsg", cmd())
	}
	if msg.Schema != "public" || msg.Table != "t" {
		t.Errorf("the message says %s.%s, want public.t", msg.Schema, msg.Table)
	}
	if msg.FKColumn != "c1" {
		t.Errorf("the FK column is %q, want c1", msg.FKColumn)
	}
	if msg.FKValue != marker(1, 1) {
		t.Errorf("the FK value is %#v, want the cell's own %q", msg.FKValue, marker(1, 1))
	}
	// The first matching foreign key wins, and it has no schema of its own,
	// so the current one fills in.
	if msg.RefSchema != "public" || msg.RefTable != "users" || msg.RefColumn != "id" {
		t.Errorf("the reference is %s.%s(%s), want public.users(id)", msg.RefSchema, msg.RefTable, msg.RefColumn)
	}

	// A reference that names its own schema keeps it.
	h := newGrid(t, 3, 2, 40, 12, 100)
	h.SetMetadata(nil, []postgres.ForeignKeyInfo{
		{Column: "c1", RefSchema: "auth", RefTable: "accounts", RefColumn: "uuid"},
	}, nil)
	h.cursorCol = 1
	cmd, ok = h.startFK()
	if !ok {
		t.Fatal("startFK refused a cross-schema reference")
	}
	if got := cmd().(GridNavigateFKMsg); got.RefSchema != "auth" {
		t.Errorf("the reference schema is %q, want auth: a reference that names its own schema keeps it", got.RefSchema)
	}

	// The refusals.
	plain := newGrid(t, 3, 2, 40, 12, 100)
	plain.cursorCol = 1
	if _, ok := plain.startFK(); ok {
		t.Error("startFK followed a column with no foreign key")
	}
	null := newGrid(t, 3, 2, 40, 12, 100)
	null.SetMetadata(nil, []postgres.ForeignKeyInfo{{Column: "c1", RefTable: "users", RefColumn: "id"}}, nil)
	null.data.Rows[0] = []interface{}{"x", nil}
	null.cursorRow = 0
	null.cursorCol = 1
	if _, ok := null.startFK(); ok {
		t.Error("startFK followed a NULL foreign key value, which names no row")
	}
	empty := newGrid(t, 0, 2, 40, 12, 100)
	if _, ok := empty.startFK(); ok {
		t.Error("startFK followed a reference on an empty grid")
	}
}

// startFK is the dispatchAction arm for navigate_fk, wrapped so the tests do not
// have to know which action id reaches it.
func (g *Grid) startFK() (tea.Cmd, bool) { return g.dispatchAction("navigate_fk") }

// ---------------------------------------------------------------------------
// Sorting, the hint, and the three two-step confirmations
// ---------------------------------------------------------------------------

// Scenario: Ordenar alterna la DIRECCION y avisa de la columna y el sentido.
//
// Sort is a round trip: the grid tells the app what to sort by, the app re-queries,
// and the grid never sorts locally. So the message is the whole behaviour — and it
// carries the WHERE clause too, because a sorted query of a filtered table has to
// keep the filter.
func TestGrid_SortingAsksForTheColumnAndItsDirection(t *testing.T) {
	// Renamed through SetData, not by assigning g.columns: the header holds
	// its own copy of the names, and reaching past SetData would leave the
	// two disagreeing.
	g := newGrid(t, 3, 3, 40, 12, 100)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "age"}},
		Rows:    g.data.Rows,
		Count:   g.data.Count,
	}, "public", "t")
	g.whereClause = "id > 1"
	g.cursorCol = 1

	cmd := g.toggleSort()
	if cmd == nil {
		t.Fatal("toggleSort produced no command")
	}
	msg, isSort := cmd().(GridSortApplyMsg)
	if !isSort {
		t.Fatalf("toggleSort produced %T, want GridSortApplyMsg", cmd())
	}
	if msg.OrderBy != "name" {
		t.Errorf("the sort column is %q, want name", msg.OrderBy)
	}
	if msg.OrderDir != "ASC" {
		t.Errorf("the first sort is %q, want ASC", msg.OrderDir)
	}
	if msg.Where != "id > 1" {
		t.Errorf("the message's WHERE is %q, want the active clause", msg.Where)
	}
	if msg.Schema != "public" || msg.Table != "t" {
		t.Errorf("the message says %s.%s", msg.Schema, msg.Table)
	}

	// A second press on the same column reverses it.
	cmd = g.toggleSort()
	if got := cmd().(GridSortApplyMsg).OrderDir; got != "DESC" {
		t.Errorf("the second sort is %q, want DESC", got)
	}

	// A column outside the range sorts nothing at all.
	g.cursorCol = 9
	if g.toggleSort() != nil {
		t.Error("toggleSort on a column outside the range produced a command")
	}
}

// Scenario: La pista de contexto se calla para una consulta y para el vacio.
//
// The ASK pane shows "you are looking at schema.table WHERE ...". That hint is
// only true for a real table: a query result is stored under the synthetic name
// "query", and telling the user they are in `public.query` would be a lie that
// sends their next question to the wrong place.
func TestGrid_TheContextHintIsSilentWithoutARealTable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		table  string
		want   bool
	}{
		{"a real table answers", "public", "users", true},
		{"a cross-schema table answers", "auth", "accounts", true},
		{"a query result does not", "public", "query", false},
		{"no schema does not", "", "users", false},
		{"no table does not", "public", "", false},
		{"neither does not", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, 2, 2, 40, 12, 100)
			g.SetData(g.data, tc.schema, tc.table)
			g.whereClause = "id = 1"

			schema, table, where := g.ContextHint()
			if !tc.want {
				if schema != "" || table != "" || where != "" {
					t.Errorf("ContextHint() = %q/%q/%q, want three empty strings", schema, table, where)
				}
				return
			}
			if schema != tc.schema || table != tc.table {
				t.Errorf("ContextHint() = %q/%q, want %q/%q", schema, table, tc.schema, tc.table)
			}
			if where != "id = 1" {
				t.Errorf("ContextHint()'s WHERE is %q, want the active clause", where)
			}
		})
	}
}

// Scenario: Guardar, descartar y recargar son de DOS pasos, y el segundo ejecuta.
//
// Deleting a row is destructive, so it is confirmed; and once a draft exists,
// overwriting it with fresh data would lose work, so refreshing is confirmed too.
// Discard needs no second step only because the FIRST step is what arms it — the
// symmetry is the point.
//
// What makes this worth a property rather than three scenarios is the ARMING: any
// other action cancels a pending confirmation. A user who presses Ctrl+S by
// accident, then moves the cursor, must not then press Ctrl+S and lose their
// draft. Commit is the exception, because the second Ctrl+S IS the commit.
func TestGrid_TheThreeConfirmationsArmAndCancel(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		if _, ok := g.HandleAction("commit_drafts"); ok {
			t.Error("commit_drafts with nothing pending was handled")
		}
		g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
		// With no deletes there is nothing to lose, so one press commits.
		cmd, ok := g.HandleAction("commit_drafts")
		if !ok {
			t.Fatal("commit_drafts with only updates was not handled")
		}
		if cmd == nil {
			t.Error("commit_drafts produced no command")
		}
		if g.IsCommitPending() {
			t.Error("a commit with no deletes still asked for confirmation")
		}

		// With a delete it takes two, because that is the destructive one.
		d := newGrid(t, 3, 2, 40, 12, 100)
		d.pendingDeletes = []PendingDelete{{RowIdx: 0, Row: []interface{}{"a", "b"}}}
		cmd, ok = d.HandleAction("commit_drafts")
		if !ok {
			t.Fatal("commit_drafts with a delete was not handled")
		}
		if cmd != nil {
			t.Error("the first commit press already ran the commit")
		}
		if !d.IsCommitPending() {
			t.Error("the first commit press did not arm the confirmation")
		}
		if !strings.Contains(ansi.Strip(resultOf(d.renderRecordsView())[0]), "confirm") {
			t.Error("the armed confirmation is not on screen")
		}
		cmd, _ = d.HandleAction("commit_drafts")
		if cmd == nil {
			t.Error("the second commit press did not run the commit")
		}
		if d.IsCommitPending() {
			t.Error("the commit confirmation stayed armed after committing")
		}
		if d.HasDrafts() {
			t.Error("committing left drafts behind")
		}
	})

	t.Run("discard", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		if _, ok := g.HandleAction("discard_drafts"); ok {
			t.Error("discard_drafts with nothing pending was handled")
		}
		g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "r0c0", NewValue: "b"}}
		if _, ok := g.HandleAction("discard_drafts"); !ok {
			t.Fatal("the first discard press was not handled")
		}
		if !g.IsDiscardPending() {
			t.Error("the first discard press did not arm the confirmation")
		}
		if !g.HasDrafts() {
			t.Error("the first discard press discarded the drafts; it should only have armed the confirmation")
		}
		if _, ok := g.HandleAction("discard_drafts"); !ok {
			t.Fatal("the second discard press was not handled")
		}
		if g.IsDiscardPending() {
			t.Error("the discard confirmation stayed armed after discarding")
		}
		if g.HasDrafts() {
			t.Error("the second discard press did not discard")
		}
		if got := g.data.Rows[0][0]; got != marker(0, 0) {
			t.Errorf("discarding left the cell at %#v, want the original", got)
		}
	})

	t.Run("refresh", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		// With no drafts there is nothing to lose, so one press reloads.
		cmd, ok := g.Refresh()
		if !ok {
			t.Fatal("refresh with no drafts was not handled")
		}
		if _, isConfirm := cmd().(GridRefreshConfirmMsg); !isConfirm {
			t.Fatalf("refresh produced %T, want GridRefreshConfirmMsg", cmd())
		}

		d := newGrid(t, 3, 2, 40, 12, 100)
		d.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
		cmd, ok = d.Refresh()
		if !ok {
			t.Fatal("refresh with drafts was not handled")
		}
		if cmd != nil {
			t.Error("the first refresh press already reloaded, which would lose the draft")
		}
		if !d.IsRefreshPending() {
			t.Error("the first refresh press did not arm the confirmation")
		}
		cmd, _ = d.Refresh()
		if _, isConfirm := cmd().(GridRefreshConfirmMsg); !isConfirm {
			t.Fatalf("the second refresh press produced %T, want GridRefreshConfirmMsg", cmd())
		}
		if d.IsRefreshPending() {
			t.Error("the refresh confirmation stayed armed")
		}
	})

	t.Run("any other action cancels an armed confirmation", func(t *testing.T) {
		for _, armed := range []config.ActionID{"discard_drafts", "commit_drafts", "refresh_data"} {
			for _, other := range []config.ActionID{"navigate_down", "navigate_up", "navigate_right", "edit_cell"} {
				t.Run(string(armed)+" then "+string(other), func(t *testing.T) {
					g := newGrid(t, 5, 2, 40, 12, 100)
					// A delete AND a draft, so all three can be armed.
					g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
					g.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{"a", "b"}}}
					g.HandleAction(armed)
					if !g.IsDiscardPending() && !g.IsCommitPending() && !g.IsRefreshPending() {
						t.Fatalf("%s did not arm anything", armed)
					}
					g.HandleAction(other)
					if g.IsDiscardPending() || g.IsCommitPending() || g.IsRefreshPending() {
						t.Errorf("%s left an armed confirmation after %s", armed, other)
					}
				})
			}
		}
	})
}

// ---------------------------------------------------------------------------
// The WHERE filter bar and its three banners
// ---------------------------------------------------------------------------

// Scenario: Filtrar emite UNA consulta con el WHERE, y dice si la hara el servidor.
//
// The message's ClientSide flag is the whole reason this exists: a result the grid
// already holds can be filtered in memory, but past MaxRows the server only ever
// sent a page, so filtering it locally would silently drop rows that are really
// there. The threshold is therefore a correctness boundary and not a tuning knob,
// and both sides of it are worth pinning — one side needs a thousand and one rows
// fixtures, which is why it is built rather than typed.
func TestGrid_ApplyingAWhereFilterSaysWhetherTheServerShouldDoIt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows       int
		wantClient bool
	}{
		{"a small result is filtered in place", 3, true},
		{"a result exactly at the threshold is still in place", MaxRows, true},
		{"a result past the threshold goes to the server", MaxRows + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, tc.rows, 2, 60, 14, 100)
			g.startWhereFilter()
			if !g.IsWhereFiltering() {
				t.Fatal("startWhereFilter did not show the filter bar")
			}
			g.whereFilter.SetInput("id = 7")

			cmd, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !handled {
				t.Fatal("enter on the filter bar was not handled")
			}
			msg, isApply := cmd().(GridFilterApplyMsg)
			if !isApply {
				t.Fatalf("applying the filter produced %T, want GridFilterApplyMsg", cmd())
			}
			if msg.Where != "id = 7" {
				t.Errorf("the WHERE is %q, want what was typed", msg.Where)
			}
			if msg.Schema != "public" || msg.Table != "t" {
				t.Errorf("the message says %s.%s, want public.t", msg.Schema, msg.Table)
			}
			if msg.ClientSide != tc.wantClient {
				t.Errorf("ClientSide is %v, want %v for %d rows against a threshold of %d", msg.ClientSide, tc.wantClient, tc.rows, MaxRows)
			}
			// The clause is kept for the banner and for the next refresh,
			// and the bar is gone.
			if g.WhereClause() != "id = 7" {
				t.Errorf("the grid's WHERE clause is %q after applying", g.WhereClause())
			}
			if g.IsWhereFiltering() {
				t.Error("the filter bar is still up after applying")
			}
		})
	}
}

// Scenario: La barra se cierra con escape, y vaciarla tambien.
//
// Two escapes, not one: the first closes the suggestion popup, and only once that
// is closed does escape reach the input — and once the input is empty the next one
// closes the bar. So a user typing a clause cannot lose it by pressing escape
// twice, which is the failure mode a single-escape implementation would have.
func TestGrid_EscapeClosesTheFilterBarOneStepAtATime(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 14, 100)
	g.startWhereFilter()
	if !g.whereFilter.showPopup {
		t.Skip("the suggestion popup is not showing, so the first escape would not have it to close")
	}

	// The FIRST escape closes the suggestion popup and leaves the bar up.
	g.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if g.whereFilter.showPopup {
		t.Error("escape did not close the suggestion popup")
	}
	if !g.IsWhereFiltering() {
		t.Error("escape closed the whole bar instead of just the popup")
	}

	// The second one, now that the popup is gone and nothing is typed,
	// closes the bar.
	g.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if g.IsWhereFiltering() {
		t.Error("escape on an empty filter bar did not close it")
	}
	if g.WhereClause() != "" {
		t.Errorf("closing the bar set the WHERE clause to %q", g.WhereClause())
	}

	// With text, escape clears the text first and keeps the bar up.
	h := newGrid(t, 3, 2, 60, 14, 100)
	h.startWhereFilter()
	h.whereFilter.SetInput("id = 1")
	h.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !h.IsWhereFiltering() {
		t.Error("escape closed a filter bar that still had text in it")
	}
	if got := h.whereFilter.Input(); got != "" {
		t.Errorf("escape left the input as %q, want it cleared", got)
	}

	// And a bar that has no columns to filter by never opens.
	none := newGrid(t, 0, 0, 60, 14, 100)
	none.startWhereFilter()
	if none.IsWhereFiltering() {
		t.Error("the filter bar opened on a result with no columns")
	}
}

// Scenario: Reabrir el filtro trae el WHERE vigente.
//
// The bar is a refinement of the current filter, not a fresh one: reopening it with
// the previous clause in place is what lets a user widen a query instead of
// retyping it.
func TestGrid_ReopeningTheFilterBarRestoresTheClause(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 14, 100)
	g.whereClause = "active = true"

	g.startWhereFilter()
	if !g.IsWhereFiltering() {
		t.Fatal("the bar did not open")
	}
	// The pre-fill lands in the LIVE input — the thing the user edits and
	// applies — and not in `applyInput`, which is only set when the clause is
	// actually applied. Reading Input() here would compare against the
	// previous application, not the text in the box.
	if got := g.whereFilter.input; got != "active = true" {
		t.Errorf("the reopened bar holds %q, want the active clause", got)
	}
	// And the bar takes a line of the view, which is why recordsHeaderOffset
	// counts it.
	if got := g.recordsHeaderOffset(); got < 1 {
		t.Errorf("the header offset is %d with the filter bar up, want at least 1", got)
	}
}

// Scenario: Los TRES banners del filtro son alternativas, y cada uno ocupa su linea.
//
// Three ways of saying "the rows on screen are not all the rows": the WHERE clause
// is applied, the column finder is open, or the filter bar is up. They are an
// if/else-if chain, so exactly one line appears above the header — and which one is
// decided in a fixed order, not by which is more recently set.
func TestGrid_ExactlyOneFilterBannerIsShownAtATime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clause   string
		finding  bool
		bar      bool
		wantText string
	}{
		{"no filter at all", "", false, false, ""},
		{"an applied clause", "id = 1", false, false, "WHERE id = 1"},
		{"the column finder", "", true, false, "Column: "},
		{"the filter bar", "", false, true, "WHERE"},
		// The bar outranks the clause, because it is the live one: a user
		// typing a new clause over an old one must see the input, not the
		// clause it is replacing.
		{"the bar outranks the clause", "id = 1", false, true, "WHERE"},
		// And the clause outranks the finder, which is only reachable
		// when no clause is applied — the finder is for jumping to a
		// column, and an applied WHERE is a statement about the rows.
		{"the clause outranks the finder", "id = 1", true, false, "WHERE id = 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, 3, 2, 60, 14, 100)
			g.whereClause = tc.clause
			g.filtering = tc.finding
			g.filter = "na"
			if tc.bar {
				g.startWhereFilter()
				g.whereFilter.SetInput("id = 2")
			}

			out := resultOf(g.renderRecordsView())
			head := g.recordsHeaderOffset()
			// Every banner is one line, so the header is at line 1 when
			// there is one and at line 0 when there is not.
			wantHead := 0
			if tc.wantText != "" {
				wantHead = 1
			}
			if head != wantHead {
				t.Errorf("the header is at line %d, want %d: these banners are single-line", head, wantHead)
			}
			// And the header line is the column names, not a banner.
			if got := ansi.Strip(out[head]); !strings.Contains(got, "C0") {
				t.Errorf("line %d is %q, want the column header", head, got)
			}

			body := ansi.Strip(strings.Join(out[:head], "\n"))
			if tc.wantText == "" {
				if body != "" {
					t.Errorf("with no filter there is a banner anyway: %q", body)
				}
				return
			}
			if !strings.Contains(body, tc.wantText) {
				t.Errorf("the banner is %q, want it to mention %q", body, tc.wantText)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rendering an edit that is off-screen
// ---------------------------------------------------------------------------

// Scenario: Editar una columna FUERA de la vista la dibuja como una celda normal.
//
// The editor can be on a column the horizontal window has scrolled away from — the
// user scrolled right while editing, or the window narrowed under them. Then there
// is no cell to put the cursor in, and the row is drawn as an ordinary row with the
// grid cursor on it. Rendering the edit there would highlight a column the user
// cannot see, which looks like a stuck cursor.
//
// Both directions are covered: the edited column left of the window and right of it.
func TestGrid_AnOffScreenEditedColumnDrawsAsAnOrdinaryRow(t *testing.T) {
	// Twelve columns in a 46-cell pane, so four fit and the window has to
	// scroll. See the pending-insert counterpart below for why the count
	// matters.
	g := newGrid(t, 4, 12, 46, 12, 100)
	g.focused = true

	// Put the editor on column 0 and scroll the window past it.
	g.cursorRow = 1
	g.cursorCol = 0
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	typeIn(t, g, "typed")
	for range 6 {
		g.moveRight()
	}
	if g.scrollCol == 0 || g.editCol >= g.scrollCol {
		t.Fatalf("the window is at %d and the editor at %d, so the case was not reached", g.scrollCol, g.editCol)
	}

	out := resultOf(g.renderRecordsView())
	row := strings.Join(out, "\n")
	if strings.Contains(ansi.Strip(row), "typed") {
		t.Error("the in-progress value is drawn even though its column is scrolled out of the window")
	}
	// And the row is still a row, drawn with the columns that ARE in view.
	// Asserting on column 0 here would be asserting on a column the window
	// has just scrolled past — the same fixture trap as reading a truncated
	// row by its own width.
	if !strings.Contains(ansi.Strip(row), marker(1, g.scrollCol)) {
		t.Errorf("the edited row lost its data from the visible columns:\n%s", row)
	}

	// And the other failure of the same guard: the editor's offset is BEYOND
	// the visible columns, which is what a pane narrowed under an open editor
	// produces. The window is at column 0 and only one column fits, while the
	// editor is on column 3 — so the offset is neither negative nor in range.
	h := newGrid(t, 4, 12, 46, 12, 100)
	h.focused = true
	h.cursorRow = 1
	h.cursorCol = 3
	if _, handled := h.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	typeIn(t, h, "typed")

	// Narrow the pane until fewer columns fit than the editor is offset by,
	// then walk the cursor back to the first column so the window does not
	// follow it.
	h.SetWidth(12)
	for range 3 {
		h.moveLeft()
	}
	visCols, _ := h.visibleColumns()
	if h.editCol < len(visCols) {
		t.Fatalf("the editor's offset %d is still inside %d visible columns, so the case was not reached", h.editCol, len(visCols))
	}
	if strings.Contains(ansi.Strip(h.renderRecordsView()), "typed") {
		t.Error("the in-progress value is drawn although its column is beyond the visible ones")
	}
}

// ---------------------------------------------------------------------------
// Window and cursor clamps
// ---------------------------------------------------------------------------

// Scenario: La ventana se ajusta SIEMPRE al cursor, incluso si el indice estaba corrupto.
//
// syncScroll is called after every width change and every horizontal move, and it
// is the only thing standing between a stale index and a panic on g.widths[i]. Both
// indices are clamped BEFORE any arithmetic, so a cursor left behind by a resize — or
// by a narrower result replacing a wider one — cannot index outside the widths.
func TestGrid_TheWindowRecoversFromAnIndexOutsideTheColumns(t *testing.T) {
	for _, nCols := range []int{1, 3, 6} {
		t.Run(fmt.Sprintf("%d columns", nCols), func(t *testing.T) {
			for _, corrupt := range []struct {
				name              string
				cursor, scrollCol int
			}{
				{"a cursor past the last column", nCols + 3, 0},
				{"a negative cursor", -2, 0},
				{"a scroll past the last column", 0, nCols + 3},
				{"a negative scroll", 0, -2},
				{"both wrong at once", nCols + 1, nCols + 5},
				{"both negative at once", -1, -1},
			} {
				t.Run(corrupt.name, func(t *testing.T) {
					h := newGrid(t, 2, nCols, 60, 12, 100)
					h.cursorCol = corrupt.cursor
					h.scrollCol = corrupt.scrollCol
					h.syncScroll()

					if h.cursorCol < 0 || h.cursorCol >= nCols {
						t.Errorf("the cursor settled at column %d, outside %d columns", h.cursorCol, nCols)
					}
					if h.scrollCol < 0 || h.scrollCol > h.cursorCol {
						t.Errorf("the window settled at column %d with the cursor at %d, so the cursor is outside it", h.scrollCol, h.cursorCol)
					}
				})
			}

			// A window with no columns collapses to zero rather than
			// keeping the old scroll.
			empty := newGrid(t, 0, 0, 60, 12, 100)
			empty.scrollCol = 4
			empty.syncScroll()
			if empty.scrollCol != 0 {
				t.Errorf("a grid with no columns left the window at %d", empty.scrollCol)
			}

			// A pane with no room at all does too, rather than looping
			// over widths that cannot fit.
			narrow := newGrid(t, 2, nCols, 1, 12, 100)
			narrow.scrollCol = 2
			narrow.syncScroll()
			if narrow.scrollCol != 0 {
				t.Errorf("a one-cell pane left the window at column %d", narrow.scrollCol)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Editing a pending insert
// ---------------------------------------------------------------------------

// Scenario: Editar una fila pendiente la cambia A ELLA, no a la fila de debajo.
//
// A pending insert lives past the last committed row, and its index in the pending
// list is the cursor's absolute row minus the number of committed rows. Getting
// that subtraction wrong writes the value into the wrong draft — and because both
// are drafts the user is looking at, the mistake is visible only after a save.
//
// A cell whose value is nil loads as an empty string rather than the text "<nil>".
func TestGrid_EditingAPendingInsertWritesIntoThePendingRow(t *testing.T) {
	for _, nPending := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("%d pending rows", nPending), func(t *testing.T) {
			g := newGrid(t, 3, 3, 60, 16, 100)
			g.pendingRows = make([][]interface{}, nPending)
			for i := range nPending {
				g.pendingRows[i] = []interface{}{nil, fmt.Sprintf("seed%d", i), nil}
			}
			// Sit on the LAST pending row, so the index is not zero and a
			// missing subtraction is visible.
			g.cursorRow = 3 + nPending - 1
			g.cursorCol = 1

			if _, handled := g.HandleAction("edit_cell"); !handled {
				t.Fatal("edit_cell on a pending row was not handled")
			}
			if !g.IsInserting() {
				t.Fatal("editing a pending row did not put the grid in insert mode")
			}
			if g.PendingCount() != nPending {
				t.Fatalf("editing a pending row left %d pending rows, want %d", g.PendingCount(), nPending)
			}
			// The value came from the pending row, not from a committed
			// one.
			want := fmt.Sprintf("seed%d", nPending-1)
			if g.editValue != want {
				t.Errorf("the editor loaded %q, want %q from the last pending row", g.editValue, want)
			}
			typeIn(t, g, "X")
			tap(t, g, tea.KeyEscape)

			// Only the row being edited changed, and it kept the text
			// rather than becoming a value of the column's own type.
			if got := g.pendingRows[nPending-1][1]; got != want+"X" {
				t.Errorf("the edited pending row holds %#v, want %q", got, want+"X")
			}
			for i := range nPending - 1 {
				if got := g.pendingRows[i][1]; got != fmt.Sprintf("seed%d", i) {
					t.Errorf("pending row %d holds %#v, want it untouched", i, got)
				}
			}
			// And nothing leaked into the committed rows.
			for i, row := range g.data.Rows {
				if got := row[1]; got != marker(i, 1) {
					t.Errorf("committed row %d holds %#v, want its own value", i, got)
				}
			}
		})
	}

	// A nil cell loads as empty rather than as Go's nil formatting.
	n := newGrid(t, 2, 2, 60, 14, 100)
	n.pendingRows = [][]interface{}{{nil, nil}}
	n.cursorRow = 2
	n.cursorCol = 1
	n.HandleAction("edit_cell")
	if n.editValue != "" {
		t.Errorf("a nil cell loaded as %q, want the empty string", n.editValue)
	}

	// A cursor past the last pending row is not an insert, and the edit
	// falls through to the committed path — where there is no row, so
	// nothing happens rather than a panic.
	p := newGrid(t, 2, 2, 60, 14, 100)
	p.pendingRows = [][]interface{}{{nil, nil}}
	p.cursorRow = 2 + 5
	if got := p.pendingInsertIndex(); got != -1 {
		t.Fatalf("the pending insert index is %d, want -1 past the last insert", got)
	}
	if _, handled := p.HandleAction("edit_cell"); !handled {
		t.Error("edit_cell past the last pending row was not handled")
	}
	if p.IsEditing() {
		t.Error("edit_cell past the last pending row started editing")
	}
}

// Scenario: En una fila pendiente, tab lleva a la SIGUIENTE celda, con nil en blanco.
//
// The same rule as a committed row, on a different data source: a nil cell loads
// as the empty string, and the value committed is the empty string — which is what
// makes a column the user never filled in come out as NULL rather than as the text
// "".
func TestGrid_TabAcrossAPendingInsertTreatsNilAsEmpty(t *testing.T) {
	g := newGrid(t, 2, 3, 60, 16, 100)
	g.pendingRows = [][]interface{}{{"first", nil, nil}}
	g.cursorRow = 2
	g.cursorCol = 0

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	if g.editValue != "first" {
		t.Fatalf("the editor loaded %q, want %q", g.editValue, "first")
	}

	tap(t, g, tea.KeyTab)
	if g.editValue != "" {
		t.Errorf("tabbing to a nil cell loaded %q, want the empty string", g.editValue)
	}
	if g.editCursor != 0 {
		t.Errorf("tabbing to a nil cell left the cursor at %d, want 0", g.editCursor)
	}
	if g.pendingCol != 1 {
		t.Errorf("tabbing left the pending column at %d, want 1", g.pendingCol)
	}

	// Committing an empty cell writes the empty string; committing through
	// the insert's own path writes NULL.
	g.pendingCol = 1
	g.editCol = 1
	g.editValue = ""
	g.inserting = true
	g.pendingRow = 0
	g.commitEdit()
	if got := g.pendingRows[0][1]; got != nil {
		t.Errorf("committing an empty pending cell wrote %#v, want nil so the column comes out NULL", got)
	}
}

// Scenario: Un INSERT pendiente con todo relleno se escribe con NULL, sin hueco en los args.
//
// In the parameterised commit a NULL is the literal `NULL` and takes NO placeholder,
// so the arguments after it are numbered without a gap. Getting the increment
// wrong shifts every subsequent placeholder and the statement binds the wrong value
// to the wrong column.
func TestGrid_CommittingAPendingInsertNumbersItsArgumentsAroundNulls(t *testing.T) {
	g := newGrid(t, 0, 0, 60, 14, 100)
	g.columns = []string{"a", "b", "c"}
	g.schema = "public"
	g.tableName = "t"
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}},
	}
	g.pendingRows = [][]interface{}{{"first", nil, "third"}}

	cmd := g.CommitAllDrafts()
	if cmd == nil {
		t.Fatal("CommitAllDrafts with a pending insert produced no command")
	}
	msg, isCommit := cmd().(GridCommitAllMsg)
	if !isCommit {
		t.Fatalf("CommitAllDrafts produced %T, want GridCommitAllMsg", cmd())
	}
	want := `INSERT INTO "public"."t" (a, b, c) VALUES ($1, NULL, $2)`
	if msg.Queries[0] != want {
		t.Errorf("the insert is %q, want %q", msg.Queries[0], want)
	}
	if len(msg.Args) != 1 || len(msg.Args[0]) != 2 {
		t.Fatalf("the args are %v, want two: the NULL takes no placeholder", msg.Args)
	}
	if msg.Args[0][0] != "first" || msg.Args[0][1] != "third" {
		t.Errorf("the args are %v, want [first third]", msg.Args[0])
	}
	if g.PendingCount() != 0 {
		t.Errorf("committing left %d pending inserts", g.PendingCount())
	}

	// Nothing to commit produces no command at all, rather than an empty
	// statement.
	empty := newGrid(t, 2, 2, 60, 14, 100)
	if cmd := empty.CommitAllDrafts(); cmd != nil {
		t.Errorf("committing with no drafts produced %T, want nil", cmd())
	}
}

// ---------------------------------------------------------------------------
// The small accessors
// ---------------------------------------------------------------------------

// Scenario: Los accesores sin datos NO devuelven el dato de otra tabla.
//
// These are read by the panes around the grid — the status bar, the hint, the yank
// path — and a nil dereference there is a crash rather than a wrong label. Each one
// is a separate guard, so each is exercised on a grid that has never been given
// data.
func TestGrid_TheAccessorsAreSafeOnAGridWithNoData(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(40)
	g.SetHeight(12)

	if g.AllRows() != nil {
		t.Error("AllRows() returned rows for a grid with no data")
	}
	if g.HasPendingRows() {
		t.Error("HasPendingRows() is true for a grid with no data")
	}
	if g.PendingCount() != 0 {
		t.Error("PendingCount() is not zero for a grid with no data")
	}
	if g.HasPendingDeletes() {
		t.Error("HasPendingDeletes() is true for a grid with no data")
	}
	if g.HasDrafts() {
		t.Error("HasDrafts() is true for a grid with no data")
	}
	if g.DraftCount() != 0 {
		t.Error("DraftCount() is not zero for a grid with no data")
	}
	if g.DraftSQL() != "" {
		t.Errorf("DraftSQL() is %q for a grid with no data", g.DraftSQL())
	}
	if g.IsInserting() || g.IsEditing() || g.IsFiltering() || g.IsExporting() || g.IsWhereFiltering() {
		t.Error("a fresh grid reports a mode it is not in")
	}
	if g.IsCommitPending() || g.IsDiscardPending() || g.IsRefreshPending() {
		t.Error("a fresh grid reports an armed confirmation")
	}
	if g.Columns() != nil {
		t.Error("Columns() returned names for a grid with no data")
	}
	if g.ForeignKeys() != nil {
		t.Error("ForeignKeys() returned keys for a grid with no data")
	}
	if g.ActiveTab() != 0 {
		t.Errorf("ActiveTab() is %d for a fresh grid", g.ActiveTab())
	}
	if g.CursorRow() != 0 || g.CursorCol() != 0 || g.ScrollRow() != 0 || g.ScrollCol() != 0 {
		t.Error("a fresh grid does not have its cursor at the origin")
	}

	// The commit flag is the one setter with no guard, because it is
	// armed from outside.
	g.SetCommitPending(true)
	if !g.IsCommitPending() {
		t.Error("SetCommitPending(true) did not arm the confirmation")
	}
	g.SetCommitPending(false)
	if g.IsCommitPending() {
		t.Error("SetCommitPending(false) did not disarm the confirmation")
	}

	// With data, the accessors answer.
	full := newGrid(t, 3, 2, 40, 12, 100)
	if len(full.AllRows()) != 3 {
		t.Errorf("AllRows() returned %d rows, want 3", len(full.AllRows()))
	}
	if len(full.Columns()) != 2 {
		t.Errorf("Columns() returned %d names, want 2", len(full.Columns()))
	}
	if full.TableName() != "t" {
		t.Errorf("TableName() is %q, want t", full.TableName())
	}
}

// ---------------------------------------------------------------------------
// The remaining clamps
// ---------------------------------------------------------------------------

// Scenario: clampCursor arrastra la vista para que el cursor quede en una fila real.
//
// Every page change and every insertion calls it, and the state it is there to
// repair is a cursor pointing past the end — which happens whenever the content
// SHRINKS under a stationary cursor: a delete staged and undone, an insert removed,
// a page turned onto a shorter page. The repair is to push the window down so the
// last row is drawn, then land the cursor on it.
func TestGrid_ClampCursorPullsTheCursorBackOntoARow(t *testing.T) {
	for _, nRows := range []int{1, 3, 6, 20} {
		for _, height := range []int{8, 12, 20} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 40, height, 100)
				ch := g.contentHeight()
				total := g.visibleRows()

				// A cursor parked far past the end, which is what a
				// shrinking result leaves behind.
				g.scrollRow = 0
				g.cursorRow = total + 5
				g.clampCursor()

				if got := g.scrollRow + g.cursorRow; got != total-1 {
					t.Errorf("clampCursor left the cursor on row %d of %d, want the last", got, total)
				}
				if g.cursorRow < 0 || g.cursorRow >= ch {
					t.Errorf("the cursor is on drawn line %d of a %d-line window", g.cursorRow, ch)
				}
				if g.scrollRow < 0 {
					t.Errorf("clampCursor produced a negative scroll of %d", g.scrollRow)
				}

				// With nothing to point at, both go to zero rather than
				// to -1.
				e := newGrid(t, 0, 2, 40, height, 100)
				e.cursorRow = 3
				e.scrollRow = 3
				e.clampCursor()
				if e.cursorRow != 0 || e.scrollRow != 0 {
					t.Errorf("clamping an empty grid left the cursor at %d/%d", e.scrollRow, e.cursorRow)
				}

				// A cursor already inside the range is left alone.
				k := newGrid(t, nRows, 2, 40, height, 100)
				k.scrollRow = 0
				k.cursorRow = 0
				k.clampCursor()
				if k.cursorRow != 0 || k.scrollRow != 0 {
					t.Errorf("clamping a cursor at the origin moved it to %d/%d", k.scrollRow, k.cursorRow)
				}
			})
		}
	}
}

// Scenario: Deshacer avisa CUANTOS cambios revirtio, y solo si hubo alguno.
//
// The count is what the status line shows, so a zero there would read as "undid
// nothing" on a row where something WAS undone. And a row with no drafts reports
// nothing at all rather than an empty message — there is nothing to tell the user.
func TestGrid_UndoReportsWhatItReverted(t *testing.T) {
	// One update on the cursor row.
	g := newGrid(t, 4, 2, 40, 12, 100)
	g.data.Rows = [][]interface{}{{"0", "zero"}, {"1", "one"}, {"2", "two"}, {"3", "three"}}
	g.pendingUpdates = []PendingUpdate{{RowIdx: 2, ColIdx: 1, OldValue: "two", NewValue: "changed"}}
	g.cursorRow = 2

	cmd, handled := g.HandleAction("undo_drafts")
	if !handled {
		t.Fatal("undo_drafts was not handled")
	}
	msg, isUndo := cmd().(GridUndoRowMsg)
	if !isUndo {
		t.Fatalf("undo_drafts produced %T, want GridUndoRowMsg", cmd())
	}
	if msg.Count != 1 {
		t.Errorf("undo_drafts reported %d changes, want 1", msg.Count)
	}
	if g.HasDrafts() {
		t.Error("the undo left drafts behind")
	}

	// Nothing on this row: handled, but no message.
	n := newGrid(t, 4, 2, 40, 12, 100)
	n.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
	n.cursorRow = 3
	cmd, handled = n.HandleAction("undo_drafts")
	if !handled {
		t.Fatal("undo_drafts on an untouched row was not handled")
	}
	if cmd != nil {
		t.Errorf("undo_drafts on an untouched row produced %T, want no message: there is nothing to report", cmd())
	}
}

// Scenario: Editar mientras ya se inserta, con el cursor en una fila real.
//
// The insert-mode continuation: the grid is still inserting but the cursor has
// moved up onto a committed row, and the editor picks up the PENDING column rather
// than the cursor's. That asymmetry is deliberate — the user is filling in one draft
// row and has not decided to leave it — but it means the editor must not read the
// cursor's column, or the draft gets values from the wrong place.
func TestGrid_EditingWhileInsertingUsesThePendingColumn(t *testing.T) {
	g := newGrid(t, 3, 3, 60, 16, 100)
	g.pendingRows = [][]interface{}{{"draft-a", "draft-b", "draft-c"}}
	g.inserting = true
	g.pendingRow = 0
	g.pendingCol = 2
	// The cursor is on a COMMITTED row, and on a different column again.
	g.cursorRow = 1
	g.cursorCol = 0

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	if !g.IsEditing() {
		t.Fatal("edit_cell did not start editing")
	}
	if g.editCol != 2 {
		t.Errorf("the editor opened column %d, want the pending column 2", g.editCol)
	}
	if g.editValue != "draft-c" {
		t.Errorf("the editor loaded %q, want the pending row's value in its own column", g.editValue)
	}
	if g.cursorRowType() == "insert" {
		t.Skip("the fixture's cursor is on the pending row, so the continuation branch is not exercised")
	}
}

// Scenario: Enter tambien avanza de celda en una fila pendiente.
//
// Enter and tab share the arm, so both have to handle the nil case. Testing only
// tab leaves one half of a shared branch uncovered — and a nil that formatted
// itself into the editor would put the text "<nil>" into a column.
func TestGrid_EnterAlsoAdvancesAcrossAPendingInsert(t *testing.T) {
	g := newGrid(t, 2, 3, 60, 16, 100)
	g.pendingRows = [][]interface{}{{"first", nil, "third"}}
	g.cursorRow = 2
	g.cursorCol = 0

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	tap(t, g, tea.KeyEnter)
	if g.editValue != "" {
		t.Errorf("enter onto a nil cell loaded %q, want the empty string", g.editValue)
	}
	if g.pendingCol != 1 {
		t.Errorf("enter left the pending column at %d, want 1", g.pendingCol)
	}

	// And a populated next cell loads its own value, so the user can see
	// what they are about to overwrite.
	g.pendingRows[0][1] = "second"
	tap(t, g, tea.KeyEnter)
	if g.editValue != "third" {
		t.Errorf("enter onto a populated cell loaded %q, want %q", g.editValue, "third")
	}
}

// Scenario: Editar una fila pendiente con la columna FUERA de la vista.
//
// The same rule as a committed row, on the other branch of the renderer: the
// pending editor's column can be outside the horizontal window, and then there is
// no cell to put the cursor in.
func TestGrid_APendingEditOutsideTheWindowDrawsAsAnOrdinaryRow(t *testing.T) {
	// Twelve columns in a 46-cell pane: four fit, so the window really has
	// to scroll rather than growing to hold everything. Eight narrow columns
	// would all fit and the case would never be reached.
	g := newGrid(t, 3, 12, 46, 12, 100)
	row := make([]interface{}, 12)
	for i := range row {
		row[i] = fmt.Sprintf("p%02d", i)
	}
	g.pendingRows = [][]interface{}{row}

	// Put the pending editor on column 0 and scroll the window past it.
	g.cursorRow = 3
	g.cursorCol = 0
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell on a pending row was not handled")
	}
	if !g.IsEditing() {
		t.Fatal("the pending row is not being edited")
	}
	typeIn(t, g, "typed")
	for range 6 {
		g.moveRight()
	}
	if g.scrollCol == 0 || g.pendingCol >= g.scrollCol {
		t.Fatalf("the window is at %d and the pending editor at %d, so the case was not reached", g.scrollCol, g.pendingCol)
	}

	if strings.Contains(ansi.Strip(g.renderRecordsView()), "typed") {
		t.Error("the in-progress value is drawn although its column is scrolled out of the window")
	}
}

// Scenario: El popup de sugerencias ocupa SUS lineas, y se cuentan.
//
// The suggestion popup is variable-height, and it sits between the filter bar and
// the column header. So both the renderer's body and the header offset have to
// agree on how many lines it took — they are two copies of the same branching, and
// a mismatch is a click that lands on the wrong row.
func TestGrid_TheSuggestionPopupIsCountedInBothPlaces(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 20, 100)
	g.startWhereFilter()
	// Suggestions are driven by CONTEXT, so the popup appears once there is
	// a column and an operator to complete — typing a bare prefix that has
	// already been matched to nothing shows no popup at all, which would
	// make the test skip for a reason that has nothing to do with the
	// height it is measuring.
	for _, ch := range "c0 " {
		g.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	popup := g.whereFilter.RenderPopup()
	if popup == "" {
		t.Skip("typing produced no suggestions, so the popup has no height to count")
	}
	popupLines := strings.Count(popup, "\n") + 1

	offset := g.recordsHeaderOffset()
	if want := 1 + popupLines; offset != want {
		t.Errorf("the header offset is %d with a %d-line popup, want %d", offset, popupLines, want)
	}

	out := resultOf(g.renderRecordsView())
	// The header really is where the offset says it is, so the two copies
	// of the branching agree.
	if got := ansi.Strip(out[offset]); !strings.Contains(got, "C0") {
		t.Errorf("line %d is %q, want the column header; the popup is %d lines and the offset says %d", offset, got, popupLines, offset)
	}

	// And closing the popup takes its lines back.
	g.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := g.recordsHeaderOffset(); got != 1 {
		t.Errorf("after closing the popup the header offset is %d, want 1", got)
	}
	if strings.Contains(ansi.Strip(g.renderRecordsView()), "p0") {
		t.Error("the closed popup is still drawn")
	}
}

// ---------------------------------------------------------------------------
// Which cell is highlighted
// ---------------------------------------------------------------------------

// paint is the pair of colours a run of text is drawn in. An unset colour is the
// terminal default, so it is distinguishable from any real colour.
type paint struct{ fg, bg string }

func (p paint) String() string { return p.fg + "|" + p.bg }

// sgrParams returns the parameters of an escape sequence, with the introducer and
// the terminator removed.
func sgrParams(sgr string) []string {
	// "\x1b[m" carries no parameters and means RESET, which is not the same
	// as an empty parameter list to be ignored: lipgloss emits it between the
	// pieces of a padded cell, so reading it as "no change" makes every cell
	// after the first inherit its neighbour's colours.
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m"), ";")
}

// apply folds one escape sequence into the paint in force over the text after it.
//
// Only what these cell styles actually use is interpreted — resets, the extended
// foreground and background forms, and the "default colour" codes — because
// anything else would have to be carried through unchanged to stay comparable.
func apply(p paint, sgr string) paint {
	params := sgrParams(sgr)
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case "", "0":
			p = paint{}
		case "39":
			p.fg = ""
		case "49":
			p.bg = ""
		case "38", "48":
			if i+4 < len(params) && params[i+1] == "2" {
				c := params[i+2] + "," + params[i+3] + "," + params[i+4]
				if params[i] == "38" {
					p.fg = c
				} else {
					p.bg = c
				}
				i += 4
				continue
			}
		}
	}
	return p
}

// paintOf returns the colours a style paints a cell with.
//
// Derived from the THEME rather than written out as literals, so a theme change
// cannot quietly turn every assertion here into one that compares the wrong thing
// and passes.
func paintOf(style lipgloss.Style) paint {
	rendered := style.Render("X")
	end := strings.Index(rendered, "X")
	if end < 0 {
		return paint{}
	}
	p := paint{}
	for _, tk := range tokenise(rendered[:end]) {
		if tk.sgr != "" {
			p = apply(p, tk.sgr)
		}
	}
	return p
}

// token splits a rendered line into escape sequences and the plain text between
// them, in order.
type tok struct{ sgr, text string }

func tokenise(line string) []tok {
	var toks []tok
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && line[j] != 'm' {
				j++
			}
			if j >= len(line) {
				break
			}
			toks = append(toks, tok{sgr: line[i : j+1]})
			i = j + 1
			continue
		}
		start := i
		for i < len(line) && line[i] != 0x1b {
			i++
		}
		toks = append(toks, tok{text: line[start:i]})
	}
	return toks
}

// cells returns the display text of every run on a rendered row together with the
// colours it was painted in, left to right.
//
// THIS IS THE OBSERVATION THAT MATTERS for the row renderer, and it is
// deliberately not a comparison against a second render of the same values. A
// differential assertion cannot say WHICH cell is highlighted: a renderer that lit
// the wrong index still varies with activeCol and still produces different bytes,
// so "the output changed" passes it. Reading each cell's colours off the escape
// sequences says exactly which cell the renderer chose, which is the whole question.
//
// The colours are accumulated across escapes rather than read from a single one,
// because lipgloss paints a padded cell in pieces: the leading pad, the value and
// the trailing pad each get their own sequence.
func cells(line string) [][2]any {
	var out [][2]any
	p := paint{}
	for _, tk := range tokenise(line) {
		if tk.sgr != "" {
			p = apply(p, tk.sgr)
			continue
		}
		if v := strings.TrimSpace(tk.text); v != "" {
			out = append(out, [2]any{v, p})
		}
	}
	return out
}

// inStyle returns the display text of every cell on a line painted in this style's
// colours.
func inStyle(line string, style lipgloss.Style) []string {
	want := paintOf(style)
	var out []string
	for _, c := range cells(line) {
		if c[1].(paint) == want {
			out = append(out, c[0].(string))
		}
	}
	return out
}

// rowLine returns one raw row line of the render, by the row's data index.
func rowLine(t *testing.T, g *Grid, rowIdx int) string {
	t.Helper()
	out := resultOf(g.renderRecordsView())
	head := g.recordsHeaderOffset()
	block := out[head+1:]
	if len(block) <= rowIdx {
		t.Fatalf("the render has %d row lines, wanted row %d:\n%s", len(block), rowIdx, strings.Join(out, "\n"))
	}
	return block[rowIdx]
}

// Scenario: La celda marcada es la del cursor, y solo esa.
//
// The grid cursor is the user's position in the data, and the cell renderer is the
// only thing that draws it. Getting the column wrong is invisible in a screenshot
// of a row with one candidate and obvious the moment the user presses right.
//
// So the assertion is positional and reads each cell's background off the row: with
// the cursor on column 1 of a three-column grid, exactly one cell is painted in the
// selection colour and it holds the value of column 1.
func TestGrid_ExactlyTheCursorColumnIsHighlighted(t *testing.T) {
	for _, col := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("cursor on column %d", col), func(t *testing.T) {
			h := newGrid(t, 3, 3, 40, 14, 100)
			h.cursorRow = 1
			h.cursorCol = col
			sel := paintOf(h.styles.Selected)

			got := inStyle(rowLine(t, h, 1), h.styles.Selected)
			if len(got) != 1 {
				t.Fatalf("the cursor row paints %v in the selection colour, want exactly one cell", got)
			}
			if got[0] != marker(1, col) {
				t.Errorf("the highlighted cell holds %q, want the cursor column's value %q", got[0], marker(1, col))
			}
			// And no other cell on that row carries a background, so the
			// highlight is unambiguous.
			for _, c := range cells(rowLine(t, h, 1)) {
				cp := c[1].(paint)
				if cp.bg != "" && cp.bg != sel.bg {
					t.Errorf("cell %q is painted on %s, so only the cursor cell is not distinguished", c[0], cp)
				}
			}
			// And the row above, which has no cursor, has none at all.
			if other := inStyle(rowLine(t, h, 0), h.styles.Selected); len(other) != 0 {
				t.Errorf("the row above the cursor paints %v in the selection colour, want nothing", other)
			}
		})
	}

	// With the window scrolled right the highlight follows the CURSOR, not
	// the window start — so a renderer that forgot to subtract scrollCol
	// would paint whatever column landed at index 0.
	//
	// The names have to be long enough that the columns do not all fit: with
	// one-character names every column is six cells and seven of them fit,
	// so the window never scrolls and the case is never reached.
	w := newGrid(t, 3, 12, 46, 14, 100)
	for i := range w.columns {
		w.columns[i] = fmt.Sprintf("column_%02d", i)
		w.data.Columns[i].Name = w.columns[i]
	}
	w.header.SetColumns(w.columns)
	w.calculateWidths()
	w.cursorRow = 0
	// Six presses, not one: the cursor is moved with the grid's own movement
	// so the window follows it exactly as it would for a user. Setting the
	// cursor directly and then pressing six times would wrap it back to
	// column zero and reset the window on the way.
	for range 6 {
		w.moveRight()
	}
	if w.scrollCol == 0 {
		t.Fatalf("the window did not scroll (cursor at column %d), so the case was not reached", w.cursorCol)
	}
	got := inStyle(rowLine(t, w, 0), w.styles.Selected)
	if len(got) != 1 || got[0] != marker(0, w.cursorCol) {
		t.Errorf("with the window at column %d and the cursor at %d the highlight is %v, want just %q",
			w.scrollCol, w.cursorCol, got, marker(0, w.cursorCol))
	}

	// A row marked for deletion paints the cell under the cursor in its own
	// "selected while deleted" colour, so the user can still see where they
	// are about to discard.
	d := newGrid(t, 3, 3, 40, 14, 100)
	d.cursorRow = 1
	d.cursorCol = 2
	d.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{marker(1, 0), marker(1, 1), marker(1, 2)}}}
	if got := inStyle(rowLine(t, d, 1), d.styles.DraftDeleteSelected); len(got) != 1 || got[0] != marker(1, 2) {
		t.Errorf("a row marked for deletion paints %v as selected-while-deleted, want just the cursor cell %q", got, marker(1, 2))
	}
}

// Scenario: Una fila multi-seleccionada se pinta ENTERA, no celda a celda.
//
// This is the one place where the grid deliberately does NOT say which cell the
// cursor is on: a multi-selected row is one thing being acted on, so every cell in
// it is painted the same. The consequence worth stating is that the row's renderer
// does not read the column at all — which is why its activeCol parameter is
// deleted rather than pinned (see the note on that below).
func TestGrid_AMultiSelectedRowIsPaintedWhole(t *testing.T) {
	for _, cursorRow := range []int{0, 1} {
		t.Run(fmt.Sprintf("cursor on row %d", cursorRow), func(t *testing.T) {
			g := newGrid(t, 3, 3, 40, 14, 100)
			g.cursorRow = cursorRow
			g.selectedRows[1] = true

			// Whether the cursor is on the row or not, a selected row away
			// from the cursor is painted entirely.
			away := rowLine(t, g, 1)
			if cursorRow == 1 {
				return // covered by the cursor-on-a-selected case below
			}
			got := inStyle(away, g.styles.Cursor)
			if len(got) != 3 {
				t.Errorf("a multi-selected row paints %v in the cursor colour, want all three cells", got)
			}
			if inStyle(away, g.styles.Selected) != nil {
				t.Error("a multi-selected row away from the cursor also paints a cell in the selection colour")
			}
		})
	}

	// The cursor ON a selected row is a different rendering: exactly one cell
	// is painted as selected and the rest as the row colour.
	g := newGrid(t, 3, 3, 40, 14, 100)
	g.cursorRow = 1
	g.cursorCol = 2
	g.selectedRows[1] = true
	line := rowLine(t, g, 1)
	got := inStyle(line, g.styles.Selected)
	if len(got) != 1 || got[0] != marker(1, 2) {
		t.Errorf("a cursor on a multi-selected row paints %v as selected, want just the cursor cell", got)
	}
	if rest := inStyle(line, g.styles.Cursor); len(rest) != 2 {
		t.Errorf("the other cells of that row are %v, want the other two", rest)
	}

	// And that rendering is its own thing, distinct from a plain row.
	plain := newGrid(t, 3, 3, 40, 14, 100)
	plain.cursorRow = 0
	if line == rowLine(t, plain, 1) {
		t.Error("a cursor on a multi-selected row renders exactly like a plain row")
	}
}

// Scenario: Una celda con un cambio pendiente lleva SU PROPIA marca, no la del cursor.
//
// The draft marker is per COLUMN: the user has edited one field of a row and needs
// to see which one. Three cases, and the third is the one that would be missed by
// a test that only ever puts the cursor on a plain row: the marked cell under the
// cursor takes a THIRD colour, distinct from both the marked-cell colour and the
// cursor colour, because "edited and where you are" has to be visible at once.
func TestGrid_APendingUpdateMarksItsOwnColumn(t *testing.T) {
	styles := func() *theme.Styles { return theme.Resolve("dark").Styles() }
	_ = styles

	for _, col := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("update on column %d", col), func(t *testing.T) {
			g := newGrid(t, 3, 3, 40, 14, 100)
			g.cursorRow = 0
			g.pendingUpdates = []PendingUpdate{{RowIdx: 1, ColIdx: col, OldValue: "x", NewValue: "y"}}
			line := rowLine(t, g, 1)

			marked := inStyle(line, g.styles.DraftUpdate)
			if len(marked) != 1 || marked[0] != marker(1, col) {
				t.Errorf("the row marks %v as changed, want just %q", marked, marker(1, col))
			}
			// An update on one row leaves the others unmarked.
			if other := inStyle(rowLine(t, g, 0), g.styles.DraftUpdate); len(other) != 0 {
				t.Errorf("a row with no pending update marks %v as changed", other)
			}
		})
	}

	// The cursor ON the marked cell: a third colour, so both facts show.
	g := newGrid(t, 3, 3, 40, 14, 100)
	g.cursorRow = 1
	g.cursorCol = 2
	g.pendingUpdates = []PendingUpdate{{RowIdx: 1, ColIdx: 2, OldValue: "x", NewValue: "y"}}
	line := rowLine(t, g, 1)
	both := inStyle(line, g.styles.DraftUpdateSelected)
	if len(both) != 1 || both[0] != marker(1, 2) {
		t.Errorf("the changed cell under the cursor paints %v as changed-and-selected, want just %q", both, marker(1, 2))
	}
	if inStyle(line, g.styles.Selected) != nil {
		t.Error("a changed cell under the cursor also paints as plain-selected: the two states are not distinguishable")
	}

	// The cursor on a DIFFERENT column of a marked row: the cursor cell is
	// plain-selected and the marked cell keeps its own colour, which is the
	// only reason the per-column map exists.
	h := newGrid(t, 3, 3, 40, 14, 100)
	h.cursorRow = 1
	h.cursorCol = 0
	h.pendingUpdates = []PendingUpdate{{RowIdx: 1, ColIdx: 2, OldValue: "x", NewValue: "y"}}
	line = rowLine(t, h, 1)
	if got := inStyle(line, h.styles.Selected); len(got) != 1 || got[0] != marker(1, 0) {
		t.Errorf("with the cursor on column 0 the selection colour is on %v, want just %q", got, marker(1, 0))
	}
	if got := inStyle(line, h.styles.DraftUpdate); len(got) != 1 || got[0] != marker(1, 2) {
		t.Errorf("the change marker is on %v, want just %q — the marker follows the column, not the cursor", got, marker(1, 2))
	}
}

// Scenario: Una insercion pendiente se pinta ENTERA, y la seleccionada tiene otro color.
//
// Like a multi-selected row, a draft insert is one thing rather than a grid of
// cells, so its row renderer does not read a column either. What it DOES read is
// whether the cursor is on it: a selected insert is a different colour from an
// unselected one, which is how the user finds the row they are filling in among a
// list of drafts.
func TestGrid_APendingInsertIsPaintedWholeAndChangesColourWhenSelected(t *testing.T) {
	insertLine := func(g *Grid, idx int) string {
		out := resultOf(g.renderRecordsView())
		block := out[g.recordsHeaderOffset()+1:]
		return block[len(g.data.Rows)+idx]
	}

	for _, col := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("cursor on column %d", col), func(t *testing.T) {
			g := newGrid(t, 2, 3, 40, 14, 100)
			g.pendingRows = [][]interface{}{{"p0", "p1", "p2"}}
			g.cursorRow = 2
			g.cursorCol = col

			if got := g.cursorRowType(); got != "insert" {
				t.Fatalf("the fixture's cursor is on a %q row", got)
			}
			line := insertLine(g, 0)
			// Selected: the whole row in the selected colour, and none of
			// it in the unselected one.
			if got := inStyle(line, g.styles.DraftInsertSelected); len(got) != 3 {
				t.Errorf("the selected insert paints %v, want all three cells", got)
			}
			if got := inStyle(line, g.styles.DraftInsert); len(got) != 0 {
				t.Errorf("the selected insert also paints %v as unselected", got)
			}
		})
	}

	// Not selected: the other colour.
	two := newGrid(t, 1, 3, 40, 14, 100)
	two.pendingRows = [][]interface{}{{"a0", "a1", "a2"}, {"b0", "b1", "b2"}}
	two.cursorRow = 1 // the FIRST insert
	two.cursorCol = 1
	if got := inStyle(insertLine(two, 0), two.styles.DraftInsertSelected); len(got) != 3 {
		t.Errorf("the insert under the cursor paints %v as selected, want all three", got)
	}
	if got := inStyle(insertLine(two, 1), two.styles.DraftInsert); len(got) != 3 {
		t.Errorf("the insert the cursor is not on paints %v, want all three", got)
	}
	if got := inStyle(insertLine(two, 1), two.styles.DraftInsertSelected); len(got) != 0 {
		t.Errorf("the insert the cursor is not on paints %v as selected", got)
	}
}

// Scenario: La fila del cursor se distingue de las demas por su fondo.
//
// The cursor is the one row the user acts on, and the app reads the cursor's
// position to decide what a keystroke does — so it has to be visible without
// reading any numbers. A row with no cursor carries no background at all, which is
// what makes the cursor findable by eye and is why a mutation that painted every
// row alike would be caught here.
func TestGrid_ARowWithoutTheCursorCarriesNoBackground(t *testing.T) {
	for _, cursorRow := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("cursor on row %d", cursorRow), func(t *testing.T) {
			g := newGrid(t, 3, 3, 40, 14, 100)
			g.cursorRow = cursorRow
			for row := range 3 {
				got := inStyle(rowLine(t, g, row), g.styles.Selected)
				if row == cursorRow {
					if len(got) != 1 {
						t.Errorf("the cursor row paints %v in the selection colour, want exactly one cell", got)
					}
					continue
				}
				if len(got) != 0 {
					t.Errorf("row %d is not the cursor row but paints %v in the selection colour", row, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The boundaries, one fixture per boundary
// ---------------------------------------------------------------------------

// Scenario: Recargar la MISMA tabla conserva el filtro y el orden.
//
// SetData resets the cursor, the scroll and every draft — a new result means a new
// grid — but the WHERE clause and the sort are the user's standing choices about
// that table, so reloading it keeps them. Reloading a DIFFERENT table clears them,
// because carrying a filter over to an unrelated table would silently show the
// wrong rows.
//
// The branch is `tableName != table`, and the case that decides it is the same name
// twice: a test that only ever loads different tables never evaluates the true side.
func TestGrid_ReloadingTheSameTableKeepsTheFilterAndSort(t *testing.T) {
	g := newGrid(t, 3, 2, 40, 12, 100)
	g.whereClause = "id > 1"
	g.toggleSort()

	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "c0"}, {Name: "c1"}},
		Rows:    [][]interface{}{{"x", "y"}},
		Count:   1,
	}, "public", "t")

	if g.WhereClause() != "id > 1" {
		t.Errorf("reloading the same table dropped the WHERE clause, leaving %q", g.WhereClause())
	}
	if g.SortColumn() == "" {
		t.Error("reloading the same table dropped the sort column")
	}
	// And the metadata is kept, because it describes this table.
	if len(g.ForeignKeys()) != 0 {
		t.Errorf("reloading the same table kept %d foreign keys it had none of", len(g.ForeignKeys()))
	}

	// A different table drops both.
	h := newGrid(t, 3, 2, 40, 12, 100)
	h.whereClause = "id > 1"
	h.toggleSort()
	h.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "c0"}, {Name: "c1"}},
		Rows:    [][]interface{}{{"x", "y"}},
		Count:   1,
	}, "public", "other")

	if h.WhereClause() != "" {
		t.Errorf("loading a different table kept the WHERE clause %q", h.WhereClause())
	}
	if h.SortColumn() != "" {
		t.Errorf("loading a different table kept the sort on %q", h.SortColumn())
	}
}

// Scenario: Las columnas marcadas son las de la constraint, y las demas no.
//
// The key icons are the * marker in the header and the +2 of column width the icons
// reserve. A column marked because it is part of a UNIQUE constraint would be a lie
// about the table's key, and one that is marked twice is double-counted in the
// width. The filter is on the constraint TYPE, compared exactly, which is why a
// lower-case spelling is tested too.
func TestGrid_KeyIconsFollowTheConstraintTypes(t *testing.T) {
	g := newGrid(t, 2, 4, 40, 12, 100)
	g.SetMetadata(
		[]postgres.ConstraintInfo{
			{Type: "PRIMARY KEY", Columns: "c0"},
			{Type: "UNIQUE", Columns: "c1"},
			{Type: "FOREIGN KEY", Columns: "c2"},
			{Type: "PRIMARY KEY", Columns: " c0 , c3 "},
		},
		[]postgres.ForeignKeyInfo{{Column: "c1", RefTable: "users", RefColumn: "id"}},
		nil,
	)

	if g.keyIcons[0] != KeyPK {
		t.Errorf("the primary key column is marked %v, want a PK icon", g.keyIcons[0])
	}
	if g.keyIcons[3] != KeyPK {
		t.Errorf("the second half of a composite key is marked %v, want a PK icon", g.keyIcons[3])
	}
	// A foreign key wins over nothing, and is keyed on the COLUMN NAME.
	if g.keyIcons[1] != KeyFK {
		t.Errorf("the referencing column is marked %v, want an FK icon even though it is also UNIQUE", g.keyIcons[1])
	}
	// A FOREIGN KEY constraint type does not mark the column as a key; only
	// the foreignKeys list does.
	if _, marked := g.keyIcons[2]; marked {
		t.Error("a column was marked from a FOREIGN KEY constraint type; keys come from PRIMARY KEY constraints and from the foreign key list")
	}

	// No metadata at all marks nothing.
	plain := newGrid(t, 2, 2, 40, 12, 100)
	plain.SetMetadata(nil, nil, nil)
	if len(plain.keyIcons) != 0 {
		t.Errorf("a grid with no metadata has %d key icons", len(plain.keyIcons))
	}

	// And a key icon reserves two cells of column width. The name has to be
	// long enough to be above the six-cell floor, because the floor is
	// applied AFTER the icon's allowance — so on a one-character column the
	// icon's two cells are absorbed by it and nothing is observable.
	styled := New(theme.Resolve("dark").Styles(), 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	styled.SetWidth(400)
	styled.SetHeight(12)
	styled.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "identifier"}},
		Rows:    [][]interface{}{{"v"}},
		Count:   1,
	}, "public", "t")
	plainWidth := styled.widths[0]
	if plainWidth != len("identifier")+2 {
		t.Fatalf("the unmarked column is %d cells, want %d", plainWidth, len("identifier")+2)
	}
	styled.SetMetadata([]postgres.ConstraintInfo{{Type: "PRIMARY KEY", Columns: "identifier"}}, nil, nil)
	if styled.widths[0] != plainWidth+2 {
		t.Errorf("marking the column as a key left its width at %d, want %d — two more cells for the icon",
			styled.widths[0], plainWidth+2)
	}
}

// Scenario: El ancho de columna lo fija su NOMBRE cuando el nombre es lo mas ancho.
//
// The width is the widest of the name and the values, so a fixture whose values are
// all one character pins the name half of the rule and nothing else. A name long
// enough to dominate is the other half, and it is a different constant: four cells
// for a value, two for a name.
func TestGrid_ALongNameSetsTheColumnWidthOnItsOwn(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(400) // wide enough that the per-column cap does not interfere
	g.SetHeight(12)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "n"}, {Name: "created_at_millis"}},
		Rows:    [][]interface{}{{"v", "w"}},
		Count:   1,
	}, "public", "t")

	if g.widths[0] != 6 {
		t.Errorf("a one-character name with a one-character value is %d cells, want the floor of 6", g.widths[0])
	}
	// 19 characters of name plus two of padding.
	if want := len("created_at_millis") + 2; g.widths[1] != want {
		t.Errorf("the long-named column is %d cells, want %d — the name plus two, with the values too short to matter", g.widths[1], want)
	}
}

// Scenario: Un panel de DOS celdas no acota los anchos, y la ventana no se mueve.
//
// available is the pane minus the two border cells, and at a pane of two it is zero.
// Two things then have to hold that the rest of the code does not quietly break:
// no column may be capped (there is no room to divide), and the scroll window must
// give up rather than trying to fit a cursor into nothing.
//
// Both are boundary cases at available == 0, which is the value a test that only
// uses realistic pane sizes never reaches.
func TestGrid_APaneWithNoRoomNeitherCapsNorScrolls(t *testing.T) {
	g := newGrid(t, 2, 4, 2, 12, 100)
	if g.width-2 != 0 {
		t.Fatalf("the fixture's pane leaves %d cells, not 0", g.width-2)
	}
	// The cap block is skipped, so the columns keep the widths the scan gave
	// them. A cap at available==0 would divide by a floor of ten and clamp
	// every column to ten.
	for i, w := range g.widths {
		if w == 10 {
			t.Errorf("column %d was capped to ten in a pane with no room at all", i)
		}
	}
	if len(g.widths) == 0 {
		t.Error("a pane with no room left no column widths at all")
	}

	// And the window gives up rather than trying to place the cursor.
	g.cursorCol = 3
	g.syncScroll()
	if g.scrollCol != 0 {
		t.Errorf("a pane with no room left the window at column %d, want 0", g.scrollCol)
	}

	// A pane one cell wide is the same case on the other side of zero.
	one := newGrid(t, 2, 4, 1, 12, 100)
	one.scrollCol = 2
	one.syncScroll()
	if one.scrollCol != 0 {
		t.Errorf("a one-cell pane left the window at column %d", one.scrollCol)
	}
}

// Scenario: Un indice EXACTAMENTE fuera del rango se deja en el ultimo, no se pasa.
//
// The clamps are `< len` / `>= len`, and the value that distinguishes them from
// `<= len` / `> len` is the index EQUAL to the count. A fixture that puts the
// cursor at count+3 exercises the same line but not the same comparison.
func TestGrid_AnIndexExactlyOnePastTheEndIsClampedToTheLast(t *testing.T) {
	for _, nCols := range []int{1, 3, 5} {
		t.Run(fmt.Sprintf("%d columns", nCols), func(t *testing.T) {
			g := newGrid(t, 2, nCols, 40, 12, 100)
			// Every column is wider than the pane, so the window cannot
			// refill from the left afterwards. That matters: with narrow
			// columns the expand-left loop slides the window all the way
			// back to zero whatever the clamps did, and the clamps become
			// invisible — a fixture that passes for the wrong reason.
			g.widths = make([]int, nCols)
			for i := range g.widths {
				g.widths[i] = 40
			}
			g.cursorCol = nCols // exactly one past the end
			g.scrollCol = nCols
			g.syncScroll()

			if g.cursorCol != nCols-1 {
				t.Errorf("a cursor one past the end settled at column %d, want %d", g.cursorCol, nCols-1)
			}
			if g.scrollCol != nCols-1 {
				t.Errorf("a window one past the end settled at column %d, want %d", g.scrollCol, nCols-1)
			}
			if g.scrollCol > g.cursorCol {
				t.Errorf("the window at %d starts after the cursor at %d", g.scrollCol, g.cursorCol)
			}
		})
	}

	// A cursor to the RIGHT of the window is what pulls the window along: the
	// clamp on scrollCol is not enough, there is a second rule for the case
	// where both indices are in range but the cursor is off the left edge.
	h := newGrid(t, 2, 6, 40, 12, 100)
	h.widths = []int{40, 40, 40, 40, 40, 40} // nothing fits, so the window follows
	h.cursorCol = 4
	h.scrollCol = 1
	h.syncScroll()
	if h.scrollCol > h.cursorCol {
		t.Errorf("the window at %d starts after the cursor at %d", h.scrollCol, h.cursorCol)
	}

	// And with the cursor exactly ON the window's first column, the rule
	// fires to the same place — which is what distinguishes it from a
	// comparison written the other way round.
	e := newGrid(t, 2, 6, 40, 12, 100)
	e.widths = []int{40, 40, 40, 40, 40, 40}
	e.cursorCol = 2
	e.scrollCol = 2
	e.syncScroll()
	if e.scrollCol != 2 {
		t.Errorf("with the cursor on the window's edge the window moved to %d, want it left at 2", e.scrollCol)
	}
}

// Scenario: El alto disponible se respeta EXACTAMENTE, sin encoger ni repartir.
//
// The shrink loop stops as soon as the span fits, and the expand loop pulls columns
// back in only while they still fit. Both boundaries — one cell too wide and one
// cell exactly right — are values only a fixture built to that arithmetic reaches,
// which is why the widths here are chosen rather than measured.
func TestGrid_TheVisibleSpanIsExactlyAsWideAsThePaneAllows(t *testing.T) {
	// Four columns of eight cells in a 34-cell pane: available is 32, so
	// three fit exactly and the fourth does not.
	build := func() *Grid {
		g := newGrid(t, 2, 4, 34, 12, 100)
		g.widths = []int{8, 8, 8, 8}
		g.cursorCol = 3
		return g
	}
	g := build()
	g.syncScroll()

	// The cursor's column must be inside, and the span up to it must fit.
	used := 0
	for i := g.scrollCol; i <= g.cursorCol; i++ {
		used += g.widths[i]
	}
	if used > 32 {
		t.Errorf("the span from column %d to the cursor at %d needs %d cells in a 32-cell pane", g.scrollCol, g.cursorCol, used)
	}
	// And one more column must NOT fit, or the window is one short.
	if g.scrollCol+1 <= g.cursorCol {
		if next := used + g.widths[g.cursorCol-1]; next <= 32 {
			t.Errorf("the window stopped at column %d although column %d would fit (%d <= 32)", g.scrollCol, g.scrollCol-1, next)
		}
	}

	// One cell too wide: the window has to give up a column rather than
	// letting the cursor fall off the right edge.
	tight := build()
	tight.widths = []int{11, 11, 11, 11} // 33 > 32 even for two
	tight.syncScroll()
	used = 0
	for i := tight.scrollCol; i <= tight.cursorCol; i++ {
		used += tight.widths[i]
	}
	if used > 32 {
		t.Errorf("with eleven-cell columns the span to the cursor needs %d cells in a 32-cell pane", used)
	}
}

// Scenario: La tecla F9 tambien salta de pagina.
//
// goto_page_9 is the last of nine digit actions, and the loop that finds them stops
// at nine. A test that only jumps to F1..F4 cannot tell a loop of nine from a loop
// of four, and the ninth page is exactly the one a long result set needs.
func TestGrid_TheNinthPageIsReachable(t *testing.T) {
	g := newGrid(t, 100, 2, 40, 12, 10) // ten pages of ten rows

	if got := g.pager.TotalPages(); got != 10 {
		t.Fatalf("the fixture has %d pages, want 10", got)
	}
	for _, page := range []config.ActionID{"goto_page_1", "goto_page_5", "goto_page_9"} {
		if _, handled := g.HandleAction(page); !handled {
			t.Errorf("%s was not handled", page)
		}
		if got := g.pager.Page(); got != int(page[len("goto_page_")]-'0') {
			t.Errorf("%s left the grid on page %d", page, got)
		}
		// The page jump resets the scroll, because the new page has its
		// own window and keeping the old offset would land the cursor
		// past its end.
		if g.scrollRow != 0 {
			t.Errorf("%s left the scroll at %d", page, g.scrollRow)
		}
	}

	// A page past the end lands on the last one rather than off it.
	over := newGrid(t, 100, 2, 40, 12, 10)
	over.HandleAction("goto_page_9")
	if _, handled := over.HandleAction("last_page"); !handled {
		t.Fatal("last_page was not handled")
	}
	if got := over.pager.Page(); got != 10 {
		t.Errorf("last_page landed on page %d, want 10", got)
	}
}

// Scenario: Editar una fila pendiente DIBUJA el editor en ESA fila.
//
// editRow for a pending insert is the number of committed rows plus the insert's
// index, because the render locates the edit by subtracting the first visible row
// from it and comparing against the count of rows drawn so far. An off-by-one puts
// the in-progress value on the row ABOVE, where the user is not editing anything —
// a value typed into one draft row appearing in another.
func TestGrid_APendingEditIsDrawnOnThePendingRowItself(t *testing.T) {
	for _, nPending := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("%d pending rows", nPending), func(t *testing.T) {
			g := newGrid(t, 3, 3, 40, 16, 100)
			for i := range nPending {
				g.pendingRows = append(g.pendingRows, []interface{}{fmt.Sprintf("p%da", i), fmt.Sprintf("p%db", i), fmt.Sprintf("p%dc", i)})
			}
			g.cursorRow = 3 + nPending - 1
			g.cursorCol = 1

			if _, handled := g.HandleAction("edit_cell"); !handled {
				t.Fatal("edit_cell was not handled")
			}
			typeIn(t, g, "ZZ")
			if g.editValue != fmt.Sprintf("p%dbZZ", nPending-1) {
				t.Fatalf("the editor holds %q, want the pending row's value with the typing on the end", g.editValue)
			}

			out := resultOf(g.renderRecordsView())
			block := out[g.recordsHeaderOffset()+1:]
			// The typing appears on the LAST pending row's line and on no
			// other line at all.
			line := block[3+nPending-1]
			if !strings.Contains(ansi.Strip(line), "ZZ") {
				t.Errorf("the last pending row's line is %q, want it to carry the in-progress value", ansi.Strip(line))
			}
			for i, l := range block {
				if i == 3+nPending-1 {
					continue
				}
				if strings.Contains(ansi.Strip(l), "ZZ") {
					t.Errorf("row line %d also carries the in-progress value:\n%s", i, strings.Join(out, "\n"))
				}
			}
			// And a committed row below the pending ones does not exist,
			// which is why the offset is what it is.
			if g.editRow != 3+nPending-1 {
				t.Errorf("editRow is %d, want %d — it has to land on the pending row's position", g.editRow, 3+nPending-1)
			}
		})
	}
}

// Scenario: Editar la PRIMERA fila funciona: el indice cero no es "fuera de rango.
//
// The guard is `absRow < 0`, and the boundary that matters is zero — a guard written
// `<= 0` would refuse to edit the first row of every table, which is the row a user
// reaches first.
func TestGrid_TheFirstRowCanBeEdited(t *testing.T) {
	for _, col := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("row 0 column %d", col), func(t *testing.T) {
			g := newGrid(t, 3, 3, 40, 16, 100)
			g.cursorRow = 0
			g.cursorCol = col

			if _, handled := g.HandleAction("edit_cell"); !handled {
				t.Fatal("edit_cell on the first row was not handled")
			}
			if !g.IsEditing() {
				t.Fatal("the first row did not enter edit mode")
			}
			if g.editRow != 0 {
				t.Errorf("the editor is on row %d, want 0", g.editRow)
			}
			if g.editCol != col {
				t.Errorf("the editor is on column %d, want %d", g.editCol, col)
			}
			if g.editValue != marker(0, col) {
				t.Errorf("the editor loaded %q, want the first row's value %q", g.editValue, marker(0, 0))
			}

			typeIn(t, g, "Q")
			tap(t, g, tea.KeyEscape)
			if len(g.pendingUpdates) != 1 {
				t.Fatalf("editing the first row recorded %d updates, want 1", len(g.pendingUpdates))
			}
			if got := g.pendingUpdates[0].RowIdx; got != 0 {
				t.Errorf("the pending update is for row %d, want 0", got)
			}
		})
	}
}

// Scenario: La busqueda de columna deja la vista dos columnas ANTES del resultado.
//
// The jump puts the matched column two from the left edge, so the reader can see
// what it is next to. A window that stopped exactly on the match would be a worse
// place to land, and a window that stopped further left is a wasted pane.
//
// The boundary is column index 2: subtracting two lands on zero, which is the clamp
// doing its job, and subtracting three would land on zero one column early for a
// match at index 2.
func TestGrid_TheColumnFinderLeavesContextToTheLeft(t *testing.T) {
	for _, target := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("jump to column %d", target), func(t *testing.T) {
			g := newGrid(t, 2, 8, 40, 12, 100)
			g.columns = []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh"}
			g.header.SetColumns(g.columns)
			g.widths = []int{8, 8, 8, 8, 8, 8, 8, 8} // two fit in a 38-cell pane
			g.calculateWidths()

			g.jumpToColumn(target)
			if g.cursorCol != target {
				t.Fatalf("the cursor is on column %d, want %d", g.cursorCol, target)
			}
			// Whatever syncScroll then does, the match must be visible
			// and the window must not start more than two columns
			// before it.
			if g.scrollCol > target-2 && g.scrollCol > 0 {
				t.Errorf("the window starts at %d for a match at %d, want at most two columns of context", g.scrollCol, target)
			}
			cols, _ := g.visibleColumns()
			found := false
			for _, c := range cols {
				if c == g.columns[target] {
					found = true
				}
			}
			if !found {
				t.Errorf("after jumping to column %d the visible columns are %v, which does not include it", target, cols)
			}

			// The clamp: a match near the left edge cannot show two
			// columns of context, so the window starts at zero.
			g.jumpToColumn(0)
			if g.scrollCol != 0 {
				t.Errorf("jumping to the first column left the window at %d", g.scrollCol)
			}
		})
	}
}

// Scenario: La clave ajena se puede seguir DESDE la primera columna.
//
// The guard is `cursorCol < 0`, so the boundary is column zero. A guard written
// `<= 0` would make the first column of every table impossible to follow — and the
// first column is very often the key.
func TestGrid_AForeignKeyCanBeFollowedFromTheFirstColumn(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	g.SetMetadata(
		[]postgres.ConstraintInfo{{Type: "PRIMARY KEY", Columns: "c0"}},
		[]postgres.ForeignKeyInfo{{Column: "c0", RefTable: "parents", RefColumn: "id"}},
		nil,
	)
	g.cursorRow = 1
	g.cursorCol = 0

	cmd, ok := g.startFK()
	if !ok {
		t.Fatal("a foreign key in the FIRST column could not be followed")
	}
	msg := cmd().(GridNavigateFKMsg)
	if msg.FKColumn != "c0" {
		t.Errorf("the message names column %q, want c0", msg.FKColumn)
	}
	// And the value is the CURSOR row's, which with scroll and page
	// offsets is not row zero.
	if msg.FKValue != marker(1, 0) {
		t.Errorf("the message carries %#v, want the cursor row's value %q", msg.FKValue, marker(1, 0))
	}
	if msg.RefTable != "parents" {
		t.Errorf("the message names table %q, want parents", msg.RefTable)
	}

	// On a later page, with the scroll moved: all three offsets add up.
	h := newGrid(t, 20, 3, 40, 12, 5)
	h.SetMetadata(nil, []postgres.ForeignKeyInfo{{Column: "c2", RefTable: "parents", RefColumn: "id"}}, nil)
	h.pager.GoToPage(3) // offset 10
	h.scrollRow = 2
	h.cursorRow = 1
	h.cursorCol = 2
	cmd, ok = h.startFK()
	if !ok {
		t.Fatal("could not follow a foreign key on a later page")
	}
	if got := cmd().(GridNavigateFKMsg).FKValue; got != marker(13, 2) {
		t.Errorf("with page offset 10, scroll 2 and cursor 1 the value is %#v, want %q", got, marker(13, 2))
	}
}

// Scenario: Las filas visibles son las que QUEDAN en esta pagina, no todas.
//
// Every movement decision is bounded by visibleRows, so it has to be the count on
// THIS page: a page of five rows out of twenty has five visible, not twenty, and the
// difference between "scroll to the end of the page" and "scroll past the end of
// the data" is exactly that.
func TestGrid_VisibleRowsCountsThisPagePlusTheDrafts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		total, page int
		pageNumber  int
		pending     int
		want        int
	}{
		{"one page, no drafts", 8, 5, 1, 0, 5},
		{"one page with drafts", 8, 5, 1, 3, 5},
		{"the last page, full", 20, 5, 4, 0, 5},
		{"a short last page", 12, 5, 3, 0, 2},
		{"a short last page with drafts", 12, 5, 3, 4, 5},
		{"drafts only, no committed rows", 0, 5, 1, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, tc.total, 2, 40, 12, tc.page)
			g.pager.GoToPage(tc.pageNumber)
			for range tc.pending {
				g.pendingRows = append(g.pendingRows, []interface{}{"p", "q"})
			}
			if got := g.visibleRows(); got != tc.want {
				t.Errorf("visibleRows() on page %d of a %d-row page is %d, want %d", tc.pageNumber, tc.page, got, tc.want)
			}
		})
	}

	// Nothing at all is nothing, which is what lets every movement function
	// bail out on one condition.
	empty := newGrid(t, 0, 2, 40, 12, 5)
	if got := empty.visibleRows(); got != 0 {
		t.Errorf("visibleRows() with nothing loaded is %d, want 0", got)
	}
}

// Scenario: El alto de contenido baja con CADA cosa que se dibuja encima.
//
// Two of the four overhead lines have their own single-line triggers that are easy
// to leave untested, because the tests for the other two already assert "one less":
// the filter BAR, and the column FINDER on its own. Both are single lines, and both
// are places where the row budget shrinks under the user without anything else
// changing.
func TestGrid_TheFilterBarAndTheColumnFinderEachCostARow(t *testing.T) {
	base := newGrid(t, 5, 3, 80, 20, 100).contentHeight()
	if want := 20 - 6; base != want {
		t.Fatalf("a bare grid's content height is %d, want %d", base, want)
	}

	// The bar, on its own.
	bar := newGrid(t, 5, 3, 80, 20, 100)
	bar.startWhereFilter()
	if got := bar.contentHeight(); got != base-1 {
		t.Errorf("with the filter bar up the content height is %d, want %d", got, base-1)
	}

	// The finder, on its own — and NOT alongside a clause, which would take
	// the other branch and prove nothing about this one.
	find := newGrid(t, 5, 3, 80, 20, 100)
	find.startColumnFind()
	if got := find.contentHeight(); got != base-1 {
		t.Errorf("with the column finder open the content height is %d, want %d", got, base-1)
	}
	// The other two triggers, for completeness, each on their own too.
	for _, tc := range []struct {
		name  string
		setup func(*Grid)
	}{
		{"drafts", func(g *Grid) {
			g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: "a", NewValue: "b"}}
		}},
		{"a clause", func(g *Grid) { g.whereClause = "id = 1" }},
		{"an armed commit", func(g *Grid) { g.SetCommitPending(true) }},
		{"an armed discard", func(g *Grid) { g.discardPending = true }},
		{"an armed refresh", func(g *Grid) { g.refreshPending = true }},
	} {
		g := newGrid(t, 5, 3, 80, 20, 100)
		tc.setup(g)
		if got := g.contentHeight(); got != base-1 {
			t.Errorf("with %s the content height is %d, want %d", tc.name, got, base-1)
		}
	}
}

// Scenario: Una media pagina que cae JUSTO en el borde tira de la ventana.
//
// halfPageDown adds half a window to the cursor and then pulls it back to the last
// drawn line if it went past it. Landing exactly ON that line is the boundary: the
// pull has to happen, and a comparison written as a strict inequality would leave
// the cursor one line below the block, where nothing is drawn for it.
func TestGrid_AHalfPageLandingOnTheEdgeStillPullsTheWindow(t *testing.T) {
	for _, height := range []int{10, 14, 24} {
		t.Run(fmt.Sprintf("height %d", height), func(t *testing.T) {
			g := newGrid(t, 200, 2, 80, height, 100)
			ch := g.contentHeight()
			half := ch / 2

			// Start exactly half a window above the edge.
			g.scrollRow = 0
			g.cursorRow = ch - half
			if g.cursorRow+half != ch {
				t.Skipf("the window is %d lines and half is %d, so the two do not meet at an edge", ch, half)
			}
			g.halfPageDown()

			if g.cursorRow != ch-1 {
				t.Errorf("landing on the edge left the cursor on drawn line %d, want %d", g.cursorRow, ch-1)
			}
			if g.cursorRow >= ch {
				t.Errorf("the cursor is on drawn line %d, past the %d-line block", g.cursorRow, ch)
			}
		})
	}
}

// Scenario: Un cursor justo en el FINAL sigue siendo una fila real.
//
// clampCursor's repair fires at `absRow >= totalRows`, so the boundary is equality:
// a cursor pointing at the row one past the end still has to be pulled back, and a
// comparison written the other way round would leave it there — pointing at nothing
// while the highlight is drawn on the last row.
func TestGrid_ACursorExactlyAtTheEndIsPulledBack(t *testing.T) {
	for _, nRows := range []int{1, 4, 12} {
		for _, height := range []int{9, 16} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 40, height, 100)
				ch := g.contentHeight()
				total := g.visibleRows()

				g.scrollRow = 0
				g.cursorRow = total // exactly one past the last row
				g.clampCursor()

				if got := g.scrollRow + g.cursorRow; got != total-1 {
					t.Errorf("a cursor at row %d stayed at %d, want it pulled back to the last row %d", total, got, total-1)
				}
				if g.cursorRow >= ch {
					t.Errorf("the cursor is on drawn line %d of a %d-line block", g.cursorRow, ch)
				}
			})
		}
	}
}

// Scenario: El aviso del WHERE ocupa el ancho del panel, sin los bordes.
//
// The banner is styled to the pane's inner width so it reads as a rule across the
// grid rather than as a fragment of text. Being one cell short or long is visible
// as a ragged line under the header.
func TestGrid_TheWhereBannerSpansTheInnerWidth(t *testing.T) {
	for _, width := range []int{20, 40, 80, 120} {
		t.Run(fmt.Sprintf("pane %d", width), func(t *testing.T) {
			g := newGrid(t, 3, 2, width, 14, 100)
			g.whereClause = "id = 1"

			out := resultOf(g.renderRecordsView())
			banner := ansi.Strip(out[0])
			if !strings.Contains(banner, "WHERE id = 1") {
				t.Fatalf("the banner is %q, want it to name the clause", banner)
			}
			if got := ansi.StringWidth(out[0]); got != width-2 {
				t.Errorf("the banner is %d cells wide in a %d-cell pane, want %d — the pane without its borders", got, width, width-2)
			}
		})
	}
}

// Scenario: Las lineas vacias rellenan EXACTAMENTE lo que falta.
//
// The row block is a budget: rows are drawn from the top, and the rest is blank so
// the block is always the same height. A mutation that adds or drops one blank line
// is invisible until the block is off by one — which is what this asserts, by
// counting the blanks rather than the rows.
//
// And the empty-table message is not a blank: it is the one line that says there is
// nothing to show, and it must not appear when there IS something.
func TestGrid_BlanksFillTheRowBudgetExactly(t *testing.T) {
	for _, nRows := range []int{0, 1, 4} {
		for _, height := range []int{10, 14, 22} {
			t.Run(fmt.Sprintf("%d rows, height %d", nRows, height), func(t *testing.T) {
				g := newGrid(t, nRows, 2, 40, height, 100)
				ch := g.contentHeight()
				out := resultOf(g.renderRecordsView())
				block := out[g.recordsHeaderOffset()+1 : g.recordsHeaderOffset()+1+ch]

				drawn, blanks := 0, 0
				for _, l := range block {
					switch {
					case ansi.Strip(l) == "":
						blanks++
					default:
						drawn++
					}
				}
				if drawn+blanks != ch {
					t.Fatalf("the block has %d drawn and %d blank lines, want %d in total", drawn, blanks, ch)
				}
				// Every committed row is drawn, so the blanks are
				// exactly the remainder.
				wantDrawn := min(nRows, ch)
				if nRows == 0 {
					wantDrawn = 1 // the empty-table message takes a slot
				}
				if drawn != wantDrawn {
					t.Errorf("the block draws %d lines, want %d: %d rows and %d blanks", drawn, wantDrawn, wantDrawn, ch-wantDrawn)
				}
				if blanks != ch-wantDrawn {
					t.Errorf("the block has %d blank lines, want %d", blanks, ch-wantDrawn)
				}
				// The message appears only for a table with no rows.
				hasMsg := strings.Contains(strings.Join(block, "\n"), "Empty table")
				if hasMsg != (nRows == 0) {
					t.Errorf("with %d rows the empty-table message is present: %v", nRows, hasMsg)
				}
			})
		}
	}
}

// Scenario: Una insercion en curso SIN editar no se dibuja como edicion.
//
// inserting stays true after the row is committed but the cursor has moved off it,
// and editing can be false while inserting is true — that is the state between
// opening an insert and typing. Drawing the edit row then would show an editor on a
// row nobody is editing, with a cursor block in it.
func TestGrid_AnInsertNotBeingEditedDrawsNoEditor(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 16, 100)
	g.pendingRows = [][]interface{}{{"p0a", "p0b", "p0c"}}
	g.cursorRow = 2
	g.cursorCol = 0
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	if !g.IsEditing() {
		t.Fatal("the fixture is not editing")
	}
	// Leave edit mode but stay in insert mode, which is what opening a
	// different cell's editor does.
	g.editing = false
	g.editValue = "ghost"

	line := resultOf(g.renderRecordsView())[g.recordsHeaderOffset()+1+2]
	if strings.Contains(ansi.Strip(line), "ghost") {
		t.Errorf("a pending insert that is not being edited still draws the editor:\n%s", ansi.Strip(line))
	}
	if !strings.Contains(ansi.Strip(line), "p0b") {
		t.Errorf("the pending row lost its data:\n%s", ansi.Strip(line))
	}
}

// Scenario: La ventana desplazada cambia el indice de columna que se pinta.
//
// Four branches of the row renderer subtract the window's first column to get the
// index within the visible set, and each of them is wrong by the window offset if it
// forgets. The window has to be scrolled for this to be visible at all — with the
// window at column zero every formula gives the same answer, which is why the
// earlier version of these assertions proved nothing about the offsets.
func TestGrid_TheScrolledWindowChangesWhichCellIsPainted(t *testing.T) {
	// Twelve columns in a 46-cell pane, so four fit and the window scrolls.
	wide := func() *Grid {
		g := newGrid(t, 4, 12, 46, 14, 100)
		for i := range g.columns {
			g.columns[i] = fmt.Sprintf("column_%02d", i)
			g.data.Columns[i].Name = g.columns[i]
		}
		g.header.SetColumns(g.columns)
		g.calculateWidths()
		return g
	}

	t.Run("a selected row", func(t *testing.T) {
		g := wide()
		g.cursorRow = 1
		g.selectedRows[1] = true
		// Six presses from the origin, not a pre-set cursor: the grid has
		// twelve columns, so setting the cursor to six AND pressing right six
		// times walks it past the last one and wraps it back to zero, which
		// leaves the window at zero and skips the case.
		for range 6 {
			g.moveRight()
		}
		if g.scrollCol == 0 {
			t.Skip("the window did not scroll")
		}
		// The selected row is painted whole, so the assertion that
		// matters is that it is painted AT ALL — which is only true
		// because the branch was taken, and only for a window that has
		// moved.
		if got := inStyle(rowLine(t, g, 1), g.styles.Cursor); len(got) == 0 {
			t.Errorf("the selected row is not painted with the window at %d", g.scrollCol)
		}
	})

	t.Run("a deleted row", func(t *testing.T) {
		g := wide()
		g.cursorRow = 1
		for range 6 {
			g.moveRight()
		}
		if g.scrollCol == 0 {
			t.Skip("the window did not scroll")
		}
		g.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{marker(1, 0)}}}
		line := rowLine(t, g, 1)
		got := inStyle(line, g.styles.DraftDeleteSelected)
		if len(got) != 1 || got[0] != marker(1, g.cursorCol) {
			t.Errorf("with the window at %d the deleted row's selected cell is %v, want just %q", g.scrollCol, got, marker(1, g.cursorCol))
		}
	})

	t.Run("an edited row inside the window", func(t *testing.T) {
		g := wide()
		g.cursorRow = 0
		for range 6 {
			g.moveRight()
		}
		if g.scrollCol == 0 || g.scrollCol > g.cursorCol {
			t.Skipf("the window is at %d and the cursor at %d", g.scrollCol, g.cursorCol)
		}
		if _, handled := g.HandleAction("edit_cell"); !handled {
			t.Fatal("edit_cell was not handled")
		}
		typeIn(t, g, "QQ")
		// The edited column is INSIDE the window, so the in-progress
		// value is drawn — in the column the cursor is on, not at the
		// window's first column.
		out := resultOf(g.renderRecordsView())
		line := out[g.recordsHeaderOffset()+1]
		if !strings.Contains(ansi.Strip(line), "QQ") {
			t.Errorf("with the window at %d and the editor at %d the in-progress value is not drawn:\n%s",
				g.scrollCol, g.editCol, ansi.Strip(line))
		}
	})

	t.Run("a pending edit inside the window", func(t *testing.T) {
		g := wide()
		row := make([]interface{}, 12)
		for i := range row {
			row[i] = fmt.Sprintf("p%02d", i)
		}
		g.pendingRows = [][]interface{}{row}
		g.cursorRow = 4
		for range 6 {
			g.moveRight()
		}
		if _, handled := g.HandleAction("edit_cell"); !handled {
			t.Fatal("edit_cell was not handled")
		}
		// pendingCol is only set BY edit_cell — it is the pending row's
		// column, not the cursor's — so the check has to come after the
		// press, not before it.
		if g.scrollCol == 0 || g.scrollCol > g.pendingCol {
			t.Fatalf("the window is at %d and the pending editor at %d, so the case was not reached", g.scrollCol, g.pendingCol)
		}
		typeIn(t, g, "QQ")
		out := resultOf(g.renderRecordsView())
		line := out[g.recordsHeaderOffset()+1+4]
		if !strings.Contains(ansi.Strip(line), "QQ") {
			t.Errorf("with the window at %d the pending in-progress value is not drawn:\n%s", g.scrollCol, ansi.Strip(line))
		}
	})
}

// ---------------------------------------------------------------------------
// The last gaps: row zero, page two, and the exact fits
// ---------------------------------------------------------------------------

// Scenario: La fila CERO se selecciona, se exporta y se borra como las demas.
//
// Every one of those paths guards its index with `>= 0`, and index zero is the one
// value a comparison written the other way round would exclude. It is also the row a
// user reaches first, so a guard that quietly skipped it would look like the grid
// ignoring the first row of every table.
func TestGrid_RowZeroIsReachableThroughEveryPath(t *testing.T) {
	t.Run("selected rows include row zero", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.selectedRows[0] = true
		g.selectedRows[2] = true
		rows := g.SelectedRows()
		if len(rows) != 2 {
			t.Fatalf("SelectedRows() returned %d rows, want 2", len(rows))
		}
		found := map[string]bool{}
		for _, r := range rows {
			found[r[0].(string)] = true
		}
		if !found[marker(0, 0)] {
			t.Errorf("SelectedRows() returned %v, which does not include row zero", found)
		}
	})

	t.Run("export includes row zero", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.selectedRows[0] = true
		g.selectedRows[1] = true
		g.startExport()
		if len(g.exportPicker.rows) != 2 {
			t.Fatalf("the picker got %d rows, want both selected ones", len(g.exportPicker.rows))
		}
		found := false
		for _, r := range g.exportPicker.rows {
			if r[0] == marker(0, 0) {
				found = true
			}
		}
		if !found {
			t.Errorf("the picker got %v, which does not include row zero", g.exportPicker.rows)
		}
	})

	t.Run("a single selected row zero is the export subject", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.selectedRows[0] = true
		g.startExport()
		if len(g.exportPicker.row) == 0 {
			t.Fatal("the picker got no single row")
		}
		if got := g.exportPicker.row[0]; got != marker(0, 0) {
			t.Errorf("the picker got %v, want row zero", got)
		}
		if g.exportPicker.fileMode {
			t.Error("a single selected row went straight to a file, so the clipboard was never offered")
		}
	})

	t.Run("yank takes row zero", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.selectedRows[0] = true
		g.startYank()
		if len(g.exportPicker.row) == 0 {
			t.Fatal("the yank got no single row")
		}
		if got := g.exportPicker.row[0]; got != marker(0, 0) {
			t.Errorf("the yank got %v, want row zero", got)
		}
	})

	t.Run("deleting a selection including row zero stages it", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.selectedRows[0] = true
		g.selectedRows[2] = true
		g.startDelete()
		if len(g.pendingDeletes) != 2 {
			t.Fatalf("staging a selection of rows 0 and 2 produced %d deletes", len(g.pendingDeletes))
		}
		idx := map[int]bool{}
		for _, d := range g.pendingDeletes {
			idx[d.RowIdx] = true
		}
		if !idx[0] {
			t.Errorf("the staged deletes are for rows %v, which does not include row zero", idx)
		}
	})

	t.Run("the cursor row zero is the row a plain delete stages", func(t *testing.T) {
		g := newGrid(t, 3, 2, 40, 12, 100)
		g.cursorRow = 0
		g.startDelete()
		if len(g.pendingDeletes) != 1 || g.pendingDeletes[0].RowIdx != 0 {
			t.Errorf("deleting with the cursor on row zero staged %v", g.pendingDeletes)
		}
	})
}

// Scenario: En la pagina DOS, el indice del cursor incluye el desplazamiento.
//
// Three things are computed from scroll plus cursor plus the page offset, and with
// everything on page one the offset is zero, which is why a fixture that never turns
// a page cannot tell a missing term from a present one. This one turns to page two and
// checks each of them against the row it actually names.
func TestGrid_ThePageOffsetCountsInEveryIndex(t *testing.T) {
	// 12 rows, five to a page, so page two starts at row 5.
	page := func() *Grid {
		g := newGrid(t, 12, 3, 40, 14, 5)
		g.pager.GoToPage(2)
		return g
	}

	t.Run("the visible rows start after the page", func(t *testing.T) {
		g := page()
		if got := g.visibleRows(); got != 5 {
			t.Fatalf("the second page of five has %d visible rows, want 5", got)
		}
		out := resultOf(g.renderRecordsView())
		block := out[g.recordsHeaderOffset()+1:]
		if !strings.Contains(ansi.Strip(block[0]), marker(5, 0)) {
			t.Errorf("the first drawn row is %q, want row five", ansi.Strip(block[0]))
		}
	})

	t.Run("the selected row is the page offset plus the cursor", func(t *testing.T) {
		g := page()
		g.scrollRow = 1
		g.cursorRow = 2
		row := g.SelectedRow()
		if row == nil || row[0] != marker(8, 0) {
			t.Errorf("SelectedRow() is %v, want row eight (offset five, scroll one, cursor two)", row)
		}
	})

	t.Run("an edit lands on the right row", func(t *testing.T) {
		g := page()
		g.scrollRow = 1
		g.cursorRow = 2
		g.cursorCol = 1
		if _, handled := g.HandleAction("edit_cell"); !handled {
			t.Fatal("edit_cell was not handled")
		}
		if g.editRow != 8 {
			t.Errorf("the editor opened row %d, want row eight", g.editRow)
		}
		typeIn(t, g, "ZZ")
		tap(t, g, tea.KeyEscape)
		if len(g.pendingUpdates) != 1 || g.pendingUpdates[0].RowIdx != 8 {
			t.Errorf("the pending update is %v, want one for row eight", g.pendingUpdates)
		}
	})

	t.Run("a staged delete names the right row", func(t *testing.T) {
		g := page()
		g.scrollRow = 1
		g.cursorRow = 2
		if _, ok := g.startDelete(); !ok {
			t.Fatal("startDelete refused")
		}
		if got := g.pendingDeletes[0].RowIdx; got != 8 {
			t.Errorf("the staged delete names row %d, want row eight", got)
		}
	})

	t.Run("an undo reverts the right row", func(t *testing.T) {
		g := page()
		g.scrollRow = 1
		g.cursorRow = 2
		g.data.Rows[8][1] = "changed"
		g.pendingUpdates = []PendingUpdate{{RowIdx: 8, ColIdx: 1, OldValue: marker(8, 1), NewValue: "changed"}}
		g.pendingUpdates[0].OldRow = append([]interface{}(nil), g.data.Rows[8]...)
		g.pendingUpdates[0].OldRow[1] = marker(8, 1)

		if got := g.UndoRowDrafts(); got != 1 {
			t.Fatalf("undoing reported %d changes, want 1", got)
		}
		if got := g.data.Rows[8][1]; got != marker(8, 1) {
			t.Errorf("row eight's edited cell is %#v after undo, want the original", got)
		}
		if len(g.pendingUpdates) != 0 {
			t.Errorf("the update for row eight survived the undo: %v", g.pendingUpdates)
		}
	})

	t.Run("a foreign key value comes from the right row", func(t *testing.T) {
		g := page()
		g.SetMetadata(nil, []postgres.ForeignKeyInfo{{Column: "c2", RefTable: "parents", RefColumn: "id"}}, nil)
		g.scrollRow = 1
		g.cursorRow = 2
		g.cursorCol = 2
		cmd, ok := g.startFK()
		if !ok {
			t.Fatal("could not follow the foreign key")
		}
		if got := cmd().(GridNavigateFKMsg).FKValue; got != marker(8, 2) {
			t.Errorf("the message carries %#v, want row eight's value %q", got, marker(8, 2))
		}
	})

	t.Run("a pending insert is past the page's committed rows", func(t *testing.T) {
		// Page THREE, which holds only two of the twelve rows: the
		// offset is ten and the page has two rows left, so absolute row
		// two is already a draft. Page two would not do — it has five
		// rows left and its offset is a different number.
		g := newGrid(t, 12, 3, 40, 14, 5)
		g.pager.GoToPage(3)
		g.pendingRows = [][]interface{}{{"p0", "p1", "p2"}}
		g.cursorRow = 2
		if got := g.cursorRowType(); got != "insert" {
			t.Fatalf("absolute row two of a two-row page is a %q row, want insert", got)
		}
		if got := g.pendingInsertIndex(); got != 0 {
			t.Errorf("the pending insert index is %d, want 0", got)
		}
		// And the page's two committed rows are still drawn first.
		out := resultOf(g.renderRecordsView())
		block := out[g.recordsHeaderOffset()+1:]
		if !strings.Contains(ansi.Strip(block[0]), marker(10, 0)) {
			t.Errorf("the first drawn row is %q, want row ten", ansi.Strip(block[0]))
		}
		if !strings.Contains(ansi.Strip(block[1]), marker(11, 0)) {
			t.Errorf("the second drawn row is %q, want row eleven", ansi.Strip(block[1]))
		}
	})
}

// Scenario: El indice de insercion JUSTO fuera de la lista no es una insercion.
//
// pendingInsertIndex names the insert the cursor is on, and the cursor can be parked
// one past the last one. The index that distinguishes the guard from an off-by-one is
// the one EQUAL to the list length — one more insert than exists — which is not the
// same as being further out.
func TestGrid_APendingIndexOneMoreThanTheListIsNotAnInsert(t *testing.T) {
	for _, nPending := range []int{1, 2, 5} {
		t.Run(fmt.Sprintf("%d pending rows", nPending), func(t *testing.T) {
			g := newGrid(t, 3, 2, 40, 14, 100)
			g.pendingRows = make([][]interface{}, nPending)
			for i := range nPending {
				g.pendingRows[i] = []interface{}{fmt.Sprintf("p%d", i), "x"}
			}
			// Exactly one row past the last insert: the index would be
			// len(pendingRows), which is not a valid position.
			g.scrollRow = 0
			g.cursorRow = 3 + nPending
			if got := g.pendingInsertIndex(); got != -1 {
				t.Errorf("a cursor one insert past the end gives index %d, want -1", got)
			}
			// The row type still says "insert" — the cursor IS past the
			// committed rows — which is why the two are separate
			// questions and why editing has to check both.
			if got := g.cursorRowType(); got != "insert" {
				t.Errorf("the row type is %q, want insert", got)
			}
			// And editing there starts no editor rather than indexing
			// one past the list.
			if _, handled := g.HandleAction("edit_cell"); !handled {
				t.Error("edit_cell was not handled")
			}
			if g.IsEditing() {
				t.Error("editing a cursor past the last pending insert started an editor")
			}
			if g.PendingCount() != nPending {
				t.Errorf("the pending list changed to %d rows", g.PendingCount())
			}
		})
	}
}

// Scenario: Una columna que el NOMBRE ensancha hasta su ancho.
//
// expandEditCol widens a column to fit the value OR the column's own name, and the
// second half of that rule is invisible to a fixture whose names are two characters.
// A name long enough to dominate is what pins it — and the constant is four cells for
// a name against four for a value, so the two rules are indistinguishable unless the
// name is the longer of the pair.
func TestGrid_ALongColumnNameWidensTheEditedColumn(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetWidth(400)
	g.SetHeight(16)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "created_at_millis"}},
		Rows:    [][]interface{}{{"v"}},
		Count:   1,
	}, "public", "t")
	g.cursorRow = 0
	g.cursorCol = 0
	g.Focus()

	base := g.widths[0]
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	want := len("created_at_millis") + 4
	if g.widths[0] != want {
		t.Errorf("the edited column is %d cells, want %d — the NAME plus four, with a one-character value", g.widths[0], want)
	}
	if g.widths[0] <= base {
		t.Errorf("editing did not widen the column at all (it was %d)", base)
	}
}

// Scenario: Teclear hasta LLENAR la columna no la ensancha todavia.
//
// The expansion triggers on the value being wider than the column by more than its
// padding, and the boundary is equality: a value that exactly fills the column is not
// yet overflowing, so the column does not grow. A comparison written the other way
// round would widen the column the moment it became exactly full, and every edit
// would nudge the grid's layout.
func TestGrid_AValueThatExactlyFillsTheColumnDoesNotWidenIt(t *testing.T) {
	for _, colWidth := range []int{6, 10, 14} {
		t.Run(fmt.Sprintf("a %d-cell column", colWidth), func(t *testing.T) {
			styles := theme.Resolve("dark").Styles()
			g := New(styles, 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
			g.SetWidth(400)
			g.SetHeight(16)
			g.SetData(&postgres.QueryResult{
				Columns: []postgres.ColumnInfo{{Name: "c0"}},
				Rows:    [][]interface{}{{""}},
				Count:   1,
			}, "public", "t")
			g.widths = []int{colWidth}
			g.Focus()
			g.cursorRow = 0
			g.cursorCol = 0

			if _, handled := g.HandleAction("edit_cell"); !handled {
				t.Fatal("edit_cell was not handled")
			}
			// Exactly fills: the value plus its padding equals the
			// column.
			exact := colWidth - 4
			if exact < 0 {
				t.Skip("the fixture's column is too narrow for the rule")
			}
			// Checked ONE CHARACTER AT A TIME, not just at the end. The
			// final width is the same whether the trigger fires at the
			// boundary or one keystroke later — the column grows to the
			// value's width either way — so an assertion on the end state
			// passes against a comparison written the other way round, and
			// what the user sees is the column nudging sideways as they
			// type.
			for i := 1; i <= exact; i++ {
				typeIn(t, g, "q")
				if g.widths[0] != colWidth {
					t.Fatalf("after %d of %d characters the column is %d cells, want it unchanged at %d: the value does not overflow until it is one character longer",
						i, exact, g.widths[0], colWidth)
				}
			}

			// One more and it does widen.
			typeIn(t, g, "q")
			if g.widths[0] != exact+1+4 {
				t.Errorf("one character over left the column at %d cells, want %d", g.widths[0], exact+1+4)
			}
		})
	}
}

// Scenario: Una columna mas ancha que el panel no empuja la ventana de sitio.
//
// The shrink loop stops when it runs out of columns to drop — at scrollCol == cursorCol
// — and there is one more column to drop in a comparison written the other way round.
// / It would move the window PAST the cursor, which no other rule brings back.
func TestGrid_AColumnWiderThanThePaneKeepsTheWindowOnIt(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	// One column, far wider than the 38-cell pane.
	g.widths = []int{40}
	g.cursorCol = 0
	g.scrollCol = 0
	g.syncScroll()

	if g.scrollCol != 0 {
		t.Errorf("the window moved to column %d with a single column on the cursor, want it left at 0", g.scrollCol)
	}
	if g.cursorCol != 0 {
		t.Errorf("the cursor moved to column %d", g.cursorCol)
	}
}

// Scenario: Las columnas que llenan el panel EXACTAMENTE se recuperan a la izquierda.
//
// The expand-left loop stops when one more column would not fit. At exactly filling
// the pane, that last column DOES fit — so the window reaches the leftmost column it
// can. A strict comparison would stop one short, and the pane would show one blank
// strip for the rest of the session.
func TestGrid_AFillWidthPaneRefillsToTheLeftEdge(t *testing.T) {
	// Three columns of eight in a 26-cell pane: available is 24, and all
	// three fill it exactly.
	g := newGrid(t, 2, 3, 26, 12, 100)
	g.widths = []int{8, 8, 8}
	g.cursorCol = 2
	g.scrollCol = 2
	g.syncScroll()

	if g.scrollCol != 0 {
		t.Errorf("three eight-cell columns filling a 24-cell pane left the window at column %d, want 0", g.scrollCol)
	}
}

// Scenario: Un panel de CERO celdas deja los anchos como estaban.
//
// calculateWidths has no width to divide when the pane is zero, so it returns without
// touching them — which means the grid keeps the layout it had rather than
// recomputing every column against a pane of nothing. Setting the width back to zero
// is a resize the app does, so this is a state the code has to survive.
func TestGrid_AZeroWidthPaneKeepsTheColumnWidths(t *testing.T) {
	g := newGrid(t, 2, 3, 40, 12, 100)
	before := append([]int(nil), g.widths...)
	if len(before) == 0 {
		t.Fatal("the fixture has no column widths to preserve")
	}

	g.SetWidth(0)
	if got := g.widths; len(got) != len(before) {
		t.Errorf("a zero-width pane replaced %d widths with %d", len(before), len(got))
	}
	for i := range before {
		if got := g.widths[i]; got != before[i] {
			t.Errorf("a zero-width pane changed column %d from %d to %d cells", i, before[i], got)
		}
	}
	// And the header is told to forget its widths, because the header
	// renders from them and would otherwise draw a header for a pane that
	// is not there.
	if g.header.widths != nil {
		t.Errorf("the header kept %v through a zero-width pane", g.header.widths)
	}
}

// Scenario: Sin celdas libres, el ancho por columna NO se acota a diez.
//
// The per-column cap exists so several columns fit in the pane. With no room at all
// there is nothing to divide, and applying the cap anyway would stretch every column
// to ten cells — a pane of zero showing 24 cells of columns. The fixture's columns
// have to be WIDER than the cap for this to be observable; a column already narrower
// than ten is unaffected either way.
func TestGrid_ColumnsWiderThanTheCapSurviveAPaneWithNoRoom(t *testing.T) {
	g := New(theme.Resolve("dark").Styles(), 100, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	g.SetHeight(12)
	// The pane is wide first, so the values get to set the widths. Loading
	// with no pane at all leaves calculateWidths with nothing to do and
	// there is nothing here to preserve.
	g.SetWidth(400)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "c0"}, {Name: "c1"}},
		Rows:    [][]interface{}{{"a value far wider than any cap", "another long value here"}},
		Count:   1,
	}, "public", "t")

	// The widths before the pane is set, so the values have set them.
	wide := append([]int(nil), g.widths...)
	for i, w := range wide {
		if w <= 10 {
			t.Fatalf("fixture column %d is %d cells, which is inside the cap, so the case cannot be reached", i, w)
		}
	}

	g.SetWidth(2) // available zero
	for i, w := range g.widths {
		if w == 10 {
			t.Errorf("column %d was capped to ten in a pane with no room at all", i)
		}
		if w != wide[i] {
			t.Errorf("column %d went from %d to %d cells in a pane with no room", i, wide[i], w)
		}
	}
}

// Scenario: Una fila marcada para borrar, sin cursor, no marca ninguna celda.
//
// The renderer paints the cell under the cursor in its "selected while deleted"
// colour and leaves the rest alone. The initial index is the sentinel for "no cell",
// so a cursor elsewhere on the row has to leave the whole row in the plain delete
// style — and a sentinel that was not -1 would paint whichever cell it happened to
// name.
func TestGrid_ADeletedRowAwayFromTheCursorMarksNoCell(t *testing.T) {
	g := newGrid(t, 3, 3, 40, 14, 100)
	g.cursorRow = 0
	g.pendingDeletes = []PendingDelete{{RowIdx: 1, Row: []interface{}{marker(1, 0), marker(1, 1), marker(1, 2)}}}

	if got := inStyle(rowLine(t, g, 1), g.styles.DraftDeleteSelected); len(got) != 0 {
		t.Errorf("a deleted row away from the cursor marks %v as selected-while-deleted, want nothing", got)
	}
	if got := inStyle(rowLine(t, g, 1), g.styles.DraftDelete); len(got) != 3 {
		t.Errorf("the deleted row paints %v in the plain delete colour, want all three cells", got)
	}
}

// Scenario: La ventana desplazada y una fila multi-seleccionada: UNA celda.
//
// With the window scrolled, the row renderer has to subtract the window's first column
// to get the index within the visible set. On a multi-selected row that index names
// the one cell painted as selected — so a renderer that forgot to subtract it either
// paints nothing or paints the wrong column, and the count is what catches it.
func TestGrid_AScrolledMultiSelectedRowStillMarksOneCell(t *testing.T) {
	g := newGrid(t, 4, 12, 46, 14, 100)
	for i := range g.columns {
		g.columns[i] = fmt.Sprintf("column_%02d", i)
		g.data.Columns[i].Name = g.columns[i]
	}
	g.header.SetColumns(g.columns)
	g.calculateWidths()

	g.cursorRow = 1
	g.selectedRows[1] = true
	for range 6 {
		g.moveRight()
	}
	if g.scrollCol == 0 {
		t.Fatal("the window did not scroll, so the case was not reached")
	}
	if g.cursorCol != 6 {
		t.Fatalf("the cursor is at column %d, want 6: six presses from the origin", g.cursorCol)
	}
	line := rowLine(t, g, 1)

	got := inStyle(line, g.styles.Selected)
	if len(got) != 1 {
		t.Fatalf("a cursor on a multi-selected row paints %v as selected, want exactly one cell", got)
	}
	if got[0] != marker(1, g.cursorCol) {
		t.Errorf("the selected cell holds %q, want the cursor column's value %q", got[0], marker(1, g.cursorCol))
	}
}

// Scenario: Insertar varias filas seguidas abre la editor en la ULTIMA.
//
// The insert's edit row is the position of the NEW row among the whole visible
// sequence — the committed rows, then the inserts. So it is the number of committed
// rows plus the insert's own index, and the index is one less than the number of
// inserts so far.
//
// A single insert cannot tell that apart from any other arrangement, because one
// minus zero is one. Two can: pressing the key twice has to leave the editor on the
// second row, which is what a subtraction instead of an addition would break.
func TestGrid_RepeatedInsertsOpenTheEditorOnTheNewestRow(t *testing.T) {
	for _, nInserts := range []int{1, 2, 3, 5} {
		t.Run(fmt.Sprintf("%d inserts", nInserts), func(t *testing.T) {
			g := newGrid(t, 4, 3, 40, 18, 100)
			committed := len(g.data.Rows)

			for i := range nInserts {
				if _, ok := g.startInsertRow(); !ok {
					t.Fatalf("insert %d was refused", i+1)
				}
				// After the Nth press the new row is the Nth insert.
				want := committed + i
				if g.editRow != want {
					t.Fatalf("after %d inserts the editor is on row %d, want row %d", i+1, g.editRow, want)
				}
				if g.pendingRow != i {
					t.Errorf("after %d inserts the pending index is %d, want %d", i+1, g.pendingRow, i)
				}
				if g.PendingCount() != i+1 {
					t.Errorf("after %d inserts there are %d pending rows", i+1, g.PendingCount())
				}
			}

			// And the in-progress value is drawn on the newest row, not
			// on any of the ones before it.
			typeIn(t, g, "NEW")
			out := resultOf(g.renderRecordsView())
			block := out[g.recordsHeaderOffset()+1:]
			line := block[committed+nInserts-1]
			if !strings.Contains(ansi.Strip(line), "NEW") {
				t.Errorf("the newest pending row's line is %q, want it to carry the in-progress value:\n%s",
					ansi.Strip(line), strings.Join(out, "\n"))
			}
		})
	}
}

// Scenario: Una pagina DIBUJA su pagina, no la siguiente.
//
// endRow is the page offset plus the page size, clamped to the data. The loop has to
// stop there — a row from the NEXT page drawn above the fold would put a row under the
// cursor that the grid's own index arithmetic never names, so pressing a key would act
// on a different row from the one highlighted.
//
// The fixture needs a page size SMALLER than the data on purpose: when the page reaches
// the end of the rows the extra iteration would read one past the end and CRASH, which
// the tool scores as a survivor — so a fixture that only ever fills its last page
// cannot tell this from a harmless one.
func TestGrid_APageDrawsItsOwnRowsAndNoMore(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows, page int
		pageNumber int
		wantDrawn  int
	}{
		{"the first of four pages", 20, 5, 1, 5},
		{"the second of four pages", 20, 5, 2, 5},
		{"a page of three", 20, 3, 3, 3},
		{"the last page, short", 20, 6, 4, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, tc.rows, 2, 40, 20, tc.page)
			g.pager.GoToPage(tc.pageNumber)

			out := resultOf(g.renderRecordsView())
			block := out[g.recordsHeaderOffset()+1:]
			first := tc.pageNumber - 1

			for i := 0; i < tc.wantDrawn; i++ {
				want := marker(first*tc.page+i, 0)
				if !strings.Contains(ansi.Strip(block[i]), want) {
					t.Errorf("row line %d is %q, want %q", i, ansi.Strip(block[i]), want)
				}
			}
			// And the line after the page is BLANK, not the next page's
			// first row. That is the assertion a fixture whose page ends
			// at the data cannot make, because there the extra iteration
			// crashes instead of drawing.
			if got := ansi.Strip(block[tc.wantDrawn]); got != "" {
				t.Errorf("the line after the page is %q, want it blank: a row from the next page was drawn", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Every offset at once
// ---------------------------------------------------------------------------

// Scenario: Con pagina, scroll Y columnas, cada indice cuenta las tres.
//
// Three offsets are added to name the row the user is looking at: the page's, the
// window's within the page, and the cursor's within the drawn block. And a fourth —
// the window's first COLUMN — is subtracted from the column index.
//
// A fixture that varies only one of them proves only one term is present. With the
// page offset at zero a missing `+ offset()` looks like a present one; with the scroll
// at zero `offset + scrollRow` and `offset - scrollRow` agree; and with the column
// window at zero `cursorCol - scrollCol` and `cursorCol + scrollCol` agree. So this
// fixture sets all of them to different non-zero numbers at once, and checks each
// thing they feed: the first drawn row, the yank's subject, the edited row's cell and
// the selected cell's column.
func TestGrid_WithEveryOffsetSetTheIndicesStillNameTheRightRow(t *testing.T) {
	// Thirty rows, five to a page, on page three: the offset is ten.
	// Twelve long-named columns in a 46-cell pane: four fit, so the column
	// window has to scroll. A ten-line pane leaves a four-line window, so a
	// four-line walk pushes the ROW window down as well.
	g := newGrid(t, 30, 12, 46, 10, 5)
	for i := range g.columns {
		g.columns[i] = fmt.Sprintf("column_%02d", i)
		g.data.Columns[i].Name = g.columns[i]
	}
	g.header.SetColumns(g.columns)
	g.calculateWidths()
	g.pager.GoToPage(3)

	// Four presses: the fourth lands on the last drawn line and scrolls the
	// window down by one.
	for range 4 {
		g.moveDown()
	}
	// Six to the right, so the column window has moved too.
	for range 6 {
		g.moveRight()
	}

	offset := g.pager.Offset()
	if offset == 0 || g.scrollRow == 0 || g.scrollCol == 0 {
		t.Skipf("the fixture did not produce three non-zero offsets (page %d, scroll %d, column %d)", offset, g.scrollRow, g.scrollCol)
	}
	absRow := offset + g.scrollRow + g.cursorRow

	// 1. The first row drawn is the offset plus the scroll. The column
	// checked is the WINDOW's first, because with the column window
	// scrolled column zero is not on screen — asserting on it would be
	// asserting on a column the render never draws.
	out := resultOf(g.renderRecordsView())
	block := out[g.recordsHeaderOffset()+1:]
	if !strings.Contains(ansi.Strip(block[0]), marker(offset+g.scrollRow, g.scrollCol)) {
		t.Errorf("the first drawn row is %q, want row %d (page offset %d plus scroll %d)",
			ansi.Strip(block[0]), offset+g.scrollRow, offset, g.scrollRow)
	}

	// 2. A yank takes the cursor's row, all three offsets included.
	g.exportPicker.Hide()
	if _, ok := g.startYank(); !ok {
		t.Fatal("startYank refused")
	}
	if len(g.exportPicker.row) == 0 {
		t.Fatal("the yank got no row")
	}
	// The picker's row is the WHOLE row, not the visible window of it, so
	// the check is on the row's own first column.
	if got := g.exportPicker.row[0]; got != marker(absRow, 0) {
		t.Errorf("the yank took %v, want the cursor's row %q", got, marker(absRow, 0))
	}
	g.exportPicker.Hide()

	// 3. An edit lands on the cursor's row and column.
	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("edit_cell was not handled")
	}
	if g.editRow != absRow {
		t.Errorf("the editor opened row %d, want the cursor's row %d", g.editRow, absRow)
	}
	typeIn(t, g, "ZZ")
	block = resultOf(g.renderRecordsView())[g.recordsHeaderOffset()+1:]
	edited := block[g.cursorRow]
	if !strings.Contains(ansi.Strip(edited), "ZZ") {
		t.Errorf("the in-progress value is not on the cursor's line:\n%s", ansi.Strip(edited))
	}
	// The value is drawn over the committed one rather than beside it: the
	// cell's whole content is the edit plus the cursor block. Asserting
	// that the committed text is GONE cannot work here — the cell is wide
	// enough that the old value is a prefix of the new one — so what is
	// checked is that the edit is at the END, which is where an edit
	// entered at the end of a value belongs.
	if !strings.HasSuffix(strings.TrimRight(ansi.Strip(edited), "\u2588 "), "ZZ") {
		t.Errorf("the edited cell does not end with the in-progress value:\n%s", ansi.Strip(edited))
	}

	// 4. A multi-selected row paints the cursor's column, with the column
	// window scrolled.
	sel := newGrid(t, 30, 12, 46, 10, 5)
	for i := range sel.columns {
		sel.columns[i] = fmt.Sprintf("column_%02d", i)
		sel.data.Columns[i].Name = sel.columns[i]
	}
	sel.header.SetColumns(sel.columns)
	sel.calculateWidths()
	sel.pager.GoToPage(3)
	sel.selectedRows[absRow] = true
	sel.scrollRow = g.scrollRow
	sel.cursorRow = g.cursorRow
	for range 6 {
		sel.moveRight()
	}
	if sel.scrollCol == 0 {
		t.Skipf("the column window did not scroll (at %d)", sel.scrollCol)
	}
	painted := inStyle(rowLine(t, sel, sel.cursorRow), sel.styles.Selected)
	if len(painted) != 1 {
		t.Fatalf("with the column window at %d and the cursor at %d the row paints %v as selected, want exactly one cell",
			sel.scrollCol, sel.cursorCol, painted)
	}
	if painted[0] != marker(absRow, sel.cursorCol) {
		t.Errorf("the selected cell holds %q, want the cursor column's value %q", painted[0], marker(absRow, sel.cursorCol))
	}
}

// ---------------------------------------------------------------------------
// The bounds guards: a test that reaches the boundary, not just the safe side
// ---------------------------------------------------------------------------

// mustNotPanic turns "the guard declined to index past the end" into a test
// failure instead of a crashed binary.
//
// It exists because of how this project measures itself. A CONDITIONALS_BOUNDARY
// mutant turns `i >= len(s)` into `i > len(s)`, and the ONLY state that tells the
// two apart is `i == len(s)` — where the original declines the index and the mutant
// performs it, reading one element past the end of a slice and panicking.
//
// A panic in a test aborts the whole binary, so the tool has no per-test result to
// read and scores the mutant as a SURVIVOR. Recovering here and failing through the
// testing package instead turns the crash into an ordinary red test, which is what
// makes these mutants killable at all.
func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v — a guard that declines to index past the end must not reach the index", what, r)
		}
	}()
	f()
}

// Scenario: Una fila con MAS celdas que columnas no se sale de la lista de anchos.
//
// calculateWidths sizes the width list from the COLUMN list and then walks each row,
// guarding against a row that is longer than the columns describe. That mismatch is
// exactly what the guard exists for, and it is reachable: a projection can return more
// values than the header lists.
//
// The fixture is the mismatch itself, and the assertion is on the widths rather than
// on "it did not crash", because a guard that clamped to the wrong length would also
// not crash.
func TestCalculateWidths_ARowLongerThanTheColumnListStaysInsideTheWidths(t *testing.T) {
	g := newGrid(t, 0, 2, 80, 20, 10) // two columns c0, c1
	g.width = 200

	// One row with THREE cells against two columns.
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "c0"}, {Name: "c1"}},
		Rows:    [][]interface{}{{"aaa", "bbb", "cccccccccc"}},
	}

	mustNotPanic(t, "calculateWidths over a 3-cell row and 2 columns", func() { g.calculateWidths() })

	if len(g.widths) != 2 {
		t.Fatalf("the width list has %d entries, want one per column (2): a row longer than the columns must not grow it", len(g.widths))
	}
	// The third cell's width must NOT have been folded into a column.
	for i, w := range g.widths {
		if w >= len("cccccccccc")+2 {
			t.Errorf("column %d was widened to %d from the cell that has no column: the guard did not stop the walk", i, w)
		}
	}
}
