package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// Scenario: La documentación de keybinds deja de duplicar la información.
func TestDocs_KeybindsNotDuplicated(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "docs", "KEYBINDS.md")); err == nil {
		t.Fatal("docs/KEYBINDS.md still exists")
	}

	readme := repoFile(t, "README.md")
	if strings.Contains(readme, "docs/KEYBINDS.md") {
		t.Fatal("README still links to docs/KEYBINDS.md")
	}
	if strings.Contains(readme, "| Key ") || strings.Contains(readme, "| Key|") {
		t.Fatal("README still contains a keybind table")
	}
	if !strings.Contains(readme, "`?`") {
		t.Fatal("README does not point users at the `?` help overlay")
	}
}

// Scenario: La prosa que no es de keybinds se conserva en un doc de features.
func TestDocs_FeaturesCarriesNonKeybindProse(t *testing.T) {
	features := repoFile(t, "docs/FEATURES.md")
	for _, want := range []string{"DML Transactions", "Action Naming Convention"} {
		if !strings.Contains(features, want) {
			t.Errorf("docs/FEATURES.md is missing %q", want)
		}
	}
}

// Scenario: El checklist de contribución refleja el nuevo flujo.
func TestDocs_AgentsChecklistPointsToRegistry(t *testing.T) {
	agents := repoFile(t, "AGENTS.md")
	if !strings.Contains(agents, "keybindings_actions.go") {
		t.Fatal("AGENTS.md keybind checklist does not point at the registry")
	}
	if strings.Contains(agents, "docs/KEYBINDS.md") {
		t.Fatal("AGENTS.md still lists docs/KEYBINDS.md as a place to touch")
	}
}
