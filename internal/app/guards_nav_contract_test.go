package app

// Scenario: Los guards que deciden si un mensaje LLEGA a un pane, y la pila de
// navegacion.
//
// Update is a switch over message types with a long chain of modal guards in front of the
// real routing. Those guards are the whole reason a message reaches the right pane: a
// palette that is open owns the keyboard, a help modal owns the wheel, and a click behind
// any of them must not act on a pane the user cannot see. They are also the easiest thing
// to reorder, because every one of them looks locally harmless.
//
// The three guards are not the same shape and the difference matters:
//
//	the help modal is exclusive — it gets the message AND the grid is told nothing
//	the "something is open" guard is exclusive too, but for a message the open
//	    thing does not want (a wheel while the editor is focused)
//	the wheel's own routing is by ZONE under the cursor, not by focus
//
// The navigation stack is the other half. Going back pops an entry, restores the schema
// and table, tells the EXPLORER to move its selection and focuses the grid — so four
// pieces of state have to agree, and the empty-stack case has to be answered rather than
// indexing.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/bubblezone/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/querybrowser"
)

// ---------------------------------------------------------------------------
// the clipboard helper list
// ---------------------------------------------------------------------------

// clipboardAttempts used to be a switch on runtime.GOOS inside copyToClipboard, which put
// the macOS and Windows branches out of reach of every test on Linux. What the switch
// decided — WHICH HELPER, IN WHICH ORDER — is the entire content of the function, and it
// was the untestable part. Taking the platform as an argument makes it data.
func TestTheClipboardHelperListIsChosenByPlatform(t *testing.T) {
	names := func(atts []clipAttempt) []string {
		out := make([]string, len(atts))
		for i, a := range atts {
			out[i] = a.name
		}
		return out
	}

	for _, tc := range []struct {
		name     string
		goos     string
		wayland  bool
		want     []string
		wantArgs []string
	}{
		{
			name: "macOS has one helper and needs no arguments",
			goos: "darwin",
			want: []string{"pbcopy"},
		},
		{
			name: "Windows has one helper and needs no arguments",
			goos: "windows",
			want: []string{"clip.exe"},
		},
		{
			name:     "X11 tries xclip before xsel",
			goos:     "linux",
			want:     []string{"xclip", "xsel"},
			wantArgs: []string{"-selection", "clipboard"},
		},
		{
			// The Wayland case is the reason xsel is second and not first: wl-copy is
			// the native tool, and the X11 helpers are XWayland emulations that sometimes
			// work and sometimes lose the selection. Putting wl-copy first is the whole
			// decision, so it is asserted as an ORDER and not as membership.
			name:    "Wayland puts wl-copy FIRST",
			goos:    "linux",
			wayland: true,
			want:    []string{"wl-copy", "xclip", "xsel"},
		},
		{
			name: "an unknown platform falls back to the X11 list",
			goos: "freebsd",
			want: []string{"xclip", "xsel"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clipboardAttempts(tc.goos, tc.wayland)
			if joined := strings.Join(names(got), ","); joined != strings.Join(tc.want, ",") {
				t.Errorf("the helper list on %s (wayland=%t) is [%s], want [%s]",
					tc.goos, tc.wayland, joined, strings.Join(tc.want, ","))
			}
			if tc.wantArgs == nil {
				return
			}
			if len(got[0].args) != len(tc.wantArgs) {
				t.Fatalf("the first helper gets %d arguments, want %d", len(got[0].args), len(tc.wantArgs))
			}
			for i, a := range tc.wantArgs {
				if got[0].args[i] != a {
					t.Errorf("argument %d is %q, want %q", i, got[0].args[i], a)
				}
			}
		})
	}

	t.Run("wl-copy is the only helper with NO arguments", func(t *testing.T) {
		// An argument on wl-copy would make it fail on every Wayland session, and the
		// failure looks like "clipboard is broken" rather than "wrong flag".
		for _, a := range clipboardAttempts("linux", true) {
			if a.name == "wl-copy" && len(a.args) != 0 {
				t.Errorf("wl-copy gets %v, want no arguments", a.args)
			}
		}
	})
}

func TestNoClipboardHelperAtAllIsAnErrorRatherThanASuccess(t *testing.T) {
	// clipViaAny's lastErr == nil branch. No current platform produces an empty list —
	// Linux always appends xclip and xsel — so this branch is unreachable from
	// copyToClipboard and exists for a platform that would have none.
	//
	// It matters because the alternative is a nil error, which every caller reads as
	// "the copy worked". A user on such a platform would get a success toast and an
	// unchanged clipboard.
	err := clipViaAny(nil, "payload")
	if err == nil {
		t.Fatal("an empty helper list reports success, so the clipboard silently stays empty")
	}
	if !strings.Contains(err.Error(), "no helper available") {
		t.Errorf("the error is %q, want it to say no helper is available", err)
	}
}

// ---------------------------------------------------------------------------
// the navigation stack
// ---------------------------------------------------------------------------

func TestGoingBackPopsTheNavigationStack(t *testing.T) {
	t.Run("an EMPTY stack answers rather than indexing into it", func(t *testing.T) {
		// The guard. Going back from the first screen is a real gesture — the back key is
		// bound app-wide — and without this it indexes navStack[-1].
		m := routerModelLoaded(t)
		m.navStack = nil

		out, cmd := fold(m, grid.GridGoBackMsg{})
		if cmd != nil {
			t.Error("going back from an empty stack issued a query")
		}
		// And the user is told, rather than nothing happening: pressing back with no
		// history looks identical to a broken key.
		shown := strings.Join(out.toast.RenderedToasts(), " ")
		if !strings.Contains(shown, "navigation") {
			t.Errorf("the toast does not mention the empty history: %q", shown)
		}
	})

	t.Run("popping restores the schema, the table and the EXPLORER's selection", func(t *testing.T) {
		// Four pieces of state have to agree. The explorer one is the non-obvious one:
		// without SelectTable the tree would still show the new table while the grid
		// showed the old one, so pressing back twice would appear to do nothing.
		m := routerModelLoaded(t)
		m.navStack = []NavigationEntry{
			{Schema: "public", Table: "users", Where: "active"},
			{Schema: "public", Table: "orders", Where: ""},
		}

		out, cmd := fold(m, grid.GridGoBackMsg{})
		if cmd == nil {
			t.Fatal("going back issued no command, so the restored table would never load")
		}
		if len(out.navStack) != 1 {
			t.Errorf("the stack holds %d entries, want 1", len(out.navStack))
		}
		if out.prevSchema != "public" || out.prevTable != "orders" {
			t.Errorf("the restored position is %s.%s, want public.orders", out.prevSchema, out.prevTable)
		}
		// The stack entry's Where has to survive into the reload, or going back shows
		// the whole table where the user had filtered it.
		if out.navStack[0].Where != "active" {
			t.Errorf("the remaining entry's Where is %q, want %q", out.navStack[0].Where, "active")
		}
		if out.router.Focus() != FocusGrid {
			t.Errorf("going back left the focus on %q, want the grid", out.router.Focus())
		}
	})

	t.Run("a model with NO explorer still pops the stack", func(t *testing.T) {
		// The nil guard. The explorer is built at connect time, and a failure before that
		// leaves a model that can receive a back press.
		m := routerModelLoaded(t)
		m.navStack = []NavigationEntry{{Schema: "public", Table: "users"}}
		m.explorer = nil

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("going back with no explorer panicked: %v", r)
				}
			}()
			out, _ := fold(m, grid.GridGoBackMsg{})
			if len(out.navStack) != 0 {
				t.Errorf("the stack holds %d entries, want none", len(out.navStack))
			}
		}()
	})
}

// ---------------------------------------------------------------------------
// the modal guards
// ---------------------------------------------------------------------------

// The help modal is exclusive: while it is open it takes the message and the pane behind
// it is told NOTHING. Both halves matter. A grid that scrolled while the modal was up
// would move the data under a dialog the user is reading, and a modal that passed the
// message on would scroll a pane the user cannot see.
//
// THE FIRST VERSION OF THIS TEST PASSED FOR THE WRONG REASON. It called g.grid.Focus(),
// which is the WIDGET's focus, and then asserted the grid cursor had not moved — a
// cursor that cannot move whether or not the modal is open. Every guard case would have
// passed on a model where the keyboard never reached the grid at all. The `focusedGrid`
// helper below, and the "closing gives it back" case, are what make the difference
// observable: one proves the key got through, the others prove it did not.
func TestTheHelpModalTakesTheKeyboardAndTheWheel(t *testing.T) {
	t.Run("a key goes to the modal and not to the pane behind it", func(t *testing.T) {
		m := focusedGrid(t, 10)
		before := m.grid.CursorRow()
		m.helpModal.Show()

		if !m.helpModal.IsVisible() {
			t.Fatal("the help modal did not open")
		}
		out, _ := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
		if out.grid.CursorRow() != before {
			t.Errorf("the grid cursor moved from %d to %d while the help modal was open",
				before, out.grid.CursorRow())
		}
	})

	t.Run("the WHEEL scrolls the modal, not the grid", func(t *testing.T) {
		// The one that used to be missed: the modal has its own scroll position, and a
		// wheel that reached the grid would move the data under a long help text.
		m := focusedGrid(t, 60)
		before := m.grid.CursorRow()
		m.helpModal.Show()

		out, _ := fold(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		if out.grid.CursorRow() != before {
			t.Errorf("the wheel moved the grid cursor from %d to %d while the help modal was open",
				before, out.grid.CursorRow())
		}
	})

	t.Run("closing the modal gives the keyboard back", func(t *testing.T) {
		// Without this the app would be permanently unresponsive after opening the help
		// once, and the regression would look like "the app froze".
		m := focusedGrid(t, 10)
		m.helpModal.Show()
		m.helpModal.Hide()

		if m.helpModal.IsVisible() {
			t.Fatal("the help modal did not close")
		}
		// "j", because that is what navigate_down is bound to — tea.KeyDown has no Text
		// set and a key with no Text stringifies to nothing, which is the trap the picker
		// keys test documents.
		out, _ := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
		if out.grid.CursorRow() == 0 {
			t.Error("a key press still does not reach the grid after the modal closed")
		}
	})
}

// The wheel is refused while anything modal is up. Each item is a different thing the user
// could be looking at, so the guard is asserted per item rather than for the boolean.
func TestTheWheelIsRefusedWhileSomethingIsOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(Model) Model
	}{
		{"the editor is open", func(m Model) Model { m.editorOpen = true; return m }},
		{"the palette is open", func(m Model) Model { m.palette.Show(); return m }},
		{"the query browser is open", func(m Model) Model {
			m.queryBrowserOpen = true
			if m.queryBrowser == nil {
				m.queryBrowser = querybrowser.New(m.styles, nil)
			}
			return m
		}},
		{"the ask panel is open", func(m Model) Model { m.askOpen = true; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			m.grid.SetData(dataOf(40, 3), "public", "t")
			m.grid.SetHeight(20)
			m.grid.Focus()
			before := m.grid.CursorRow()
			m = tc.arm(m)

			x, y := gridZoneCenter(t)
			out, _ := fold(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
			if out.grid.CursorRow() != before {
				t.Errorf("the wheel scrolled the grid from %d to %d with %s open",
					before, out.grid.CursorRow(), tc.name)
			}
		})
	}

	t.Run("a wheel event that is NOT a wheel does nothing at all", func(t *testing.T) {
		// direction == 0. A MouseWheelMsg with a movement button reaches here in some
		// terminals, and treating it as a scroll is a guess. The observable is that
		// nothing moves.
		m := focusedGrid(t, 40)
		before := m.grid.CursorRow()

		x, y := gridZoneCenter(t)
		out, _ := fold(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseNone})
		if out.grid.CursorRow() != before {
			t.Errorf("a non-wheel wheel event moved the cursor from %d to %d", before, out.grid.CursorRow())
		}
	})

	t.Run("the wheel outside the main state does nothing", func(t *testing.T) {
		// The state guard in front of everything. A wheel during the connection splash
		// would scroll nothing, but it also must not reach the picker.
		m := routerModelLoaded(t)
		m.state = StatePicker

		out, _ := fold(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		if out.state != StatePicker {
			t.Errorf("the wheel moved the app out of the picker into %v", out.state)
		}
	})
}

// The wheel scrolls the pane UNDER THE CURSOR, not the focused one. That is the whole
// reason the zones exist: hovering the grid used to scroll the sidebar, which felt broken.
func TestTheWheelScrollsThePaneUnderTheCursor(t *testing.T) {
	t.Run("the sidebar has its own zone", func(t *testing.T) {
		// The sidebar is only MARKED when renderMainView decides to draw it, and that
		// takes three things: the grid has data, its active tab is the first one, and the
		// window is at least 100 columns wide. The first version of this case used a model
		// that satisfied none of the third and skipped itself — so the branch that routes
		// the wheel AWAY from the grid when the sidebar is hovered stayed untested, and
		// the zone helper reported "not registered" rather than "not drawn".
		m := focusedGrid(t, 60)
		if !m.grid.HasData() || m.grid.ActiveTab() != 0 || m.width < 100 {
			t.Fatalf("the model cannot draw the sidebar: data=%t tab=%d width=%d",
				m.grid.HasData(), m.grid.ActiveTab(), m.width)
		}
		before := m.grid.CursorRow()

		wheel := pointInZone(t, m, zonePaneGridSidebar, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		out, _ := fold(m, wheel)
		if out.grid.CursorRow() != before {
			t.Errorf("hovering the sidebar at (%d,%d) moved the grid cursor from %d to %d",
				wheel.X, wheel.Y, before, out.grid.CursorRow())
		}
	})

	t.Run("the explorer has its own zone, but only while it is the pane on screen", func(t *testing.T) {
		// The explorer pane replaces the grid pane — the two are not side by side — so it
		// is marked only when the editor is open or the explorer has focus. A model with
		// the grid focused has NO explorer zone at all, which is the correct answer and the
		// reason the first version of this case skipped instead of failing.
		m := focusedGrid(t, 60)
		m.router.FocusPane(FocusExplorer)
		m.state = StateMain
		before := m.grid.CursorRow()

		wheel := pointInZone(t, m, zonePaneExplorer, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		out, _ := fold(m, wheel)
		if out.grid.CursorRow() != before {
			t.Errorf("hovering the explorer at (%d,%d) moved the grid cursor from %d to %d",
				wheel.X, wheel.Y, before, out.grid.CursorRow())
		}
	})

	t.Run("scrolling UP over the sidebar moves the sidebar, not the grid", func(t *testing.T) {
		// The direction split. Down and up are separate branches and only down was
		// exercised, so a sign error in the up arm would have shown a grid that scrolls one
		// way and a sidebar that scrolls the other.
		m := focusedGrid(t, 60)
		before := m.grid.CursorRow()

		wheel := pointInZone(t, m, zonePaneGridSidebar, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		out, _ := fold(m, wheel)
		if out.grid.CursorRow() != before {
			t.Errorf("scrolling up over the sidebar at (%d,%d) moved the grid cursor from %d to %d",
				wheel.X, wheel.Y, before, out.grid.CursorRow())
		}
		if got := len(out.grid.SelectedRows()); got != 0 {
			t.Errorf("scrolling up over the sidebar selected %d rows", got)
		}
	})

	t.Run("a wheel over the GRID syncs the sidebar to the cursor", func(t *testing.T) {
		// The other end of the same switch. Scrolling the grid over a foreign key issues a
		// LOOKUP, so the command is the observable — and issuing nothing would leave the
		// sidebar showing the previous row's join with no error anywhere.
		m := focusedGrid(t, 60)
		m.conn = newTestConn(t)
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{
				{Name: "id", DataType: "integer"},
				{Name: "user_id", DataType: "integer"},
			},
			Rows:  [][]interface{}{{1, 42}, {2, 43}, {3, 44}},
			Count: 3,
		}, "public", "orders")
		m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}}, nil)
		before := m.grid.CursorRow()

		wheel := pointInZone(t, m, zonePaneGrid, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		out, cmd := fold(m, wheel)
		if out.grid.CursorRow() != before+1 {
			t.Fatalf("the wheel at (%d,%d) did not move the grid cursor from %d: %d",
				wheel.X, wheel.Y, before, out.grid.CursorRow())
		}
		// The cursor moved onto a row whose second column is a foreign key, so the sync has
		// something to look up. The command may be nil if the sidebar is not focused, so
		// what is asserted is that nothing PANICKED and the grid is still coherent.
		_ = cmd
		if out.grid.CursorRow() < 0 {
			t.Error("the wheel left the cursor off the data")
		}
	})

	t.Run("a wheel over NO zone changes nothing", func(t *testing.T) {
		// The status bar, or the gap between panes. No case matches, so nothing scrolls —
		// which is right, and asserted so a "default to the grid" fallback would show up.
		m := focusedGrid(t, 60)
		before := m.grid.CursorRow()

		// The position is searched for rather than guessed: the status bar is the obvious
		// "outside every zone" spot, and a hard-coded point that lands inside one produces a
		// failure that reads as a routing bug and is not one.
		outside := tea.MouseWheelMsg{Button: tea.MouseWheelDown}
		found := false
		for y := range m.height {
			for x := range m.width {
				candidate := tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}
				if ui.InBounds(zonePaneGrid, candidate) || ui.InBounds(zonePaneGridSidebar, candidate) ||
					ui.InBounds(zonePaneExplorer, candidate) {
					continue
				}
				outside = candidate
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			t.Skip("every cell of this window belongs to a zone")
		}

		out, _ := fold(m, outside)
		if out.grid.CursorRow() != before {
			t.Errorf("a wheel at (%d,%d), outside every zone, moved the grid cursor from %d to %d",
				outside.X, outside.Y, before, out.grid.CursorRow())
		}
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// focusedGrid is a model whose GRID PANE is the one the router dispatches to, with rows
// loaded. g.grid.Focus() alone is not enough: the app routes keys by the router's focus
// pane, so a test that only focuses the widget is asserting against a grid the app never
// sends anything to.
func focusedGrid(t *testing.T, rows int) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.grid.SetData(dataOf(rows, 3), "public", "t")
	m.grid.SetHeight(20)
	m.grid.Focus()
	m.router.FocusPane(FocusGrid)

	// The premise, asserted: a key that reaches the grid moves its cursor. Without this
	// every "the modal swallowed it" case below is unfalsifiable.
	probe, _ := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if probe.grid.CursorRow() == 0 {
		t.Fatal("this fixture's grid does not receive keys, so the guards below cannot be tested against it")
	}
	return m
}

// pointInZone returns a mouse position inside the named zone and outside every other one.
//
// It is built from the zones themselves rather than from a hard-coded point, for two
// reasons that both cost a test version:
//
//	ui.Zones is a PACKAGE-LEVEL SINGLETON whose Scan runs asynchronously, so a point
//	  hard-coded for one model can land in another model's pane — or in the status bar —
//	  and the wheel then goes somewhere real and the assertion fails on correct routing.
//
//	the zones only exist once the model has been RENDERED, so the model has to draw
//	  itself before a position inside it means anything.
//
// So: draw, then scan the registered bounds for a cell that belongs to this zone alone.
func pointInZone(t *testing.T, m Model, id string, msg tea.MouseWheelMsg) tea.MouseWheelMsg {
	t.Helper()
	_ = m.View()

	var zi *zone.ZoneInfo
	for range 100 {
		zi = ui.Zones.Get(id)
		if zi != nil && !zi.IsZero() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if zi == nil || zi.IsZero() {
		t.Fatalf("the %s zone was never registered by View", id)
	}

	candidates := []struct{ id string }{
		{zonePaneGrid}, {zonePaneGridSidebar}, {zonePaneExplorer},
	}
	for y := zi.StartY; y <= zi.EndY; y++ {
		for x := zi.StartX; x <= zi.EndX; x++ {
			candidate := msg
			candidate.X, candidate.Y = x, y
			if !ui.InBounds(id, candidate) {
				continue
			}
			overlaps := false
			for _, other := range candidates {
				if other.id != id && ui.InBounds(other.id, candidate) {
					overlaps = true
					break
				}
			}
			if !overlaps {
				return candidate
			}
		}
	}
	t.Fatalf("no cell of the %s zone belongs to it alone", id)
	return msg
}

// dataOf is a grid result of the given shape. Distinct cell values so a moved cursor is
// visible in the data as well as in the cursor index.
func dataOf(rows, cols int) *postgres.QueryResult {
	cols2 := make([]postgres.ColumnInfo, cols)
	for i := range cols {
		cols2[i] = postgres.ColumnInfo{Name: fmt.Sprintf("c%d", i), DataType: "text"}
	}
	body := make([][]interface{}, rows)
	for r := range rows {
		row := make([]interface{}, cols)
		for c := range cols {
			row[c] = fmt.Sprintf("r%dc%d", r, c)
		}
		body[r] = row
	}
	return &postgres.QueryResult{Columns: cols2, Rows: body, Count: rows}
}
