package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// cell.go:49 `lipgloss.Width(text) <= maxWidth` -> `<`
func TestZZKill_CellExactWidthMultilineIsNotTruncated(t *testing.T) {
	cr := newCellRendererForTest()
	text := "abcde\nxy" // lipgloss.Width = 5 (max line width) == maxWidth
	if got := cr.Truncate(text, 5); got != text {
		t.Errorf("Truncate(%q, 5) = %q, want it untouched: it fits line by line", text, got)
	}
	// And through the render path the value must survive too: both lines are
	// still there (the cell pads each line out to its width).
	if got := ansi.Strip(cr.RenderCell(text, 7, false)); !strings.Contains(got, "abcde") || !strings.Contains(got, "xy") {
		t.Errorf("RenderCell(%q) lost the value: %q", text, got)
	}
}

// header.go:141 `lipgloss.Width(label) > maxTextWidth` -> `>=`
func TestZZKill_HeaderExactWidthMultibyteLabelIsNotTruncated(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"漢"}) // display width 2, 3 bytes
	h.SetWidths([]int{4})       // maxTextWidth = 4-2 = 2 == lipgloss.Width(label)
	got := ansi.Strip(h.Render(0, 1))
	if !strings.Contains(got, "漢") {
		t.Errorf("a label that exactly fills the header cell was mangled: %q", got)
	}
	if strings.Contains(got, "�") {
		t.Errorf("the label was cut on a byte boundary: %q", got)
	}
	// ASCII control at the same boundary stays intact either way.
	h2 := newHeaderForTest()
	h2.SetColumns([]string{"abc"})
	h2.SetWidths([]int{5})
	if got := ansi.Strip(h2.Render(0, 1)); !strings.Contains(got, "ABC") {
		t.Errorf("ASCII label at exact maxTextWidth was truncated: %q", got)
	}
}
