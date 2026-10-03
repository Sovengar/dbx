package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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

// Conn is everything the CLI and the schema exporter need from a database
// connection: run a query, read a single value, and close.
//
// Depending on this rather than on *pgx.Conn is what lets `dbx context`,
// `dbx pipe` and the schema loader be tested without a live PostgreSQL. The
// interface is deliberately the three methods actually called, so a fake is
// three methods wide and not a reimplementation of pgx.
type Conn interface {
	Querier
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close(ctx context.Context) error
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
