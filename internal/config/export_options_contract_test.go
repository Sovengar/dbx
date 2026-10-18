package config

// Scenario: An unknown or empty delimiter falls back to a comma.
//
// The delimiter is a user preference read from a TOML file, and encoding/csv
// answers an invalid Comma by refusing to write ANY record: the exporters ignore
// that error, so the user gets an empty file that looks like a successful export
// of an empty result. Normalising here is what keeps a typo from silently
// emptying every export, and it is why the fallback has to be exactly what
// encoding/csv accepts.

import "testing"

// Scenario: The delimited export uses the configured delimiter.
// Scenario: A tab delimiter writes real tab bytes and a .tsv file.
func TestTheExportDelimiterIsNormalisedToOneUsableRune(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  rune
	}{
		// The defaults: unset and the historical comma both mean comma. The empty
		// case is load-bearing beyond typos — every fixture that builds
		// &config.Config{} has an empty delimiter.
		{"unset", "", ','},
		{"comma", ",", ','},

		// The delimiters the feature exists for.
		{"semicolon", ";", ';'},
		{"pipe", "|", '|'},
		{"literal tab", "\t", '\t'},

		// Both spellings of a tab: a TOML reader cannot see a raw tab in every
		// context, so the name and the escape are accepted too.
		{"tab alias", "tab", '\t'},
		{"escaped tab", `\t`, '\t'},

		// A multi-byte character is ONE rune, not one byte.
		{"enye", "ñ", 'ñ'},

		// A literal backslash is a usable delimiter as far as encoding/csv cares.
		{"backslash", `\`, '\\'},

		// Everything encoding/csv rejects, plus the multi-character values it
		// cannot take at all.
		{"too long", "ab", ','},
		{"newline", "\n", ','},
		{"carriage return", "\r", ','},
		{"quote", `"`, ','},
		{"nul", "\x00", ','},
		{"replacement character", "\uFFFD", ','},
		{"word that is not tab", "tabs", ','},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExportDelimiterRune(tc.value); got != tc.want {
				t.Errorf("ExportDelimiterRune(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
