package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/jackc/pgx/v5"
)

// dmlKeywords are the statement prefixes that mutate data.
//
// WITH is included conservatively: a data-modifying CTE must be
// rollbackable, and treating the whole statement as DML is safer than
// letting it autocommit.
var dmlKeywords = []string{"INSERT", "UPDATE", "DELETE", "WITH"}

// isDML reports whether a statement mutates data and therefore must run
// inside a transaction the user can roll back.
func isDML(sql string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(sql))
	for _, keyword := range dmlKeywords {
		if hasKeywordPrefix(trimmed, keyword) {
			return true
		}
	}
	return false
}

// hasKeywordPrefix reports whether sql starts with keyword as a whole word.
func hasKeywordPrefix(sql, keyword string) bool {
	if !strings.HasPrefix(sql, keyword) {
		return false
	}
	rest := sql[len(keyword):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r' || rest[0] == '('
}

// statementRunner executes SQL while tracking the pending DML transaction.
//
// DML statements share a single transaction that stays open after the
// execution finishes, so the user can still roll it back. Every other
// statement (SELECT, DDL, ...) runs in autocommit after committing any
// pending transaction first.
type statementRunner struct {
	conn  postgres.Querier
	begin func(ctx context.Context) (pgx.Tx, error)
	// beginReadOnly opens a server-enforced READ ONLY transaction. It backs
	// the ASK path so the database itself rejects any mutation even if the
	// statement passes the client-side validation.
	beginReadOnly func(ctx context.Context) (pgx.Tx, error)
	exec          func(ctx context.Context, q postgres.Querier, sql string) (*postgres.QueryResult, error)
	tx            pgx.Tx
}

func newStatementRunner(conn *pgx.Conn) *statementRunner {
	return &statementRunner{
		conn:  conn,
		begin: conn.Begin,
		beginReadOnly: func(ctx context.Context) (pgx.Tx, error) {
			return conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		},
		exec: postgres.ExecuteQuery,
	}
}

// pending reports whether a transaction is waiting to be committed or rolled back.
func (r *statementRunner) pending() bool { return r.tx != nil }

// execute runs every statement of sql in order and returns the last result.
// It reports whether a pending DML transaction was committed along the way.
//
// A new execution closes the transaction opened by the previous one: the
// user's rollback window is the statement (or batch) currently being run.
func (r *statementRunner) execute(ctx context.Context, sql string) (*postgres.QueryResult, bool, error) {
	committed, err := r.commitPending(ctx)
	if err != nil {
		return nil, false, err
	}

	var last *postgres.QueryResult
	for _, stmt := range splitSQL(sql) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		querier, committedNow, err := r.querierFor(ctx, stmt)
		if err != nil {
			return nil, committed, err
		}
		committed = committed || committedNow

		result, err := r.exec(ctx, querier, stmt)
		if err != nil {
			return nil, committed, err
		}
		last = result
	}
	if last == nil {
		last = &postgres.QueryResult{}
	}
	return last, committed, nil
}

// querierFor returns the querier a statement must run on, and reports
// whether a pending transaction was committed to get there. DML reuses the
// pending transaction — opening one when none exists — so a batch of DML
// statements shares a single transaction. Anything else commits the
// pending transaction and runs in autocommit.
func (r *statementRunner) querierFor(ctx context.Context, stmt string) (postgres.Querier, bool, error) {
	if isDML(stmt) {
		if r.tx == nil {
			tx, err := r.begin(ctx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to begin transaction: %w", err)
			}
			r.tx = tx
		}
		return r.tx, false, nil
	}
	if r.tx != nil {
		committed, err := r.commitPending(ctx)
		if err != nil {
			return nil, false, err
		}
		return r.conn, committed, nil
	}
	return r.conn, false, nil
}

// commitPending commits and clears the pending transaction, if any, and
// reports whether there was one.
func (r *statementRunner) commitPending(ctx context.Context) (bool, error) {
	if r.tx == nil {
		return false, nil
	}
	tx := r.tx
	r.tx = nil
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}

// rollback discards the pending transaction and reports whether there was one.
func (r *statementRunner) rollback(ctx context.Context) (bool, error) {
	if r.tx == nil {
		return false, nil
	}
	tx := r.tx
	r.tx = nil
	if err := tx.Rollback(ctx); err != nil {
		return true, fmt.Errorf("failed to roll back transaction: %w", err)
	}
	return true, nil
}

// executeReadOnly runs sql inside a server-enforced READ ONLY transaction and
// discards it (nothing is ever committed). It refuses to run while a DML
// transaction is pending because pgx cannot open a second transaction on the
// same connection.
func (r *statementRunner) executeReadOnly(ctx context.Context, sql string) (*postgres.QueryResult, error) {
	if r.pending() {
		return nil, fmt.Errorf("a DML transaction is pending: commit or roll back first")
	}
	if r.beginReadOnly == nil {
		return nil, fmt.Errorf("read-only transactions are not available")
	}
	tx, err := r.beginReadOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := r.exec(ctx, tx, sql)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// forbiddenStatementKeywords are mutation/DDL keywords that must never appear
// in a statement run through the ASK read-only path. WITH is intentionally
// absent: a data-modifying CTE is caught by its inner DELETE/INSERT/UPDATE.
var forbiddenStatementKeywords = map[string]bool{
	"INSERT":   true,
	"UPDATE":   true,
	"DELETE":   true,
	"MERGE":    true,
	"TRUNCATE": true,
	"ALTER":    true,
	"DROP":     true,
	"CREATE":   true,
	"GRANT":    true,
	"REVOKE":   true,
	"COPY":     true,
	"CALL":     true,
	"DO":       true,
	"VACUUM":   true,
	"REINDEX":  true,
	"CLUSTER":  true,
	"REFRESH":  true,
	"IMPORT":   true,
}

// isSelectOnly reports whether sql is a single SELECT or WITH ... SELECT
// statement. It is the client-side half of the SELECT-only defense; the
// READ ONLY transaction is the authoritative one. It tolerates leading
// comments and stray semicolons, and rejects data-modifying CTEs.
func isSelectOnly(sql string) bool {
	body := ""
	count := 0
	for _, stmt := range splitSQL(sql) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		body = stmt
		count++
	}
	if count != 1 {
		return false
	}

	tokens := sqlKeywordTokens(strings.ToUpper(stripSQLLiterals(body)))
	if len(tokens) == 0 {
		return false
	}
	if tokens[0] != "SELECT" && tokens[0] != "WITH" {
		return false
	}
	for _, tok := range tokens {
		if forbiddenStatementKeywords[tok] {
			return false
		}
	}
	if tokens[0] == "WITH" {
		for _, tok := range tokens {
			if tok == "SELECT" {
				return true
			}
		}
		return false
	}
	return true
}

// stripSQLLiterals removes string literals, quoted identifiers and comments so
// keyword scanning cannot be fooled by their contents.
func stripSQLLiterals(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\'':
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			b.WriteByte(' ')
		case ch == '"':
			i++
			for i < len(sql) && sql[i] != '"' {
				i++
			}
			b.WriteByte(' ')
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i++
			b.WriteByte(' ')
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// sqlKeywordTokens splits cleaned SQL into upper-case keyword-ish tokens.
func sqlKeywordTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && r != '_'
	})
}
