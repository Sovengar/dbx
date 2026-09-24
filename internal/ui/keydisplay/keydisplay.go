// Package keydisplay renders keyboard key hints for the TUI. When Nerd Font
// glyphs are enabled (the default) whole-word key tokens are replaced with their
// glyph and modifier combinations collapse to their symbol, so "ctrl+enter"
// becomes "⌃". Every other character, including spacing and separators, is
// preserved.
//
// Pass only key labels and key-hint footers to Key. The token set contains
// ordinary English words ("up", "down", "left", "right", "home", "end",
// "space", "tab", "delete"), so prose would be rewritten as well. This is why
// action descriptions are never routed through Key.
package keydisplay

import (
	"regexp"
	"strings"
)

// Glyphs. The md-* icons live in Supplementary Private Use Area-A, so they need
// the \U escape form; the modifier symbols are plain Unicode and render as-is.
//
// Every glyph below advances exactly one cell in the Nerd Font Mono builds, so
// rune-counted padding stays visually aligned.
const (
	glyphEnter     = "\U000F0311" // nf-md-keyboard_return
	glyphEscape    = "\U000F12B7" // nf-md-keyboard_esc
	glyphTab       = "\U000F0312" // nf-md-keyboard_tab
	glyphSpace     = "\U000F1050" // nf-md-keyboard_space
	glyphBackspace = "\U000F030D" // nf-md-keyboard_backspace
	glyphDelete    = "\U000F01B4" // nf-md-delete
	glyphUp        = "\U000F005D" // nf-md-arrow_up
	glyphDown      = "\U000F0045" // nf-md-arrow_down
	glyphLeft      = "\U000F004D" // nf-md-arrow_left
	glyphRight     = "\U000F0054" // nf-md-arrow_right
	glyphHome      = "\U000F0600" // nf-md-page_first
	glyphEnd       = "\U000F0601" // nf-md-page_last
	glyphPageUp    = "\U000F013F" // nf-md-chevron_double_up
	glyphPageDown  = "\U000F013C" // nf-md-chevron_double_down

	glyphCtrl  = "\u2303" // ⌃
	glyphShift = "\u21E7" // ⇧
	glyphAlt   = "\u2325" // ⌥
	glyphCmd   = "\u2318" // ⌘
)

// keyGlyphs maps a key token to its glyph. Lookup is by lowercased token.
var keyGlyphs = map[string]string{
	"enter":     glyphEnter,
	"escape":    glyphEscape,
	"esc":       glyphEscape,
	"tab":       glyphTab,
	"space":     glyphSpace,
	"backspace": glyphBackspace,
	"delete":    glyphDelete,
	"del":       glyphDelete,
	"up":        glyphUp,
	"down":      glyphDown,
	"left":      glyphLeft,
	"right":     glyphRight,
	"home":      glyphHome,
	"end":       glyphEnd,
	"pgup":      glyphPageUp,
	"pageup":    glyphPageUp,
	"pgdn":      glyphPageDown,
	"pagedown":  glyphPageDown,
}

// modifierGlyphs maps a modifier token to its symbol. Lookup is by lowercased
// token.
var modifierGlyphs = map[string]string{
	"ctrl":    glyphCtrl,
	"control": glyphCtrl,
	"shift":   glyphShift,
	"alt":     glyphAlt,
	"opt":     glyphAlt,
	"option":  glyphAlt,
	"cmd":     glyphCmd,
	"command": glyphCmd,
	"meta":    glyphCmd,
	"super":   glyphCmd,
}

// modifierPattern matches a modifier name immediately followed by "+", so the
// separator can be dropped: "ctrl+enter" renders as "⌃" and not "⌃+". Longer
// names are listed first so "option+" is not matched as "opt" plus a stray "ion".
var modifierPattern = regexp.MustCompile(`(?i)\b(control|ctrl|shift|option|opt|command|cmd|meta|super|alt)\+`)

// keyPattern matches whole-word key tokens. The trailing word boundary is what
// disambiguates "esc" from "escape" and "del" from "delete" (after "esc" comes
// "a", a word character, so \b fails); the longer alternatives are listed first
// only for readability. The boundaries keep substrings such as "Editor",
// "entered", "escalate" or "Update" untouched.
//
// f-keys are deliberately absent: the keybinds pane compresses a contiguous run
// into a single "f1-f9" token, which would render as nine glyphs jammed
// together, and the help modal lists those keys individually. Mapping them would
// also make the two surfaces disagree.
var keyPattern = regexp.MustCompile(`(?i)\b(escape|esc|enter|tab|space|backspace|delete|del|up|down|left|right|home|end|pgup|pageup|pgdn|pagedown)\b`)

// nerdFont is the process-wide glyph switch, defaulting to on. The app sets it
// from ui.nerd_font before any view renders.
var nerdFont = true

// SetNerdFont toggles glyph substitution for key hints and returns the previous
// value, so callers (notably tests) can restore the prior state.
func SetNerdFont(on bool) bool {
	prev := nerdFont
	nerdFont = on
	return prev
}

// NerdFont reports whether glyph substitution is enabled.
func NerdFont() bool { return nerdFont }

// Key renders a key-hint string. With glyphs disabled it returns s unchanged.
// Otherwise modifier combinations collapse to their symbol and whole-word key
// tokens become their glyph, preserving all other characters.
func Key(s string) string {
	if !nerdFont {
		return s
	}
	s = modifierPattern.ReplaceAllStringFunc(s, func(match string) string {
		return modifierGlyphs[strings.ToLower(strings.TrimSuffix(match, "+"))]
	})
	return keyPattern.ReplaceAllStringFunc(s, func(match string) string {
		return keyGlyphs[strings.ToLower(match)]
	})
}
