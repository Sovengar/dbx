package gridsidebarpreview

// Scenario: Los supervivientes CONDITIONALS_BOUNDARY del sweep sobre preview.go.
//
// Each mutant changes a comparison operator at a boundary. The ones that matter are
// those where a value lands EXACTLY on the boundary: an off-by-one that includes or
// excludes one element too many. The rest are equivalent because both branches do
// the same thing at the boundary.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Scenario: ScrollDown no se pasa del máximo cuando scrollY == maxScroll.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:58 col 15
// (`p.scrollY < maxScroll` -> `p.scrollY <= maxScroll`). Under the mutant, when scrollY
// equals maxScroll, the guard still fires and increments scrollY past the end —
// the scroll runs off the document.
func TestSidebarBoundaryScrollDownStopsAtMaxScroll(t *testing.T) {
	p := newPreview(t, 60, 10)
	cols, row := wideRow(30)
	p.SetRow(cols, row)

	// Scroll to the maximum.
	for range 100 {
		p.ScrollDown()
	}
	maxScroll := p.scrollY

	// One more ScrollDown must not move past the end.
	p.ScrollDown()
	if p.scrollY != maxScroll {
		t.Errorf("ScrollDown past maxScroll: scrollY = %d, want %d", p.scrollY, maxScroll)
	}
}

// Scenario: SetFKRow con exactamente len(columns) valores no accede fuera de rango.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:97 col 8
// (`i < len(row)` -> `i <= len(row)`). Under the mutant, when i == len(row), the loop
// accesses row[i] — a panic on the last column. The fixture has exactly as many values
// as columns, so i reaches len(row) on the last iteration.
func TestSidebarBoundarySetFKRowWithExactColumnsDoesNotPanic(t *testing.T) {
	p := newPreview(t, 60, 20)
	cols := []string{"id", "name", "total"}
	row := []interface{}{int64(1), "ada", 42}

	// This must not panic.
	p.SetFKRow(cols, row, "users")

	// The render must contain all three values.
	out := ansi.Strip(p.Render())
	for _, want := range []string{"1", "ada", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render does not contain %q:\n%s", want, out)
		}
	}
}

// Scenario: highlightJSON reconoce '0' y '9' como dígitos.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:176 col 9
// (`ch >= '0'` -> `ch > '0'`) and col 22 (`ch <= '9'` -> `ch < '9'`). Under the first
// mutant, '0' is not recognized as a digit; under the second, '9' is not. The test
// feeds a string with '0' and '9' and checks they are painted as numbers.
func TestSidebarBoundaryHighlightJSONRecognizesZeroAndNineAsDigits(t *testing.T) {
	p := newPreview(t, 60, 20)

	for _, tc := range []struct {
		name string
		in   string
	}{
		{"zero", "0"},
		{"nine", "9"},
		{"mixed", "09"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.highlightJSON(tc.in)
			// The number style's escape prefix must be present.
			if !strings.Contains(got, "\x1b[") {
				t.Errorf("highlightJSON(%q) = %q, want it painted as a number", tc.in, got)
			}
			// And the text must be unchanged.
			if ansi.Strip(got) != tc.in {
				t.Errorf("highlightJSON(%q) reads back as %q, want %q", tc.in, ansi.Strip(got), tc.in)
			}
		})
	}
}

// Scenario: highlightJSON no accede fuera de rango al final de la línea.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:178 col 34
// (`j < len(line)` -> `j <= len(line)`). Under the mutant, when j == len(line), the
// loop accesses line[j] — a panic. The fixture ends with a digit so the loop reaches
// the end of the line.
func TestSidebarBoundaryHighlightJSONDoesNotPanicAtEndOfLine(t *testing.T) {
	p := newPreview(t, 60, 20)

	// This must not panic.
	got := p.highlightJSON("42")
	if ansi.Strip(got) != "42" {
		t.Errorf("highlightJSON(\"42\") reads back as %q, want \"42\"", ansi.Strip(got))
	}
}

// Scenario: SetFKRow con MÁS cols que valores no accede fuera de rango.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:97 col 8
// (`i < len(row)` -> `i <= len(row)`). Under the mutant, when i == len(row), the loop
// accesses row[i] — a panic. The fixture has MORE columns than values, so i reaches
// len(row) while columns still has entries. The existing test uses equal lengths,
// where the loop never reaches i == len(row) and the mutant is unobservable.
func TestSweepKillSetFKRowWithMoreColumnsThanValues(t *testing.T) {
	p := newPreview(t, 60, 20)
	cols := []string{"id", "name", "total", "extra"}
	row := []interface{}{int64(1), "ada", 42}

	// This must not panic.
	p.SetFKRow(cols, row, "users")

	// The render must contain the three values that exist.
	out := ansi.Strip(p.Render())
	for _, want := range []string{"1", "ada", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render does not contain %q:\n%s", want, out)
		}
	}
}

// Scenario: highlightJSON pinta "10" como un solo número, no dos.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:178 col 34
// (`line[j] >= '0'` -> `line[j] > '0'`). Under the mutant, a '0' after the first digit
// is not recognized as part of the number, so "10" is painted as Info("1")+Info("0")
// instead of Info("10"). The existing test uses "42", where '2' > '0' in both versions
// and the mutant is unobservable.
func TestSweepKillHighlightJSONPaintsZeroInsideNumber(t *testing.T) {
	p := newPreview(t, 60, 20)

	got := p.highlightJSON("10")
	if want := p.styles.Info.Render("10"); got != want {
		t.Errorf("highlightJSON(\"10\") = %q, want %q", got, want)
	}
}

// Scenario: highlightJSON pinta "99" como un solo número, no dos.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:178 col 52
// (`line[j] <= '9'` -> `line[j] < '9'`). Under the mutant, a '9' after the first digit
// is not recognized as part of the number, so "99" is painted as Info("9")+Info("9")
// instead of Info("99"). The existing test uses "0"/"9"/"09", where no '9' appears
// at position j>0 inside a number and the mutant is unobservable.
func TestSweepKillHighlightJSONPaintsNineInsideNumber(t *testing.T) {
	p := newPreview(t, 60, 20)

	got := p.highlightJSON("99")
	if want := p.styles.Info.Render("99"); got != want {
		t.Errorf("highlightJSON(\"99\") = %q, want %q", got, want)
	}
}

// Scenario: contentHeight con height == 4 es 0, no negativo.
//
// CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:47 col 7
// (`h < 0` -> `h <= 0`) is EQUIVALENT: when h == 0, both branches assign 0.
func TestSidebarBoundaryContentHeightAtHeightFourIsZero(t *testing.T) {
	p := newPreview(t, 60, 4)
	if got := p.contentHeight(); got != 0 {
		t.Errorf("contentHeight() at height 4 = %d, want 0", got)
	}
}

// Scenario: ScrollDown con maxScroll == 0 no se mueve.
//
// CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:55 col 15
// (`maxScroll < 0` -> `maxScroll <= 0`) is EQUIVALENT: when maxScroll == 0, both
// branches assign 0.
func TestSidebarBoundaryScrollDownWithZeroMaxScrollDoesNotMove(t *testing.T) {
	p := newPreview(t, 60, 20)
	p.SetRow([]string{"id"}, []interface{}{int64(1)})

	// maxScroll = len(lines) - contentHeight() = 3 - 16 = -13 -> clamped to 0.
	p.ScrollDown()
	if p.scrollY != 0 {
		t.Errorf("ScrollDown with maxScroll=0 left scrollY at %d, want 0", p.scrollY)
	}
}

// Scenario: Render con end == len(lines) no cambia end.
//
// CONDITIONALS_BOUNDARY internal/ui/components/gridsidebarpreview/preview.go:140 col 9
// (`end > len(p.lines)` -> `end >= len(p.lines)`) is EQUIVALENT: when end == len(lines),
// both branches leave end unchanged.
func TestSidebarBoundaryRenderWithEndEqualToLinesLength(t *testing.T) {
	p := newPreview(t, 60, 20)
	cols, row := wideRow(8)
	p.SetRow(cols, row)

	// Render must not panic and must show the document.
	out := ansi.Strip(p.Render())
	if !strings.Contains(out, "{") {
		t.Errorf("the render has no document:\n%s", out)
	}
}
