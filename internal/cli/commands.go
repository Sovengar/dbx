package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/jackc/pgx/v5"
)

var queryCmd = &cobra.Command{
	Use:   "query [sql]",
	Short: "Execute a SQL query",
	Long:  `Execute a SQL query and output results as JSON.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runQuery,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List database objects",
	Long:  `List tables, columns, or schemas from the database.`,
}

var listTablesCmd = &cobra.Command{
	Use:   "tables",
	Short: "List all tables",
	RunE:  runListTables,
}

var listColumnsCmd = &cobra.Command{
	Use:   "columns [table]",
	Short: "List columns for a table",
	Args:  cobra.ExactArgs(1),
	RunE:  runListColumns,
}

var schemaCmd = &cobra.Command{
	Use:   "schema [table]",
	Short: "Show table schema",
	Args:  cobra.ExactArgs(1),
	RunE:  runSchema,
}

func init() {
	queryCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	queryCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	listCmd.AddCommand(listTablesCmd)
	listCmd.AddCommand(listColumnsCmd)
	listCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	listCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	listTablesCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	listTablesCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	listColumnsCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	listColumnsCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	schemaCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	schemaCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(schemaCmd)
}

func getConnection(cmd *cobra.Command) (*pgx.Conn, error) {
	connName, _ := cmd.Flags().GetString("connection")

	// First, try to find .dbx.toml in current directory
	if dsn := findLocalDSN(); dsn != "" {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to connect: %w", err)
		}
		if err := conn.Ping(ctx); err != nil {
			_ = conn.Close(ctx)
			return nil, fmt.Errorf("failed to ping: %w", err)
		}
		return conn, nil
	}

	// Fall back to global config
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	var connCfg *config.ConnectionConfig
	for _, c := range cfg.Connections {
		if connName == "" || c.Name == connName {
			connCfg = &c
			break
		}
	}

	if connCfg == nil {
		return nil, fmt.Errorf("connection not found: %s (no .dbx.toml in current directory)", connName)
	}

	var dsn string
	if connCfg.URL != "" {
		dsn = connCfg.URL
	} else {
		dsn = fmt.Sprintf("postgres://%s@%s:%d/%s",
			connCfg.User,
			connCfg.Host,
			connCfg.Port,
			connCfg.Database,
		)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	return conn, nil
}

func findLocalDSN() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	cfg, err := config.LoadProjectConfig(wd + "/.dbx.toml")
	if err != nil {
		return ""
	}

	for _, conn := range cfg.Connections {
		return conn.GetDSN()
	}

	return ""
}

func runQuery(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	sql := args[0]
	ctx := context.Background()

	result, err := postgres.ExecuteQuery(ctx, conn, sql)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
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

	// Simple text output
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

func runListTables(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	ctx := context.Background()
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schemas: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	var allTables []map[string]interface{}

	for _, schema := range schemas {
		tables, err := loader.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		for _, table := range tables {
			if jsonOutput {
				allTables = append(allTables, map[string]interface{}{
					"schema":    schema,
					"name":      table.Name,
					"type":      table.Type,
					"row_count": table.RowCount,
				})
			} else {
				fmt.Printf("%s.%s (%s, %d rows)\n", schema, table.Name, table.Type, table.RowCount)
			}
		}
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(allTables)
	}

	return nil
}

func runListColumns(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	table := args[0]
	ctx := context.Background()
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schemas: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	var found bool

	for _, schema := range schemas {
		columns, err := loader.ListColumns(ctx, schema, table)
		if err != nil {
			continue
		}

		if len(columns) > 0 {
			found = true
			if jsonOutput {
				output := map[string]interface{}{
					"schema":  schema,
					"table":   table,
					"columns": columns,
				}
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(output)
			}

			fmt.Printf("Table: %s.%s\n", schema, table)
			for _, col := range columns {
				Nullable := ""
				if col.IsNullable == "YES" {
					Nullable = " NULL"
				}
				Default := ""
				if col.Default != nil {
					Default = fmt.Sprintf(" DEFAULT %s", *col.Default)
				}
				fmt.Printf("  %-30s %s%s%s\n", col.Name, col.DataType, Nullable, Default)
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("table not found: %s", table)
	}

	return nil
}

func runSchema(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	table := args[0]
	ctx := context.Background()
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schemas: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	var found bool

	for _, schema := range schemas {
		columns, err := loader.ListColumns(ctx, schema, table)
		if err != nil {
			continue
		}

		if len(columns) > 0 {
			found = true
			if jsonOutput {
				output := map[string]interface{}{
					"schema":  schema,
					"table":   table,
					"columns": columns,
				}
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(output)
			}

			fmt.Printf("Table: %s.%s\n\n", schema, table)
			fmt.Println("Columns:")
			for _, col := range columns {
				Nullable := ""
				if col.IsNullable == "YES" {
					Nullable = " NULL"
				}
				Default := ""
				if col.Default != nil {
					Default = fmt.Sprintf(" DEFAULT %s", *col.Default)
				}
				fmt.Printf("  %-30s %s%s%s\n", col.Name, col.DataType, Nullable, Default)
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("table not found: %s", table)
	}

	return nil
}
