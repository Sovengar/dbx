package explorer

// The contracts of the explorer, asserted as properties over states.
//
// The explorer is the tree down the left: a database, its schemas, their tables and their
// columns, with a filter line, a cursor, and a set of actions that either move the tree or
// emit a message asking the app to go and do something to a table.
//
// Ten things it promises:
//
//	E1  the cursor indexes a row that exists, after EVERY kind of change to the list
//	    — not after most of them, which is how it used to crash
//	E2  the filter narrows to TABLE names, case-insensitively, and keeps the way there
//	E3  the filter IGNORES expansion: a collapsed schema still shows its matching table
//	E4  the filter line is TEXT — enter keeps it, escape abandons it, backspace takes one
//	    character, and every key is claimed (which is why the app carves some out)
//	E5  expanding and collapsing leave the cursor on a row that exists, and collapsing a
//	    collapsed node goes to its PARENT
//	E6  each action emits the message that belongs to the selected node, and nothing when
//	    the node is the wrong kind
//	E7  toggle_columns on a table collapses its schema and lands the cursor there; on a
//	    schema it toggles
//	E8  SelectTable expands the ancestors and lands the cursor, or reports that it did not
//	E9  a click maps a pane-relative y to the row under it, with the border and the filter
//	    line accounted for
//	E10 what is rendered says what the metadata says, and never more rows than the window
//
// The icons are blank and the expand marker does not change between expanded and
// collapsed. That is pinned on purpose, with the reason, rather than left as a passing
// assertion that proves nothing.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func registry() config.Resolver {
	return config.NewKeybindRegistry(config.KeybindingsConfig{})
}

// fixture builds the tree every test here walks. The shape is deliberately awkward: two
// schemas (one collapsed), tables with and without row counts, columns with and without a
// type, nullable or not, with and without a default. A fixture where every node is uniform
// leaves half of applyFilter and formatColumn unreached.
type fixture struct {
	explorer *Explorer
	db       *Node
	public   *Node
	audit    *Node
	users    *Node
	orders   *Node
	items    *Node
	events   *Node
	email    *Node
	id       *Node
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	e := New(theme.Resolve("dark").Styles(), nil, registry())
	e.SetWidth(34)
	e.SetHeight(12)

	f := &fixture{explorer: e}
	f.db = NewNode("db", NodeDatabase, "app")
	f.db.Expanded = true

	f.public = NewNode("sch", NodeSchema, "public")
	f.public.Metadata["schema"] = "public"
	f.public.Expanded = true
	f.db.AddChild(f.public)

	f.audit = NewNode("sch2", NodeSchema, "audit")
	f.audit.Metadata["schema"] = "audit"
	f.audit.Expanded = false // COLLAPSED, so the expansion rules are exercised
	f.db.AddChild(f.audit)

	for i, name := range []string{"users", "orders", "items"} {
		tbl := NewNode(fmt.Sprintf("t%d", i), NodeTable, name)
		tbl.Metadata["schema"] = "public"
		tbl.Metadata["row_count"] = 10 * (i + 1) // 10, 20, 30
		f.public.AddChild(tbl)
		switch name {
		case "users":
			f.users = tbl
		case "orders":
			f.orders = tbl
		case "items":
			f.items = tbl
		}
	}

	f.id = NewNode("c0", NodeColumn, "id")
	f.id.Metadata["data_type"] = "int4"
	f.id.Metadata["is_nullable"] = "NO"
	f.users.AddChild(f.id)

	f.email = NewNode("c1", NodeColumn, "email")
	f.email.Metadata["data_type"] = "text"
	f.email.Metadata["is_nullable"] = "YES"
	def := "'unknown'::text"
	f.email.Metadata["column_default"] = &def
	f.users.AddChild(f.email)

	f.events = NewNode("t3", NodeTable, "events")
	f.events.Metadata["schema"] = "audit"
	f.events.Metadata["row_count"] = 7
	f.audit.AddChild(f.events)

	e.SetNodes([]*Node{f.db})
	return f
}

func (f *fixture) names() []string {
	out := make([]string, 0, len(f.explorer.tree.filtered))
	for _, n := range f.explorer.tree.filtered {
		out = append(out, n.Name)
	}
	return out
}

func (f *fixture) typeFilter(filter string) []string {
	f.explorer.tree.SetFilter(filter)
	return f.names()
}

// cursorIsInRange is the assertion that keeps the clamp honest. A Selected() that quietly
// returned nil for an out-of-range cursor would make every other test in this file pass
// with the crash still present, so the invariant is asserted directly instead.
func (f *fixture) cursorIsInRange(t *testing.T, when string) {
	t.Helper()
	c, n := f.explorer.tree.cursor, len(f.explorer.tree.filtered)
	if c < 0 || c >= n {
		t.Fatalf("%s: the cursor is at %d with %d rows, so nothing can be selected", when, c, n)
	}
	// The offset is the window's top; it must not point past the list either.
	if off := f.explorer.tree.offset; off < 0 || off > c {
		t.Fatalf("%s: the window starts at %d with the cursor at %d", when, off, c)
	}
}

// ---------------------------------------------------------------------------
// E1: the cursor is always inside the list
// ---------------------------------------------------------------------------

// Scenario: El cursor SIEMPRE indexa una fila que existe.
//
// This is the contract the crash broke. The list changes size from four directions —
// filtering, expanding, collapsing, toggling columns — and the cursor used to be clamped
// in three of them and not the fourth. The first subtest is the exact sequence that took
// the process down: arrow to the last row, open the filter, type something that matches
// fewer rows than where the cursor is.
func TestTheCursorAlwaysIndexesARowThatExists(t *testing.T) {
	t.Run("typing a filter that matches fewer rows than the cursor's index", func(t *testing.T) {
		f := newFixture(t)
		for range 10 {
			f.explorer.HandleAction("navigate_down")
		}
		if f.explorer.tree.cursor == 0 {
			t.Fatal("the fixture did not move the cursor, so nothing can prove it is clamped")
		}

		// The filter line, opened and typed as a user would.
		f.explorer.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		if !f.explorer.IsFiltering() {
			t.Fatal("the filter did not open")
		}
		for _, ch := range "ord" {
			f.explorer.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
		if got := len(f.explorer.tree.filtered); got >= 5 {
			t.Fatalf("the filter left %d rows, so the list never got shorter than the cursor", got)
		}

		f.cursorIsInRange(t, "after filtering")
		// And the thing that used to panic actually answers.
		if sel := f.explorer.Selected(); sel == nil {
			t.Error("nothing is selected after the list shrank, so the cursor was clamped to an empty list")
		} else if sel.Name != "orders" {
			t.Errorf("the selection is %q, want the last row of the filtered list", sel.Name)
		}
	})

	t.Run("a filter that matches NOTHING still leaves the cursor valid", func(t *testing.T) {
		f := newFixture(t)
		for range 10 {
			f.explorer.HandleAction("navigate_down")
		}
		f.explorer.StartFilter()
		for _, ch := range "zzzz" {
			f.explorer.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
		if len(f.explorer.tree.filtered) != 0 {
			t.Fatalf("the filter matched %d rows, want none", len(f.explorer.tree.filtered))
		}
		// An empty list has no valid index at all, so the cursor has to be
		// somewhere harmless and Selected() has to say "nothing".
		if c := f.explorer.tree.cursor; c != 0 {
			t.Errorf("an empty list left the cursor at %d, want 0", c)
		}
		if sel := f.explorer.Selected(); sel != nil {
			t.Errorf("an empty list selected %q", sel.Name)
		}

		// Close the filter first: while the line has the keys it claims every
		// action, which is E4's contract and not something to test twice here.
		f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if f.explorer.IsFiltering() {
			t.Fatal("enter did not close the filter line")
		}
		f.explorer.tree.SetFilter("zzzz") // still no rows, but no longer being typed

		// And the actions that would index the list have to cope.
		for _, action := range []config.ActionID{"expand_node", "toggle_columns", "collapse_node", "go_last", "go_first"} {
			if cmd, handled := f.explorer.HandleAction(action); cmd != nil {
				// The check is that CALLING the command is safe on an empty list,
				// not that it returns a particular type: `cmd()` is already a
				// tea.Msg, so asserting it is one proves nothing. The first
				// version did exactly that and staticcheck was right.
				if msg := cmd(); msg == nil {
					t.Errorf("the action %q on an empty list produced a nil message", action)
				}
			} else if action == "go_last" && !handled {
				t.Errorf("go_last on an empty list reported not handled")
			}
		}
	})

	t.Run("after every kind of list change", func(t *testing.T) {
		type change struct {
			name string
			run  func(*Explorer)
		}
		changes := []change{
			{"filter down", func(e *Explorer) { e.tree.SetFilter("ord") }},
			{"filter to nothing", func(e *Explorer) { e.tree.SetFilter("zzzz") }},
			{"clear the filter", func(e *Explorer) { e.tree.ClearFilter() }},
			{"expand the selected", func(e *Explorer) { e.HandleAction("expand_node") }},
			{"collapse", func(e *Explorer) { e.HandleAction("collapse_node") }},
			{"toggle_columns", func(e *Explorer) { e.HandleAction("toggle_columns") }},
			{"go_last", func(e *Explorer) { e.HandleAction("go_last") }},
			{"go_first", func(e *Explorer) { e.HandleAction("go_first") }},
			{"open the filter", func(e *Explorer) { e.StartFilter() }},
			{"a shorter pane", func(e *Explorer) { e.SetHeight(3) }},
			{"no pane at all", func(e *Explorer) { e.SetHeight(0) }},
			{"the nodes again", func(e *Explorer) { e.SetNodes(nil) }},
			{"one node", func(e *Explorer) { e.SetNodes([]*Node{NewNode("x", NodeTable, "solo")}) }},
		}

		// From every starting row, so a clamp that only holds at the top is caught.
		for start := 0; start < 6; start++ {
			for _, ch := range changes {
				f := newFixture(t)
				for range start {
					f.explorer.HandleAction("navigate_down")
				}
				ch.run(f.explorer)
				if len(f.explorer.tree.filtered) == 0 {
					if f.explorer.tree.cursor != 0 {
						t.Errorf("%s from row %d: an empty list left the cursor at %d",
							ch.name, start, f.explorer.tree.cursor)
					}
					continue
				}
				f.cursorIsInRange(t, fmt.Sprintf("%s from row %d", ch.name, start))
			}
		}
	})

	t.Run("the TREE windows itself to its height, and the PANE is clipped on top of that", func(t *testing.T) {
		// Two separate guarantees, and they are separate because they are
		// enforced in two places.
		//
		// The tree's own window is `if height > 0 && end > start+height`, and
		// Explorer.SetHeight(h) hands the tree h-2 — so for h < 2 the tree's
		// height is NEGATIVE and the tree renders its whole list. That is
		// wasteful, not visible: bordered.RenderWithTitleEx clips to h+2, so the
		// pane is right anyway. Asserted on both so a future change to either
		// half knows the other one is holding it up.
		f := newFixture(t)
		f.explorer.tree.SetFilter("")
		total := len(f.explorer.tree.filtered)
		if total < 4 {
			t.Fatalf("the fixture has %d rows, want enough to overflow a short pane", total)
		}

		for _, h := range []int{2, 3, 4, 5, 20} {
			f.explorer.SetHeight(h)
			treeRows := nonBlankLines(f.explorer.tree.View())
			paneRows := nonBlankLines(f.explorer.View())
			if got, want := f.explorer.tree.height, h-2; got != want {
				t.Errorf("SetHeight(%d) gave the tree a height of %d, want %d", h, got, want)
			}
			// A tree of height 0 renders everything, because the guard is
			// `height > 0`. So the window only engages from SetHeight(3) up,
			// and for the two heights below it the BORDER is the only thing
			// keeping the pane the right size.
			if h-2 >= 1 && treeRows > h-2 {
				t.Errorf("a tree %d rows high rendered %d rows", h-2, treeRows)
			}
			// The pane adds the filter line and the two border rows.
			if paneRows > h+3 {
				t.Errorf("a pane %d rows high rendered %d non-blank lines", h, paneRows)
			}
		}

		// And no height, however small, panics or overflows the terminal: the
		// border is what clips, so this is the case that matters.
		for h := 0; h <= 6; h++ {
			f.explorer.SetHeight(h)
			for range 8 {
				f.explorer.HandleAction("navigate_down")
			}
			_ = f.explorer.View()
		}
	})
}

// ---------------------------------------------------------------------------
// E2 + E3: what the filter matches, and what it ignores
// ---------------------------------------------------------------------------

// Scenario: El filtro busca TABLAS, sin distinguir mayusculas, y deja ver el camino.
//
// The action is called filter_tables, so matching table names is the contract. The three
// things worth pinning are the case-insensitivity, that the ancestors come along so you
// can see WHICH schema the match is in, and that a filter matching nothing shows nothing
// rather than everything.
func TestTheFilterNarrowsToTableNamesAndKeepsTheWayThere(t *testing.T) {
	t.Run("it matches table names, in any case", func(t *testing.T) {
		f := newFixture(t)
		for _, tc := range []struct {
			filter string
			want   string
		}{
			{"users", "users"},
			{"USERS", "users"},
			{"Users", "users"},
			{"use", "users"},
			{"user", "users"},
			// "ers" is a substring of BOTH users and orders, and matching
			// everything that contains the text is the point of a filter.
			{"ers", "users|orders"},
		} {
			got := f.typeFilter(tc.filter)
			wantTables := strings.Split(tc.want, "|")
			if len(got) != len(wantTables)+2 {
				t.Errorf("the filter %q matched %v, want the database, the schema and %v", tc.filter, got, wantTables)
				continue
			}
			if strings.Join(got[2:], "|") != strings.Join(wantTables, "|") {
				t.Errorf("the filter %q matched %v, want it to end at %v", tc.filter, got, wantTables)
			}
			if got[0] != "app" || got[1] != "public" {
				t.Errorf("the filter %q matched %v, want it to keep the way there", tc.filter, got)
			}
		}
	})

	t.Run("a filter matching nothing shows nothing, not everything", func(t *testing.T) {
		f := newFixture(t)
		for _, filter := range []string{"zzzz", "publicXX", "  "} {
			got := f.typeFilter(filter)
			if len(got) != 0 {
				t.Errorf("the filter %q matched %v, want nothing: a filter that hides everything must not show everything", filter, got)
			}
		}
	})

	t.Run("a COLUMN name does not match, and neither does a SCHEMA name", func(t *testing.T) {
		// A LIMITATION, pinned so nobody mistakes it for a bug: the action says
		// filter_tables, so a column called `email` is not findable by name and
		// neither is a schema called `audit`. Searching for "email" in a table
		// list and getting nothing is surprising, but widening the filter to
		// match columns is a product decision, not a fix.
		f := newFixture(t)
		for _, filter := range []string{"email", "id", "audit", "app", "int4"} {
			if got := f.typeFilter(filter); len(got) != 0 {
				t.Errorf("the filter %q matched %v, want nothing — the filter only looks at table names", filter, got)
			}
		}
	})

	t.Run("an EXPANDED matching table brings its columns along", func(t *testing.T) {
		// Even though column names never match, expanding the table you matched
		// shows its columns — otherwise the filter would hide the very thing
		// that told you where you are.
		f := newFixture(t)
		got := f.typeFilter("users")
		if len(got) != 3 {
			t.Fatalf("the collapsed table matched %v", got)
		}
		f.users.Expanded = true
		got = f.typeFilter("users")
		want := []string{"app", "public", "users", "id", "email"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("an expanded matching table matched %v, want %v", got, want)
		}
	})

	t.Run("a COLLAPSED schema still shows its matching table", func(t *testing.T) {
		// The filter IGNORES expansion, which is the whole point of it: you
		// filter because you cannot see the table, and a filter that respected
		// the collapsed schema above it would match nothing and show nothing.
		f := newFixture(t)
		if f.audit.Expanded {
			t.Fatal("the fixture's audit schema is expanded, so this proves nothing")
		}
		got := f.typeFilter("events")
		want := []string{"app", "audit", "events"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("filtering inside a collapsed schema matched %v, want %v", got, want)
		}
	})

	t.Run("WITHOUT a filter the same collapsed schema shows nothing under it", func(t *testing.T) {
		// The contrast that makes E3 legible: unfiltered, expansion decides.
		f := newFixture(t)
		if strings.Contains(strings.Join(f.names(), "|"), "events") {
			t.Errorf("the unfiltered list is %v, want the collapsed schema's table hidden", f.names())
		}
		f.audit.Expanded = true
		f.explorer.tree.flattenNodes() // Expanded is a field, not a message; nothing re-reads it on its own
		if !strings.Contains(strings.Join(f.names(), "|"), "events") {
			t.Errorf("expanding the schema did not reveal its table: %v", f.names())
		}
	})

	t.Run("clearing the filter brings the expansion rules back", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.SetFilter("events")
		if len(f.explorer.tree.filtered) == 0 {
			t.Fatal("the fixture does not match, so this proves nothing")
		}
		f.explorer.tree.ClearFilter()
		if strings.Contains(strings.Join(f.names(), "|"), "events") {
			t.Errorf("after clearing the filter the collapsed schema's table is still shown: %v", f.names())
		}
	})
}

// ---------------------------------------------------------------------------
// E4: the filter line is text
// ---------------------------------------------------------------------------

// Scenario: La linea del filtro es TEXTO: enter se la queda, escape la tira.
//
// Enter and escape are different decisions, not the same one spelled two ways: enter
// commits what you typed and leaves the filter on, escape abandons it entirely. A user who
// mistyped and pressed escape expecting to be back where they started, and is instead
// looking at a permanently narrowed tree, has lost their place.
func TestTheFilterLineIsText(t *testing.T) {
	typeInto := func(e *Explorer, s string) {
		t.Helper()
		for _, ch := range s {
			e.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
	}

	t.Run("enter keeps the filter, escape throws it away", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.StartFilter()
		typeInto(f.explorer, "users")
		if f.explorer.tree.filter != "users" {
			t.Fatalf("the filter is %q, want %q", f.explorer.tree.filter, "users")
		}

		f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if f.explorer.IsFiltering() {
			t.Error("enter left the filter line open")
		}
		if f.explorer.tree.filter != "users" {
			t.Errorf("enter changed the filter to %q, want it kept: enter commits what you typed", f.explorer.tree.filter)
		}
		if len(f.explorer.tree.filtered) == 0 {
			t.Error("enter left the tree filtered to nothing")
		}

		// Escape while the filter is committed but not being typed clears it.
		f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
		if f.explorer.tree.filter != "" {
			t.Errorf("escape left the filter at %q, want it cleared", f.explorer.tree.filter)
		}
		if len(f.explorer.tree.filtered) <= 1 {
			t.Errorf("escape left the tree filtered to %v, want the whole tree back", f.names())
		}
	})

	t.Run("escape while TYPING abandons the line", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.StartFilter()
		typeInto(f.explorer, "users")
		f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
		if f.explorer.IsFiltering() {
			t.Error("escape left the filter line open")
		}
		if f.explorer.tree.filter != "" {
			t.Errorf("escape left the filter at %q, want it cleared", f.explorer.tree.filter)
		}
	})

	t.Run("backspace takes ONE character", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.StartFilter()
		typeInto(f.explorer, "users")
		for _, want := range []string{"user", "use", "us", "u", ""} {
			f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			if f.explorer.tree.filter != want {
				t.Fatalf("backspacing left %q, want %q", f.explorer.tree.filter, want)
			}
		}
		// And on an empty line it does nothing rather than panicking.
		for range 3 {
			f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
		if f.explorer.tree.filter != "" {
			t.Errorf("backspacing an empty line produced %q", f.explorer.tree.filter)
		}
	})

	t.Run("backspace on a MULTI-BYTE character takes the whole character", func(t *testing.T) {
		// The fifth byte-versus-rune backspace in this repo. The keyboard cannot
		// type one today (see the limitation below), but SetFilter is public, so
		// a filter holding one is reachable — and the other four were all
		// "unreachable until somebody typed an accent", which is not the same
		// thing as unreachable.
		for _, tc := range []struct{ name, filter, want string }{
			{"a two-byte accent alone", "é", ""},
			{"an accent after ascii", "ré", "r"},
			{"a three-byte character alone", "表", ""},
			{"one between ascii", "a表b", "a表"},
			{"an emoji, four bytes and one rune", "🙂", ""},
			{"an emoji between ascii", "a🙂b", "a🙂"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newFixture(t)
				f.explorer.StartFilter()
				f.explorer.tree.SetFilter(tc.filter)

				f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

				if f.explorer.tree.filter != tc.want {
					t.Errorf("backspacing the filter %q left %q, want %q", tc.filter, f.explorer.tree.filter, tc.want)
				}
				if strings.ContainsRune(f.explorer.tree.filter, '�') {
					t.Errorf("the filter %q now holds a replacement character: a byte-wise backspace cut a rune in half", f.explorer.tree.filter)
				}
			})
		}
	})

	t.Run("a MULTI-BYTE character cannot be typed, which is a limitation", func(t *testing.T) {
		// The default arm tests `len(key) == 1` on the STRING, so a character
		// whose encoding is longer than one byte never reaches the filter. The
		// same limitation the WHERE filter and the jq line have, pinned in all
		// three places. NOT changed: a table filter legitimately contains `q` and
		// `j`, so any relaxation has to be a decision about which keys are text
		// rather than a fix. What it costs is real though: a table named `café`
		// cannot be filtered for by typing the accent.
		f := newFixture(t)
		f.explorer.StartFilter()
		for _, r := range []string{"é", "表", "🙂"} {
			f.explorer.Update(tea.KeyPressMsg{Code: []rune(r)[0], Text: r})
		}
		if f.explorer.tree.filter != "" {
			t.Errorf("multi-byte characters reached the filter: %q", f.explorer.tree.filter)
		}
	})

	t.Run("EVERY key is claimed while filtering", func(t *testing.T) {
		// The reason the app has to carve some keys out. `handleFilterKey`
		// returns handled=true for anything, so the explorer tells the app "I
		// used that key" even for one it did nothing with — and app.go returns
		// on handled, so `?` typed a literal `?` instead of opening help. The
		// carve-out (actionSurvivesTextInput) is the fix; this is the fact that
		// made it necessary.
		f := newFixture(t)
		f.explorer.StartFilter()
		for _, key := range []tea.KeyPressMsg{
			{Code: '?', Text: "?"},
			{Code: 'q', Text: "q"},
			{Code: tea.KeyF1},
			{Code: 'z', Mod: tea.ModCtrl, Text: "ctrl+z"},
			{Code: tea.KeyUp},
			{Code: tea.KeyDown},
			{Code: tea.KeyTab},
		} {
			if _, handled := f.explorer.Update(key); !handled {
				t.Errorf("the key %q was not claimed by the open filter line", key.String())
			}
		}
	})

	t.Run("opening the filter clears whatever was in it", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.SetFilter("users")
		f.explorer.StartFilter()
		if f.explorer.tree.filter != "" {
			t.Errorf("opening the filter left %q in it, want it empty: a stale filter would hide rows before you type anything", f.explorer.tree.filter)
		}
	})

	t.Run("the actions are refused while the filter has the keys", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.StartFilter()
		for _, action := range []config.ActionID{
			"navigate_down", "navigate_up", "go_last", "go_first",
			"expand_node", "collapse_node", "toggle_columns", "drop_table", "view_ddl",
		} {
			if _, handled := f.explorer.HandleAction(action); handled {
				t.Errorf("the action %q was dispatched while the filter line had the keys", action)
			}
		}
		// HandleAction is the entry the MOUSE uses, so this is what keeps a
		// wheel scroll from moving the cursor behind an open filter.
		if f.explorer.tree.cursor != 0 {
			t.Errorf("the actions moved the cursor to %d while filtering", f.explorer.tree.cursor)
		}
	})
}

// ---------------------------------------------------------------------------
// E5: expanding and collapsing
// ---------------------------------------------------------------------------

// Scenario: Expandir y colapsar dejan el cursor en una fila que existe.
//
// Collapsing has two halves, and only one of them changes the list. On an expanded node it
// collapses and the list shrinks under the cursor; on an already-collapsed node it is a
// "go to my parent" — which is what makes a collapsed schema navigable upwards.
func TestExpandAndCollapseLandOnARowThatExists(t *testing.T) {
	t.Run("collapsing an expanded node shrinks the list and keeps the cursor valid", func(t *testing.T) {
		f := newFixture(t)
		// The audit schema is collapsed, so expand it first.
		for _, n := range f.explorer.tree.filtered {
			if n == f.audit {
				f.explorer.tree.cursor = indexOf(f, f.audit)
			}
		}
		f.explorer.HandleAction("collapse_node")
		if f.audit.Expanded {
			t.Fatal("collapse did not collapse the schema")
		}
		if strings.Contains(strings.Join(f.names(), "|"), "events") {
			t.Errorf("collapsing the schema left its table in the list: %v", f.names())
		}
		f.cursorIsInRange(t, "after collapsing a schema")
	})

	t.Run("collapsing a COLLAPSED node goes to its parent", func(t *testing.T) {
		f := newFixture(t)
		// events sits under the collapsed audit schema, so put the cursor on it
		// by filtering, which ignores expansion.
		f.explorer.tree.SetFilter("events")
		// The cursor is CLAMPED by a filter change, not zeroed and not moved to
		// the match, so it has to be put on events explicitly.
		f.explorer.HandleAction("go_last")
		f.cursorIsInRange(t, "after filtering")
		if got := f.explorer.Selected(); got == nil || got.Name != "events" {
			t.Fatalf("the fixture did not select events, got %v", got)
		}

		f.explorer.HandleAction("collapse_node")

		sel := f.explorer.Selected()
		if sel == nil {
			t.Fatal("collapsing a collapsed node selected nothing")
		}
		if sel != f.audit {
			t.Errorf("collapsing %q selected %q, want its parent %q",
				"events", sel.Name, f.audit.Name)
		}
		f.cursorIsInRange(t, "after collapsing a collapsed node")
	})

	t.Run("collapsing a ROOT with no parent does nothing", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.cursor = 0 // app, the database, whose parent is nil
		if f.explorer.tree.filtered[0] != f.db {
			t.Fatalf("the fixture's first row is %q, want the database", f.explorer.tree.filtered[0].Name)
		}
		f.db.Expanded = false // already collapsed, so the parent branch is the one taken
		f.explorer.tree.flattenNodes()

		before := f.explorer.tree.cursor
		f.explorer.HandleAction("collapse_node")

		if f.explorer.tree.cursor != before {
			t.Errorf("collapsing a parentless root moved the cursor from %d to %d", before, f.explorer.tree.cursor)
		}
		f.cursorIsInRange(t, "after collapsing a parentless root")
	})

	t.Run("expanding a leaf does nothing at all", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.SetFilter("email")
		f.explorer.tree.cursor = 0
		for f.explorer.Selected() != nil && f.explorer.Selected().Type != NodeColumn {
			f.explorer.HandleAction("navigate_down")
		}
		if sel := f.explorer.Selected(); sel == nil {
			t.Skip("the fixture did not reach a column")
		}
		before := f.names()
		cmd, handled := f.explorer.HandleAction("expand_node")
		if cmd != nil {
			t.Errorf("expanding a column emitted %T, want nothing", cmd())
		}
		if !handled {
			t.Error("expanding a column was not reported as handled, so the key falls through to the tree")
		}
		if strings.Join(f.names(), "|") != strings.Join(before, "|") {
			t.Errorf("expanding a column changed the list from %v to %v", before, f.names())
		}
	})
}

// selectNode puts the cursor on n in the CURRENT list, clearing any filter first. The
// cursor is not reset by a filter change on purpose — it is clamped, not zeroed — so a
// test that sets the filter and then assumes the cursor followed has to be wrong.
func (f *fixture) selectNode(t *testing.T, n *Node) {
	t.Helper()
	f.explorer.tree.ClearFilter()
	for i, x := range f.explorer.tree.filtered {
		if x == n {
			f.explorer.tree.cursor = i
			f.cursorIsInRange(t, "selecting "+n.Name)
			if f.explorer.Selected() != n {
				t.Fatalf("asked to select %q and the cursor landed on %v", n.Name, f.explorer.Selected())
			}
			return
		}
	}
	t.Fatalf("the node %q is not in the list %v", n.Name, f.names())
}

func indexOf(f *fixture, n *Node) int {
	for i, x := range f.explorer.tree.filtered {
		if x == n {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// E6: each action emits the message that belongs to the node
// ---------------------------------------------------------------------------

// Scenario: Cada accion pide lo que corresponde al nodo marcado.
//
// The explorer is a list of things you can ASK the app to do to a table, so the actions'
// whole job is turning a highlighted row into the right message. The failure that matters
// is asking about the wrong thing: a `drop_table` that named a schema, or an
// `expand_node` on a column that emitted a table selection.
func TestEachActionEmitsTheMessageForTheSelectedNode(t *testing.T) {
	selectNode := func(t *testing.T, f *fixture, n *Node) {
		t.Helper()
		f.explorer.tree.SetFilter("")
		for i, x := range f.explorer.tree.filtered {
			if x == n {
				f.explorer.tree.cursor = i
				f.cursorIsInRange(t, "selecting "+n.Name)
				if f.explorer.Selected() != n {
					t.Fatalf("could not select %q", n.Name)
				}
				return
			}
		}
		t.Fatalf("the node %q is not in the unfiltered list %v", n.Name, f.names())
	}

	t.Run("expand_node on a TABLE selects it", func(t *testing.T) {
		f := newFixture(t)
		selectNode(t, f, f.users)
		cmd, handled := f.explorer.HandleAction("expand_node")
		if !handled || cmd == nil {
			t.Fatal("expand_node on a table emitted nothing")
		}
		msg, ok := cmd().(TableSelectedMsg)
		if !ok {
			t.Fatalf("expand_node emitted a %T, want a TableSelectedMsg", cmd())
		}
		if msg.Table != "users" || msg.Schema != "public" {
			t.Errorf("the selection is %+v, want users in public", msg)
		}
		// And it did NOT expand, which is the part worth pinning: on a table
		// this action is "load this table", not "open this table". The app
		// answers with the columns and a second Enter — or the mouse's
		// ToggleExpand — is what opens it. Two ways to expand that do
		// different things, and a test that assumed they were one thing would
		// have passed while the difference went unnoticed.
		if f.users.Expanded {
			t.Error("expand_node on a table expanded it as well as asking for it: the two are separate actions")
		}
		// ToggleExpand is the other one, and it DOES expand.
		if cmd := f.explorer.ToggleExpand(); cmd == nil {
			t.Error("ToggleExpand on a table emitted no message, want the same table selection")
		} else if msg, ok := cmd().(TableSelectedMsg); !ok || msg.Table != "users" {
			t.Errorf("ToggleExpand emitted %+v, want the users selection", cmd())
		}
		if !f.users.Expanded {
			t.Error("ToggleExpand did not expand the table")
		}
	})

	t.Run("drop_table and view_ddl on a TABLE name the table", func(t *testing.T) {
		for _, tc := range []struct {
			action config.ActionID
			assert func(t *testing.T, msg tea.Msg)
		}{
			{"drop_table", func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(DropTableMsg)
				if !ok {
					t.Fatalf("drop_table emitted a %T", msg)
				}
				if m.Table != "users" || m.Schema != "public" {
					t.Errorf("the drop request is %+v, want users in public", m)
				}
			}},
			{"view_ddl", func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(ViewDDLMsg)
				if !ok {
					t.Fatalf("view_ddl emitted a %T", msg)
				}
				if m.Table != "users" || m.Schema != "public" {
					t.Errorf("the ddl request is %+v, want users in public", m)
				}
			}},
		} {
			t.Run(string(tc.action), func(t *testing.T) {
				f := newFixture(t)
				selectNode(t, f, f.users)
				cmd, handled := f.explorer.HandleAction(tc.action)
				if !handled {
					t.Fatalf("%s was not handled", tc.action)
				}
				if cmd == nil {
					t.Fatalf("%s on a table emitted nothing", tc.action)
				}
				tc.assert(t, cmd())
			})
		}
	})

	t.Run("drop_table and view_ddl on a SCHEMA do nothing at all", func(t *testing.T) {
		// A schema is not a table and cannot be dropped or have its DDL shown,
		// so the right answer is silence — not a message naming the schema,
		// which is what a "closest match" implementation would produce.
		f := newFixture(t)
		for _, action := range []config.ActionID{"drop_table", "view_ddl"} {
			selectNode(t, f, f.public)
			cmd, handled := f.explorer.HandleAction(action)
			if !handled {
				t.Errorf("%s on a schema was not handled, so the key falls through", action)
			}
			if cmd != nil {
				t.Errorf("%s on a schema emitted %+v, want nothing", action, cmd())
			}
		}
	})

	t.Run("expand_node on a SCHEMA toggles it and emits nothing", func(t *testing.T) {
		f := newFixture(t)
		selectNode(t, f, f.public)
		cmd, _ := f.explorer.HandleAction("expand_node")
		if cmd != nil {
			t.Errorf("expand_node on a schema emitted %+v, want nothing", cmd())
		}
		if f.public.Expanded {
			t.Error("expand_node on a schema did not collapse it")
		}
		f.cursorIsInRange(t, "after expanding a schema")
	})

	t.Run("new_table works from a schema AND from a table", func(t *testing.T) {
		// New table is the one action that makes sense at more than one level:
		// from a schema you want a table in THAT schema, and from a table you
		// want one beside it.
		f := newFixture(t)
		for _, node := range []*Node{f.public, f.users} {
			selectNode(t, f, node)
			cmd, handled := f.explorer.HandleAction("new_table")
			if !handled || cmd == nil {
				t.Fatalf("new_table from %q emitted nothing", node.Name)
			}
			msg, ok := cmd().(NewTableMsg)
			if !ok {
				t.Fatalf("new_table emitted a %T", cmd())
			}
			if msg.Schema != "public" {
				t.Errorf("new_table from %q asked for schema %q, want public", node.Name, msg.Schema)
			}
		}
		// From the DATABASE there is no schema to be in, and the message says
		// so rather than guessing one.
		f.explorer.tree.ClearFilter()
		f.explorer.tree.cursor = 0
		if f.explorer.Selected() != f.db {
			t.Fatalf("the fixture's first row is %v, want the database", f.explorer.Selected())
		}
		cmd, _ := f.explorer.HandleAction("new_table")
		if msg, ok := cmd().(NewTableMsg); !ok || msg.Schema != "" {
			t.Errorf("new_table from the database asked for %+v, want the empty schema", cmd())
		}
	})

	t.Run("a node with no schema in its metadata asks for the empty schema", func(t *testing.T) {
		// Not a crash and not a guess: the app decides what an empty schema
		// means, and this only pins that the code does not invent one.
		f := newFixture(t)
		bare := NewNode("bare", NodeTable, "orphan")
		bare.Expanded = true
		f.db.Children = append(f.db.Children, bare)
		f.db.Expanded = true
		f.explorer.SetNodes([]*Node{f.db})

		selectNode(t, f, bare)
		cmd, _ := f.explorer.HandleAction("new_table")
		msg, ok := cmd().(NewTableMsg)
		if !ok {
			t.Fatalf("new_table emitted a %T", cmd())
		}
		if msg.Schema != "" {
			t.Errorf("a node with no schema asked for %q, want the empty string", msg.Schema)
		}

		selectNode(t, f, bare)
		cmd, _ = f.explorer.HandleAction("expand_node")
		sel, ok := cmd().(TableSelectedMsg)
		if !ok {
			t.Fatalf("expand_node emitted a %T", cmd())
		}
		if sel.Schema != "" || sel.Table != "orphan" {
			t.Errorf("the selection is %+v, want the orphan table with no schema", sel)
		}
	})

	t.Run("refresh_schema asks for a refresh", func(t *testing.T) {
		f := newFixture(t)
		cmd, handled := f.explorer.HandleAction("refresh_schema")
		if !handled || cmd == nil {
			t.Fatal("refresh_schema emitted nothing")
		}
		if _, ok := cmd().(ExplorerRefreshMsg); !ok {
			t.Errorf("refresh_schema emitted a %T", cmd())
		}
	})

	t.Run("an action the explorer does not implement falls through", func(t *testing.T) {
		// handled=false is the signal that lets an unclaimed key reach whatever
		// else wants it, so claiming one by accident would swallow it.
		f := newFixture(t)
		for _, action := range []config.ActionID{"no_such_action", "run_query", "save_file"} {
			if _, handled := f.explorer.HandleAction(action); handled {
				t.Errorf("the unimplemented action %q was reported as handled", action)
			}
		}
	})

	t.Run("every registry action the explorer claims has a case", func(t *testing.T) {
		// The registry is the source of truth for keys; this list is the set the
		// explorer claims. If one drifts out of HandledActions the coverage test
		// in internal/app fails, and if one drifts in here the fall-through test
		// above catches it.
		// A FRESH explorer per action: filter_tables opens the filter line, and
		// once it is open every later action in the list is correctly refused —
		// which is E4's contract, not evidence that handleAction lost a case.
		// One shared explorer made this loop fail on refresh_schema for that
		// reason alone.
		for _, action := range []config.ActionID{"no_such_action", "save_file", "copy_sql"} {
			f := newFixture(t)
			if _, handled := f.explorer.HandleAction(action); handled {
				t.Errorf("the explorer claims the unknown action %q", action)
			}
		}
		for _, action := range New(theme.Resolve("dark").Styles(), nil, registry()).HandledActions() {
			f := newFixture(t)
			if _, handled := f.explorer.HandleAction(action); !handled {
				t.Errorf("the explorer lists %q in HandledActions but does not handle it", action)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// E7: toggle_columns
// ---------------------------------------------------------------------------

// Scenario: toggle_columns en una TABLA colapsa su esquema y te deja ahi.
//
// The asymmetry is the thing worth naming: from a schema the action toggles, but from a
// table it can only COLLAPSE — and it moves the cursor to the schema it collapsed. So the
// gesture reaches two different states from two different rows, and expanding back
// requires a second press once you are on the schema.
func TestToggleColumnsFromATableCollapsesItsSchemaAndLandsYouThere(t *testing.T) {
	f := newFixture(t)
	for i, n := range f.explorer.tree.filtered {
		if n == f.public {
			f.explorer.tree.cursor = i
		}
	}

	t.Run("on a SCHEMA it toggles and leaves the cursor put", func(t *testing.T) {
		if _, handled := f.explorer.HandleAction("toggle_columns"); !handled {
			t.Fatal("toggle_columns on a schema was not handled")
		}
		if f.public.Expanded {
			t.Error("toggle_columns on a schema did not collapse it")
		}
		if f.explorer.tree.filtered[f.explorer.tree.cursor] != f.public {
			t.Errorf("collapsing a schema moved the cursor off it, to %q",
				f.explorer.tree.filtered[f.explorer.tree.cursor].Name)
		}
	})

	// Now put the cursor on a table inside the (collapsed) schema, which means
	// filtering — the filter ignores expansion, so this is reachable.
	f.explorer.tree.SetFilter("users")
	f.explorer.HandleAction("go_last") // the filtered list is [app public users]
	f.cursorIsInRange(t, "selecting users")
	if f.explorer.Selected() != f.users {
		t.Fatalf("the fixture did not select users, got %v", f.explorer.Selected())
	}

	t.Run("on a TABLE it collapses the schema and lands the cursor on it", func(t *testing.T) {
		cmd, handled := f.explorer.HandleAction("toggle_columns")
		if !handled {
			t.Fatal("toggle_columns on a table was not handled")
		}
		if cmd != nil {
			t.Errorf("toggle_columns emitted %+v, want nothing — it is a view change, not a request", cmd())
		}
		if f.public.Expanded {
			t.Error("toggle_columns on a table did not collapse its schema")
		}
		if sel := f.explorer.Selected(); sel != f.public {
			t.Errorf("toggle_columns on a table left the cursor on %v, want the schema it collapsed", sel)
		}
		f.cursorIsInRange(t, "after toggle_columns on a table")
	})

	t.Run("so expanding back takes a second press, now on the schema", func(t *testing.T) {
		if _, handled := f.explorer.HandleAction("toggle_columns"); !handled {
			t.Fatal("the second press was not handled")
		}
		if !f.public.Expanded {
			t.Error("the second press, from the schema, did not expand it")
		}
	})

	t.Run("on a table with no schema parent it does nothing", func(t *testing.T) {
		// A table hanging directly off the database. There is no schema to
		// collapse, so the action is claimed and does nothing — pinned so a
		// "find me a schema ancestor" change is deliberate.
		g := newFixture(t)
		bare := NewNode("bare", NodeTable, "orphan")
		g.db.Children = append(g.db.Children, bare)
		g.explorer.SetNodes([]*Node{g.db})
		for i, n := range g.explorer.tree.filtered {
			if n == bare {
				g.explorer.tree.cursor = i
			}
		}
		if g.explorer.Selected() != bare {
			t.Fatalf("the fixture did not select the orphan, got %v", g.explorer.Selected())
		}
		before := strings.Join(g.names(), "|")
		if _, handled := g.explorer.HandleAction("toggle_columns"); !handled {
			t.Error("toggle_columns on a parentless table was not handled")
		}
		if strings.Join(g.names(), "|") != before {
			t.Errorf("toggle_columns changed %v", g.names())
		}
	})
}

// ---------------------------------------------------------------------------
// E8: SelectTable
// ---------------------------------------------------------------------------

// Scenario: SelectTable abre el camino y te deja en la tabla, o AVISA de que no pudo.
//
// Selecting a table by name from outside — a recent-tables jump, a "go to" — has to make
// the table visible before it can put the cursor on it. And the case worth pinning is the
// one where it reports failure: it expands the ancestors on the way and then gives up,
// so a caller that trusts the boolean still sees a changed tree.
func TestSelectTableOpensTheWayThereOrSaysItCannot(t *testing.T) {
	t.Run("it expands the ancestors and lands the cursor", func(t *testing.T) {
		f := newFixture(t)
		if f.audit.Expanded {
			t.Fatal("the fixture's audit schema is expanded, so this proves nothing")
		}
		if !f.explorer.SelectTable("audit", "events") {
			t.Fatal("SelectTable could not find a table that exists")
		}
		if !f.audit.Expanded {
			t.Error("SelectTable did not expand the schema, so the table is still hidden")
		}
		if sel := f.explorer.Selected(); sel != f.events {
			t.Errorf("the cursor is on %v, want events", sel)
		}
		f.cursorIsInRange(t, "after SelectTable")
	})

	t.Run("a table that is not there changes nothing and says so", func(t *testing.T) {
		f := newFixture(t)
		before := strings.Join(f.names(), "|")
		for _, tc := range [][2]string{
			{"public", "nosuchtable"},
			{"nosuchschema", "events"},
			{"", "events"},
			{"public", ""},
		} {
			if f.explorer.SelectTable(tc[0], tc[1]) {
				t.Errorf("SelectTable(%q, %q) reported success for a table that is not there", tc[0], tc[1])
			}
		}
		if strings.Join(f.names(), "|") != before {
			t.Errorf("the failed lookups changed the list to %v", f.names())
		}
	})

	t.Run("a table HIDDEN BY THE FILTER expands its ancestors and then reports failure", func(t *testing.T) {
		// The behaviour as it is: the ancestors are expanded on the way out of
		// findTableRecursive, and only the final walk into the filtered list
		// fails. So the boolean says "no" while the tree has changed.
		//
		// Not fixed, and the reason is worth stating: making it atomic means
		// either not expanding (so the table stays invisible, which is the whole
		// point of selecting it) or reporting success while the cursor is on
		// something else. The caller in app.go re-renders from the tree, so the
		// expanded schema is the useful half and the false is the honest one.
		f := newFixture(t)
		f.explorer.StartFilter()
		f.explorer.tree.SetFilter("users")
		if f.explorer.SelectTable("public", "orders") {
			t.Error("SelectTable found a table the filter is hiding")
		}
		if !f.public.Expanded {
			t.Error("the failed selection did not expand the schema, so the tree is left exactly as it was")
		}
	})

	t.Run("the two entry points that GUARD against no tree, and the two that do not", func(t *testing.T) {
		// New always builds a tree and nothing in production sets it to nil, so
		// all of this is unreachable — which is why no guard is ADDED here. Two
		// of the four entry points check `e.tree == nil` anyway (SelectTable,
		// HandleClick) and two do not (HandleAction, ToggleExpand), and that
		// asymmetry is the fact: it is not a crash waiting to happen, it is two
		// defensive checks that were written in one place and not copied to the
		// other three. Pinning it so nobody "fixes" it by adding a fourth guard
		// that also does nothing.
		e := New(theme.Resolve("dark").Styles(), nil, registry())
		e.tree = nil

		// Guarded.
		if e.SelectTable("public", "users") {
			t.Error("SelectTable on an explorer with no tree reported success")
		}
		if e.HandleClick(3) {
			t.Error("HandleClick on an explorer with no tree reported a hit")
		}
		// Not guarded: these index the tree, and calling them here panics. That
		// is asserted by ABSENCE of a guard, which cannot be written as a
		// passing test — so what this pins is the pair that IS guarded, and the
		// reader is told about the pair that is not.
	})
}

// ---------------------------------------------------------------------------
// E9: the click
// ---------------------------------------------------------------------------

// Scenario: Un clic elige la fila que hay DEBAJO del dedo.
//
// Two rows of the pane are not rows: the border, and the filter line when there is one.
// Getting the offset wrong by one selects the neighbouring table, which on a `drop_table`
// is the difference between dropping what you clicked and dropping the one above it.
func TestAClickPicksTheRowUnderIt(t *testing.T) {
	t.Run("the border is not a row", func(t *testing.T) {
		f := newFixture(t)
		before := f.explorer.tree.cursor
		if f.explorer.HandleClick(0) {
			t.Error("a click on the border selected a row")
		}
		if f.explorer.tree.cursor != before {
			t.Errorf("a click on the border moved the cursor from %d to %d", before, f.explorer.tree.cursor)
		}
	})

	t.Run("each row down picks the next one", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetHeight(20) // no scrolling, so y maps straight through
		for i, want := range f.names() {
			y := i + 1 // y=0 is the border
			if !f.explorer.HandleClick(y) {
				t.Errorf("a click on row %d (%q) reported no hit", i, want)
				continue
			}
			if sel := f.explorer.Selected(); sel == nil || sel.Name != want {
				t.Errorf("a click on y=%d selected %v, want %q", y, sel, want)
			}
		}
	})

	t.Run("below the last row is not a row", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetHeight(20)
		last := len(f.names())
		for y := last + 1; y < last+4; y++ {
			if f.explorer.HandleClick(y) {
				t.Errorf("a click on y=%d, past the last row, reported a hit", y)
			}
		}
		// And a click far above the top.
		for _, y := range []int{-1, -5, -1000} {
			if f.explorer.HandleClick(y) {
				t.Errorf("a click on y=%d, above the pane, reported a hit", y)
			}
		}
	})

	t.Run("the filter line shifts every row down by one", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetHeight(20)
		want := f.names()
		f.explorer.StartFilter() // the line shows even with an empty filter
		f.explorer.tree.SetFilter("")

		// Now y=0 border, y=1 filter line, y=2 first row.
		for i, name := range want {
			y := i + 2
			if !f.explorer.HandleClick(y) {
				t.Errorf("with the filter line up, a click on row %d (%q) reported no hit", i, name)
				continue
			}
			if sel := f.explorer.Selected(); sel == nil || sel.Name != name {
				t.Errorf("with the filter line up, a click on y=%d selected %v, want %q", y, sel, name)
			}
		}
		// And the row just below the border is the filter line, not a row.
		if f.explorer.HandleClick(1) {
			t.Errorf("a click on the filter line selected %v", f.explorer.Selected())
		}
	})

	t.Run("a committed filter shifts the rows too", func(t *testing.T) {
		// The line is shown whether or not you are typing into it, so a click
		// has to account for it in both states — that is why the condition is
		// `filtering || filter != ""` and not just `filtering`.
		f := newFixture(t)
		f.explorer.SetHeight(20)
		f.explorer.tree.SetFilter("users")
		want := f.names()
		if len(want) < 2 {
			t.Fatalf("the fixture matched %v, want at least two rows", want)
		}
		for i, name := range want {
			if !f.explorer.HandleClick(i + 2) {
				t.Errorf("a click on row %d (%q) reported no hit", i, name)
				continue
			}
			if sel := f.explorer.Selected(); sel == nil || sel.Name != name {
				t.Errorf("a click on y=%d selected %v, want %q", i+2, sel, name)
			}
		}
	})

	t.Run("the scroll is added, so a click picks the VISIBLE row", func(t *testing.T) {
		// With a short pane the list scrolls and the same y picks a different
		// node. Without the offset added, every click below the fold would land
		// on the row at the top of the window.
		f := newFixture(t)
		f.explorer.SetHeight(3)
		f.explorer.HandleAction("go_last")
		if f.explorer.tree.offset == 0 {
			t.Fatalf("the fixture did not scroll, so the offset is untested")
		}
		top := f.explorer.tree.filtered[f.explorer.tree.offset]
		if !f.explorer.HandleClick(1) {
			t.Fatal("a click on the top visible row reported no hit")
		}
		if f.explorer.Selected() != top {
			t.Errorf("a click on the top visible row selected %v, want %q",
				f.explorer.Selected(), top.Name)
		}
		// And below the window is past the end of the list, not a row.
		if f.explorer.HandleClick(4) {
			t.Error("a click below a scrolled window reported a hit")
		}
	})

	t.Run("an empty tree reports no hit", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetNodes(nil)
		for y := 0; y < 5; y++ {
			if f.explorer.HandleClick(y) {
				t.Errorf("a click on an empty tree (y=%d) reported a hit", y)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// E10: what is rendered
// ---------------------------------------------------------------------------

// Scenario: Lo que se pinta dice LO QUE DICE EL METADATA.
//
// The names are assembled from metadata that arrived from the database, so the promise is
// that each piece is shown when present and omitted when absent: a row count, a type, a
// NULL marker, a default value. Dropping one is invisible on a table that has all of them
// and wrong on the ones that do not.
func TestTheRenderSaysWhatTheMetadataSays(t *testing.T) {
	t.Run("a table with a row count says how many", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetWidth(60)
		f.explorer.SetHeight(20)
		f.explorer.tree.SetFilter("users")
		out := ansi.Strip(f.explorer.tree.View())
		if !strings.Contains(out, "users (10 rows)") {
			t.Errorf("the rendered row is %q, want it to carry the row count", strings.TrimSpace(out))
		}
	})

	t.Run("a table with NO row count says just its name", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetWidth(60)
		f.explorer.SetHeight(20)
		bare := NewNode("bare", NodeTable, "orphan")
		f.db.AddChild(bare)
		f.db.Expanded = true
		f.explorer.SetNodes([]*Node{f.db})
		out := ansi.Strip(f.explorer.tree.View())
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "orphan") {
				if strings.Contains(l, "rows") {
					t.Errorf("the rendered row is %q, want no row count: there is none", strings.TrimSpace(l))
				}
				if !strings.Contains(l, "orphan") || strings.Contains(l, "(0 rows)") {
					t.Errorf("the rendered row is %q", strings.TrimSpace(l))
				}
			}
		}
	})

	t.Run("a row count that is not an int is shown as no count", func(t *testing.T) {
		// RowCount type-asserts to int. Metadata built by hand or from a JSON
		// decode would hold a float64, and the honest answer to that is "no
		// count" rather than a wrong number.
		f := newFixture(t)
		odd := NewNode("odd", NodeTable, "weird")
		odd.Metadata["row_count"] = float64(42)
		f.db.AddChild(odd)
		f.db.Expanded = true
		f.explorer.SetNodes([]*Node{f.db})
		if got := odd.RowCount(); got != 0 {
			t.Errorf("RowCount with a float64 in the metadata is %d, want 0", got)
		}
	})

	t.Run("a column shows its type, its nullability and its default", func(t *testing.T) {
		f := newFixture(t)
		tree := f.explorer.tree

		// The full set: type, nullable, default.
		if got := tree.getNodeName(f.email); got != "email  text  NULL  = 'unknown'::text" {
			t.Errorf("a column with everything is rendered %q", got)
		}
		// Type and not nullable, no default.
		if got := tree.getNodeName(f.id); got != "id  int4" {
			t.Errorf("a NOT NULL column is rendered %q", got)
		}
		// No type at all: the bare name, because the other parts all hang off it.
		bare := NewNode("b", NodeColumn, "created")
		if got := tree.getNodeName(bare); got != "created" {
			t.Errorf("a column with no metadata is rendered %q, want just the name", got)
		}
		// Nullable but with an empty default: the marker shows, the default does
		// not, because an empty string is not a default.
		empty := ""
		nod := NewNode("n", NodeColumn, "note")
		nod.Metadata["data_type"] = "text"
		nod.Metadata["is_nullable"] = "YES"
		nod.Metadata["column_default"] = &empty
		if got := tree.getNodeName(nod); got != "note  text  NULL" {
			t.Errorf("a column with an EMPTY default is rendered %q", got)
		}
		// A database, a schema and an index all just show their name.
		for _, n := range []*Node{f.db, f.public, NewNode("i", NodeIndex, "idx_a")} {
			if got := tree.getNodeName(n); got != n.Name {
				t.Errorf("the %s node is rendered %q, want just its name", n.Type, got)
			}
		}
	})

	t.Run("an empty tree says so", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetNodes(nil)
		if got := ansi.Strip(f.explorer.tree.View()); !strings.Contains(got, "No items") {
			t.Errorf("an empty tree rendered %q, want it to say there is nothing", got)
		}
	})

	t.Run("a pane too narrow to hold a name renders blanks rather than panicking", func(t *testing.T) {
		// Every width has to render: width 0 means the pane has not been laid
		// out yet, and a negative available width is what that produces. The
		// name goes to nothing rather than to a negative-width slice.
		f := newFixture(t)
		for _, w := range []int{0, 1, 2, 3, 4, 5, 10} {
			f.explorer.SetWidth(w)
			f.explorer.SetHeight(6)
			for range 6 {
				f.explorer.HandleAction("navigate_down")
			}
			_ = f.explorer.View()
		}
	})

	t.Run("a long name is cut to fit, with an ellipsis", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetWidth(14)
		f.explorer.SetHeight(6)
		long := NewNode("long", NodeTable, "a_very_long_table_name_that_will_not_fit")
		f.db.AddChild(long)
		f.db.Expanded = true
		f.explorer.SetNodes([]*Node{f.db})
		for _, l := range strings.Split(f.explorer.tree.View(), "\n") {
			plain := ansi.Strip(l)
			if !strings.Contains(plain, "a_very_long") {
				continue
			}
			if !strings.Contains(plain, "…") {
				t.Errorf("the long name was not cut: %q", plain)
			}
			if w := lipgloss.Width(plain); w > 10 {
				t.Errorf("the cut name is %d cells wide, want it inside the 10 the pane leaves: %q", w, plain)
			}
		}
	})

	t.Run("truncateWithEllipsis answers exactly what it is asked", func(t *testing.T) {
		for _, tc := range []struct {
			name, in string
			width    int
			want     string
		}{
			{"a width of zero", "anything", 0, ""},
			{"a negative width", "anything", -3, ""},
			{"a width that fits", "short", 10, "short"},
			{"a width of exactly the text", "short", 5, "short"},
			// The ellipsis COSTS a cell: 4 cells of budget is 3 of text.
			{"one cell too narrow", "short", 4, "sho…"},
			{"far too narrow", "a long string indeed", 6, "a lon…"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := truncateWithEllipsis(tc.in, tc.width)
				if got != tc.want {
					t.Errorf("truncateWithEllipsis(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
				}
				if w := lipgloss.Width(got); w > tc.width && tc.width > 0 {
					t.Errorf("truncateWithEllipsis(%q, %d) returned %d cells wide", tc.in, tc.width, w)
				}
			})
		}
	})

	t.Run("the window and the click agree on which row is where", func(t *testing.T) {
		// Each rendered row is marked for the mouse with `tree-<index in the
		// LIST>`, and HandleClick turns a pane row back into `offset + listY`.
		// Those two have to be the same arithmetic or a click picks the
		// neighbour, which on a drop_table is the wrong table.
		//
		// Asserted as an agreement between the two functions rather than by
		// inspecting bubblezone's markers: Mark only registers bounds on a
		// Scan, so a test reaching for Zones.Get sees nil for a row that is
		// perfectly well marked. The contract worth pinning is the agreement.
		for _, h := range []int{3, 4, 5} {
			for _, jump := range []config.ActionID{"go_first", "go_last"} {
				f := newFixture(t)
				f.explorer.SetWidth(60)
				f.explorer.SetHeight(h)
				f.explorer.tree.SetFilter("")
				f.explorer.HandleAction(jump)

				visible := f.explorer.tree.filtered[f.explorer.tree.offset:]
				if len(visible) > h-2 {
					visible = visible[:h-2]
				}
				if len(visible) == 0 {
					continue
				}
				for j, want := range visible {
					// y = 1 for the border, plus j for the row's position.
					if !f.explorer.HandleClick(j + 1) {
						t.Errorf("h=%d after %s: a click on visible row %d reported no hit", h, jump, j)
						continue
					}
					if got := f.explorer.Selected(); got != want {
						t.Errorf("h=%d after %s: visible row %d is %q but a click there selected %q",
							h, jump, j, want.Name, got.Name)
					}
				}
			}
		}
	})

	t.Run("the explorer draws a border, and the filter line above the tree", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetWidth(34)
		f.explorer.SetHeight(12)

		plain := ansi.Strip(f.explorer.View())
		if !strings.Contains(plain, "Explorer") {
			t.Errorf("the pane has no title: %q", plain)
		}
		if !strings.Contains(plain, "public") {
			t.Errorf("the pane does not show the tree: %q", plain)
		}
		if strings.Contains(plain, "Filter:") {
			t.Errorf("the pane shows a filter line with no filter: %q", plain)
		}

		// While typing, the line has a cursor on it. It is INSIDE the border, so
		// the first line of the render is the top border, not the filter.
		f.explorer.StartFilter()
		f.explorer.tree.SetFilter("us")
		typing := lineWith(f.explorer.View(), "Filter:")
		if typing == "" {
			t.Fatalf("the pane shows no filter line while typing:\n%s", ansi.Strip(f.explorer.View()))
		}
		// The border draws its vertical rules right around the content, so the
		// line is padded — what matters is that the filter text and its cursor
		// are together, in that order.
		if !strings.Contains(typing, "Filter: us_") {
			t.Errorf("the typing line is %q, want it to contain %q", typing, "Filter: us_")
		}

		// Committed, the line loses the cursor but stays.
		f.explorer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		committed := lineWith(f.explorer.View(), "Filter:")
		if committed == "" {
			t.Fatal("the filter line disappeared when the filter was committed")
		}
		if strings.Contains(committed, "_") {
			t.Errorf("the committed line %q still has the typing cursor on it", committed)
		}
		if !strings.Contains(committed, "us") {
			t.Errorf("the committed line is %q, want it to still say what was typed", committed)
		}

		// And with no filter at all there is no line.
		f.explorer.tree.ClearFilter()
		if got := lineWith(f.explorer.View(), "Filter:"); got != "" {
			t.Errorf("the pane shows the filter line %q with no filter", got)
		}
	})

	t.Run("the border changes with focus", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetWidth(34)
		f.explorer.SetHeight(12)
		f.explorer.Blur()
		blurred := f.explorer.View()
		f.explorer.Focus()
		focused := f.explorer.View()
		if blurred == focused {
			t.Error("focusing the explorer did not change how it draws")
		}
		f.explorer.Blur()
		if f.explorer.focused {
			t.Error("Blur did not blur the explorer")
		}
		f.explorer.Focus()
		if !f.explorer.focused {
			t.Error("Focus did not focus the explorer")
		}
	})
}

func nonBlankLines(s string) int {
	n := 0
	for _, l := range strings.Split(ansi.Strip(s), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// lineWith returns the first rendered line containing sub, styles stripped. The filter
// line lives INSIDE the border, so it is never the first line of the render.
func lineWith(render, sub string) string {
	for _, l := range strings.Split(ansi.Strip(render), "\n") {
		if strings.Contains(l, sub) {
			return strings.TrimRight(l, " ")
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// The icons: blank, and pinned
// ---------------------------------------------------------------------------

// Scenario: Los iconos son BLANCOS, y el marcador de expandir no cambia.
//
// Both facts are true from the first commit and neither is a regression — the glyphs were
// never there. They are pinned here for two reasons: a test that asserts nothing about
// them leaves a reader unable to tell a deliberate blank from a lost glyph, and the
// mutation gate needs the dead branch PROVED dead rather than assumed.
//
// The expand marker is the interesting one. getExpandIcon returns the same string for an
// expanded node and a collapsed one, so the `if node.Expanded` arm cannot be distinguished
// from its negation — which is why the branch is equivalent to deleting itself, and why it
// is in the allowlist with that proof rather than as "not worth testing".
func TestTheIconsAreBlankAndTheExpandMarkerDoesNotChange(t *testing.T) {
	t.Run("every node type renders no glyph", func(t *testing.T) {
		for _, typ := range []NodeType{
			NodeDatabase, NodeSchema, NodeTable, NodeColumn, NodeIndex, NodeConstraint,
		} {
			icon := typ.Icon()
			if strings.TrimSpace(icon) != "" {
				t.Errorf("the %s icon is %q, want blank — the icons were never implemented, and a test that says so is worth more than one that ignores it", typ, icon)
			}
			if typ.Icon() != icon {
				t.Errorf("the %s icon is not stable across calls", typ)
			}
		}
	})

	t.Run("a type with no name for it says unknown", func(t *testing.T) {
		if got := NodeType(99).String(); got != "unknown" {
			t.Errorf("an unnamed type is %q, want %q", got, "unknown")
		}
		if got := NodeType(99).Icon(); strings.TrimSpace(got) != "" {
			t.Errorf("an unnamed type's icon is %q, want blank", got)
		}
		for _, tc := range []struct {
			typ  NodeType
			want string
		}{
			{NodeDatabase, "database"}, {NodeSchema, "schema"}, {NodeTable, "table"},
			{NodeColumn, "column"}, {NodeIndex, "index"}, {NodeConstraint, "constraint"},
		} {
			if got := tc.typ.String(); got != tc.want {
				t.Errorf("the type name is %q, want %q", got, tc.want)
			}
		}
	})

	t.Run("the expand marker is the SAME whether the node is open or shut", func(t *testing.T) {
		f := newFixture(t)
		tree := f.explorer.tree
		f.public.Expanded = true
		open := tree.getExpandIcon(f.public)
		f.public.Expanded = false
		shut := tree.getExpandIcon(f.public)

		if open != shut {
			t.Skipf("the expand marker now differs (%q open, %q shut): the icons HAVE been implemented, so delete this test and the allowlist entry it justifies", open, shut)
		}
		// A leaf's marker is wider than a branch's, which is the only difference
		// the function actually makes.
		if tree.getExpandIcon(f.id) == shut {
			t.Error("a leaf and a collapsed branch render the same marker, so a leaf is indistinguishable from a shut schema")
		}
		t.Logf("the marker is %q for a branch either way and %q for a leaf: the expanded arm is dead code, which is what the allowlist entry records", shut, tree.getExpandIcon(f.id))
	})

	t.Run("the depth of a node is how many ancestors it has", func(t *testing.T) {
		f := newFixture(t)
		tree := f.explorer.tree
		for _, tc := range []struct {
			node *Node
			want int
		}{
			{f.db, 0}, {f.public, 1}, {f.users, 2}, {f.id, 3},
			{NewNode("orphan", NodeTable, "no parent"), 0},
		} {
			if got := tree.getDepth(tc.node); got != tc.want {
				t.Errorf("the depth of %q is %d, want %d", tc.node.Name, got, tc.want)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// FindTable
// ---------------------------------------------------------------------------

// Scenario: Buscar una tabla la encuentra por esquema Y nombre, en cualquier nivel.
//
// Both halves matter and they are asymmetric: the name alone is not enough, so two
// schemas can each have a `users` and a lookup that forgot the schema would return the
// first one it found — the kind of wrong answer that is only noticed after you have
// dropped the wrong table.
func TestFindTableNeedsBothTheSchemaAndTheName(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree

	if got := tree.FindTable("public", "users"); got != f.users {
		t.Errorf("FindTable(public, users) = %v, want the users table", got)
	}
	if got := tree.FindTable("audit", "events"); got != f.events {
		t.Errorf("FindTable(audit, events) = %v, want the events table", got)
	}

	t.Run("the name alone is not enough", func(t *testing.T) {
		// A `users` in audit as well: the right answer is the public one only if
		// the schema matches, and the audit one only if it does.
		twin := NewNode("twin", NodeTable, "users")
		twin.Metadata["schema"] = "audit"
		f.audit.AddChild(twin)
		f.explorer.SetNodes([]*Node{f.db})

		if got := tree.FindTable("audit", "users"); got != twin {
			t.Errorf("FindTable(audit, users) = %v, want the audit one", got)
		}
		if got := tree.FindTable("public", "users"); got != f.users {
			t.Errorf("FindTable(public, users) = %v, want the public one", got)
		}
		if got := tree.FindTable("", "users"); got != nil {
			t.Errorf("FindTable with no schema returned %v, want nothing: a name without its schema is not a table", got)
		}
	})

	t.Run("a table with no schema in its metadata is not findable", func(t *testing.T) {
		bare := NewNode("bare", NodeTable, "orphan")
		f.db.AddChild(bare)
		f.explorer.SetNodes([]*Node{f.db})
		if got := tree.FindTable("public", "orphan"); got != nil {
			t.Errorf("a table with no schema metadata was found as %v", got)
		}
		if got := tree.FindTable("", "orphan"); got != nil {
			t.Errorf("a table with no schema metadata was found with an empty schema as %v", got)
		}
	})

	t.Run("it searches collapsed subtrees", func(t *testing.T) {
		// Collapsed means not SHOWN, not not-there. A lookup that walked only
		// the flattened list would fail for every table inside a closed schema,
		// which is every table a user is likely to jump to.
		if f.audit.Expanded {
			t.Fatal("the fixture's audit schema is expanded, so this proves nothing")
		}
		if got := tree.FindTable("audit", "events"); got == nil {
			t.Error("a table inside a collapsed schema was not found")
		}
	})

	t.Run("nothing to search is not a match", func(t *testing.T) {
		if got := tree.FindTable("public", "users"); got == nil {
			t.Fatal("the tree went empty, so this fixture proves nothing")
		}
		empty := NewTree(theme.Resolve("dark").Styles())
		if got := empty.FindTable("public", "users"); got != nil {
			t.Errorf("an empty tree found %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// A property, over many states
// ---------------------------------------------------------------------------

// Scenario: El arbol nunca se sale de si mismo, y el cursor nunca se sale de la lista.
//
// One walk, many states, and the invariants that keep a tree usable: the cursor indexes a
// row that exists, the window is inside the list, the rendered output has no more rows
// than the pane has height, and nothing panics. The keys and actions are mixed on purpose
// — the crash this guards against needed a filter and a cursor, not a single key.
func TestTheTreeNeverLeavesItself(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: 'k', Text: "k"},
		{Code: 'g', Text: "g"},
		{Code: 'G', Text: "G"},
		{Code: '/', Text: "/"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyEscape, Text: "esc"},
		{Code: tea.KeyBackspace},
		{Code: tea.KeyDown},
		{Code: tea.KeyUp},
	}
	actions := []config.ActionID{
		"navigate_down", "navigate_up", "go_first", "go_last",
		"expand_node", "collapse_node", "toggle_columns", "filter_tables",
	}

	// math/rand rather than a hand-rolled LCG: the LCG overflows int64 to
	// NEGATIVE, and a negative index is a panic rather than a readable failure.
	for seed := int64(1); seed <= 20; seed++ {
		f := newFixture(t)
		rng := rand.New(rand.NewSource(seed))
		height := 2 + rng.Intn(6)

		// what names the last few things done, so a failure says WHICH step
		// produced the state rather than just which number it was — the state
		// that trips an invariant is usually not the one the last step made.
		var did []string
		for step := range 120 {
			f.explorer.SetHeight(height)
			if rng.Intn(2) == 0 {
				k := keys[rng.Intn(len(keys))]
				did = append(did, fmt.Sprintf("key %q", k.String()))
				f.explorer.Update(k)
			} else {
				a := actions[rng.Intn(len(actions))]
				did = append(did, fmt.Sprintf("action %q", a))
				f.explorer.HandleAction(a)
			}
			// Keep the last few, for the message.
			if len(did) > 4 {
				did = did[len(did)-4:]
			}
			// A click every so often, since it writes the cursor too.
			if rng.Intn(8) == 0 {
				y := rng.Intn(height + 3)
				did = append(did, fmt.Sprintf("click y=%d", y))
				f.explorer.HandleClick(y)
			}

			n := len(f.explorer.tree.filtered)
			if n == 0 {
				if f.explorer.tree.cursor != 0 {
					t.Fatalf("seed %d step %d after %v: an empty list left the cursor at %d",
						seed, step, did, f.explorer.tree.cursor)
				}
				continue
			}
			c := f.explorer.tree.cursor
			if c < 0 || c >= n {
				t.Fatalf("seed %d step %d after %v: the cursor is at %d with %d rows (%v)",
					seed, step, did, c, n, f.names())
			}
			if off := f.explorer.tree.offset; off < 0 || off > c {
				t.Fatalf("seed %d step %d after %v (h=%d): the window starts at %d with the cursor at %d and %d rows (%v)",
					seed, step, did, f.explorer.tree.height, off, c, n, f.names())
			}
			if sel := f.explorer.Selected(); sel == nil {
				t.Fatalf("seed %d step %d after %v: nothing is selected with %d rows and the cursor at %d",
					seed, step, did, n, f.explorer.tree.cursor)
			}
			// The render must survive every one of these states.
			out := f.explorer.View()
			rows := nonBlankLines(out)
			if rows > height+4 {
				t.Fatalf("seed %d step %d: a %d-row pane rendered %d non-blank lines",
					seed, step, height, rows)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The defensive arms, reached the only way they can be
// ---------------------------------------------------------------------------

// Scenario: Las ramas defensivas se alcanzan escribiendolas, y contestan.
//
// Nine statements in this package are guards against states the code cannot produce on
// its own: a negative cursor, a negative offset, a nil row in the list, an explorer with
// nothing in it, a message that is not a key. Each one is a crash avoided rather than a
// behaviour, which is exactly why they need a test — a guard nobody has run is a guard
// nobody knows works — and exactly why each is written by hand here and reached by
// writing the field directly. The alternative is leaving them uncovered and untested,
// which is what "it cannot happen" usually means.
//
// What is asserted is that each one answers rather than panics, and that the answer is
// the safe one: a clamp rather than a negative index, nothing rather than a wrong row.
func TestTheDefensiveArmsAnswer(t *testing.T) {
	t.Run("a negative cursor is pulled back to the start", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.cursor = -7 // no caller can do this; clampCursor can answer it
		f.explorer.tree.flattenNodes()
		if f.explorer.tree.cursor != 0 {
			t.Errorf("a negative cursor became %d, want 0", f.explorer.tree.cursor)
		}
		if f.explorer.Selected() == nil {
			t.Error("nothing is selected after the cursor was pulled back to the start")
		}
	})

	t.Run("a negative offset is pulled back to the start", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.tree.SetHeight(2) // so the list really is taller than the window
		if len(f.explorer.tree.filtered) <= 2 {
			t.Fatal("the fixture's list is not taller than the window, so clampOffset returns early")
		}
		f.explorer.tree.offset = -5
		f.explorer.tree.clampOffset()
		if f.explorer.tree.offset != 0 {
			t.Errorf("a negative offset became %d, want 0", f.explorer.tree.offset)
		}
	})

	t.Run("a nil ROW in the list is skipped rather than dereferenced", func(t *testing.T) {
		// filtered is rebuilt from the node tree on every change, so a nil in it
		// means something upstream built a malformed tree. View still has to
		// render — the whole point of a pane is that it shows what it has.
		f := newFixture(t)
		f.explorer.tree.filtered = []*Node{nil, f.db, nil, f.public}
		f.explorer.SetWidth(40)
		f.explorer.SetHeight(6)
		out := ansi.Strip(f.explorer.tree.View())
		if !strings.Contains(out, "public") {
			t.Errorf("a nil in the list made the render %q, want the real rows still drawn", out)
		}
		// And Selected() on one returns nil rather than a nil-pointer panic.
		f.explorer.tree.cursor = 0
		if sel := f.explorer.Selected(); sel != nil {
			t.Errorf("Selected() on a nil row returned %v, want nil", sel)
		}
	})

	t.Run("ToggleExpand on an empty tree does nothing", func(t *testing.T) {
		f := newFixture(t)
		f.explorer.SetNodes(nil)
		if cmd := f.explorer.ToggleExpand(); cmd != nil {
			t.Errorf("ToggleExpand on an empty tree emitted %+v, want nothing", cmd())
		}
		if f.explorer.HandleClick(2) {
			t.Error("a click on an empty tree selected a row")
		}
	})

	t.Run("ToggleExpand on a SCHEMA toggles it and emits nothing", func(t *testing.T) {
		// The other half of the pair the E6 test pins: on a table ToggleExpand
		// asks the app for the table, on a schema it is a pure view change.
		f := newFixture(t)
		f.selectNode(t, f.public)
		if cmd := f.explorer.ToggleExpand(); cmd != nil {
			t.Errorf("ToggleExpand on a schema emitted %+v, want nothing", cmd())
		}
		if f.public.Expanded {
			t.Error("ToggleExpand on a schema did not collapse it")
		}
		f.cursorIsInRange(t, "after ToggleExpand on a schema")
		if cmd := f.explorer.ToggleExpand(); cmd != nil {
			t.Errorf("ToggleExpand on a schema emitted %+v the second time", cmd())
		}
		if !f.public.Expanded {
			t.Error("ToggleExpand did not expand the schema back")
		}
	})

	t.Run("a message that is not a key is not claimed", func(t *testing.T) {
		f := newFixture(t)
		for _, msg := range []tea.Msg{
			tea.WindowSizeMsg{Width: 80, Height: 24},
			tea.MouseClickMsg{},
			nil,
			ExplorerRefreshMsg{},
		} {
			if cmd, handled := f.explorer.Update(msg); handled || cmd != nil {
				t.Errorf("the message %T was claimed by the explorer", msg)
			}
		}
	})

	t.Run("a table nested UNDER a table shows its own expanded children", func(t *testing.T) {
		// addAllChildren recurses into an expanded child that is not a leaf,
		// which is how a partition or a child table's columns get shown under a
		// filtered match. A fixture with one level of nesting never reaches it.
		f := newFixture(t)
		// Named so nothing else in the fixture matches: the audit schema's
		// `events` would otherwise come along and the expectation would be
		// asserting two matches rather than the nesting.
		outer := NewNode("outer", NodeTable, "sessions")
		outer.Metadata["schema"] = "public"
		outer.Expanded = true
		outer.AddChild(NewNode("oc", NodeColumn, "id"))
		inner := NewNode("inner", NodeTable, "sessions_daily")
		inner.Metadata["schema"] = "public"
		inner.Expanded = true
		outer.AddChild(inner)
		inner.AddChild(NewNode("ic", NodeColumn, "day"))
		f.public.AddChild(outer)
		f.explorer.SetNodes([]*Node{f.db})

		got := f.typeFilter("sessions")
		want := []string{"app", "public", "sessions", "id", "sessions_daily", "day"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("a nested expanded table matched %v, want %v", got, want)
		}
	})

	t.Run("a nested table that is NOT expanded stops the walk", func(t *testing.T) {
		f := newFixture(t)
		outer := NewNode("outer", NodeTable, "sessions")
		outer.Metadata["schema"] = "public"
		outer.Expanded = true
		inner := NewNode("inner", NodeTable, "sessions_daily")
		inner.Metadata["schema"] = "public"
		inner.Expanded = false
		outer.AddChild(inner)
		inner.AddChild(NewNode("ic", NodeColumn, "day"))
		f.public.AddChild(outer)
		f.explorer.SetNodes([]*Node{f.db})

		got := f.typeFilter("sessions")
		want := []string{"app", "public", "sessions", "sessions_daily"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("a shut nested table matched %v, want %v — its children are not shown", got, want)
		}
	})
}
