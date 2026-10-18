package cli

import (
	"time"

	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/config"
)

// openSessionLog returns a logger for one CLI execution, or nil when
// session.enabled is false, the config could not be loaded, or the log file
// cannot be opened.
//
// A CLI command must not fail because its audit log could not be opened: the
// query is the job and the log is the record. The error is dropped and the caller
// writes nothing, which is the same contract app.NewModel keeps for the TUI.
//
// The retention sweep runs here as well as in the TUI's Init, because a machine
// that only ever runs the CLI would otherwise never prune a file. It is one
// directory walk per executed statement, over a handful of daily files.
func openSessionLog(cfg *config.Config) *session.Logger {
	if cfg == nil || !cfg.Session.Enabled {
		return nil
	}
	logger, err := session.NewLogger(cfg.Session.Dir, cfg.Session.RetentionDays)
	if err != nil {
		return nil
	}
	_ = logger.Cleanup()
	return logger
}

// logSessionQuery records one statement that executed.
//
// The logger is opened and closed around the single write because a CLI
// invocation runs one statement: there is nothing to keep open, and opening only
// here means a command that never executes (ask --dry-run, a declined confirm)
// does not leave an empty daily file behind.
func logSessionQuery(cfg *config.Config, sql string, d time.Duration, rows int) {
	logger := openSessionLog(cfg)
	if logger == nil {
		return
	}
	defer func() { _ = logger.Close() }()
	_ = logger.LogQuery(sql, d, rows)
}

// logSessionError records one statement that failed to execute. A nil error is
// not an error and produces no entry.
func logSessionError(cfg *config.Config, sql string, err error) {
	if err == nil {
		return
	}
	logger := openSessionLog(cfg)
	if logger == nil {
		return
	}
	defer func() { _ = logger.Close() }()
	_ = logger.LogError(sql, err)
}
