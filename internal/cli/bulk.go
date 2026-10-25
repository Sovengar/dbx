package cli

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/drivers/postgres"
)

var exportCmd = &cobra.Command{
	Use:   "export [table]",
	Short: "Stream a table as CSV with PostgreSQL COPY",
	Args:  cobra.ExactArgs(1),
	RunE:  runBulkExport,
}

var importCmd = &cobra.Command{
	Use:   "import [table] [file]",
	Short: "Stream CSV into a table with PostgreSQL COPY",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runBulkImport,
}

func init() {
	for _, cmd := range []*cobra.Command{exportCmd, importCmd} {
		cmd.Flags().StringP("connection", "c", "", "Connection name from config")
		cmd.Flags().BoolP("json", "j", false, "Output the completion summary as JSON")
		cmd.Flags().String("columns", "", "Comma-separated target columns in COPY order")
	}
	exportCmd.Flags().StringP("output", "o", "", "Output file (default stdout; .gz compresses)")
	rootCmd.AddCommand(exportCmd, importCmd)
}

func runBulkExport(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	output, _ := cmd.Flags().GetString("output")
	columns, err := copyColumns(cmd)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	return exportTableAndReport(os.Stdout, conn, args[0], output, columns, asJSON)
}

func runBulkImport(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	input := "-"
	if len(args) == 2 {
		input = args[1]
	}
	columns, err := copyColumns(cmd)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	return importTableAndReport(os.Stdout, os.Stdin, conn, args[0], input, columns, asJSON)
}

func copyColumns(cmd *cobra.Command) ([]string, error) {
	raw, _ := cmd.Flags().GetString("columns")
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, fmt.Errorf("--columns contains an empty column name")
		}
	}
	return parts, nil
}

func parseCopyTable(value string) (string, string, error) {
	parts := strings.Split(value, ".")
	if len(parts) == 1 && parts[0] != "" {
		return "public", parts[0], nil
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("table must be written as table or schema.table")
}

func exportTableAndReport(stdout io.Writer, conn postgres.Conn, tableRef, output string, columns []string, asJSON bool) error {
	schema, table, err := parseCopyTable(tableRef)
	if err != nil {
		return err
	}
	if (output == "" || output == "-") && asJSON {
		return fmt.Errorf("--json requires --output because stdout carries the CSV stream")
	}

	var rows int64
	if output == "" || output == "-" {
		rows, err = postgres.CopyTableTo(context.Background(), conn, stdout, schema, table, columns)
	} else {
		rows, err = exportTableToFile(conn, schema, table, output, columns)
	}
	if err != nil {
		return err
	}
	if output == "" || output == "-" {
		return nil
	}

	if asJSON {
		return writeJSON(stdout, map[string]any{"table": schema + "." + table, "file": output, "rows": rows})
	}
	_, err = fmt.Fprintf(stdout, "Exported %d rows from %s.%s to %s\n", rows, schema, table, output)
	return err
}

func exportTableToFile(conn postgres.Conn, schema, table, output string, columns []string) (rows int64, err error) {
	dir := filepath.Dir(output)
	file, err := os.CreateTemp(dir, ".dbx-copy-*")
	if err != nil {
		return 0, fmt.Errorf("failed to create export file: %w", err)
	}
	tmp := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return 0, fmt.Errorf("failed to secure export file: %w", err)
	}

	var dst io.Writer = file
	var gz *gzip.Writer
	if strings.HasSuffix(strings.ToLower(output), ".gz") {
		gz = gzip.NewWriter(file)
		dst = gz
	}
	rows, err = postgres.CopyTableTo(context.Background(), conn, dst, schema, table, columns)
	if err != nil {
		return 0, err
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			return 0, fmt.Errorf("failed to finish gzip export: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return 0, fmt.Errorf("failed to finish export: %w", err)
	}
	if err := os.Rename(tmp, output); err != nil {
		return 0, fmt.Errorf("failed to install export file: %w", err)
	}
	keep = true
	return rows, nil
}

func importTableAndReport(stdout io.Writer, stdin io.Reader, conn postgres.Conn, tableRef, input string, columns []string, asJSON bool) error {
	schema, table, err := parseCopyTable(tableRef)
	if err != nil {
		return err
	}

	src := stdin
	var file *os.File
	if input != "" && input != "-" {
		file, err = os.Open(input)
		if err != nil {
			return fmt.Errorf("failed to open import file: %w", err)
		}
		defer func() { _ = file.Close() }()
		src = file
	}
	var gz *gzip.Reader
	if input != "-" && strings.HasSuffix(strings.ToLower(input), ".gz") {
		gz, err = gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("failed to open gzip import: %w", err)
		}
		defer func() { _ = gz.Close() }()
		src = gz
	}

	rows, err := postgres.CopyTableFrom(context.Background(), conn, src, schema, table, columns)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(stdout, map[string]any{"table": schema + "." + table, "file": input, "rows": rows})
	}
	_, err = fmt.Fprintf(stdout, "Imported %d rows into %s.%s\n", rows, schema, table)
	return err
}
