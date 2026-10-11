package gridsidebarpreview

// Scenario: Los diez CONDITIONALS_NEGATION de preview.go:178, el número como token.
//
// Line 178 is the loop that extends a number token past its first character: it runs while
// the byte is a digit or one of the characters a JSON number/timestamp may contain
// (`. - e E + t Z`). Every assertion here is against the Info style's OWN BYTES, because
// the stripped text reads back identically however the token is segmented: a mutant that
// splits `42` into two spans or swallows a foreign character into the number still prints
// the same characters — it prints them PAINTED differently, and that is the observable.
//
// Killed mutants (gremlins CONDITIONALS_NEGATION, internal/ui/components/gridsidebarpreview/preview.go:178):
//
//	col 10  `j < len(line)`       -> `j >= len(line)`      TestHighlightJSON_NumberAtLineEndIsOneStyledSpan
//	col 34  `line[j] >= '0'`      -> `<` (the digit run never starts)
//	col 52  `line[j] <= '9'`      -> `>` (the digit run never continues)
//	col 70  `line[j] == '.'`      -> `!=` (the loop swallows everything after a digit)
//	col 88  `line[j] == '-'`      -> `!=`                  TestHighlightJSON_NumberTokenRejectsForeignBytes
//	col 106 `line[j] == 'e'`      -> `!=`
//	col 124 `line[j] == 'E'`      -> `!=`
//	col 142 `line[j] == '+'`      -> `!=`
//	col 160 `line[j] == 't'`      -> `!=`
//	col 178 `line[j] == 'Z'`      -> `!=`

import (
	"testing"

	"github.com/buble/dbx/internal/theme"
)

// Scenario: Un número al FINAL de la línea es UN solo span del estilo Info.
//
// Kills the negations of cols 10, 34 and 52 of preview.go:178. The loop guard negated
// (col 10) never runs the extension loop for a byte inside the line and DOES run it for
// j == len(line), where `line[j]` panics — either way `42` comes out as two one-byte
// spans instead of the single Info span `st.Info.Render("42")`. The negated digit
// comparisons (cols 34 and 52) stop the extension at the second byte, producing the same
// two spans. Asserted against the style's own bytes: the unstyled text is `42` either way.
func TestHighlightJSON_NumberAtLineEndIsOneStyledSpan(t *testing.T) {
	p := newPreview(t, 60, 20)
	st := theme.Resolve("dark").Styles()

	if got, want := p.highlightJSON("42"), st.Info.Render("42"); got != want {
		t.Errorf("highlightJSON(%q)\n got %q\nwant %q", "42", got, want)
	}
}

// Scenario: El token numérico NO se traga un carácter que no es de número.
//
// Kills the negations of the seven value disjuncts of preview.go:178 (cols 70, 88, 106,
// 124, 142, 160, 178: `. - e E + t Z` -> `!=`). Each negated disjunct is true for `x`,
// so under any of them the extension loop eats the rest of the line into the number's
// span: `1x` is painted as ONE Info token instead of the Info span `1` followed by the
// plain `x`. `x` differs from every member of the set, so this one input kills all seven.
func TestHighlightJSON_NumberTokenRejectsForeignBytes(t *testing.T) {
	p := newPreview(t, 60, 20)
	st := theme.Resolve("dark").Styles()

	if got, want := p.highlightJSON("1x"), st.Info.Render("1")+"x"; got != want {
		t.Errorf("highlightJSON(%q)\n got %q\nwant %q", "1x", got, want)
	}
}

// Scenario: El punto decimal va DENTRO del token numérico.
//
// Kills the col 70 negation of preview.go:178 (`line[j] == '.'` -> `!=`, which stops the
// extension AT the dot: `12.5` is painted as two number spans with a plain dot between
// them, instead of the single Info token the other three tests on this file would group
// differently). Kept separate from the foreign-byte test so the fixture states the
// decimal's own contract: a decimal number is one token, dots included.
func TestHighlightJSON_DecimalNumberIsOneStyledSpan(t *testing.T) {
	p := newPreview(t, 60, 20)
	st := theme.Resolve("dark").Styles()

	if got, want := p.highlightJSON("12.5"), st.Info.Render("12.5"); got != want {
		t.Errorf("highlightJSON(%q)\n got %q\nwant %q", "12.5", got, want)
	}
}
