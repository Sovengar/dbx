package app

import (
	"testing"
)

// Scenario: blankRange with from == 0 — the exact boundary of `if from < 0`.
// The clamp is a no-op at zero: relaxed to `from <= 0` the arm runs and assigns
// from = 0, the value it already had, so the output cannot differ. The test
// pins the boundary input itself, and the negative side that clamps onto it.
func TestTxnBoundaryBlankRangeFromAtZero(t *testing.T) {
	out := []byte("abc")
	blankRange(out, 0, 2)
	if string(out) != "  c" {
		t.Errorf("blankRange(from=0) = %q, want %q", out, "  c")
	}
	// A negative from clamps to 0 — the same assignment the relaxed arm
	// performs at the boundary, so both inputs must agree.
	out = []byte("abc")
	blankRange(out, -3, 2)
	if string(out) != "  c" {
		t.Errorf("blankRange(from<0) = %q, want %q", out, "  c")
	}
}

// Scenario: blankRange with to == len(out) — the exact boundary of
// `if to > len(out)`. Relaxed to `to >= len(out)` the arm runs and assigns
// to = len(out), the value it already had. Newlines survive both ways, which
// is the other property this boundary must not disturb.
func TestTxnBoundaryBlankRangeToAtLength(t *testing.T) {
	out := []byte("abc")
	blankRange(out, 1, 3)
	if string(out) != "a  " {
		t.Errorf("blankRange(to=len) = %q, want %q", out, "a  ")
	}
	withNl := []byte("a\nc")
	blankRange(withNl, 0, 3)
	if string(withNl) != " \n " {
		t.Errorf("blankRange(to=len) disturbed the newline: %q, want %q", withNl, " \n ")
	}
}

// Scenario: an E-string whose LAST byte is a backslash. The escape guard
// `i+1 < len(sql)` refuses to consume a byte past the end; relaxed to
// `i+1 <= len(sql)` it advances two and lands at len+1 — and the blankRange
// clamp at len(out) makes the two readings mask exactly the same bytes.
func TestTxnBoundaryEStringEndingInBackslash(t *testing.T) {
	if got, want := maskNonCode(`E'\`), "   "; got != want {
		t.Errorf("maskNonCode of an E-string ending in a backslash = %q, want %q", got, want)
	}
}

// Scenario: an unterminated block comment. Its scan loop stops with
// i+1 == len(sql); the guard `i+1 < len(sql)` then takes the else arm and sets
// i = len(sql). Relaxed to `i+1 <= len(sql)` it takes the if arm and sets
// i = len(sql)+2 — and the blankRange clamp at len(out) masks the same bytes
// either way.
func TestTxnBoundaryUnterminatedBlockComment(t *testing.T) {
	if got, want := maskNonCode("/* ab"), "     "; got != want {
		t.Errorf("maskNonCode of an unterminated block comment = %q, want %q", got, want)
	}
}
