package theme

// Scenario: `theme.Resolve` es la PRIMERA cosa que hace cada arranque, y no tenia ni un
// solo test.
//
// NewModel opens with `t := theme.Resolve(cfg.Theme.Mode)`, so every theme the user can
// select was reached for the first time in the terminal they were looking at. A theme with
// a colour field left unset does not fail loudly: lipgloss drops a nil colour and renders
// the text in whatever the terminal's default happens to be, so the symptom is "the grid
// looks slightly wrong on a dark background" — which nobody reports as a bug and everybody
// works around.
//
// The invariant asserted here is therefore not "Resolve returns something" but "EVERY
// colour the theme type can carry is set, in EVERY theme Resolve can hand back". It is
// written over the struct by reflection rather than as a hand-written list, so adding a
// colour to Theme turns this into a failing test until every theme sets it — which is the
// moment the bug would otherwise ship.

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"

	"charm.land/lipgloss/v2"
	"testing"
)

// modes is every mode Resolve accepts by name, plus the ones it does not but still answers.
// The unnamed ones are the interesting half: an unrecognised value is not an error, it is
// a theme, and which one is worth pinning rather than leaving to be discovered by looking
// at a screenshot.
var modes = []string{"dark", "light", "nord", "gruvbox", "catppuccin", "system", "", "bogus", "Dark", "DARK"}

// colourFields is every colour field of Theme, by name.
//
// Names and not values on purpose: a reflect.Value of a pointer's field panics the first
// time somebody forgets Elem(), and the mistake would be in the helper that every
// assertion in this file goes through.
func colourFields(t *testing.T) []string {
	t.Helper()
	tt := reflect.TypeOf(Theme{})
	var names []string
	for i := range tt.NumField() {
		if f := tt.Field(i); f.Type == reflect.TypeFor[color.Color]() {
			names = append(names, f.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("Theme has no colour fields; the invariant this test exists to protect cannot be checked")
	}
	return names
}

func TestResolveNeverReturnsAnIncompleteTheme(t *testing.T) {
	for _, mode := range modes {
		t.Run("mode "+quote(mode), func(t *testing.T) {
			th := Resolve(mode)
			if th == nil {
				t.Fatalf("Resolve(%q) returned nil; NewModel dereferences the result immediately", mode)
			}

			// Every colour set. Checked by field name so the failure says WHICH one,
			// and exhaustively over the struct so a new field is covered without
			// anyone remembering to add it.
			v := reflect.ValueOf(th).Elem()
			for _, name := range colourFields(t) {
				if v.FieldByName(name).IsNil() {
					t.Errorf("the %q theme leaves %s unset; lipgloss renders it as the terminal default, not as an error", th.Name, name)
				}
			}

			// A name, because the theme picker and the settings screen both show it.
			if th.Name == "" {
				t.Error("the theme has no Name")
			}
		})
	}
}

// TestResolveNamesEachModeAfterItself is the assertion that makes the first one meaningful:
// without it, a mode that returned the wrong theme would still pass "every colour is set",
// because every theme sets every colour. The name is what says WHICH theme you got.
func TestResolveNamesEachModeAfterItself(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"dark", "dark"},
		{"light", "light"},
		{"nord", "nord"},
		{"gruvbox", "gruvbox"},
		{"catppuccin", "catppuccin"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			if got := Resolve(tc.mode).Name; got != tc.want {
				t.Errorf("Resolve(%q) is the %q theme", tc.mode, got)
			}
		})
	}
}

// TestResolveIsCaseSensitive pins a decision the code makes implicitly and the settings
// screen cannot express.
//
// A user who writes `theme.mode = "Dark"` in config.toml gets a theme, not an error — and
// not the dark one. This is what the switch actually does, and the test exists so that a
// future "be forgiving" change has to be a decision rather than an accident.
func TestResolveIsCaseSensitive(t *testing.T) {
	for _, mode := range []string{"Dark", "DARK"} {
		if got := Resolve(mode).Name; got == "dark" {
			t.Errorf("Resolve(%q) is the dark theme, so the switch is case-insensitive after all; this test is stale", mode)
		}
	}
}

// TestAnUnknownModeGetsTheSystemTheme pins the fallback, and this one is worth reading
// twice because "system" does not mean what the name suggests.
//
// `detectTerminalColors` takes no arguments and returns two hardcoded hex strings, so the
// system theme is a fixed palette and not a detection of anything. Combined with the
// `default:` arm of Resolve — which returns the same thing — an unknown mode, an empty
// mode, and the literal "system" all produce one identical palette. Asserted as three
// cases with the SAME expected answer on purpose: it documents that the fallback is the
// system palette rather than the dark one a reader would assume from the case order.
func TestAnUnknownModeGetsTheSystemTheme(t *testing.T) {
	want := Resolve("system")
	if want == nil {
		t.Fatal("Resolve(\"system\") returned nil")
	}
	for _, mode := range []string{"system", "", "bogus", "Dark"} {
		t.Run("mode "+quote(mode), func(t *testing.T) {
			got := Resolve(mode)
			if got.Name != want.Name {
				t.Errorf("Resolve(%q) is the %q theme, want %q — the fallback is detectSystem", mode, got.Name, want.Name)
			}
			if !sameColour(got.Background, want.Background) {
				t.Errorf("Resolve(%q) has background %v, want the system's %v", mode, got.Background, want.Background)
			}
		})
	}
}

// TestTheThemesAreActuallyDifferent is a guard against a theme being copy-pasted and
// never changed, which passes every other test in this file: it is complete, it is named
// correctly, and it looks exactly like one of its neighbours.
//
// The one that matters most is light against dark — a user who selects light and gets dark
// has no way to tell that the setting was read, applied and ignored.
func TestTheThemesAreActuallyDifferent(t *testing.T) {
	seen := map[string]string{}
	for _, mode := range []string{"dark", "light", "nord", "gruvbox", "catppuccin"} {
		th := Resolve(mode)
		key := colourKey(th)
		if other, dup := seen[key]; dup {
			t.Errorf("the %q theme has exactly the same colours as the %q theme", mode, other)
		}
		seen[key] = mode
	}

	// Spelled out for light, because "the themes differ" is weaker than "selecting
	// light gives you a light background".
	light := Resolve("light")
	dark := Resolve("dark")
	if !isLight(light.Background) {
		t.Errorf("the light theme's background is %v, which is not lighter than the dark theme's %v",
			light.Background, dark.Background)
	}
}

// TestStylesAreBuiltFromTheTheme closes the loop: a complete theme and a usable Styles are
// different claims, because Styles reads the fields one by one.
func TestStylesAreBuiltFromTheTheme(t *testing.T) {
	for _, mode := range modes {
		t.Run("mode "+quote(mode), func(t *testing.T) {
			th := Resolve(mode)

			// Built from the pointer receiver, as the callers do.
			st := th.Styles()
			if st == nil {
				t.Fatalf("the %q theme produced nil Styles", th.Name)
			}
			// And again, because a method with a value receiver would compile on a
			// copy and silently discard anything it set.
			if NewStyles(th) == nil {
				t.Fatalf("NewStyles(%q) returned nil", th.Name)
			}
		})
	}
}

// TestAnUnsetColourBecomesNoColorAndNotBlack records what a theme with a missing colour
// actually does, because the guess is wrong in the expensive direction.
//
// The intuitive fear is that a nil colour renders as literal black, which on a dark
// background is invisible text. That is not what happens: lipgloss substitutes its
// NoColor sentinel, whose RGBA is 0,0,0 but which the renderer treats as "emit no colour
// code at all". So the fallback is the terminal's own default, and the symptom of a
// half-filled theme is text in whatever the terminal chose — visible, just not the colour
// anyone designed.
//
// Pinned as a test rather than left as a belief, because the two candidates differ only in
// a rendering decision made inside a dependency, and a dependency upgrade is exactly the
// kind of change that would move it without anyone reading this file.
func TestAnUnsetColourBecomesNoColorAndNotBlack(t *testing.T) {
	st := NewStyles(&Theme{Name: "empty"})

	got := st.Border.GetBorderTopForeground()
	if got == nil {
		t.Fatal("an empty theme produced a nil border colour; NewStyles is supposed to substitute NoColor")
	}
	if _, ok := got.(lipgloss.NoColor); !ok {
		t.Errorf("an empty theme produced a %T (%v); lipgloss substitutes NoColor for a nil colour", got, got)
	}

	// And a theme that DOES set the colour gets that exact colour, so the two are
	// distinguishable — otherwise "every field is NoColor" would also be a passing
	// state for a working theme.
	set := NewStyles(&Theme{Name: "set", Border: c("#ff0000")}).Border.GetBorderTopForeground()
	if _, ok := set.(lipgloss.NoColor); ok {
		t.Error("a theme that set its border colour produced NoColor; the colour was dropped")
	}
	r, g, b, a := set.RGBA()
	if r>>8 != 0xff || g>>8 != 0 || b>>8 != 0 || a>>8 != 0xff {
		t.Errorf("the border colour is %d,%d,%d,%d, want pure red at full alpha", r>>8, g>>8, b>>8, a>>8)
	}
}

// ---------------------------------------------------------------------------

// colourKey is every colour of a theme in one comparable string, so "identical themes"
// is one map lookup instead of twenty comparisons.
func colourKey(t *Theme) string {
	v := reflect.ValueOf(*t)
	tt := v.Type()
	var b strings.Builder
	for i := range tt.NumField() {
		f := tt.Field(i)
		if f.Type == reflect.TypeFor[color.Color]() {
			b.WriteString(f.Name)
			b.WriteString("=")
			field := v.Field(i)
			if field.IsNil() {
				b.WriteString("nil")
			} else {
				b.WriteString(colourKeyOf(field.Interface().(color.Color)))
			}
			b.WriteString(";")
		}
	}
	return b.String()
}

// colourKeyOf renders a colour as a stable string. Colours come from lipgloss.Color,
// which is an RGBColor, so the concrete type is reached through an interface assertion
// rather than a type switch on the concrete value — the interface is all Resolve's
// signature promises.
func colourKeyOf(c color.Color) string {
	if c == nil {
		return "nil"
	}
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("%04x%04x%04x%04x", r, g, b, a)
}

func sameColour(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// isLight is a crude luminance test, chosen so that it agrees with a human on the palette
// this project ships without needing the colour-science dependency. Rec. 601 luma on
// gamma-encoded values is good enough to separate "dark" from "light" and nothing subtler
// is being claimed.
func isLight(c color.Color) bool {
	if c == nil {
		return false
	}
	r, g, b, _ := c.RGBA()
	// RGBA returns alpha-premultiplied 16-bit values, so divide back to 8 bits.
	rr, gg, bb := float64(r>>8), float64(g>>8), float64(b>>8)
	return 0.299*rr+0.587*gg+0.114*bb > 127
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	return s
}
