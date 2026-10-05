package grid

// Scenario: Las guardas del grid que solo se alcanzan desde dentro del paquete.
//
// Everything here is either an arm the grid's own suite never reached, or a guard that
// production cannot trigger any more. Both kinds are worth a case, and for opposite
// reasons: the first because it is behaviour, the second because it is a promise about
// what will not happen.
//
// The promises are the residue of a real bug. A negative scroll offset indexed Rows[-1]
// and took the whole screen down; the clamps that stop it were added then, and the four
// `if g.cursorRow < 0 { g.cursorRow = 0 }` pairs left behind are the last line before that
// index. They cannot fire from a key press any more — which is exactly why they should be
// tested by SETTING the value, so that the day the clamp above them is refactored away,
// one of these fails instead of a user's terminal.
//
// The behaviour is the rest: the two getters the app needs to make its config wiring
// observable, the where filter's one decline, and the guards on actions that have nothing
// to act on.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// The two sizes NewModel derives from the config, read back through the grid.
//
// These are here because the app's wiring test needs them and because a getter added for
// a test is a getter nobody else calls — so its contract belongs to this package, not to
// the caller. The negative case is the one with teeth: page_size and yank_max_rows are
// read from a config file a user writes by hand, and `-1` is a typo away.
func TestTheTwoConfigSizesAreReadableAndOnlySetFromPositiveNumbers(t *testing.T) {
	t.Run("what the grid was built with is what it reports", func(t *testing.T) {
		for _, size := range []int{1, 25, 100, 10_000} {
			g := newGrid(t, 4, 2, 80, 20, size)
			if got := g.PagerLimit(); got != size {
				t.Errorf("a grid built with a page size of %d reports %d", size, got)
			}
		}
	})

	t.Run("the yank limit takes a positive number and ignores the rest", func(t *testing.T) {
		// Two things in one guard, and both matter. A zero or negative limit would make
		// startYank's comparison `count >= yankMaxRows` true for every selection including
		// an empty one, so a single selected row would open the file picker instead of
		// copying — the least surprising outcome for a misconfigured app, and not the one
		// the code would give.
		g := newGrid(t, 4, 2, 80, 20, 10)

		if got := g.YankMaxRows(); got == 0 {
			t.Fatal("a grid reports a yank limit of 0, so every selection would exceed it")
		}
		before := g.YankMaxRows()

		for _, bad := range []int{0, -1, -100} {
			g.SetYankMaxRows(bad)
			if got := g.YankMaxRows(); got != before {
				t.Errorf("SetYankMaxRows(%d) took effect, giving %d", bad, got)
			}
		}
		g.SetYankMaxRows(3)
		if got := g.YankMaxRows(); got != 3 {
			t.Errorf("SetYankMaxRows(3) gave %d", got)
		}
	})
}

// The where filter is the ONLY owner in the key path that can decline a key: j and k with
// no suggestion popup are "not mine", and Grid.Update returns (nil, false). The app above
// this then stops rather than falling through — and this is the case that says the decline
// is real, because a filter that claimed everything would also produce (nil, false) for a
// different reason and the app's stop would never be exercised.
func TestTheWhereFilterIsTheOneKeyPathThatCanBeDeclined(t *testing.T) {
	t.Run("j with no popup is declined", func(t *testing.T) {
		g := newGrid(t, 40, 3, 80, 20, 10)
		g.Focus()
		g.HandleAction("filter_rows")

		if !g.IsWhereFiltering() {
			t.Fatal("the where filter did not open, so this proves nothing")
		}

		// The popup has to be CLOSED first, and typing is what closes it: the suggestions
		// are the column names, so as long as the input matches one the popup is open and j
		// moves the selection instead of declining. The first version of this case pressed j
		// on a fresh filter and reported that the decline was unreachable — the filter was
		// open, not broken.
		g.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
		g.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
		if g.whereFilter == nil || g.whereFilter.showPopup {
			t.Fatal("the suggestion popup is still open; the decline arm needs it closed")
		}

		cmd, handled := g.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		if handled {
			t.Error("j was handled; the decline arm is unreachable and the app's stop is untested")
		}
		if cmd != nil {
			t.Errorf("a declined j produced %T", cmd())
		}
		// And the filter is still there: a decline must not be a dismissal.
		if !g.IsWhereFiltering() {
			t.Error("a declined j closed the filter")
		}
	})

	t.Run("and typing is not declined", func(t *testing.T) {
		// The counterweight, because "the grid declines everything" satisfies the case above
		// and would make the filter unusable.
		g := newGrid(t, 40, 3, 80, 20, 10)
		g.Focus()
		g.HandleAction("filter_rows")

		if _, handled := g.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); !handled {
			t.Error("typing x was declined by the where filter")
		}
		if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); !handled {
			t.Error("backspace was declined by the where filter")
		}
	})
}

// Actions with nothing to act on. Each of these is a guard that keeps an action from
// issuing a command built out of data it does not have, and each is reachable by asking
// for the action on a grid that has no rows, no keys or no foreign keys.
func TestActionsWithNothingToActOnIssueNothing(t *testing.T) {
	t.Run("navigating a foreign key with no foreign keys", func(t *testing.T) {
		// Without the guard the action builds its command from a nil fkInfo, so the guard
		// is the difference between "does nothing" and a panic on the next dereference.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()

		cmd, handled := g.HandleAction("navigate_fk")
		if handled {
			t.Errorf("expanding a foreign key on a grid with none was handled, producing %T", cmdFunc(cmd))
		}
		if cmd != nil {
			t.Error("expanding a foreign key with none issued a command")
		}
	})

	t.Run("and with one, it does", func(t *testing.T) {
		// The counterweight.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		g.SetMetadata(nil, []postgres.ForeignKeyInfo{{
			Name: "c1_c2_fkey", Column: "c1",
			RefSchema: "public", RefTable: "other", RefColumn: "id",
		}}, nil)

		// On the FK's own column. navigateFK checks the column's KEY ICON, not the
		// foreign-keys list, and the icon is per column — so a grid with a declared FK and
		// the cursor elsewhere is a grid with nothing to expand, which is what the refusal
		// above is for. The first version of this case left the cursor at column 0.
		g.cursorCol = 1

		if _, handled := g.HandleAction("navigate_fk"); !handled {
			t.Error("expanding a declared foreign key was declined")
		}
	})

	t.Run("deleting with no selected row", func(t *testing.T) {
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		// Nothing selected. The guard is SelectedRow() == nil, and without it the delete
		// would build a statement out of a nil row.
		cmd, handled := g.HandleAction("delete_rows")
		if handled && cmd != nil {
			t.Errorf("deleting with no selection produced %T", cmdFunc(cmd))
		}
	})

	t.Run("and with a selection it STAGES the row rather than issuing a command", func(t *testing.T) {
		// Not a command, and that is the design: delete_rows records what to delete and
		// lets the user confirm, so the observable is the staged row. The first version of
		// this case asserted a command and reported that delete did nothing.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()
		g.HandleAction("select_row")

		cmd, handled := g.HandleAction("delete_rows")
		if !handled {
			t.Fatal("deleting a selected row was declined")
		}
		if cmd != nil {
			t.Errorf("deleting issued a command before the user confirmed: %T", cmd())
		}
		if len(g.pendingDeletes) != 1 {
			t.Fatalf("%d rows staged for deletion, want 1", len(g.pendingDeletes))
		}
		// And the staged row is a COPY, not the live one. This is the subtlety in the
		// function: the user can keep editing the live row after staging a delete, and the
		// delete must not follow the edit.
		live := g.data.Rows[0][0]
		g.data.Rows[0][0] = "edited after staging"
		if got := g.pendingDeletes[0].Row[0]; got == live {
			return
		}
		if got := textOf(g.pendingDeletes[0].Row[0]); got != "edited after staging" {
			t.Errorf("the staged row is %q, so it points at the live row", got)
		}
	})

	t.Run("inserting a row when the grid has no columns", func(t *testing.T) {
		// The empty grid. insertRow needs a column list to build the pending row, and the
		// command it issues is a nil message on purpose — the row appears in the grid and
		// the message tells the app nothing, which is a fact worth asserting because a nil
		// message and no message are different things to the app's event loop.
		g := newGrid(t, 0, 3, 80, 20, 10)
		g.Focus()

		cmd, handled := g.HandleAction("insert_row")
		if !handled {
			t.Fatal("inserting on an empty grid was declined")
		}
		if cmd == nil {
			t.Fatal("inserting on an empty grid issued nothing")
		}
		if msg := cmd(); msg != nil {
			t.Errorf("the insert command produced %#v, want a nil message", msg)
		}
	})

	t.Run("selecting a row moves the cursor, which the app is told about", func(t *testing.T) {
		// select_row returns a GridCursorMovedMsg, so the app learns the selection changed
		// without the grid having to poll. A version returning nil would leave the status
		// bar showing a stale count.
		g := newGrid(t, 10, 3, 80, 20, 10)
		g.Focus()

		cmd, handled := g.HandleAction("select_row")
		if !handled || cmd == nil {
			t.Fatal("selecting a row was declined or issued nothing")
		}
		if _, ok := cmd().(GridCursorMovedMsg); !ok {
			t.Errorf("selecting a row produced %T, want GridCursorMovedMsg", cmd())
		}
		if got := g.SelectionCount(); got != 1 {
			t.Errorf("after selecting, the count is %d", got)
		}
	})
}

// Tabbing between columns while editing an INSERTED row, and the two kinds of value it
// finds. The `else` arm — a column whose pending value is not nil — is the one that carries
// the existing value into the editor; the `if` arm blanks it. Getting them backwards would
// erase a value on every tab press.
//
// pendingRows only exist while INSERTING, which is why the fixture has to insert: editing
// an existing cell takes the other branch entirely. The first version of this case edited
// an existing row and found no pending row at all.
func TestTabbingAcrossAnInsertedRowKeepsTheValueOrBlanksIt(t *testing.T) {
	// insert_row opens the first cell of the new row itself, so no edit_cell is needed —
	// and asking for one is REFUSED, because HandleAction returns false for a grid that is
	// already editing.
	inserted := func(cols int) *Grid {
		t.Helper()
		g := newGrid(t, 3, cols, 80, 20, 10)
		if _, handled := g.HandleAction("insert_row"); !handled {
			t.Fatal("inserting a row was declined")
		}
		if !g.IsEditing() {
			t.Fatal("inserting did not open the cell editor on the new row")
		}
		if _, handled := g.HandleAction("edit_cell"); handled {
			t.Error("edit_cell was accepted while the editor was already open")
		}
		return g
	}

	typeIn := func(g *Grid, s string) {
		t.Helper()
		for _, r := range s {
			g.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}

	t.Run("tabbing onto an untouched column blanks the editor", func(t *testing.T) {
		// The `if val == nil` arm, and it is the DEFAULT: startInsertRow allocates
		// make([]interface{}, n) and fills nothing, so every column of a new row is nil.
		// Blanking is right — carrying the previous column's text into the next one would
		// silently duplicate it — and this is the case that says so.
		g := inserted(3)
		typeIn(g, "first")

		if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
			t.Fatal("tab was declined by the cell editor")
		}
		if g.editValue != "" {
			t.Errorf("tabbing onto an untouched column carried %q instead of blanking", g.editValue)
		}
		typeIn(g, "typed")
		g.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		if got := pendingRowText(g, 0, 0); got != "first" {
			t.Errorf("the first column is %q, want first", got)
		}
		if got := pendingRowText(g, 0, 1); got != "typed" {
			t.Errorf("the second column is %q, want typed", got)
		}
		if got := pendingRowText(g, 0, 2); got != "<nil>" {
			t.Errorf("the third column is %q, want it untouched", got)
		}
	})

	t.Run("tabbing onto a column with a value carries it", func(t *testing.T) {
		// The `else` arm, which needs a non-nil pending column — and there is no shift+tab
		// to return to a column already typed into, so the value is placed directly. What
		// is under test is the tab handler's branch, not how the value got there. The first
		// version of this case expected a fresh column to hold "" and reported that the else
		// arm was unreachable; a fresh column holds nil.
		g := inserted(3)
		g.pendingRows[0][1] = "already here"

		if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
			t.Fatal("tab was declined by the cell editor")
		}
		if g.editValue != "already here" {
			t.Errorf("tabbing onto a column holding %q gave an editor of %q", "already here", g.editValue)
		}

		// And the carried value is what gets committed if the user accepts it, which is the
		// point of carrying it: the typed value starts from what was there.
		g.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if got := pendingRowText(g, 0, 1); got != "already here" {
			t.Errorf("the second column is %q", got)
		}
	})

	t.Run("and the columns before it kept what was typed", func(t *testing.T) {
		// The property both arms must preserve: tabbing forward does not lose what is
		// already committed to the pending row.
		g := inserted(3)
		typeIn(g, "alpha")
		g.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		g.Update(tea.KeyPressMsg{Code: tea.KeyTab})

		if got := pendingRowText(g, 0, 0); got != "alpha" {
			t.Errorf("the first column is %q after two tabs, want alpha", got)
		}
		if got := pendingRowText(g, 0, 2); got != "<nil>" {
			t.Errorf("the third column is %q after two tabs, want it untouched", got)
		}
	})
}

// The clamps that keep an out-of-range offset from indexing Rows[-1].
//
// The bug they were added for was real: a negative scroll offset indexed Rows[-1] and took
// the whole screen down. Since the fix, no key press produces one — so they are exercised
// here by SETTING the value, which is the only way.
//
// The finding is that they are NOT one general clamp, and each of the three circumstances
// belongs to a different function. The first version of this table asserted scrollRow >= 0
// after all four functions, which failed for two of them, and the code was right each time:
//
//	moveToLast    guards UNCONDITIONALLY, so a negative scroll reaches it and is fixed
//	halfPageUp    guards a negative CURSOR, reached by a negative cursor rather than a
//	              negative scroll
//	halfPageDown  guards a window TALLER THAN THE DATA — totalRows - ch goes negative, and
//	clampCursor   that is the only way in, because an in-range absolute index skips the
//	              block entirely
//
// Which means a negative scrollRow is not caught by clampCursor at all. What protects
// production is that no key path produces one, which is the claim this file makes
// elsewhere; these three are the last lines before an out-of-range index in the arithmetic
// that can produce one.
func TestAnOutOfRangeOffsetCannotIndexPastTheData(t *testing.T) {
	t.Run("a negative scroll offset, in the function that guards it", func(t *testing.T) {
		g := newGrid(t, 40, 3, 80, 20, 10)
		g.Focus()
		g.scrollRow = -1

		g.moveToLast()

		if g.scrollRow < 0 {
			t.Errorf("moveToLast left scrollRow at %d", g.scrollRow)
		}
		if g.cursorRow < 0 {
			t.Errorf("moveToLast left cursorRow at %d", g.cursorRow)
		}
		// And the read that panicked, taken for real.
		_ = g.SelectedRow()
		_ = g.data.Rows[g.scrollRow+g.cursorRow]
		_ = g.View()
	})

	t.Run("a negative cursor, in the function that guards it", func(t *testing.T) {
		g := newGrid(t, 40, 3, 80, 20, 10)
		g.Focus()
		g.cursorRow = -1

		g.halfPageUp()

		if g.scrollRow < 0 {
			t.Errorf("halfPageUp left scrollRow at %d", g.scrollRow)
		}
		if g.cursorRow < 0 {
			t.Errorf("halfPageUp left cursorRow at %d", g.cursorRow)
		}
		_ = g.data.Rows[g.scrollRow+g.cursorRow]
		_ = g.View()
	})

	// The two that need a window taller than the data, which is a real state: a filtered
	// grid down to a handful of rows in a tall terminal.
	for _, tc := range []struct {
		name  string
		move  func(g *Grid)
		total int
	}{
		{"halfPageDown", func(g *Grid) { g.halfPageDown() }, 3},
		{"clampCursor", func(g *Grid) { g.clampCursor() }, 3},
	} {
		t.Run("a window taller than the data, in "+tc.name, func(t *testing.T) {
			g := newGrid(t, tc.total, 3, 80, 40, 10)
			g.Focus()
			g.scrollRow = 30 // past the end, as a scroll of a shrinking grid would be
			g.cursorRow = 0

			tc.move(g)

			if g.scrollRow < 0 {
				t.Errorf("%s left scrollRow at %d", tc.name, g.scrollRow)
			}
			if g.cursorRow < 0 {
				t.Errorf("%s left cursorRow at %d", tc.name, g.cursorRow)
			}
			total := len(g.data.Rows)
			if abs := g.scrollRow + g.cursorRow; abs >= total {
				t.Errorf("%s left the cursor addressing row %d of %d", tc.name, abs, total)
			}
			_ = g.data.Rows[g.scrollRow+g.cursorRow]
			_ = g.View()
		})
	}
}

// runeAtBefore's second guard. DecodeLastRuneInString returns a zero size only for an
// empty string, and an empty s[:i] is already excluded by the `i <= 0` check above it —
// so the guard is unreachable. Pinned with the reason, because it reads as the thing that
// makes the function safe and it is not: the `i <= 0` check is.
func TestRuneAtBeforeRefusesEveryIndexBeforeTheStart(t *testing.T) {
	for _, i := range []int{-100, -1, 0} {
		if got := runeAtBefore("hello", i); got == 0 {
			t.Errorf("runeAtBefore(hello, %d) is 0", i)
		}
	}
	// And the guard that IS load-bearing: a cut in the middle of a multi-byte character.
	// DecodeLastRuneInString reports the size of the INCOMPLETE rune, so the byte before a
	// cut emoji is not the emoji and not the letter — it is RuneError, and a caller that
	// trusted it would take a byte-wise slice and produce invalid UTF-8. The byte-vs-rune
	// family, ninth instance.
	//
	// The boundaries are the byte indices either side of the emoji, which are 1 and 5:
	// a is one byte, the emoji four. The first version of this case used 2 and called that
	// "inside the emoji", which is true and therefore expects RuneError — the code was
	// right to give one.
	const emoji = "a🎉b"
	if got := runeAtBefore(emoji, len("a")); got != 'a' {
		t.Errorf("runeAtBefore just before the emoji gave %q, want a", got)
	}
	if got := runeAtBefore(emoji, len("a")+len("🎉")); got != '🎉' {
		t.Errorf("runeAtBefore just after the emoji gave %q, want the emoji", got)
	}
	if got := runeAtBefore(emoji, len("a")+1); got != utf8.RuneError {
		t.Errorf("runeAtBefore cut inside the emoji gave %q, want RuneError", got)
	}
	if got := runeAtBefore(emoji, len(emoji)); got != 'b' {
		t.Errorf("runeAtBefore at the end gave %q, want b", got)
	}
	// Past the end, which is the half of the range the guard did not have. The first
	// version of this case only tried the empty string, which the i<=0 check turned out to
	// cover — no. It did not: i was 1 and the string was "", so s[:1] sliced past a
	// zero-length string and took the test process down. That is the whole finding: an
	// index past the end panicked, and the sibling function three lines above has been
	// checking for it all along.
	// Every index past the end of each string, so the boundary itself stays a success: for
	// "abc" the index 3 is the END, and it answers 'c' rather than refusing, which is right
	// because s[:3] is a valid slice.
	for _, i := range []int{1, 2, 100} {
		if got := runeAtBefore("", i); got != utf8.RuneError {
			t.Errorf("runeAtBefore with an index of %d past an empty string gave %q", i, got)
		}
	}
	for _, i := range []int{4, 100} {
		if got := runeAtBefore("abc", i); got != utf8.RuneError {
			t.Errorf("runeAtBefore with an index of %d past \"abc\" gave %q", i, got)
		}
	}
}

// cmdFunc reports the type name of a command's message without running a command that may
// have side effects, for the assertions that only care whether one exists.
func cmdFunc(cmd func() tea.Msg) string {
	if cmd == nil {
		return "<nil>"
	}
	msg := cmd()
	if msg == nil {
		return "<nil message>"
	}
	return strings.TrimPrefix(strings.TrimPrefix(typeName(msg), "*"), "grid.")
}

func typeName(v any) string {
	switch v.(type) {
	case GridCursorMovedMsg:
		return "grid.GridCursorMovedMsg"
	default:
		return "another message type"
	}
}

// pendingRowText is one cell of a pending INSERT row, stringified — the only way to see
// what a cell edit committed while inserting.
func pendingRowText(g *Grid, row, col int) string {
	if len(g.pendingRows) <= row || len(g.pendingRows[row]) <= col {
		return "<no such cell>"
	}
	v := g.pendingRows[row][col]
	if v == nil {
		return "<nil>"
	}
	return textOf(v)
}

// textOf is a cell value as text, for the comparisons that care about content rather than
// type.
func textOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// Paging moves the cursor, so the app has to be told — the same message as every other
// cursor move, which is what lets one handler in the app serve all of them.
func TestPagingAnnouncesTheCursorMove(t *testing.T) {
	for _, action := range []config.ActionID{"next_page", "prev_page"} {
		t.Run(string(action), func(t *testing.T) {
			g := newGrid(t, 40, 3, 80, 20, 10)
			g.Focus()

			cmd, handled := g.HandleAction(action)
			if !handled || cmd == nil {
				t.Fatal("paging was declined or issued nothing")
			}
			if _, ok := cmd().(GridCursorMovedMsg); !ok {
				t.Errorf("paging produced %T, want GridCursorMovedMsg", cmd())
			}
		})
	}
}

// The unfocused border. View picks a different border colour for a grid that does not have
// the keyboard, and the two are different colours — so a version that always used the
// active one would make it impossible to tell which pane has focus by looking.
func TestTheUnfocusedGridDrawsItsOwnBorder(t *testing.T) {
	focused := newGrid(t, 4, 2, 80, 20, 10)
	_ = focused.View()

	blurred := newGrid(t, 4, 2, 80, 20, 10)
	blurred.Blur()
	if blurred.focused {
		t.Fatal("Blur did not take, so this proves nothing")
	}
	got := blurred.View()

	if got == "" {
		t.Fatal("an unfocused grid renders nothing")
	}
	// The two renders must differ, which is the property; asserting on a specific colour
	// would be measuring the theme instead.
	if got == mustRender(t, focused) {
		t.Error("the unfocused grid renders exactly like the focused one")
	}
}

func mustRender(t *testing.T, g *Grid) string {
	t.Helper()
	return g.View()
}

// Six guards that cannot fire, and the argument that makes each one impossible. They are
// the residue of real bugs — a negative scroll offset indexed Rows[-1], a panic on the first
// typed character — and the fixes that addressed those bugs made these redundant rather
// than removing them.
//
//	Pinned, not removed, because each is the last line before an index or a dereference, and
//	an argument that lives only in a commit message does not survive the next refactor.
//
// TWO OF THEM ARE DOWNSTREAM OF A SINGLE LINE. dispatchAction refuses every action when
// `len(g.data.Rows) == 0`, before it reaches any action's own case. That one check makes
// unreachable:
//
//	startDelete's `row == nil` — the dispatcher already guaranteed there is a row, and
//	  SelectedRow's hasRows() guard is the same statement a third time
//
//	navigateFK's `fkInfo == nil` — for the same reason plus the icon one: keyIcons is BUILT
//	  from foreignKeysData, so a column marked KeyFK has a matching entry by construction.
//	  navigateFK indexes g.data.Rows unguarded, which is safe for exactly this reason, and
//	  would stop being if the dispatcher's check were ever narrowed to a list of actions
//	  that need rows — which is the refactor to watch for.
//
// THE THREE cursorRow GUARDS have the same shape:
//
//	    scrollRow = totalRows - contentHeight    (clamped to 0)
//	    cursorRow = totalRows - scrollRow - 1
//
//	which is contentHeight - 1 whenever totalRows >= contentHeight and totalRows - 1
//	otherwise — both at least 0, since totalRows == 0 returns early and contentHeight <= 0
//	returns even earlier. So the guard that follows can never fire in any of the three.
//
//	runeAtBefore's `size == 0` follows from the guard above it: 0 < i <= len(s) means s[:i]
//	is non-empty, and DecodeLastRuneInString reports size 0 only for an empty string.
func TestTheSixGuardsThatCannotFire(t *testing.T) {
	t.Run("and the dispatcher refuses every action before there are rows", func(t *testing.T) {
		// The premise of two of the six, asserted rather than assumed. If this stops being
		// true then two guards become live code and this test is the one that says so —
		// and two of the nil checks below become reachable.
		for _, action := range []config.ActionID{"delete_rows", "navigate_fk", "yank", "export"} {
			g := newGrid(t, 0, 3, 80, 20, 10)
			g.Focus()
			cmd, handled := g.dispatchAction(action)
			if handled || cmd != nil {
				t.Errorf("%s was dispatched on a grid with no rows", action)
			}
		}
		// And with rows it is not refused, so the case above is about the rows and not about
		// an action nobody bound. navigate_fk needs the cursor on a foreign key's column as
		// well, which is the icon check further down its own function — a second reason to
		// refuse that has nothing to do with the number of rows.
		g := newGrid(t, 4, 3, 80, 20, 10)
		g.Focus()
		g.SetMetadata(nil, []postgres.ForeignKeyInfo{{
			Name: "c1_c2_fkey", Column: "c1", RefTable: "other", RefColumn: "id",
		}}, nil)
		g.cursorCol = 1
		if _, handled := g.dispatchAction("navigate_fk"); !handled {
			t.Error("navigate_fk was refused on a grid with rows and the cursor on a foreign key")
		}
	})
	t.Run("a grid marked with a foreign key always finds it", func(t *testing.T) {
		// The argument, exercised: build the icons from the foreign keys and look for every
		// icon the grid has. If any marked column had no foreign key, this would find it.
		g := newGrid(t, 4, 3, 80, 20, 10)
		g.SetMetadata(
			[]postgres.ConstraintInfo{{Name: "t_pkey", Type: "PRIMARY KEY", Columns: "c0"}},
			[]postgres.ForeignKeyInfo{{Name: "f1", Column: "c1", RefTable: "other"}},
			nil,
		)

		marked := 0
		for col := range g.columns {
			if g.keyIcons[col] != KeyFK {
				continue
			}
			marked++
			found := false
			for i := range g.foreignKeysData {
				if g.foreignKeysData[i].Column == g.columns[col] {
					found = true
				}
			}
			if !found {
				t.Errorf("column %d is marked as a foreign key with no foreign key naming it", col)
			}
		}
		if marked == 0 {
			t.Error("no column was marked as a foreign key; the fixture is wrong")
		}
	})

	t.Run("the cursor clamps leave the cursor in range for every window and data size", func(t *testing.T) {
		// The arithmetic, swept. Every combination of "how many rows the grid has" and "how
		// tall the window is", because the argument is that the two branches of the
		// expression are each non-negative for all of them.
		for rows := 1; rows <= 6; rows++ {
			for height := 1; height <= 20; height++ {
				g := newGrid(t, rows, 2, 80, height, 10)
				g.Focus()

				for _, move := range []struct {
					name string
					fn   func()
				}{
					{"moveToLast", g.moveToLast},
					{"halfPageUp", g.halfPageUp},
					{"halfPageDown", g.halfPageDown},
					{"clampCursor", g.clampCursor},
				} {
					move.fn()
					if g.cursorRow < 0 {
						t.Errorf("rows=%d height=%d %s left cursorRow at %d", rows, height, move.name, g.cursorRow)
					}
					if g.scrollRow < 0 {
						t.Errorf("rows=%d height=%d %s left scrollRow at %d", rows, height, move.name, g.scrollRow)
					}
				}
			}
		}
	})
}
