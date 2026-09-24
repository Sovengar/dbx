// Package keydisplay renders keyboard key hints for the TUI. When Nerd Font
// glyphs are enabled (the default) whole-word "enter" and "esc"/"escape" tokens
// are replaced with their Material Design glyphs; every other character,
// including spacing and separators, is preserved.
package keydisplay

import (
	"regexp"
	"strings"
)

// Nerd Font Material Design glyphs (Supplementary Private Use Area-A, so they
// need the \U escape form).
const (
	glyphEnter  = "\U000F0311" // nf-md-keyboard_return
	glyphEscape = "\U000F12B7" // nf-md-keyboard_esc
)

// keyPattern matches whole-word Enter/Escape tokens case-insensitively. The
// longer "escape" alternative precedes "esc" so it wins; the word boundaries
// keep substrings such as "Editor", "entered" or "escalate" untouched.
var keyPattern = regexp.MustCompile(`(?i)\b(escape|esc|enter)\b`)

// nerdFont is the process-wide glyph switch, defaulting to on. The app sets it
// from ui.nerd_font before any view renders.
var nerdFont = true

// SetNerdFont toggles glyph substitution for Enter/Escape key hints.
func SetNerdFont(on bool) { nerdFont = on }

// NerdFont reports whether glyph substitution is enabled.
func NerdFont() bool { return nerdFont }

// Key renders a key-hint string. With glyphs disabled it returns s unchanged.
// Otherwise whole-word occurrences of "enter" and "esc"/"escape" become their
// glyphs, preserving all other characters.
func Key(s string) string {
	if !nerdFont {
		return s
	}
	return keyPattern.ReplaceAllStringFunc(s, func(match string) string {
		if strings.EqualFold(match, "enter") {
			return glyphEnter
		}
		return glyphEscape
	})
}
