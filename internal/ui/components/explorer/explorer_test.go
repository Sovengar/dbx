package explorer

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func newExplorerTest() *Explorer {
	styles := theme.Resolve("dark").Styles()
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{})
	e := New(styles, nil, kb)

	root := NewNode("root", NodeDatabase, "db")
	root.Expanded = true
	schema := NewNode("s", NodeSchema, "public")
	schema.Parent = root
	schema.Expanded = true
	root.Children = append(root.Children, schema)
	for i := 0; i < 5; i++ {
		tbl := NewNode(fmt.Sprintf("t%d", i), NodeTable, fmt.Sprintf("t%d", i))
		tbl.Parent = schema
		schema.Children = append(schema.Children, tbl)
	}
	e.SetNodes([]*Node{root})
	return e
}

// Scenario: Mouse wheel scrolls through resolved navigation actions, even when
// the explorer is not focused.
func TestExplorer_HandleAction_NavigateWorksUnfocused(t *testing.T) {
	e := newExplorerTest()
	e.focused = false

	if _, handled := e.HandleAction("navigate_down"); !handled {
		t.Fatal("HandleAction(navigate_down) was not handled")
	}
	if e.tree.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", e.tree.cursor)
	}

	e.HandleAction("navigate_up")
	if e.tree.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after navigate_up", e.tree.cursor)
	}
}

// Scenario: the wheel must not move the cursor while the filter input is active.
func TestExplorer_HandleAction_NoOpWhileFiltering(t *testing.T) {
	e := newExplorerTest()
	e.StartFilter()
	before := e.tree.cursor

	if _, handled := e.HandleAction("navigate_down"); handled {
		t.Fatal("HandleAction(navigate_down) was handled while filtering")
	}
	if e.tree.cursor != before {
		t.Fatalf("cursor moved from %d to %d while filtering", before, e.tree.cursor)
	}
}

// Scenario: Dead raw-key handling in the explorer tree is removed.
func TestExplorer_TreeNoLongerHandlesRawKeys(t *testing.T) {
	e := newExplorerTest()
	before := e.tree.cursor

	for _, msg := range []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: 'k', Text: "k"},
		{Code: 'g', Text: "g"},
		{Code: 'G', Text: "G"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyBackspace},
	} {
		if _, handled := e.tree.Update(msg); handled {
			t.Errorf("tree handled raw key %q by itself", msg.String())
		}
	}
	if e.tree.cursor != before {
		t.Fatalf("raw keys moved the cursor from %d to %d", before, e.tree.cursor)
	}
}
