package explorerpreview

// Scenario: La barra de pestañas es un anillo, y el anillo no se rompe.
//
// The tab bar was exercised only through the panel that owns it, which always drove it
// with a valid index and never asked what happens at the ends. Two of its four methods
// are a wrap, and a wrap is the kind of thing that is written correctly once and then
// "simplified" into a clamp by someone who did not notice — at which point tabbing
// forward from the last tab silently stops.
//
// So the ring is walked all the way round and the count is checked at every step, which
// is the only way to catch a wrap that wraps to the wrong place. And the out-of-range
// cases are asserted, because SetActive is called with values derived from elsewhere in
// the app and silently ignoring them is a decision worth pinning rather than inheriting.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

func newTabBar(t *testing.T) *TabBar {
	t.Helper()
	return NewTabBar(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
}

// tabNames is the rendered label of every tab, in order.
func tabNames(tb *TabBar) []string {
	var out []string
	for _, tab := range tb.tabs {
		out = append(out, tab.ID)
	}
	return out
}

func TestTheTabBarCyclesThroughEveryTabAndBack(t *testing.T) {
	tb := newTabBar(t)
	n := tb.TabCount()
	if n < 2 {
		t.Fatalf("the tab bar has %d tabs; a ring needs at least two", n)
	}

	t.Run("forward visits every tab once and returns to the first", func(t *testing.T) {
		seen := map[string]int{}
		start := tb.ActiveIndex()
		first := tb.ActiveID()

		for range n {
			seen[tb.ActiveID()]++
			tb.NextTab()
		}

		if len(seen) != n {
			t.Errorf("forward from tab %d visited %d distinct tabs in %d steps, want %d", start, len(seen), n, n)
		}
		for id, times := range seen {
			if times != 1 {
				t.Errorf("tab %q was visited %d times in one lap, want once", id, times)
			}
		}
		if got := tb.ActiveID(); got != first {
			t.Errorf("after one lap the active tab is %q, want %q", got, first)
		}
		if got := tb.ActiveIndex(); got != start {
			t.Errorf("after one lap the index is %d, want %d", got, start)
		}
	})

	t.Run("backward visits every tab once and returns to the first", func(t *testing.T) {
		start := tb.ActiveIndex()
		first := tb.ActiveID()

		seen := map[string]int{}
		for range n {
			seen[tb.ActiveID()]++
			tb.PrevTab()
		}

		if len(seen) != n {
			t.Errorf("backward visited %d distinct tabs in %d steps, want %d", len(seen), n, n)
		}
		if got := tb.ActiveID(); got != first {
			t.Errorf("after one lap backwards the active tab is %q, want %q", got, first)
		}
		if got := tb.ActiveIndex(); got != start {
			t.Errorf("after one lap backwards the index is %d, want %d", got, start)
		}
	})

	t.Run("forward and backward are inverses", func(t *testing.T) {
		tb2 := newTabBar(t)
		tb2.SetActive(3)
		before := tb2.ActiveID()
		tb2.NextTab()
		tb2.PrevTab()
		if got := tb2.ActiveID(); got != before {
			t.Errorf("NextTab then PrevTab left %q, want %q", got, before)
		}
		tb2.PrevTab()
		tb2.NextTab()
		if got := tb2.ActiveID(); got != before {
			t.Errorf("PrevTab then NextTab left %q, want %q", got, before)
		}
	})

	t.Run("the tab ids are unique", func(t *testing.T) {
		// ActiveID resolves an index to an id, so duplicate ids make two indexes
		// indistinguishable to everything downstream — the panel switches content by id.
		seen := map[string]bool{}
		for _, id := range tabNames(tb) {
			if seen[id] {
				t.Errorf("tab id %q appears twice", id)
			}
			seen[id] = true
		}
		if len(seen) != n {
			t.Errorf("there are %d tabs but %d distinct ids", n, len(seen))
		}
	})
}

// Scenario: Un indice fuera de rango se ignora, y ActiveID no inventa nada.
func TestOutOfRangeTabIndexesAreIgnored(t *testing.T) {
	tb := newTabBar(t)
	n := tb.TabCount()
	tb.SetActive(2)
	good := tb.ActiveID()

	for _, bad := range []int{-1, -100, n, n + 1, 1000} {
		tb.SetActive(bad)
		if got := tb.ActiveIndex(); got != 2 {
			t.Errorf("SetActive(%d) moved the index to %d, want it left at 2", bad, got)
		}
		if got := tb.ActiveID(); got != good {
			t.Errorf("SetActive(%d) changed the active tab to %q, want %q", bad, got, good)
		}
	}

	t.Run("every index inside the range is accepted", func(t *testing.T) {
		for i := range n {
			tb.SetActive(i)
			if tb.ActiveIndex() != i {
				t.Errorf("SetActive(%d) left the index at %d", i, tb.ActiveIndex())
			}
			if tb.ActiveID() == "" {
				t.Errorf("SetActive(%d) left the active id empty", i)
			}
		}
	})

	t.Run("an index past the end has no active id", func(t *testing.T) {
		// ActiveID re-checks the bound rather than trusting SetActive, so a struct built
		// directly with an out-of-range active (which nothing does today) reads as "no
		// tab" instead of panicking.
		bare := &TabBar{tabs: tb.tabs, active: n}
		if got := bare.ActiveID(); got != "" {
			t.Errorf("ActiveID with an index of %d is %q, want empty", n, got)
		}
		negative := &TabBar{tabs: tb.tabs, active: -1}
		if got := negative.ActiveID(); got != "" {
			t.Errorf("ActiveID with an index of -1 is %q, want empty", got)
		}
	})
}

// Scenario: El texto de cada pestaña sale de los keybinds, no de literales.
func TestTabKeysComeFromTheRegistry(t *testing.T) {
	t.Run("with no registry the keys are empty and the labels still render", func(t *testing.T) {
		// NewTabBar takes a Resolver interface, so nil is legal. The label then has an
		// empty bracket, which is ugly but better than a crash on the first render.
		tb := NewTabBar(theme.Resolve("dark").Styles(), nil)
		out := tb.Render()
		if out == "" {
			t.Fatal("the tab bar rendered nothing with no registry")
		}
		for _, tab := range tb.tabs {
			if tab.Key != "" {
				t.Errorf("tab %q has key %q with no registry", tab.ID, tab.Key)
			}
			if !strings.Contains(out, tab.Label) {
				t.Errorf("tab %q does not appear in the render:\n%s", tab.Label, out)
			}
		}
	})

	t.Run("a REBOUND key shows up in the label", func(t *testing.T) {
		kb := config.NewKeybindRegistry(config.KeybindingsConfig{
			Custom: map[string]string{"columns_tab": "C"},
		})
		tb := NewTabBar(theme.Resolve("dark").Styles(), kb)

		var found bool
		for _, tab := range tb.tabs {
			if tab.ID == "columns" {
				found = true
				if tab.Key != "C" {
					t.Errorf("the columns tab shows the key %q, want the rebound C", tab.Key)
				}
			}
		}
		if !found {
			t.Fatal("there is no columns tab; the test needs one to rebind")
		}
		if !strings.Contains(stripEsc(tb.Render()), "Columns [C]") {
			t.Errorf("the render does not show the rebound key:\n%s", stripEsc(tb.Render()))
		}
	})
}

// Scenario: Solo la pestaña activa se ve distinta.
func TestOnlyTheActiveTabIsHighlighted(t *testing.T) {
	tb := newTabBar(t)

	// One render with tab 1 active, and the two styles read off it: the style on the
	// active tab and the style on an inactive one. Compared on the escape sequences,
	// because in stripped text every tab looks identical — which is why this assertion
	// cannot be made any other way.
	//
	// The first version built the two styles from two DIFFERENT renders and then read
	// them back with the branches crossed, so it compared the active tab's style against
	// itself and reported that nothing was ever highlighted.
	tb.SetActive(1)
	activeStyle := tabSgr(tb.Render(), 1, tb)
	inactiveStyle := tabSgr(tb.Render(), 0, tb)
	if activeStyle == inactiveStyle {
		t.Fatalf("an active tab and an inactive one are styled identically: %q", activeStyle)
	}
	if activeStyle == "" || inactiveStyle == "" {
		t.Fatalf("no styling was found: active %q inactive %q", activeStyle, inactiveStyle)
	}

	// Exactly one tab carries the active style, and it is the active one. "The active
	// tab is special" and "exactly one tab is special" are different claims, and only
	// the second is what the user sees.
	for i := range tb.TabCount() {
		want := inactiveStyle
		if i == 1 {
			want = activeStyle
		}
		if got := tabSgr(tb.Render(), i, tb); got != want {
			t.Errorf("tab %d (%s) is styled %q, want %q", i, tb.tabs[i].ID, got, want)
		}
	}

	t.Run("moving the highlight moves the style with it", func(t *testing.T) {
		for i := range tb.TabCount() {
			tb.SetActive(i)
			if got := tabSgr(tb.Render(), i, tb); got != activeStyle {
				t.Errorf("with tab %d active it is styled %q, want %q", i, got, activeStyle)
			}
		}
	})
}

// tabSgr is the escape sequence in effect immediately before the label of the tab at
// index i, in the render the bar produces AS IT IS.
//
// It must not change the active index. The first version did — it set active = i before
// reading, so by construction every index returned the ACTIVE style and the test
// concluded that nothing is ever highlighted. Which was true of its own probe.
//
// Found by locating the label in the raw render and walking BACK to the last SGR
// terminator, because the sequences and the tab boundaries do not line up: the tabs are
// joined by a separator that is styled independently.
func tabSgr(rendered string, i int, tb *TabBar) string {
	if i < 0 || i >= len(tb.tabs) {
		return ""
	}
	label := tb.tabs[i].Label
	at := strings.Index(rendered, label)
	if at < 0 {
		return ""
	}
	before := rendered[:at]
	end := strings.LastIndex(before, "m")
	if end < 0 {
		return ""
	}
	start := strings.LastIndex(before[:end], "\x1b")
	if start < 0 {
		return ""
	}
	return before[start : end+1]
}

func TestTabLabelsAreNotEmpty(t *testing.T) {
	tb := newTabBar(t)
	for _, tab := range tb.tabs {
		if tab.Label == "" {
			t.Errorf("tab %q has no label", tab.ID)
		}
		if tab.ID == "" {
			t.Error("a tab has no id; ActiveID would resolve to something the panel cannot switch on")
		}
	}
	if got := lipgloss.Width(tb.Render()); got == 0 {
		t.Error("the render is zero columns wide")
	}
}

// stripEsc removes the SGR sequences.
func stripEsc(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case in:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
