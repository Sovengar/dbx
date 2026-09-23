package ui

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func testRegistry() *config.KeybindRegistry {
	return config.NewKeybindRegistry(config.KeybindingsConfig{})
}

func paneWith(focus string) *KeybindsPane {
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), testRegistry())
	p.SetWidth(200)
	p.SetFocus(focus)
	return p
}

func linesContain(lines []string, want string) bool {
	return strings.Contains(strings.Join(lines, "\n"), want)
}

// Scenario: La tecla que se muestra es la que ejecuta la acción.
func TestKeybindsPane_ExplorerShowsFilterTablesKey(t *testing.T) {
	p := paneWith(config.ContextExplorer)
	if !linesContain(p.renderLines(), "/ Filter Tables") {
		t.Fatalf("explorer pane does not show '/ Filter Tables': %v", p.renderLines())
	}
}

// Scenario: Customizar una tecla cambia display y ejecución a la vez.
func TestKeybindsPane_CustomKeyChangesDisplay(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"filter_tables": "F"}})
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), kb)
	p.SetWidth(200)
	p.SetFocus(config.ContextExplorer)

	lines := p.renderLines()
	if !linesContain(lines, "F Filter Tables") {
		t.Fatalf("custom key not reflected in pane: %v", lines)
	}
	if linesContain(lines, "/ Filter Tables") {
		t.Fatalf("default key still shown after override: %v", lines)
	}
}

// Scenario: El panel muestra solo las acciones de la vista actual (Explorer).
func TestKeybindsPane_ExplorerHidesOtherViewsAndGlobal(t *testing.T) {
	joined := strings.Join(paneWith(config.ContextExplorer).renderLines(), "\n")
	if strings.Contains(joined, "Edit Cell") {
		t.Fatalf("explorer pane leaked a grid-only action: %q", joined)
	}
	if strings.Contains(joined, "Copy SQL") {
		t.Fatalf("explorer pane leaked an editor-only action: %q", joined)
	}
	if strings.Contains(joined, "Global") {
		t.Fatalf("explorer pane still renders a 'Global' label: %q", joined)
	}
}

// Scenario: Cambiar de vista cambia el contenido del panel.
func TestKeybindsPane_ViewChangeChangesContent(t *testing.T) {
	explorer := strings.Join(paneWith(config.ContextExplorer).renderLines(), "\n")
	grid := strings.Join(paneWith(config.ContextGrid).renderLines(), "\n")

	if !strings.Contains(grid, "Edit Cell") {
		t.Fatalf("grid pane does not list its own actions: %q", grid)
	}
	if strings.Contains(grid, "Filter Tables") {
		t.Fatalf("grid pane still lists an explorer-only action: %q", grid)
	}
	if !strings.Contains(explorer, "Filter Tables") {
		t.Fatalf("explorer pane lost its own actions: %q", explorer)
	}
}

// Scenario: "q" es un carácter literal en el editor, no un atajo de salida.
func TestKeybindsPane_EditorDoesNotAnnounceQuit(t *testing.T) {
	p := paneWith(config.ContextEditor)
	p.SetEditorOpen(true)

	if linesContain(p.renderLines(), "Quit") {
		t.Fatalf("editor pane announces quit: %v", p.renderLines())
	}
}

// Scenario: Una acción deja de aplicar en una vista donde no tiene sentido.
func TestKeybindsPane_GridDoesNotAnnounceCopySQL(t *testing.T) {
	if linesContain(paneWith(config.ContextGrid).renderLines(), "Copy SQL") {
		t.Fatalf("grid pane shows the editor-only Copy SQL action")
	}
}

// Scenario: ASK keybind shown in the bottom bar.
func TestKeybindsPane_ShowsAskKeybind(t *testing.T) {
	if !linesContain(paneWith(config.ContextExplorer).renderLines(), "a Ask AI (NL→SQL)") {
		t.Fatalf("pane does not show 'a Ask AI (NL→SQL)': %v", paneWith(config.ContextExplorer).renderLines())
	}
}

// Pane reflects the pending transaction state.
func TestKeybindsPane_TxPendingShowsRollback(t *testing.T) {
	p := paneWith(config.ContextExplorer)
	p.SetTxPending(true)
	if !linesContain(p.renderLines(), "Rollback Last Transaction") {
		t.Fatalf("pane does not show rollback while a tx is pending: %v", p.renderLines())
	}
	if !linesContain(p.renderLines(), "tx pending") {
		t.Fatalf("pane does not show 'tx pending': %v", p.renderLines())
	}
}

// Scenario: The editor announces its Esc binding (close editor).
func TestKeybindsPane_EditorShowsCloseKey(t *testing.T) {
	p := paneWith(config.ContextEditor)
	p.SetEditorOpen(true)
	if !linesContain(p.renderLines(), "esc Close Editor") {
		t.Fatalf("editor pane does not show 'esc Close Editor': %v", p.renderLines())
	}
}

// Scenario: The pane packs at most 7 keybinds per line and wraps the rest.
func TestKeybindsPane_MaxSevenKeybindsPerLine(t *testing.T) {
	p := paneWith(config.ContextGrid)
	p.SetWidth(1000) // wide enough that only the count cap forces a wrap

	lines := p.renderLines()
	if len(lines) < 2 {
		t.Fatalf("expected the grid keybinds to span multiple lines, got %d: %v", len(lines), lines)
	}
	for i, line := range lines {
		if n := strings.Count(line, " · ") + 1; n > maxKeybindsPerLine {
			t.Fatalf("line %d has %d keybinds (> %d): %q", i, n, maxKeybindsPerLine, line)
		}
	}
	if n := strings.Count(lines[0], " · ") + 1; n != maxKeybindsPerLine {
		t.Fatalf("first line should be full (%d keybinds), got %d: %q", maxKeybindsPerLine, n, lines[0])
	}
}

func TestKeybindsPane_NoTxHidesRollback(t *testing.T) {
	p := paneWith(config.ContextExplorer)
	p.SetTxPending(false)
	if linesContain(p.renderLines(), "Rollback") {
		t.Fatalf("pane shows rollback without a pending transaction: %v", p.renderLines())
	}
}

// Scenario: Sibling actions of a group render as one segment with combined keys.
func TestKeybindsPane_GridRendersGroups(t *testing.T) {
	lines := paneWith(config.ContextGrid).renderLines()
	for _, want := range []string{
		"hjkl Navigate",
		"g/G First/Last",
		"ctrl+u/ctrl+d Half Page",
		"f1-f9 Go to Page",
	} {
		if !linesContain(lines, want) {
			t.Errorf("grid pane does not show group segment %q: %v", want, lines)
		}
	}
}

// Scenario: The pane shows only the primary key of an action.
func TestKeybindsPane_ShowsPrimaryKeysOnly(t *testing.T) {
	lines := paneWith(config.ContextGrid).renderLines()
	if !linesContain(lines, "n Next Page") {
		t.Fatalf("grid pane does not show 'n Next Page': %v", lines)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "ctrl+right") {
		t.Errorf("pane leaked the next_page alias 'ctrl+right': %q", joined)
	}
	if strings.Contains(joined, "n/]/ctrl+right") {
		t.Errorf("pane leaked the raw alias list for next_page: %q", joined)
	}
}

// Scenario: Conditionally hidden actions do not break grouping.
func TestKeybindsPane_TxPendingKeepsGroups(t *testing.T) {
	p := paneWith(config.ContextGrid)
	p.SetTxPending(true)
	lines := p.renderLines()
	if !linesContain(lines, "tx pending") {
		t.Fatalf("pending-tx segment missing: %v", lines)
	}
	if !linesContain(lines, "hjkl Navigate") {
		t.Fatalf("tx state broke the grouped segments: %v", lines)
	}
}

// Scenario: A partially active group shows only the members of the current context.
func TestKeybindsPane_ExplorerPartialGroup(t *testing.T) {
	lines := paneWith(config.ContextExplorer).renderLines()
	joined := strings.Join(lines, "\n")
	if !linesContain(lines, "j/k Navigate") {
		t.Fatalf("explorer pane does not show the partial nav group 'j/k Navigate': %v", lines)
	}
	if strings.Contains(joined, "Half Page") {
		t.Errorf("explorer pane rendered the half-page group: %q", joined)
	}
	if strings.Contains(joined, "Go to Page") {
		t.Errorf("explorer pane rendered the goto-page group: %q", joined)
	}
}
