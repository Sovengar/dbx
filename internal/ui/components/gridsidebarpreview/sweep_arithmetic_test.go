package gridsidebarpreview

// Scenario: Los supervivientes no-Boundary del sweep sobre preview.go:160.
//
// Both mutants — INVERT_NEGATIVES and ARITHMETIC_BASE — rewrite the SAME token: the `-`
// of `end == -1` in highlightJSON's unterminated-string branch becomes `+` (gremlins maps
// token.SUB to token.ADD for both types), so one observable kills the pair. `end` comes
// from strings.IndexByte, which reports "no closing quote" as -1; the mutant reads -1 as
// "the closer is at offset 1", skips the branch, and paints an unterminated string as if
// it were closed. The assertions are against the STYLES' OWN BYTES: the un-styled text
// diff would show the same characters either way.

import (
	"testing"
)

// Scenario: Una cadena sin comilla de cierre se pinta ENTERA como texto plano.
//
// Kills INVERT_NEGATIVES internal/ui/components/gridsidebarpreview/preview.go:160 and
// ARITHMETIC_BASE internal/ui/components/gridsidebarpreview/preview.go:160 (`end == -1`
// -> `end == +1`). Under either mutant the not-found sentinel no longer matches: the code
// falls through to the closing-quote path with end = i, paints only the opening quote as
// a string VALUE and lets the rest of the line leak through unpainted. Asserted against
// the Text style's exact bytes for the whole remainder, so neither a half-painted line
// nor a differently-styled one can pass.
func TestHighlightJSON_PaintsAnUnterminatedStringAsPlainRemainder(t *testing.T) {
	p := newPreview(t, 60, 20)

	got := p.highlightJSON(`  "unterminated`)
	if want := "  " + p.styles.Text.Render(`"unterminated`); got != want {
		t.Errorf("highlightJSON of an unterminated string\n got %q\nwant %q", got, want)
	}
}
