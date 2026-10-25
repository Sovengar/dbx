package postgres

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// rawCopier is the PostgreSQL wire-level COPY surface. A real *pgx.Conn exposes
// it through PgConn; focused tests can implement it directly without a server.
type rawCopier interface {
	CopyTo(context.Context, io.Writer, string) (pgconn.CommandTag, error)
	CopyFrom(context.Context, io.Reader, string) (pgconn.CommandTag, error)
}

type pgConnProvider interface {
	PgConn() *pgconn.PgConn
}

// CopyTableTo streams a table as CSV with a header through PostgreSQL's COPY
// protocol. An empty columns slice means every table column in database order.
func CopyTableTo(ctx context.Context, conn Conn, dst io.Writer, schema, table string, columns []string) (int64, error) {
	copier, err := copyProtocol(conn)
	if err != nil {
		return 0, err
	}

	statement, err := copyStatement(schema, table, columns, "TO")
	if err != nil {
		return 0, err
	}
	tag, err := copier.CopyTo(ctx, dst, statement)
	if err != nil {
		return 0, fmt.Errorf("COPY TO failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CopyTableFrom streams CSV with a header into a table through PostgreSQL's
// COPY protocol. The file's fields must follow the table (or --columns) order.
func CopyTableFrom(ctx context.Context, conn Conn, src io.Reader, schema, table string, columns []string) (int64, error) {
	copier, err := copyProtocol(conn)
	if err != nil {
		return 0, err
	}

	statement, err := copyStatement(schema, table, columns, "FROM")
	if err != nil {
		return 0, err
	}
	tag, err := copier.CopyFrom(ctx, src, statement)
	if err != nil {
		return 0, fmt.Errorf("COPY FROM failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

func copyProtocol(conn Conn) (rawCopier, error) {
	if copier, ok := conn.(rawCopier); ok {
		return copier, nil
	}
	provider, ok := conn.(pgConnProvider)
	if !ok || provider.PgConn() == nil {
		return nil, fmt.Errorf("connection does not support PostgreSQL COPY")
	}
	return provider.PgConn(), nil
}

func copyStatement(schema, table string, columns []string, direction string) (string, error) {
	if schema == "" || table == "" {
		return "", fmt.Errorf("schema and table are required")
	}
	if direction != "TO" && direction != "FROM" {
		return "", fmt.Errorf("invalid COPY direction %q", direction)
	}

	target := pgx.Identifier{schema, table}.Sanitize()
	if len(columns) > 0 {
		quoted := make([]string, len(columns))
		for _, column := range columns {
			if column == "" {
				return "", fmt.Errorf("COPY column names cannot be empty")
			}
		}
		for i, column := range columns {
			quoted[i] = pgx.Identifier{column}.Sanitize()
		}
		target += " (" + strings.Join(quoted, ",") + ")"
	}
	stream := "STDOUT"
	if direction == "FROM" {
		stream = "STDIN"
	}
	return fmt.Sprintf("COPY %s %s %s WITH (FORMAT csv, HEADER true)", target, direction, stream), nil
}
