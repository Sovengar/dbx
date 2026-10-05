package ask

// Scenario: El panel ASK y los mensajes que no le pertenecen.
//
// Update has one switch and a fallthrough, and the fallthrough is the half that matters:
// the app routes every message through its overlays in order, so an overlay that claimed a
// message it does not understand would freeze the panel behind it. Nothing in the package
// tested it, because nothing sent the panel a message that was not a keypress.
//
// View's size arithmetic is in the other file's history: it had two floors for one quantity,
// and the second was unreachable. What is left here is the property that survives.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

func askForTest() *Ask {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(80)
	a.SetHeight(30)
	a.Show()
	return a
}

func TestTheAskPanelDeclinesWhatItDoesNotUnderstand(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"a window resize", tea.WindowSizeMsg{Width: 100, Height: 40}},
		{"a mouse click", tea.MouseClickMsg{X: 3, Y: 3}},
		{"a message that is nothing in particular", struct{ tea.Msg }{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := askForTest()

			cmd, handled := a.Update(tc.msg)
			if handled {
				t.Errorf("the panel claimed %T", tc.msg)
			}
			if cmd != nil {
				t.Errorf("the panel produced %T for a message it does not understand", cmd())
			}
			if !a.IsVisible() {
				t.Error("the panel closed over a message it does not understand")
			}
		})
	}

	t.Run("a hidden panel declines a key too", func(t *testing.T) {
		// The other half, and the one that matters in practice: a closed ASK panel must be
		// invisible to the router, or every keystroke would be offered to a panel nobody is
		// looking at.
		a := askForTest()
		a.Hide()

		if cmd, handled := a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); handled || cmd != nil {
			t.Error("a hidden panel claimed a key")
		}
	})

	t.Run("a visible panel DOES claim a key", func(t *testing.T) {
		// The counterweight: an Update that always declined would satisfy both cases above
		// and would make the panel unusable.
		a := askForTest()

		if _, handled := a.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); !handled {
			t.Error("a visible panel declined j")
		}
	})
}

// A window too small for the panel's own chrome. modalH has a floor of 10 and innerH is
// derived from it, so the interior is never negative — the arithmetic the removed second
// floor used to guard.
func TestTheAskPanelSurvivesAWindowShorterThanItsChrome(t *testing.T) {
	for _, height := range []int{0, 1, 5, 12, 25} {
		a := askForTest()
		a.SetHeight(height)

		out := a.View()
		if out == "" {
			t.Errorf("at height %d the panel renders nothing", height)
		}
		// The title is what makes it recognisable as a panel rather than a stray border.
		if !strings.Contains(out, "ASK") {
			t.Errorf("at height %d the panel does not show its title:\n%q", height, out)
		}
	}
}
