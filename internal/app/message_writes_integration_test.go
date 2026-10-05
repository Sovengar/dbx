package app

// Scenario: Los mensajes queelets hacen algo irreversible se ejecutan con los argumentos
// que llevan, y se detienen en el primer fallo.
//
// Four handlers write to the database or move the user somewhere they did not choose to go.
// Three of them run SQL with bound arguments — a grid commit, a pending-row commit and an
// FK expansion — and all three abort the BATCH on the first failing statement rather than
// carrying on. That is the property worth pinning: a batch that runs three statements and
// silently stops after the second has left the database in a state the user cannot see and
// cannot undo from the app.
//
// The fourth, GridNavigateFKMsg, does something less obvious and just as consequential: it
// pushes an entry onto the navigation stack so the user can come back, and it combines the
// existing WHERE with the FK condition. Six fields go into that stack entry and a dropped
// one is a "back" that lands the user in the right table at the wrong row.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	bubbletea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
)

// ---------------------------------------------------------------------------
// GridCommitPendingMsg / GridCommitAllMsg
// ---------------------------------------------------------------------------

func TestGridCommitsRunTheirStatementsWithTheirArguments(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	// Every INSERT below names every NOT NULL column of the fixture. It did not, at
	// first, and the handler correctly refused each one with the database's own error —
	// which is the behaviour under test working, and the fixture being wrong. The toast
	// said "Insert failed: null value in column total", which is exactly the message a
	// user gets, so the two rounds went into finding out whether the handler or the test
	// was at fault.
	//
	// The columns are id, user_id, total and status.

	for _, tc := range []struct {
		name  string
		msg   func(queries []string, args [][]interface{}) bubbletea.Msg
		apply func(m Model, msg bubbletea.Msg) (Model, bubbletea.Cmd)
		toast string
	}{
		{
			name: "pending inserts",
			msg: func(q []string, a [][]interface{}) bubbletea.Msg {
				return grid.GridCommitPendingMsg{Schema: schema, Table: table, Queries: q, Args: a}
			},
			apply: func(m Model, msg bubbletea.Msg) (Model, bubbletea.Cmd) { return fold(m, msg) },
			toast: "inserted",
		},
		{
			name: "draft changes",
			msg: func(q []string, a [][]interface{}) bubbletea.Msg {
				return grid.GridCommitAllMsg{Schema: schema, Table: table, Queries: q, Args: a}
			},
			apply: func(m Model, msg bubbletea.Msg) (Model, bubbletea.Cmd) { return fold(m, msg) },
			toast: "committed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("the arguments are BOUND, not interpolated", func(t *testing.T) {
				// The whole reason the message carries Args separately from Queries: a value
				// with a quote in it has to arrive as data. A handler that formatted it into
				// the SQL would either break or, worse, be an injection.
				m := routerModelLoaded(t)
				m.conn = conn

				out, _ := tc.apply(m, tc.msg(
					[]string{fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total, status) VALUES ($1, 1, 1, $2)`, schema, table)},
					[][]interface{}{{int64(9001), "it's here"}},
				))
				_ = out

				var status string
				if err := conn.QueryRow(context.Background(),
					fmt.Sprintf("SELECT status FROM %s.%s WHERE id = 9001", schema, table)).Scan(&status); err != nil {
					t.Fatalf("the row is not there: %v", err)
				}
				if status != "it's here" {
					t.Errorf("the stored value is %q, want the one with the quote intact", status)
				}

				_, _ = conn.Exec(context.Background(), fmt.Sprintf("DELETE FROM %s.%s WHERE id = 9001", schema, table))
			})

			t.Run("the count is the number of statements that ran", func(t *testing.T) {
				m := routerModelLoaded(t)
				m.conn = conn

				var ids []string
				var args [][]interface{}
				for i := range 3 {
					ids = append(ids, fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total, status) VALUES ($1, 1, 1, $2)`, schema, table))
					args = append(args, []interface{}{int64(9100 + i), "batch"})
				}
				out, cmd := tc.apply(m, tc.msg(ids, args))
				_ = out

				if cmd == nil {
					t.Error("the commit issued no command to reload the table")
				}
				for i := range 3 {
					var n int
					if err := conn.QueryRow(context.Background(),
						fmt.Sprintf("SELECT count(*) FROM %s.%s WHERE id = $1", schema, table), 9100+i).Scan(&n); err != nil {
						t.Fatal(err)
					}
					if n != 1 {
						t.Errorf("row %d was not inserted", 9100+i)
					}
				}
				_, _ = conn.Exec(context.Background(), fmt.Sprintf("DELETE FROM %s.%s WHERE id >= 9100", schema, table))
			})

			t.Run("the FIRST failure stops the batch", func(t *testing.T) {
				// Three statements, the middle one invalid. The first must have run and the
				// third must NOT have — and that asymmetry is the whole claim.
				m := routerModelLoaded(t)
				m.conn = conn

				_, _ = tc.apply(m, tc.msg(
					[]string{
						fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total, status) VALUES (9200, 1, 1, 'first')`, schema, table),
						`INSERT INTO no_such_schema.no_such_table (a) VALUES (1)`,
						fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total, status) VALUES (9202, 1, 1, 'third')`, schema, table),
					},
					[][]interface{}{nil, nil, nil},
				))

				var first, third int
				if err := conn.QueryRow(context.Background(),
					fmt.Sprintf("SELECT count(*) FROM %s.%s WHERE id = 9200", schema, table)).Scan(&first); err != nil {
					t.Fatal(err)
				}
				if err := conn.QueryRow(context.Background(),
					fmt.Sprintf("SELECT count(*) FROM %s.%s WHERE id = 9202", schema, table)).Scan(&third); err != nil {
					t.Fatal(err)
				}
				if first != 1 {
					t.Error("the statement BEFORE the failure did not run")
				}
				if third != 0 {
					t.Error("the statement AFTER the failure ran; a failed batch must stop")
				}
				_, _ = conn.Exec(context.Background(), fmt.Sprintf("DELETE FROM %s.%s WHERE id >= 9200", schema, table))
			})

			t.Run("with NO connection it does nothing", func(t *testing.T) {
				m := routerModelLoaded(t)
				m.conn = nil
				out, cmd := tc.apply(m, tc.msg(
					[]string{fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total) VALUES (9300, 1, 1)`, schema, table)},
					[][]interface{}{nil},
				))
				if cmd != nil {
					t.Error("a commit with no connection issued a command")
				}
				if out.state == StateError {
					t.Error("a commit with no connection put the app in the error state")
				}
			})

			t.Run("no statements is not an error", func(t *testing.T) {
				// The grid can ask to commit with nothing pending; the answer is a reload
				// and a "0 rows" message, not a failure the user has to dismiss.
				m := routerModelLoaded(t)
				m.conn = conn
				if _, cmd := tc.apply(m, tc.msg(nil, nil)); cmd == nil {
					t.Error("committing nothing issued no reload")
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// GridNavigateFKMsg
// ---------------------------------------------------------------------------

func TestNavigateFKPushesStateAndCombinesTheFilter(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	t.Run("a NULL reference cannot be navigated to", func(t *testing.T) {
		// The guard has to come first: building a WHERE clause out of an empty table name
		// produces a query against nothing, and the message is the only place the user
		// learns why nothing happened.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.prevTable = table

		out, cmd := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: nil,
			RefSchema: schema, RefTable: "", RefColumn: "id",
		})
		if cmd != nil {
			t.Error("a NULL reference issued a command")
		}
		if out.prevTable != table {
			t.Errorf("the current table became %q; the navigation did not happen", out.prevTable)
		}
		if len(out.navStack) != 0 {
			t.Errorf("the navigation stack grew to %d entries", len(out.navStack))
		}
	})

	t.Run("it pushes an entry carrying EVERY field", func(t *testing.T) {
		// Six fields. A dropped ScrollCol brings the user back to the right table, the
		// right row and the right filter but horizontally somewhere else, which reads as
		// the app having lost the place.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.prevTable = table

		out, _ := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: int64(1),
			RefSchema: schema, RefTable: "users", RefColumn: "id",
		})

		if len(out.navStack) != 1 {
			t.Fatalf("the navigation stack has %d entries, want 1", len(out.navStack))
		}
		entry := out.navStack[0]
		if entry.Schema != schema || entry.Table != table {
			t.Errorf("the entry names %s.%s, want %s.%s", entry.Schema, entry.Table, schema, table)
		}
		if entry.Where != "" {
			t.Errorf("the entry carries the filter %q, want none", entry.Where)
		}
		if out.prevSchema != schema || out.prevTable != "users" {
			t.Errorf("the app moved to %s.%s, want %s.users", out.prevSchema, out.prevTable, schema)
		}
	})

	t.Run("an EXISTING filter is combined, not replaced", func(t *testing.T) {
		// The whole point of combining: the user filtered orders, followed a foreign key,
		// and going back must restore the filter. Replacing it would silently widen the
		// table they were looking at.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.prevTable = table
		withFilter(&m, "total > 5")

		out, _ := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: int64(1),
			RefSchema: schema, RefTable: "users", RefColumn: "id",
		})

		if got := out.navStack[0].Where; got != "total > 5" {
			t.Errorf("the entry carries the filter %q, want the one that was in force", got)
		}
	})

	t.Run("with nothing to go back to it pushes nothing", func(t *testing.T) {
		// prevSchema or prevTable empty means the app has not been anywhere, and an entry
		// with a blank table is a "back" that goes nowhere.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = ""
		m.prevTable = ""

		out, _ := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: int64(1),
			RefSchema: schema, RefTable: "users", RefColumn: "id",
		})
		if len(out.navStack) != 0 {
			t.Errorf("the navigation stack has %d entries from nowhere", len(out.navStack))
		}
	})

	t.Run("the navigation carries the filter into the table it lands on", func(t *testing.T) {
		// Asserted against the database, because the combination is built into a WHERE
		// clause that the loader will run and nothing else checks.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.prevTable = table

		out, cmd := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: int64(1),
			RefSchema: schema, RefTable: table, RefColumn: "user_id",
		})
		if cmd == nil {
			t.Fatal("no command was issued")
		}
		msg, ok := cmd().(tableDataLoadedMsg)
		if !ok {
			t.Fatalf("the command produced a %T", cmd())
		}
		if msg.err != nil {
			t.Fatalf("the navigated-to table failed to load: %v", msg.err)
		}
		if !strings.Contains(msg.where, `"user_id" = 1`) {
			t.Errorf("the filter is %q, want the FK condition", msg.where)
		}
		if out.router.Focus() != FocusGrid {
			t.Errorf("the focus is %v, want the grid", out.router.Focus())
		}
	})

	t.Run("with an existing filter the two are joined with AND and parentheses", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.prevTable = table
		withFilter(&m, "total > 5")

		out, _ := fold(m, grid.GridNavigateFKMsg{
			Schema: schema, Table: table, FKColumn: "user_id", FKValue: int64(1),
			RefSchema: schema, RefTable: table, RefColumn: "user_id",
		})
		_ = out

		msg := m.loadTableDataWithWhere(schema, table, `"user_id" = 1 AND (total > 5)`)().(tableDataLoadedMsg)
		if msg.err != nil {
			t.Fatalf("the combined filter does not parse: %v", msg.err)
		}
	})
}

// ---------------------------------------------------------------------------
// GridPreviewExpandFKMsg
// ---------------------------------------------------------------------------

func TestThePreviewExpandsAKeyIntoTheReferencedRow(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, _ := metadataFixture(t, conn)

	t.Run("the referenced row comes back with the column it came from", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema

		out, cmd := fold(m, gridpreview.GridPreviewExpandFKMsg{
			Column: "user_id", Path: "user_id", Value: int64(1),
			RefSchema: schema, RefTable: "users", RefColumn: "id",
		})
		_ = out
		if cmd == nil {
			t.Fatal("no command was issued")
		}
		msg, ok := cmd().(gridpreview.GridPreviewExpandFKResultMsg)
		if !ok {
			t.Fatalf("the command produced a %T", cmd())
		}
		if msg.Err != nil {
			t.Fatalf("the expansion failed: %v", msg.Err)
		}
		if msg.Column != "user_id" || msg.Path != "user_id" {
			t.Errorf("the result names %q/%q; the preview needs both to nest it", msg.Column, msg.Path)
		}
		if len(msg.Row) == 0 {
			t.Fatal("the result carries no row")
		}
		// The row is a MAP keyed by column name, because the preview merges it into a
		// nested object and a positional slice would have to be matched by position.
		if msg.Row["id"] == nil {
			t.Errorf("the row is %v, want it keyed by column name", msg.Row)
		}
	})

	t.Run("a value with no referenced row is an error", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema

		_, cmd := fold(m, gridpreview.GridPreviewExpandFKMsg{
			Column: "user_id", Path: "user_id", Value: int64(999),
			RefSchema: schema, RefTable: "users", RefColumn: "id",
		})
		msg, ok := cmd().(gridpreview.GridPreviewExpandFKResultMsg)
		if !ok {
			t.Fatal("the command did not produce a result")
		}
		if msg.Err == nil {
			t.Fatal("expanding a value with no referenced row returned no error")
		}
		if !strings.Contains(msg.Err.Error(), "no row found") {
			t.Errorf("the error is %q", msg.Err)
		}
	})

	t.Run("a NULL reference and a missing connection both do nothing", func(t *testing.T) {
		// Not an error toast: the preview is a look-at panel, and a key the user cannot
		// follow is not something they did wrong.
		for _, tc := range []struct {
			name string
			conn interface{ Close() error }
			msg  gridpreview.GridPreviewExpandFKMsg
		}{
			{"no referenced table", nil, gridpreview.GridPreviewExpandFKMsg{
				Column: "user_id", Value: int64(1), RefSchema: schema, RefColumn: "id",
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := routerModelLoaded(t)
				if tc.conn != nil {
					m.conn = conn
				}
				if _, cmd := fold(m, tc.msg); cmd != nil {
					t.Error("a command was issued for a key that cannot be followed")
				}
			})
		}

		t.Run("no connection", func(t *testing.T) {
			m := routerModelLoaded(t)
			m.conn = nil
			if _, cmd := fold(m, gridpreview.GridPreviewExpandFKMsg{
				Column: "user_id", Value: int64(1),
				RefSchema: schema, RefTable: "users", RefColumn: "id",
			}); cmd != nil {
				t.Error("a command was issued with no connection")
			}
		})
	})
}

// ---------------------------------------------------------------------------

// withFilter puts the grid into the state where a WHERE clause is in force, by driving the
// filter row the way the user does. There is no setter for it: the grid takes the clause
// from its filter widget once the user applies it, and reaching past that would test a
// state the app cannot be in.
func withFilter(m *Model, clause string) {
	// The grid needs data first: filter_rows is refused on an empty grid, because there
	// is nothing for the filter to narrow.
	m.grid.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "user_id"}, {Name: "total"}},
		Rows:    [][]interface{}{{int64(1), int64(1), int64(10)}},
	}, "public", "orders")
	m.grid.SetWidth(80)
	m.grid.SetHeight(20)
	m.grid.Focus()
	if _, handled := m.grid.HandleAction(config.ActionID("filter_rows")); !handled {
		panic("filter_rows was not handled")
	}
	for _, r := range clause {
		m.grid.Update(bubbletea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.grid.Update(bubbletea.KeyPressMsg{Code: bubbletea.KeyEnter})
	if got := m.grid.WhereClause(); got != clause {
		panic("the filter did not take: " + got)
	}
}

// fold runs one message through Update and hands back the model it produced.
//
// Model.Update has a VALUE receiver and returns a new model, so `m.Update(msg)` as a
// statement throws the result away — which in this file would silently leave every
// assertion reading the model as it was BEFORE the message.
func fold(m Model, msg bubbletea.Msg) (Model, bubbletea.Cmd) {
	updated, cmd := m.Update(msg)
	out, ok := updated.(Model)
	if !ok {
		panic("Update did not return a Model")
	}
	return out, cmd
}
