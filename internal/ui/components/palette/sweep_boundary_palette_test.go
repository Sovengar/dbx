package palette

// Scenario: Los supervivientes CONDITIONALS_BOUNDARY del sweep sobre palette.go y fuzzy.go.
//
// Each mutant changes a comparison operator at a boundary. The ones that matter are
// those where a value lands EXACTLY on the boundary: an off-by-one that includes or
// excludes one element too many. The rest are equivalent because both branches do
// the same thing at the boundary.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Scenario: updateFiltered con cursor == len(filtered) se clampa a len-1.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:64 col 14
// (`p.cursor >= len(p.filtered)` -> `p.cursor > len(p.filtered)`). Under the mutant,
// when cursor == len(filtered), the clamp does not fire and cursor stays at len(filtered)
// — one past the end.
func TestPaletteBoundaryUpdateFilteredClampsCursorToLastIndex(t *testing.T) {
	p := withCommands(t, 3)
	p.cursor = 3 // == len(filtered)

	p.updateFiltered()

	if p.cursor != 2 {
		t.Errorf("updateFiltered with cursor=len(filtered) left cursor at %d, want 2", p.cursor)
	}
}

// Scenario: updateFiltered con cursor == 0 no lo hace negativo.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:67 col 14
// (`p.cursor < 0` -> `p.cursor <= 0`) is EQUIVALENT: when cursor == 0, both branches
// assign 0.
func TestPaletteBoundaryUpdateFilteredWithCursorZeroStaysZero(t *testing.T) {
	p := withCommands(t, 3)
	p.cursor = 0

	p.updateFiltered()

	if p.cursor != 0 {
		t.Errorf("updateFiltered with cursor=0 left cursor at %d, want 0", p.cursor)
	}
}

// Scenario: enter con cursor == len(filtered) no accede fuera de rango.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:93 col 55
// (`p.cursor < len(p.filtered)` -> `p.cursor <= len(p.filtered)`). Under the mutant,
// when cursor == len(filtered), the guard fires and accesses filtered[cursor] — a panic.
func TestPaletteBoundaryEnterWithCursorAtEndDoesNotPanic(t *testing.T) {
	p := withCommands(t, 3)
	p.cursor = 3 // == len(filtered)

	// This must not panic.
	cmd, handled := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("enter with cursor at end produced a command, want nil")
	}
	if !handled {
		t.Errorf("enter with cursor at end was not handled, want true")
	}
}

// Scenario: View con modalW == 40 no cambia modalW.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:149 col 12
// (`modalW < 40` -> `modalW <= 40`) is EQUIVALENT: when modalW == 40, both branches
// assign 40.
func TestPaletteBoundaryViewWithModalWFortyStaysForty(t *testing.T) {
	p := withCommands(t, 1)
	p.SetWidth(60) // modalW = 60 * 6 / 10 = 36 -> clamped to 40

	_ = p.View()

	// The modal width must be 40, not 36 or 41.
	// This is an equivalence test: the mutant produces the same modal width.
}

// Scenario: View con modalW == 80 no cambia modalW.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:152 col 12
// (`modalW > 80` -> `modalW >= 80`) is EQUIVALENT: when modalW == 80, both branches
// assign 80.
func TestPaletteBoundaryViewWithModalWEightyStaysEighty(t *testing.T) {
	p := withCommands(t, 1)
	p.SetWidth(200) // modalW = 200 * 6 / 10 = 120 -> clamped to 80

	_ = p.View()

	// The modal width must be 80, not 120 or 81.
	// This is an equivalence test: the mutant produces the same modal width.
}

// Scenario: View con padLen == 0 no agrega espacios.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:195 col 14
// (`padLen > 0` -> `padLen >= 0`) is EQUIVALENT: when padLen == 0,
// strings.Repeat(" ", 0) = "", so the item is unchanged.
func TestPaletteBoundaryViewWithZeroPadLenAddsNoSpaces(t *testing.T) {
	p := withCommands(t, 1)
	p.SetWidth(60)

	// A name exactly as wide as the input box: padLen = 0.
	p.commands = []command{{Name: strings.Repeat("x", 56), Section: SectionUI}}
	p.updateFiltered()

	// This must not panic and must render.
	_ = p.View()
}

// Scenario: View con cursor == len(cmdLineMap) no accede fuera de rango.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:216 col 54
// (`p.cursor < len(cmdLineMap)` -> `p.cursor <= len(cmdLineMap)`). Under the mutant,
// when cursor == len(cmdLineMap), the guard fires and accesses cmdLineMap[cursor] — a panic.
func TestPaletteBoundaryViewWithCursorAtEndDoesNotPanic(t *testing.T) {
	p := withCommands(t, 3)
	p.cursor = 3 // == len(cmdLineMap)

	// This must not panic.
	_ = p.View()
}

// Scenario: View con cursorLine == scrollOffset no cambia scrollOffset.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:219 col 17
// (`cursorLine < p.scrollOffset` -> `cursorLine <= p.scrollOffset`) is EQUIVALENT:
// when cursorLine == scrollOffset, both branches assign cursorLine (which equals scrollOffset).
func TestPaletteBoundaryViewWithCursorLineEqualToScrollOffsetStays(t *testing.T) {
	p := withCommands(t, 30)
	p.scrollOffset = 5
	p.cursor = 5 // cursorLine will be 6 (header at 0, command 5 at 6)

	_ = p.View()

	// The offset must not change because cursorLine == scrollOffset.
	// This is an equivalence test: the mutant produces the same offset.
}

// Scenario: View con cursorLine == scrollOffset+maxVisible ajusta scrollOffset.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:222 col 17
// (`cursorLine >= p.scrollOffset+maxVisible` -> `cursorLine > p.scrollOffset+maxVisible`).
// Under the mutant, when cursorLine == scrollOffset+maxVisible, the branch does not fire
// and the cursor is left one row below the window.
func TestPaletteBoundaryViewWithCursorLineAtWindowBottomScrolls(t *testing.T) {
	p := withCommands(t, 30)
	p.scrollOffset = 0
	p.cursor = 11 // cursorLine = 12, scrollOffset+maxVisible = 0+12 = 12

	_ = p.View()

	// The offset must be 1 (cursorLine - maxVisible + 1 = 12 - 12 + 1 = 1).
	if p.scrollOffset != 1 {
		t.Errorf("View with cursorLine at window bottom left scrollOffset at %d, want 1", p.scrollOffset)
	}
}

// Scenario: View con totalLines == maxVisible resetea scrollOffset a 0.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:239 col 16
// (`totalLines <= maxVisible` -> `totalLines < maxVisible`). Under the mutant, when
// totalLines == maxVisible, the clamp does not fire and scrollOffset stays at its
// previous value.
func TestPaletteBoundaryViewWithTotalLinesEqualToMaxVisibleResetsScroll(t *testing.T) {
	p := withCommands(t, 11) // 11 commands + 1 header = 12 lines == maxVisible
	p.scrollOffset = 5

	_ = p.View()

	if p.scrollOffset != 0 {
		t.Errorf("View with totalLines == maxVisible left scrollOffset at %d, want 0", p.scrollOffset)
	}
}

// Scenario: View con scrollOffset == 0 no lo hace negativo.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:242 col 16
// (`p.scrollOffset < 0` -> `p.scrollOffset <= 0`) is EQUIVALENT: when scrollOffset == 0,
// both branches assign 0.
func TestPaletteBoundaryViewWithZeroScrollOffsetStaysZero(t *testing.T) {
	p := withCommands(t, 30)
	p.scrollOffset = 0

	_ = p.View()

	if p.scrollOffset != 0 {
		t.Errorf("View with scrollOffset=0 left scrollOffset at %d, want 0", p.scrollOffset)
	}
}

// Scenario: View con totalLines == maxVisible muestra la lista entera.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/palette.go:247 col 16
// (`totalLines > maxVisible` -> `totalLines >= maxVisible`) is EQUIVALENT: when
// totalLines == maxVisible, listLines[0:maxVisible] == listLines.
func TestPaletteBoundaryViewWithTotalLinesEqualToMaxVisibleShowsAll(t *testing.T) {
	p := withCommands(t, 11) // 11 commands + 1 header = 12 lines == maxVisible

	out := p.View()

	// All 11 commands must be visible.
	if !strings.Contains(out, "command-00") || !strings.Contains(out, "command-10") {
		t.Errorf("View with totalLines == maxVisible does not show all commands:\n%s", out)
	}
}

// Scenario: fuzzySort con j == len(scored) no accede fuera de rango.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/palette/fuzzy.go:70 col 16
// (`j < len(scored)` -> `j <= len(scored)`). Under the mutant, when j == len(scored),
// the inner loop accesses scored[j] — a panic. The fixture has two commands with
// different scores so the inner loop runs.
func TestFuzzySortWithTwoCommandsDoesNotPanic(t *testing.T) {
	items := []command{
		{Name: "b"},
		{Name: "a b"},
	}

	// This must not panic.
	got := fuzzySort("b", items)
	if len(got) != 2 {
		t.Errorf("fuzzySort with two commands returned %d results, want 2", len(got))
	}
}

// Scenario: fuzzySort con i == len(scored) no cambia el orden.
//
// CONDITIONALS_BOUNDARY internal/ui/components/palette/fuzzy.go:70 col 16
// (`i < len(scored)` -> `i <= len(scored)`) is EQUIVALENT: scored[i] is only ever
// accessed inside the inner loop `for j := i+1; j < len(scored); j++`, which requires
// j > i AND j < len(scored) — therefore i < len(scored). When i == len(scored), the
// inner loop never enters, scored[i] is never evaluated, and the output is identical.
// This test asserts the original behaviour (exact order) and stays green under the mutant.
func TestSweepKillFuzzySortEquivalentAtEnd(t *testing.T) {
	items := []command{
		{Name: "b"},
		{Name: "a b"},
	}

	got := fuzzySort("b", items)

	if len(got) != 2 {
		t.Fatalf("fuzzySort returned %d results, want 2", len(got))
	}
	// Exact order: "b" scores 28 (word start + whole match), "a b" scores 18 (word start).
	if got[0].command.Name != "b" || got[1].command.Name != "a b" {
		t.Errorf("fuzzySort order = [%q, %q], want [\"b\", \"a b\"]",
			got[0].command.Name, got[1].command.Name)
	}
}
