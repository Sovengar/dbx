package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/jackc/pgx/v5"
)

type fakeAppTunnel struct {
	err    error
	closed int
}

func (t *fakeAppTunnel) Close() error {
	t.closed++
	return nil
}
func (t *fakeAppTunnel) Err() error { return t.err }

type fakeBeginner struct{ *pgxfake.Conn }

func (c *fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return nil, nil }
func (c *fakeBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, nil
}

func tunneledProject() config.FoundProject {
	return config.FoundProject{
		Name: "prod",
		Connection: config.ProjectConnection{
			Driver:    "postgres",
			DSN:       "postgres://db.internal/app",
			SSHTunnel: "bastion",
		},
	}
}

func TestProjectSSHTunnelIsOpenedAndOwnedByTheTUIConnection(t *testing.T) {
	oldOpen, oldConnect := openSSHTunnel, pgxConnect
	t.Cleanup(func() { openSSHTunnel, pgxConnect = oldOpen, oldConnect })
	tunnel := &fakeAppTunnel{}
	openSSHTunnel = func(dsn, via string) (string, appTunnelLifecycle, error) {
		if dsn != "postgres://db.internal/app" || via != "bastion" {
			t.Errorf("Open(%q, %q)", dsn, via)
		}
		return "postgres://127.0.0.1:43210/app", tunnel, nil
	}
	db := &fakeBeginner{Conn: pgxfake.New()}
	pgxConnect = func(_ context.Context, dsn string) (postgres.Conn, error) {
		if dsn != "postgres://127.0.0.1:43210/app" {
			t.Errorf("driver DSN = %q", dsn)
		}
		return db, nil
	}

	msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if _, ok := msg.conn.(postgres.TxBeginner); !ok {
		t.Errorf("tunneled connection %T cannot open transactions", msg.conn)
	}
	if db.Pinged() != 1 {
		t.Errorf("database pinged %d times", db.Pinged())
	}
	if err := msg.conn.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if db.Closed() != 1 || tunnel.closed != 1 {
		t.Errorf("close counts: database=%d tunnel=%d", db.Closed(), tunnel.closed)
	}
}

func TestTUIClosesTheTunnelOnEveryConnectionFailure(t *testing.T) {
	oldOpen, oldConnect := openSSHTunnel, pgxConnect
	t.Cleanup(func() { openSSHTunnel, pgxConnect = oldOpen, oldConnect })

	t.Run("opening", func(t *testing.T) {
		want := errors.New("ssh unavailable")
		openSSHTunnel = func(string, string) (string, appTunnelLifecycle, error) { return "", nil, want }
		msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
		if !errors.Is(msg.err, want) {
			t.Errorf("error = %v", msg.err)
		}
	})

	t.Run("SSH process", func(t *testing.T) {
		want := errors.New("permission denied")
		tunnel := &fakeAppTunnel{err: want}
		openSSHTunnel = func(string, string) (string, appTunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		pgxConnect = func(context.Context, string) (postgres.Conn, error) {
			return nil, errors.New("unexpected EOF")
		}
		msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
		if !errors.Is(msg.err, want) || !strings.Contains(msg.err.Error(), "SSH tunnel") {
			t.Errorf("error = %v", msg.err)
		}
		if tunnel.closed != 1 {
			t.Errorf("tunnel closed %d times", tunnel.closed)
		}
	})

	t.Run("database dial", func(t *testing.T) {
		tunnel := &fakeAppTunnel{}
		openSSHTunnel = func(string, string) (string, appTunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		want := errors.New("connection refused")
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return nil, want }
		msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
		if !errors.Is(msg.err, want) {
			t.Errorf("error = %v", msg.err)
		}
		if tunnel.closed != 1 {
			t.Errorf("tunnel closed %d times", tunnel.closed)
		}
	})

	t.Run("database ping", func(t *testing.T) {
		tunnel := &fakeAppTunnel{}
		openSSHTunnel = func(string, string) (string, appTunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		want := errors.New("not ready")
		db := pgxfake.New()
		db.PingErr = want
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return db, nil }
		msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
		if !errors.Is(msg.err, want) {
			t.Errorf("error = %v", msg.err)
		}
		if db.Closed() != 1 || tunnel.closed != 1 {
			t.Errorf("close counts: database=%d tunnel=%d", db.Closed(), tunnel.closed)
		}
	})

	t.Run("connection without transactions", func(t *testing.T) {
		tunnel := &fakeAppTunnel{}
		openSSHTunnel = func(string, string) (string, appTunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		db := pgxfake.New()
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return db, nil }
		msg := m0(t).connectToDB(tunneledProject())().(dbConnectedMsg)
		if msg.err == nil || !strings.Contains(msg.err.Error(), "transactions") {
			t.Errorf("error = %v", msg.err)
		}
		if db.Closed() != 1 || tunnel.closed != 1 {
			t.Errorf("close counts: database=%d tunnel=%d", db.Closed(), tunnel.closed)
		}
	})
}

func TestConnectionRepliesThatCannotBeUsedAreClosed(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		m := m0(t)
		m.connectCancelled = true
		db := pgxfake.New()
		_, _ = fold(m, dbConnectedMsg{conn: db})
		if db.Closed() != 1 {
			t.Errorf("cancelled connection closed %d times", db.Closed())
		}
	})

	t.Run("no transaction support", func(t *testing.T) {
		m := m0(t)
		db := pgxfake.New()
		project := tunneledProject()
		out, _ := fold(m, dbConnectedMsg{conn: db, project: &project})
		if out.state != StateError || db.Closed() != 1 {
			t.Errorf("state=%v close count=%d", out.state, db.Closed())
		}
	})
}

var _ postgres.TxBeginner = (*tunneledAppConn)(nil)
