package ui

// Scenario: El modal de ayuda en los tres casos que el camino feliz no toca.
//
// Everything here is a guard with an observable consequence and no test:
//
//	Update's final fallthrough — the difference between "the modal used this message" and
//	  "the modal has no opinion, so the app should keep looking". A fallthrough instead of
//	  a stop is how a key that closes a modal also moves a table.
//
//	the minimum-height clamp — a window too short for the content, where the clamp is the
//	  only thing keeping a slice index in range
//
//	the empty-key placeholder — an action bound to no key at all, which is what a user
//	  sees for an action they can only reach from the palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func helpModalForTest(height int) *HelpModal {
	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.Show()
	m.SetWidth(100)
	m.SetHeight(height)
	return m
}

// The fallthrough. A message that is not a key press reaches it, and the modal must decline
// rather than swallow: the app routes every message through its overlays in order, and an
// overlay that claimed messages it does not understand would freeze the UI behind it.
func TestTheHelpModalDeclinesWhatItDoesNotUnderstand(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"a message that is not a key", struct{ tea.Msg }{}},
		{"a window resize", tea.WindowSizeMsg{Width: 90, Height: 30}},
		{"a mouse move", tea.MouseMotionMsg{X: 4, Y: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := helpModalForTest(40)

			cmd, handled := m.Update(tc.msg)
			if handled {
				t.Errorf("the modal claimed %T", tc.msg)
			}
			if cmd != nil {
				t.Errorf("the modal produced %T for a message it does not understand", cmd())
			}
			if !m.IsVisible() {
				t.Error("the modal closed over a message it does not understand")
			}
		})
	}

	t.Run("an invisible modal declines everything", func(t *testing.T) {
		// The other half of the same guard, and the one that matters more in practice: a
		// hidden modal must be invisible to the router, or every key would be offered to a
		// panel nobody is looking at.
		m := helpModalForTest(40)
		m.Hide()

		if cmd, handled := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); handled || cmd != nil {
			t.Error("a hidden modal claimed a key")
		}
	})
}

// A window shorter than the modal's own chrome. maxVisible has a floor of 5, and the floor
// is the only thing keeping the visible slice non-empty: at a height of 1 the content is
// long past the window, and the scroll arithmetic below indexes against maxVisible.
//
// The close hint is the LAST line of that list, so on a short window it is off screen until
// you scroll — which is the design, not a bug, and the first version of this case asserted
// it was always visible and reported the modal broken at every height.
func TestTheHelpModalSurvivesAWindowShorterThanItsChrome(t *testing.T) {
	for _, height := range []int{0, 1, 5, 10, 14, 15, 40, 200} {
		m := helpModalForTest(height)

		out := m.View()
		if out == "" {
			t.Errorf("at height %d the modal renders nothing", height)
		}

		// Scrolling it must not panic, which is what an unclamped slice would do — and it must
		// stay visible, because a scroll that closed the modal would be a worse failure than
		// an ugly one.
		for range 5 {
			if cmd, handled := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled && cmd != nil {
				_ = cmd()
			}
		}
		if !m.IsVisible() {
			t.Errorf("at height %d scrolling closed the modal", height)
		}
	}

	t.Run("and the close hint is reachable by scrolling", func(t *testing.T) {
		// The property a user actually depends on: whatever the window, the key that closes
		// the modal is findable. Asserted as "reaches it", not "always shows it".
		m := helpModalForTest(20)

		// "close" and not "Esc close": the hint is rendered through keydisplay.Key, which
		// replaces a key name with its NERD FONT glyph — the rendered line contains U+F12B7
		// where "Esc" was. So a search for the literal text finds nothing on any window, and
		// the first version of this case failed at every height for that reason alone. The
		// glyph is the feature; the assertion has to look past it.
		found := strings.Contains(m.View(), "close")
		for range 200 {
			if found {
				break
			}
			if cmd, handled := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled && cmd != nil {
				_ = cmd()
			}
			found = strings.Contains(m.View(), "close")
		}
		if !found {
			t.Error("scrolling to the end never showed the close hint")
		}
	})
}

// An action with no keys. The registry can hold one — a user can rebind an action to
// nothing, and an action whose only route is the palette has no keys by construction — and
// then the rendered line needs SOMETHING in the key column or the description shifts left
// and the pane's alignment is off by one for every row.
func TestAnActionWithNoKeysRendersAPlaceholder(t *testing.T) {
	m := helpModalForTest(40)

	keyless := config.Action{ID: "no_keys", Description: "an action with no keys"}
	got := m.renderAction(keyless)
	if !strings.Contains(got, "?") {
		t.Errorf("a keyless action renders as %q, with no placeholder in the key column", got)
	}
	if !strings.Contains(got, keyless.Description) {
		t.Errorf("a keyless action lost its description: %q", got)
	}

	t.Run("and a keyed one is untouched", func(t *testing.T) {
		// The counterweight: a version that always printed "?" would satisfy the case above.
		keyed := config.Action{ID: "quit", Keys: []string{"q", "ctrl+c"}, Description: "Quit"}
		out := m.renderAction(keyed)
		if strings.Contains(out, "?") {
			t.Errorf("a keyed action rendered a placeholder: %q", out)
		}
		if !strings.Contains(out, "q") {
			t.Errorf("a keyed action did not render its key: %q", out)
		}
	})
}
