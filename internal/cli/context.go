package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	aictx "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/drivers/postgres"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Export database schema as JSON for LLMs",
	Long:  `Export the complete database schema optimized for LLM consumption.`,
	RunE:  runContext,
}

func init() {
	contextCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	contextCmd.Flags().BoolP("json", "j", true, "Output as JSON (default)")
	contextCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	rootCmd.AddCommand(contextCmd)
}

func runContext(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	export, err := aictx.ExportSchema(context.Background(), conn, getDatabaseName(conn))
	if err != nil {
		return fmt.Errorf("failed to export schema: %w", err)
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	outputFile, _ := cmd.Flags().GetString("output")
	return writeSchemaOutput(data, outputFile, os.Stdout, os.Stderr)
}

// writeSchemaOutput sends the exported JSON to a file or to a stream, and is the
// whole of what `-o` decides.
//
// It takes its streams as arguments rather than reaching for os.Stdout, so a test
// can assert on what a user would actually see. The file branch is the only
// genuine I/O left in `dbx context`, and the error it can fail with — an
// unwritable path — is reachable from a test with a path under a directory that
// does not exist.
func writeSchemaOutput(data []byte, outputFile string, stdout, stderr io.Writer) error {
	if outputFile == "" {
		_, err := fmt.Fprintln(stdout, string(data))
		return err
	}
	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	// The export itself already succeeded and the file is written, so a failure to
	// print the confirmation is not a failure of the command.
	_, _ = fmt.Fprintf(stderr, "Schema exported to %s\n", outputFile)
	return nil
}

func getDatabaseName(conn postgres.Conn) string {
	var name string
	if err := conn.QueryRow(context.Background(), "SELECT current_database()").Scan(&name); err != nil {
		return "unknown"
	}
	return name
}
