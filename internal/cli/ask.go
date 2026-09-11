package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/ai/nl2sql"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/jackc/pgx/v5"
)

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Ask a question in natural language",
	Long:  `Convert a natural language question to SQL and execute it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runAsk,
}

func init() {
	askCmd.Flags().StringP("connection", "c", "", "Connection name from config")
	askCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	askCmd.Flags().BoolP("sql-only", "s", false, "Only output the generated SQL")
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	sqlOnly, _ := cmd.Flags().GetBool("sql-only")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	provider, err := nl2sql.Resolve(nl2sql.Config{
		Provider:  cfg.AI.Provider,
		Model:     cfg.AI.Model,
		Providers: toProviderConfigs(cfg.AI.Providers),
	})
	if err != nil {
		return fmt.Errorf("failed to resolve AI provider: %w", err)
	}

	// For sql-only mode, we don't need a database connection
	if sqlOnly {
		ctx := context.Background()
		sql, err := provider.Generate(ctx, question, "")
		if err != nil {
			return fmt.Errorf("failed to generate SQL: %w", err)
		}
		fmt.Println(sql)
		return nil
	}

	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	ctx := context.Background()
	schemaStr, err := getSchemaForLLM(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Generating SQL with %s...\n", provider.Name())

	sql, err := provider.Generate(ctx, question, schemaStr)
	if err != nil {
		return fmt.Errorf("failed to generate SQL: %w", err)
	}

	if sqlOnly {
		fmt.Println(sql)
		return nil
	}

	fmt.Fprintf(os.Stderr, "SQL: %s\n\n", sql)

	result, err := postgres.ExecuteQuery(ctx, conn, sql)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"sql":    sql,
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

func getSchemaForLLM(ctx context.Context, conn *pgx.Conn) (string, error) {
	loader := postgres.NewSchemaLoader(conn)

	dbName := "database"
	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return "", err
	}

	var result string
	for _, schema := range schemas {
		if schema == "pg_catalog" || schema == "information_schema" || schema == "pg_toast" {
			continue
		}

		tables, err := loader.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		result += fmt.Sprintf("Schema: %s\n", schema)
		for _, table := range tables {
			columns, err := loader.ListColumns(ctx, schema, table.Name)
			if err != nil {
				continue
			}

			result += fmt.Sprintf("  Table: %s (%d rows)\n", table.Name, table.RowCount)
			for _, col := range columns {
				nullable := ""
				if col.IsNullable == "YES" {
					nullable = " NULL"
				}
				result += fmt.Sprintf("    %s %s%s\n", col.Name, col.DataType, nullable)
			}
		}
		result += "\n"
	}

	_ = dbName
	return result, nil
}

func toProviderConfigs(m map[string]config.AIProviderConf) map[string]nl2sql.ProviderConfig {
	result := make(map[string]nl2sql.ProviderConfig, len(m))
	for k, v := range m {
		result[k] = nl2sql.ProviderConfig{
			APIKeyEnv: v.APIKeyEnv,
			Model:     v.Model,
		}
	}
	return result
}
