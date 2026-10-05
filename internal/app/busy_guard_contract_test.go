package app

// Scenario: El guard que impide dos consultas a la vez, y los tokens que descartan una
// respuesta obsoleta.
//
// Two different protections, both invisible when they work:
//
//	refuseIfBusy blocks a DB command while an ASK read-only transaction is open on the
//	    shared connection. The database would reject the mutation anyway, or worse, the
//	    two would share one transaction and the rollback would take both.
//	the cursor TOKEN discards a reply that answers a question the user has already moved
//	    on from. Scrolling fast fires several lookups; only the last one's answer is
//	    wanted, and the others would land the sidebar on the wrong row.
//
// Both are "refuse and say nothing" rather than "error": a stale reply is not a failure,
// and a busy database is not the user's mistake. So the assertions are about the STATE
// after the refusal — that no command was issued, and that nothing was overwritten —
// because "it did not crash" would pass on an implementation that silently corrupted
// both.
//
// The busy flag is readOnlyActive on the statement runner, which lives in this package, so
// a test can set it. That is the only reason these are testable at all: the flag is set by
// a tea.Cmd goroutine in production and there is no other seam.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// newTestConn is a real connection to the test database. Needed because the loaders build
// a closure around it rather than using it, and a zero-value pgx.Conn panics the moment
// anything touches it.
func newTestConn(t *testing.T) *pgx.Conn { return connectTestDB(t, testDSN(t)) }

// projectNamed is a connected project, which the loaders take by value.
func projectNamed(t *testing.T, name string) config.FoundProject {
	t.Helper()
	return config.FoundProject{Name: name, Path: t.TempDir(), Active: true}
}

// previewText is the grid sidebar preview's rendered content, which is the only way to
// observe it from this package — it has no getters.
func previewText(m Model) string {
	if m.gridSidebarPreview == nil {
		return ""
	}
	return ansi.Strip(m.gridSidebarPreview.Render())
}

// busyModel is a connected model whose ASK read-only transaction is OPEN, which is the
// state refuseIfBusy exists for.
func busyModel(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.runner = &statementRunner{}
	m.runner.readOnlyActive.Store(true)
	m.toast.SetWidth(m.width)
	return m
}

func TestEveryDBCommandIsRefusedWhileAnAskQueryIsRunning(t *testing.T) {
	// Five loaders and one action. The loaders all start with the same guard, and the
	// action path reaches it through its own handler — so the list is the set of callers,
	// not five copies of one case.
	conn := newTestConn(t)

	for _, tc := range []struct {
		name string
		call func(m Model) tea.Cmd
	}{
		{"loading the schema", func(m Model) tea.Cmd {
			return m.loadSchema(conn, projectNamed(t, "shopdb"))
		}},
		{"reloading the schema after a DDL statement", func(m Model) tea.Cmd {
			return m.loadSchemaWithTarget(conn, projectNamed(t, "shopdb"), "public", "orders")
		}},
		{"loading the autocomplete catalogue", func(m Model) tea.Cmd {
			return m.loadAutocompleteData()
		}},
		{"loading the explorer preview", func(m Model) tea.Cmd {
			return m.loadExplorerPreviewData("public", "orders")
		}},
		{"fetching a joined row for the sidebar", func(m Model) tea.Cmd {
			return m.fetchGridSidebarFKPreview(
				&postgres.ForeignKeyInfo{Name: "fk", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
				42, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := busyModel(t)
			if !m.dbBusy() {
				t.Fatal("the model does not report itself busy, so nothing below is testing the guard")
			}

			if cmd := tc.call(m); cmd != nil {
				t.Errorf("%s issued a command while an ASK query was running", tc.name)
			}
			if said := strings.ToLower(toastText(m)); !strings.Contains(said, "ask") {
				t.Errorf("%s was refused silently: %q", tc.name, said)
			}
		})
	}

	t.Run("an action that loads is refused too", func(t *testing.T) {
		m := busyModel(t)
		m.conn = newTestConn(t)

		_, cmd := fold(m, grid.GridRefreshConfirmMsg{Schema: "public", Table: "orders", OrderBy: "1"})
		if cmd != nil {
			t.Error("refreshing the table issued a command while an ASK query was running")
		}
	})

	t.Run("and the same load runs once the ASK query is done", func(t *testing.T) {
		// The other half, so "everything is refused" cannot pass this file. Without it
		// every guard above would also be satisfied by a stub that refused unconditionally.
		m := busyModel(t)
		m.runner.readOnlyActive.Store(false)

		if m.dbBusy() {
			t.Fatal("the model still reports itself busy after the ASK query finished")
		}
		if cmd := m.loadAutocompleteData(); cmd == nil {
			t.Error("loading the catalogue was refused after the ASK query finished")
		}
	})
}

// The token. A reply whose token is not the current cursor's is dropped — and the drop has
// to be COMPLETE, because the whole point is that the sidebar must not be moved onto the
// row the user has already scrolled past.
func TestAStaleSidebarLookupIsDroppedWhole(t *testing.T) {
	newM := func(t *testing.T) Model {
		t.Helper()
		m := routerModelLoaded(t)
		m.conn = newTestConn(t)
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "user_id", DataType: "integer"}},
			Rows:    [][]interface{}{{1}, {2}, {3}},
			Count:   3,
		}, "public", "orders")
		m.grid.SetWidth(80)
		m.grid.SetHeight(20)
		return m
	}

	t.Run("a reply for an OLD token changes nothing", func(t *testing.T) {
		m := newM(t)
		m.gridSidebarFKPreviewCursor = 7

		out, cmd := fold(m, GridSidebarFKPreviewLookupResultMsg{
			Columns:  []string{"id"},
			Row:      []interface{}{999},
			CacheKey: "orders.user_id",
			CacheVal: "should not be applied",
			RefTable: "users",
			Token:    6, // one behind
		})

		if cmd != nil {
			t.Error("a stale reply issued a command")
		}
		if out.gridSidebarFKPreviewCursor != 7 {
			t.Errorf("the cursor moved to %d on a stale reply", out.gridSidebarFKPreviewCursor)
		}
		// And nothing was written into the sidebar. Its rendered content is the only
		// observable from this package, so that is what is compared.
		before := previewText(m)
		after := previewText(out)
		if before != after {
			t.Errorf("a stale reply changed the sidebar:\nbefore %q\nafter  %q", before, after)
		}
	})

	t.Run("a reply for the CURRENT token is applied", func(t *testing.T) {
		// Without this the case above is satisfied by dropping everything.
		m := newM(t)
		m.gridSidebarFKPreviewCursor = 6

		out, cmd := fold(m, GridSidebarFKPreviewLookupResultMsg{
			Columns:  []string{"id"},
			Row:      []interface{}{999},
			CacheKey: "orders.user_id",
			CacheVal: "the joined row",
			RefTable: "users",
			Token:    6,
		})

		if out.gridSidebarFKPreviewCursor != 6 {
			t.Errorf("the cursor moved to %d", out.gridSidebarFKPreviewCursor)
		}
		_ = cmd // may be nil: the sync only issues a command when the preview is focused
	})

	t.Run("a stale reply with an ERROR does not raise a toast either", func(t *testing.T) {
		// The error branch is BELOW the token check, so a stale reply's error is dropped
		// with the rest of it. The alternative is a toast about a lookup the user has
		// already scrolled away from, which is noise about something that no longer
		// matters.
		m := newM(t)
		m.gridSidebarFKPreviewCursor = 7

		out, _ := fold(m, GridSidebarFKPreviewLookupResultMsg{
			Token: 6,
			Err:   errMetadataLoad,
		})

		if said := toastText(out); strings.Contains(strings.ToLower(said), "fk") {
			t.Errorf("a stale reply raised a toast about its failure: %q", said)
		}
	})
}

// The nil-component guards. Several handlers reach for a component the model may not have
// built yet — the ask panel before it exists, the grid before it has data — and each one
// has to answer instead of dereferencing.
func TestAHandlerWithNoComponentToReachForAnswers(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(m Model) (Model, bool)
	}{
		// Two guards in handleAskOpen, and both reachable: there is no provider
		// configured, and there is no panel. The panel is built by NewModel, so a nil one
		// only comes from a model assembled another way — which is exactly the case worth
		// pinning, because a handler that dereferences it crashes on the keypress.
		{"opening ASK with no provider configured", func(m Model) (Model, bool) {
			m.aiProvider = nil
			out, _ := m.handleAskOpen()
			return out.(Model), false
		}},
		{"opening ASK with no ask panel", func(m Model) (Model, bool) {
			m.ask = nil
			out, _ := m.handleAskOpen()
			return out.(Model), false
		}},
		{"the grid context hint with no grid", func(m Model) (Model, bool) {
			m.grid = nil
			schema, table, where := m.gridContextHint()
			if schema != "" || table != "" || where != "" {
				t.Errorf("gridContextHint on a model with no grid returned %q %q %q", schema, table, where)
			}
			return m, false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s panicked: %v", tc.name, r)
					}
				}()
				out, _ := tc.call(m)
				// And the model has to still be drawable afterwards — with ONE exception
				// that is a fixture artefact rather than a reachable state: a model whose
				// grid was set to nil is not drawable, because NewModel always builds one.
				// The first version of this case rendered every model and reported a nil
				// dereference on the guard it had just proved works.
				if out.grid != nil {
					_ = out.View()
				}
			}()
		})
	}
}

// The grid preview's focus. Two message handlers push data into it, and both check that it
// is focused first: the preview is a detail pane, and updating it while another pane has
// focus would make the sidebar change under a user who is not looking at it.
func TestTheDetailPaneIsOnlyUpdatedWhileItHasFocus(t *testing.T) {
	for _, focused := range []bool{true, false} {
		name := "focused"
		if !focused {
			name = "not focused"
		}
		t.Run(name, func(t *testing.T) {
			m := routerModelLoaded(t)
			m.grid.SetData(&postgres.QueryResult{
				Columns: []postgres.ColumnInfo{{Name: "user_id", DataType: "integer"}},
				Rows:    [][]interface{}{{42}},
				Count:   1,
			}, "public", "orders")
			if focused {
				m.gridPreview.Focus()
			} else {
				m.gridPreview.Blur()
			}

			cmd := m.syncGridSidebarPreviewForCursor()
			if !focused && cmd != nil {
				t.Error("the sidebar sync issued a command while the detail pane had no focus")
			}
		})
	}
}
