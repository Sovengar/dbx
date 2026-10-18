package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/ai/nl2sql"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
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
	askCmd.Flags().BoolP("sql-only", "s", false, "Only output the generated SQL, without the database")
	askCmd.Flags().Bool("dry-run", false, "Generate with the real schema and print the SQL, without executing it")
	askCmd.Flags().Bool("confirm", false, "Ask for confirmation before executing the generated SQL")
	rootCmd.AddCommand(askCmd)
}

// askOptions is what the flags above decided, passed down as one value so runAsk
// stays a thin reader of cobra and askAndReport stays a plain function a test can
// call with any of the three review modes.
type askOptions struct {
	dryRun  bool
	confirm bool
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	sqlOnly, _ := cmd.Flags().GetBool("sql-only")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	confirm, _ := cmd.Flags().GetBool("confirm")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	provider, err := nl2sql.Resolve(nl2sql.Config{
		Provider:  cfg.AI.Provider,
		Model:     cfg.AI.Model,
		Providers: cfg.AI.Nl2sqlProviders(),
	})
	if err != nil {
		return fmt.Errorf("failed to resolve AI provider: %w", err)
	}

	// In sql-only mode there is no database involved at all: the point is to see
	// the SQL without running it, and connecting would make the flag fail for
	// anyone without a reachable database.
	if sqlOnly {
		return generateSQLOnly(os.Stdout, provider, question)
	}

	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	return askAndReport(os.Stdout, os.Stderr, conn, provider, question, jsonOutput, cfg, os.Stdin, askOptions{
		dryRun:  dryRun,
		confirm: confirm,
	})
}

// generateSQLOnly prints the SQL and stops there.
//
// It takes an empty schema, and that is the honest thing to pass: the database was
// never consulted in this mode, so a schema would be a fabrication. The generated
// SQL is a best guess from the question alone, which is what `--sql-only` is for.
func generateSQLOnly(w io.Writer, provider nl2sql.Provider, question string) error {
	sql, err := provider.Generate(context.Background(), question, "")
	if err != nil {
		return fmt.Errorf("failed to generate SQL: %w", err)
	}
	_, err = fmt.Fprintln(w, sql)
	return err
}

// askAndReport is `dbx ask` with the connection and the provider already in hand:
// generate the SQL from the real schema and then either stop at the review step or
// execute and report.
//
// The two modes are separate functions rather than one with a flag, because they
// use the database differently: this one reads the schema first and can therefore
// fail before the model is ever called.
//
// cfg carries the session-log settings for the execution this function may run.
// A nil config is the "log nothing" case; the helpers are no-ops without one.
//
// stdin is passed in rather than read from os.Stdin so the confirmation prompt is a
// plain function of its inputs; stdout carries the result and stderr everything
// else, which is what keeps `--json` parseable while a prompt is on screen.
func askAndReport(stdout, stderr io.Writer, conn postgres.Conn, provider nl2sql.Provider, question string, asJSON bool, cfg *config.Config, stdin io.Reader, opts askOptions) error {
	ctx := context.Background()

	schemaStr, err := getSchemaForLLM(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	// Progress goes to stderr and is best-effort: a failure to print "generating"
	// is not a reason to abandon a query the model has already produced.
	_, _ = fmt.Fprintf(stderr, "Generating SQL with %s...\n", provider.Name())

	sql, err := provider.Generate(ctx, question, schemaStr)
	if err != nil {
		return fmt.Errorf("failed to generate SQL: %w", err)
	}

	// The review step. `--dry-run` prints the schema-aware SQL and stops, which is
	// the non-interactive counterpart of the TUI's ASK pane: the SQL goes to
	// stdout because in this mode it is the result, not progress.
	if opts.dryRun {
		if asJSON {
			return writeJSON(stdout, map[string]interface{}{"sql": sql})
		}
		_, err := fmt.Fprintln(stdout, sql)
		return err
	}

	_, _ = fmt.Fprintf(stderr, "SQL: %s\n\n", sql)

	// A declined prompt is a successful run that executed nothing, not an error: the
	// user asked to review the SQL and the review said no. The exit stays 0 so a
	// cancelled review is not mistaken for a failure by the shell.
	if opts.confirm && !confirmExecution(stdin, stderr) {
		return nil
	}

	start := time.Now()
	result, err := postgres.ExecuteQuery(ctx, conn, sql)
	if err != nil {
		logSessionError(cfg, sql, err)
		return fmt.Errorf("query failed: %w", err)
	}
	logSessionQuery(cfg, sql, time.Since(start), result.Count)

	if asJSON {
		return writeJSON(stdout, map[string]interface{}{
			"sql":     sql,
			"columns": result.Columns,
			"rows":    result.Rows,
			"count":   result.Count,
		})
	}

	return writeQueryResult(stdout, result, false)
}

// confirmExecution asks for approval of the generated SQL and reports whether it
// was given.
//
// The prompt goes to stderr so stdout stays the result stream, and the answer is
// read from stdin. Anything that is not an explicit yes is a no — including an
// empty line and a read error — because the whole point of the review is that
// silence does not execute. That default is why the prompt reads `[y/N]`.
func confirmExecution(stdin io.Reader, stderr io.Writer) bool {
	_, _ = fmt.Fprint(stderr, "Execute this query? [y/N] ")

	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil {
		_, _ = fmt.Fprintln(stderr, "Cancelled.")
		return false
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}

	_, _ = fmt.Fprintln(stderr, "Cancelled.")
	return false
}

func getSchemaForLLM(ctx context.Context, conn postgres.Conn) (string, error) {
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return "", err
	}

	var details []postgres.SchemaDetail
	for _, schema := range schemas {
		if schema == "pg_catalog" || schema == "information_schema" || schema == "pg_toast" {
			continue
		}

		tables, err := loader.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		sd := postgres.SchemaDetail{Name: schema}
		for _, table := range tables {
			columns, err := loader.ListColumns(ctx, schema, table.Name)
			if err != nil {
				continue
			}
			sd.Tables = append(sd.Tables, postgres.TableDetail{
				Name:     table.Name,
				RowCount: table.RowCount,
				Columns:  columns,
			})
		}
		details = append(details, sd)
	}

	return postgres.SchemaText(details), nil
}
