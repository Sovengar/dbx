package grid

// Scenario: Las cuatro acciones del grid que funcionan SIN filas, y las tres guardas que
// deciden si una pulsacion llega al dispatch.
//
// dispatchAction tiene dos mitades separadas por un comentario: las acciones que
// funcionan "regardless of whether there are rows" y las que no. La frontera importa
//INSERT_ROW tiene que funcionar en una tabla VACIA — es la unica forma de darle la
// primera fila — y eso es justo el estado donde las otras cinco no pueden hacer nada.
//
// El otro bloque es el orden de las guardas en Update: foco, luego el export picker
// abierto, luego datos. Cada guarda devuelve "no manejado", y una reordenacion cambia
// cual de las tres se，食 knocks out la pulsacion.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// emptyTableGrid is a grid on an EMPTY table: columns loaded, zero rows. The state that
// makes the two halves of dispatchAction distinguishable.
func emptyTableGrid(t *testing.T) *Grid {
	t.Helper()
	g := newGrid(t, 0, 4, 80, 20, 10)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
			{Name: "email", DataType: "text"},
			{Name: "note", DataType: "text"},
		},
		Count: 0,
	}, "public", "empty_table")
	return g
}

func TestTheActionsThatWorkOnAnEmptyTable(t *testing.T) {
	t.Run("INSERT ROW is the one that has to", func(t *testing.T) {
		// The whole reason the two halves exist. An empty table with columns is the
		// normal state of a table nobody has written to yet, and inserting has to work
		// there. If this action were below the `len(Rows) == 0` gate there would be no
		// way to add the first row from the UI at all.
		g := emptyTableGrid(t)
		if g.hasRows() {
			t.Fatal("the fixture is not an empty table")
		}
		if !g.hasColumns() {
			t.Fatal("the fixture has no columns, which is a different state")
		}

		cmd, handled := g.HandleAction("insert_row")
		if !handled {
			t.Fatal("insert_row was refused on an empty table, so the first row could never be added")
		}
		if len(g.pendingRows) != 1 {
			t.Errorf("insert_row made %d pending rows, want 1", len(g.pendingRows))
		}
		// The pending row has to be as WIDE as the table, or writing into it indexes
		// past the end on the first keystroke.
		if len(g.pendingRows[0]) != len(g.columns) {
			t.Errorf("the pending row is %d cells wide, want the table's %d",
				len(g.pendingRows[0]), len(g.columns))
		}
		if cmd == nil {
			t.Error("insert_row issued no command")
		}
		// And it opens in the editor, positioned on the first cell, with the insert
		// flag set — otherwise the edit keypress goes to the grid's own key handler
		// and the character is eaten as navigation.
		if !g.editing {
			t.Error("insert_row did not open the cell editor")
		}
		if !g.inserting {
			t.Error("insert_row did not set the inserting flag, so the edit would write into an existing row")
		}
		if g.editCol != 0 {
			t.Errorf("the edit opened on column %d, want the first", g.editCol)
		}
	})

	t.Run("GO BACK works on an empty table", func(t *testing.T) {
		// Pure navigation: it emits a message and reads no state. If it were behind the
		// row gate, backing out of an empty table would be impossible — which is
		// exactly the moment a user presses escape.
		g := emptyTableGrid(t)
		cmd, handled := g.HandleAction("go_back")
		if !handled {
			t.Fatal("go_back was refused on an empty table")
		}
		if cmd == nil {
			t.Fatal("go_back issued no command")
		}
		if _, ok := cmd().(GridGoBackMsg); !ok {
			t.Errorf("go_back produced %T, want GridGoBackMsg", cmd())
		}
	})

	t.Run("SORT COLUMN works on an empty table", func(t *testing.T) {
		// Sorting zero rows is a no-op as a QUERY but it still has to record the sort
		// state, because the sort survives the reload that gives the table its rows.
		g := emptyTableGrid(t)
		cmd, handled := g.HandleAction("sort_column")
		if !handled {
			t.Fatal("sort_column was refused on an empty table, so the sort could not be set before loading")
		}
		_ = cmd
		// The sort is remembered, not applied: with no rows there is nothing to order,
		// but the state has to survive so the reload that brings the rows honours it.
		if g.header.SortColumn() == "" {
			t.Error("sort_column left no column sorted, so the sort would be lost on reload")
		}
	})

	t.Run("FILTER ROWS works on an empty table", func(t *testing.T) {
		// Filtering an empty table is how a user narrows a query before any rows come
		// back — and, more importantly, refusing it would leave them unable to open the
		// popup at all.
		g := emptyTableGrid(t)
		_, handled := g.HandleAction("filter_rows")
		if !handled {
			t.Fatal("filter_rows was refused on an empty table")
		}
		if g.whereFilter == nil {
			t.Fatal("filter_rows did not open the where-filter popup")
		}
		if !g.whereFilter.Visible() {
			t.Error("the where-filter popup is not visible")
		}
	})

	t.Run("FIND COLUMN works on an empty table", func(t *testing.T) {
		// Same reasoning as the filter: the columns are known even when no rows are.
		g := emptyTableGrid(t)
		_, handled := g.HandleAction("find_column")
		if !handled {
			t.Fatal("find_column was refused on an empty table, so the columns could not be searched")
		}
		// find_column reuses the column-filter mode rather than a separate widget: it
		// sets the same flag with an empty needle. Pinned because it is the reason the
		// two actions are not interchangeable — one starts with a filter already typed.
		if !g.filtering {
			t.Error("find_column did not open the column filter")
		}
		if g.filter != "" {
			t.Errorf("find_column started with the filter at %q, want it empty", g.filter)
		}
	})
}

func TestTheActionsThatNeedRowsAreRefusedWithoutThem(t *testing.T) {
	// The other half of dispatchAction. Each of these reads the row under the cursor or
	// the selection, so with no rows there is nothing to act on and returning
	// "not handled" lets the key fall through to whatever is next in line.
	// The real action IDs, taken from dispatchAction's own switch. Getting them wrong
	// is silent: an unknown ID falls through the switch and lands in the default
	// branch, which also returns not-handled — so a typo here looks exactly like a
	// correct refusal. The first version of this list used plausible names
	// (delete_row, yank_cell, export_data, next_row) and every case passed for the
	// wrong reason.
	for _, action := range []config.ActionID{
		"delete_rows",
		"edit_cell",
		"yank",
		"export",
		"select_row",
		"refresh_data",
		"navigate_fk",
		"commit_drafts",
		"discard_drafts",
		"undo_drafts",
		"navigate_down",
		"navigate_up",
		"go_last",
		"half_page_down",
	} {
		t.Run(string(action), func(t *testing.T) {
			g := emptyTableGrid(t)
			_, handled := g.HandleAction(action)
			if handled {
				t.Errorf("%s claims to have handled an empty table", action)
			}
		})
	}

	t.Run("the refusal is not a panic and does not corrupt the grid", func(t *testing.T) {
		g := emptyTableGrid(t)
		for _, action := range []config.ActionID{"delete_row", "edit_cell", "yank_cell", "copy_row"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s panicked on an empty table: %v", action, r)
					}
				}()
				_, _ = g.HandleAction(action)
			}()
		}
		// And the grid still renders — a refusal that left it in a state where View
		// panics would be worse than the original crash.
		//
		// Asserted as "it renders something with the right shape" rather than on the
		// column headers: at this width the header row is squeezed and only the first
		// column is legible, so pinning the header text would be pinning the column
		// layout instead. The first version of this case asserted all four names and
		// reported a mismatch on a correct render.
		out := ansi.Strip(g.View())
		if !strings.Contains(out, "id") {
			t.Errorf("the empty table does not render its header row:\n%s", out)
		}
		if len(strings.Split(out, "\n")) < 5 {
			t.Errorf("the empty table rendered %d lines, which is not a grid",
				len(strings.Split(out, "\n")))
		}
	})
}

func TestUpdateRefusesInOrder(t *testing.T) {
	t.Run("an UNFOCUSED grid drops everything", func(t *testing.T) {
		// The outermost guard. Mouse handlers bypass it through HandleAction — that is
		// documented on HandleAction and is why the two entry points disagree on
		// purpose. Pinning it so a future reordering is a decision.
		g := newGrid(t, 5, 3, 80, 20, 10)
		g.Blur()

		cmd, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if handled {
			t.Error("an unfocused grid handled a key")
		}
		if cmd != nil {
			t.Errorf("an unfocused grid produced %T, want no command", cmd())
		}
		if g.cursorRow != 0 {
			t.Errorf("an unfocused grid moved the cursor to row %d", g.cursorRow)
		}

		// And the SAME action through the mouse path does reach it. That asymmetry is
		// the design, so it gets asserted rather than left as a comment.
		if _, handled := g.HandleAction("navigate_down"); !handled {
			t.Error("HandleAction refuses an unfocused grid, which is what lets the mouse drive it")
		}
	})

	t.Run("an open export picker SWALLOWS the key before anything else", func(t *testing.T) {
		// Second guard, and the one whose position matters. The picker is a modal over
		// the grid: while it is up, esc and enter belong to the picker. If the data guard
		// ran first the picker would be unreachable on a grid with no data, and if the
		// grid's own handler ran first, q would close the export instead of the picker.
		g := newGrid(t, 5, 3, 80, 20, 10)
		g.exportPicker.Show("public", "t", nil, nil, g.columns)

		// Down belongs to the PICKER: it moves the option cursor, and the grid's row
		// cursor must not move. If the guards were in the other order, down would scroll
		// the table behind a modal the user cannot see through.
		if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyDown}); !handled {
			t.Error("down did not reach the open export picker")
		}
		if g.cursorRow != 0 {
			t.Errorf("the grid cursor moved to row %d while the export picker was open", g.cursorRow)
		}
		if g.exportPicker.cursor != 1 {
			t.Errorf("the picker's cursor is at option %d, want the second", g.exportPicker.cursor)
		}

		// Escape closes the picker and produces no command — it is a dismissal, not an
		// export. The observable that matters is that it stops being visible.
		_, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !handled {
			t.Error("escape did not reach the open export picker")
		}
		if g.exportPicker.IsVisible() {
			t.Error("escape left the export picker open")
		}

		// And once closed, the grid takes keys again. Without this the picker would
		// swallow everything for the rest of the session.
		if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyDown}); !handled {
			t.Error("the grid did not take keys back after the picker closed")
		}
	})

	t.Run("a grid with NO data drops every key", func(t *testing.T) {
		// Third guard. Distinct from the other two: here the grid is focused and nothing
		// modal is up, but there is no result set — the tab is a schema node that was
		// never queried. Keys must fall through to the app router, not be swallowed.
		g := newGrid(t, 0, 0, 80, 20, 10)
		g.data = nil

		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyDown},
			{Code: 'e', Text: "e"},
			{Code: tea.KeyEnter},
		} {
			if _, handled := g.Update(key); handled {
				t.Errorf("%s was handled by a grid with no data", key)
			}
		}
	})

	t.Run("a key the picker does NOT know is SWALLOWED, not passed to the grid", func(t *testing.T) {
		// The `return nil, false` after the picker's own handler. The picker is modal: it
		// says "I do not handle this" and the grid must NOT get a second chance at it,
		// or q would both close the picker (as its own esc/q case) and move the table.
		// So this is a real decision rather than a fallthrough — and it is the line a
		// "just remove the redundant return" cleanup would silently delete.
		g := newGrid(t, 5, 3, 80, 20, 10)
		g.exportPicker.Show("public", "t", nil, nil, g.columns)

		// "x" is in neither the grid's dispatch nor the picker's switch.
		_, handled := g.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		if handled {
			t.Error("an unrecognised key reached the grid while the picker was open")
		}
		if !g.exportPicker.IsVisible() {
			t.Error("an unrecognised key closed the picker")
		}
	})

	t.Run("a non-key message is not handled either", func(t *testing.T) {
		// The type switch has one case. A tick, a resize or a message the grid does not
		// know about reaches it and has to come back unhandled, or the app's router
		// would never see its own messages.
		g := newGrid(t, 5, 3, 80, 20, 10)
		for _, msg := range []tea.Msg{
			tea.WindowSizeMsg{Width: 100, Height: 30},
			tea.MouseWheelMsg{},
			struct{ X, Y int }{5, 5},
		} {
			if _, handled := g.Update(msg); handled {
				t.Errorf("%T was handled by the grid", msg)
			}
		}
	})
}

func TestHandleActionIsRefusedWhileATextEntryIsOpen(t *testing.T) {
	// The guard between the nil check and the dispatch. While the cell editor, the
	// column filter or the where-filter owns the keyboard, an action from the MOUSE must
	// not also run — otherwise clicking a row while editing a cell would both move the
	// cell editor and start a delete.
	for _, tc := range []struct {
		name string
		arm  func(*Grid)
	}{
		{"the cell editor is open", func(g *Grid) { g.editing = true }},
		{"the column filter is open", func(g *Grid) { g.filtering = true }},
		{"the where-filter popup is open", func(g *Grid) {
			g.startWhereFilter()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrid(t, 5, 3, 80, 20, 10)
			tc.arm(g)

			_, handled := g.HandleAction("delete_row")
			if handled {
				t.Error("an action reached the dispatch while a text entry was open")
			}
		})
	}

	t.Run("and a grid with no data is refused before that check", func(t *testing.T) {
		// The ORDER of the two guards. With no data AND an editor open, both would
		// refuse; the observable that separates them is that the nil check comes first
		// and cannot itself dereference anything.
		g := newGrid(t, 5, 3, 80, 20, 10)
		g.data = nil
		g.editing = true

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("HandleAction panicked with no data and an open editor: %v", r)
				}
			}()
			if _, handled := g.HandleAction("delete_row"); handled {
				t.Error("delete_row was handled with no data")
			}
		}()
	})
}
