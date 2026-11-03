package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/ai/nl2sql"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List the models the configured AI provider offers",
	Long: `List the models of the configured AI provider (GET /models).

The provider, endpoint and key come from the same config resolution ` + "`dbx ask`" + ` uses.`,
	Args: cobra.NoArgs,
	RunE: runModels,
}

func init() {
	modelsCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	rootCmd.AddCommand(modelsCmd)
}

func runModels(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")

	models, err := nl2sql.Models(context.Background(), nl2sql.Config{
		Provider:  cfg.AI.Provider,
		Model:     cfg.AI.Model,
		Providers: cfg.AI.Nl2sqlProviders(),
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		return writeJSON(cmd.OutOrStdout(), map[string]any{"models": models})
	}
	for _, model := range models {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), model); err != nil {
			return err
		}
	}
	return nil
}
