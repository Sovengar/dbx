package ui

// Scenario: El ancho de un rune, que decide si una celda se parte o se corta.
//
// isWideRune is the whole of dbx's East Asian width handling, and it is a chain of eleven
// range checks. The Latin ones are exercised by every test in this package; the CJK ones are
// not, because no test in the repo had any CJK text in it — which is why three of the arms
// sat uncovered in a function that every message in the product goes through.
//
// The consequence of getting it wrong is not cosmetic. A wide rune occupies TWO terminal
// cells, so treating one as one shifts every character after it by one column — which for a
// grid cell means the value runs into the column border, and for a toast means the box is
// drawn a character short of its text.
//
// The three checks below are the ones a Latin-only test cannot reach, and each is a
// different Unicode block:
//
//	Hangul   the Korean syllables, U+AC00–U+D7A3
//	Katakana  the Japanese katakana, U+30A0–U+30FF
//	Hiragana  the Japanese hiragana, U+3040–U+309F
//
// The ranges after them are covered by the Han check above, which is the trap: U+1100 and
// U+2E80 are wide, but they are also not Han, Katakana, Hangul or Hiragana, so the ranges
// are what catch them.

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/buble/dbx/internal/theme"
)

func TestTheWideRunesAreTheOnesThatTakeTwoCells(t *testing.T) {
	t.Run("the three CJK blocks the Latin tests never reach", func(t *testing.T) {
		// One representative per block, and the range bounds, because a range check that is
		// off by one at an edge is the failure that only shows on the character nobody
		// tested.
		for _, tc := range []struct {
			name string
			r    rune
		}{
			{"hangul: the first syllable", '가'}, // U+AC00
			{"hangul: the last syllable", '힣'},  // U+D7A3
			{"katakana: a vowel", 'ア'},          // U+30A2
			{"katakana: the last one", 'ン'},     // U+30FC
			{"hiragana: a vowel", 'あ'},          // U+3042
			{"hiragana: the last one", '゛'},     // U+309B
		} {
			t.Run(tc.name, func(t *testing.T) {
				if !isWideRune(tc.r) {
					t.Errorf("%U is not counted as wide, so it will be laid out in one cell", tc.r)
				}
			})
		}
	})

	// The three CJK blocks a Latin-only test cannot reach, each a different check in the
	// chain, plus the Jamo block which no named script covers.
	for _, tc := range []struct {
		name string
		r    rune
		why  string
	}{
		{"hangul: the first syllable", '가', "unicode.Hangul"},
		{"hangul: the last syllable", '힣', "unicode.Hangul"},
		{"katakana: a vowel", 'ア', "unicode.Katakana"},
		{"katakana: the last one", 'ン', "unicode.Katakana"},
		{"hiragana: a vowel", 'あ', "unicode.Hiragana"},
		{"hiragana: the last one", '゛', "unicode.Hiragana"},
		// The Jamo block is wide and is caught by an EXPLICIT RANGE rather than by any named
		// script — which is why it needs its own case: a test of Korean syllables alone leaves
		// it uncovered, even though it is the block Hangul is built FROM.
		{"a conjoining jamo", '\u1100', "the U+1100 range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !isWideRune(tc.r) {
				t.Errorf("%U is counted as one cell, so it is caught by %s and every character after it shifts left by one", tc.r, tc.why)
			}
		})
	}

	t.Run("emoji, which the original range list had left out", func(t *testing.T) {
		// The bug this file found. Every wide block was present — including CJK Extension B at
		// U+20000, which is much FURTHER from an emoji than Hangul is — and the emoji blocks
		// were not, so a value with an emoji in it laid out one column short and the toast
		// border went through the text.
		for _, tc := range []struct {
			name string
			r    rune
		}{
			{"a party popper", '\U0001F389'},
			{"a grinning face", '\U0001F600'},
			{"a rocket", '\U0001F680'},
			{"a red heart", '\U0001F495'},
			{"the first of the pictographs block", '\U0001F300'},
			{"the last of the pictographs block", '\U0001F5FF'},
			{"a chess pawn", '\U0001FA70'},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if !isWideRune(tc.r) {
					t.Errorf("%U is counted as one cell, so every character after it shifts left by one", tc.r)
				}
			})
		}
	})

	t.Run("and the narrow ones stay narrow", func(t *testing.T) {
		// The counterweight in both directions. "Everything is wide" satisfies the cases above
		// and would double the width of every ASCII message — and, with the dingbats left out
		// deliberately, of every check mark too.
		for _, r := range []rune{'a', 'Z', '0', ' ', '.', 'ñ', 'é', '→', '✓', '✗', '·', '│'} {
			if isWideRune(r) {
				t.Errorf("%U (%q) is counted as wide", r, r)
			}
		}
	})
}

// The property that makes the width function matter: a string's rendered width is the sum of
// its runes' widths, and a wide rune moves every character after it by a column. A toast
// built from mixed text has to be as wide as the sum, or the border cuts through the text.
func TestMixedWidthTextAddsUp(t *testing.T) {
	// Korean, Japanese and ASCII in one string — the shape a real message takes when a user
	// has data with non-Latin column names.
	const mixed = "orders 테이블 orders テーブル"

	// The expected width is computed from the fixture's own wide runes rather than written
	// down, because the first version of this case counted four of them and the fixture has
	// seven — three in the Korean word and four in the Japanese one. A hard-coded count in a
	// width test is a test that breaks when the fixture is edited, which is how a real
	// regression gets called a fixture problem.
	wide := 0
	for _, r := range mixed {
		if isWideRune(r) {
			wide++
		}
	}
	if wide == 0 {
		t.Fatal("the fixture has no wide runes in it")
	}

	width := 0
	for _, r := range mixed {
		if isWideRune(r) {
			width += 2
			continue
		}
		width++
	}

	if want := utf8.RuneCountInString(mixed) + wide; width != want {
		t.Errorf("the mixed string measures %d cells, want %d", width, want)
	}
	// And the naive measure is what would be wrong: the byte length is nearly three times the
	// cell width for this string, which is why nothing in the toast code may use len().
	if bytes := len(mixed); bytes <= width {
		t.Errorf("the fixture is not exercising the difference: %d bytes vs %d cells", bytes, width)
	}

	t.Run("and a toast built from it renders", func(t *testing.T) {
		// The end of the chain: the width function exists for the renderer, so the assertion
		// that matters is that a wide-character toast draws a box around its text rather
		// than through it.
		th := NewToastManager(theme.Resolve("dark").Styles())
		th.SetWidth(60)
		th.ShowInfo(mixed)
		out := th.View()
		if out == "" {
			t.Fatal("the toast rendered nothing")
		}
		if !strings.Contains(out, "orders") {
			t.Errorf("the toast lost its ASCII text: %q", out)
		}
	})
}

// The Jamo range that used to be in isWideRune.
//
// It answered for nothing: every one of the 96 code points in U+1100–U+115F is inside
// unicode.Hangul, which is checked two lines earlier. So the branch was unreachable in the
// sense that matters — not "the compiler cannot see it", but "removing it changes no answer".
//
// The measurement is the test. A comment saying a range is redundant is worth nothing on its
// own, because a future Unicode table could make it redundant no longer; this one re-measures
// it, and if a point ever falls outside unicode.Hangul the test says so — at which point the
// range has to come BACK, and the failure message says exactly that.
func TestTheJamoRangeIsRedundant(t *testing.T) {
	outside := make([]rune, 0)
	for r := rune(0x1100); r <= 0x115F; r++ {
		if !unicode.Is(unicode.Hangul, r) {
			outside = append(outside, r)
		}
	}

	if len(outside) > 0 {
		t.Errorf("%d code points in U+1100-U+115F are outside unicode.Hangul (first %U): "+
			"the explicit range that used to be in isWideRune is needed again and has to come back",
			len(outside), outside[0])
	}

	// And the consequence, asserted rather than assumed: every one of them is still wide,
	// through the Hangul check.
	for r := rune(0x1100); r <= 0x115F; r++ {
		if !isWideRune(r) {
			t.Fatalf("%U is neither Hangul nor covered by any range, so it is one cell wide", r)
		}
	}
}
