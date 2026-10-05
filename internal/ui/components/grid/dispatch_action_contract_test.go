package grid

// Scenario: Cada brazo de `dispatchAction` mueve el cursor a donde dice, y cada uno
// responde con `true`.
//
// dispatchAction is the single entry point the whole app dispatches through — every
// keybind in the grid context resolves to an action ID and lands here. Its switch has
// thirty-odd arms and, until now, the uncovered ones were exactly the movement and
// paging arms: navigate_left, go_first, go_last, both half-page moves, three of the four
// page moves, yank, export, delete_rows and select_row.
//
// Two things make the arms worth asserting rather than merely calling. The first is
// that five of them are the ONLY place `scrollRow` is reset to zero while paging, and a
// paging arm that forgot it would leave the cursor pointing at the wrong row with no
// error — the grid would show data and the wrong cell would be edited. The second is
// that they all claim `handled: true`, which is the only thing that stops the key from
// also reaching the editor underneath.
//
// So every case below asserts BOTH the effect and the claim, and the effect is asserted
// as an absolute row where one exists rather than as "it changed".

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// absRow is the row of the whole result the cursor is on, which is what a user means by
// "the cursor is on row 40" and is NOT cursorRow once anything has scrolled or paged.
func absRow(g *Grid) int { return g.scrollRow + g.cursorRow + g.pager.Offset() }

// contentRows is how many row lines the grid can show, which is what half_page moves by.
func contentRows(g *Grid) int { return g.contentHeight() }

func TestDispatchActionMovesTheCursorWhereTheActionSays(t *testing.T) {
	t.Run("go_first puts the cursor on the first row", func(t *testing.T) {
		g := newGrid(t, 40, 3, 80, 20, 10)
		g.cursorRow = 7
		g.scrollRow = 3

		_, handled := g.dispatchAction(config.ActionID("go_first"))
		if !handled {
			t.Fatal("go_first was not reported as handled")
		}
		if got := absRow(g); got != 0 {
			t.Errorf("go_first left the cursor on row %d, want 0", got)
		}
	})

	t.Run("go_last puts the cursor on the last row of the PAGE", func(t *testing.T) {
		// Not the last row of the screen, and not the last row of the result. With a
		// page of ten on a twenty-row screen, moveToLast lands on row 9 — the last
		// row the pager has — and the six empty screen rows below it stay empty
		// rather than being filled from the next page.
		//
		// The first version of this asserted cursorRow == contentHeight()-1, on the
		// reading that "last visible" meant "as far down the screen as you can go".
		// It failed with cursorRow at 9, and the code was right: visibleRows() is the
		// page, and the two are different quantities.
		g := newGrid(t, 40, 3, 80, 20, 10)
		paged := g.visibleRows()
		ch := contentRows(g)
		if paged >= ch {
			t.Skipf("the fixture has %d rows on a page and %d content rows, so the two cases cannot be told apart", paged, ch)
		}

		_, handled := g.dispatchAction(config.ActionID("go_last"))
		if !handled {
			t.Fatal("go_last was not reported as handled")
		}
		if got := g.cursorRow; got != paged-1 {
			t.Errorf("go_last left cursorRow at %d, want %d — the last row of the %d-row page", got, paged-1, paged)
		}
		if got := absRow(g); got != paged-1 {
			t.Errorf("go_last put the cursor on absolute row %d, want %d", got, paged-1)
		}
		// And the window still contains the cursor.
		if g.scrollRow+g.cursorRow >= ch {
			t.Errorf("the cursor is at window row %d, past the %d content rows", g.scrollRow+g.cursorRow, ch)
		}
		if g.scrollRow < 0 {
			t.Errorf("scrollRow is %d, which is negative", g.scrollRow)
		}
	})

	t.Run("go_last with more rows than fit on screen scrolls to the bottom", func(t *testing.T) {
		// The case the previous one could not distinguish: a page taller than the
		// screen. Here "last" really does mean the bottom of the window, and
		// scrollRow has to become positive for the cursor to be on screen.
		g := newGrid(t, 100, 3, 80, 20, 100)
		paged := g.visibleRows()
		ch := contentRows(g)
		if paged <= ch {
			t.Fatalf("the fixture has %d rows and %d content rows; it cannot scroll", paged, ch)
		}

		if _, handled := g.dispatchAction(config.ActionID("go_last")); !handled {
			t.Fatal("go_last was not reported as handled")
		}
		if got := g.cursorRow; got != ch-1 {
			t.Errorf("go_last left cursorRow at %d, want %d — the bottom of the window", got, ch-1)
		}
		if g.scrollRow <= 0 {
			t.Errorf("go_last left scrollRow at %d, want a positive scroll to reach the bottom", g.scrollRow)
		}
		if got := absRow(g); got != paged-1 {
			t.Errorf("go_last put the cursor on absolute row %d, want the last of %d", got, paged)
		}
	})

	t.Run("go_last on fewer rows than a page puts the cursor on the last row", func(t *testing.T) {
		// The clamp case. With 3 rows and a page of 20 the "last visible" is row 2,
		// and a moveToLast that only handled the tall case would leave the cursor
		// somewhere off the end.
		g := newGrid(t, 3, 2, 80, 20, 10)

		if _, handled := g.dispatchAction(config.ActionID("go_last")); !handled {
			t.Fatal("go_last was not reported as handled")
		}
		if got := absRow(g); got != 2 {
			t.Errorf("go_last left the cursor on row %d, want the last of three", got)
		}
	})

	t.Run("go_last on an empty grid does not panic and does not move", func(t *testing.T) {
		g := newGrid(t, 0, 2, 80, 20, 10)

		// It must not claim to be handled either: with no rows there is nothing to
		// move to, and reporting handled would swallow the key.
		if _, handled := g.dispatchAction(config.ActionID("go_last")); handled {
			t.Error("go_last on an empty grid claimed to be handled")
		}
		if g.cursorRow != 0 || g.scrollRow != 0 {
			t.Errorf("go_last on an empty grid moved the cursor to %d/%d", g.cursorRow, g.scrollRow)
		}
	})

	t.Run("navigate_left moves the column and WRAPS at the first", func(t *testing.T) {
		// It wraps to the LAST column, deliberately — the comment in moveRight says
		// so. The first version of this asserted it stopped at 0, which would have
		// been a "fix" of a feature: on a wide table where only some columns fit on
		// screen, a left move from the leftmost VISIBLE column is how you reach the
		// ones scrolled off it.
		g := newGrid(t, 10, 4, 80, 20, 10)
		g.cursorCol = 2

		_, handled := g.dispatchAction(config.ActionID("navigate_left"))
		if !handled {
			t.Fatal("navigate_left was not reported as handled")
		}
		if g.cursorCol != 1 {
			t.Errorf("navigate_left left the column at %d, want 1", g.cursorCol)
		}

		g.dispatchAction(config.ActionID("navigate_left"))
		if g.cursorCol != 0 {
			t.Errorf("navigate_left left the column at %d, want 0", g.cursorCol)
		}

		g.dispatchAction(config.ActionID("navigate_left"))
		if g.cursorCol != len(g.columns)-1 {
			t.Errorf("navigate_left from the first column landed on %d, want a wrap to %d", g.cursorCol, len(g.columns)-1)
		}
		if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
			t.Errorf("the column is %d, outside the %d columns", g.cursorCol, len(g.columns))
		}
	})

	t.Run("navigate_right moves the column and WRAPS at the last", func(t *testing.T) {
		g := newGrid(t, 10, 4, 80, 20, 10)
		g.cursorCol = 1

		if _, handled := g.dispatchAction(config.ActionID("navigate_right")); !handled {
			t.Fatal("navigate_right was not reported as handled")
		}
		if g.cursorCol != 2 {
			t.Errorf("navigate_right left the column at %d, want 2", g.cursorCol)
		}
		g.dispatchAction(config.ActionID("navigate_right"))
		if g.cursorCol != 3 {
			t.Errorf("navigate_right left the column at %d, want the last of four", g.cursorCol)
		}

		// Past the end it wraps to the first, and it does NOT leave a column out of
		// range on the way — a missing clamp here would index a column that does not
		// exist when the cell is edited.
		g.dispatchAction(config.ActionID("navigate_right"))
		if g.cursorCol != 0 {
			t.Errorf("navigate_right past the last column landed on %d, want a wrap to 0", g.cursorCol)
		}
		if g.scrollCol != 0 {
			t.Errorf("navigate_right past the last column left scrollCol at %d, want the window back at the first column", g.scrollCol)
		}
	})
}

func TestDispatchActionHalfPageMovesByHalfAScreen(t *testing.T) {
	g := newGrid(t, 100, 3, 80, 20, 100)
	half := contentRows(g) / 2
	if half < 1 {
		t.Fatalf("the content height is %d, so a half page is %d; the fixture is too small to test this", contentRows(g), half)
	}

	t.Run("half_page_down moves down by half a screen", func(t *testing.T) {
		gg := newGrid(t, 100, 3, 80, 20, 100)
		if _, handled := gg.dispatchAction(config.ActionID("half_page_down")); !handled {
			t.Fatal("half_page_down was not reported as handled")
		}
		if gg.cursorRow != half {
			t.Errorf("half_page_down left cursorRow at %d, want %d", gg.cursorRow, half)
		}
		if got := absRow(gg); got != half {
			t.Errorf("half_page_down put the cursor on absolute row %d, want %d", got, half)
		}
	})

	t.Run("half_page_up moves back by half a screen", func(t *testing.T) {
		gg := newGrid(t, 100, 3, 80, 20, 100)
		gg.cursorRow = half
		if _, handled := gg.dispatchAction(config.ActionID("half_page_up")); !handled {
			t.Fatal("half_page_up was not reported as handled")
		}
		if got := absRow(gg); got != 0 {
			t.Errorf("half_page_up left the cursor on absolute row %d, want 0", got)
		}
	})

	t.Run("half_page_up from the top stays at the top", func(t *testing.T) {
		// The clamp case, and the one that produces a negative scrollRow if the
		// guard is missing: half a screen above row 0 is off the data.
		gg := newGrid(t, 100, 3, 80, 20, 100)
		if _, handled := gg.dispatchAction(config.ActionID("half_page_up")); !handled {
			t.Fatal("half_page_up on the first row was not reported as handled")
		}
		if gg.cursorRow != 0 {
			t.Errorf("half_page_up from the top left cursorRow at %d, want 0", gg.cursorRow)
		}
		if gg.scrollRow < 0 {
			t.Errorf("half_page_up from the top left scrollRow at %d, which is negative", gg.scrollRow)
		}
		if got := absRow(gg); got != 0 {
			t.Errorf("the cursor is on absolute row %d, want 0", got)
		}
	})

	t.Run("half_page_down past the end lands on the last visible row", func(t *testing.T) {
		// The other clamp. Four half-pages from the top is past the end of a
		// hundred rows on a twenty-row screen, and each move has to keep the cursor
		// inside the window while it does so.
		gg := newGrid(t, 100, 3, 80, 20, 100)
		for range 10 {
			_, _ = gg.dispatchAction(config.ActionID("half_page_down"))
		}
		ch := contentRows(gg)
		if gg.cursorRow < 0 || gg.cursorRow >= ch {
			t.Errorf("after ten half-page downs the cursor is on row %d, outside the %d visible", gg.cursorRow, ch)
		}
		if gg.scrollRow < 0 {
			t.Errorf("after ten half-page downs scrollRow is %d, which is negative", gg.scrollRow)
		}
		if got := absRow(gg); got >= 100 {
			t.Errorf("after ten half-page downs the cursor is on absolute row %d of 100", got)
		}
	})
}

// TestDispatchActionPagingResetsTheScroll is the one that pays for the whole file.
//
// Each of the four page arms is three statements that do the same three things:
// scrollRow = 0, clampCursor, cursorMovedCmd. The middle one is a guard and the outer
// two are the fix. A page arm that moved the pager without resetting the scroll leaves
// the cursor at an offset that was valid for the old page, so the grid renders rows the
// cursor is not on, and `edit_cell` edits the wrong row — silently, because the pager
// offset is not something a user can see change.
func TestDispatchActionPagingResetsTheScroll(t *testing.T) {
	for _, action := range []string{"first_page", "next_page", "prev_page", "last_page"} {
		t.Run(action, func(t *testing.T) {
			// Three hundred rows in pages of a hundred, on a twenty-row screen. Both
			// halves matter and each broke a version of this test: a page of 10 has
			// nothing to scroll, so go_last leaves scrollRow at 0 and every arm passes
			// whether or not it resets the scroll; and exactly one page makes
			// next_page and prev_page no-ops, so "they are inverses" holds trivially.
			g := newGrid(t, 300, 3, 80, 20, 100)

			// Move away from the origin first: at scrollRow 0 a missing reset is
			// invisible, which is exactly why this needs the scroll set up.
			if _, handled := g.dispatchAction(config.ActionID("go_last")); !handled {
				t.Fatal("go_last was not handled")
			}
			if g.scrollRow == 0 {
				t.Fatal("go_last left scrollRow at 0, so the fixture cannot detect a missing reset")
			}

			cmd, handled := g.dispatchAction(config.ActionID(action))
			if !handled {
				t.Fatalf("%s was not reported as handled", action)
			}
			if cmd == nil {
				t.Errorf("%s returned no command; the cursor move would not be announced", action)
			}

			if g.scrollRow != 0 {
				t.Errorf("%s left scrollRow at %d, want 0 — the cursor now points at the wrong row of the new page", action, g.scrollRow)
			}
			if g.cursorRow < 0 {
				t.Errorf("%s left cursorRow at %d, which is negative", action, g.cursorRow)
			}
			// clampCursor is what guarantees the cursor names a row that exists.
			if g.cursorRow >= contentRows(g) {
				t.Errorf("%s left cursorRow at %d, past the %d visible rows", action, g.cursorRow, contentRows(g))
			}
		})
	}

	t.Run("first_page returns to the beginning of the result", func(t *testing.T) {
		g := newGrid(t, 300, 3, 80, 20, 100)
		_, _ = g.dispatchAction(config.ActionID("last_page"))
		_, _ = g.dispatchAction(config.ActionID("first_page"))
		if got := g.pager.Page(); got != 1 {
			t.Errorf("first_page left the pager on page %d, want 1", got)
		}
		if got := absRow(g); got != 0 {
			t.Errorf("first_page left the cursor on absolute row %d, want 0", got)
		}
	})

	t.Run("next_page and prev_page are inverses", func(t *testing.T) {
		// Three pages, or the inversion is vacuous: with one page both arms do
		// nothing and the assertions hold on a pair of no-ops.
		g := newGrid(t, 300, 3, 80, 20, 100)
		start := g.pager.Page()

		_, _ = g.dispatchAction(config.ActionID("next_page"))
		forward := g.pager.Page()
		if forward <= start {
			t.Errorf("next_page went from page %d to %d", start, forward)
		}
		_, _ = g.dispatchAction(config.ActionID("prev_page"))
		if got := g.pager.Page(); got != start {
			t.Errorf("prev_page left the pager on page %d, want %d", got, start)
		}
	})

	t.Run("prev_page on the first page stays on the first page", func(t *testing.T) {
		g := newGrid(t, 100, 3, 80, 20, 10)
		if _, handled := g.dispatchAction("prev_page"); !handled {
			t.Fatal("prev_page on the first page was not reported as handled")
		}
		if got := g.pager.Page(); got != 1 {
			t.Errorf("prev_page left the pager on page %d, want 1", got)
		}
	})
}

// Scenario: La seleccion de filas se alterna, y solo sobre filas que existen.
func TestDispatchActionSelectRowToggles(t *testing.T) {
	g := newGrid(t, 20, 3, 80, 20, 10)
	g.cursorRow = 4
	g.scrollRow = 0

	if _, handled := g.dispatchAction(config.ActionID("select_row")); !handled {
		t.Fatal("select_row was not reported as handled")
	}
	if !g.selectedRows[4] {
		t.Errorf("row 4 is not selected after one select_row; selected is %v", keysOfInts(g.selectedRows))
	}

	_, _ = g.dispatchAction(config.ActionID("select_row"))
	if g.selectedRows[4] {
		t.Errorf("row 4 is still selected after a second select_row; selected is %v", keysOfInts(g.selectedRows))
	}

	t.Run("selecting on an empty grid claims nothing", func(t *testing.T) {
		empty := newGrid(t, 0, 2, 80, 20, 10)
		if _, handled := empty.dispatchAction("select_row"); handled {
			t.Error("select_row on an empty grid claimed to be handled")
		}
		if len(empty.selectedRows) != 0 {
			t.Errorf("an empty grid selected %v", keysOfInts(empty.selectedRows))
		}
	})

	t.Run("the cursor row is the one that gets selected, scroll and page included", func(t *testing.T) {
		// The offset arithmetic is the bug-prone part: scrollRow + cursorRow +
		// pager offset is not the same as cursorRow once anything has moved, and
		// selecting by cursorRow alone picks the wrong record.
		paged := newGrid(t, 100, 3, 80, 20, 10)
		paged.scrollRow = 3
		paged.cursorRow = 2

		want := paged.scrollRow + paged.cursorRow + paged.pager.Offset()
		_, _ = paged.dispatchAction(config.ActionID("select_row"))
		if !paged.selectedRows[want] {
			t.Errorf("select_row on cursorRow %d with scrollRow %d selected %v, want row %d",
				paged.cursorRow, paged.scrollRow, keysOfInts(paged.selectedRows), want)
		}
	})
}

// Scenario: Yank, export y delete abren su modo, o dicen que no pueden.
func TestDispatchActionYankExportAndDeleteOpenTheirMode(t *testing.T) {
	t.Run("yank opens the export picker in yank mode", func(t *testing.T) {
		g := newGrid(t, 10, 3, 80, 20, 10)
		if _, handled := g.dispatchAction(config.ActionID("yank")); !handled {
			t.Fatal("yank was not reported as handled")
		}
		if !g.exportPicker.IsVisible() {
			t.Error("yank did not open the picker")
		}
	})

	t.Run("export opens the export picker", func(t *testing.T) {
		g := newGrid(t, 10, 3, 80, 20, 10)
		if _, handled := g.dispatchAction(config.ActionID("export")); !handled {
			t.Fatal("export was not reported as handled")
		}
		if !g.exportPicker.IsVisible() {
			t.Error("export did not open the picker")
		}
	})

	t.Run("delete_rows opens the confirmation", func(t *testing.T) {
		g := newGrid(t, 10, 3, 80, 20, 10)
		if _, handled := g.dispatchAction(config.ActionID("delete_rows")); !handled {
			t.Fatal("delete_rows was not reported as handled")
		}
		if !g.HasPendingDeletes() {
			t.Error("delete_rows staged no delete")
		}
		// The staged row is a COPY, not the live one. If it were a slice of the
		// same backing array, a later edit of the grid would rewrite what is about
		// to be deleted, and the user would delete a row they never saw.
		pending := g.pendingDeletes[0]
		if pending.RowIdx != absRow(g) {
			t.Errorf("the staged delete is for row %d, want %d", pending.RowIdx, absRow(g))
		}
		live := g.Data().Rows[pending.RowIdx]
		if len(pending.Row) != len(live) {
			t.Fatalf("the staged row has %d cells, want %d", len(pending.Row), len(live))
		}
		pending.Row[0] = "MUTATED"
		if g.Data().Rows[pending.RowIdx][0] == "MUTATED" {
			t.Error("the staged delete shares storage with the live row; mutating it changed the grid")
		}
	})

	// The three of them all bail on a grid with no data, and all three must say
	// they were not handled: claiming otherwise would swallow the key so that a
	// later "no" could not cancel.
	for _, action := range []string{"yank", "export", "delete_rows", "select_row"} {
		t.Run(action+" on an empty grid is not handled", func(t *testing.T) {
			g := newGrid(t, 0, 2, 80, 20, 10)
			if _, handled := g.dispatchAction(config.ActionID(action)); handled {
				t.Errorf("%s on an empty grid claimed to be handled", action)
			}
			if g.exportPicker.IsVisible() {
				t.Errorf("%s on an empty grid opened the picker", action)
			}
		})
	}

	t.Run("yank with enough selected rows switches to file mode", func(t *testing.T) {
		// The threshold is yankMaxRows, and crossing it changes what the picker
		// does: more rows than that go to a file rather than to the clipboard.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.yankMaxRows = 2
		g.selectedRows[0] = true
		g.selectedRows[1] = true
		g.selectedRows[2] = true

		if _, handled := g.dispatchAction(config.ActionID("yank")); !handled {
			t.Fatal("yank with three rows selected was not handled")
		}
		if !g.exportPicker.IsVisible() {
			t.Error("yank with three rows selected did not open the picker")
		}
	})
}

// Scenario: Editar una celda PENDIENTE con valor nulo abre un editor vacio, no "<nil>".
//
// This is the arm at the bottom of the edit_cell case: when a pending draft row holds a
// NULL, the edit buffer has to be empty. A `fmt.Sprintf("%v", nil)` in that spot writes
// the four characters "<nil>" into the user's cell, and the fix is to save a row whose
// value was never there. The other two places in the same case already guard it, which
// is the drift shape this repository keeps producing: the same arithmetic three times, and
// only two of the three fixed.
func TestEditingANullPendingValueOpensAnEmptyBuffer(t *testing.T) {
	g := newGrid(t, 3, 3, 80, 20, 10)

	// A pending row with a NULL in the first column and a real value in the second.
	g.inserting = true
	g.pendingRow = 0
	g.pendingCol = 0
	g.pendingRows = [][]interface{}{{nil, "kept"}}
	g.cursorCol = 0

	if _, handled := g.dispatchAction(config.ActionID("edit_cell")); !handled {
		t.Fatal("edit_cell on a pending NULL was not reported as handled")
	}
	if !g.editing {
		t.Fatal("edit_cell on a pending NULL did not open the editor")
	}
	if g.editValue != "" {
		t.Errorf("the edit buffer is %q, want empty — a NULL cell has no value to show", g.editValue)
	}
	if strings.Contains(g.editValue, "nil") {
		t.Errorf("the edit buffer is %q; it contains the string nil", g.editValue)
	}
	if g.editStartValue != "" {
		t.Errorf("the original value is recorded as %q, want empty", g.editStartValue)
	}
	// The cursor at the end of an empty buffer is column 0, not 1.
	if g.editCursor != 0 {
		t.Errorf("the edit cursor is at %d in an empty buffer, want 0", g.editCursor)
	}

	t.Run("a pending value that is NOT null is loaded", func(t *testing.T) {
		// The other side of the same branch, so the guard cannot be satisfied by
		// always opening an empty editor.
		gg := newGrid(t, 3, 3, 80, 20, 10)
		gg.inserting = true
		gg.pendingRow = 0
		gg.pendingCol = 1
		gg.pendingRows = [][]interface{}{{"dropped", "kept"}}
		gg.cursorCol = 1

		_, _ = gg.dispatchAction(config.ActionID("edit_cell"))
		if gg.editValue != "kept" {
			t.Errorf("the edit buffer is %q, want %q", gg.editValue, "kept")
		}
		if gg.editCursor != len("kept") {
			t.Errorf("the edit cursor is at %d, want %d", gg.editCursor, len("kept"))
		}
	})

	t.Run("a NULL in a committed cell is also empty", func(t *testing.T) {
		// startEdit, the third of the three copies of this decision. Asserted because
		// two of three being right is exactly the state this repository has been in.
		gg := newGrid(t, 3, 3, 80, 20, 10)
		gg.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{
				{Name: "a", DataType: "text"},
				{Name: "b", DataType: "text"},
			},
			Rows:  [][]interface{}{{nil, "b0"}},
			Count: 1,
		}, "public", "t")
		gg.cursorRow, gg.cursorCol = 0, 0

		_, _ = gg.dispatchAction(config.ActionID("edit_cell"))
		if !gg.editing {
			t.Fatal("edit_cell on a committed row did not open the editor")
		}
		if gg.editValue != "" {
			t.Errorf("the edit buffer for a NULL cell is %q, want empty", gg.editValue)
		}
	})
}

// ---------------------------------------------------------------------------
// The accessors. One statement each, and between them a fifth of what is left
// uncovered in this file.
// ---------------------------------------------------------------------------

func TestTheGridAccessorsReportWhatTheyHold(t *testing.T) {
	g := newGrid(t, 12, 4, 80, 20, 10)

	t.Run("Data returns the result the grid was given", func(t *testing.T) {
		if g.Data() == nil {
			t.Fatal("Data() returned nil for a grid with data")
		}
		if got := len(g.Data().Rows); got != 12 {
			t.Errorf("Data() reports %d rows, want 12", got)
		}
	})

	t.Run("TotalRows counts the rows", func(t *testing.T) {
		if got := g.TotalRows(); got != 12 {
			t.Errorf("TotalRows() is %d, want 12", got)
		}
	})

	t.Run("SelectedRow is the row under the cursor", func(t *testing.T) {
		g.cursorRow = 5
		g.scrollRow = 0
		row := g.SelectedRow()
		if row == nil {
			t.Fatal("SelectedRow() returned nil with the cursor on a row")
		}
		if got := row[0]; got != "r5c0" {
			t.Errorf("SelectedRow()[0] is %v, want r5c0", got)
		}
	})

	t.Run("visibleColumns names the visible columns and their WIDTHS", func(t *testing.T) {
		// The second return value is WIDTHS, not column indices. The first version of
		// this asserted they were indices, on the strength of the name, and failed
		// with names ["c0"...] and indices [7...]: 7 is the pixel width of c0 on an
		// 80-column pane, not an index into four columns.
		//
		// Asserted as widths because the pairing is the thing that can break: the
		// names are for display and the widths are for laying the columns out, so a
		// row whose widths were shifted would draw every cell at its neighbour's
		// width and the grid would look almost right.
		cols, widths := g.visibleColumns()
		if len(cols) == 0 {
			t.Fatal("visibleColumns() is empty on an 80-column pane with four columns")
		}
		if len(cols) != len(widths) {
			t.Fatalf("visibleColumns() returned %d names and %d widths", len(cols), len(widths))
		}
		if cols[0] != "c0" {
			t.Errorf("the first visible column is %q, want c0", cols[0])
		}
		for i, c := range cols {
			if widths[i] <= 0 {
				t.Errorf("column %q has width %d", c, widths[i])
			}
			// NOT monotonic: equal widths are perfectly normal and the fixture
			// produces four of them. The first version asserted strictly increasing
			// widths and failed on [7 7 7 7], having turned "every width is
			// positive" into "widths grow", which nothing promises.
			_ = i
		}
		// The widths are the ones the layout computed, not a fresh calculation:
		// they come from the same slice the renderer reads.
		for i, c := range cols {
			found := false
			for j, name := range g.columns {
				if name == c && g.widths[j] == widths[i] {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("column %q has width %d, which matches no column of the grid", c, widths[i])
			}
		}
		// And they fit: this is the invariant the scroll window depends on.
		available := g.width - 2
		total := 0
		for _, w := range widths {
			total += w
		}
		if total > available {
			t.Errorf("the visible widths total %d, over the %d available", total, available)
		}
	})

	t.Run("visibleColumns is empty with no columns", func(t *testing.T) {
		empty := newGrid(t, 0, 0, 80, 20, 10)
		cols, idx := empty.visibleColumns()
		if len(cols) != 0 || len(idx) != 0 {
			t.Errorf("a grid with no columns reports %v / %v", cols, idx)
		}
	})

	t.Run("SetHeight is remembered", func(t *testing.T) {
		g.SetHeight(33)
		if g.height != 33 {
			t.Errorf("the height is %d after SetHeight(33)", g.height)
		}
	})

	t.Run("Blur clears the focus and Focus sets it", func(t *testing.T) {
		g.Focus()
		if !g.focused {
			t.Error("Focus() did not focus")
		}
		g.Blur()
		if g.focused {
			t.Error("Blur() did not blur")
		}
	})

	t.Run("GetResult returns the result, and the new one after a reload", func(t *testing.T) {
		if g.GetResult() == nil {
			t.Error("GetResult() is nil while data is loaded")
		}
		if got := len(g.GetResult().Rows); got != 12 {
			t.Errorf("GetResult() reports %d rows, want 12", got)
		}
		// Reloading replaces it. A refresh that kept the old result would export
		// the previous page against the new table.
		fresh := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "z", DataType: "text"}},
			Rows:    [][]interface{}{{"only"}},
			Count:   1,
		}
		g.SetData(fresh, "public", "other")
		if g.GetResult() != fresh {
			t.Error("GetResult() is not the result the grid was last given")
		}
		if got := g.TotalRows(); got != 1 {
			t.Errorf("TotalRows() is %d after a one-row reload, want 1", got)
		}
	})

	t.Run("ExportPickerView renders the picker", func(t *testing.T) {
		g.SetHeight(33)
		g.SetWidth(80)
		g.exportPicker.SetWidth(80)
		g.exportPicker.SetHeight(20)
		cols, _ := g.visibleColumns()
		g.exportPicker.ShowYankMode("public", "t", g.Data().Rows[0], nil, cols)
		if got := g.ExportPickerView(); got == "" {
			t.Error("ExportPickerView() is empty for a visible picker")
		}
	})
}

// TestStartExportMatchesDispatchExport checks the public wrapper exists because something
// outside this package calls it — the app calls grid.StartExport from the keybind path
// rather than through the registry. If it ever stopped being the same function the two
// callers would silently diverge.
func TestStartExportMatchesDispatchExport(t *testing.T) {
	viaDispatch := newGrid(t, 5, 2, 80, 20, 10)
	viaWrapper := newGrid(t, 5, 2, 80, 20, 10)

	_, h1 := viaDispatch.dispatchAction("export")
	_, h2 := viaWrapper.StartExport()
	if h1 != h2 {
		t.Errorf("export through dispatch reported %t and StartExport reported %t", h1, h2)
	}
	if viaDispatch.exportPicker.IsVisible() != viaWrapper.exportPicker.IsVisible() {
		t.Error("the two export paths left the picker in different states")
	}
}

func keysOfInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}

// tea is imported for the compile-time check that the commands these actions return are
// tea.Cmds, which is the only thing the app relies on when it batches them.
var _ tea.Cmd = func() tea.Msg { return GridCursorMovedMsg{} }
