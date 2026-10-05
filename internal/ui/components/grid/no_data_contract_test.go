package grid

// Scenario: Los ocho guards "hay algo que hacer?" del grid, y los de "hay donde moverse?".
//
// hasRows and hasColumns replaced fourteen inline copies of two guards. Copying a guard
// into a function is where it stops being checked: this file is what replaced them, and
// this file is what keeps them honest — every one of the eight callers is invoked here on a
// grid that has nothing to act on, because a guard with no caller tested is a guard that
// can be deleted without anything failing.
//
// The two predicates are separate ON PURPOSE, and this is the file that says so. A table
// with columns and no rows is the normal state of a table nobody has written to, and
// inserting its first row has to work there. A function that merged the two would break
// that, and would look tidier.
//
// The movement guards are the other half: every one of them is `if totalRows == 0` or
// `if cursorRow < 0` on the way to arithmetic that indexes a slice, and a grid with no rows
// reaches all of them — the question is only whether it reaches them without a panic.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// withColumnsNoRows is a grid whose table exists and has no rows: the state every
// row-requiring action has to refuse, and the one insert has to survive.
func withColumnsNoRows(t *testing.T) *Grid {
	t.Helper()
	g := newGrid(t, 0, 0, 80, 20, 10)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
		},
		Count: 0,
	}, "public", "orders")
	if !g.hasColumns() || g.hasRows() {
		t.Fatalf("the fixture is not a table with columns and no rows: columns=%t rows=%t",
			g.hasColumns(), g.hasRows())
	}
	return g
}

// withNothing is a grid with neither rows nor columns, which is what a schema node that was
// never queried looks like.
func withNothing(t *testing.T) *Grid {
	t.Helper()
	g := newGrid(t, 0, 0, 80, 20, 10)
	g.data = &postgres.QueryResult{}
	if g.hasColumns() || g.hasRows() {
		t.Fatal("the fixture has something in it")
	}
	return g
}

// TestEveryRowActionIsRefusedOnATableWithNoRows is the eight guards, one case each.
//
// The assertion is a refusal, not a panic: a guard that answers by panicking satisfies
// "does not succeed" as well, and the difference is the whole point.
func TestEveryRowActionIsRefusedOnATableWithNoRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		// call is the function under test. It returns whatever it returns and whether it
		// claims to have handled the request.
		call func(*Grid) (tea.Cmd, bool)
	}{
		// startEdit answers nothing — it is the one row action with no command — so the
		// wrapper reports "not handled" from the grid's own state instead. That is the
		// observable for a function whose signature carries no answer.
		{"edit_cell", func(g *Grid) (tea.Cmd, bool) {
			g.startEdit()
			return nil, g.editing
		}},
		{"navigate_fk", func(g *Grid) (tea.Cmd, bool) {
			return g.navigateFK()
		}},
		// NOTE which of the row actions are NOT here. Yank, export, delete and refresh ask
		// for COLUMNS, not rows, and that is right for each of them:
		//
		//   yank and export build a header from the columns, so an empty result still has
		//     something to export
		//   refresh re-runs the query, which is exactly what a user does when the table
		//     looks empty because the filter is too narrow
		//
		// The first version of this test put all eight here on the reasoning that they were
		// "the row actions", and four of them reported a mismatch on correct code. Asking
		// for rows where a function asks for columns is not a near miss; it is the
		// difference between a guard and a bug.
		{"select_row", func(g *Grid) (tea.Cmd, bool) {
			before := len(g.SelectedRows())
			g.toggleRowSelection()
			return nil, len(g.SelectedRows()) != before
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := withColumnsNoRows(t)
			g.Focus()

			var cmd tea.Cmd
			var handled bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s panicked on a table with no rows: %v", tc.name, r)
					}
				}()
				cmd, handled = tc.call(g)
			}()

			if handled {
				t.Errorf("%s claimed to have handled a table with no rows", tc.name)
			}
			if cmd != nil {
				t.Errorf("%s issued a command on a table with no rows", tc.name)
			}
			if g.editing || g.inserting {
				t.Errorf("%s left the grid in an editing state with no row to edit", tc.name)
			}
			if len(g.SelectedRows()) != 0 {
				t.Errorf("%s selected something out of an empty table", tc.name)
			}
		})
	}

	t.Run("and REFRESH still works there", func(t *testing.T) {
		// The other half of the note above, and the most useful thing a user can do on an
		// empty result: re-run the query. A refresh that refused here would make a stale
		// filter look like an empty table.
		g := withColumnsNoRows(t)
		g.Focus()

		cmd, handled := g.handleRefreshKey()
		if !handled || cmd == nil {
			t.Fatalf("refresh was refused on an empty table: handled=%t", handled)
		}
	})

	t.Run("and insert_row STILL works there", func(t *testing.T) {
		// The reason the two predicates are separate. A table nobody has written to yet is
		// an ordinary state, and if INSERT is behind the row guard there is no way to give
		// it its first row from the UI at all.
		g := withColumnsNoRows(t)
		g.Focus()

		cmd, handled := g.startInsertRow()
		if !handled || cmd == nil {
			t.Fatalf("insert_row was refused on an empty table: handled=%t", handled)
		}
		if len(g.pendingRows) != 1 {
			t.Errorf("insert_row made %d pending rows, want 1", len(g.pendingRows))
		}
	})
}

// TestEveryColumnActionIsRefusedWithNoColumns is the other half, and it needs a DIFFERENT
// fixture: columns are the thing being asked about, so rows cannot supply them.
func TestEveryColumnActionIsRefusedWithNoColumns(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Grid) (tea.Cmd, bool)
	}{
		{"insert_row", func(g *Grid) (tea.Cmd, bool) { return g.startInsertRow() }},
		{"yank", func(g *Grid) (tea.Cmd, bool) { return g.startYank() }},
		{"export", func(g *Grid) (tea.Cmd, bool) { return g.startExport() }},
		{"delete_rows", func(g *Grid) (tea.Cmd, bool) { return g.startDelete() }},
		{"refresh_data", func(g *Grid) (tea.Cmd, bool) { return g.handleRefreshKey() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := withNothing(t)
			g.Focus()

			var cmd tea.Cmd
			var handled bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s panicked with no columns: %v", tc.name, r)
					}
				}()
				cmd, handled = tc.call(g)
			}()

			if handled {
				t.Errorf("%s claimed to have handled a grid with no columns", tc.name)
			}
			if cmd != nil {
				t.Errorf("%s issued a command with no columns", tc.name)
			}
			if len(g.pendingRows) != 0 {
				t.Errorf("%s made a pending row with no columns to fill it", tc.name)
			}
		})
	}
}

// SelectedRow is the read side of the same guard, and it has one more: a grid with TABS
// answers for the active one only, because the selection the sidebar follows is the
// selection of the tab on screen.
func TestSelectedRowAnswersOnlyForTheActiveTab(t *testing.T) {
	g := newGrid(t, 3, 3, 80, 20, 10)
	if g.SelectedRow() == nil {
		t.Fatal("the active tab has a row selected and SelectedRow says no")
	}

	g.activeTab = 1
	if got := g.SelectedRow(); got != nil {
		t.Errorf("SelectedRow answered %v for a tab that is not active, want nothing", got)
	}

	g.activeTab = 0
	if got := g.SelectedRow(); got == nil {
		t.Error("SelectedRow answered nothing for the active tab again")
	}

	t.Run("and nothing for an empty table", func(t *testing.T) {
		empty := withColumnsNoRows(t)
		if got := empty.SelectedRow(); got != nil {
			t.Errorf("SelectedRow answered %v for a table with no rows", got)
		}
	})
}

// The movement guards. Every one of these is arithmetic that indexes a slice, so the
// question is whether a grid with no rows — and a grid with a cursor pointed off the end —
// moves without a panic.
func TestMovementOnAnEmptyTableIsHarmless(t *testing.T) {
	for _, tc := range []struct {
		name string
		move func(*Grid)
	}{
		{"down", func(g *Grid) { g.moveDown() }},
		{"up", func(g *Grid) { g.moveUp() }},
		{"last", func(g *Grid) { g.moveToLast() }},
		{"first", func(g *Grid) { g.moveToFirst() }},
		{"half page down", func(g *Grid) { g.halfPageDown() }},
		{"half page up", func(g *Grid) { g.halfPageUp() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []int{0, 1} {
				g := newGrid(t, rows, 3, 80, 12, 10)
				if rows == 0 {
					g.SetData(&postgres.QueryResult{
						Columns: []postgres.ColumnInfo{{Name: "c0", DataType: "text"}},
					}, "public", "t")
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked with %d rows: %v", tc.name, rows, r)
						}
					}()
					tc.move(g)
				}()
				if g.cursorRow < 0 {
					t.Errorf("%s with %d rows left the cursor at %d", tc.name, rows, g.cursorRow)
				}
				// The grid still draws, which is the property that matters: a movement that
				// left it in a state where View panics is worse than the crash it avoided.
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s left the grid unrenderable: %v", tc.name, r)
						}
					}()
					_ = g.View()
				}()
			}
		})
	}

	t.Run("a cursor one row above the start lands on the first row", func(t *testing.T) {
		// The `cursorRow < 0` guard, and the case it is for. A cursor at -1 is what an
		// off-by-one in a decrement produces, and moving down from it must land on row 0
		// rather than on -1 plus one.
		g := newGrid(t, 5, 3, 80, 12, 10)
		g.cursorRow = -1
		g.scrollRow = -1

		g.moveDown()

		if g.cursorRow != 0 {
			t.Errorf("a cursor at -1 moved to %d, want the first row", g.cursorRow)
		}
	})

	t.Run("a cursor or window below zero still RENDERS", func(t *testing.T) {
		// What is asserted is not that the value is corrected — no movement function owns
		// it, and clamping it in each of them would be the drift pattern — but that the
		// grid draws.
		//
		// It used NOT to draw. `startRow := offset + g.scrollRow` went straight into the
		// row loop and `g.data.Rows[-1]` panicked the renderer: a negative scrollRow took
		// the whole screen down rather than producing a wrong row. The clamp now sits at
		// that boundary, which is the one place that knows a negative index is fatal.
		//
		// The first version of this case asserted scrollRow >= 0 after moveDown and failed
		// on correct code, because moveDown does not touch it — the value was never its to
		// correct.
		for _, bad := range []int{-1, -100} {
			g := newGrid(t, 5, 3, 80, 12, 10)
			g.cursorRow = bad
			g.scrollRow = bad

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("a grid with cursor and window at %d panicked on render: %v", bad, r)
					}
				}()
				if out := g.View(); !strings.Contains(out, "Grid") {
					t.Errorf("a grid with cursor and window at %d did not render:\n%s", bad, out)
				}
			}()
		}
	})
}

// The column filter swallows EVERY key it does not name — its switch has esc, enter and
// backspace, and a default that appends single characters and returns handled for
// everything else. That looks alarming on its own: a filter that eats ctrl+c means a user
// who opens "find column" and changes their mind cannot quit.
//
// It cannot happen, and the reason is a layer up. The app resolves EVERY bound key and
// dispatches it before handing anything to the focused pane, so the keys that reach
// handleFilterKey are the ones the app did not claim — and ctrl+c is bound to quit, which
// the app claims. The first version of this test asserted that ctrl+c reached the filter
// and reported that it was swallowed, reasoning from the grid's own switch alone. The
// grid cannot tell which keys the app already took, and does not need to.
//
// So the contract is the pair: the app claims the bound keys, and the filter keeps
// everything else. Both halves are asserted, because each alone is satisfied by the other
// being broken.
func TestTheColumnFilterKeepsWhatTheAppDidNotClaim(t *testing.T) {
	t.Run("quit is a bound key, so the filter never sees it", func(t *testing.T) {
		// The structural fact, from the only side the grid can see: ctrl+c resolves to an
		// action, and the app dispatches every resolved action before it hands anything to
		// the focused pane. So the keys that arrive here are the ones the app did not take.
		//
		// Asserted through the grid's own registry rather than in the app package because
		// the point is that the SAME registry the app reads resolves it — a second registry
		// here would prove nothing about the app's behaviour.
		g := newGrid(t, 10, 3, 80, 20, 10)
		action, ok := g.keybinds.Resolve("ctrl+c", config.ContextGrid)
		if !ok {
			t.Fatal("ctrl+c resolves to nothing, so there is no quit key to protect")
		}
		if action != "quit" {
			t.Errorf("ctrl+c resolves to %q, want the quit action", action)
		}
	})

	t.Run("and the filter keeps an UNBOUND key", func(t *testing.T) {
		// The other half: a key no action claims still belongs to the filter, because
		// swallowing it would make a character untypable in the one place a user types one.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		if _, handled := g.dispatchAction("find_column"); !handled {
			t.Fatal("find_column did not open the column filter")
		}

		if _, ok := g.keybinds.Resolve("f12", config.ContextGrid); ok {
			t.Skip("f12 is bound in this build, so it is not an unbound key")
		}
		before := len(g.filter)
		_, handled := g.handleKey(tea.KeyPressMsg{Code: tea.KeyF12})
		if !handled {
			t.Error("the column filter passed on an unbound key it had no business with")
		}
		if len(g.filter) != before {
			t.Errorf("an unbound key added %q to the filter", g.filter[before:])
		}
	})

	t.Run("a PRINTABLE key goes into the filter rather than moving the cursor", func(t *testing.T) {
		// The positive case, and the reason the default branch exists: `j` is bound to
		// navigate_down, and inside the filter it must type a j.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		if _, handled := g.dispatchAction("find_column"); !handled {
			t.Fatal("find_column did not open the column filter")
		}
		before := g.cursorRow

		_, handled := g.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
		if !handled {
			t.Fatal("a printable key was not handled by the filter")
		}
		if g.filter != "j" {
			t.Errorf("the filter holds %q, want %q", g.filter, "j")
		}
		if g.cursorRow != before {
			t.Errorf("typing j in the filter moved the cursor from %d to %d", before, g.cursorRow)
		}
	})

	t.Run("escape closes the filter and is NOT passed on", func(t *testing.T) {
		// The two halves of the switch. Escape is handled by the filter, so it does not
		// reach the grid's own escape handling — which would otherwise clear the row
		// selection as well.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		g.dispatchAction("find_column")
		g.toggleRowSelection()
		selected := len(g.SelectedRows())

		_, handled := g.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !handled {
			t.Error("escape did not reach the column filter")
		}
		if g.filtering {
			t.Error("escape left the column filter open")
		}
		if len(g.SelectedRows()) != selected {
			t.Errorf("escape also cleared the row selection (%d -> %d)", selected, len(g.SelectedRows()))
		}
	})
}
