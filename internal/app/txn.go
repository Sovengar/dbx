package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

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
	// mu guards tx. It is written from tea.Cmd goroutines and read from
	// Update, so access must be synchronized.
	mu sync.Mutex
	tx pgx.Tx
	// readOnlyActive reports that an ASK read-only transaction is open on the
	// shared connection. Other DB commands must not run while it is set, or
	// they would either fail read-only or be rolled back with it. It is read
	// from Update and written from a tea.Cmd goroutine, hence atomic.
	readOnlyActive atomic.Bool
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
func (r *statementRunner) pending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tx != nil
}

// readOnlyBusy reports whether an ASK read-only transaction is currently open.
// It is safe to call from Update while a tea.Cmd goroutine is executing.
func (r *statementRunner) readOnlyBusy() bool { return r.readOnlyActive.Load() }

// execute runs every statement of sql in order and returns the last result.
// It reports whether a pending DML transaction was committed along the way.
//
// A new execution closes the transaction opened by the previous one: the
// user's rollback window is the statement (or batch) currently being run.
func (r *statementRunner) execute(ctx context.Context, sql string) (*postgres.QueryResult, bool, error) {
	if r.readOnlyActive.Load() {
		return nil, false, fmt.Errorf("a read-only ASK query is still running")
	}
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
		r.mu.Lock()
		if r.tx == nil {
			tx, err := r.begin(ctx)
			if err != nil {
				r.mu.Unlock()
				return nil, false, fmt.Errorf("failed to begin transaction: %w", err)
			}
			r.tx = tx
		}
		tx := r.tx
		r.mu.Unlock()
		return tx, false, nil
	}
	if r.pending() {
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
	r.mu.Lock()
	tx := r.tx
	r.tx = nil
	r.mu.Unlock()
	if tx == nil {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}

// rollback discards the pending transaction and reports whether there was one.
func (r *statementRunner) rollback(ctx context.Context) (bool, error) {
	r.mu.Lock()
	tx := r.tx
	r.tx = nil
	r.mu.Unlock()
	if tx == nil {
		return false, nil
	}
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
	if r.readOnlyActive.Load() {
		return nil, fmt.Errorf("a read-only query is already running")
	}
	if r.beginReadOnly == nil {
		return nil, fmt.Errorf("read-only transactions are not available")
	}
	tx, err := r.beginReadOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin read-only transaction: %w", err)
	}
	r.readOnlyActive.Store(true)
	defer func() {
		r.readOnlyActive.Store(false)
		_ = tx.Rollback(ctx)
	}()

	result, err := r.exec(ctx, tx, sql)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// forbiddenStatementKeywords are mutation/DDL keywords that must never appear
// in a statement run through the ASK read-only path. WITH is intentionally
// absent: a data-modifying CTE is caught by its inner DELETE/INSERT/UPDATE.
// INTO catches SELECT ... INTO; NEXTVAL catches sequence advancement, which
// the READ ONLY transaction would also reject. FOR is handled separately
// because it is also a legal clause in expressions such as
// substring(x FROM 1 FOR 2).
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
	"INTO":     true,
	"NEXTVAL":  true,
}

// lockingForKeywords are the tokens that turn a FOR clause into a row lock
// (FOR UPDATE / FOR SHARE / FOR NO KEY UPDATE / FOR KEY SHARE / FOR UPDATE OF).
var lockingForKeywords = map[string]bool{
	"UPDATE": true,
	"SHARE":  true,
	"KEY":    true,
	"NO":     true,
}

// isSelectOnly reports whether sql is a single SELECT or WITH ... SELECT
// statement. It is the client-side half of the SELECT-only defense; the
// READ ONLY transaction is the authoritative one. It tolerates leading
// comments and stray semicolons, and rejects data-modifying CTEs.
func isSelectOnly(sql string) bool {
	return selectOnlyViolation(sql) == ""
}

// selectOnlyViolation returns a human-readable reason when sql is not a single
// read-only SELECT, or "" when it is allowed.
func selectOnlyViolation(sql string) string {
	body := ""
	count := 0
	for _, stmt := range splitSQL(sql) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		body = stmt
		count++
	}
	if count == 0 {
		return "empty statement"
	}
	if count > 1 {
		return "only a single statement is allowed"
	}

	tokens := sqlKeywordTokens(strings.ToUpper(maskNonCode(body)))
	if len(tokens) == 0 {
		return "empty statement"
	}
	if tokens[0] != "SELECT" && tokens[0] != "WITH" {
		return fmt.Sprintf("%s statements are not allowed", tokens[0])
	}
	for i, tok := range tokens {
		if forbiddenStatementKeywords[tok] {
			return fmt.Sprintf("%s is not allowed in an ASK query", tok)
		}
		if tok == "FOR" && i+1 < len(tokens) && lockingForKeywords[tokens[i+1]] {
			return "FOR UPDATE/SHARE row locking is not allowed in an ASK query"
		}
	}
	if tokens[0] == "WITH" {
		for _, tok := range tokens {
			if tok == "SELECT" {
				return ""
			}
		}
		return "WITH queries must end in a SELECT"
	}
	return ""
}

// maskNonCode returns a copy of sql with string literals (including E-strings
// and dollar-quoted strings), quoted identifiers and comments replaced by
// spaces, preserving byte length. Callers can then find top-level separators or
// keywords without being fooled by their contents.
func maskNonCode(sql string) string {
	out := []byte(sql)
	blank := func(from, to int) {
		if from < 0 {
			from = 0
		}
		if to > len(out) {
			to = len(out)
		}
		for i := from; i < to; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}

	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '\'':
			start := i
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			blank(start, i)
		case (sql[i] == 'e' || sql[i] == 'E') && i+1 < len(sql) && sql[i+1] == '\'' &&
			(i == 0 || !isIdentifierByte(sql[i-1])):
			// E-string: backslash escapes are honored.
			start := i
			i += 2
			for i < len(sql) {
				if sql[i] == '\\' && i+1 < len(sql) {
					i += 2
					continue
				}
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			blank(start, i)
		case sql[i] == '"':
			start := i
			i++
			for i < len(sql) {
				if sql[i] == '"' {
					if i+1 < len(sql) && sql[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			blank(start, i)
		case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
			start := i
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			blank(start, i)
		case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
			start := i
			i += 2
			for i+1 < len(sql) && (sql[i] != '*' || sql[i+1] != '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			} else {
				i = len(sql)
			}
			blank(start, i)
		case sql[i] == '$':
			if end, ok := matchDollarQuote(sql, i); ok {
				blank(i, end)
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

// matchDollarQuote reports the end index of the dollar-quoted string starting
// at i (sql[i] == '$'), if one starts there and is terminated.
func matchDollarQuote(sql string, i int) (int, bool) {
	j := i + 1
	for j < len(sql) && sql[j] != '$' {
		if !isSQLIdentChar(sql[j]) {
			return 0, false
		}
		j++
	}
	if j >= len(sql) || sql[j] != '$' {
		return 0, false
	}
	tag := sql[i : j+1]
	idx := strings.Index(sql[j+1:], tag)
	if idx < 0 {
		return 0, false
	}
	return j + 1 + idx + len(tag), true
}

// isSQLIdentChar reports whether b can appear in an unquoted identifier or a
// dollar-quote tag. Dollar-quote tags cannot contain '$', so callers that need
// the identifier rule (which allows '$') should use isIdentifierByte.
func isSQLIdentChar(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b >= 0x80
}

// isIdentifierByte reports whether b can appear inside a PostgreSQL identifier.
// Unlike a dollar-quote tag, identifiers may contain '$', and non-ASCII bytes
// are legal, so an E'...' immediately preceded by either is part of a longer
// identifier rather than an E-string.
func isIdentifierByte(b byte) bool {
	return isSQLIdentChar(b) || b == '$'
}

// sqlKeywordTokens splits cleaned SQL into upper-case keyword-ish tokens.
func sqlKeywordTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'A' || r > 'Z') && r != '_'
	})
}
