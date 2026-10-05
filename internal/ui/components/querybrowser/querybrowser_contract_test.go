package querybrowser

// The contracts of the query browser, asserted as properties over states rather than one
// scenario at a time.
//
// The browser is a modal over the query history: it shows a list, lets you move a cursor,
// load a query, favorite it, delete it, and filter it. Its dependency is a QueryStore,
// which a test builds over t.TempDir() so nothing touches the real history file.
//
// Four things it promises, in one place:
//
//	P1  the keys it claims are the keys it consumes, and the keys it does not claim reach
//	    the view underneath. A modal that swallows every key is not a modal, it is a
//	    hang.
//	P2  the cursor is a position in the list ON SCREEN. Filtering, switching tab and
//	    deleting all move the list under the cursor, and each of them has to bring the
//	    cursor with it.
//	P3  a key that CHANGES THE STORE acts on the query the cursor is on. This is the one
//	    that is not a property of the widget but of the mapping between the list on
//	    screen and the list in the store, and it is where the damage is.
//	P4  what is rendered is the frame the formula says, with the entry the cursor is on
//	    inside it.

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/store"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// newStore builds a store holding the given SQL strings, in order, so a test can say
// "the store has these three" and have index 0 be the first of them.
func newStore(t *testing.T, sqls ...string) *store.QueryStore {
	t.Helper()
	s := store.NewQueryStore(t.TempDir())
	for _, sql := range sqls {
		s.Add(sql)
	}
	return s
}

// newQB builds an OPEN browser over the store, at a size where the list fits. Open,
// because every test is about what it does while it is up.
func newQB(t *testing.T, sqls ...string) *QueryBrowser {
	t.Helper()
	b := New(theme.Resolve("dark").Styles(), newStore(t, sqls...))
	b.SetWidth(100)
	b.SetHeight(30)
	b.Show()
	return b
}

// press sends one key through the public entry point.
func press(t *testing.T, b *QueryBrowser, name string) (tea.Cmd, bool) {
	t.Helper()
	return b.Update(keyMsg(name))
}

// typeText sends one character per call, the way a terminal delivers a word.
func typeText(t *testing.T, b *QueryBrowser, text string) {
	t.Helper()
	for _, r := range text {
		b.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune(name[0]), Text: name}
}

// sqlsOnScreen reads the SQL text out of the rendered frame, which is what a user sees
// and therefore what an assertion about "the cursor is on this query" has to be about.
func sqlsOnScreen(b *QueryBrowser) []string {
	var out []string
	for _, e := range b.entries {
		out = append(out, e.SQL)
	}
	return out
}

// ---------------------------------------------------------------------------
// P1: what the browser claims
// ---------------------------------------------------------------------------

// Scenario: Cerrado, el navegador NO se come ninguna tecla.
//
// Same reason as the palette: while it is closed every key has to reach the grid, editor
// or explorer below it. Checked for the whole key set including the destructive ones, so
// a "d" typed into the grid cannot reach a delete.
func TestQueryBrowser_AClosedBrowserSwallowsNothing(t *testing.T) {
	for _, key := range []string{"j", "k", "g", "G", "f", "d", "/", "tab", "esc", "enter", "up", "down", "backspace"} {
		t.Run(key, func(t *testing.T) {
			b := New(theme.Resolve("dark").Styles(), newStore(t, "SELECT 1", "SELECT 2"))
			b.SetWidth(100)
			b.SetHeight(30)
			if b.IsVisible() {
				t.Fatal("a browser that was never shown reports itself visible")
			}
			if got := b.View(); got != "" {
				t.Errorf("a closed browser rendered %d bytes, want the empty string", len(got))
			}

			cmd, handled := press(t, b, key)
			if handled {
				t.Errorf("the key %q was handled by a closed browser", key)
			}
			if cmd != nil {
				t.Errorf("the key %q produced a command while closed", key)
			}
			if b.IsVisible() {
				t.Errorf("the key %q opened a closed browser", key)
			}
			// And nothing was destroyed.
			if got := b.store.Len(); got != 2 {
				t.Errorf("the key %q changed the store's length to %d, want 2", key, got)
			}
		})
	}

	t.Run("a message that is not a key is ignored", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		before := len(b.entries)
		cmd, handled := b.Update(otherMsg{})
		if handled || cmd != nil {
			t.Errorf("a non-key message produced handled=%v cmd=%v", handled, cmd != nil)
		}
		if len(b.entries) != before {
			t.Error("a non-key message changed the entry list")
		}
	})

	t.Run("a bare key the switch does not name is not handled", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		for _, code := range []rune{tea.KeyF1, tea.KeyHome, tea.KeyEnd} {
			if _, handled := b.Update(tea.KeyPressMsg{Code: code}); handled {
				t.Errorf("the bare key %q was reported as handled", code)
			}
		}
	})
}

type otherMsg struct{ N int }

// Scenario: Cerrar avisa, y abrir forgets the previous session.
//
// Two different messages: escape produces QueryBrowserClosedMsg so the app can put the
// grid back the way it was, and selecting produces QuerySelectedMsg carrying the SQL.
// Both are the widget's only outputs, so both are asserted by RUNNING the command — a
// command that returns nil, or the wrong message, fails the user silently.
func TestQueryBrowser_TheTwoMessagesItEmits(t *testing.T) {
	t.Run("escape closes and says so", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		cmd, handled := press(t, b, "esc")
		if !handled {
			t.Fatal("escape was not reported as handled")
		}
		if b.IsVisible() {
			t.Error("escape did not close the browser")
		}
		if cmd == nil {
			t.Fatal("escape returned no command, so the app is never told the modal is gone")
		}
		if _, ok := cmd().(QueryBrowserClosedMsg); !ok {
			t.Errorf("escape produced a %T, want a QueryBrowserClosedMsg", cmd())
		}
	})

	t.Run("enter loads the query under the cursor and closes", func(t *testing.T) {
		b := newQB(t, "SELECT 1", "SELECT 2")
		// All() is newest first, so index 0 is the LAST one added. The cursor
		// lands on index 1, so THAT is the entry enter has to load — not the one
		// that was on screen when the browser opened. (ineffassign flagged this
		// variable because the first version assigned `want` before moving the
		// cursor and then reassigned it, which is the same bug this test exists
		// to catch: loading the newest entry regardless of the cursor.)
		press(t, b, "j")
		if b.cursor != 1 {
			t.Fatalf("the cursor is at %d after one down, want 1", b.cursor)
		}
		want := b.entries[1].SQL
		if want == b.entries[0].SQL {
			t.Fatal("the fixture has two identical entries, so this test cannot tell them apart")
		}

		cmd, handled := press(t, b, "enter")
		if !handled {
			t.Fatal("enter was not reported as handled")
		}
		if cmd == nil {
			t.Fatal("enter returned no command")
		}
		msg, ok := cmd().(QuerySelectedMsg)
		if !ok {
			t.Fatalf("enter produced a %T, want a QuerySelectedMsg", cmd())
		}
		if msg.SQL != want {
			t.Errorf("enter loaded %q, want %q — the query under the cursor", msg.SQL, want)
		}
		if b.IsVisible() {
			t.Error("the browser stayed open after loading a query")
		}
	})

	t.Run("enter on an empty list loads nothing and stays open", func(t *testing.T) {
		b := newQB(t)
		if len(b.entries) != 0 {
			t.Fatalf("the fixture has %d entries", len(b.entries))
		}
		cmd, handled := press(t, b, "enter")
		if !handled {
			t.Error("enter on an empty list was not reported as handled")
		}
		if cmd != nil {
			t.Error("enter on an empty list produced a command to load")
		}
		if !b.IsVisible() {
			t.Error("enter on an empty list closed the browser")
		}
	})

	t.Run("opening is a reset", func(t *testing.T) {
		b := newQB(t, "SELECT 1", "SELECT 2", "SELECT 3")
		press(t, b, "tab")    // onto Favorites
		press(t, b, "/")      // filter mode
		typeText(t, b, "zzz") // and a filter that matches nothing
		press(t, b, "esc")    // which cancels the filter mode
		press(t, b, "j")

		b.Show()
		if b.tab != 0 {
			t.Errorf("Show left the tab at %d, want History", b.tab)
		}
		if b.filter != "" || b.filterActive {
			t.Errorf("Show left the filter %q active=%v", b.filter, b.filterActive)
		}
		if b.cursor != 0 || b.scroll != 0 {
			t.Errorf("Show left the cursor at %d and the scroll at %d", b.cursor, b.scroll)
		}
		if len(b.entries) != 3 {
			t.Errorf("Show left %d entries, want all 3", len(b.entries))
		}
	})
}

// ---------------------------------------------------------------------------
// P2: the cursor is a position in the list on screen
// ---------------------------------------------------------------------------

// Scenario: El cursor se mueve DENTRO de la lista, y los saltos no se salen.
//
// Walking with j/k, the two jumps (g to the first, G to the last) and the two ends of the
// list. The end stops are the ones that matter: a cursor that walks past the end selects
// a row that is not there, and the next f or d acts on whatever happens to be at that
// index in the store.
func TestQueryBrowser_TheCursorNeverLeavesTheList(t *testing.T) {
	const n = 6
	b := newQB(t, "q1", "q2", "q3", "q4", "q5", "q6")
	if len(b.entries) != n {
		t.Fatalf("the store produced %d entries, want %d", len(b.entries), n)
	}

	t.Run("j and k walk and stop at both ends", func(t *testing.T) {
		for range n + 4 {
			press(t, b, "j")
		}
		if b.cursor != n-1 {
			t.Errorf("j pressed %d times left the cursor at %d, want %d", n+4, b.cursor, n-1)
		}
		for range n + 4 {
			press(t, b, "k")
		}
		if b.cursor != 0 {
			t.Errorf("k pressed %d times left the cursor at %d, want 0", n+4, b.cursor)
		}
	})

	t.Run("down and up are the same as j and k", func(t *testing.T) {
		press(t, b, "down")
		press(t, b, "down")
		if b.cursor != 2 {
			t.Errorf("two downs left the cursor at %d, want 2", b.cursor)
		}
		press(t, b, "up")
		if b.cursor != 1 {
			t.Errorf("an up left the cursor at %d, want 1", b.cursor)
		}
	})

	t.Run("g and G jump to the ends", func(t *testing.T) {
		press(t, b, "G")
		if b.cursor != n-1 {
			t.Errorf("G left the cursor at %d, want %d", b.cursor, n-1)
		}
		press(t, b, "g")
		if b.cursor != 0 {
			t.Errorf("g left the cursor at %d, want 0", b.cursor)
		}
		if b.scroll != 0 {
			t.Errorf("g left the scroll at %d, want 0", b.scroll)
		}
	})

	t.Run("G on an empty list leaves the cursor at zero, not minus one", func(t *testing.T) {
		empty := newQB(t)
		press(t, empty, "G")
		if empty.cursor != 0 {
			t.Errorf("G on an empty list left the cursor at %d, want 0: a negative cursor is what indexes from the end of the store", empty.cursor)
		}
		press(t, empty, "j")
		if empty.cursor != 0 {
			t.Errorf("j on an empty list left the cursor at %d, want 0", empty.cursor)
		}
	})

	t.Run("deleting the last entry pulls the cursor back", func(t *testing.T) {
		b := New(theme.Resolve("dark").Styles(), newStore(t, "keep me", "delete me"))
		b.SetWidth(100)
		b.SetHeight(30)
		b.Show()
		press(t, b, "j") // onto "delete me", which All() puts first
		press(t, b, "d")
		if len(b.entries) != 1 {
			t.Fatalf("after deleting, %d entries remain, want 1", len(b.entries))
		}
		if b.cursor >= len(b.entries) || b.cursor < 0 {
			t.Errorf("after deleting the entry under the cursor it is at %d with %d entries", b.cursor, len(b.entries))
		}
	})
}

// Scenario: Filtrar mueve la lista DEBAJO del cursor, y el cursor la sigue.
//
// Filtering is the case where the list under the cursor changes shape, and it is the one
// where a cursor left pointing at the old index selects something the user never looked
// at. Both directions matter: narrowing under the cursor, and clearing back out.
func TestQueryBrowser_FilteringBringsTheCursorWithIt(t *testing.T) {
	// A FRESH browser per subtest, and that is not tidiness. "/" only opens the
	// filter when filter mode is already OFF; with it on, "/" is typed INTO the
	// filter. A shared fixture carries the mode from one subtest into the next, and
	// every assertion after the first is then about the wrong state — which is
	// exactly what happened the first time this was written.
	fresh := func(t *testing.T) *QueryBrowser {
		t.Helper()
		return newQB(t, "SELECT apple", "SELECT banana", "SELECT cherry", "SELECT durian")
	}

	t.Run("the filter matches the SQL and the name, case-insensitively", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		typeText(t, b, "APPLE")
		if len(b.entries) != 1 || b.entries[0].SQL != "SELECT apple" {
			t.Errorf("filtering for APPLE left %v", sqlsOnScreen(b))
		}
		press(t, b, "esc")
		if len(b.entries) != 4 {
			t.Errorf("escape did not clear the filter: %v", sqlsOnScreen(b))
		}
	})

	t.Run("narrowing under the cursor leaves it inside", func(t *testing.T) {
		b := fresh(t)
		for range 3 {
			press(t, b, "j")
		}
		far := b.cursor
		if far < 2 {
			t.Skipf("only %d entries, not enough to move away from the first", far+1)
		}

		press(t, b, "/")
		typeText(t, b, "SELECT a") // matches exactly one of them
		if b.cursor < 0 || b.cursor >= len(b.entries) {
			t.Errorf("after filtering to %d entries the cursor is at %d", len(b.entries), b.cursor)
		}
	})

	t.Run("a filter that matches nothing empties the list and says so", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		typeText(t, b, "zzzz")
		if len(b.entries) != 0 {
			t.Errorf("a nonsense filter left %v", sqlsOnScreen(b))
		}
		if !strings.Contains(b.View(), "No matches") {
			t.Error("an empty filtered list does not say that nothing matched")
		}
		if b.cursor != 0 {
			t.Errorf("an empty list left the cursor at %d, want 0", b.cursor)
		}
		press(t, b, "esc")
	})

	t.Run("enter in filter mode keeps the filter and leaves filter mode", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		typeText(t, b, "banana")
		press(t, b, "enter")
		if b.filterActive {
			t.Error("enter did not leave filter mode")
		}
		if b.filter != "banana" {
			t.Errorf("enter cleared the filter to %q, want it kept", b.filter)
		}
		if len(b.entries) != 1 {
			t.Errorf("after enter the list is %v, want the one match", sqlsOnScreen(b))
		}
		// Escape now takes the MAIN arm, because filter mode is over: it closes
		// the browser rather than clearing the filter, and the filter survives
		// into the next session. That is deliberate — reopening on the same
		// filter is what you want after a mis-typed search — and it means
		// clearing it means going back into filter mode and escaping there.
		cmd, handled := press(t, b, "esc")
		if !handled {
			t.Error("escape after applying a filter was not reported as handled")
		}
		if cmd == nil {
			t.Error("escape produced no command, so the app is not told the modal closed")
		} else if _, ok := cmd().(QueryBrowserClosedMsg); !ok {
			t.Errorf("escape produced a %T, want a QueryBrowserClosedMsg", cmd())
		}
		if b.IsVisible() {
			t.Error("escape did not close the browser")
		}
		if b.filter != "banana" {
			t.Errorf("closing cleared the filter to %q, want it kept for the next session", b.filter)
		}

		b.Show()
		if b.filter != "" {
			t.Errorf("Show left the previous session's filter %q in place", b.filter)
		}
	})

	t.Run("backspace removes one character, and nothing at all when empty", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		typeText(t, b, "banana")
		press(t, b, "backspace")
		if b.filter != "banan" {
			t.Errorf("one backspace left the filter %q, want %q", b.filter, "banan")
		}
		for range 10 {
			press(t, b, "backspace")
		}
		if b.filter != "" {
			t.Errorf("backspacing past the start left the filter %q", b.filter)
		}
		if len(b.entries) != 4 {
			t.Errorf("an empty filter left %d entries, want all 4", len(b.entries))
		}
		press(t, b, "esc")
	})

	t.Run("a multi-byte filter is backspaced by CHARACTER", func(t *testing.T) {
		b := fresh(t)
		// The filter is a slice of the keystrokes, so it is text the user typed and
		// an accented character is one keystroke carrying several bytes.
		press(t, b, "/")
		b.Update(tea.KeyPressMsg{Code: 'é', Text: "é"})
		press(t, b, "backspace")
		if b.filter != "" {
			t.Errorf("backspacing an accented character left %q, want the empty filter", b.filter)
		}
		press(t, b, "esc")
	})

	t.Run("while the filter is active the navigation keys type instead of moving", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		typeText(t, b, "banana")
		if b.filter != "banana" {
			t.Errorf("filtering typed %q, want %q", b.filter, "banana")
		}
		if b.cursor != 0 {
			t.Errorf("typing in the filter moved the cursor to %d", b.cursor)
		}
		press(t, b, "esc")
	})

	t.Run("space is a character of the filter", func(t *testing.T) {
		b := fresh(t)
		press(t, b, "/")
		press(t, b, "space")
		if b.filter != " " {
			t.Errorf("a space produced the filter %q, want one space", b.filter)
		}
		press(t, b, "esc")
	})
}

// ---------------------------------------------------------------------------
// P3: the keys that change the store act on the cursor's query
// ---------------------------------------------------------------------------

// Scenario: f y d tocan la consulta que esta DEBAJO del cursor, no la de su indice.
//
// THIS IS THE ONE THAT MATTERS, and it is not a property of the widget: it is a property
// of the mapping between the list on screen and the list in the store.
//
// The store's ToggleFavorite and Delete take an index into All() — the UNFILTERED,
// reverse-chronological list. The browser hands them b.cursor, which indexes b.entries,
// and b.entries is the FILTERED list whenever a filter is active and the FAVORITES list
// whenever the Favorites tab is. Those are three different orderings, so the index the
// browser passes names a different query than the one the user is looking at.
//
// For 'd' that is data loss: the wrong saved query disappears and the one under the
// cursor stays. For 'f' it is quieter and just as wrong — a star appears next to some
// other query.
//
// Both are asserted against what the user SEES, not against the store's internals: the
// entry under the cursor before the keystroke, and the store's contents after it.
func TestQueryBrowser_FavoriteAndDeleteActOnTheQueryUnderTheCursor(t *testing.T) {
	// Three queries whose names make the order obvious, and none of them a
	// prefix of another so a filter selects exactly one.
	t.Run("with a filter active", func(t *testing.T) {
		b := newQB(t, "SELECT alpha", "SELECT bravo", "SELECT charlie")

		// Narrow to exactly one — and it has to be the OLDEST query. All() is
		// newest-first, so whenever the newest entry survives the filter, index 0
		// of the filtered list and index 0 of the store are the SAME row, and a
		// fixture that lands there proves nothing while looking as though it
		// does. "alpha" was added first, so it sits LAST in All(). Skipping here
		// instead of failing is what hid this the first time.
		press(t, b, "/")
		typeText(t, b, "alpha")
		if len(b.entries) != 1 {
			t.Fatalf("the filter selected %d entries, want 1", len(b.entries))
		}
		// Leave filter mode with enter before acting. While the filter is being
		// typed, "d" is a LETTER — it goes into the filter, which is the right
		// thing, since a keystroke that deletes history should not be one
		// character away. What a user does is filter, press enter, then act.
		press(t, b, "enter")
		if b.filterActive {
			t.Fatal("enter did not leave filter mode")
		}
		onScreen := b.entries[0].SQL
		storeAt := b.store.All()[0].SQL
		if onScreen == storeAt {
			t.Fatalf("the fixture picked %q, which is also the store's index 0, so the two orderings cannot be told apart — filter for the oldest query instead", onScreen)
		}

		press(t, b, "d")
		// The assertion, and its direction, are the whole test. The query under
		// the cursor must be GONE and the one at the store's index 0 must have
		// SURVIVED — a first version of these two lines had the conditions
		// inverted, so the test passed with the bug present while its message
		// said the opposite. A message that disagrees with its own condition is
		// worse than no message: it reads like a reason to believe the code.
		if containsSQL(b.store.All(), onScreen) {
			t.Errorf("d left %q in the store, so the query under the cursor was NOT the one deleted", onScreen)
		}
		if !containsSQL(b.store.All(), storeAt) {
			t.Errorf("d deleted %q, which is the store's index 0, instead of %q, the query under the cursor", storeAt, onScreen)
		}
		press(t, b, "esc")
	})

	t.Run("on the Favorites tab, with no filter at all", func(t *testing.T) {
		b := New(theme.Resolve("dark").Styles(), newStore(t, "favA", "plain", "favB"))
		b.SetWidth(100)
		b.SetHeight(30)
		b.Show()

		// A favorite, then a plain one, then another favorite. The plain one in
		// the middle is the whole point: All() is favB, plain, favA while
		// Favorites() is favB, favA, so the two lists AGREE on row 0 and DIVERGE
		// on row 1. With the favorites adjacent they agree everywhere and the
		// fixture cannot see the bug at all.
		favoriteByName(t, b, "favA")
		favoriteByName(t, b, "favB")

		press(t, b, "tab")
		if b.tab != 1 {
			t.Fatalf("tab left the browser on tab %d", b.tab)
		}
		if len(b.entries) != 2 {
			t.Fatalf("the Favorites tab shows %d entries, want 2", len(b.entries))
		}
		if b.entries[0].SQL == b.store.All()[0].SQL && b.entries[1].SQL == b.store.All()[1].SQL {
			t.Fatal("the fixture's favorites and the store's first two entries agree on both rows, so the two orderings cannot be told apart")
		}
		// Index 0 AGREES between the two lists, because the newest favorite is
		// also the newest entry overall. The mapping only breaks where they
		// DIVERGE, which is the second row: the favorites are D and B while All()
		// is D, C, B, A — so favorites[1] is B and All()[1] is C.
		onScreen := b.entries[1].SQL
		storeAt := b.store.All()[1].SQL
		if onScreen == storeAt {
			t.Fatalf("the fixture's row 1 is %q in both lists, so the two orderings cannot be told apart", onScreen)
		}
		press(t, b, "j")
		if b.entries[b.cursor].SQL != onScreen {
			t.Fatalf("the cursor is on %q, want the row the fixture picked (%q)", b.entries[b.cursor].SQL, onScreen)
		}

		press(t, b, "d")
		if containsSQL(b.store.All(), onScreen) {
			t.Errorf("d left %q in the store, so the favorite under the cursor was NOT the one deleted", onScreen)
		}
		if !containsSQL(b.store.All(), storeAt) {
			t.Errorf("d deleted %q, which is the store's index %d, instead of %q, the row under the cursor", storeAt, 1, onScreen)
		}
	})

	t.Run("f favorites the entry under the cursor, unfiltered", func(t *testing.T) {
		b := newQB(t, "SELECT one", "SELECT two", "SELECT three")
		press(t, b, "j")
		onScreen := b.entries[b.cursor].SQL
		storeAt := b.store.All()[b.cursor].SQL
		if onScreen != storeAt {
			t.Skipf("unfiltered, the cursor's entry and the store's index %d differ (%q vs %q); the mapping is broken even with no filter", b.cursor, onScreen, storeAt)
		}
		press(t, b, "f")
		if !isFavorite(b.store.All(), onScreen) {
			t.Errorf("f did not favorite %q, the query under the cursor", onScreen)
		}
	})
}

// favoriteByName marks the named query as a favorite by walking the browser, so the
// fixture does not depend on which index the store happens to put it at.
// favoriteByName marks the named query as a favorite through the browser. It searches
// the STORE rather than b.entries on purpose: the Favorites tab is empty by definition,
// so a fixture that calls this while that tab is up is asking the wrong list.
func favoriteByName(t *testing.T, b *QueryBrowser, sql string) {
	t.Helper()
	idx := -1
	for i, e := range b.store.All() {
		if e.SQL == sql {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("the store has no entry %q", sql)
	}
	// Favoriting is only reachable from History — on the Favorites tab the rows
	// are already favorites, and "f" there would UNfavorite one. So the helper
	// switches, acts, and switches BACK, which is what makes it usable from a
	// fixture that is sitting on either tab.
	wasFavorites := b.tab == 1
	if wasFavorites {
		press(t, b, "tab")
	}
	defer func() {
		if wasFavorites {
			press(t, b, "tab")
		}
	}()
	if idx >= len(b.entries) || b.entries[idx].SQL != sql {
		t.Fatalf("the store's index %d is %q but the History tab shows %q there",
			idx, sql, b.entries[idx].SQL)
	}
	b.cursor = idx
	press(t, b, "f")
	if !isFavorite(b.store.All(), sql) {
		t.Fatalf("pressing f on %q did not favorite it", sql)
	}
}

func containsSQL(entries []store.QueryEntry, sql string) bool {
	for _, e := range entries {
		if e.SQL == sql {
			return true
		}
	}
	return false
}

func isFavorite(entries []store.QueryEntry, sql string) bool {
	for _, e := range entries {
		if e.SQL == sql {
			return e.Favorite
		}
	}
	return false
}

// Scenario: La pestaña Favorites MUESTRA los favoritos, y la de Historial todos.
//
// Tab is a two-way toggle and both halves have to be right, including the empty states,
// which are different strings on purpose: "no queries yet" is an invitation and "no
// favorites" is an instruction.
func TestQueryBrowser_TheTwoTabsShowWhatTheySay(t *testing.T) {
	b := New(theme.Resolve("dark").Styles(), newStore(t, "one", "two"))
	b.SetWidth(100)
	b.SetHeight(30)
	b.Show()

	if len(b.entries) != 2 {
		t.Fatalf("History shows %d entries, want 2", len(b.entries))
	}
	if !strings.Contains(b.View(), "History") {
		t.Error("the History tab is not named in the rendered frame")
	}

	press(t, b, "tab")
	if len(b.entries) != 0 {
		t.Errorf("Favorites shows %d entries with nothing favorited", len(b.entries))
	}
	view := b.View()
	if !strings.Contains(view, "No favorites") {
		t.Errorf("an empty Favorites tab does not say so: %q", ansi.Strip(view))
	}

	// Favorite from the Favorites tab: the helper goes to History, presses f, and
	// comes back, so the assertion below is about the tab the user is looking at.
	favoriteByName(t, b, "two")
	if len(b.entries) != 1 {
		t.Errorf("after favoriting one query the Favorites tab shows %d entries, want 1", len(b.entries))
	}

	press(t, b, "tab")
	if len(b.entries) != 2 {
		t.Errorf("going back to History shows %d entries, want 2", len(b.entries))
	}
	if b.cursor != 0 || b.scroll != 0 {
		t.Errorf("switching tabs left the cursor at %d and the scroll at %d, want both 0", b.cursor, b.scroll)
	}

	t.Run("an empty store says the History tab is empty", func(t *testing.T) {
		empty := newQB(t)
		if !strings.Contains(empty.View(), "No queries yet") {
			t.Errorf("an empty History tab does not say so: %q", ansi.Strip(empty.View()))
		}
	})
}

// ---------------------------------------------------------------------------
// P4: what is drawn
// ---------------------------------------------------------------------------

// Scenario: La ventana tiene UNA FORMULA, con suelo y techo.
//
// The frame is three quarters of the viewport wide, clamped to [50, 90], and four fifths
// tall with a floor of 10. Both clamps are pinned because the floor swallows the fraction
// for small terminals exactly the way the palette's does, and an unpinned clamp is a
// layout change nobody notices until a user has a small window.
func TestQueryBrowser_TheFrameHasAFormulaWithAFloorAndACeiling(t *testing.T) {
	wantW := func(v int) int {
		w := v * 3 / 4
		if w < 50 {
			w = 50
		}
		if w > 90 {
			w = 90
		}
		return w
	}
	wantH := func(v int) int {
		h := v * 8 / 10
		if h < 10 {
			h = 10
		}
		return h
	}

	for _, tc := range []struct{ w, h int }{
		{20, 10}, {40, 10}, {80, 24}, {100, 30}, {120, 40}, {200, 60},
	} {
		b := New(theme.Resolve("dark").Styles(), newStore(t, "SELECT 1"))
		b.SetWidth(tc.w)
		b.SetHeight(tc.h)
		b.Show()

		lines := strings.Split(b.View(), "\n")
		if got := ansi.StringWidth(lines[0]); got != wantW(tc.w) {
			t.Errorf("at %dx%d the frame is %d cells wide, want %d", tc.w, tc.h, got, wantW(tc.w))
		}
		if got := len(lines); got != wantH(tc.h) {
			t.Errorf("at %dx%d the frame is %d lines tall, want %d", tc.w, tc.h, got, wantH(tc.h))
		}
		for i, line := range lines {
			if n := ansi.StringWidth(line); n != wantW(tc.w) {
				t.Errorf("at %dx%d line %d is %d cells wide, want %d: %q", tc.w, tc.h, i, n, wantW(tc.w), ansi.Strip(line))
			}
		}
	}
}

// Scenario: Una lista larga se DESPLAZA, y la consulta del cursor queda a la vista.
//
// The window shows modalH-4 entries and the store can hold five hundred, so the list has
// to scroll. What is asserted is the promise — the highlighted query is a row the user can
// see — plus the scroll offset moving in both directions, because a scroll that only goes
// one way is a list you cannot read to the end.
func TestQueryBrowser_ALongListScrollsAndKeepsTheCursorOnScreen(t *testing.T) {
	const n = 40
	sqls := make([]string, n)
	for i := range n {
		sqls[i] = "SELECT " + strings.Repeat("x", i+1)
	}
	b := newQB(t, sqls...)

	// The window at height 30 is (30*8/10)-4 = 20 entries.
	if got, want := b.maxVisibleEntries(), 20; got != want {
		t.Fatalf("the window is %d entries, want %d — the fixture's size assumption is wrong", got, want)
	}

	walkTo := func(i int) {
		b.cursor = 0
		b.scroll = 0
		for range i {
			press(t, b, "j")
		}
	}
	onScreen := func() string {
		return ansi.Strip(b.View())
	}

	walkTo(n - 1)
	if b.scroll == 0 {
		t.Error("walking to the end of a longer-than-window list left the scroll at zero")
	}
	if !strings.Contains(onScreen(), b.entries[b.cursor].SQL) {
		t.Errorf("the cursor's query is not on screen at the end of the list")
	}

	// Every row the cursor visits must be visible: that is what the scroll is for.
	for _, i := range []int{0, 1, 19, 20, 21, n - 2, n - 1} {
		walkTo(i)
		view := onScreen()
		if !strings.Contains(view, b.entries[b.cursor].SQL) {
			t.Errorf("at the cursor's row %d the query is not on screen", i)
		}
		if b.scroll < 0 || b.scroll > b.cursor {
			t.Errorf("at row %d the scroll is %d, want 0..%d", i, b.scroll, b.cursor)
		}
	}

	// And back to the top.
	press(t, b, "g")
	if b.scroll != 0 {
		t.Errorf("g left the scroll at %d, want 0", b.scroll)
	}
}

// Scenario: Cada fila dice CUANDO se ejecuto, y como un RELOJ y no como un numero.
//
// The timestamp is rendered as a human age, and the four buckets are the contract: under a
// minute, under an hour, under a day, and days. The boundaries are what a test has to pin,
// because "59m ago" and "1h ago" are the same fact told two ways and only one of them is
// right at the boundary.
func TestQueryBrowser_EachRowSaysHowOldTheQueryIs(t *testing.T) {
	for _, tc := range []struct {
		name string
		age  time.Duration
		want string
	}{
		{"seconds old", 3 * time.Second, "just now"},
		{"fifty-nine seconds old", 59 * time.Second, "just now"},
		{"sixty seconds old", 60 * time.Second, "1m ago"},
		{"one hour minus a second", 59*time.Minute + 59*time.Second, "59m ago"},
		{"exactly one hour", time.Hour, "1h ago"},
		{"twenty-three hours", 23 * time.Hour, "23h ago"},
		{"exactly a day", 24 * time.Hour, "1d ago"},
		{"ten days", 10 * 24 * time.Hour, "10d ago"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := store.QueryEntry{SQL: "SELECT 1", Timestamp: time.Now().Add(-tc.age)}
			b := newQB(t, "SELECT 1")
			line := ansi.Strip(b.renderEntry(0, e, 60))
			if !strings.Contains(line, tc.want) {
				t.Errorf("a query %s old rendered %q, want it to say %q", tc.name, line, tc.want)
			}
		})
	}

	t.Run("a multiline query is shown on one line", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		e := store.QueryEntry{SQL: "SELECT a,\n       b,\n       c", Timestamp: time.Now()}
		line := ansi.Strip(b.renderEntry(0, e, 60))
		if strings.Count(line, "\n") != 0 {
			t.Errorf("a three-line query rendered as %q: the newlines have to be flattened or the frame tears", line)
		}
		if !strings.Contains(line, "SELECT a,        b,        c") {
			t.Errorf("the flattened query is %q, want the lines joined with spaces", line)
		}
	})

	t.Run("a favorite is starred and a plain query is not", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		plain := ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "x", Timestamp: time.Now()}, 60))
		starred := ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "x", Favorite: true, Timestamp: time.Now()}, 60))
		if strings.Contains(plain, "★") {
			t.Errorf("a plain query is starred: %q", plain)
		}
		if !strings.Contains(starred, "★") {
			t.Errorf("a favorite is not starred: %q", starred)
		}
	})

	t.Run("a query too long for the row is cut with an ellipsis", func(t *testing.T) {
		b := newQB(t, "SELECT 1")
		long := store.QueryEntry{SQL: "SELECT " + strings.Repeat("y", 200), Timestamp: time.Now()}
		line := ansi.Strip(b.renderEntry(0, long, 40))
		if n := ansi.StringWidth(line); n != 40 {
			t.Errorf("the row is %d cells wide, want exactly 40: %q", n, line)
		}
		if !strings.Contains(line, "…") {
			t.Errorf("a cut row does not end in an ellipsis: %q", line)
		}
	})

	t.Run("the selected row is styled differently from the others", func(t *testing.T) {
		b := newQB(t, "SELECT one", "SELECT two")
		sel := b.renderEntry(0, b.entries[0], 50)
		other := b.renderEntry(1, b.entries[1], 50)
		if sel == other {
			t.Error("the row under the cursor renders exactly like the row that is not")
		}
		if b.cursor != 0 {
			b.cursor = 1
			if b.renderEntry(1, b.entries[1], 50) != sel {
				t.Error("the highlight did not follow the cursor")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// A property, over many states
// ---------------------------------------------------------------------------

// Scenario: Despues de CUALQUIER secuencia de teclas, el navegador sigue siendo coherente.
//
// The scenario tests each reach one state. This walks a lot of them — deterministic, so a
// failure names a seed — and checks the invariants that keep the widget usable rather than
// merely correct on the happy path:
//
//   - the cursor indexes an entry that exists
//   - the scroll offset is inside the list
//   - the frame is the size the formula says, at any viewport
//   - the query under the cursor is on screen
//   - the filter is valid UTF-8, which is the invariant the byte-wise backspace breaks
//
// The last one is the reason this test exists as well as the scenarios: the bug it catches
// is invisible in a screenshot and in every other assertion here.
func TestQueryBrowser_AfterAnyKeySequenceTheBrowserIsStillCoherent(t *testing.T) {
	keys := []string{"j", "k", "g", "G", "f", "d", "/", "tab", "esc", "enter",
		"up", "down", "backspace", "a", "z", "SELECT", "é"}
	viewports := []struct{ w, h int }{{40, 10}, {80, 24}, {100, 30}, {200, 60}}

	seed := int64(1)
	for round := range 24 {
		sqls := make([]string, 12)
		for i := range sqls {
			sqls[i] = "SELECT t" + string(rune('a'+i))
		}
		vp := viewports[round%len(viewports)]
		b := New(theme.Resolve("dark").Styles(), newStore(t, sqls...))
		b.SetWidth(vp.w)
		b.SetHeight(vp.h)
		b.Show()

		rng := rand.New(rand.NewSource(seed + int64(round)))
		for step := range 70 {
			key := keys[rng.Intn(len(keys))]
			if key == "esc" {
				b.Show() // keep the walk inside one session
			}
			if len(key) == 1 {
				b.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
			} else {
				b.Update(keyMsg(key))
			}

			if b.cursor < 0 {
				t.Fatalf("round %d step %d: the cursor went negative (%d) after the key %q",
					round, step, b.cursor, key)
			}
			if len(b.entries) > 0 && b.cursor >= len(b.entries) {
				t.Fatalf("round %d step %d: the cursor is %d with %d entries after the key %q",
					round, step, b.cursor, len(b.entries), key)
			}
			if b.scroll < 0 {
				t.Fatalf("round %d step %d: the scroll went negative (%d) after the key %q",
					round, step, b.scroll, key)
			}
			if !b.IsVisible() {
				continue
			}

			view := b.View()
			want := wantFrameWidth(vp.w)
			lines := strings.Split(view, "\n")
			for i, line := range lines {
				if n := ansi.StringWidth(line); n != want {
					t.Fatalf("round %d step %d: line %d is %d cells wide, want %d, after the key %q\n%q",
						round, step, i, n, want, key, ansi.Strip(view))
				}
			}
			if len(b.entries) > 0 {
				if !strings.Contains(ansi.Strip(view), b.entries[b.cursor].SQL) {
					t.Fatalf("round %d step %d: the cursor's query %q is not on screen after the key %q\n%s",
						round, step, b.entries[b.cursor].SQL, key, ansi.Strip(view))
				}
			}
		}
	}
}

func wantFrameWidth(viewport int) int {
	w := viewport * 3 / 4
	if w < 50 {
		w = 50
	}
	if w > 90 {
		w = 90
	}
	return w
}

// Scenario: La fila se recorta al ANCHO de la fila, incluso cuando el hueco es minimo.
//
// renderEntry takes the width the row has and lays out three parts in it: a ten-cell
// timestamp, a two-cell favorite mark and the SQL. The SQL's share is the row width minus
// thirteen, with a floor of ten cells — so for a row narrower than twenty-three the floor
// wins and the assembled row is WIDER than the row it was given, which is the one case
// where the line has to be cut back down.
//
// Three widths, because each one takes a different branch and a fixture that only used a
// comfortable width would never reach the other two: comfortable (padding), at the floor
// (the SQL's floor wins), and below it (the row has to be cut back).
func TestRenderEntry_AWideRowIsCutBackToTheRowWidth(t *testing.T) {
	b := newQB(t, "SELECT 1")
	now := time.Now()
	long := store.QueryEntry{SQL: "SELECT " + strings.Repeat("y", 200), Timestamp: now}

	for _, tc := range []struct {
		name   string
		sql    string
		maxW   int
		wantSq string // "" = do not check the SQL's own text
	}{
		{"a comfortable width pads the row", "SELECT 1", 60, "SELECT 1"},
		{"the SQL's own floor of ten cells wins", strings.Repeat("z", 40), 18, ""},
		{"a row narrower than the fixed parts is cut back", "SELECT 1", 12, ""},
		{"a very narrow row is still cut back, not negative", "x", 8, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := store.QueryEntry{SQL: tc.sql, Timestamp: now}
			line := b.renderEntry(0, e, tc.maxW)
			got := ansi.StringWidth(line)
			if got > tc.maxW {
				t.Errorf("the row is %d cells wide for a %d-cell row: %q", got, tc.maxW, ansi.Strip(line))
			}
			if tc.wantSq != "" && !strings.Contains(ansi.Strip(line), tc.wantSq) {
				t.Errorf("the row is %q, want it to contain %q", ansi.Strip(line), tc.wantSq)
			}
		})
	}

	// The long query at a comfortable width is cut with an ellipsis rather than
	// being allowed to overflow, which is the other half of the same promise.
	line := ansi.Strip(b.renderEntry(0, long, 40))
	if n := ansi.StringWidth(line); n != 40 {
		t.Errorf("a 200-character query in a 40-cell row rendered %d cells", n)
	}
	if !strings.Contains(line, "…") {
		t.Errorf("the cut row does not end in an ellipsis: %q", line)
	}
}

// Scenario: Subir el cursor tambien desplaza la lista.
//
// ensureVisible moves the window in BOTH directions, and only walking down exercises one
// of them: walking up past the top of the window is the case where the cursor ends up
// above the scroll and the window has to be pulled up to meet it. A long list walked to the
// end and then back up one row is the only way to reach it — "g" jumps straight to zero
// and resets the scroll itself, so it never goes through this.
func TestQueryBrowser_WalkingBackUpPullsTheWindowWithTheCursor(t *testing.T) {
	const n = 40
	sqls := make([]string, n)
	for i := range n {
		sqls[i] = "SELECT " + strings.Repeat("x", i+1)
	}
	b := newQB(t, sqls...)

	for range n {
		press(t, b, "j")
	}
	if b.scroll == 0 {
		t.Fatal("walking down never scrolled, so there is nothing to pull back up")
	}
	scrolledTo := b.scroll

	// Now walk back up, one row at a time and all the way. Pairing each "k"
	// with a "j" looks like it makes progress and does not: the net movement is
	// zero, so the cursor never climbs back over the top edge and the branch that
	// pulls the window up is never reached. The scroll only shrinks as the cursor
	// actually rises, so the walk has to be one-directional.
	rows := 0
	for b.cursor > 0 {
		press(t, b, "k")
		rows++
		if b.cursor < b.scroll {
			t.Fatalf("after %d up presses the cursor is at %d and the window starts at %d: the row is off screen",
				rows, b.cursor, b.scroll)
		}
		if b.scroll > scrolledTo {
			t.Fatalf("after %d up presses the scroll grew to %d, from %d", rows, b.scroll, scrolledTo)
		}
		if !strings.Contains(ansi.Strip(b.View()), b.entries[b.cursor].SQL) {
			t.Fatalf("after %d up presses, at row %d, the query is not on screen:\n%s",
				rows, b.cursor, ansi.Strip(b.View()))
		}
	}
	if b.scroll != 0 {
		t.Errorf("walking all the way to the first row left the scroll at %d, want 0", b.scroll)
	}

	press(t, b, "g")
	if b.scroll != 0 {
		t.Errorf("g left the scroll at %d, want 0", b.scroll)
	}
}
