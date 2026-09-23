package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	aictx "github.com/buble/dbx/internal/ai/context"
	"github.com/jackc/pgx/v5"
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

	ctx := context.Background()

	export, err := aictx.ExportSchema(ctx, conn, getDatabaseName(conn))
	if err != nil {
		return fmt.Errorf("failed to export schema: %w", err)
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	outputFile, _ := cmd.Flags().GetString("output")
	if outputFile != "" {
		if err := os.WriteFile(outputFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Schema exported to %s\n", outputFile)
	} else {
		fmt.Println(string(data))
	}

	return nil
}

func getDatabaseName(conn *pgx.Conn) string {
	var name string
	err := conn.QueryRow(context.Background(), "SELECT current_database()").Scan(&name)
	if err != nil {
		return "unknown"
	}
	return name
}
