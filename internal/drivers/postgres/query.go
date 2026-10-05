package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type QueryResult struct {
	Columns []ColumnInfo
	Rows    [][]interface{}
	Count   int
}

type SelectOptions struct {
	Schema   string
	Where    string
	OrderBy  string
	OrderDir string
	Limit    int
	Offset   int
}

func (s *SchemaLoader) Select(ctx context.Context, table string, opts SelectOptions) (*QueryResult, error) {
	result := &QueryResult{}

	query := fmt.Sprintf("SELECT * FROM %q.%q", opts.Schema, table)

	if opts.Where != "" {
		query += " WHERE " + opts.Where
	}

	if opts.OrderBy != "" {
		dir := "ASC"
		if strings.EqualFold(opts.OrderDir, "DESC") {
			dir = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", opts.OrderBy, dir)
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
		if opts.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", opts.Offset)
		}
	}

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w\nQuery was: %s", err, query)
	}
	defer rows.Close()

	fieldDescriptions := rows.FieldDescriptions()
	for _, fd := range fieldDescriptions {
		result.Columns = append(result.Columns, ColumnInfo{
			Name:     fd.Name,
			DataType: dataTypeOID(fd.DataTypeOID),
		})
	}

	collected, err := collectRows(rows)
	if err != nil {
		return nil, err
	}
	result.Rows = collected

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	result.Count = len(result.Rows)

	return result, nil
}

// rowIterator is the part of pgx.Rows that turning a result set into values needs: advance,
// and read the current row. pgx.Rows satisfies it, and so does a three-line fake.
//
// It exists for one reason. The decode failure inside the loop — pgx unable to turn a field
// into an `any` — is the one error arm of a query that a connection fake cannot produce: it
// needs a result set the DRIVER itself rejects, so scripting the query's own failure, or the
// iteration's (Next), never reaches it. Taking the iteration out as a function of this
// interface is what turns "needs a broken server" into "needs three lines".
type rowIterator interface {
	Next() bool
	Values() ([]any, error)
}

// collectRows drains rows into slices of values.
//
// A function of its argument and nothing else — no connection, no context, no package state —
// which is the whole point of the extraction. The error it returns is the one a caller has
// always seen: "failed to scan row", naming the row rather than the query.
func collectRows(rows rowIterator) ([][]any, error) {
	var out [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		out = append(out, values)
	}
	return out, nil
}

func (s *SchemaLoader) CountRows(ctx context.Context, schema, table string) (int64, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %q.%q`, schema, table)
	var count int64
	err := s.conn.QueryRow(ctx, query).Scan(&count)
	return count, err
}

// Querier is the subset of pgx used to run a statement. Both *pgx.Conn
// (autocommit) and pgx.Tx (pending transaction) satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Conn is everything the CLI, the schema exporter and the TUI need from a database
// connection: run a query, run a statement, read a single value, and close.
//
// Depending on this rather than on *pgx.Conn is what lets `dbx context`,
// `dbx pipe` and the schema loader be tested without a live PostgreSQL. The
// interface is deliberately the methods actually called, so a fake is four
// methods wide and not a reimplementation of pgx.
//
// Exec was added when the app's model moved onto this interface. It is the fourth
// method because the app's insert/update/delete path uses it, and it is on the
// INTERFACE rather than reached through a concrete *pgx.Conn for the same reason
// the rest of it is: a live database cannot fail one statement and answer the next
// on demand, so the arms that handle a rejected write could not be reached at all
// from a test while the model held a concrete connection.
//
// Ping was added for the same reason and with more force. The app asks "is this
// connection still usable?" in exactly one place — after a COMMIT that failed, which
// pgx answers by closing the connection out from under the caller. Whether a failed
// commit leaves a usable connection depends on the server's transaction status, so
// the app cannot tell from the error and has to ask; and asking through a concrete
// *pgx.Conn would have made the question unaskable from a test.
type Conn interface {
	Querier
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// TxBeginner is the part of *pgx.Conn that opens a transaction. It exists so the app's
// statement runner — which needs Begin and BeginTx and nothing else from the connection —
// does not have to hold a concrete *pgx.Conn to get them.
//
// The runner's error handling is the app's most consequential: a commit that fails must
// not be reported as a commit, and a rollback that fails must not be swallowed. None of
// those arms could be reached from a test while the runner took a *pgx.Conn, because a
// live database only fails a commit if something is genuinely wrong with it.
type TxBeginner interface {
	Conn
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
}

func ExecuteQuery(ctx context.Context, q Querier, sql string) (*QueryResult, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &QueryResult{}

	fieldDescriptions := rows.FieldDescriptions()
	for _, fd := range fieldDescriptions {
		result.Columns = append(result.Columns, ColumnInfo{
			Name:     fd.Name,
			DataType: dataTypeOID(fd.DataTypeOID),
		})
	}

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result.Rows = append(result.Rows, values)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	result.Count = len(result.Rows)
	return result, nil
}

func (s *SchemaLoader) ExecuteRaw(ctx context.Context, sql string) (*QueryResult, error) {
	return ExecuteQuery(ctx, s.conn, sql)
}

func dataTypeOID(oid uint32) string {
	switch oid {
	case 16:
		return "bool"
	case 17:
		return "bytea"
	case 20, 21, 23, 26:
		return "integer"
	case 25:
		return "text"
	case 700, 701:
		return "numeric"
	case 1043:
		return "varchar"
	case 1082:
		return "date"
	case 1114:
		return "timestamp"
	case 1184:
		return "timestamptz"
	case 2950:
		return "uuid"
	case 3802:
		return "jsonb"
	case 114:
		return "json"
	default:
		return "unknown"
	}
}
