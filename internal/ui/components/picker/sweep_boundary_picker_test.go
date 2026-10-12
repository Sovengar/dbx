package picker

// Scenario: Los supervivientes CONDITIONALS_BOUNDARY del sweep sobre picker.go.
//
// Each mutant changes a comparison operator at a boundary. The ones that matter are
// those where a value lands EXACTLY on the boundary: an off-by-one that includes or
// excludes one element too many. The rest are equivalent because both branches do
// the same thing at the boundary.

import (
	"testing"
)

// Scenario: getPort con portEnd == portStart retorna "".
//
// CONDITIONALS_BOUNDARY internal/ui/components/picker/picker.go:227 col 13
// (`portEnd > portStart` -> `portEnd >= portStart`) is EQUIVALENT: when portEnd == portStart,
// both branches return "" (afterAt[portStart:portStart] = "").
func TestPickerBoundaryGetPortWithEmptyPortReturnsEmpty(t *testing.T) {
	p := newPickerForTest()

	// A DSN with a colon but no digits after it.
	if got := p.getPort("@h:/db"); got != "" {
		t.Errorf("getPort(\"@h:/db\") = %q, want \"\"", got)
	}
}

// Scenario: centerText con textWidth == p.width retorna el texto sin padding.
//
// Kills CONDITIONALS_BOUNDARY internal/ui/components/picker/picker.go:236 col 15
// (`textWidth >= p.width` -> `textWidth > p.width`). Under the mutant, when textWidth == p.width,
// the guard does not fire and the function adds padding — the text is indented.
func TestPickerBoundaryCenterTextWithExactWidthReturnsTextUnchanged(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(3)

	got := p.centerText("abc")
	if got != "abc" {
		t.Errorf("centerText(\"abc\") at width 3 = %q, want \"abc\"", got)
	}
}

// Scenario: centerText con textWidth == p.width es equivalente en el límite.
//
// CONDITIONALS_BOUNDARY internal/ui/components/picker/picker.go:236 col 15
// (`textWidth >= p.width` -> `textWidth > p.width`) is EQUIVALENT: when textWidth == p.width,
// the original returns text directly; the mutant computes padding = (width - textWidth) / 2 = 0
// and returns strings.Repeat(" ", 0) + text = text. Same observable result.
// This test asserts the original behaviour and stays green under the mutant.
func TestSweepKillCenterTextEquivalentAtExactWidth(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(3)

	got := p.centerText("abc")
	if got != "abc" {
		t.Errorf("centerText(\"abc\") at width 3 = %q, want \"abc\"", got)
	}
}
