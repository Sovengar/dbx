package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

type fakeTunnel struct {
	err    error
	closed int
}

func (t *fakeTunnel) Close() error {
	t.closed++
	return nil
}
func (t *fakeTunnel) Err() error { return t.err }

func TestProjectSSHTunnelIsOpenedAndOwnedByTheCLIConnection(t *testing.T) {
	dir := t.TempDir()
	body := "[connections.prod]\ndriver = \"postgres\"\ndsn = \"postgres://db.internal/app\"\nssh_tunnel = \"bastion\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	oldOpen, oldConnect := openSSHTunnel, pgxConnect
	t.Cleanup(func() { openSSHTunnel, pgxConnect = oldOpen, oldConnect })
	tunnel := &fakeTunnel{}
	openSSHTunnel = func(dsn, via string) (string, tunnelLifecycle, error) {
		if dsn != "postgres://db.internal/app" || via != "bastion" {
			t.Errorf("Open(%q, %q)", dsn, via)
		}
		return "postgres://127.0.0.1:43210/app", tunnel, nil
	}
	db := &fakeConn{}
	pgxConnect = func(_ context.Context, dsn string) (pgconnPing, error) {
		if dsn != "postgres://127.0.0.1:43210/app" {
			t.Errorf("driver DSN = %q", dsn)
		}
		return db, nil
	}

	conn, err := getConnection(testCommand(t))
	if err != nil {
		t.Fatal(err)
	}
	if db.pinged != 1 {
		t.Errorf("database pinged %d times", db.pinged)
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if db.closed != 1 || tunnel.closed != 1 {
		t.Errorf("close counts: database=%d tunnel=%d", db.closed, tunnel.closed)
	}
}

func TestCLIClosesTheTunnelOnEveryConnectionFailure(t *testing.T) {
	oldOpen, oldConnect := openSSHTunnel, pgxConnect
	t.Cleanup(func() { openSSHTunnel, pgxConnect = oldOpen, oldConnect })

	t.Run("opening", func(t *testing.T) {
		want := errors.New("ssh unavailable")
		openSSHTunnel = func(string, string) (string, tunnelLifecycle, error) { return "", nil, want }
		if _, err := connectConfigured(context.Background(), "postgres://db/app", "bastion"); !errors.Is(err, want) {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("SSH process", func(t *testing.T) {
		want := errors.New("permission denied")
		tunnel := &fakeTunnel{err: want}
		openSSHTunnel = func(string, string) (string, tunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		pgxConnect = func(context.Context, string) (pgconnPing, error) {
			return nil, errors.New("unexpected EOF")
		}
		_, err := connectConfigured(context.Background(), "postgres://db/app", "bastion")
		if !errors.Is(err, want) || !strings.Contains(err.Error(), "SSH tunnel") {
			t.Errorf("error = %v", err)
		}
		if tunnel.closed != 1 {
			t.Errorf("tunnel closed %d times", tunnel.closed)
		}
	})

	t.Run("database dial", func(t *testing.T) {
		tunnel := &fakeTunnel{}
		openSSHTunnel = func(string, string) (string, tunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		want := errors.New("connection refused")
		pgxConnect = func(context.Context, string) (pgconnPing, error) { return nil, want }
		if _, err := connectConfigured(context.Background(), "postgres://db/app", "bastion"); !errors.Is(err, want) {
			t.Errorf("error = %v", err)
		}
		if tunnel.closed != 1 {
			t.Errorf("tunnel closed %d times", tunnel.closed)
		}
	})

	t.Run("database ping", func(t *testing.T) {
		tunnel := &fakeTunnel{}
		openSSHTunnel = func(string, string) (string, tunnelLifecycle, error) {
			return "postgres://127.0.0.1:1/app", tunnel, nil
		}
		want := errors.New("not ready")
		db := &fakeConn{pingErr: want}
		pgxConnect = func(context.Context, string) (pgconnPing, error) { return db, nil }
		if _, err := connectConfigured(context.Background(), "postgres://db/app", "bastion"); !errors.Is(err, want) {
			t.Errorf("error = %v", err)
		}
		if db.closed != 1 || tunnel.closed != 1 {
			t.Errorf("close counts: database=%d tunnel=%d", db.closed, tunnel.closed)
		}
	})
}

var _ postgres.Conn = (*tunneledCLIConn)(nil)
