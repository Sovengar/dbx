package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/store"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/jackc/pgx/v5"
)

// This file holds the shared test harness for the DML transaction scenarios:
// an in-memory statement runner for the lifecycle assertions and a real
// PostgreSQL harness for the transaction-semantics acceptance tests.

// fakeQuerier stands in for the autocommit connection. It is only used to
// tell "ran in autocommit" apart from "ran inside the transaction"; the
// injected exec never calls Query.
type fakeQuerier struct{ label string }

func (f *fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

// fakeTx records commits and rollbacks so tests can assert what was sent
// to the database for a pending transaction.
type fakeTx struct {
	pgx.Tx
	commits   int
	rollbacks int
}

func (f *fakeTx) Commit(context.Context) error   { f.commits++; return nil }
func (f *fakeTx) Rollback(context.Context) error { f.rollbacks++; return nil }

type recordedExec struct {
	querier postgres.Querier
	sql     string
}

// fakeRunner wires a statementRunner to deterministic fakes so the
// transaction lifecycle can be observed without a live database. Real
// transaction semantics are covered by the integration tests.
type fakeRunner struct {
	runner *statementRunner
	conn   *fakeQuerier
	txs    []*fakeTx
	begins int
	execs  []recordedExec
}

func newFakeRunner() *fakeRunner {
	f := &fakeRunner{conn: &fakeQuerier{label: "conn"}}
	f.runner = &statementRunner{
		conn: f.conn,
		begin: func(context.Context) (pgx.Tx, error) {
			f.begins++
			tx := &fakeTx{}
			f.txs = append(f.txs, tx)
			return tx, nil
		},
		exec: func(_ context.Context, q postgres.Querier, sql string) (*postgres.QueryResult, error) {
			f.execs = append(f.execs, recordedExec{querier: q, sql: sql})
			return &postgres.QueryResult{}, nil
		},
	}
	return f
}

// newRollbackTestModel returns a main-state model wired to the components the
// key handling consults before dispatching global actions.
func newRollbackTestModel() Model {
	styles := testStyles()
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{})
	return Model{
		state:        StateMain,
		styles:       styles,
		router:       NewRouter(kb),
		keybinds:     kb,
		editor:       editor.NewSQLEditor(styles),
		palette:      palette.New(styles, kb),
		helpModal:    ui.NewHelpModal(styles, kb),
		toast:        ui.NewToastManager(styles),
		keybindsPane: ui.NewKeybindsPane(styles, kb),
	}
}

// newModelWithGrid adds the components the queryExecutedMsg handler touches,
// so messages produced by executeQuery can be dispatched end to end.
func newModelWithGrid(t *testing.T) Model {
	t.Helper()
	m := newRollbackTestModel()
	m.grid = grid.New(m.styles, 100, m.keybinds)
	m.queryStore = store.NewQueryStore(t.TempDir())
	return m
}

func runExecuteQuery(t *testing.T, m Model, sql string) queryExecutedMsg {
	t.Helper()
	cmd := m.executeQuery(sql)
	if cmd == nil {
		t.Fatalf("executeQuery(%q) returned a nil command", sql)
	}
	msg := cmd()
	executed, ok := msg.(queryExecutedMsg)
	if !ok {
		t.Fatalf("executeQuery(%q) produced %T, want queryExecutedMsg", sql, msg)
	}
	return executed
}

// pressRollback sends the global rollback key through the real key handling.
func pressRollback(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'U'})
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(U) returned %T, want Model", updated)
	}
	return model
}

func lastToastText(t *testing.T, m Model) string {
	t.Helper()
	rendered := m.toast.RenderedToasts()
	if len(rendered) == 0 {
		t.Fatal("no toast was shown")
	}
	return rendered[len(rendered)-1]
}

func lastToastContains(m Model, want string) bool {
	for _, rendered := range m.toast.RenderedToasts() {
		if strings.Contains(rendered, want) {
			return true
		}
	}
	return false
}

// --- Real PostgreSQL harness (skipped unless DBX_TEST_DSN is set) ---

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DBX_TEST_DSN")
	if dsn == "" {
		t.Skip("set DBX_TEST_DSN to run the PostgreSQL integration tests")
	}
	return dsn
}

func connectTestDB(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to %q: %v", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// seedTxTestTable creates a throwaway table with the given rows so the test
// can observe which changes became visible to other sessions.
func seedTxTestTable(t *testing.T, conn *pgx.Conn, rows map[int]string) string {
	t.Helper()
	ctx := context.Background()
	table := fmt.Sprintf("dbx_tx_test_%d", time.Now().UnixNano())

	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id int primary key, name text)", table)); err != nil {
		t.Fatalf("create %s: %v", table, err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	for id, name := range rows {
		if _, err := conn.Exec(ctx, fmt.Sprintf("INSERT INTO %s (id, name) VALUES ($1, $2)", table), id, name); err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
	return table
}

// readName reads a row as a different session so visibility reflects what is
// actually committed in the database.
func readName(t *testing.T, conn *pgx.Conn, table string, id int) string {
	t.Helper()
	var name string
	if err := conn.QueryRow(context.Background(),
		fmt.Sprintf("SELECT name FROM %s WHERE id = $1", table), id).Scan(&name); err != nil {
		t.Fatalf("read %s (id=%d): %v", table, id, err)
	}
	return name
}
