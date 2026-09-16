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
	exec  func(ctx context.Context, q postgres.Querier, sql string) (*postgres.QueryResult, error)
	tx    pgx.Tx
}

func newStatementRunner(conn *pgx.Conn) *statementRunner {
	return &statementRunner{
		conn:  conn,
		begin: conn.Begin,
		exec:  postgres.ExecuteQuery,
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
	committed := false
	if r.tx != nil {
		if err := r.commitPending(ctx); err != nil {
			return nil, false, err
		}
		committed = true
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
		if err := r.commitPending(ctx); err != nil {
			return nil, false, err
		}
		return r.conn, true, nil
	}
	return r.conn, false, nil
}

// commitPending commits and clears the pending transaction, if any.
func (r *statementRunner) commitPending(ctx context.Context) error {
	if r.tx == nil {
		return nil
	}
	tx := r.tx
	r.tx = nil
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
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
