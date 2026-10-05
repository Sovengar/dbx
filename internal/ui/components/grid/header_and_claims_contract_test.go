package grid

// Scenario: La cabecera, el picker de exportacion, y la lista de acciones.
//
// Three small things the grid's own suite never touched, and each has a shape worth writing
// down:
//
//   - ToggleSort's SortNone arm. It is reached by pressing the SAME column twice, and the two
//     calls come from two different places — a keypress and a click — so a test that only ever
//     presses one key, or only ever clicks once, never sees the cycle start. The cycle is
//     three states and a fourth press leaves the column unsorted, which is the part a user
//     would notice and a test that stops at two presses would miss.
//   - ExportPicker.Update's fallthrough. It is an overlay, so it must DECLINE messages it does
//     not understand; an overlay that claims everything freezes the grid behind it.
//   - HandledActions, which the app's coverage test reads but this package never calls — so a
//     name that stopped being dispatched would still be claimed.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// The full cycle on one column: none, ascending, descending, none — and off.
//
// The premise is that the header starts UNSORTED, because the cycle's first arm is only
// reachable from SortNone, and a header that arrived sorted would skip straight past it.
func TestTheSortCycleIsFourPressesLong(t *testing.T) {
	h := NewHeader(theme.Resolve("dark").Styles())

	if h.sortDir != SortNone {
		t.Fatalf("a fresh header starts at %v, want SortNone", h.sortDir)
	}

	for _, want := range []SortDirection{SortAsc, SortDesc, SortNone} {
		got := h.ToggleSort(3)

		if got != want {
			t.Fatalf("press %d gave %v, want %v", pressCount(want), got, want)
		}
		if h.sortDir != want {
			t.Errorf("after the press the header is at %v, want %v", h.sortDir, want)
		}
	}

	// And the column is released, so a press on it starts the cycle again rather than
	// continuing one the user has already finished.
	if h.sortCol != -1 {
		t.Errorf("the sorted column is still %d, want -1 — a finished sort releases it", h.sortCol)
	}
	if got := h.ToggleSort(3); got != SortAsc {
		t.Errorf("the next press on the released column gave %v, want a fresh SortAsc", got)
	}

	t.Run("and a DIFFERENT column starts its own cycle", func(t *testing.T) {
		// The counterweight: a ToggleSort that always cycled the same column would satisfy
		// the case above.
		other := NewHeader(theme.Resolve("dark").Styles())

		if got := other.ToggleSort(7); got != SortAsc {
			t.Errorf("the first press on column 7 gave %v, want SortAsc", got)
		}
		if other.sortCol != 7 {
			t.Errorf("the sorted column is %d, want 7", other.sortCol)
		}

		// And pressing the ORIGINAL column again is a first press on it, not a third.
		if got := other.ToggleSort(3); got != SortAsc {
			t.Errorf("a first press on column 3 gave %v, want SortAsc", got)
		}
		if other.sortCol != 3 {
			t.Errorf("the sorted column is %d, want 3", other.sortCol)
		}
	})

	t.Run("and a column ALREADY SELECTED but unsorted starts the cycle, not a new one", func(t *testing.T) {
		// This is the arm the main cycle cannot reach, and the reason it exists.
		//
		// The outer `if h.sortCol == col` is what distinguishes a first press from a repeat,
		// and the inner switch's SortNone arm is the first step of a REPEAT. So the arm needs
		// a header whose column is already selected and unsorted at the same time — which is
		// not a state a user reaches by pressing, because the cycle never passes through it.
		//
		// Which means it is reachable from the load path instead: a header restored with a
		// column remembered but no direction. My first version of this case asserted the press
		// would change NOTHING, reading the switch as having no SortNone arm; it has one, and
		// it is the line this whole subtest exists for.
		inconsistent := NewHeader(theme.Resolve("dark").Styles())
		inconsistent.sortCol = 2
		inconsistent.sortDir = SortNone

		if got := inconsistent.ToggleSort(2); got != SortAsc {
			t.Errorf("the press gave %v, want SortAsc — a selected-but-unsorted column begins the cycle", got)
		}
		if inconsistent.sortCol != 2 {
			t.Errorf("the sorted column moved to %d, want 2 — a repeat does not reselect", inconsistent.sortCol)
		}
	})
}

func pressCount(want SortDirection) int {
	switch want {
	case SortAsc:
		return 1
	case SortDesc:
		return 2
	case SortNone:
		return 3
	}
	return 0
}

// The overlay's fallthrough. ExportPicker is shown on top of the grid, and the app routes
// every message through its overlays in order — so an overlay that claimed a message it does
// not understand would swallow the grid's own input while the picker was open.
func TestTheExportPickerDeclinesWhatItDoesNotUnderstand(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"a window resize", tea.WindowSizeMsg{Width: 100, Height: 30}},
		{"a mouse click", tea.MouseClickMsg{X: 2, Y: 2}},
		{"a message that is nothing in particular", struct{ tea.Msg }{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ep := visibleExportPicker()

			cmd, handled := ep.Update(tc.msg)

			if handled {
				t.Errorf("the picker claimed %T", tc.msg)
			}
			if cmd != nil {
				t.Errorf("the picker produced %T for a message it does not understand", cmd())
			}
			if !ep.IsVisible() {
				t.Error("the picker closed over a message it does not understand")
			}
		})
	}

	t.Run("a hidden picker claims nothing at all", func(t *testing.T) {
		// The other half, and the one that matters in practice: a closed picker must be
		// invisible to the router, or every keystroke is offered to a panel nobody is looking at.
		ep := newExportPickerForTest()

		if cmd, handled := ep.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled || cmd != nil {
			t.Error("a hidden picker claimed a key")
		}
	})

	t.Run("a visible picker DOES claim a key", func(t *testing.T) {
		// The counterweight: an Update that always declined would satisfy both cases above and
		// the picker would be unusable.
		ep := visibleExportPicker()

		if _, handled := ep.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); !handled {
			t.Error("a visible picker declined j")
		}
	})
}

// HandledActions is what the app's coverage test checks the grid against, and nothing in this
// package called it. So the list is asserted here, exactly — a count and a direction per row,
// because "the app claimed it" is only true if the grid both lists it and dispatches it.
func TestTheHandledActionsAreExactlyTheOnesTheGridSwitchesOn(t *testing.T) {
	// A grid WITH ROWS. dispatchAction refuses every action when the data is empty, so a fresh
	// grid declines all thirty-two of them and "the grid claims it" would be tested against a
	// grid that handles nothing — which is a property of the fixture, not of the claim.
	g := newGrid(t, 20, 4, 100, 30, 100)

	claimed := map[config.ActionID]bool{}
	for _, id := range g.HandledActions() {
		if claimed[id] {
			t.Errorf("the grid claims %q twice", id)
		}
		claimed[id] = true
	}

	// The actions the app intercepts BEFORE handing the key to the grid are deliberately not
	// here, and the comment above the function says so. Asserting their absence is what stops
	// a future edit from "helpfully" adding them and claiming coverage for the app's path.
	for _, notGrid := range []config.ActionID{
		"export", "refresh_data", "undo_drafts", "commit_drafts",
	} {
		if claimed[notGrid] {
			t.Errorf("the grid claims %q, which the app intercepts before the grid sees the key", notGrid)
		}
	}

	// And the exact count, which is the assertion that would catch an action added to the
	// registry and to this list without anyone deciding whether the grid really handles it.
	//
	// Thirty-two, arrived at by adding the list up: 4 navigation, 4 page jumps, 4 page steps,
	// 9 goto-page, 5 row/cell edits, 3 column operations, 3 draft/FK/back actions. The first
	// version of this case said 29 — it dropped the last group — and the code was right.
	const wantActions = 32
	if got := len(g.HandledActions()); got != wantActions {
		t.Errorf("the grid claims %d actions, want %d", got, wantActions)
	}

	t.Run("the navigation group is dispatched from a plain loaded grid", func(t *testing.T) {
		// What IS unconditional: every action that only needs a grid with rows and a cursor.
		// These are the twenty-four the grid can do at any time, and the assertion is that all
		// of them work with nothing else arranged.
		//
		// The other ten are NOT unconditional, and claiming otherwise would be the wrong test —
		// they need a mode open, a draft, a clipboard, or a foreign key under the cursor, and
		// they are named below.
		for _, id := range g.HandledActions() {
			switch id {
			case "delete_rows", "insert_row", "yank", "select_row",
				"sort_column", "filter_rows", "find_column", "discard_drafts",
				"navigate_fk", "go_back":
				continue
			}
			if _, handled := g.HandleAction(id); !handled {
				t.Errorf("the grid claims %q but does not handle it from a plain loaded grid", id)
			}
		}
	})

	t.Run("and the ten that need state are claimed but not dispatchable yet", func(t *testing.T) {
		// The distinction, stated rather than asserted as a bug: claiming is unconditional and
		// dispatching is conditional, because these eight produce a MESSAGE or write to the
		// clipboard and need something to act on.
		//
		// A test that demanded all thirty-two be dispatched from one fixture would be asking
		// for a fixture that opens seven modes at once, and it would report the state machine
		// as broken. The state machine's own tests are elsewhere; this one records WHICH
		// actions are the conditional half, so a new action added to the list has to say which
		// side it is on.
		conditional := map[config.ActionID]string{
			"delete_rows":    "needs rows marked for deletion",
			"insert_row":     "needs a row to insert after",
			"yank":           "needs a clipboard and a selection",
			"select_row":     "needs a draft to select",
			"sort_column":    "needs the column the sort key chose",
			"filter_rows":    "needs filter mode open",
			"find_column":    "needs find mode open",
			"discard_drafts": "needs drafts to discard",
			// The two whose guards are downstream of dispatchAction's own emptiness check: a
			// grid with rows and a cursor is not enough, because navigate_fk also needs the
			// cursor's column to BE a foreign key, and go_back needs navigation history.
			"navigate_fk": "needs the cursor's column to be a foreign key",
			"go_back":     "needs navigation history to go back through",
		}

		claimed := map[config.ActionID]bool{}
		for _, id := range g.HandledActions() {
			claimed[id] = true
		}

		for id, why := range conditional {
			if !claimed[id] {
				t.Errorf("%q needs %s but the grid does not claim it, so the app would never route it", id, why)
			}
			if _, handled := g.HandleAction(id); handled {
				t.Errorf("%q was dispatched from a plain grid, but it needs %s", id, why)
			}
		}
		// And the count is the sum of the two groups, so an action added to the list has to be
		// accounted for by one of them.
		claimedCount := len(g.HandledActions())
		unconditional := claimedCount - len(conditional)
		if unconditional != 22 {
			t.Errorf("the grid claims %d actions: %d conditional and %d unconditional, want 10 and 22",
				claimedCount, len(conditional), unconditional)
		}
	})

}

// visibleExportPicker is an open picker over one row, which is the state its key switch is
// written for — the descriptions and the visible list both need something to describe.
func visibleExportPicker() *ExportPicker {
	ep := newExportPickerForTest()
	ep.Show("public", "orders",
		[]interface{}{1, "a@example.com"},
		[][]interface{}{{1, "a@example.com"}},
		[]string{"id", "email"},
	)
	return ep
}

// The invariant that replaced startDelete's `row == nil` guard.
//
// The guard existed because SelectedRow() can return nil, and it could not fire: SelectedRow
// returns nil only when there are no rows at all, and dispatchAction refuses every action when
// len(g.data.Rows) == 0. So by the time startDelete runs there is a row under the cursor.
//
// A guard removed on the strength of an argument is worth nothing on its own, so the argument
// IS this test: walk the cursor to both ends of a paginated grid and delete at every stop. If
// the cursor could ever sit outside the result set, one of those stops has no row and the copy
// below panics — which is a louder failure than the guard was a safer one.
//
// The pagination matters and is why the fixture is not three rows: with one page the cursor can
// never leave the first screen, so the interesting case is the boundary where the cursor moves
// off the end of a page and the SCROLL offset has to move with it.
func TestDeletingEveryRowWorks(t *testing.T) {
	// Seven rows on a page of three: two full pages and a part, which is the smallest shape
	// where scrollRow has to move.
	g := newGrid(t, 7, 3, 100, 20, 3)
	g.Focus()

	// Every cursor position the app can produce: down until the last row, up until the first,
	// a page down and back, and home/last.
	for _, action := range []config.ActionID{
		"navigate_down", "navigate_down", "navigate_down", "navigate_down",
		"navigate_down", "navigate_down", "navigate_down",
		"next_page", "next_page", "navigate_down", "prev_page", "prev_page",
		"go_last", "go_first", "half_page_down", "half_page_up",
	} {
		if _, handled := g.HandleAction(action); !handled {
			t.Fatalf("%s was refused on a grid with rows", action)
		}
		if g.hasRows() && g.SelectedRow() == nil {
			t.Fatalf("after %s the cursor is at row %d (scroll %d) with %d rows and no row under it",
				action, g.cursorRow, g.scrollRow, g.TotalRows())
		}
	}

	// And a deletion at the position the walk ended on, which is the operation whose guard was
	// removed.
	if _, handled := g.HandleAction("delete_rows"); !handled {
		t.Fatal("delete_rows was refused on a grid with rows")
	}
	if !g.HasDrafts() {
		t.Error("the deletion did not become a draft")
	}
	if g.DraftCount() == 0 {
		t.Error("the draft count is zero after a deletion")
	}
}

// fkAtColumn, and the refusal that replaced navigateFK's dead nil check.
//
// navigateFK used to ask the icon map whether the cursor was on a key and then walk the
// foreign keys again. The icon map is BUILT from the foreign keys — in SetMetadata, in the
// same function — so the two lookups were one fact stated twice, and the second copy's nil guard
// could not fire. The single lookup makes the refusal live: the cursor is on a column that is
// not a key, which is an ordinary thing for a user to do.
func TestNavigatingAForeignKeyNeedsTheCursorOnAKey(t *testing.T) {
	g := newGrid(t, 4, 3, 100, 20, 10)
	g.Focus()
	g.SetMetadata(nil, []postgres.ForeignKeyInfo{{
		Name: "c0_c2_fkey", Column: "c0", RefTable: "other", RefColumn: "id",
	}}, nil)

	// The lookup itself, by column index.
	if fk := g.fkAtColumn(0); fk == nil {
		t.Fatal("the foreign key on column 0 was not found")
	} else if fk.RefTable != "other" {
		t.Errorf("the lookup returned the key to %q", fk.RefTable)
	}
	// And the three ways it says no: a column that is not a key, an index past the end, and a
	// negative one — which is what a cleared cursor field would be.
	for _, idx := range []int{1, 2, 3, 99, -1} {
		if fk := g.fkAtColumn(idx); fk != nil {
			t.Errorf("fkAtColumn(%d) returned %+v, want nothing — that column is not a key", idx, fk)
		}
	}

	// Through the action, which is the user-facing half.
	// The cursor starts on column 0, which IS a key, so it navigates — and that is the
	// counterweight for the refusal below.
	if _, handled := g.HandleAction("navigate_fk"); !handled {
		t.Error("the cursor is on column 0, which is a key, and it was refused anyway")
	}

	g.cursorCol = 1
	if _, handled := g.HandleAction("navigate_fk"); handled {
		t.Error("the cursor is on column 1, which is not a key, and it was navigated anyway")
	}
}
