package cli

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/app"
	"github.com/buble/dbx/internal/config"
)

var rootCmd = &cobra.Command{
	Use:   "dbx [connection]",
	Short: "Database x — Modern TUI database client with AI integration",
	Long:  `A terminal UI for databases with native AI integration.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTUI,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	model := app.NewModel(cfg)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}
