package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

var productionCLIOpenSSHTunnel = openSSHTunnel

type cliPgConnProvider struct {
	*fakeConn
	pgConn *pgconn.PgConn
}

func (p *cliPgConnProvider) PgConn() *pgconn.PgConn { return p.pgConn }

func TestTheDefaultCLISSHOpenerRejectsInvalidTargets(t *testing.T) {
	if _, _, err := productionCLIOpenSSHTunnel("postgres://db.internal/app", ""); err == nil {
		t.Fatal("an empty SSH target was accepted")
	}
	if _, _, err := productionCLIOpenSSHTunnel("postgres://db.internal/app", "-oProxyCommand=bad"); err == nil {
		t.Fatal("an SSH option was accepted as a target")
	}
}

func TestTheTunneledCLIConnectionExposesTheDriverConnection(t *testing.T) {
	t.Run("without a provider it exposes nothing", func(t *testing.T) {
		conn := &tunneledCLIConn{pgconnPing: &fakeConn{}, tunnel: &fakeTunnel{}}
		if got := conn.PgConn(); got != nil {
			t.Errorf("PgConn = %v, want nil for a connection without one", got)
		}
	})

	t.Run("with a provider it returns that connection", func(t *testing.T) {
		want := &pgconn.PgConn{}
		conn := &tunneledCLIConn{
			pgconnPing: &cliPgConnProvider{fakeConn: &fakeConn{}, pgConn: want},
			tunnel:     &fakeTunnel{},
		}
		if got := conn.PgConn(); got != want {
			t.Errorf("PgConn = %p, want the provider's %p", got, want)
		}
	})
}

func TestFindLocalConnectionRefusesAnEntryWithoutADSN(t *testing.T) {
	dir := t.TempDir()
	body := "[connections.empty]\ndriver = \"postgres\"\n"
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

	conn, ok := findLocalConnection("")
	if ok {
		t.Errorf("findLocalConnection returned %+v for an entry without a DSN", conn)
	}
	if dsn := findLocalDSN(""); dsn != "" {
		t.Errorf("findLocalDSN returned %q for an entry without a DSN", dsn)
	}
}
