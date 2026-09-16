package app

import (
	"context"
	"fmt"
	"testing"
)

// Scenario: New DML auto-commits previous transaction.
func TestExecuteQuery_NewDMLCommitsPreviousTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")
	msg := runExecuteQuery(t, m, "UPDATE users SET name='test2' WHERE id=2")
	if msg.err != nil {
		t.Fatalf("second UPDATE error = %v", msg.err)
	}

	if f.txs[0].commits != 1 {
		t.Fatalf("COMMIT sent %d times for the first transaction, want 1", f.txs[0].commits)
	}
	if f.begins != 2 {
		t.Fatalf("BEGIN sent %d times, want 2 — the second statement needs a new transaction", f.begins)
	}
	if len(f.execs) != 2 {
		t.Fatalf("statements executed = %d, want 2", len(f.execs))
	}
	if f.execs[1].querier != f.txs[1] {
		t.Fatal("second statement did not run inside a new transaction")
	}
	if f.txs[1].commits != 0 {
		t.Fatal("the new transaction was committed before the user could review it")
	}
	if !f.runner.pending() {
		t.Fatal("no pending transaction for the second statement")
	}
}

// Scenario: DDL auto-commits previous transaction.
func TestExecuteQuery_DDLAutoCommitsPreviousTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")
	msg := runExecuteQuery(t, m, "CREATE TABLE test_rollback (id SERIAL PRIMARY KEY)")
	if msg.err != nil {
		t.Fatalf("CREATE TABLE error = %v", msg.err)
	}

	if f.txs[0].commits != 1 {
		t.Fatalf("COMMIT sent %d times for the pending transaction, want 1", f.txs[0].commits)
	}
	if f.begins != 1 {
		t.Fatalf("BEGIN sent %d times, want 1 — DDL must not open a transaction", f.begins)
	}
	if len(f.execs) != 2 || f.execs[1].querier != f.conn {
		t.Fatal("DDL did not run in autocommit")
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after DDL")
	}
}

// Scenario: DDL auto-commits previous transaction (real PostgreSQL).
func TestIntegration_NewDMLAutoCommitsPreviousTransaction(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	observer := connectTestDB(t, dsn)
	table := seedTxTestTable(t, conn, map[int]string{1: "before", 2: "before"})

	runner := newStatementRunner(conn)
	ctx := context.Background()

	if _, _, err := runner.execute(ctx, fmt.Sprintf("UPDATE %s SET name='first' WHERE id=1", table)); err != nil {
		t.Fatalf("first UPDATE: %v", err)
	}
	if _, _, err := runner.execute(ctx, fmt.Sprintf("UPDATE %s SET name='second' WHERE id=2", table)); err != nil {
		t.Fatalf("second UPDATE: %v", err)
	}

	if got := readName(t, observer, table, 1); got != "first" {
		t.Fatalf("first transaction was not auto-committed: other session sees %q, want %q", got, "first")
	}
	if got := readName(t, observer, table, 2); got != "before" {
		t.Fatalf("second transaction was committed too early: other session sees %q, want %q", got, "before")
	}

	if _, err := runner.rollback(ctx); err != nil {
		t.Fatalf("ROLLBACK: %v", err)
	}
	if got := readName(t, observer, table, 2); got != "before" {
		t.Fatalf("rollback did not undo the second statement: other session sees %q, want %q", got, "before")
	}
}
