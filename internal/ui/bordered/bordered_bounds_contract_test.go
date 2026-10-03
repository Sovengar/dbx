// parseAnsiSegments, at the boundary of its own escape guard.
//
// The scanner walks the string looking for `\033[`, and the guard that keeps it from
// reading past the end is `i+1 < len(runes)`. The value that tells the guard apart from
// a version of itself is a string whose LAST rune is a bare ESC:
//
//	original  i+1 == len(runes), so the guard declines and the ESC is text
//	mutant    i+1 <= len(runes), so it reads runes[i+1] — one past the end
//
// The mutant PANICS, and a panic inside a test aborts the whole binary, which is why
// this reads as a survivor to the tool rather than a kill. `mustNotPanic` recovers it
// and reports it through the testing package.
//
// A bare trailing ESC is not an exotic input: it is what arrives when a string is cut
// at a byte boundary, when a terminal hands over a partial sequence, or when a caller
// builds one by slicing. The guard is load-bearing and this is what makes it verified
// rather than merely present.

package bordered

import (
	"strings"
	"testing"
)

// mustNotPanic turns "the guard declined to read past the end" into a test failure
// instead of a crashed binary. The panic is what the tool scores as a survivor, so it
// has to be caught for the mutant to count as killed at all.
func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v — the guard must decline an index past the end, not perform it", what, r)
		}
	}()
	f()
}

// textsOf joins the text of every segment, which is what the caller actually renders.
func textsOf(segs []ansiSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// Scenario: Una cadena que ACABA en ESC suelto se trata como texto.
//
// The scanner's contract is that every byte of the input comes back exactly once, in
// order: escape sequences become a style and no text, everything else becomes text. A
// trailing ESC cannot start a sequence, so it has to land in the text — and reading the
// next rune to find that out is precisely what must not happen at the end of the string.
func TestParseAnsiSegments_AStringEndingInABareEscapeIsText(t *testing.T) {
	const esc = "\033"

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		// The boundary itself: the last rune is ESC and there is no next
		// rune to inspect.
		{"a lone escape", esc, esc},
		{"text then a trailing escape", "hello" + esc, "hello" + esc},
		{"a trailing escape after a real sequence", "\033[32mgreen\033[0m" + esc, "green" + esc},
		{"two escapes at the end", "x" + esc + esc, "x" + esc + esc},
		// And the shape it is NOT: a complete sequence, which the scanner
		// is supposed to recognise. Without this a "does not crash" result
		// would also be produced by a scanner that recognises nothing.
		{"a complete sequence is a style and no text", "\033[32mgreen\033[0m", "green"},
		{"a bare opening bracket is text", "a[b", "a[b"},
		{"an escape then a non-bracket is text", esc + "x", esc + "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var segs []ansiSegment
			mustNotPanic(t, "parseAnsiSegments", func() { segs = parseAnsiSegments(tc.in) })

			if got := textsOf(segs); got != tc.want {
				t.Errorf("parseAnsiSegments(%q) produced the text %q, want %q — every byte has to come back once, in order",
					tc.in, got, tc.want)
			}
		})
	}

	// The empty string, which is the other end of the loop and the reason the
	// guard has two halves to get right.
	t.Run("an empty string has no segments", func(t *testing.T) {
		if got := parseAnsiSegments(""); len(got) != 0 {
			t.Errorf("parseAnsiSegments(\"\") produced %d segments, want none", len(got))
		}
	})

	// A sequence with no terminator, which sends the inner scan to the end of
	// the string and is the shape most likely to reach the same guard from the
	// other side.
	t.Run("an unterminated sequence does not read past the end", func(t *testing.T) {
		for _, in := range []string{"\033", "\033[", "\033[3", "\033[32", "\033[32m", "abc\033["} {
			var segs []ansiSegment
			mustNotPanic(t, "parseAnsiSegments on "+in, func() { segs = parseAnsiSegments(in) })
			// Whatever it decides, the total length in runes cannot
			// exceed the input: a scanner that invents runes would
			// render more than it was given.
			total := 0
			for _, s := range segs {
				total += len([]rune(s.text))
			}
			if total > len([]rune(in)) {
				t.Errorf("parseAnsiSegments(%q) produced %d text runes from a %d-rune input",
					in, total, len([]rune(in)))
			}
		}
	})
}
