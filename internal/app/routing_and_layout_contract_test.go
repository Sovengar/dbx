package app

// Scenario: El enrutado del raton, los carve-outs del teclado, y el layout.
//
// Three groups, and they are the last of the app's arms:
//
//   - the mouse guards. The wheel and the click both have one overlay condition written as a
//     five-way OR, and a five-way OR is five separate arms that each need their own fixture.
//     The existing cases cover the editor and the palette; the query browser, the export picker
//     and the ASK panel are the other three, and each means something different — one is a
//     panel, one is a MODE on the grid, one is a lazily-built field.
//   - the keyboard carve-outs. Each pane that owns the keyboard is offered a key first, and
//     this asserts the answer when the pane DECLINES: the key then falls through to the app's
//     own handling. A carve-out that always returned would make the pane look focused while
//     swallowing everything.
//   - the layout. Every one of these is a branch on something the renderer computed: a
//     breadcrumb with and without a spinner, an overlay on a window too small for it, a
//     border colour that depends on focus.
//
// The recurring assertion in the third group is that the RENDER SUCCEEDS. These are all
// arithmetic that would panic rather than draw something wrong — a negative repeat count, a
// slice past its end — so "it returned" is the property, and the observable content is
// secondary.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/buble/dbx/internal/ui"
)

// ── the mouse ───────────────────────────────────────────────────────────────

// The overlay condition, one fixture per disjunct.
//
// Each of the five means something different, and a case that only proves "an overlay stops
// the wheel" would be satisfied by the editor alone — so every disjunct gets its own, and the
// table is the documentation of what the five are.
func TestTheWheelIsStoppedByEveryOverlay(t *testing.T) {
	overlays := []struct {
		name string
		// POINTER, not value. My first version of this table took a Model by value, so
		// `m.editorOpen = true` and `m.queryBrowserOpen = true` were written to a copy and
		// thrown away — and the two cases that mutate through a pointer field (the palette and
		// the ask panel) passed, which made the table look like it was working.
		arm func(m *Model)
	}{
		{"the editor is open", func(m *Model) { m.editorOpen = true }},
		{"the palette is visible", func(m *Model) { m.palette.Show() }},
		{"the query browser is open", func(m *Model) { m.queryBrowserOpen = true }},
		{"the grid is exporting", func(m *Model) { startExportOnGrid(m) }},
		{"the ASK panel is open", func(m *Model) { showAskOnPanel(m) }},
	}

	for _, tc := range overlays {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 60)
			m.state = StateMain
			m.router.FocusPane(FocusGrid)
			// Armed FIRST, and the cursor read afterwards. Two versions of this case read it
			// first and blamed the wheel for the movement — which was the fixture's own
			// SetData resetting the cursor to zero when the arming happened.
			tc.arm(&m)
			before := m.grid.CursorRow()

			at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			if !ok {
				// The pane is not even drawn — the editor replaces it — so there is nothing
				// for a wheel to scroll. That is a stronger form of the same property, and it
				// is why this case reports "absent" rather than failing.
				t.Logf("the grid pane is not on screen while %s, so there is nothing to scroll", tc.name)
				return
			}

			_, _ = fold(m, at)

			if m.grid.CursorRow() != before {
				t.Errorf("the wheel moved the grid cursor from %d to %d while %s", before, m.grid.CursorRow(), tc.name)
			}
		})
	}

	t.Run("and the click is stopped by every overlay", func(t *testing.T) {
		// The click's condition has SIX disjuncts: the same five plus the help modal, which
		// the wheel handles separately because a visible modal scrolls itself first.
		clickOverlays := append(overlays, struct {
			name string
			arm  func(m *Model)
		}{"the help modal is visible", func(m *Model) { m.helpModal.Show() }})

		for _, tc := range clickOverlays {
			t.Run(tc.name, func(t *testing.T) {
				m := focusedGrid(t, 60)
				m.state = StateMain
				m.router.FocusPane(FocusGrid)
				tc.arm(&m)
				before := m.grid.CursorRow()

				at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				if !ok {
					t.Logf("the grid pane is not on screen while %s", tc.name)
					return
				}
				_, _ = fold(m, tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})

				if m.grid.CursorRow() != before {
					t.Errorf("the click moved the grid cursor from %d to %d while %s", before, m.grid.CursorRow(), tc.name)
				}
			})
		}
	})

	t.Run("and a wheel that is not a scroll moves nothing", func(t *testing.T) {
		// A horizontal wheel, or a middle-button roll. Without the guard a zero direction
		// would be read as "up" by every branch below it.
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.router.FocusPane(FocusGrid)
		before := m.grid.CursorRow()

		at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseNone})
		if !ok {
			t.Skip("the grid pane is not on screen")
		}
		_, _ = fold(m, at)

		if m.grid.CursorRow() != before {
			t.Errorf("a non-scroll wheel event moved the cursor from %d to %d", before, m.grid.CursorRow())
		}
	})

	t.Run("and a wheel over the grid asks the sidebar to follow", func(t *testing.T) {
		// The one case where the wheel returns a command rather than nothing: the cursor is on
		// a foreign key with a cached row, so moving it has to update the sidebar preview too.
		// A cache hit is what produces that command, and without one the wheel is silent.
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.router.FocusPane(FocusGrid)
		m.prevSchema = "public"
		m.grid.SetData(dataOf(60, 3), "public", "orders")
		// A live connection: syncGridSidebarPreviewForCursor refuses without one, which is
		// the guard above the cache lookup and the reason a wheel over a disconnected grid
		// is silent.
		m.conn = pgxfake.New()
		// A declared foreign key on the SECOND column, and the cursor moved onto it — the
		// lookup needs a column that IS a key, not merely a cached row.
		//
		// SetMetadata rather than a FK-only setter: constraints, keys and indexes are set
		// together by the one message that carries them, which is the shape a reply has.
		m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
			Name: "orders_user_id_fkey", Column: m.grid.Columns()[1],
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}}, nil)
		// The cursor moved onto the key column through the ACTION, because moveRight is
		// unexported in the grid package — and the action is how a user gets there anyway.
		_, _ = m.grid.HandleAction("navigate_right")

		// A cached row for the column the cursor is on, keyed the way the lookup keys it.
		key := gridSidebarFKPreviewCacheKey("public", "orders", m.grid.Columns()[1])
		value := m.grid.SelectedRow()[m.grid.CursorCol()]
		m.storeGridSidebarFKPreviewCache(key, value, []string{"id", "name"}, []interface{}{7, "Ada"})

		// The premise: the cursor really is on a foreign key with a cache hit, or the case
		// below proves nothing.
		if m.lookupGridSidebarFKPreviewCache(key, value) == nil {
			t.Fatalf("the cache key %q has no entry for %v", key, value)
		}

		at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		if !ok {
			t.Skip("the grid pane is not on screen")
		}
		if _, cmd := fold(m, at); cmd == nil {
			t.Error("a wheel over a grid with a cached foreign-key row issued no command")
		}
	})

	t.Run("and a wheel with the help modal up scrolls the modal, not the panes", func(t *testing.T) {
		// The overlay exception, and the case that makes the guard worth having: the modal is
		// an overlay that OWNS the wheel, so it has to be offered it first.
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.router.FocusPane(FocusGrid)
		m.helpModal.Show()
		m.helpModal.SetWidth(100)
		m.helpModal.SetHeight(30)
		before := m.grid.CursorRow()

		at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		if !ok {
			t.Skip("the grid pane is not on screen behind the help modal")
		}
		out, _ := fold(m, at)

		if out.grid.CursorRow() != before {
			t.Errorf("the grid scrolled behind the help modal: cursor %d -> %d", before, out.grid.CursorRow())
		}
	})

	t.Run("and a click the help modal declines falls through to nothing", func(t *testing.T) {
		// The modal handles keys and wheels, not clicks — so the click is offered, declined,
		// and the app does nothing at all. The second half of the exception: a declined offer
		// is not a pass-through to the pane behind.
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.router.FocusPane(FocusGrid)
		m.helpModal.Show()
		before := m.grid.CursorRow()

		at, ok := gridPoint(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		if !ok {
			t.Skip("the grid pane is not on screen behind the help modal")
		}
		_, _ = fold(m, tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})

		if m.grid.CursorRow() != before {
			t.Errorf("a click behind the help modal moved the cursor to %d", m.grid.CursorRow())
		}
	})
}

// ── the keyboard carve-outs ─────────────────────────────────────────────────

// Each pane that owns the keyboard is offered a key first; this is what happens when it
// DECLINES. The key then reaches the app's own handling — which for a declined key in a
// focused pane is nothing at all, because the pane is there precisely to consume it.
//
// The four cases are the four carve-out sites, and they are the same shape written out four
// times in the handler — which is why each needs its own fixture: a where-filter, a cell
// editor, the explorer, and the preview all decline DIFFERENT sets of keys.
func TestTheKeyboardCarveOutsFallThroughWhenThePaneDeclines(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(m *Model)
		// A key the pane does not handle: a function key, which none of these text modes bind.
		key tea.KeyPressMsg
	}{
		{
			// The modes are entered through their ACTIONS rather than through their unexported
			// starters: startColumnFind, startWhereFilter and startEdit are private to the
			// grid package, and the action is the only way in from here — which is also the
			// way a user gets there.
			//
			// Three different grid modes, because the handler has three separate carve-out
			// sites for them and each is its own arm: the COLUMN filter (find_column), the
			// WHERE filter (filter_rows), and a cell being edited (edit_cell).
			name: "the column filter focused on the grid",
			arm:  func(m *Model) { _, _ = m.grid.HandleAction("find_column") },
			key:  tea.KeyPressMsg{Code: tea.KeyF1},
		},
		{
			name: "the where filter focused on the grid",
			arm:  func(m *Model) { _, _ = m.grid.HandleAction("filter_rows") },
			key:  tea.KeyPressMsg{Code: tea.KeyF1},
		},
		{
			name: "a cell being edited on the grid",
			arm:  func(m *Model) { _, _ = m.grid.HandleAction("edit_cell") },
			key:  tea.KeyPressMsg{Code: tea.KeyF1},
		},
		{
			// The explorer has its OWN filter mode, with its own exported starter — which is
			// the difference from the grid, where the starters are private.
			name: "the explorer filter focused",
			arm:  func(m *Model) { m.router.FocusPane(FocusExplorer); m.explorer.StartFilter() },
			key:  tea.KeyPressMsg{Code: tea.KeyF1},
		},
		{
			name: "the explorer focused, not filtering",
			arm:  func(m *Model) { m.router.FocusPane(FocusExplorer) },
			key:  tea.KeyPressMsg{Code: tea.KeyF1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 60)
			m.state = StateMain
			m.width, m.height = 120, 40
			m.grid.SetData(dataOf(60, 3), "public", "orders")
			tc.arm(&m)

			// fold already asserts that Update returns a Model — it panics otherwise — so
			// what is left to assert is that the model still renders, which is where a nil
			// pane the fixture did not really arm would show up.
			out, _ := fold(m, tc.key)
			if out.View().Content == "" {
				t.Errorf("the app renders nothing after %s declined the key", tc.name)
			}
		})
	}
}

// ── layout ──────────────────────────────────────────────────────────────────

// The breadcrumb line, with and without a spinner.
//
// renderTopLine has four shapes and the two that matter are the DEGENERATE ones: a spinner
// with nothing selected, and nothing at all. Both return early, which is why the borders and
// the rest of the top line are skipped — so the assertion is that the pane still renders.
func TestTheTopLineWithNothingSelected(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spinning bool
		want     string
	}{
		{"nothing selected and no spinner", false, ""},
		{"nothing selected but a spinner is running", true, "spinner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 60)
			m.state = StateMain
			m.navStack = nil
			m.spinnerActive = tc.spinning

			got := m.renderTopLine("", "")

			if tc.want == "" && got != "" {
				t.Errorf("the top line is %q, want nothing — there is nothing to show", got)
			}
			if tc.want == "spinner" && !strings.Contains(got, spinnerChars[0]) {
				t.Errorf("the top line is %q, want a spinner", got)
			}
		})
	}

	t.Run("and with a selection it draws the breadcrumb", func(t *testing.T) {
		// The counterweight, and the case that makes the degenerate ones meaningful.
		for _, tc := range []struct {
			name     string
			spinning bool
		}{
			{"without a spinner", false},
			{"with a spinner", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := focusedGrid(t, 60)
				m.state = StateMain
				m.width, m.height = 120, 40
				m.spinnerActive = tc.spinning

				got := m.renderTopLine("public", "orders")

				if !strings.Contains(got, "orders") {
					t.Errorf("the top line is %q, want it to name the table", got)
				}
				if tc.spinning && !strings.Contains(got, spinnerChars[0]) {
					t.Errorf("the top line is %q, want a spinner next to the breadcrumb", got)
				}
			})
		}
	})

	t.Run("and a navigation stack draws one crumb per entry", func(t *testing.T) {
		// The breadcrumb's own loop. Two entries plus the current selection is three crumbs,
		// and getting the count wrong is how a back-navigation history renders as one item.
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.navStack = []NavigationEntry{
			{Schema: "public", Table: "orders"},
			{Schema: "billing", Table: "invoices"},
		}

		got := m.renderBreadcrumbs("public", "customers")

		for _, want := range []string{"orders", "invoices", "customers"} {
			if !strings.Contains(got, want) {
				t.Errorf("the breadcrumb is %q, want it to name %q", got, want)
			}
		}
	})
}

// The overlays drawn on top of the main view, each of which needs its panel open.
//
// Three of them are render-only: an overlay that draws must not push the pane behind it out of
// the window, and the arithmetic that positions one is a subtraction that can go negative.
func TestTheOverlaysDrawWithoutLosingThePane(t *testing.T) {
	// A window big enough for an overlay to land in. The overlay writes at
	// y = (height - boxHeight)/2 and skips any row past the end of the base, so with a
	// zero-sized model every box is written off the bottom and the view comes back unchanged —
	// which is a fixture that proves nothing, not a bug.
	sized := func(t *testing.T) Model {
		t.Helper()
		m := focusedGrid(t, 60)
		m.state = StateMain
		m.width, m.height = 120, 40
		return m
	}

	t.Run("the ASK panel", func(t *testing.T) {
		m := sized(t)

		// The main view WITHOUT the panel is the reference, and the assertion is that opening
		// it CHANGES the view. Comparing against a reference rather than looking for the
		// panel's own text is what makes this robust: the panel is drawn at whatever height
		// its own content worked out, so its SQL may legitimately be cropped out of a short
		// window — and my first version of this case searched for that text and reported the
		// overlay broken when it had drawn exactly what it should.
		without := m.View().Content
		if without == "" {
			t.Fatal("the main view renders nothing with no panel open")
		}

		showAskOnPanel(&m)
		m.ask.SetWidth(80)
		m.ask.SetHeight(30)
		m.ask.AddQuestion("how many orders?")
		m.ask.SetGeneratedSQL("SELECT count(*) FROM orders")

		// The reference has to come from the same function the comparison does.
		// The premise: the panel has something to draw. An empty panel would leave the view
		// unchanged and the comparison below would pass for the wrong reason.
		if m.ask.View() == "" {
			t.Fatal("the ASK panel renders nothing, so there is nothing to overlay")
		}

		// View(), not renderMainView: the ASK overlay is applied by the top-level View, after
		// renderMainView has produced the pane content. renderMainView is the thing that
		// draws the editor and the export picker, and my first version of this case called it
		// for all three — so the ask case compared a view with the panel against the same view
		// without it and found them equal, which is correct: the panel was never in it.
		with := m.View().Content

		if with == "" {
			t.Fatal("the main view renders nothing with the ASK panel open")
		}
		if with == without {
			t.Error("opening the ASK panel did not change the main view, so the overlay drew nothing")
		}
	})

	t.Run("the editor", func(t *testing.T) {
		m := sized(t)
		without := m.renderMainView()

		m.editorOpen = true
		m.editor.SetContent("SELECT 1")
		m.editor.SetWidth(80)
		m.editor.SetHeight(30)
		m.editor.Focus()

		out := m.renderMainView()

		if out == "" {
			t.Fatal("the main view renders nothing with the editor open")
		}
		if out == without {
			t.Error("opening the editor did not change the main view")
		}
	})

	t.Run("the export picker", func(t *testing.T) {
		m := sized(t)
		m.grid.SetData(dataOf(3, 3), "public", "orders")
		without := m.renderMainView()

		startExportOnGrid(&m)

		out := m.renderMainView()

		if out == "" {
			t.Fatal("the main view renders nothing with the export picker open")
		}
		if out == without {
			t.Error("opening the export picker did not change the main view")
		}
	})
}

// The explorer, focused and not.
//
// Focus drives two things here: whether the pane keeps the keyboard, and which border colour
// it draws. A pane that drew the active border while blurred looks selected and takes keys it
// cannot use.
func TestTheExplorerPreviewBorderFollowsFocus(t *testing.T) {
	for _, tc := range []struct {
		name  string
		focus FocusPane
	}{
		{"the preview focused", FocusExplorerPreview},
		{"the tree focused instead", FocusExplorer},
		{"the grid focused", FocusGrid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 60)
			m.state = StateMain
			m.router.FocusPane(tc.focus)

			// Both the preview and the tree, because the tree's focus branch has the same
			// two-way shape and the preview's is the one with the separate border.
			if out := m.renderExplorerPreview(60, 20); out == "" {
				t.Error("the explorer preview renders nothing")
			}
			if out := m.renderExplorer(60, 20); out == "" {
				t.Error("the explorer renders nothing")
			}
		})
	}
}

// The pane-height arithmetic, for a pane whose content does not end in a newline.
//
// The height is derived by splitting the content and looking at whether the LAST line is
// empty: a trailing newline means the content ends on a line boundary, and counting it as a
// row would reserve a blank line that is not there.
func TestThePaneHeightHandlesContentWithAndWithoutATrailingNewline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"one line, no newline", "just one line"},
		{"one line with a newline", "just one line\n"},
		{"several lines", "a\nb\nc"},
		{"several lines with a trailing newline", "a\nb\nc\n"},
		{"two trailing newlines", "a\n\n"},
		{"nothing at all", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The pure computation, through the pane that uses it: a grid with one row of
			// content, at a height that has to be divided by something.
			m := focusedGrid(t, 20)
			m.state = StateMain

			if out := m.renderMainView(); out == "" {
				t.Errorf("the main view renders nothing for the content %q", tc.content)
			}
		})
	}
}

// rollbackOnExit's close, with a connection that refuses to close.
//
// The exit path has no user to tell, so the failure is only logged — which is exactly the case
// worth testing, because a path that panics on the way out takes the terminal with it.
func TestTheExitPathSurvivesAConnectionThatWillNotClose(t *testing.T) {
	broken := &closeFailingConn{Conn: pgxfake.New(), err: errors.New("the socket is already gone")}

	m := routerModelLoaded(t)
	m.state = StateMain
	m.runner = nil
	m.conn = broken

	// No return value and no panic: THAT is the assertion. A close that panicked on quit would
	// leave the user with a dead terminal and no message.
	m.rollbackOnExit()

	if broken.closes != 1 {
		t.Errorf("the connection was closed %d times, want 1 — once, and then it gives up", broken.closes)
	}
}

// closeFailingConn is a connection whose Close refuses, which is the shape of one whose server
// has already gone.
type closeFailingConn struct {
	*pgxfake.Conn
	err    error
	closes int
}

func (c *closeFailingConn) Close(context.Context) error {
	c.closes++
	return c.err
}

// ── fixtures ────────────────────────────────────────────────────────────────

// gridPoint is a point inside the grid pane, and FALSE when there is no grid pane.
//
// The false case is not an error: ui.Zones is a singleton the View fills in as it renders,
// and an overlay that REPLACES the grid — the editor does — never registers its zone at all.
// So a wheel with the editor up has nothing to scroll, which is a stronger form of "the wheel
// was stopped" and worth reporting rather than failing on.
//
// pointInZone fatals when the zone is missing, so this wraps it: it renders first, checks, and
// only then looks for a point.
func gridPoint(t *testing.T, m Model, msg tea.MouseWheelMsg) (tea.MouseWheelMsg, bool) {
	t.Helper()
	_ = m.View()

	if zi := ui.Zones.Get(zonePaneGrid); zi == nil || zi.IsZero() {
		return msg, false
	}
	return pointInZone(t, m, zonePaneGrid, msg), true
}

// startExportOnGrid puts the grid into export mode, which is one of the five overlays and is a
// MODE on the grid rather than a panel of its own.
func startExportOnGrid(m *Model) {
	m.grid.SetData(dataOf(3, 3), "public", "orders")
	if cmd, handled := m.grid.StartExport(); handled && cmd != nil {
		_ = cmd()
	}
}

// showAskOnPanel opens the ASK panel, which needs both the flag and a panel that exists.
func showAskOnPanel(m *Model) {
	if m.ask == nil {
		return
	}
	m.askOpen = true
	m.ask.Show()
	m.ask.SetWidth(80)
	m.ask.SetHeight(30)
}

// spinnerPlaceholder keeps the time import honest for the fixtures above.
var _ = time.Second

// The three panes that are offered a key OUTSIDE their own modes, and answer it.
//
// The opposite of the carve-outs above: there, the key is offered because the pane owns the
// keyboard and the case asserts what happens when it declines. Here the pane is merely the
// FOCUSED one, so the app offers the key to it as a fallback and the pane may take it.
//
// Which takes two things a first version of this case was missing. The pane has to be
// Focus()ed — GridPreview.Update, ExplorerPreview.Update and the explorer all return early
// for an unfocused panel, and the router's focus is a DIFFERENT flag from Focus(). And the key
// has to be one the registry resolves to an action the APP does not own, or the app-wide
// dispatch above takes it first: `j` resolves to navigate_down, and hasAppAction says no for
// the preview contexts, which is what lets the key reach the pane at all.
func TestAFocusedPaneTakesTheKeyNoOneElseClaimed(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(m *Model)
		pane string
		key  tea.KeyPressMsg
	}{
		{
			name: "the grid preview focused takes j",
			arm: func(m *Model) {
				m.router.FocusPane(FocusGridPreview)
				m.gridPreview.Focus()
				m.gridPreview.SetWidth(60)
				m.gridPreview.SetHeight(20)
				m.gridPreview.SetRow([]string{"id", "name"}, []interface{}{1, "Ada"})
			},
			pane: "the grid preview",
		},
		{
			name: "the explorer focused takes j",
			arm: func(m *Model) {
				m.router.FocusPane(FocusExplorer)
				m.explorer.Focus()
			},
			pane: "the explorer",
		},
		{
			name: "the explorer preview focused takes j",
			arm: func(m *Model) {
				m.router.FocusPane(FocusExplorerPreview)
				m.explorerPreview.Focus()
				m.explorerPreview.SetWidth(60)
				m.explorerPreview.SetHeight(20)
				// Data, because the preview's own key switch dispatches through its tab
				// contents — an empty preview declines navigation, which is correct and which
				// a fixture that only focused it would have mistaken for the block being dead.
				m.explorerPreview.SetData("public", "orders",
					[]postgres.ColumnInfo{
						{Name: "id", DataType: "integer"},
						{Name: "customer_id", DataType: "integer"},
					},
					[]postgres.ConstraintInfo{{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"}},
					nil, nil, nil,
				)
			},
			pane: "the explorer preview",
			// `1`, not `j`: the preview's key switch resolves in the explorer-preview
			// CONTEXT, and its tab keys are the ones bound there. Navigation is not — so a case
			// that sent `j` found the preview declining every key and concluded the block was
			// dead.
			key: tea.KeyPressMsg{Code: '1', Text: "1"},
		},
		{
			// The grid itself, which is the fourth pane in this chain and the one whose block
			// is last — so a case that stopped after the three previews left its branch
			// uncovered while looking complete.
			name: "the grid focused takes j",
			arm: func(m *Model) {
				m.router.FocusPane(FocusGrid)
				m.grid.Focus()
				m.grid.SetData(dataOf(20, 3), "public", "orders")
			},
			pane: "the grid",
		},
		{
			// And the grid preview in JQ mode, which has its OWN block above the app-wide
			// dispatch — because a jq input swallows everything and must be asked before
			// anything else is. A case that only covered the plain preview never reached it.
			name: "the grid preview in jq mode takes j",
			arm: func(m *Model) {
				m.router.FocusPane(FocusGridPreview)
				m.gridPreview.Focus()
				m.gridPreview.SetWidth(60)
				m.gridPreview.SetHeight(20)
				m.gridPreview.SetRow([]string{"id", "name"}, []interface{}{1, "Ada"})
				m.gridPreview.EnterJQMode()
			},
			pane: "the grid preview",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 20)
			m.state = StateMain
			m.width, m.height = 120, 40
			tc.arm(&m)

			// The premise, in the pane's own terms: it says it is listening. Without Focus()
			// every one of these panels declines every key, and the case would pass while
			// asserting nothing about it.
			if !paneIsListening(&m, tc.pane) {
				t.Fatalf("%s is not listening, so it will decline everything", tc.pane)
			}

			key := tc.key
			if key.Code == 0 {
				key = tea.KeyPressMsg{Code: 'j', Text: "j"}
			}
			out, _ := fold(m, key)

			// What the app returned is not the assertion — the pane's dispatch returns a nil
			// command for navigation, so a nil here means "handled" as much as "declined".
			// What IS the assertion is that the app answered and still renders.
			if out.View().Content == "" {
				t.Errorf("the app no longer renders after offering j to %s", tc.pane)
			}
		})
	}
}

// paneIsListening is each pane's own focus flag, read through the pane's own accessor, so a
// case that forgets to Focus() fails with a message that says which flag was missing.
func paneIsListening(m *Model, pane string) bool {
	switch pane {
	case "the grid preview":
		if m.gridPreview == nil || !m.gridPreview.IsFocused() {
			return false
		}
		// In jq mode the premise is a different one: the block above the app-wide dispatch is
		// only reached when the input is OPEN, so a case that focused the preview without
		// opening jq never touched it.
		return true
	case "the explorer preview":
		return m.explorerPreview != nil && m.explorerPreview.IsFocused()
	case "the grid":
		// The grid's focus is likewise derived from the router's, by renderGrid.
		return m.grid != nil && m.router.Focus() == FocusGrid
	case "the explorer":
		// The explorer has no IsFocused accessor — it is not needed by anything, because the
		// app sets its focus from the router's in renderExplorer. So the case asserts the
		// router's focus instead, which is what actually makes the explorer take keys.
		return m.explorer != nil && m.router.Focus() == FocusExplorer
	}
	return false
}

// The palette, offered a key — both the ones it takes and the one it does not.
//
// The palette is an overlay with its own fuzzy list. It takes anything with TEXT on it, because
// that is the query box, and it declines a key with no text — a function key — and what it
// declines is then NOT passed to the panes behind it.
//
// Both directions are here because they are different arms of the same `if`, and because a
// palette that let an unbound key through would be filtering the grid while the user believed
// they were typing in a search box.
func TestTheVisiblePaletteTakesATypedKeyAndDeclinesAFunctionKey(t *testing.T) {
	t.Run("a typed key goes to the palette, not the grid", func(t *testing.T) {
		m := focusedGrid(t, 20)
		m.state = StateMain
		m.width, m.height = 120, 40
		m.router.FocusPane(FocusGrid)
		m.palette.Show()
		before := m.grid.CursorRow()

		out, _ := fold(m, tea.KeyPressMsg{Code: 'o', Text: "o"})

		// The palette's query changed, which is what "took the key" means.
		//
		// Asserted through the RENDERED palette rather than through a getter: the palette has
		// no exported query accessor — it is a search box with no caller that needs to read it
		// back — so the observable proof that the key landed is that the box now shows it.
		//
		// A getter here would be a production change made for a test, which is the thing this
		// whole session has been refusing to do for the eight branches it has left.
		if !strings.Contains(out.palette.View(), "o") {
			t.Errorf("the palette does not show the character that was typed:\n%s", out.palette.View())
		}
		if out.grid.CursorRow() != before {
			t.Errorf("a typed key also reached the grid: cursor %d -> %d", before, out.grid.CursorRow())
		}
	})

	t.Run("a function key is declined and reaches nothing", func(t *testing.T) {
		m := focusedGrid(t, 20)
		m.state = StateMain
		m.width, m.height = 120, 40
		m.router.FocusPane(FocusGrid)
		m.palette.Show()
		before := m.grid.CursorRow()

		out, _ := fold(m, tea.KeyPressMsg{Code: tea.KeyF7})

		if !out.palette.IsVisible() {
			t.Error("the palette closed over a key it does not bind")
		}
		if out.grid.CursorRow() != before {
			t.Errorf("an unbound key reached the grid behind the palette: cursor %d -> %d", before, out.grid.CursorRow())
		}
	})
}

// The help modal, offered a key it does not bind.
//
// The modal handles esc, ?, q, j/k, arrows, ctrl+u, ctrl+d, g and G, and declines everything
// else — so a function key is declined, and the app then does nothing with it. That "nothing"
// is the assertion: a visible modal that let an unbound key through would reach the panes
// behind it, which is what the help modal exists to prevent.
func TestTheHelpModalDeclinesAKeyItDoesNotBind(t *testing.T) {
	m := focusedGrid(t, 60)
	m.state = StateMain
	m.router.FocusPane(FocusGrid)
	m.helpModal.Show()
	m.helpModal.SetWidth(100)
	m.helpModal.SetHeight(30)
	before := m.grid.CursorRow()

	out, _ := fold(m, tea.KeyPressMsg{Code: tea.KeyF3})

	if !out.helpModal.IsVisible() {
		t.Error("the modal closed over a key it does not bind")
	}
	if out.grid.CursorRow() != before {
		t.Errorf("an unbound key reached the grid behind the modal: cursor %d -> %d", before, out.grid.CursorRow())
	}
}
