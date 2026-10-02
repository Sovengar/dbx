// The bounds guards of table.go, reached AT the boundary rather than beside it.
//
// Every function in this file covers one shape: a guard of the form
//
//	if i >= 0 && i < len(S) { use S[i] }
//
// where a CONDITIONALS_BOUNDARY mutant turns `<` into `<=`, so the only state that
// tells the original from the mutant is `i == len(S)` — where the original declines
// the index and the mutant performs it, reading one element past the end of a slice.
//
// WHY THE RECOVERY MATTERS, and why these were allowlisted at all
//
// A panic inside a test aborts the whole test binary. The mutation tool then has no
// per-test result to read for that mutant and scores it as a SURVIVOR — which is how
// twenty-nine of these ended up written down as "there is no way to kill these from
// the test side". That was true only of the tests that existed. `mustNotPanic` (in
// table_contract_test.go) recovers the panic and reports it through the testing
// package, so the crash becomes an ordinary red test and the mutant is killed for
// real. The guard is load-bearing and now it is also VERIFIED.
//
// WHY THE STATE IS BUILT DIRECTLY
//
// These are not cursor-movement tests. The contract under test is "an index that has
// fallen out of range does not take the slice with it", and the way that state comes
// about is a stale entry surviving something the grid cannot see: a selection left
// over from a query that returned fewer rows, a draft recorded against a row that was
// then deleted, a pending update carried across a refresh. Building that state
// directly IS the input to the contract; there is no key sequence that produces it
// deterministically, and a test that cannot reach the boundary proves nothing.
//
// Each fixture therefore asserts the boundary it claims to be at BEFORE calling the
// function under test. A guard test that silently sits one short of the boundary is
// the worst kind of hole: it looks like coverage and is not.

package grid

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// staleSelection builds a grid whose selection map holds an index one past the last
// row — the shape left behind by a query that returned fewer rows than were selected.
//
// It returns the grid and the stale index so a test can state what it is asserting.
func staleSelection(t *testing.T, nRows, nCols int) (*Grid, int) {
	t.Helper()
	g := newGrid(t, nRows, nCols, 120, 20, 100)
	stale := len(g.data.Rows) // exactly one past the end
	// TWO stale keys, not one: the loops under test sit behind `count > 1` and
	// `len(...) > 1`, so a single entry never enters them at all. That was
	// measured — the first run of this fixture left four mutants alive because it
	// put the grid on the single-selection branch instead.
	g.selectedRows = map[int]bool{stale: true, stale - 1: true}
	if len(g.selectedRows) != 2 {
		t.Fatalf("the fixture holds %d selections, want two", len(g.selectedRows))
	}
	return g, stale
}

// ---------------------------------------------------------------------------
// The selection index
// ---------------------------------------------------------------------------

// Scenario: Una seleccion con un indice OBSOLETO no se lleva la fila.
//
// Four functions walk `selectedRows` and index `g.data.Rows` with the key. The key is
// an absolute row index that can outlive the row it names, so each of them has to
// check it against the row count — and a check that is off by one at the boundary is
// exactly the difference between declining an index and performing it.
//
// The assertion is on what the function RETURNS as well as on not crashing: a guard
// that dropped the stale entry but also dropped a valid one would pass a smoke test.
func TestSelectedRowOperations_SurviveAnIndexPastTheLastRow(t *testing.T) {
	t.Run("SelectedRows", func(t *testing.T) {
		g, stale := staleSelection(t, 3, 2)
		// The fixture holds two indices: one past the end and one inside it. So
		// the answer is not "nothing" — it is the ONE valid row. Asserting
		// "nothing" would be a weaker test, because a guard that dropped the
		// whole selection would pass it.
		var got [][]interface{}
		mustNotPanic(t, "SelectedRows with a stale selection index", func() { got = g.SelectedRows() })
		if len(got) != 1 {
			t.Fatalf("SelectedRows returned %d rows, want the one valid row of the two selected (index %d is past the last of %d)",
				len(got), stale, len(g.data.Rows))
		}
		if want := stale - 1; got[0][0] != marker(want, 0) {
			t.Errorf("SelectedRows returned %v, want the row at the valid index %d", got[0][0], want)
		}
	})

	// The three commands each have a MULTI-selection branch and a single-selection
	// branch, and the stale-index loops live in both. Two stale keys take the multi
	// branch; the single branch gets its own fixtures below.
	t.Run("startYank over two stale selections", func(t *testing.T) {
		g, _ := staleSelection(t, 3, 2)
		mustNotPanic(t, "startYank with two stale selection indices", func() { g.startYank() })
	})

	t.Run("startExport over two stale selections", func(t *testing.T) {
		g, _ := staleSelection(t, 3, 2)
		mustNotPanic(t, "startExport with two stale selection indices", func() { g.startExport() })
	})

	t.Run("startDelete over two stale selections", func(t *testing.T) {
		g, _ := staleSelection(t, 3, 2)
		mustNotPanic(t, "startDelete with two stale selection indices", func() { g.startDelete() })
	})

	t.Run("startYank over one stale selection", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.selectedRows = map[int]bool{len(g.data.Rows): true}
		mustNotPanic(t, "startYank with one stale selection index", func() { g.startYank() })
	})

	t.Run("startExport over one stale selection", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.selectedRows = map[int]bool{len(g.data.Rows): true}
		mustNotPanic(t, "startExport with one stale selection index", func() { g.startExport() })
	})

	// And the control: a selection that is IN range is still returned. Without this a
	// test that broke the guard in the other direction — returning nothing always —
	// would pass.
	t.Run("an index inside the range is still returned", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.selectedRows = map[int]bool{1: true}
		got := g.SelectedRows()
		if len(got) != 1 {
			t.Fatalf("SelectedRows returned %d rows, want the one selected", len(got))
		}
		if got[0][0] != marker(1, 0) {
			t.Errorf("SelectedRows returned row %v, want the row at index 1", got[0])
		}
	})

	// And the far end: a NEGATIVE index is out of range too, and the guard has to
	// decline it as well.
	t.Run("a negative index is declined", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.selectedRows = map[int]bool{-1: true}
		var got [][]interface{}
		mustNotPanic(t, "SelectedRows with a negative selection index", func() { got = g.SelectedRows() })
		if len(got) != 0 {
			t.Errorf("SelectedRows returned %d rows for a negative index, want none", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// The draft index
// ---------------------------------------------------------------------------

// Scenario: Un borrador con un indice OBSOLETO no se aplica.
//
// A draft records the value a cell had before an edit. Its RowIdx is absolute, and a
// row can disappear between recording and discarding — a refresh, a delete, a
// re-query. So each of these has to decline a RowIdx past the last row instead of
// writing through it.
//
// The discard functions WRITE, so the assertion here is not just "no crash": the cell
// outside the range must be untouched, and a valid draft must still be applied.
func TestDraftOperations_SurviveAnIndexPastTheLastRow(t *testing.T) {
	t.Run("DiscardAllDrafts", func(t *testing.T) {
		g, stale := staleSelection(t, 3, 2)
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: stale, ColIdx: 0, OldValue: "SHOULD-NOT-BE-WRITTEN"},
		}
		mustNotPanic(t, "DiscardAllDrafts with a stale draft", func() { g.DiscardAllDrafts() })
		// Every cell must still hold what the query returned.
		for r, row := range g.data.Rows {
			for c, v := range row {
				if v == "SHOULD-NOT-BE-WRITTEN" {
					t.Errorf("the stale draft was written through to row %d column %d", r, c)
				}
			}
		}
	})

	t.Run("UndoRowDrafts", func(t *testing.T) {
		g, stale := staleSelection(t, 3, 2)
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: stale, ColIdx: 0, OldValue: "SHOULD-NOT-BE-WRITTEN"},
		}
		// The inner guard is nested inside `u.RowIdx == targetRowIdx`, where
		// targetRowIdx is the CURSOR's row. A stale draft alone is not enough: the
		// cursor has to be out of range too, or the outer test rejects the draft
		// before the inner one is reached. The first run of this fixture left the
		// mutant alive for exactly that reason.
		g.cursorRow = len(g.data.Rows) // sum == len(rows), out of range
		if got := g.scrollRow + g.cursorRow + g.pager.Offset(); got != stale {
			t.Fatalf("the cursor's row is %d and the draft's is %d, want them equal so the outer test passes", got, stale)
		}
		mustNotPanic(t, "UndoRowDrafts with a stale draft", func() { g.UndoRowDrafts() })
		for r, row := range g.data.Rows {
			for c, v := range row {
				if v == "SHOULD-NOT-BE-WRITTEN" {
					t.Errorf("the stale draft was written through to row %d column %d", r, c)
				}
			}
		}
	})

	t.Run("groupUpdatesByRow", func(t *testing.T) {
		g, stale := staleSelection(t, 3, 2)
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: stale, ColIdx: 0, OldValue: "x", NewValue: "y"},
		}
		var got []rowUpdateGroup
		mustNotPanic(t, "groupUpdatesByRow with a stale draft", func() { got = g.groupUpdatesByRow() })
		if len(got) != 0 {
			t.Errorf("groupUpdatesByRow produced %d groups, want none for an index past the last row", len(got))
		}
	})

	// The controls, because a guard that dropped everything would pass the three
	// above: a valid draft is applied, and a valid row is still grouped.
	t.Run("a draft inside the range is still discarded", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		original := g.data.Rows[1][0]
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: 1, ColIdx: 0, OldValue: original},
		}
		g.DiscardAllDrafts()
		if g.data.Rows[1][0] != original {
			t.Errorf("discarding a valid draft left row 1 column 0 as %v, want the recorded old value %v",
				g.data.Rows[1][0], original)
		}
	})

	t.Run("a draft inside the range is still grouped", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.pendingUpdates = []PendingUpdate{
			{RowIdx: 2, ColIdx: 0, OldValue: "x", NewValue: "y"},
			{RowIdx: 2, ColIdx: 1, OldValue: "z", NewValue: "w"},
		}
		got := g.groupUpdatesByRow()
		if len(got) != 1 {
			t.Fatalf("groupUpdatesByRow produced %d groups, want one for row 2", len(got))
		}
		if len(got[0].updates) != 2 {
			t.Errorf("the group for row 2 holds %d updates, want both of them", len(got[0].updates))
		}
	})

	t.Run("a negative draft index is declined", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.pendingUpdates = []PendingUpdate{{RowIdx: -1, ColIdx: 0, OldValue: "x"}}
		mustNotPanic(t, "DiscardAllDrafts with a negative draft index", func() { g.DiscardAllDrafts() })
		mustNotPanic(t, "groupUpdatesByRow with a negative draft index", func() { g.groupUpdatesByRow() })
	})
}

// ---------------------------------------------------------------------------
// The pending-insert index
// ---------------------------------------------------------------------------

// Scenario: Una fila pendiente con un indice OBSOLETO no borra la fila de al lado.
//
// Cancelling an insert removes the row at `pendingRow` out of `pendingRows` with a
// splice, so an index one past the end would remove nothing and — worse — a guard that
// is off by one would splice off the end and panic inside append.
//
// The stale index is reachable for the same reason the others are: pendingRows is
// appended to and spliced, and a cancel that races a second insert leaves the counter
// behind the slice.
func TestPendingInsertOperations_SurviveAnIndexPastTheLastRow(t *testing.T) {
	t.Run("handleEditKey escaping a stale pending row", func(t *testing.T) {
		g := newGrid(t, 2, 2, 120, 20, 100)
		g.inserting = true
		g.pendingRows = [][]interface{}{{"new0", "new1"}}
		g.pendingRow = len(g.pendingRows) // one past the end
		g.editValue = "typed"
		g.editStartValue = "typed"

		mustNotPanic(t, "escaping an insert with a stale pending row", func() {
			g.handleEditKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		})
		if len(g.pendingRows) != 1 {
			t.Errorf("escaping left %d pending rows, want the one valid row untouched", len(g.pendingRows))
		}
	})

	t.Run("commitEdit with a stale pending row", func(t *testing.T) {
		g := newGrid(t, 2, 2, 120, 20, 100)
		g.inserting = true
		g.pendingRows = [][]interface{}{{"new0", "new1"}}
		g.pendingRow = len(g.pendingRows)
		g.editValue = "typed"

		mustNotPanic(t, "committing an insert with a stale pending row", func() {
			g.commitEdit()
		})
	})

	// The control: a valid pending row IS removed on escape. Without it, a guard that
	// removed nothing would pass.
	t.Run("a valid pending row is removed", func(t *testing.T) {
		g := newGrid(t, 2, 2, 120, 20, 100)
		g.inserting = true
		g.pendingRows = [][]interface{}{{"drop0", "drop1"}, {"keep0", "keep1"}}
		g.pendingRow = 0
		g.editValue = "typed"
		g.editStartValue = "typed"

		g.handleEditKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if len(g.pendingRows) != 1 {
			t.Fatalf("escaping left %d pending rows, want the second one only", len(g.pendingRows))
		}
		if g.pendingRows[0][0] != "keep0" {
			t.Errorf("escaping removed the wrong row: kept %v", g.pendingRows[0])
		}
	})
}

// ---------------------------------------------------------------------------
// The column index
// ---------------------------------------------------------------------------

// Scenario: Un cursor en una columna OBSOLETA no se lleva la columna.
//
// cursorCol indexes `g.columns`, and the column list is rebuilt on every query while
// the cursor is not necessarily reset with it. So these three have to decline a
// cursorCol past the last column.
//
// Each of them is reached from a key the user can press, so the assertion is on the
// EFFECT and not only on the crash: no editor opens, no navigation fires, no sort is
// requested.
func TestColumnOperations_SurviveACursorPastTheLastColumn(t *testing.T) {
	setup := func() *Grid {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.cursorCol = len(g.columns) // one past the end
		return g
	}

	t.Run("startEdit", func(t *testing.T) {
		g := setup()
		mustNotPanic(t, "startEdit with the cursor past the last column", func() { g.startEdit() })
		if g.editing {
			t.Error("startEdit opened an editor with the cursor past the last column")
		}
	})

	t.Run("navigateFK", func(t *testing.T) {
		g := setup()
		mustNotPanic(t, "navigateFK with the cursor past the last column", func() { g.navigateFK() })
	})

	t.Run("toggleSort", func(t *testing.T) {
		g := setup()
		// The observable difference here is NOT a crash. The guard is what makes
		// toggleSort return nil; without it the stale index reaches the header,
		// which declines it, and the function then returns a sort command for
		// whatever column WAS sorted before — so a cursor that has wandered past
		// the last column silently re-sorts the table by an unrelated one.
		// Asserting the return value is what kills this; mustNotPanic alone does
		// not.
		var cmd tea.Cmd
		mustNotPanic(t, "toggleSort with the cursor past the last column", func() { cmd = g.toggleSort() })
		if cmd != nil {
			t.Error("toggleSort returned a sort command with the cursor past the last column, want nil")
		}
		// And it really would have returned one: on a valid column it does, so
		// the assertion above is not vacuous.
		ok := newGrid(t, 3, 2, 120, 20, 100)
		ok.cursorCol = 0
		if ok.toggleSort() == nil {
			t.Error("toggleSort refused to sort a valid column, so the assertion above proves nothing")
		}
	})

	t.Run("the cursor one before the last still works", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.cursorCol = len(g.columns) - 1
		g.startEdit()
		if !g.editing {
			t.Error("startEdit refused to open on the last valid column")
		}
	})
}

// ---------------------------------------------------------------------------
// The width arithmetic
// ---------------------------------------------------------------------------

// Scenario: Un ancho que no deja espacio no divide por cero.
//
// calculateWidths caps column widths so that several fit in the viewport. The cap
// divides the available width by the number of columns, and it guards that division
// twice: the available width has to be positive AND there has to be at least one
// column. A guard that admits `available == 0` lets the division happen, and with an
// empty width list the divisor is zero as well.
//
// This is the one guard in the group whose failure mode is not a slice index but a
// divide-by-zero, and it is the only one reachable from a plain resize: SetWidth
// clamps at a small terminal and the grid is asked to lay itself out again.
func TestCalculateWidths_AViewportWithNoRoomDoesNotDivideByZero(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, nCols  int
		wantNoDivider bool
	}{
		{"a viewport with no room at all", 2, 2, true},
		{"a viewport one cell wide", 3, 2, true},
		{"a viewport narrower than its borders", 1, 2, true},
		{"a viewport with no columns at all", 80, 0, true},
		// The control: real room, so the division DOES happen and the
		// widths come back positive. A guard that skipped the cap
		// entirely would pass the cases above and fail this one.
		{"a normal viewport still caps the widths", 120, 6, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, 2, tc.nCols, tc.width, 20, 100)
			mustNotPanic(t, "calculateWidths", func() { g.calculateWidths() })

			for i, w := range g.widths {
				if w < 0 {
					t.Errorf("column %d came out with a negative width %d", i, w)
				}
			}
			if !tc.wantNoDivider {
				sum := 0
				for _, w := range g.widths {
					sum += w
				}
				if sum == 0 {
					t.Error("a normal viewport produced no widths at all: the cap must still divide")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The row index resolved from the cursor
// ---------------------------------------------------------------------------

// Scenario: Un cursor en una fila que ya no existe no se lleva la fila.
//
// These four resolve the absolute row as `scrollRow + cursorRow + pager.Offset()` and
// then check it. Unlike the previous groups, this one is reachable through keys alone:
// page forward past the end of the data and the sum lands one past the last row while
// the cursor itself is perfectly legal.
//
// So the fixture is a real grid paged off the end, and it ASSERTS the sum before
// calling anything — a fixture that is one row short of the boundary would pass
// against a guard that is one short too, which is the whole bug.
func TestRowLookups_SurviveACursorOnAPagePastTheLastRow(t *testing.T) {
	// Ten rows and a page of five: the sum has to be driven past the end, and a grid
	// of this shape can produce it.
	const rows, cols, pageSize = 10, 2, 5

	build := func(t *testing.T) (*Grid, int) {
		t.Helper()
		g := newGrid(t, rows, cols, 120, 10, pageSize)
		if !g.pager.NextPage() {
			t.Fatal("NextPage refused with 10 rows and 5 to a page")
		}
		// Walking down does NOT get here, and that was MEASURED for this
		// test rather than assumed: clampCursor holds the sum inside the
		// data, so paging to the end parks it on the LAST row, and
		// SetData resets it to zero when a query comes back shorter.
		//
		// So the boundary is unreachable through the state machine and the
		// guard is defensive code against a sum that some future path —
		// or a future bug — could produce. The fields are set directly
		// because THAT is the claim under test: given an absolute row past
		// the end, does the function decline it? A guard nobody can reach
		// is exactly the guard that rots, so it is worth pinning even
		// though no key sequence reaches it today.
		g.cursorRow = len(g.data.Rows) - g.pager.Offset() // 10 - 5 = 5
		g.scrollRow = 0
		return g, g.scrollRow + g.cursorRow + g.pager.Offset()
	}

	t.Run("the fixture really is one row past the end", func(t *testing.T) {
		g, absRow := build(t)
		if absRow != len(g.data.Rows) {
			t.Fatalf("the fixture's absolute row is %d over %d rows, want it to be %d — the boundary IS the test",
				absRow, len(g.data.Rows), len(g.data.Rows))
		}
	})

	t.Run("SelectedRow", func(t *testing.T) {
		g, _ := build(t)
		var got []interface{}
		mustNotPanic(t, "SelectedRow with the cursor past the last row", func() { got = g.SelectedRow() })
		if got != nil {
			t.Errorf("SelectedRow returned %v with the cursor past the last row, want nil", got)
		}
	})

	t.Run("startEdit", func(t *testing.T) {
		g, _ := build(t)
		mustNotPanic(t, "startEdit with the cursor past the last row", func() { g.startEdit() })
		if g.editing {
			t.Error("startEdit opened an editor for a row that does not exist")
		}
	})

	t.Run("startYank", func(t *testing.T) {
		g, _ := build(t)
		mustNotPanic(t, "startYank with the cursor past the last row", func() { g.startYank() })
	})

	t.Run("toggleRowSelection", func(t *testing.T) {
		g, stale := build(t)
		mustNotPanic(t, "toggleRowSelection with the cursor past the last row", func() { g.toggleRowSelection() })
		if g.selectedRows[stale] {
			t.Errorf("toggling selected the row at %d, which is past the last of %d", stale, len(g.data.Rows))
		}
	})

	// The controls, because a fix that declined everything would pass the four
	// above: the last real row still resolves, and a negative sum does not.
	t.Run("the last real row still resolves", func(t *testing.T) {
		g := newGrid(t, rows, cols, 120, 10, pageSize)
		if !g.pager.NextPage() {
			t.Fatal("NextPage refused")
		}
		g.cursorRow = len(g.data.Rows) - g.pager.Offset() - 1
		g.scrollRow = 0
		if got := g.scrollRow + g.cursorRow + g.pager.Offset(); got != len(g.data.Rows)-1 {
			t.Fatalf("the control fixture is at %d, want %d", got, len(g.data.Rows)-1)
		}
		row := g.SelectedRow()
		if row == nil {
			t.Fatal("SelectedRow returned nil for a row that exists")
		}
		if row[0] != marker(len(g.data.Rows)-1, 0) {
			t.Errorf("SelectedRow returned %v, want the last row %s", row[0], marker(len(g.data.Rows)-1, 0))
		}
	})

	t.Run("a negative sum is declined", func(t *testing.T) {
		g := newGrid(t, rows, cols, 120, 10, pageSize)
		g.cursorRow = -1
		g.scrollRow = 0
		if got := g.scrollRow + g.cursorRow + g.pager.Offset(); got >= 0 {
			t.Fatalf("the control fixture is at %d, want it negative", got)
		}
		mustNotPanic(t, "SelectedRow with a negative absolute row", func() { g.SelectedRow() })
	})
}

// ---------------------------------------------------------------------------
// The commit target
// ---------------------------------------------------------------------------

// Scenario: Confirmar una edicion sobre una fila que ya no existe no escribe nada.
//
// commitEdit writes the edited value back through `editRow`. The edit is started on a
// row that exists, and between starting and committing the row can go away — the query
// is refreshed, a delete lands, the page is turned. So editRow has to be checked
// against the row count at commit time, not just at start time.
func TestCommitEdit_SurvivesARowThatWentAwayUnderTheEditor(t *testing.T) {
	t.Run("commitEdit", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.editing = true
		g.editRow = len(g.data.Rows) // the row went away
		g.editCol = 0
		g.editValue = "written anyway"

		mustNotPanic(t, "commitEdit for a row that no longer exists", func() { g.commitEdit() })
		for r, row := range g.data.Rows {
			for c, v := range row {
				if v == "written anyway" {
					t.Errorf("the edit landed on row %d column %d, which is not the row that went away", r, c)
				}
			}
		}
	})

	t.Run("a valid edit row is still written", func(t *testing.T) {
		g := newGrid(t, 3, 2, 120, 20, 100)
		g.editing = true
		g.editRow = 1
		g.editCol = 0
		g.editValue = "accepted"

		g.commitEdit()
		if g.data.Rows[1][0] != "accepted" {
			t.Errorf("commitEdit left row 1 column 0 as %v, want the edited value", g.data.Rows[1][0])
		}
	})
}
