package postgres

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type copyConn struct {
	toSQL   string
	fromSQL string
	input   string
	output  string
	err     error
}

func (c *copyConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *copyConn) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (c *copyConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (c *copyConn) Ping(context.Context) error  { return nil }
func (c *copyConn) Close(context.Context) error { return nil }
func (c *copyConn) CopyTo(_ context.Context, dst io.Writer, sql string) (pgconn.CommandTag, error) {
	c.toSQL = sql
	if c.err != nil {
		return pgconn.CommandTag{}, c.err
	}
	_, _ = io.WriteString(dst, c.output)
	return pgconn.NewCommandTag("COPY 2"), nil
}
func (c *copyConn) CopyFrom(_ context.Context, src io.Reader, sql string) (pgconn.CommandTag, error) {
	c.fromSQL = sql
	data, _ := io.ReadAll(src)
	c.input = string(data)
	if c.err != nil {
		return pgconn.CommandTag{}, c.err
	}
	return pgconn.NewCommandTag("COPY 3"), nil
}

func TestCopyTableToStreamsCSVThroughTheProtocol(t *testing.T) {
	conn := &copyConn{output: "id,name\n1,Ada\n2,Grace\n"}
	var dst bytes.Buffer

	rows, err := CopyTableTo(context.Background(), conn, &dst, "odd schema", `order"items`, []string{"id", "display name"})
	if err != nil {
		t.Fatalf("CopyTableTo: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2", rows)
	}
	if dst.String() != conn.output {
		t.Errorf("output = %q, want %q", dst.String(), conn.output)
	}
	want := `COPY "odd schema"."order""items" ("id","display name") TO STDOUT WITH (FORMAT csv, HEADER true)`
	if conn.toSQL != want {
		t.Errorf("SQL = %q, want %q", conn.toSQL, want)
	}
}

func TestCopyTableFromStreamsCSVThroughTheProtocol(t *testing.T) {
	conn := &copyConn{}
	input := "id,name\n1,Ada\n2,Grace\n3,Linus\n"

	rows, err := CopyTableFrom(context.Background(), conn, strings.NewReader(input), "public", "users", nil)
	if err != nil {
		t.Fatalf("CopyTableFrom: %v", err)
	}
	if rows != 3 || conn.input != input {
		t.Errorf("rows=%d input=%q, want 3 and the original stream", rows, conn.input)
	}
	want := `COPY "public"."users" FROM STDIN WITH (FORMAT csv, HEADER true)`
	if conn.fromSQL != want {
		t.Errorf("SQL = %q, want %q", conn.fromSQL, want)
	}
}

func TestCopyProtocolErrorsRemainActionable(t *testing.T) {
	boom := errors.New("permission denied")
	conn := &copyConn{err: boom}

	if _, err := CopyTableTo(context.Background(), conn, io.Discard, "public", "users", nil); !errors.Is(err, boom) || !strings.Contains(err.Error(), "COPY TO") {
		t.Errorf("CopyTableTo error = %v, want wrapped COPY TO failure", err)
	}
	if _, err := CopyTableFrom(context.Background(), conn, strings.NewReader("id\n"), "public", "users", nil); !errors.Is(err, boom) || !strings.Contains(err.Error(), "COPY FROM") {
		t.Errorf("CopyTableFrom error = %v, want wrapped COPY FROM failure", err)
	}
}

func TestCopyStatementRejectsIncompleteTargetsAndColumns(t *testing.T) {
	for _, tc := range []struct {
		name      string
		schema    string
		table     string
		columns   []string
		direction string
	}{
		{name: "missing schema", table: "users", direction: "TO"},
		{name: "missing table", schema: "public", direction: "TO"},
		{name: "empty column", schema: "public", table: "users", columns: []string{"id", ""}, direction: "FROM"},
		{name: "unknown direction", schema: "public", table: "users", direction: "SIDEWAYS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := copyStatement(tc.schema, tc.table, tc.columns, tc.direction); err == nil {
				t.Fatal("invalid COPY statement was accepted")
			}
		})
	}
}
