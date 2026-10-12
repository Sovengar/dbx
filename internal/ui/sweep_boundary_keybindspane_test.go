package ui

// Boundary proof for keybindspane.go:70, the `s.width <= 0` guard in Height().
//
// The mutant (`<=` -> `<`) differs from the original on exactly one input:
// width == 0. There the original returns 0 (the pane does not draw), while the
// mutant falls through to `len(s.renderLines()) + 2`, which counts the wrapped
// segment list — one line plus the rounded border's two rows. The test pins the
// original's answer at that width.
//
// Verified with /tmp/opencode/sweep/verify.py.

import (
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func TestKeybindsPaneBoundaryZeroWidthHasZeroHeight(t *testing.T) {
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	p.SetWidth(0)
	p.SetFocus(config.ContextExplorer)
	if got := p.Height(); got != 0 {
		t.Errorf("a zero-width pane reports height %d, want 0 (it must not reserve rows it never draws)", got)
	}
	// A sized pane reports its lines plus the border, so the mutant's answer is
	// distinguishable from the original's 0.
	p.SetWidth(200)
	if got := p.Height(); got <= 2 {
		t.Errorf("a sized pane reports height %d, want more than the 2 border rows", got)
	}
}
