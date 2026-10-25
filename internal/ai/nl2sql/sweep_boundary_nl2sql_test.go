package nl2sql

import (
	"strings"
	"testing"
)

// Scenario: cleanSQL on a fence whose CLOSING backticks sit at position zero of
// the rest, with content after them — "``````x", an empty fenced block whose
// closing fence is glued to trailing text. The guard is `closed >= 0`: relaxed
// to `closed > 0` the closing fence is missed, the switch falls to the
// glued-fence branch with the trailing text, and that text leaks into the
// statement. (Six bare backticks alone do NOT distinguish them: the final
// TrimSuffix eats the whole "```" under both readings, so the trailing byte is
// what makes the boundary observable.)
func TestNl2sqlBoundaryCleanSQLEmptyFenceAtPositionZero(t *testing.T) {
	if got := cleanSQL("``````x"); got != "" {
		t.Errorf("cleanSQL of a six-backtick fence glued to text = %q, want empty: an empty fenced block is no statement", got)
	}
}

// Scenario: isWordRune at the LOWERCASE A edge — the exact boundary of
// `r >= 'a'`. Relaxed to `r > 'a'` the scan of a word stops on its very first
// lowercase a, the word read is the empty string, and a statement that begins
// with "alter" is no longer recognised as one. The edge itself is pinned too,
// so the acceptance of 'a' is asserted and not inferred.
func TestNl2sqlBoundaryWordRuneLowerAEdge(t *testing.T) {
	if !isWordRune('a') {
		t.Error("isWordRune('a') = false, want true: the first lowercase letter is a word rune")
	}
	if isWordRune('`') {
		t.Error("isWordRune('`') = true, want false: one below the range is not a word rune")
	}
	if !statementOnLine("alter table t") {
		t.Error(`statementOnLine("alter table t") = false, want true: a word starting with 'a' must scan whole`)
	}
}

// Scenario: isWordRune at the LOWERCASE Z edge — the exact boundary of
// `r <= 'z'`. Relaxed to `r < 'z'` a word carrying a 'z' past its middle is
// cut at the z: "analyze" reads as "analy", which is not a statement keyword,
// so ANALYZE stops being a statement. The edge itself is pinned too.
func TestNl2sqlBoundaryWordRuneLowerZEdge(t *testing.T) {
	if !isWordRune('z') {
		t.Error("isWordRune('z') = false, want true: the last lowercase letter is a word rune")
	}
	if isWordRune('{') {
		t.Error("isWordRune('{') = true, want false: one above the range is not a word rune")
	}
	if !statementOnLine("analyze statistic") {
		t.Error(`statementOnLine("analyze statistic") = false, want true: the 'z' must not cut the word in half`)
	}
}

// Scenario: isWordRune at the UPPERCASE A edge — the exact boundary of
// `r >= 'A'`. Relaxed to `r > 'A'` a capitalised word is cut on its first
// letter, so "Alter" reads as the empty word and the statement is missed.
func TestNl2sqlBoundaryWordRuneUpperAEdge(t *testing.T) {
	if !isWordRune('A') {
		t.Error("isWordRune('A') = false, want true: the first uppercase letter is a word rune")
	}
	if isWordRune('@') {
		t.Error("isWordRune('@') = true, want false: one below the range is not a word rune")
	}
	if !statementOnLine("Alter table t") {
		t.Error(`statementOnLine("Alter table t") = false, want true: a word starting with 'A' must scan whole`)
	}
}

// Scenario: isWordRune at the UPPERCASE Z edge — the exact boundary of
// `r <= 'Z'`. Relaxed to `r < 'Z'` a keyword carrying a 'Z' mid-word is cut
// at it: "ANALYZE" reads as "ANALY", which is not in the statement map, and
// the statement is missed. The edge itself is pinned too.
func TestNl2sqlBoundaryWordRuneUpperZEdge(t *testing.T) {
	if !isWordRune('Z') {
		t.Error("isWordRune('Z') = false, want true: the last uppercase letter is a word rune")
	}
	if isWordRune('[') {
		t.Error("isWordRune('[') = true, want false: one above the range is not a word rune")
	}
	if !statementOnLine("ANALYZE statistic") {
		t.Error(`statementOnLine("ANALYZE statistic") = false, want true: the 'Z' must not cut the keyword in half`)
	}
}

// Scenario: the digit range of isWordRune is EQUIVALENT under `r > '0'`, and
// the reason is structural, not incidental. Every key of sqlStatementStarts is
// letters only — none begins with or contains a digit — so a word cut at a '0'
// and a word kept whole at a '0' are both absent from the map, and
// statementOnLine answers false either way. The test pins the acceptance of
// both digit edges, the false answers of the digit-cut shapes, and the absence
// of any key that could tell the two readings apart: green under the mutant,
// which is exactly what documents that the mutant changes nothing observable.
func TestNl2sqlBoundaryWordRuneDigitZeroEdgeIsEquivalent(t *testing.T) {
	if !isWordRune('0') {
		t.Error("isWordRune('0') = false, want true: the first digit is a word rune")
	}
	if !isWordRune('9') {
		t.Error("isWordRune('9') = false, want true: the last digit is a word rune")
	}
	// Cut at the digit vs kept whole: neither "a0" nor "a" is a keyword, and
	// neither is "0A" nor "" — false under both the original and the mutant.
	if statementOnLine("a0") {
		t.Error(`statementOnLine("a0") = true, want false: no keyword contains a digit`)
	}
	if statementOnLine("0a") {
		t.Error(`statementOnLine("0a") = true, want false: no keyword starts with a digit`)
	}
	for key := range sqlStatementStarts {
		if strings.ContainsAny(key, "0123456789") {
			t.Errorf("sqlStatementStarts holds %q, which a digit-cut word could half-match: the equivalence argument breaks", key)
		}
	}
}

// Scenario: a block comment whose terminator sits at position zero of what
// follows the opener — "/**/select 1", an empty comment glued to a statement on
// the same line. The guard is `end >= 0`: relaxed to `end > 0` the reader
// gives up on the line and returns false, hiding a real statement behind an
// empty comment.
func TestNl2sqlBoundaryBlockCommentTerminatorAtPositionZero(t *testing.T) {
	if !statementOnLine("/**/select 1") {
		t.Error(`statementOnLine("/**/select 1") = false, want true: an empty comment does not swallow the statement`)
	}
}

// Scenario: stripFenceTag at the boundary of `nl >= 0` is EQUIVALENT under
// `nl > 0`, and the reason is structural. The mutant only matters when the
// newline sits at position 0, making first = s (newline included) instead of
// first = s[:0] = "". But statementOnLine answers false for BOTH: "" has no
// word, and a line starting with '\n' has no word either — '\n' is not in the
// TrimLeft cutset (" \t\r") and it ends the word scan at index 0, so the word
// read is the empty string either way. With nl > 0 both readings take the same
// first line. The test pins the observable result at the boundary input.
func TestNl2sqlBoundaryStripFenceTagLeadingNewline(t *testing.T) {
	got, ok := stripFenceTag("\nSELECT 1")
	if !ok {
		t.Error(`stripFenceTag("\nSELECT 1") ok = false, want true: the empty first line is a tag position`)
	}
	if got != "SELECT 1" {
		t.Errorf(`stripFenceTag("\nSELECT 1") = %q, want "SELECT 1"`, got)
	}
}

// Scenario: stripJSONC on a document that ends with a comma followed by nothing
// but spaces. skipJSONCNoise then runs to len(src), and the guard
// `j < len(src)` is what refuses to read the byte AT len. Relaxed to
// `j <= len(src)` it indexes src[len(src)] and panics — the observable
// difference is the panic itself. The original keeps the trailing comma,
// because nothing follows it that would make it a trailing comma.
func TestNl2sqlBoundaryStripJSONCTrailingCommaThenSpaces(t *testing.T) {
	got := stripJSONC([]byte("{\"a\": 1,   "))
	if !strings.ContainsRune(string(got), ',') {
		t.Errorf("stripJSONC dropped the trailing comma: got %q, want it preserved (only spaces follow it)", got)
	}
}

// Scenario: skipJSONCNoise scanning a // comment that runs to the very end of
// the input without a newline. Its inner loop is `for i < len(src) &&
// src[i] != '\n'`; relaxed to `i <= len(src)` the loop reads src[len(src)] and
// panics when the comment is the last thing in the document. Reached from
// stripJSONC through a comma followed by a line comment.
func TestNl2sqlBoundarySkipJSONCCommentRunsToInputEnd(t *testing.T) {
	got := stripJSONC([]byte("{\"a\": 1, // x"))
	if !strings.ContainsRune(string(got), ',') {
		t.Errorf("stripJSONC dropped the comma: got %q, want it preserved (the comment is not a closing brace)", got)
	}
}

// The first non-word byte terminates a SQL verb. This makes punctuation after
// a verb valid while keeping punctuation alone out of the statement set.
func TestNl2sqlStatementWordTerminatesAtPunctuation(t *testing.T) {
	if !statementOnLine("SELECT; -- terminate") {
		t.Error("a semicolon after SELECT must not hide the statement")
	}
	if statementOnLine("; SELECT 1") {
		t.Error("punctuation before SELECT is not a SQL statement start")
	}
}
