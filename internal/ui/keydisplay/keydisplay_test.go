package keydisplay

import "testing"

func TestKey_OffIsIdentity(t *testing.T) {
	SetNerdFont(false)
	defer SetNerdFont(true)

	for _, s := range []string{
		"",
		"enter",
		"esc",
		"escape",
		"Enter/Tab",
		"j/k ↑↓   enter copy   esc cancel",
	} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) with nerd font off = %q, want identity", s, got)
		}
	}
}

func TestKey_OnMapsEnterAndEsc(t *testing.T) {
	SetNerdFont(true)

	cases := map[string]string{
		"enter":  glyphEnter,
		"Enter":  glyphEnter,
		"ENTER":  glyphEnter,
		"esc":    glyphEscape,
		"Esc":    glyphEscape,
		"ESC":    glyphEscape,
		"escape": glyphEscape,
		"Escape": glyphEscape,
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKey_NoFalsePositives(t *testing.T) {
	SetNerdFont(true)

	for _, s := range []string{"Editor", "entered", "escalate", "selection", "ESCALATE"} {
		if got := Key(s); got != s {
			t.Errorf("Key(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestKey_CombinedTokens(t *testing.T) {
	SetNerdFont(true)

	cases := map[string]string{
		"Enter/Tab": glyphEnter + "/Tab",
		"j/k ↑↓   enter copy   esc cancel": "j/k ↑↓   " + glyphEnter +
			" copy   " + glyphEscape + " cancel",
		" j/k navigate · Enter load · Esc close": " j/k navigate · " + glyphEnter +
			" load · " + glyphEscape + " close",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNerdFont_DefaultAndToggle(t *testing.T) {
	SetNerdFont(true)
	if !NerdFont() {
		t.Fatal("NerdFont() = false after SetNerdFont(true)")
	}
	SetNerdFont(false)
	if NerdFont() {
		t.Fatal("NerdFont() = true after SetNerdFont(false)")
	}
	SetNerdFont(true)
}
