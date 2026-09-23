package grid

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// Scenario: typing a printable key while editing appends it to the cell editor
// (the ASK global key must not steal it).
func TestGrid_EditKeyTypesIntoCell(t *testing.T) {
	kbs := config.NewKeybindRegistry(config.KeybindingsConfig{})
	g := New(theme.Resolve("dark").Styles(), 100, kbs)
	g.SetWidth(100)
	g.SetHeight(30)
	g.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "n"}},
		Rows:    [][]interface{}{{"1"}},
		Count:   1,
	}, "public", "users")
	g.Focus()

	if _, handled := g.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !handled || !g.IsEditing() {
		t.Fatalf("grid did not enter edit mode (handled=%v editing=%v)", handled, g.IsEditing())
	}

	g.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	if !strings.Contains(g.editValue, "a") {
		t.Fatalf("edit value = %q, want it to contain 'a'", g.editValue)
	}
}
