package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/config"
)

// cliSessionConfig is a session config pointed at a disposable directory, so the
// CLI tests never touch the user's real /tmp/dbx/sessions.
func cliSessionConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{Session: config.SessionConfig{
		Enabled:       true,
		Dir:           filepath.Join(t.TempDir(), "sessions"),
		RetentionDays: 30,
	}}
}

func readSessionEntries(t *testing.T, dir string) []session.LogEntry {
	t.Helper()
	entries, err := session.NewReader(dir).ReadAll()
	if err != nil {
		t.Fatalf("read session log in %s: %v", dir, err)
	}
	return entries
}

// Scenario: `dbx query` and `dbx pipe` append the statement they ran to the
// session log, with its row count.
func TestQueryAndReport_LogsTheExecutedStatement(t *testing.T) {
	cfg := cliSessionConfig(t)
	conn := &fakeConn{steps: []step{{contains: "SELECT", spec: rowsSpec{rows: [][]any{{1}}}}}}

	var out bytes.Buffer
	if err := queryAndReport(&out, conn, "SELECT 1", false, cfg); err != nil {
		t.Fatalf("queryAndReport: %v", err)
	}

	entries := readSessionEntries(t, cfg.Session.Dir)
	if len(entries) != 1 {
		t.Fatalf("session log has %d entries, want 1", len(entries))
	}
	if e := entries[0]; e.Level != session.LogQuery || e.SQL != "SELECT 1" || e.Rows != 1 {
		t.Errorf("logged %+v, want a query entry for SELECT 1 with one row", e)
	}
}

// Scenario: a statement that fails is logged as an error, not silently dropped.
func TestQueryAndReport_LogsTheFailedStatement(t *testing.T) {
	cfg := cliSessionConfig(t)
	boom := errors.New("boom")
	conn := &fakeConn{steps: []step{{contains: "SELCT", err: boom}}}

	var out bytes.Buffer
	if err := queryAndReport(&out, conn, "SELCT 1", false, cfg); err == nil {
		t.Fatal("queryAndReport returned no error")
	}

	entries := readSessionEntries(t, cfg.Session.Dir)
	if len(entries) != 1 {
		t.Fatalf("session log has %d entries, want 1", len(entries))
	}
	if e := entries[0]; e.Level != session.LogError || e.SQL != "SELCT 1" || !strings.Contains(e.Error, "boom") {
		t.Errorf("logged %+v, want an error entry carrying the driver error", e)
	}
}

// Scenario: session.enabled = false means the CLI writes nothing — not even the
// directory is created.
func TestQueryAndReport_SessionDisabledWritesNothing(t *testing.T) {
	cfg := cliSessionConfig(t)
	cfg.Session.Enabled = false
	conn := &fakeConn{steps: []step{{contains: "SELECT", spec: rowsSpec{rows: [][]any{{1}}}}}}

	var out bytes.Buffer
	if err := queryAndReport(&out, conn, "SELECT 1", false, cfg); err != nil {
		t.Fatalf("queryAndReport: %v", err)
	}
	if _, err := os.Stat(cfg.Session.Dir); !os.IsNotExist(err) {
		t.Errorf("session dir state = %v, want it not to exist when disabled", err)
	}
}

// Scenario: a log directory that cannot be opened must not stop the command.
func TestOpenSessionLog_AnUnopenableLogIsSilentlyDisabled(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg := &config.Config{Session: config.SessionConfig{
		Enabled:       true,
		Dir:           filepath.Join(blocker, "sessions"),
		RetentionDays: 30,
	}}

	if got := openSessionLog(cfg); got != nil {
		_ = got.Close()
		t.Error("openSessionLog returned a logger for an unopenable directory")
	}
}

// Scenario: a nil config is the "no session log" case rather than a panic.
func TestOpenSessionLog_ANilConfigIsSilentlyDisabled(t *testing.T) {
	if got := openSessionLog(nil); got != nil {
		_ = got.Close()
		t.Error("openSessionLog returned a logger for a nil config")
	}
}

// Scenario: logSessionError is a no-op for a nil error.
func TestLogSessionError_ANilErrorWritesNothing(t *testing.T) {
	cfg := cliSessionConfig(t)
	logSessionError(cfg, "SELECT 1", nil)
	if _, err := os.Stat(cfg.Session.Dir); !os.IsNotExist(err) {
		t.Errorf("session dir state = %v, want nothing written for a nil error", err)
	}
}

// Scenario: `dbx ask` logs the statement it executed once the review step let it
// through.
func TestAskAndReport_LogsTheExecutedStatement(t *testing.T) {
	cfg := cliSessionConfig(t)
	conn := askConn()
	provider := &stubProvider{name: "test-provider", sql: "SELECT id FROM users"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "users?", false, cfg, nil, askOptions{}); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	entries := readSessionEntries(t, cfg.Session.Dir)
	if len(entries) != 1 {
		t.Fatalf("session log has %d entries, want 1", len(entries))
	}
	if e := entries[0]; e.Level != session.LogQuery || e.SQL != "SELECT id FROM users" {
		t.Errorf("logged %+v, want a query entry for the generated SQL", e)
	}
}

// Scenario: the statement `dbx ask` executed and the database refused is logged
// as an error.
func TestAskAndReport_LogsTheFailedStatement(t *testing.T) {
	cfg := cliSessionConfig(t)
	boom := errors.New("permission denied")
	conn := &fakeConn{steps: []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows([]string{"public"})}},
		{contains: "information_schema.tables", spec: rowsSpec{rows: [][]any{{"users", "BASE TABLE", 3}}}},
		{contains: "information_schema.columns", spec: rowsSpec{rows: [][]any{{"id", "integer", "NO", nil}}}},
		{contains: "SELECT id FROM users", err: boom},
	}}
	provider := &stubProvider{name: "test-provider", sql: "SELECT id FROM users"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "users?", false, cfg, nil, askOptions{}); err == nil {
		t.Fatal("askAndReport returned no error")
	}

	entries := readSessionEntries(t, cfg.Session.Dir)
	if len(entries) != 1 {
		t.Fatalf("session log has %d entries, want 1", len(entries))
	}
	if e := entries[0]; e.Level != session.LogError || e.SQL != "SELECT id FROM users" {
		t.Errorf("logged %+v, want an error entry for the generated SQL", e)
	}
}
