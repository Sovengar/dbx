package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/drivers/postgres"
)

var replayCmd = &cobra.Command{
	Use:   "replay [session-file]",
	Short: "Replay a session log",
	Long:  `Re-execute all queries from a session log file.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runReplay,
}

func init() {
	replayCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	replayCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	rootCmd.AddCommand(replayCmd)
}

func runReplay(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	reader := session.NewReader(cfg.Session.Dir)
	entries, err := reader.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read session file: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return replayEntries(os.Stdout, os.Stderr, conn, entries, args[0], jsonOutput)
}

func runPipe(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	data, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	if (data.Mode() & os.ModeCharDevice) != 0 {
		return fmt.Errorf("no input provided. Usage: echo \"SELECT 1\" | dbx pipe")
	}

	sql, err := readSQL(os.Stdin)
	if err != nil {
		return err
	}
	if sql == "" {
		return fmt.Errorf("empty SQL input")
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return queryAndReport(os.Stdout, conn, sql, jsonOutput)
}

// readSQL reads a whole stream and trims it.
//
// Piping is how this command is meant to be used, so the reading has to handle
// input that arrives in pieces larger or smaller than any buffer, and a read that
// returns data together with io.EOF — the normal way the last chunk of a pipe
// arrives, and the case a naive loop silently truncates.
//
// A real Read error is reported rather than swallowed: executing a truncated
// statement against the database is worse than refusing to run it.
func readSQL(r io.Reader) (string, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return "", fmt.Errorf("failed to read stdin: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// ReplayResult is one line of `dbx replay -j`.
type ReplayResult struct {
	SQL      string `json:"sql"`
	Duration int64  `json:"duration_ms"`
	Rows     int    `json:"rows"`
	Error    string `json:"error,omitempty"`
}

// replayEntries re-runs every query in a session log.
//
// A failing statement is recorded and the replay continues: the point of a replay
// is to see how far a session gets, and stopping at the first error would answer
// a different question. The error count in the summary is what tells the user
// where it stopped mattering.
func replayEntries(stdout, stderr io.Writer, conn postgres.Conn, entries []session.LogEntry, filename string, asJSON bool) error {
	loader := postgres.NewSchemaLoader(conn)

	// Initialised, not declared: with nothing to replay a nil slice marshals to
	// JSON `null`, and the empty case is an ordinary one.
	results := []ReplayResult{}
	queryCount := 0
	errorCount := 0

	for _, entry := range entries {
		if entry.Level != session.LogQuery || entry.SQL == "" {
			continue
		}

		queryCount++
		start := time.Now()
		result, err := loader.ExecuteRaw(context.Background(), entry.SQL)
		duration := time.Since(start).Milliseconds()

		r := ReplayResult{SQL: entry.SQL, Duration: duration}
		if err != nil {
			r.Error = err.Error()
			errorCount++
			_, _ = fmt.Fprintf(stderr, "FAIL: %s\n  Error: %v\n", entry.SQL, err)
		} else {
			r.Rows = result.Count
			_, _ = fmt.Fprintf(stderr, "OK: %s (%d rows, %dms)\n", entry.SQL, result.Count, duration)
		}
		results = append(results, r)
	}

	if asJSON {
		return writeJSON(stdout, map[string]interface{}{
			"session": filename,
			"total":   queryCount,
			"errors":  errorCount,
			"queries": results,
		})
	}

	_, _ = fmt.Fprintf(stderr, "\nReplay complete: %d queries, %d errors\n", queryCount, errorCount)
	return nil
}
