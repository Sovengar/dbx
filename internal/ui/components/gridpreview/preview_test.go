package gridpreview

import (
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func newPreviewTest() *GridPreview {
	styles := theme.Resolve("dark").Styles()
	p := New(styles, config.NewKeybindRegistry(config.KeybindingsConfig{}))
	p.SetWidth(80)
	p.SetHeight(20)
	p.SetRow([]string{"a", "b", "c"}, []interface{}{1, 2, 3})
	return p
}

// Scenario: the preview cursor actions dispatch through action IDs and work
// regardless of focus.
func TestGridPreview_HandleAction_NavigateWorksUnfocused(t *testing.T) {
	p := newPreviewTest()
	if len(p.lines) < 2 {
		t.Fatalf("precondition failed: need multiple lines, got %d", len(p.lines))
	}
	if p.focused {
		t.Fatal("precondition failed: preview should start unfocused")
	}

	if _, handled := p.HandleAction("navigate_down"); !handled {
		t.Fatal("HandleAction(navigate_down) was not handled")
	}
	if p.cursorLine != 1 {
		t.Fatalf("cursorLine = %d, want 1", p.cursorLine)
	}

	p.HandleAction("navigate_up")
	if p.cursorLine != 0 {
		t.Fatalf("cursorLine = %d, want 0 after navigate_up", p.cursorLine)
	}
}

// Scenario: while typing a JQ filter the preview owns the keys.
func TestGridPreview_HandleAction_NoOpInJQMode(t *testing.T) {
	p := newPreviewTest()
	p.EnterJQMode()

	if _, handled := p.HandleAction("navigate_down"); handled {
		t.Fatal("HandleAction(navigate_down) was handled while in JQ mode")
	}
	if p.cursorLine != 0 {
		t.Fatalf("cursorLine = %d, want 0 while in JQ mode", p.cursorLine)
	}
}
