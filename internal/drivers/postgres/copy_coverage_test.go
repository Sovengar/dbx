package postgres

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type plainConn struct{}

func (plainConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (plainConn) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (plainConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (plainConn) Ping(context.Context) error  { return nil }
func (plainConn) Close(context.Context) error { return nil }

type pgConnOnly struct {
	plainConn
	pgConn *pgconn.PgConn
}

func (c *pgConnOnly) PgConn() *pgconn.PgConn { return c.pgConn }

func TestCopyTableToRefusesAConnectionWithoutTheProtocol(t *testing.T) {
	if _, err := CopyTableTo(context.Background(), plainConn{}, io.Discard, "public", "users", nil); err == nil {
		t.Fatal("COPY TO accepted a connection without the protocol")
	}
}

func TestCopyTableFromRefusesAConnectionWithoutTheProtocol(t *testing.T) {
	if _, err := CopyTableFrom(context.Background(), plainConn{}, nil, "public", "users", nil); err == nil {
		t.Fatal("COPY FROM accepted a connection without the protocol")
	}
}

func TestCopyTableToRefusesAnIncompleteTarget(t *testing.T) {
	if _, err := CopyTableTo(context.Background(), &copyConn{}, io.Discard, "", "users", nil); err == nil {
		t.Fatal("COPY TO accepted a target without a schema")
	}
}

func TestCopyTableFromRefusesAnIncompleteTarget(t *testing.T) {
	if _, err := CopyTableFrom(context.Background(), &copyConn{}, nil, "public", "", nil); err == nil {
		t.Fatal("COPY FROM accepted a target without a table")
	}
}

func TestCopyProtocolUsesTheDriverConnectionWhenItCan(t *testing.T) {
	want := &pgconn.PgConn{}
	got, err := copyProtocol(&pgConnOnly{pgConn: want})
	if err != nil {
		t.Fatalf("copyProtocol: %v", err)
	}
	if got != want {
		t.Errorf("copyProtocol = %v, want the provider's connection", got)
	}

	if _, err := copyProtocol(&pgConnOnly{}); err == nil {
		t.Fatal("copyProtocol accepted a provider that returned no connection")
	}
}
