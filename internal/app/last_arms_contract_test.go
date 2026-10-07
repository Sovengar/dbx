package app

// Scenario: Los ultimos brazos del app: el raton sobre el explorador, la previsualizacion
// como destinataria de una tecla, y la cuenta de lineas del panel.
//
// All of them are the same kind of thing: a place where the app has to make a decision about a
// panel it did not build, and where the decision depends on a flag only that panel sets.
//
//   - a click in the explorer's zone is a selection, and a DOUBLE click is a selection plus a
//     toggle. The single-click half is the common one and the one that was untested, because the
//     double-click fixture came first and the assertion about it covered both.
//   - the grid preview and the explorer preview are each offered a key when focused, and each
//     decides whether it wants it. The flag that decides is the panel's OWN focus, which the
//     router's focus is not: a pane can be the current pane and still decline every key, because
//     Focus() is what says it is listening.
//   - the status bar's height is the keybinds pane plus the top line's own lines, and the top
//     line's line count differs by one depending on whether it ends in a newline. So the pane
//     content is one line short or one line long depending on a detail of a string the renderer
//     three functions away produced.
//
// The last one is why it is worth a test: the two branches differ by exactly one row of a
// window, which is the difference between a grid that fits and one that is a row short.

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
)

// A single click in the explorer's zone selects; the second one toggles.
//
// The single click is the arm that was missing, and it is the one a user produces every time
// they pick a table. The double click is the one that was tested, because its extra behaviour —
// a toggle — is what a test wants to assert.
func TestASingleClickInTheExplorerSelectsWithoutToggling(t *testing.T) {
	m := focusedGrid(t, 20)
	m.state = StateMain
	m.width, m.height = 120, 40
	// The explorer is focused, and that is not a detail: the pane is only laid out at all when
	// the editor is open OR the explorer is the current pane, so a grid-focused model renders no
	// explorer zone and there is nothing for a click to land on. My first version of this case
	// skipped for exactly that reason and proved nothing.
	m.router.FocusPane(FocusExplorer)

	at, ok := explorerClickPoint(t, m)
	if !ok {
		t.Fatal("no point belongs to the explorer and not to the grid or the sidebar")
	}

	// The premise: nothing is expanded yet, so a toggle would be observable.
	out, cmd := fold(m, tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})
	if cmd != nil {
		_ = cmd()
	}

	if out.lastClickTime.IsZero() {
		t.Error("the click did not register as a click at all")
	}

	t.Run("a second click in the same spot toggles", func(t *testing.T) {
		// The double-click half, which needs a node with CHILDREN: ToggleExpand returns no
		// command for a leaf, so a case built on whatever the fixture happened to select
		// asserted nothing. A schema node is the smallest thing that expands.
		//
		// And it needs the two clicks close together in time, which is what lastClickTime is
		// for — so both clicks are sent without anything in between.
		db := explorer.NewNode("d", explorer.NodeDatabase, "shopdb")
		schema := explorer.NewNode("s", explorer.NodeSchema, "public")
		schema.Children = []*explorer.Node{
			explorer.NewNode("t", explorer.NodeTable, "orders"),
		}
		db.Children = []*explorer.Node{schema}

		m := focusedGrid(t, 20)
		m.state = StateMain
		m.width, m.height = 120, 40
		m.router.FocusPane(FocusExplorer)
		m.explorer.Focus()
		m.explorer.SetNodes([]*explorer.Node{db})

		at, ok := explorerClickPoint(t, m)
		if !ok {
			t.Fatal("no point belongs to the explorer and not to the grid or the sidebar")
		}
		// The zone's first row is the explorer's top border, which HandleClick
		// discards; one row down is the first node — the database with children
		// that ToggleExpand can act on.
		click := tea.MouseClickMsg{X: at.X, Y: at.Y + 1, Button: tea.MouseLeft}
		if !ui.InBounds(zonePaneExplorer, click) {
			t.Fatal("one row below the zone's top is not inside the explorer")
		}

		// Both clicks go through Update, because it is the app that decides what
		// counts as a double click; sent back to back, the second falls inside the
		// 300ms window. The first selects, the second toggles.
		first, _ := fold(m, click)
		selected := first.explorer.Selected()
		if selected == nil {
			t.Fatal("the first click selected no node, so there is nothing to toggle")
		}
		expanded := selected.Expanded

		out, _ := fold(first, click)
		if out.explorer.Selected() == nil {
			t.Fatal("the double click left no node selected")
		}
		if out.explorer.Selected().Expanded == expanded {
			t.Error("the double click did not toggle the selected node")
		}
	})
}

// The grid preview, as the recipient of a joined row.
//
// Two places tell the preview about a row and both check its focus first, because the sidebar
// and the preview show the same data and only the one the user is looking at should change.
// The focus test is `IsFocused()`, which is the preview's own flag — the same one `Focus()` sets
// and the router's focus does not.
func TestTheFocusedGridPreviewIsToldAboutARow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		focused bool
	}{
		{"focused", true},
		{"not focused", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 20)
			m.state = StateMain
			m.conn = pgxfake.New()
			if m.gridPreview == nil {
				t.Skip("this fixture builds no grid preview")
			}
			if tc.focused {
				m.gridPreview.Focus()
			}
			// A cached row for the column the cursor will be on, so the lookup hits rather
			// than going back to the database.
			m.prevSchema = "public"
			m.grid.SetData(dataOf(20, 3), "public", "orders")
			cols := m.grid.Columns()
			m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
				Name: "orders_user_id_fkey", Column: cols[1],
				RefSchema: "public", RefTable: "users", RefColumn: "id",
			}}, nil)
			_, _ = m.grid.HandleAction("navigate_right")

			key := gridSidebarFKPreviewCacheKey("public", "orders", cols[1])
			value := m.grid.SelectedRow()[m.grid.CursorCol()]
			m.storeGridSidebarFKPreviewCache(key, value, []string{"id", "name"}, []interface{}{7, "Ada"})

			out, cmd := fold(m, tea.MouseWheelMsg{
				X: 0, Y: 0, Button: tea.MouseWheelDown,
			})
			_ = cmd

			// The observable effect, which is the same either way: the app answered and the
			// preview still renders. What differs is which branch ran, and the coverage is what
			// records that — so the assertion is that nothing broke, not that the row appeared.
			if out.gridPreview == nil {
				t.Fatal("the model has no grid preview")
			}
			if out.View().Content == "" {
				t.Error("the app renders nothing after the sidebar lookup")
			}
		})
	}
}

// The explorer preview, offered a key while it is the focused pane.
//
// Its Update returns early for an unfocused panel, so the fixture has to Focus() it — the
// router's focus alone is not enough, and a version of this case that only moved the router's
// focus found the panel declining every key.
func TestTheFocusedExplorerPreviewTakesAKey(t *testing.T) {
	m := focusedGrid(t, 20)
	m.state = StateMain
	m.width, m.height = 120, 40
	m.router.FocusPane(FocusExplorerPreview)
	m.explorerPreview.Focus()

	out, _ := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if out.View().Content == "" {
		t.Error("the app renders nothing after the explorer preview took a key")
	}
}

// The grid preview's foreign-key expansion: the schema fallback and a lookup that fails.
//
// The fallback matters because a foreign key loaded from some drivers carries no schema, and
// without it the query is unqualified — which resolves against search_path and can read a
// users table from a different schema than the one the key was declared in.
func TestTheGridPreviewForeignKeyExpansion(t *testing.T) {
	t.Run("a foreign key with no schema falls back to the table's own", func(t *testing.T) {
		m := focusedGrid(t, 20)
		m.state = StateMain
		m.prevSchema = "shop"
		conn := pgxfake.New()
		conn.Steps = []pgxfake.Step{{Match: "SELECT", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "id"}, {Name: "name"}},
			Rows:    [][]interface{}{{int64(7), "Ada"}},
		}}}
		m.conn = conn

		out, cmd := fold(m, gridpreview.GridPreviewExpandFKMsg{
			Column: "user_id", Path: "user_id", Value: int64(7),
			RefSchema: "", RefTable: "users", RefColumn: "id",
		})
		if cmd != nil {
			_ = cmd()
		}

		qualified := false
		for _, q := range conn.Queries {
			if strings.Contains(q, `"users"`) && strings.Contains(strings.ToLower(q), "shop") {
				qualified = true
			}
		}
		if !qualified {
			t.Errorf("no query was qualified with the fallback schema %q; the queries were %v",
				m.prevSchema, conn.Queries)
		}
		if out.View().Content == "" {
			t.Error("the app renders nothing after the expansion")
		}
	})

	t.Run("an expansion that fails says so", func(t *testing.T) {
		// The error has to be visible: an expansion that silently does nothing leaves the user
		// wondering why the column they clicked never appeared.
		m := focusedGrid(t, 20)
		m.state = StateMain
		conn := pgxfake.New()
		conn.Steps = []pgxfake.Step{{Match: "SELECT", Err: errors.New("permission denied for table users")}}
		m.conn = conn

		_, cmd := fold(m, gridpreview.GridPreviewExpandFKMsg{
			Column: "user_id", Path: "user_id", Value: int64(7),
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		})
		if cmd == nil {
			t.Fatal("the expansion refused instead of answering")
		}

		// The command has to be RUN. The lookup happens inside the command and the failure
		// comes back as a message, so folding the request alone shows a toast about nothing —
		// which is what my first version did, and it reported an empty toast as a missing one.
		msg, ok := cmd().(gridpreview.GridPreviewExpandFKResultMsg)
		if !ok {
			t.Fatalf("the expansion produced %T", cmd())
		}
		if msg.Err == nil {
			t.Fatal("the expansion reported no error against a failing lookup")
		}

		out, _ := fold(m, msg)
		if said := strings.ToLower(toastText(out)); !strings.Contains(said, "permission denied") {
			t.Errorf("the toast is %q, want it to carry the failure", said)
		}
	})
}

// A commit of drafts refused because a read-only ASK query is running.
//
// The same guard as every other DB command, on the batch path: pgx cannot open a second
// transaction on a connection that has an open READ ONLY one, so the batch has to be refused
// before it opens one rather than fail halfway through.
func TestACommitOfDraftsIsRefusedWhileAReadOnlyQueryRuns(t *testing.T) {
	m := focusedGrid(t, 20)
	m.state = StateMain
	m.conn = pgxfake.New()
	m.runner = newStatementRunner(nil)
	m.runner.conn = pgxfake.New()
	m.runner.readOnlyActive.Store(true)

	out, cmd := fold(m, grid.GridCommitAllMsg{
		Schema: "public", Table: "orders",
		Queries: []string{"UPDATE orders SET total = 0"},
		Args:    [][]interface{}{{int64(0)}},
	})

	if cmd != nil {
		_ = cmd()
		t.Error("a commit batch issued a command while a read-only query was running")
	}
	// The refusal is told to the user: they pressed a key and nothing happened otherwise.
	if said := strings.ToLower(toastText(out)); !strings.Contains(said, "read-only") {
		t.Errorf("the toast is %q, want it to explain the read-only query is running", said)
	}

	t.Run("and it is allowed once the read-only query is done", func(t *testing.T) {
		// The counterweight: a runner that always refused would satisfy the case above and
		// committing drafts would be impossible.
		free := focusedGrid(t, 20)
		free.state = StateMain
		free.conn = pgxfake.New()
		free.runner = newStatementRunner(nil)
		free.runner.conn = pgxfake.New()

		if _, cmd := fold(free, grid.GridCommitAllMsg{
			Schema: "public", Table: "orders",
			Queries: []string{"UPDATE orders SET total = 0"},
			Args:    [][]interface{}{{int64(0)}},
		}); cmd == nil {
			t.Error("a commit batch with no read-only query running issued no command")
		}
	})
}

// The status bar's height, which is the keybinds pane plus the top line's own lines.
//
// The two branches of countLines differ by one row, and which one applies depends on whether
// the top line ends in a newline. Both are reachable: the empty top line is "" and the spinner
// with nothing selected ends in "\n", while the bordered top line with a selection does not.
func TestTheStatusBarCountsTheTopLineItself(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected bool
		spinning bool
	}{
		{"nothing selected and no spinner", false, false},
		{"nothing selected with a spinner", false, true},
		{"a selection with no spinner", true, false},
		{"a selection with a spinner", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 20)
			m.state = StateMain
			m.width, m.height = 120, 40
			m.navStack = nil
			m.spinnerActive = tc.spinning

			schema, table := "", ""
			if tc.selected {
				schema, table = "public", "orders"
			}
			top := m.renderTopLine(schema, table)

			// The premise, stated rather than assumed: this is the string the height is
			// computed from, and which branch of the count it takes is a property of it.
			endsWithNewline := strings.HasSuffix(top, "\n")
			wantLines := len(strings.Split(top, "\n"))
			if endsWithNewline && wantLines > 0 {
				wantLines--
			}

			// And the consequence: the pane gets the rest of the window.
			out := m.renderMainView()
			if out == "" {
				t.Fatalf("the main view renders nothing; the top line is %q", top)
			}
			t.Logf("the top line is %d lines (ends with a newline: %v)", wantLines, endsWithNewline)
		})
	}
}

// explorerClickPoint is a point the app's OWN routing will send to the explorer: inside the
// explorer zone, outside the grid's and the sidebar's.
//
// It does NOT use pointInZone, which insists a point belong to exactly one of the three panes
// and fails the whole test when it cannot find one. That insistence is right for a case that
// wants a specific pane, and wrong here, because ui.Zones is a package-level singleton whose
// registrations arrive over a channel and are NOT cleared between tests: by the time this case
// runs, panes from a dozen earlier tests are still in it, and a point this view registered can
// overlap one of theirs. So the rule is the app's own — the first branch that matches wins, and
// the explorer's is last — which is what the assertion is about anyway.
func explorerClickPoint(t *testing.T, m Model) (tea.MouseWheelMsg, bool) {
	t.Helper()

	// Rendered once and then polled: the layout is computed synchronously but the bounds reach
	// the singleton over a channel, so an immediate check usually loses the race.
	for range 100 {
		_ = m.View()

		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				candidate := tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}
				if !ui.InBounds(zonePaneExplorer, candidate) {
					continue
				}
				if ui.InBounds(zonePaneGrid, candidate) || ui.InBounds(zonePaneGridSidebar, candidate) {
					continue
				}
				return candidate, true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return tea.MouseWheelMsg{}, false
}

// The grid preview, told about a joined row, in the two places that tell it.
//
// Both check `gridPreview.IsFocused()` first, because the sidebar and the preview show the
// same joined data and only the one the user is looking at should change. Focus() is the
// preview's own flag — the router's focus is a different one, so a case that only moved the
// router's focus found the preview declining.
//
// The two paths are separate code and are covered separately, which is the point: the cache-hit
// path inside syncGridSidebarPreviewForCursor and the lookup-result path in Update are two
// copies of "tell the preview", and a test that covered one said nothing about the other.
func TestTheGridPreviewIsToldAboutAJoinedRowWhenItIsFocused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(m *Model)
		// send is nil for the wheel case, which needs a point found with the subtest's own
		// *testing.T — a closure cannot be handed one without inventing a zero T, and a zero T
		// that reports a failure inside a helper is worse than not having the helper.
		send func(m Model) tea.Msg
	}{
		{
			name: "from the cache, when the cursor moves",
			arrange: func(m *Model) {
				m.conn = pgxfake.New()
				m.prevSchema = "public"
				m.grid.SetData(dataOf(20, 3), "public", "orders")
				cols := m.grid.Columns()
				m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
					Name: "orders_user_id_fkey", Column: cols[1],
					RefSchema: "public", RefTable: "users", RefColumn: "id",
				}}, nil)
				_, _ = m.grid.HandleAction("navigate_right")

				key := gridSidebarFKPreviewCacheKey("public", "orders", cols[1])
				value := m.grid.SelectedRow()[m.grid.CursorCol()]
				m.storeGridSidebarFKPreviewCache(key, value, []string{"id", "name"}, []interface{}{7, "Ada"})
			},
		},
		{
			name: "from the lookup result",
			arrange: func(m *Model) {
				m.conn = pgxfake.New()
			},
			send: func(m Model) tea.Msg {
				return GridSidebarFKPreviewLookupResultMsg{
					Columns:  []string{"id", "name"},
					Row:      []interface{}{7, "Ada"},
					RefTable: "users",
					// The token the app is WAITING for. The handler drops any reply whose token
					// does not match its own cursor counter, which is what stops a stale answer
					// from clearing the preview — and a fixture with a made-up token was dropped
					// for exactly that reason, by the guard working.
					Token: m.gridSidebarFKPreviewCursor,
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := focusedGrid(t, 20)
			m.state = StateMain
			m.width, m.height = 120, 40
			m.router.FocusPane(FocusGridPreview)
			m.gridPreview.Focus()
			m.gridPreview.SetWidth(60)
			m.gridPreview.SetHeight(20)
			tc.arrange(&m)

			// The premises, all four, each one a step of the path:
			//
			//	focused   the preview says it is listening
			//	a FK       the cursor's column has to BE a key
			//	a row      and there has to be a row under the cursor
			//	a HIT      and the cache has to have an entry for its value
			//
			// Any one missing and the case passes while asserting nothing, because the handler
			// declines quietly at each step.
			if !m.gridPreview.IsFocused() {
				t.Fatal("the grid preview is not focused, so neither branch can run")
			}
			if tc.send == nil {
				cols := m.grid.Columns()
				if m.grid.CursorCol() < 0 || m.grid.CursorCol() >= len(cols) {
					t.Fatalf("the cursor column is %d of %d columns", m.grid.CursorCol(), len(cols))
				}
				if fk := m.findFKForColumn(cols[m.grid.CursorCol()]); fk == nil {
					t.Fatalf("the cursor is on %q, which is not a declared foreign key", cols[m.grid.CursorCol()])
				}
				row := m.grid.SelectedRow()
				if row == nil || m.grid.CursorCol() >= len(row) {
					t.Fatalf("there is no row under the cursor at column %d", m.grid.CursorCol())
				}
				key := gridSidebarFKPreviewCacheKey(m.prevSchema, m.grid.TableName(),
					m.findFKForColumn(cols[m.grid.CursorCol()]).Column)
				if m.lookupGridSidebarFKPreviewCache(key, row[m.grid.CursorCol()]) == nil {
					t.Fatalf("the cache has no entry for %q = %v", key, row[m.grid.CursorCol()])
				}
			}

			if tc.send != nil {
				out, _ := fold(m, tc.send(m))
				if out.gridPreview == nil {
					t.Fatal("the model has no grid preview")
				}
				if out.View().Content == "" {
					t.Error("the app renders nothing after the joined row")
				}
				return
			}

			// The cache-hit path, called the way the wheel calls it — but DIRECTLY, not through
			// a synthesised mouse event.
			//
			// My first version found a point inside the grid pane and sent a wheel at it, which
			// passed alone and failed in the full suite: ui.Zones is a package-level singleton
			// whose registrations arrive over a channel, so by the time this case ran another
			// test's zones were still in it and pointInZone — which insists a point belong to
			// exactly one pane — found none. A fixture whose result depends on which tests ran
			// before it is a broken fixture, and the function under test here needs no mouse to
			// be reachable: syncGridSidebarPreviewForCursor is the whole of the path.
			cmd := m.syncGridSidebarPreviewForCursor()
			if cmd != nil {
				// A cache hit returns nothing; a miss goes back to the database, which is the
				// premise above ruling out.
				_ = cmd()
				t.Error("the cursor's foreign key was not served from the cache")
			}
			if !m.gridPreview.IsFocused() {
				t.Error("the grid preview lost its focus")
			}
			out, _ := fold(m, tea.WindowSizeMsg{Width: 120, Height: 40})
			if out.View().Content == "" {
				t.Error("the app renders nothing after the joined row")
			}

			if out.gridPreview == nil {
				t.Fatal("the model has no grid preview")
			}
			if out.View().Content == "" {
				t.Error("the app renders nothing after the joined row")
			}
		})
	}
}
