package app

import (
	"context"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/jackc/pgx/v5"
)

func TestIsSelectOnly(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"plain select", "SELECT * FROM users", true},
		{"lowercase select", "select id from users", true},
		{"with select", "WITH t AS (SELECT 1) SELECT * FROM t", true},
		{"leading comment", "-- comment\nSELECT 1", true},
		{"block comment", "/* hi */ SELECT 1", true},
		{"leading semicolon", "; SELECT 1", true},
		{"trailing semicolon", "SELECT 1;", true},
		{"select with string keyword", "SELECT * FROM users WHERE action = 'delete'", true},
		{"delete", "DELETE FROM users", false},
		{"update", "UPDATE users SET a = 1", false},
		{"insert", "INSERT INTO users VALUES (1)", false},
		{"drop", "DROP TABLE users", false},
		{"comment then delete", "-- safe\nDELETE FROM users", false},
		{"leading semicolon then delete", ";DELETE FROM users", false},
		{"cte with delete", "WITH d AS (DELETE FROM users RETURNING *) SELECT * FROM d", false},
		{"cte with insert", "WITH i AS (INSERT INTO users VALUES (1) RETURNING *) SELECT * FROM i", false},
		{"multiple statements", "SELECT 1; DELETE FROM users", false},
		{"with values only", "WITH t AS (VALUES (1)) SELECT * FROM t", true},
		{"empty", "   ", false},
		// Dollar-quoted and E-string literals must not be split or scanned.
		{"dollar-quoted semicolon", "SELECT $q$a;b$q$", true},
		{"dollar-quoted keyword", "SELECT $q$DELETE FROM users$q$", true},
		{"dollar-quoted then delete", "SELECT $q$;$q$; DELETE FROM users", false},
		{"tagged dollar quote", "SELECT $tag$a;b$tag$", true},
		{"e-string escaped quote", `SELECT E'\'; DROP TABLE t; --'`, true},
		{"e-string then delete", `SELECT E'x'; DELETE FROM users`, false},
		{"standard string backslash", `SELECT 'a\'; DELETE FROM users`, false},
		// SELECT-based writes and locks must fail client-side.
		{"select into", "SELECT * INTO newtab FROM users", false},
		{"for update", "SELECT * FROM users FOR UPDATE", false},
		{"for share", "SELECT * FROM users FOR SHARE", false},
		{"nextval", "SELECT nextval('s')", false},
		{"for in string", "SELECT 'for' AS x", true},
		// FOR is legal in expressions; only row locks must be rejected.
		{"substring for", "SELECT substring(name FROM 1 FOR 2) FROM users", true},
		{"overlay for", "SELECT overlay(name placing 'x' from 1 for 2) FROM users", true},
		{"for key share", "SELECT * FROM users FOR KEY SHARE", false},
		{"for no key update", "SELECT * FROM users FOR NO KEY UPDATE", false},
		// E-string detection must not be fooled by '$' or non-ASCII identifiers.
		{"dollar ident before e-string", `SELECT foo$E'a\'; DELETE FROM users`, false},
		{"unicode ident before e-string", `SELECT caféE'a\'; DELETE FROM users`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSelectOnly(tc.sql); got != tc.want {
				t.Fatalf("isSelectOnly(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}

// Scenario: The server rejects DML even if validation is bypassed
func TestExecuteReadOnly_RefusesWhenTxPending(t *testing.T) {
	f := newFakeRunner()
	f.runner.beginReadOnly = func(context.Context) (pgx.Tx, error) {
		return &fakeTx{}, nil
	}
	f.runner.tx = &fakeTx{}

	if _, err := f.runner.executeReadOnly(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("executeReadOnly ran while a DML transaction was pending")
	}
	if len(f.execs) != 0 {
		t.Fatalf("executeReadOnly executed %d statements, want 0", len(f.execs))
	}
}

func TestExecuteReadOnly_RunsInReadOnlyTxAndRollsBack(t *testing.T) {
	f := newFakeRunner()
	tx := &fakeTx{}
	f.runner.beginReadOnly = func(context.Context) (pgx.Tx, error) { return tx, nil }

	result, err := f.runner.executeReadOnly(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("executeReadOnly: %v", err)
	}
	if result == nil {
		t.Fatal("executeReadOnly returned nil result")
	}
	if len(f.execs) != 1 {
		t.Fatalf("executed %d statements, want 1", len(f.execs))
	}
	if f.execs[0].querier != postgres.Querier(tx) {
		t.Fatal("statement did not run on the read-only transaction")
	}
	if tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("tx commits=%d rollbacks=%d, want 0/1", tx.commits, tx.rollbacks)
	}
}

func TestSelectOnlyViolation_Message(t *testing.T) {
	cases := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM users FOR SHARE", "row locking is not allowed"},
		{"SELECT * INTO t FROM users", "INTO is not allowed"},
		{"SELECT nextval('s')", "NEXTVAL is not allowed"},
		{"DELETE FROM users", "DELETE statements are not allowed"},
		{"SELECT 1; SELECT 2", "single statement"},
	}
	for _, tc := range cases {
		got := selectOnlyViolation(tc.sql)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("selectOnlyViolation(%q) = %q, want it to contain %q", tc.sql, got, tc.want)
		}
	}
	if got := selectOnlyViolation("SELECT 1"); got != "" {
		t.Fatalf("selectOnlyViolation(SELECT 1) = %q, want empty", got)
	}
	if got := selectOnlyViolation("SELECT substring(name FROM 1 FOR 2) FROM users"); got != "" {
		t.Fatalf("selectOnlyViolation(substring FOR) = %q, want empty", got)
	}
}

func TestExecute_RefusedWhileReadOnlyActive(t *testing.T) {
	f := newFakeRunner()
	f.runner.readOnlyActive = true

	if _, _, err := f.runner.execute(context.Background(), "UPDATE users SET a = 1"); err == nil {
		t.Fatal("execute ran while a read-only query was active")
	}
	if len(f.execs) != 0 {
		t.Fatalf("executed %d statements, want 0", len(f.execs))
	}
}

func TestExecuteReadOnly_RefusesWhenAlreadyActive(t *testing.T) {
	f := newFakeRunner()
	f.runner.beginReadOnly = func(context.Context) (pgx.Tx, error) { return &fakeTx{}, nil }
	f.runner.readOnlyActive = true

	if _, err := f.runner.executeReadOnly(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("executeReadOnly started while one was already active")
	}
}

func TestExecuteReadOnly_ClearsActiveFlag(t *testing.T) {
	f := newFakeRunner()
	f.runner.beginReadOnly = func(context.Context) (pgx.Tx, error) { return &fakeTx{}, nil }

	if _, err := f.runner.executeReadOnly(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("executeReadOnly: %v", err)
	}
	if f.runner.readOnlyActive {
		t.Fatal("readOnlyActive was not cleared after executeReadOnly returned")
	}
}
