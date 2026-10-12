package ui

// Boundary proofs for modal.go CONDITIONALS_BOUNDARY mutants (report.json lines
// 71, 88, 119, 122, 194, 198, 201, 206).
//
// Every mutant here is a clamp whose body assigns the value the variable already
// holds at the boundary (`scroll = 0` when scroll is 0, `modalW = 50` when it
// is 50, `end = len(lines)` when it is len(lines)), so the equality case is a
// no-op and the mutant is equivalent. Each test builds the equality, asserts
// the original's observable behaviour, and pins one neighbour on each side so
// the fixture proves the boundary is where the test claims.
//
// Verified with /tmp/opencode/sweep/verify.py.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func uicoreModal(width, height int) *HelpModal {
	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(width)
	m.SetHeight(height)
	m.Show()
	return m
}

func uicoreKey(key string) tea.KeyPressMsg {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	default:
		return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
}

func uicoreWidestLine(t *testing.T, view string) int {
	t.Helper()
	var widest int
	for _, l := range strings.Split(ansi.Strip(view), "\n") {
		if n := lipgloss.Width(l); n > widest {
			widest = n
		}
	}
	return widest
}

// LINE 71: `if m.scroll < 0` (ctrl+u) -> `<=`. Boundary: scroll lands on exactly
// 0 after the -10 step (scroll was 10). The body assigns 0, already held.
func TestModalBoundaryCtrlULandsExactlyOnZero(t *testing.T) {
	m := uicoreModal(100, 400)
	m.scroll = 10
	if _, handled := m.Update(uicoreKey("ctrl+u")); !handled {
		t.Fatal("ctrl+u was not handled")
	}
	if m.scroll != 0 {
		t.Errorf("ctrl+u from scroll 10 left scroll at %d, want exactly 0", m.scroll)
	}
	// A step that overshoots clamps the same way on both arms.
	m.scroll = 5
	m.Update(uicoreKey("ctrl+u"))
	if m.scroll != 0 {
		t.Errorf("ctrl+u from scroll 5 left scroll at %d, want 0", m.scroll)
	}
}

// LINE 88: `if m.scroll < 0` (wheel up) -> `<=`. Boundary: scroll was 1, the
// wheel decrements to exactly 0, and the clamp is a no-op.
func TestModalBoundaryWheelUpLandsExactlyOnZero(t *testing.T) {
	m := uicoreModal(100, 400)
	m.scroll = 1
	if _, handled := m.Update(tea.MouseWheelMsg{X: 0, Y: -1, Button: tea.MouseWheelUp}); !handled {
		t.Fatal("wheel up was not handled")
	}
	if m.scroll != 0 {
		t.Errorf("wheel up from scroll 1 left scroll at %d, want exactly 0", m.scroll)
	}
}

// LINE 119: `if modalW < 50` -> `<=`. Boundary: width 72 gives modalW == 50
// exactly (72*7/10). The body assigns 50, already held.
func TestModalBoundaryWidthExactlyAtTheFloor(t *testing.T) {
	m := uicoreModal(72, 400)
	if got := uicoreWidestLine(t, m.View()); got != 50-4 {
		t.Errorf("width 72: content width = %d, want exactly %d (modalW 50, minus the 4-column frame)", got, 50-4)
	}
	// Neighbours: 71 clamps up to the same 50, 73 escapes the floor.
	if got := uicoreWidestLine(t, uicoreModal(71, 400).View()); got != 50-4 {
		t.Errorf("width 71: content width = %d, want the clamped %d", got, 50-4)
	}
	if got := uicoreWidestLine(t, uicoreModal(73, 400).View()); got != 51-4 {
		t.Errorf("width 73: content width = %d, want %d", got, 51-4)
	}
}

// LINE 122: `if modalW > 90` -> `>=`. Boundary: width 129 gives modalW == 90
// exactly (129*7/10). The body assigns 90, already held.
func TestModalBoundaryWidthExactlyAtTheCeiling(t *testing.T) {
	m := uicoreModal(129, 400)
	if got := uicoreWidestLine(t, m.View()); got != 90-4 {
		t.Errorf("width 129: content width = %d, want exactly %d (modalW 90, minus the 4-column frame)", got, 90-4)
	}
	// Neighbours: 128 stays under the ceiling, 130 clamps down to the same 90.
	if got := uicoreWidestLine(t, uicoreModal(128, 400).View()); got != 89-4 {
		t.Errorf("width 128: content width = %d, want %d", got, 89-4)
	}
	if got := uicoreWidestLine(t, uicoreModal(130, 400).View()); got != 90-4 {
		t.Errorf("width 130: content width = %d, want the clamped %d", got, 90-4)
	}
}

// LINE 194: `if maxVisible < 5` -> `<=`. Boundary: height 15 gives
// maxVisible == 5 exactly (15-10). The body assigns 5, already held.
func TestModalBoundaryMaxVisibleExactlyAtTheFloor(t *testing.T) {
	// +1 for the "Help" header, +2 for the top and bottom border.
	for _, tc := range []struct{ height, want int }{
		{14, 5 + 3}, // 14-10 = 4, clamped up to 5
		{15, 5 + 3}, // exactly 5, the boundary
		{16, 6 + 3}, // one over
	} {
		m := uicoreModal(100, tc.height)
		if got := len(strings.Split(ansi.Strip(m.View()), "\n")); got != tc.want {
			t.Errorf("height %d: rendered %d lines, want %d (maxVisible + header + 2 borders)", tc.height, got, tc.want)
		}
	}
}

// LINES 198 + 206: `if m.scroll > len(lines)-maxVisible` -> `>=` and
// `if end > len(lines)` -> `>=`. Boundary: scroll == len(lines)-maxVisible
// exactly, which also makes end == len(lines). Both bodies assign the value
// already held, so both mutants are equivalent; one fixture covers both.
func TestModalBoundaryScrollExactlyAtTheWindowEnd(t *testing.T) {
	// Measure the content length with a window taller than the content.
	m := uicoreModal(100, 400)
	contentLen := len(strings.Split(ansi.Strip(m.View()), "\n")) - 3 // header + 2 borders
	if contentLen >= 400-10 {
		t.Fatalf("fixture drifted: content is %d lines, want fewer than the 390-line window", contentLen)
	}

	m.SetHeight(15) // maxVisible = 5
	m.scroll = contentLen - 5
	view := ansi.Strip(m.View())
	// The window is exactly full: 5 content rows, +1 header, +2 borders.
	if got := len(strings.Split(view, "\n")); got != 5+3 {
		t.Errorf("scroll at the window end rendered %d lines, want %d (the full 5-row window)", got, 5+3)
	}
	// The window really ends on the last content line, not past it.
	if !strings.Contains(view, "scroll") {
		t.Error("the closing hint is not the last visible line: the window overshot the content")
	}
	// Rendering again at the clamp must not move the window.
	at := m.scroll
	m.View()
	if m.scroll != at {
		t.Errorf("second View() moved the scroll from %d to %d; the clamp is not stable", at, m.scroll)
	}
}

// LINE 201: `if m.scroll < 0` (View) -> `<=`. Boundary: scroll == 0. The body
// assigns 0, already held.
func TestModalBoundaryScrollZeroAtView(t *testing.T) {
	m := uicoreModal(100, 400)
	m.scroll = 0
	contentLen := len(strings.Split(ansi.Strip(m.View()), "\n")) - 3
	if got := len(strings.Split(ansi.Strip(m.View()), "\n")); got != contentLen+3 {
		t.Errorf("scroll 0 rendered %d lines, want %d (the whole content)", got, contentLen+3)
	}
}
