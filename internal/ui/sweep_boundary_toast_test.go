package ui

// Boundary proofs for toast.go CONDITIONALS_BOUNDARY mutants (report.json lines
// 78, 130, 133, 215, 218, 221, 224, 227, 230, 233, 236, 251, 254, 257).
//
// A boundary mutant (`<` -> `<=`, `>` -> `>=`, `<=` -> `<`, `>=` -> `>`)
// differs from the original on EXACTLY ONE input: equality of the two operands.
// Each test below builds that equality (and guards the fixture so the equality
// really holds), then asserts the original's observable behaviour.
//
// Two shapes occur:
//
//   - KILLED: the equality is reachable and changes the outcome (a code point
//     whose width flips, a toast the strict `<` reaps and the `<=` keeps).
//   - EQUIVALENT: the branch body assigns the value the variable already holds
//     (the two clamps), or a guard ABOVE the mutated line already answers for
//     that exact code point (the unicode script tables), so both arms produce
//     the same observable result. The equivalence test states the guard and
//     pins the behaviour the original has at the boundary.
//
// Verified with /tmp/opencode/sweep/verify.py.

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

// LINE 78: `now.Sub(toast.Created) < toast.Duration` -> `<=`.
//
// The only state where the two differ is age == Duration exactly. That state is
// unreachable through Update() with a real clock: the toast is stamped with
// time.Now() by the test and Update() samples its own time.Now() afterwards,
// and the monotonic clock advances between the two reads, so the age is
// strictly greater than 0 (when Duration == 0) and strictly greater than
// Duration (when Created is in the past). The test pins the original behaviour
// at the one candidate point — a zero-duration toast reaps on the next Update —
// and asserts the age measurement is never equal to the duration.
func TestToastBoundaryZeroDurationToastExpiresOnUpdate(t *testing.T) {
	m := toastMgr()
	before := time.Now()
	m.toasts = []Toast{{
		Message:  "edge",
		Level:    ToastInfo,
		Created:  before,
		Duration: 0,
	}}
	m.Update()
	if len(m.toasts) != 0 {
		t.Fatalf("a zero-duration toast survived Update: %v", m.toasts)
	}
	// The age at Update time is what makes the equality unreachable: the
	// reaper sampled the clock after the stamp, so age > 0 == Duration.
	if age := time.Since(before); age <= 0 {
		t.Fatalf("fixture: the clock did not advance between the stamp and the reaper (age %v)", age)
	}
}

// LINE 130: `if needed < toastMinWidth` -> `<=`. Boundary: needed == 20, a
// 12-column message (12 + 4 icon + 4 border). The body assigns needed = 20,
// which is the value it already holds, so both arms return 20.
func TestToastBoundaryCalculateWidthExactlyAtTheFloor(t *testing.T) {
	m := toastMgr()
	at := strings.Repeat("a", 12) // 12 + 4 + 4 == toastMinWidth
	if got := m.calculateWidth(at); got != toastMinWidth {
		t.Errorf("a 12-column message measured %d, want exactly the %d floor", got, toastMinWidth)
	}
	// Neighbours: one column under still clamps up, one over escapes the clamp.
	if got := m.calculateWidth(strings.Repeat("a", 11)); got != toastMinWidth {
		t.Errorf("an 11-column message measured %d, want the %d floor", got, toastMinWidth)
	}
	if got := m.calculateWidth(strings.Repeat("a", 13)); got != 13+4+4 {
		t.Errorf("a 13-column message measured %d, want its own %d", got, 13+4+4)
	}
}

// LINE 133: `if needed > toastMaxWidth` -> `>=`. Boundary: needed == 60, a
// 52-column message. The body assigns needed = 60, already held.
func TestToastBoundaryCalculateWidthExactlyAtTheCeiling(t *testing.T) {
	m := toastMgr()
	at := strings.Repeat("a", 52) // 52 + 4 + 4 == toastMaxWidth
	if got := m.calculateWidth(at); got != toastMaxWidth {
		t.Errorf("a 52-column message measured %d, want exactly the %d ceiling", got, toastMaxWidth)
	}
	if got := m.calculateWidth(strings.Repeat("a", 51)); got != 51+4+4 {
		t.Errorf("a 51-column message measured %d, want its own %d", got, 51+4+4)
	}
	if got := m.calculateWidth(strings.Repeat("a", 53)); got != toastMaxWidth {
		t.Errorf("a 53-column message measured %d, want the %d ceiling", got, toastMaxWidth)
	}
}

// LINES 215/218/221/224/227/230/233/236/251/254/257: the eleven hardcoded wide
// ranges, each with both of its bounds mutated. The table below was MEASURED
// (unicode.Is over the script tables): a head marked tableCovered is already
// answered by a script-table guard ABOVE the mutated line, so `>=` weakened to
// `>` cannot change its answer — the equivalence test states the guard. Every
// other bound is KILLED by isWideRune itself: the mutant drops (or adds) exactly
// one code point, and the assertion on that point is what fails under it.
//
// Each entry also pins the narrow neighbour below the head, so the fixture
// proves the head really is where the answer changes.
func TestToastBoundaryWideRangeBoundsMeasured(t *testing.T) {
	for _, tc := range []struct {
		name        string
		line        int
		head, tail  rune
		table       *unicode.RangeTable // nil: no script table claims the head
		tableClaims rune                // the code point the table does claim (guard witness)
		belowWide   bool                // the code point below the head is already wide (previous range's tail)
	}{
		{"line 215 cjk radicals", 215, 0x2E80, 0x303E, unicode.Han, 0x2E81, false},
		{"line 218 hiragana/han", 218, 0x3040, 0x9FFF, nil, 0, false},
		{"line 221 hangul syllables", 221, 0xAC00, 0xD7AF, unicode.Hangul, 0xAC01, false},
		{"line 224 cjk compat ideographs", 224, 0xF900, 0xFAFF, unicode.Han, 0xF901, false},
		{"line 227 cjk compat forms", 227, 0xFE30, 0xFE6F, nil, 0, false},
		{"line 230 fullwidth ascii", 230, 0xFF01, 0xFF60, nil, 0, false},
		{"line 233 fullwidth currency", 233, 0xFFE0, 0xFFE6, nil, 0, false},
		{"line 236 cjk ext B", 236, 0x20000, 0x2FA1F, unicode.Han, 0x20001, false},
		{"line 251 emoji pictographs", 251, 0x1F300, 0x1F5FF, nil, 0, false},
		{"line 254 emoji transport", 254, 0x1F600, 0x1F9FF, nil, 0, true},
		{"line 257 emoji supplemental", 257, 0x1FA70, 0x1FAFF, nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both ends of the range are wide: the tail mutant (`<=` -> `<`)
			// drops the last code point unless a table claims it.
			if tc.table != nil {
				// Head EQUIVALENT: the script table answers before the mutated
				// line is reached, and it claims the head but NOT the code point
				// below it (otherwise the head would not be a range edge at all).
				if !unicode.Is(tc.table, tc.head) {
					t.Errorf("fixture: %v does not claim U+%04X as the guard proof requires", tc.table, tc.head)
				}
				if !isWideRune(tc.head) {
					t.Errorf("U+%04X is the range head but is reported narrow", tc.head)
				}
				if !unicode.Is(tc.table, tc.tableClaims) && !isWideRune(tc.tableClaims) {
					t.Errorf("fixture: the guard witness U+%04X is not wide through the table", tc.tableClaims)
				}
			} else if !isWideRune(tc.head) {
				// Head KILLED: the range itself is the only thing answering for
				// the head, so `>=` -> `>` drops it.
				t.Errorf("U+%04X is the range head but is reported narrow", tc.head)
			}
			// Neighbour below the head. For most ranges it is narrow, which
			// proves the head is where the answer changes. Line 254's head
			// (U+1F600) sits directly on line 251's tail (U+1F5FF), so there
			// the neighbour is the previous range's wide tail instead.
			if tc.belowWide {
				if !isWideRune(tc.head - 1) {
					t.Errorf("U+%04X below the head is reported narrow, want the previous range's wide tail", tc.head-1)
				}
			} else if isWideRune(tc.head - 1) {
				t.Errorf("U+%04X sits just below the range but is reported wide", tc.head-1)
			}
			// The tail: only line 218's tail (U+9FFF) is table-claimed; every
			// other tail is the range's own last answer.
			if tc.line == 218 {
				if !unicode.Is(unicode.Han, tc.tail) {
					t.Errorf("fixture: Han does not claim U+%04X", tc.tail)
				}
				if !isWideRune(tc.tail) {
					t.Errorf("U+%04X is the range tail but is reported narrow", tc.tail)
				}
			} else if !isWideRune(tc.tail) {
				t.Errorf("U+%04X is the range tail but is reported narrow", tc.tail)
			}
			// The width the tail mutant would change is observable downstream.
			if got := displayWidth(string(tc.tail)); got != 2 {
				t.Errorf("U+%04X measures %d cells, want 2", tc.tail, got)
			}
		})
	}
}
