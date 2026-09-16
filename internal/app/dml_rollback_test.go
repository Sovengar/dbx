package app

import (
	"context"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

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
			m.runner = f.runner

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
	m.runner = f.runner

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
	m.runner = f.runner

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
	m.runner = f.runner

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
