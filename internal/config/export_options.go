package config

import "unicode/utf8"

// ExportDelimiterRune turns `ui.export_delimiter` into the single rune the
// delimited exporters hand to encoding/csv.
//
// The accepted spellings are a literal character (";", "|", ",") or the aliases
// "tab" and `\t` (two characters, backslash then t) for the tab character,
// which is what a TOML string can hold unambiguously.
//
// Everything else — empty, more than one character, or a rune encoding/csv
// rejects — degrades to ',' instead of failing. A preference is not a request:
// a mistyped delimiter must not turn every export into an empty file, which is
// exactly what encoding/csv does when Comma is invalid (Write reports an error
// the exporters ignore). This mirrors theme.Resolve falling back to a safe
// value rather than aborting.
func ExportDelimiterRune(value string) rune {
	switch value {
	case "":
		return ','
	case "tab", `\t`:
		return '\t'
	}

	runes := []rune(value)
	if len(runes) != 1 || !validDelimiter(runes[0]) {
		return ','
	}
	return runes[0]
}

// validDelimiter is encoding/csv's own validDelim, spelled out here because it
// is unexported and the fallback must happen in ONE place: a rune csv rejects
// silently produces an empty export, so it has to become ',' before it gets
// there. The surprising entry is '"' — it is the quote character, not a
// separator.
func validDelimiter(r rune) bool {
	return r != 0 && r != '"' && r != '\r' && r != '\n' &&
		utf8.ValidRune(r) && r != utf8.RuneError
}
