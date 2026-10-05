package gridpreview

// Scenario: The JSON syntax highlighter, which is the most heavily mutated function in this
// package and the one with the fewest assertions.
//
// highlightJSON walks a line byte by byte and colours each token by what it is. Almost every
// mutation in it is invisible to a test that only checks the TEXT comes back unchanged -- and
// that is exactly the test that exists (TestTheHighlighterLeavesPlainTextAlone asserts
// ansi.Strip(out) == line for a corpus). A highlighter that painted every token the same
// colour would satisfy it perfectly.
//
// So the assertions here are on the COLOUR: each token is looked for wrapped in the exact
// escape sequence one named style produces. Two consequences follow, and both matter:
//
//   - A key and a value are both quoted strings, so text-only assertions cannot tell them
//     apart. The highlighter tells them apart by looking at what follows the closing quote,
//     which is the branch the corpus never reached.
//   - The number scanner's character set includes `.`, `e`, `E`, `+`, `-`, `t` and `Z` --
//     the last two for ISO timestamps like 2024-01-15T10:30:00Z. Every one of those is a
//     boundary a test has to actually feed it, and a test that only feeds plain integers
//     leaves the whole exponent and timestamp half of the scanner unverified.
//
// The values below are chosen to sit ON the boundaries rather than near them: 0 and 9 for the
// first and last digits, a leading minus, a fraction, an exponent, and a timestamp.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ansi_Strip removes the colour escapes so an assertion can be about the text.
func ansi_Strip(s string) string { return ansi.Strip(s) }

// themed is a preview whose styles the assertions read, so a theme change shows up as a
// failure here rather than as a mysterious escape sequence nobody can interpret.
func themed(t *testing.T) *GridPreview {
	t.Helper()
	return newPreviewTest()
}

// colourOf reports whether s appears in out wrapped in the exact escapes style produces.
//
// Containment rather than parsing: the highlighter emits style.Render(token) verbatim, so the
// wrapped form IS the output, and comparing the whole rendered token catches a style swap as
// reliably as it catches a missing one.
func colourOf(t *testing.T, out string, style lipgloss.Style, token string) bool {
	t.Helper()
	return strings.Contains(out, style.Render(token))
}

// The whole point of the highlighter: the two kinds of quoted string get different colours,
// and nothing else changes.
func TestTheHighlighterTellsAKeyFromAValue(t *testing.T) {
	p := themed(t)
	out := p.highlightJSON(`{"name": "ada", "count": 7}`)

	cases := []struct {
		what   string
		style  lipgloss.Style
		style2 *lipgloss.Style
		token  string
	}{
		// A string followed by a colon is a KEY.
		{what: "a key", style: p.styles.Primary, token: `"name"`},
		// A string not followed by one is a VALUE.
		{what: "a value", style: p.styles.Success, token: `"ada"`},
	}
	for _, tc := range cases {
		if !colourOf(t, out, tc.style, tc.token) {
			t.Errorf("%s (%s) was not rendered in its own colour.\n got: %q", tc.what, tc.token, out)
		}
	}

	// And the counterweight: the value must NOT be painted as a key. Without this, a
	// highlighter that painted everything Primary passes both assertions above, and "which
	// colour is this" is the only question this function answers.
	if colourOf(t, out, p.styles.Primary, `"ada"`) {
		t.Error(`"ada" is a value, not a key, but it was painted as a key`)
	}
	if colourOf(t, out, p.styles.Success, `"name"`) {
		t.Error(`"name" is a key, not a value, but it was painted as a value`)
	}

	// The number is a third colour, distinct from both string colours.
	if !colourOf(t, out, p.styles.Info, "7") {
		t.Errorf("the number was not rendered in the number colour.\n got: %q", out)
	}
}

// The branch that decides key from value is `rest := TrimSpace(line[end+1:]); len(rest) > 0 &&
// rest[0] == ':'`. Three ways of failing to be a key, and each is a different half of the
// condition, so each is a separate case.
func TestTheHighlighterNeedsTheColonRightAfterTheQuote(t *testing.T) {
	p := themed(t)

	for _, tc := range []struct {
		name string
		line string
		// isKey says whether the quoted run is a key, i.e. whether a colon follows it.
		isKey bool
	}{
		{"a colon immediately after", `{"k": 1}`, true},
		{"a space before the colon", `{"k" : 1}`, true},
		{"no colon at all, an array element", `["v", 1]`, false},
		{"no colon at all, a bare value", `"v"`, false},
		{"nothing after the closing quote", `{"k"}`, false},
		// And the empty-rest half: a colon that is not the next non-space character is not
		// a key marker. `{"k"}` above covers the empty case; this one covers a rest that is
		// non-empty and not a colon.
		{"a non-colon character after the quote", `{"k"x 1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := p.highlightJSON(tc.line)
			// The quoted run is the first pair of quotes in the line.
			start := strings.Index(tc.line, `"`)
			rest := tc.line[start+1:]
			end := strings.Index(rest, `"`)
			token := tc.line[start : start+end+2]

			keyStyle, valueStyle := p.styles.Primary, p.styles.Success
			wantStyle := valueStyle
			if tc.isKey {
				wantStyle = keyStyle
			}
			if !colourOf(t, out, wantStyle, token) {
				t.Errorf("%s was not rendered as a %s.\n got: %q",
					token, map[bool]string{true: "key", false: "value"}[tc.isKey], out)
			}
			// And not as the other kind. One assertion of each pair is enough; asserting both
			// on every case would only re-test the same branch.
			other := keyStyle
			if tc.isKey {
				other = valueStyle
			}
			if colourOf(t, out, other, token) {
				t.Errorf("%s was painted as a %s instead of the %s it is",
					token,
					map[bool]string{true: "value", false: "key"}[tc.isKey],
					map[bool]string{true: "key", false: "value"}[tc.isKey])
			}
		})
	}
}

// The number scanner's boundaries.
//
// `ch >= '0' && ch <= '9' || ch == '-'` decides where a number starts, and the inner loop's
// wider character set decides where it ends. Both are boundary conditions, so the values here
// are the boundary values themselves: 0 and 9 are the first and last digits, and a leading
// minus is the only other thing allowed to start one.
//
// If the start condition lost its `<= '9'` half, a value of 9 would be painted as plain text.
// If it lost `>= '0'`, a value of 0 would be. Both are one character of difference in the
// output and neither is visible in the plain text.
func TestTheHighlighterRecognisesNumbersAtTheirBoundaries(t *testing.T) {
	p := themed(t)

	for _, tc := range []struct {
		name string
		line string
		num  string
	}{
		{"the lowest single digit", `{"v": 0}`, "0"},
		{"the highest single digit", `{"v": 9}`, "9"},
		{"a digit in the middle", `{"v": 4}`, "4"},
		{"a negative number", `{"v": -1}`, "-1"},
		{"a negative fraction", `{"v": -0.5}`, "-0.5"},
		{"a fraction", `{"v": 1.25}`, "1.25"},
		{"a trailing zero", `{"v": 1.0}`, "1.0"},
		{"a positive exponent", `{"v": 1e3}`, "1e3"},
		{"a negative exponent", `{"v": 1e-3}`, "1e-3"},
		{"a capital E exponent", `{"v": 1E3}`, "1E3"},
		{"an explicit plus", `{"v": 1e+3}`, "1e+3"},
		// NOT a timestamp: see TestTheNumberScannerDoesNotHandleATimestamp. The set has 't'
		// and 'Z' but not 'T', so a real ISO timestamp stops at the T.
		{"a lowercase t is inside the number", `{"v": 1et}`, "1et"},
		{"a Z is inside the number", `{"v": 1Z}`, "1Z"},
		{"a zero in a multi-digit number", `{"v": 101}`, "101"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := p.highlightJSON(tc.line)
			if !colourOf(t, out, p.styles.Info, tc.num) {
				t.Errorf("the number %q was not painted as a number.\n got: %q", tc.num, out)
			}
		})
	}
}

// The literals, and the one that is deliberately a DIFFERENT colour from the others.
//
// null is TextMuted while true and false are Info. That asymmetry is asserted rather than
// assumed: it is the kind of thing a theme change fixes "for consistency" without anyone
// noticing, and the cost of noticing is one line here versus a screenshot later.
func TestTheHighlighterColoursTheThreeLiterals(t *testing.T) {
	p := themed(t)

	t.Run("null is muted", func(t *testing.T) {
		out := p.highlightJSON(`{"v": null}`)
		if !colourOf(t, out, p.styles.TextMuted, "null") {
			t.Errorf("null was not painted muted.\n got: %q", out)
		}
		if colourOf(t, out, p.styles.Info, "null") {
			t.Error("null was painted as a number, the same colour as true and false")
		}
	})

	for _, lit := range []string{"true", "false"} {
		t.Run(lit+" is a number colour", func(t *testing.T) {
			out := p.highlightJSON(`{"v": ` + lit + `}`)
			if !colourOf(t, out, p.styles.Info, lit) {
				t.Errorf("%s was not painted as a number.\n got: %q", lit, out)
			}
		})
	}

	t.Run("and a word that merely starts like one is not a literal", func(t *testing.T) {
		// `nullify` begins with `null`, and the scanner uses HasPrefix -- so it matches the
		// first four characters and leaves `ify` as text. Whether that is right is not the
		// question here; that it is DELIBERATE is, because a reader would otherwise assume the
		// literals are whole words and "fix" it into a word-boundary check.
		out := p.highlightJSON(`{"v": nullify}`)
		if !strings.Contains(ansi_Strip(out), "nullify") {
			t.Errorf("the text was not preserved: %q", out)
		}
		if colourOf(t, out, p.styles.TextMuted, "nullify") {
			t.Errorf("nullify was treated as the literal null.\n got: %q", out)
		}
	})
}

// The two branches that only malformed input reaches.
//
// An unterminated quote, and a line with no quotes at all. Both are ordinary states for a
// highlighter that is fed a partially-typed document, which is exactly what the jq input is.
func TestTheHighlighterSurvivesHalfTypedJSON(t *testing.T) {
	p := themed(t)

	for _, tc := range []struct {
		name string
		line string
	}{
		{"an unterminated string", `{"k": "unfinished`},
		{"a lone opening quote", `"`},
		{"no quotes at all", `123`},
		{"an empty line", ``},
		{"just a brace", `{`},
		{"a bare number with a sign", `-`},
		{"a colon with nothing around it", `:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The one property that matters: nothing panics, and nothing hangs. A highlighter
			// that loses track of its index on an unterminated quote loops forever or reads
			// out of bounds, and both happen on a document the user is halfway through typing.
			out := p.highlightJSON(tc.line)
			if got := ansi_Strip(out); got != tc.line {
				t.Errorf("the text came back as %q, want %q unchanged", got, tc.line)
			}
		})
	}
}

// The number scanner's character set contains 't' and 'Z'.
//
// Those two characters have no purpose in a JSON number. There is no exponent that uses them,
// and no literal that contains them. What they DO appear in is an ISO-8601 timestamp -- which
// makes the set read like someone started adding timestamp support and stopped halfway: the
// set has 'Z' but not 'T', and 'e'/'E'/'+' for exponents but no ':'.
//
// So an ISO timestamp is not highlighted as one. It is highlighted as four separate numbers
// with a bare capital T between the first two, and the capital T is the only thing on the line
// not painted as a number:
//
//	{"v": 2024-01-15T10:30:00Z}
//	        ^^^^^^^^ number   ^ plain  ^ number  ^ number  ^ number
//
// The exact wiring point is the character test at the bottom of highlightJSON:
//
//	if ch >= '0' && ch <= '9' || ch == '-' {          <- where a number starts
//	    for j < len(line) && (line[j] >= '0' && line[j] <= '9' || line[j] == '.' ||
//	        line[j] == '-' || line[j] == 'e' || line[j] == 'E' || line[j] == '+' ||
//	        line[j] == 't' || line[j] == 'Z') {          <- where it ends: 't' and 'Z' only
//
// Fixing it means adding 'T' and ':' to that set -- and ':' would then be consumed by any
// number followed by a colon, which is not obviously right either, because in pretty-printed
// JSON `1:` does not occur but `{"a":1}` does put a colon after a number. So the honest
// statement is that the set's intent is unclear enough that changing it is a design decision,
// not a bug fix, which is why this is pinned rather than changed.
//
// What is pinned is the CURRENT behaviour, exactly: the timestamp comes back with its text
// intact, which is the property that matters, and the capital T is the only uncoloured part.
func TestTheNumberScannerDoesNotHandleATimestamp(t *testing.T) {
	p := themed(t)
	const line = `{"v": 2024-01-15T10:30:00Z}`

	out := p.highlightJSON(line)

	// The text is untouched, whatever the colours are.
	if got := ansi_Strip(out); got != line {
		t.Errorf("the text came back as %q, want %q unchanged", got, line)
	}

	// The four fragments it does paint as numbers, and the one it does not.
	for _, frag := range []string{"2024-01-15", "10", "30", "00Z"} {
		if !colourOf(t, out, p.styles.Info, frag) {
			t.Errorf("the fragment %q was not painted as a number.\n got: %q", frag, out)
		}
	}
	if colourOf(t, out, p.styles.Info, "T") {
		t.Error("the capital T was painted as part of the number, so this set does include it " +
			"and the comment above is wrong")
	}

	// And the lowercase 't' that IS in the set, which is what makes the gap look deliberate
	// rather than accidental: it works for the spelling nobody writes.
	lower := p.highlightJSON(`{"v": 1et}`)
	if !colourOf(t, lower, p.styles.Info, "1et") {
		t.Errorf("a lowercase t did not extend the number.\n got: %q", lower)
	}
}
