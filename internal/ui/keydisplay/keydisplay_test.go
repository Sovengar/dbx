package keydisplay

import "testing"

func TestKey_OffIsIdentity(t *testing.T) {
	prev := SetNerdFont(false)
	defer SetNerdFont(prev)

	for _, s := range []string{
		"",
		"enter",
		"esc",
		"escape",
		"tab",
		"space",
		"backspace",
		"ctrl+enter",
		"ctrl+left",
		"Enter/Tab",
		"j/k ↑↓   enter copy   esc cancel",
	} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) with nerd font off = %q, want identity", s, got)
		}
	}
}

func TestKey_OnMapsKeyTokens(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	cases := map[string]string{
		"enter":     glyphEnter,
		"Enter":     glyphEnter,
		"ENTER":     glyphEnter,
		"esc":       glyphEscape,
		"Esc":       glyphEscape,
		"ESC":       glyphEscape,
		"escape":    glyphEscape,
		"Escape":    glyphEscape,
		"tab":       glyphTab,
		"Tab":       glyphTab,
		"space":     glyphSpace,
		"Space":     glyphSpace,
		"backspace": glyphBackspace,
		"Backspace": glyphBackspace,
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
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

// A modifier consumes its "+" so the hint reads as a symbol chain rather than
// "⌃+".
func TestKey_OnCollapsesModifiers(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	cases := map[string]string{
		"ctrl+d":             glyphCtrl + "d",
		"ctrl+enter":         glyphCtrl + glyphEnter,
		"ctrl+space":         glyphCtrl + glyphSpace,
		"ctrl+left":          glyphCtrl + glyphLeft,
		"ctrl+right":         glyphCtrl + glyphRight,
		"ctrl+u/ctrl+d":      glyphCtrl + "u/" + glyphCtrl + "d",
		"ctrl+enter, ctrl+r": glyphCtrl + glyphEnter + ", " + glyphCtrl + "r",
		"shift+tab":          glyphShift + glyphTab,
		"alt+f4":             glyphAlt + "f4",
		"option+x":           glyphAlt + "x",
		"opt+x":              glyphAlt + "x",
		"cmd+k":              glyphCmd + "k",
		"command+k":          glyphCmd + "k",
		"meta+k":             glyphCmd + "k",
		"super+k":            glyphCmd + "k",
		"CTRL+D":             glyphCtrl + "D",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

// A bare modifier is left alone: only the "modifier+key" shape is rewritten, so
// a stray "ctrl" token cannot silently become a symbol.
func TestKey_BareModifierUntouched(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	for _, s := range []string{"ctrl", "shift", "alt", "cmd"} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) = %q, want unchanged", s, got)
		}
	}
}

// Words that merely contain a token must survive: the boundary anchors are what
// keep "esc" out of "escapee" and "tab" out of "tabled".
func TestKey_NoSubstringFalsePositives(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	for _, s := range []string{
		"Editor", "entered", "escalate", "selection", "ESCALATE",
		"Update", "Uptime", "leftmost", "righteous", "Deleted",
		"escapee", "spaceship", "tabs", "tabled", "pgupdate",
	} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) = %q, want unchanged", s, got)
		}
	}
}

// Documented hazard: a whole-word occurrence is rewritten even inside prose.
// This is why action descriptions are never routed through Key — "Delete Row(s)"
// and "Expand FK / Collapse" are descriptions, not key hints. If a future render
// site passes prose here, it will be mangled, and this test is the tripwire.
func TestKey_WholeWordProseIsRewritten(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	got := Key("Delete Row(s)")
	want := glyphDelete + " Row(s)"
	if got != want {
		t.Errorf("Key(%q) = %q, want %q (prose hazard is expected)", "Delete Row(s)", got, want)
	}
	if got == "Delete Row(s)" {
		t.Error("prose is no longer rewritten; the package doc hazard note is stale")
	}
}

// f-keys are intentionally not mapped: the pane compresses "f1".."f9" into the
// single token "f1-f9", and substituting each would jam nine glyphs together.
func TestKey_FKeysUntouched(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	for _, s := range []string{"f1", "f9", "f1-f9", "f1-f12"} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) = %q, want unchanged", s, got)
		}
	}
}

// The footer literals actually handed to Key by the render sites must survive
// substitution without collateral damage.
func TestKey_RealFooterLiterals(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	cases := map[string]string{
		"  esc cancel": "  " + glyphEscape + " cancel",
		"  r retry  ·  esc connections  ·  q quit": "  r retry  ·  " +
			glyphEscape + " connections  ·  q quit",
		" Enter send · Esc close": " " + glyphEnter + " send · " +
			glyphEscape + " close",
		"j/k ↑↓   enter copy   esc cancel": "j/k ↑↓   " + glyphEnter +
			" copy   " + glyphEscape + " cancel",
		"j/k ↑↓   space toggle   enter select   q quit": "j/k ↑↓   " +
			glyphSpace + " toggle   " + glyphEnter + " select   q quit",
		" j/k navigate · Enter load · f favorite · d delete · / filter · Tab switch · Esc close": " j/k navigate · " +
			glyphEnter + " load · f favorite · d " + glyphDelete +
			" · / filter · " + glyphTab + " switch · " + glyphEscape + " close",
		"  j/k scroll · Esc close": "  j/k scroll · " + glyphEscape + " close",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

// Panel key texts come from the registry's key field, including grouped and
// aliased forms.
func TestKey_RealPanelKeyTexts(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	cases := map[string]string{
		"enter":             glyphEnter,
		"esc":               glyphEscape,
		"tab":               glyphTab,
		"space":             glyphSpace,
		"backspace/h":       glyphBackspace + "/h",
		"tab/esc":           glyphTab + "/" + glyphEscape,
		"h/left":            "h/" + glyphLeft,
		"j/down":            "j/" + glyphDown,
		"k/up":              "k/" + glyphUp,
		"l/right":           "l/" + glyphRight,
		"n/]/ctrl+right":    "n/]/" + glyphCtrl + glyphRight,
		"p/[/ctrl+left":     "p/[/" + glyphCtrl + glyphLeft,
		"q/ctrl+c":          "q/" + glyphCtrl + "c",
		"ctrl+enter/ctrl+r": glyphCtrl + glyphEnter + "/" + glyphCtrl + "r",
		"ctrl+u/ctrl+d":     glyphCtrl + "u/" + glyphCtrl + "d",
		"g/G":               "g/G",
		"j/k":               "j/k",
		"hjkl":              "hjkl",
		"f1-f9":             "f1-f9",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNerdFont_DefaultAndToggle(t *testing.T) {
	prev := SetNerdFont(true)
	defer SetNerdFont(prev)

	if !NerdFont() {
		t.Fatal("NerdFont() = false after SetNerdFont(true)")
	}
	if got := SetNerdFont(false); !got {
		t.Fatal("SetNerdFont(false) returned false, want the previous value true")
	}
	if NerdFont() {
		t.Fatal("NerdFont() = true after SetNerdFont(false)")
	}
}
