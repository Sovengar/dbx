package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// These tests exercise the transaction lifecycle against a real PostgreSQL
// server. They are skipped unless DBX_TEST_DSN points at a disposable
// database, e.g.:
//
//	DBX_TEST_DSN="postgres://user:pass@localhost:5432/postgres?sslmode=disable" go test ./internal/app/...

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

func TestIntegration_DMLIsNotCommittedUntilRolledBack(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	observer := connectTestDB(t, dsn)
	table := seedTxTestTable(t, conn, map[int]string{1: "before"})

	runner := newStatementRunner(conn)
	ctx := context.Background()

	if _, _, err := runner.execute(ctx, fmt.Sprintf("UPDATE %s SET name='after' WHERE id=1", table)); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	if !runner.pending() {
		t.Fatal("UPDATE did not leave a pending transaction")
	}
	if got := readName(t, observer, table, 1); got != "before" {
		t.Fatalf("other session sees %q while the transaction is open, want %q", got, "before")
	}
	if got := readName(t, conn, table, 1); got != "after" {
		t.Fatalf("transaction's own session sees %q, want %q", got, "after")
	}

	if _, err := runner.rollback(ctx); err != nil {
		t.Fatalf("ROLLBACK: %v", err)
	}
	if runner.pending() {
		t.Fatal("transaction still pending after rollback")
	}
	if got := readName(t, observer, table, 1); got != "before" {
		t.Fatalf("rollback did not undo the change: other session sees %q, want %q", got, "before")
	}
}

func TestIntegration_DDLAutoCommitsAndRunsInAutocommit(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	observer := connectTestDB(t, dsn)
	table := seedTxTestTable(t, conn, map[int]string{1: "before"})

	runner := newStatementRunner(conn)
	ctx := context.Background()

	if _, _, err := runner.execute(ctx, fmt.Sprintf("UPDATE %s SET name='after' WHERE id=1", table)); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}

	extra := table + "_extra"
	if _, _, err := runner.execute(ctx, fmt.Sprintf("CREATE TABLE %s (id int)", extra)); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+extra) })

	if runner.pending() {
		t.Fatal("DDL did not autocommit the pending transaction")
	}
	if got := readName(t, observer, table, 1); got != "after" {
		t.Fatalf("DDL did not commit the pending UPDATE: other session sees %q, want %q", got, "after")
	}
	var exists bool
	if err := observer.QueryRow(context.Background(),
		"SELECT to_regclass($1) IS NOT NULL", extra).Scan(&exists); err != nil {
		t.Fatalf("check %s: %v", extra, err)
	}
	if !exists {
		t.Fatalf("DDL did not execute in autocommit: other session cannot see %s", extra)
	}
}

func TestIntegration_BatchDMLRollsBackAsOneTransaction(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	observer := connectTestDB(t, dsn)
	table := seedTxTestTable(t, conn, map[int]string{1: "before", 2: "before"})

	runner := newStatementRunner(conn)
	ctx := context.Background()

	batch := fmt.Sprintf(
		"UPDATE %s SET name='a' WHERE id=1; UPDATE %s SET name='b' WHERE id=2",
		table, table)
	if _, _, err := runner.execute(ctx, batch); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if !runner.pending() {
		t.Fatal("batch did not leave a pending transaction")
	}
	if got := readName(t, observer, table, 1); got != "before" {
		t.Fatalf("first statement of the batch escaped the transaction: other session sees %q", got)
	}

	if _, err := runner.rollback(ctx); err != nil {
		t.Fatalf("ROLLBACK: %v", err)
	}
	for _, id := range []int{1, 2} {
		if got := readName(t, observer, table, id); got != "before" {
			t.Fatalf("rollback did not undo statement on id=%d: other session sees %q, want %q", id, got, "before")
		}
	}
}
