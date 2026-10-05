package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
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

func getConnection(cmd *cobra.Command) (postgres.Conn, error) {
	connName, _ := cmd.Flags().GetString("connection")

	// First, try to find .dbx.toml in current directory.
	//
	// The flag is passed through, and it used not to be: `--connection analytics` from
	// inside a project with a .dbx.toml connected to whatever entry the map happened to
	// yield, which is a different database from the one the user named. The flag was
	// honoured only in the global-config branch below, so the same command behaved
	// differently depending on whether a project file happened to exist.
	if dsn := findLocalDSN(connName); dsn != "" {
		ctx := context.Background()
		conn, err := pgxConnect(ctx, dsn)
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
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	dsn, err := dsnFor(connName, cfg)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	conn, err := pgxConnect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	return conn, nil
}

// dsnFor picks the DSN for a named connection, or for the first one when no name
// was given.
//
// It is separated from getConnection because it is the part with a real decision
// in it — which config entry, and how a URL-less entry is turned into a URL — and
// that decision is worth testing without a database or a config file on disk.
//
// passwordEnv returns the password named by a connection's `password_env`, or "" when
// there is none. It exists because that knob was declared, given no default and read by
// nothing at all: the documented way to keep a password out of config.toml did nothing,
// and a user who set it and dropped the password from the URL simply failed to connect
// with nothing saying the knob was unsupported. The sibling mechanism already worked —
// project .dbx.toml files expand `${env:NAME}` in their DSN — so this is the global
// config learning the convention the project files already had.
func passwordEnv(conn *config.ConnectionConfig) string {
	if conn.PasswordEnv == "" {
		return ""
	}
	return os.Getenv(conn.PasswordEnv)
}

// dsnWithPassword puts a password into a DSN that has no password in it.
//
// A DSN that ALREADY carries a password is left alone: the file is the more explicit
// statement, and overwriting it would make `password_env` silently discard what the user
// wrote. Every form is handled — url form and keyword form, with or without a port —
// because the DSN in the file is whichever of them the user happened to write, and
// "does this DSN have a password" has more than one answer if you only check for '@'.
func dsnWithPassword(dsn, password string) string {
	if password == "" || dsn == "" {
		return dsn
	}
	at := strings.LastIndex(dsn, "@")
	if at < 0 {
		// No credentials section at all: postgres://host/db. Splice one in
		// before the host, which is where a userinfo goes.
		if i := strings.Index(dsn, "://"); i >= 0 {
			return dsn[:i+3] + ":" + password + "@" + dsn[i+3:]
		}
		return dsn
	}

	creds := dsn[:at]
	scheme := ""
	if i := strings.Index(creds, "://"); i >= 0 {
		scheme = creds[:i+3]
		creds = creds[i+3:]
	}
	if strings.Contains(creds, ":") {
		// Already has user:password.
		return dsn
	}
	return scheme + creds + ":" + password + dsn[at:]
}

// dsnFor picks the DSN for a named connection, or for the first one when no name
// was given.
//
// It is separated from getConnection because it is the part with a real decision
// in it — which config entry, and how a URL-less entry is turned into a URL — and
// that decision is worth testing without a database or a config file on disk.
func dsnFor(connName string, cfg *config.Config) (string, error) {
	var connCfg *config.ConnectionConfig
	for _, c := range cfg.Connections {
		if connName == "" || c.Name == connName {
			connCfg = &c
			break
		}
	}

	if connCfg == nil {
		return "", fmt.Errorf("connection not found: %s (no .dbx.toml in current directory)", connName)
	}

	password := passwordEnv(connCfg)

	if connCfg.URL != "" {
		return dsnWithPassword(connCfg.URL, password), nil
	}
	dsn := fmt.Sprintf("postgres://%s@%s:%d/%s",
		connCfg.User,
		connCfg.Host,
		connCfg.Port,
		connCfg.Database,
	)
	return dsnWithPassword(dsn, password), nil
}

// findLocalDSN returns the DSN of the named connection in the .dbx.toml of the working
// directory, or of a default one when name is empty.
//
// The default is the alphabetically FIRST connection name, not "whichever one the map
// yields". ProjectConfig.Connections is a map[string]ProjectConnection, and `for _, c :=
// range m { return c }` returns an arbitrary element: Go randomises map iteration, so a
// project with two connections connected to a DIFFERENT DATABASE ON EVERY INVOCATION.
// The first version of the test below asserted "the first of several" by writing them in
// an order, and the assertion passed or failed depending on the run.
//
// Sorting also makes `--connection` unnecessary for the common case: the alphabetically
// first name is at least stable enough to document.
func findLocalDSN(name string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	cfg, err := config.LoadProjectConfig(wd + "/.dbx.toml")
	if err != nil {
		return ""
	}
	if len(cfg.Connections) == 0 {
		return ""
	}

	if name != "" {
		conn, ok := cfg.Connections[name]
		if !ok {
			return ""
		}
		return conn.GetDSN()
	}

	names := make([]string, 0, len(cfg.Connections))
	for n := range cfg.Connections {
		names = append(names, n)
	}
	sort.Strings(names)

	conn := cfg.Connections[names[0]]
	return conn.GetDSN()
}

func runQuery(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return queryAndReport(os.Stdout, conn, args[0], jsonOutput)
}

// queryAndReport runs one statement and prints the result. Everything `dbx query`
// does is in here, with the connection already in hand.
func queryAndReport(w io.Writer, conn postgres.Conn, sql string, asJSON bool) error {
	result, err := postgres.ExecuteQuery(context.Background(), conn, sql)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	return writeQueryResult(w, result, asJSON)
}

// writeQueryResult prints a result set in one of two shapes, and is the whole of
// what `--json` decides.
//
// The text shape is a column-per-20-cells header, one line per row, then a count;
// the JSON shape is a single object with the columns, the rows and the count. It
// takes an io.Writer so a test can assert on the exact bytes a user would see,
// and because `query`, `pipe` and `ask` all share it, a change to one is a change
// to all three.
func writeQueryResult(w io.Writer, result *postgres.QueryResult, asJSON bool) error {
	if asJSON {
		return writeJSON(w, map[string]interface{}{
			"columns": result.Columns,
			"rows":    result.Rows,
			"count":   result.Count,
		})
	}

	for _, col := range result.Columns {
		if _, err := fmt.Fprintf(w, "%-20s", col.Name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	for _, row := range result.Rows {
		for _, val := range row {
			if _, err := fmt.Fprintf(w, "%-20v", val); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(w, "\n%d rows\n", result.Count)
	return err
}

// writeJSON is the one place the CLI produces JSON, so the indentation is
// decided once: two spaces, which diffs cleanly and is what the docs show.
func writeJSON(w io.Writer, v interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func runListTables(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return listTablesAndReport(os.Stdout, conn, jsonOutput)
}

// listTablesAndReport lists every table in every schema the user can see.
//
// A schema whose tables cannot be listed is skipped rather than fatal: the usual
// cause is permissions on one schema in a database with many, and failing the
// whole command over it would make `dbx list tables` useless on a shared server.
func listTablesAndReport(w io.Writer, conn postgres.Conn, asJSON bool) error {
	ctx := context.Background()
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schemas: %w", err)
	}

	// Initialised, not declared: with no tables a nil slice marshals to JSON
	// `null`, and `dbx list tables -j | jq '.[]'` then fails on a database that
	// is merely empty rather than on anything actually wrong.
	allTables := []map[string]interface{}{}

	for _, schema := range schemas {
		tables, err := loader.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		for _, table := range tables {
			if asJSON {
				allTables = append(allTables, map[string]interface{}{
					"schema":    schema,
					"name":      table.Name,
					"type":      table.Type,
					"row_count": table.RowCount,
				})
				continue
			}
			if _, err := fmt.Fprintf(w, "%s.%s (%s, %d rows)\n", schema, table.Name, table.Type, table.RowCount); err != nil {
				return err
			}
		}
	}

	if asJSON {
		return writeJSON(w, allTables)
	}
	return nil
}

func runListColumns(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return listColumnsAndReport(os.Stdout, conn, args[0], jsonOutput, false)
}

// listColumnsAndReport describes one table, and `heading` is what separates the
// two commands that share it: `dbx list columns` opens with "Table: s.t", while
// `dbx schema` opens with "Table: s.t" and a "Columns:" line. The schemas and the
// table argument are the rest of the difference.
func listColumnsAndReport(w io.Writer, conn postgres.Conn, table string, asJSON, schemaLike bool) error {
	columns, schema, err := findColumns(context.Background(), conn, table)
	if err != nil {
		return err
	}

	if asJSON {
		return writeJSON(w, map[string]interface{}{
			"schema":  schema,
			"table":   table,
			"columns": columns,
		})
	}

	if schemaLike {
		if _, err := fmt.Fprintf(w, "Table: %s.%s\n\n", schema, table); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "Columns:"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "Table: %s.%s\n", schema, table); err != nil {
			return err
		}
	}
	return writeColumns(w, columns)
}

func runSchema(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	jsonOutput, _ := cmd.Flags().GetBool("json")
	return listColumnsAndReport(os.Stdout, conn, args[0], jsonOutput, true)
}

// findColumns looks for a table across the schemas and returns the first one
// that has columns.
//
// Searching rather than requiring a schema is what makes `dbx schema users` work
// without the user having to know whether the table is in public or not. The
// first schema with a match wins, which means a same-named table in an earlier
// schema shadows a later one; that is the same order the server lists schemas in,
// so the answer is stable.
func findColumns(ctx context.Context, conn postgres.Conn, table string) ([]postgres.ColumnInfo, string, error) {
	loader := postgres.NewSchemaLoader(conn)

	schemas, err := loader.ListSchemas(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list schemas: %w", err)
	}

	for _, schema := range schemas {
		columns, err := loader.ListColumns(ctx, schema, table)
		if err != nil {
			continue
		}
		if len(columns) > 0 {
			return columns, schema, nil
		}
	}

	return nil, "", fmt.Errorf("table not found: %s", table)
}

// writeColumns prints the column block both `dbx schema` and `dbx list columns`
// end with: name, type, and the two annotations that change how a value is
// inserted by hand.
func writeColumns(w io.Writer, columns []postgres.ColumnInfo) error {
	for _, col := range columns {
		nullable := ""
		if col.IsNullable == "YES" {
			nullable = " NULL"
		}
		def := ""
		if col.Default != nil {
			def = fmt.Sprintf(" DEFAULT %s", *col.Default)
		}
		if _, err := fmt.Fprintf(w, "  %-30s %s%s%s\n", col.Name, col.DataType, nullable, def); err != nil {
			return err
		}
	}
	return nil
}
