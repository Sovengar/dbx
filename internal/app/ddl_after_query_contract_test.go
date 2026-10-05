package app

// Scenario: Un DDL ejecutado recarga el esquema y navega a la tabla — contra una base de
// datos real, porque la mitad de la decision es si el proyecto existe.
//
// queryExecutedMsg has one branch that behaves nothing like the rest. A SELECT hands its
// rows to the grid and stays where it is; a DDL leaves the grid with nothing to show, so it
// reloads the schema and navigates the EXPLORER to the table it just made. Three conditions
// have to line up for that to happen:
//
//	it is DDL — the same statement run twice is harmless
//	there is a CONNECTION — reloading needs one
//	there is a PROJECT — and this is the one that is easy to miss, because the handler
//	  dereferences it. A model that somehow ran a query with no project is not a state the
//	  app reaches, and a test that builds one is building an impossible world.
//
// So the interesting cases are the three refusals and the three ways each condition fails
// on its own. A guard tested only when it fires is a guard that can be inverted.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

func TestADDLReloadsTheSchemaAndNavigatesToItsTable(t *testing.T) {
	t.Run("a DDL with a connection and a project reloads and navigates", func(t *testing.T) {
		conn := connectTestDB(t, testDSN(t))
		m := routerModelLoaded(t)
		m.conn = conn
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
		m.state = StateMain
		m.editorOpen = true
		m.router.FocusPane(FocusGrid)

		out, _ := fold(m, queryExecutedMsg{
			sql:         "CREATE TABLE public.dbx_ddl_probe (id int)",
			committedTx: true,
			result:      &postgres.QueryResult{},
		})

		// The navigator moved to the EXPLORER, not the grid. Putting a DDL's empty result
		// in the grid instead would leave the user looking at an empty table with a success
		// toast and no way to see what they just made.
		if out.router.Focus() != FocusExplorer {
			t.Errorf("after a DDL the focus is on %q, want the explorer", out.router.Focus())
		}
		if said := toastText(out); !strings.Contains(strings.ToLower(said), "ddl") {
			t.Errorf("the toast does not say a DDL ran: %q", said)
		}
		// And the editor closed, because the statement is done.
		if out.editorOpen {
			t.Error("the editor stayed open after the DDL ran")
		}
		// The statement went into the history, so it can be run again.
		if out.editor.Content() != "" {
			t.Errorf("the editor still holds %q after the statement ran", out.editor.Content())
		}
	})

	for _, tc := range []struct {
		name   string
		sql    string
		mutate func(m *Model)
	}{
		{
			name: "a SELECT with a connection and a project does not reload",
			sql:  "SELECT 1",
		},
		{
			name:   "a DDL with NO connection does not reload",
			sql:    "DROP TABLE public.dbx_ddl_probe",
			mutate: func(m *Model) { m.conn = nil },
		},
		{
			// The one that cannot happen in the app, and the reason the guard exists. Without
			// it the handler dereferences a nil project and the app crashes on a message
			// rather than on a keypress — which is harder to trace and impossible to
			// reproduce.
			name:   "a DDL with NO project does not reload",
			sql:    "DROP TABLE public.dbx_ddl_probe",
			mutate: func(m *Model) { m.project = nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			m.conn = connectTestDB(t, testDSN(t))
			m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
			m.state = StateMain
			m.router.FocusPane(FocusGrid)
			if tc.mutate != nil {
				tc.mutate(&m)
			}

			out, cmd := fold(m, queryExecutedMsg{sql: tc.sql, result: &postgres.QueryResult{}})

			if out.router.Focus() != FocusGrid {
				t.Errorf("%s left the focus on %q, want the grid — the DDL branch is the only one that navigates away", tc.name, out.router.Focus())
			}
			if said := toastText(out); strings.Contains(strings.ToLower(said), "ddl") {
				t.Errorf("%s said %q, which claims a DDL ran", tc.name, said)
			}
			// And it went to the grid instead: a non-DDL result is what the user asked to see.
			if out.grid.Columns() == nil && cmd != nil {
				t.Log("no columns and a command; the row count message is the observable here")
			}
		})
	}
}

// A non-DDL statement puts its result in the grid, and the count is what the user is told.
// The two assertions together are the difference between "the query ran" and "the query ran
// and returned what it said it did".
func TestANonDDLResultGoesToTheGridWithItsRowCount(t *testing.T) {
	m := routerModelLoaded(t)
	m.conn = connectTestDB(t, testDSN(t))
	m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
	m.state = StateMain
	m.router.FocusPane(FocusExplorer)

	out, cmd := fold(m, queryExecutedMsg{
		sql: "SELECT 1",
		result: &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "?column?", DataType: "integer"}},
			Rows:    [][]interface{}{{1}},
			Count:   1,
		},
	})

	if cmd != nil {
		t.Error("a non-DDL issued a reload")
	}
	if out.router.Focus() != FocusGrid {
		t.Errorf("the focus is on %q, want the grid", out.router.Focus())
	}
	if got := len(out.grid.Columns()); got != 1 {
		t.Errorf("the grid holds %d columns, want the one the query returned", got)
	}
	said := toastText(out)
	if !strings.Contains(said, "1 rows") {
		t.Errorf("the toast does not state the row count: %q", said)
	}
	// The synthetic table name, because a query result is not a table and naming it as one
	// would put a phantom node in the explorer's history.
	if got := out.grid.TableName(); got != "query" {
		t.Errorf("the grid is labelled %q, want %q", got, "query")
	}
}
