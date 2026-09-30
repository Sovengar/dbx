package cli

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/app"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

var rootCmd = &cobra.Command{
	Use:   "dbx [connection]",
	Short: "Database x — Modern TUI database client with AI integration",
	Long:  `A terminal UI for databases with native AI integration.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTUI,
}

// These three are the only things between the CLI and the outside world that a
// test cannot supply itself: the user's config file, the TUI model, and the
// terminal. They are variables so a test can replace one without the others, and
// so `runTUI` stays a plain function that can be called and asserted on.
//
// The production values are the obvious ones; nothing here changes behaviour.
var (
	loadConfig = config.Load
	buildModel = func(cfg *config.Config) tea.Model { return app.NewModel(cfg) }
	runProgram = func(m tea.Model) (tea.Model, error) { return tea.NewProgram(m).Run() }
	pgxConnect = func(ctx context.Context, dsn string) (pgconnPing, error) {
		return pgx.Connect(ctx, dsn)
	}
)

// pgconnPing is what getConnection needs from a live connection: the three
// postgres.Conn methods plus Ping, which only the real connection has. It is an
// interface so the connect path can be tested without a server.
type pgconnPing interface {
	postgres.Conn
	Ping(ctx context.Context) error
}

// exit is os.Exit behind a variable, for the same reason loadConfig is one: the
// last statement of the program is otherwise untestable, and "prints the error and
// exits 1" is exactly the behaviour worth pinning. A test replaces it and records
// the code, so this function's whole contract becomes observable.
var exit = os.Exit

func Execute() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exit(1)
	}
}

// execute is Execute without the process exit. Separating them is what makes the
// error path testable: `os.Exit` cannot be observed from a test, so the code
// that decides to exit has to sit behind a function that returns instead.
func execute() error {
	return rootCmd.Execute()
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, err := runProgram(buildModel(cfg)); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}
