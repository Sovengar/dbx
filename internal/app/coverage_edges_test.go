package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/buble/dbx/internal/ui/components/grid"
)

var productionOpenSSHTunnel = openSSHTunnel

type appPgConnProvider struct {
	*fakeBeginner
	pgConn *pgconn.PgConn
}

func (p *appPgConnProvider) PgConn() *pgconn.PgConn { return p.pgConn }

func TestTheDefaultAppSSHTunnelOpenerRejectsInvalidTargets(t *testing.T) {
	if _, _, err := productionOpenSSHTunnel("postgres://db.internal/app", ""); err == nil {
		t.Fatal("an empty SSH target was accepted")
	}
	if _, _, err := productionOpenSSHTunnel("postgres://db.internal/app", "-oProxyCommand=bad"); err == nil {
		t.Fatal("an SSH option was accepted as a target")
	}
}

func TestTheTunneledAppConnectionExposesTheDriverConnection(t *testing.T) {
	t.Run("without a provider it exposes nothing", func(t *testing.T) {
		conn := &tunneledAppConn{TxBeginner: &fakeBeginner{Conn: pgxfake.New()}}
		if got := conn.PgConn(); got != nil {
			t.Errorf("PgConn = %v, want nil for a connection without one", got)
		}
	})

	t.Run("with a provider it returns that connection", func(t *testing.T) {
		want := &pgconn.PgConn{}
		conn := &tunneledAppConn{TxBeginner: &appPgConnProvider{
			fakeBeginner: &fakeBeginner{Conn: pgxfake.New()},
			pgConn:       want,
		}}
		if got := conn.PgConn(); got != want {
			t.Errorf("PgConn = %p, want the provider's %p", got, want)
		}
	})
}

func TestGridCopyExportRefusesAModelItCannotUse(t *testing.T) {
	msg := grid.ExportSelectedMsg{Format: grid.ExportCopyCSV, Schema: "public", Table: "users"}

	t.Run("no connection", func(t *testing.T) {
		m := routerModelLoaded(t)
		done := m.handleCopyExport(msg)().(exportDoneMsg)
		if done.err == nil || !strings.Contains(done.err.Error(), "not connected") {
			t.Errorf("error = %v, want a refusal to export without a connection", done.err)
		}
	})

	t.Run("no project directory", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = &copyExportConn{}
		done := m.handleCopyExport(msg)().(exportDoneMsg)
		if done.err == nil || !strings.Contains(done.err.Error(), "no project") {
			t.Errorf("error = %v, want a refusal to export without a project", done.err)
		}
	})

	t.Run("an unopenable destination", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		m := routerModelLoaded(t)
		m.conn = &copyExportConn{}
		m.project = &config.FoundProject{Name: "p", Path: file}
		done := m.handleCopyExport(msg)().(exportDoneMsg)
		if done.err == nil {
			t.Fatal("an unopenable destination reported success")
		}
		if _, err := os.Stat(file); err != nil {
			t.Errorf("the destination was disturbed: %v", err)
		}
	})
}

func TestGridCopyExportRemovesTheFileWhenTheCopyFails(t *testing.T) {
	project := t.TempDir()
	m := routerModelLoaded(t)
	m.project = foundProjectIn(project)
	m.conn = &copyExportConn{data: "id\n1\n", err: os.ErrDeadlineExceeded}

	done := m.handleCopyExport(grid.ExportSelectedMsg{
		Format: grid.ExportCopyCSV,
		Schema: "public",
		Table:  "users",
	})().(exportDoneMsg)
	if done.err == nil {
		t.Fatal("a failed COPY reported success")
	}
	if _, err := os.Stat(filepath.Join(project, "public_users.csv")); !os.IsNotExist(err) {
		t.Errorf("a partial export remains: %v", err)
	}
}

func TestGridCopyExportClosesItsStreams(t *testing.T) {
	for _, tc := range []struct {
		name      string
		compress  bool
		filename  string
		wantError string
	}{
		{name: "plain file", filename: "public_users.csv", wantError: "failed to finish export"},
		{name: "gzip stream", compress: true, filename: "public_users.csv.gz", wantError: "failed to finish gzip export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			path := filepath.Join(project, tc.filename)
			m := routerModelLoaded(t)
			m.project = foundProjectIn(project)
			m.config = &config.Config{UI: config.UIConfig{ExportCompress: tc.compress}}
			m.conn = &fdClosingCopyConn{
				copyExportConn: copyExportConn{data: "id\n1\n2\n"},
				path:           path,
				t:              t,
			}

			done := m.handleCopyExport(grid.ExportSelectedMsg{
				Format: grid.ExportCopyCSV,
				Schema: "public",
				Table:  "users",
			})().(exportDoneMsg)
			if done.err == nil {
				t.Fatal("a stream that could not be closed reported success")
			}
			if !strings.Contains(done.err.Error(), tc.wantError) {
				t.Errorf("error = %q, want it to mention %q", done.err, tc.wantError)
			}
		})
	}
}
