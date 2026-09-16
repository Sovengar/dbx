package app

import (
	"context"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/jackc/pgx/v5"
)

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
// transaction semantics are covered by the integration test.
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

func newRollbackTestModel() Model {
	styles := testStyles()
	kbs := config.NewKeybindRegistry(config.KeybindingsConfig{}).Flatten()
	return Model{
		state:     StateMain,
		styles:    styles,
		router:    NewRouter(kbs),
		keybinds:  kbs,
		editor:    editor.NewSQLEditor(styles),
		palette:   palette.New(styles, kbs),
		helpModal: ui.NewHelpModal(styles, kbs),
		toast:     ui.NewToastManager(styles),
		statusbar: ui.NewStatusBar(styles, kbs),
	}
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

func TestIsDML(t *testing.T) {
	dml := []string{
		"UPDATE users SET name='test'",
		"update users set name='test'",
		"  INSERT INTO users (name) VALUES ('x')",
		"DELETE FROM users WHERE id=1",
		"WITH changed AS (UPDATE users SET name='x' RETURNING id) SELECT * FROM changed",
	}
	for _, sql := range dml {
		if !isDML(sql) {
			t.Errorf("isDML(%q) = false, want true", sql)
		}
	}

	notDML := []string{
		"SELECT * FROM users",
		"CREATE TABLE t (id int)",
		"ALTER TABLE users ADD COLUMN age int",
		"DROP TABLE t",
		"TRUNCATE users",
		"EXPLAIN SELECT * FROM users",
		"UPDATED_AT",
	}
	for _, sql := range notDML {
		if isDML(sql) {
			t.Errorf("isDML(%q) = true, want false", sql)
		}
	}
}

// Scenario: UPDATE / INSERT / DELETE open a transaction that stays open.
func TestExecuteQuery_DMLOpensTransaction(t *testing.T) {
	for _, sql := range []string{
		"UPDATE users SET name='test' WHERE id=1",
		"INSERT INTO users (name) VALUES ('new_user')",
		"DELETE FROM users WHERE id=999",
	} {
		t.Run(sql, func(t *testing.T) {
			f := newFakeRunner()
			m := newRollbackTestModel()
			m.txRunner = f.runner

			msg := runExecuteQuery(t, m, sql)
			if msg.err != nil {
				t.Fatalf("executeQuery(%q) error = %v", sql, msg.err)
			}
			if f.begins != 1 {
				t.Fatalf("BEGIN sent %d times, want 1", f.begins)
			}
			if len(f.execs) != 1 {
				t.Fatalf("statements executed = %d, want 1", len(f.execs))
			}
			if f.execs[0].querier != f.txs[0] {
				t.Fatal("statement did not run inside the transaction")
			}
			if f.txs[0].commits != 0 {
				t.Fatal("transaction was committed — changes would already be visible in the database")
			}
			if !f.runner.pending() {
				t.Fatal("no pending transaction after DML")
			}
		})
	}
}

// Scenario: SELECT does not open a transaction.
func TestExecuteQuery_SelectDoesNotOpenTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	msg := runExecuteQuery(t, m, "SELECT * FROM users LIMIT 10")
	if msg.err != nil {
		t.Fatalf("executeQuery() error = %v", msg.err)
	}
	if f.begins != 0 {
		t.Fatalf("BEGIN sent %d times, want 0", f.begins)
	}
	if f.runner.pending() {
		t.Fatal("SELECT opened a transaction")
	}
	if len(f.execs) != 1 || f.execs[0].querier != postgres.Querier(f.conn) {
		t.Fatal("SELECT did not run in autocommit")
	}
}

// Scenario: DDL does not open a transaction.
func TestExecuteQuery_DDLDoesNotOpenTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	msg := runExecuteQuery(t, m, "CREATE TABLE test_no_tx (id INT)")
	if msg.err != nil {
		t.Fatalf("executeQuery() error = %v", msg.err)
	}
	if f.begins != 0 {
		t.Fatalf("BEGIN sent %d times, want 0", f.begins)
	}
	if f.runner.pending() {
		t.Fatal("DDL opened a transaction")
	}
	if len(f.execs) != 1 || f.execs[0].querier != postgres.Querier(f.conn) {
		t.Fatal("DDL did not run in autocommit")
	}
}

// Scenario: batch of DML statements in a single execution.
func TestExecuteQuery_BatchDMLSharesOneTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	msg := runExecuteQuery(t, m, "UPDATE users SET name='a' WHERE id=1; UPDATE users SET name='b' WHERE id=2")
	if msg.err != nil {
		t.Fatalf("executeQuery() error = %v", msg.err)
	}
	if f.begins != 1 {
		t.Fatalf("BEGIN sent %d times, want 1 for the whole batch", f.begins)
	}
	if len(f.execs) != 2 {
		t.Fatalf("statements executed = %d, want 2", len(f.execs))
	}
	for i, exec := range f.execs {
		if exec.querier != f.txs[0] {
			t.Fatalf("statement %d did not run inside the batch transaction", i+1)
		}
	}
	if f.txs[0].commits != 0 {
		t.Fatal("batch transaction was committed between statements")
	}

	rolledBack, err := f.runner.rollback(context.Background())
	if err != nil {
		t.Fatalf("rollback() error = %v", err)
	}
	if !rolledBack {
		t.Fatal("rollback() reported no pending transaction")
	}
	if f.txs[0].rollbacks != 1 {
		t.Fatalf("ROLLBACK sent %d times, want 1", f.txs[0].rollbacks)
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after rollback")
	}
}
