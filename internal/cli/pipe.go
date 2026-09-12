package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/config"
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
	defer conn.Close(context.Background())

	filename := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	sessionDir := cfg.Session.Dir

	reader := session.NewReader(sessionDir)
	entries, err := reader.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read session file: %w", err)
	}

	ctx := context.Background()
	loader := postgres.NewSchemaLoader(conn)

	type ReplayResult struct {
		SQL      string `json:"sql"`
		Duration int64  `json:"duration_ms"`
		Rows     int    `json:"rows"`
		Error    string `json:"error,omitempty"`
	}

	var results []ReplayResult
	queryCount := 0
	errorCount := 0

	for _, entry := range entries {
		if entry.Level != session.LogQuery || entry.SQL == "" {
			continue
		}

		queryCount++
		start := time.Now()

		result, err := loader.ExecuteRaw(ctx, entry.SQL)
		duration := time.Since(start).Milliseconds()

		r := ReplayResult{
			SQL:      entry.SQL,
			Duration: duration,
		}

		if err != nil {
			r.Error = err.Error()
			errorCount++
			fmt.Fprintf(os.Stderr, "FAIL: %s\n  Error: %v\n", entry.SQL, err)
		} else {
			r.Rows = result.Count
			fmt.Fprintf(os.Stderr, "OK: %s (%d rows, %dms)\n", entry.SQL, result.Count, duration)
		}

		results = append(results, r)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session":     filename,
			"total":       queryCount,
			"errors":      errorCount,
			"queries":     results,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	fmt.Fprintf(os.Stderr, "\nReplay complete: %d queries, %d errors\n", queryCount, errorCount)
	return nil
}

var pipeCmd = &cobra.Command{
	Use:   "pipe",
	Short: "Execute SQL from stdin",
	Long:  `Read SQL from stdin and execute it. Useful for piping.`,
	RunE:  runPipe,
}

func init() {
	pipeCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	pipeCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	rootCmd.AddCommand(pipeCmd)
}

func runPipe(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	data, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	if (data.Mode() & os.ModeCharDevice) != 0 {
		return fmt.Errorf("no input provided. Usage: echo \"SELECT 1\" | dbx pipe")
	}

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}

	sql := strings.TrimSpace(string(buf))
	if sql == "" {
		return fmt.Errorf("empty SQL input")
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	result, err := postgres.ExecuteQuery(ctx, conn, sql)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"columns": result.Columns,
			"rows":    result.Rows,
			"count":   result.Count,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	for _, col := range result.Columns {
		fmt.Printf("%-20s", col.Name)
	}
	fmt.Println()

	for _, row := range result.Rows {
		for _, val := range row {
			fmt.Printf("%-20v", val)
		}
		fmt.Println()
	}

	fmt.Printf("\n%d rows\n", result.Count)
	return nil
}
