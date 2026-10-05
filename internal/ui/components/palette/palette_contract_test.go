package palette

// The contracts of the command palette, asserted as properties over states rather than
// one scenario at a time.
//
// The palette is a self-contained widget: it takes a theme and a keybind resolver, and its
// whole behaviour is "what does it offer for what I have typed, and what happens when I
// press a key". There is no I/O and no app wiring, and the in-package test file reaches
// every field, which is why this file drives handleKey through Update and asserts on the
// state it leaves rather than only on the rendered string.
//
// Four things it promises:
//
//	P1  it only reacts while it is OPEN. A closed palette is invisible to keys, which is
//	    what lets the grid keep them.
//	P2  typing filters, and the filter is a RUNE operation. Every key that edits the
//	    query has to treat the query as text rather than as bytes, because the query is
//	    whatever the user typed and that includes accents.
//	P3  the selection is a position in the FILTERED list, and it stays inside it however
//	    the filter changes.
//	P4  what is rendered is grouped by section in a fixed order, and the cursor is always
//	    on a row that was actually drawn.

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// newPalette builds an open palette at a usable size. Open, because every test here is
// about what it does while it is up; the closed case is its own test.
func newPalette(t *testing.T) *Palette {
	t.Helper()
	p := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	p.SetWidth(100)
	p.SetHeight(30)
	p.Show()
	return p
}

// typeKey sends one key through the public entry point, so the test exercises the same
// path the app uses rather than reaching past Update into handleKey.
func typeKey(t *testing.T, p *Palette, s string) {
	t.Helper()
	msg := tea.KeyPressMsg{Code: keyCode(s), Text: textOf(s)}
	if s == "ctrl+u" {
		msg.Mod = tea.ModCtrl
	}
	p.Update(msg)
}

// runeKey sends a key whose TEXT is a multi-byte rune, which is what a real terminal
// delivers for an accented character and what a bare byte-oriented test never produces.
func runeKey(t *testing.T, p *Palette, text string) {
	t.Helper()
	r := []rune(text)
	if len(r) == 0 {
		t.Fatalf("runeKey called with an empty string")
	}
	p.Update(tea.KeyPressMsg{Code: r[0], Text: text})
}

func keyCode(name string) rune {
	switch name {
	case "esc":
		return tea.KeyEscape
	case "enter":
		return tea.KeyEnter
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "tab":
		return tea.KeyTab
	case "backspace":
		return tea.KeyBackspace
	case "ctrl+u":
		return 'u' // with Mod set below; there is no single constant for it
	case "space":
		return tea.KeySpace
	}
	return 'x'
}

func textOf(name string) string {
	switch name {
	case "space":
		return " "
	case "up", "down", "tab":
		return ""
	}
	return name
}

// labels is the visible command names, in render order, which is what the tests assert
// on: the NAMES are the contract, the styling is decoration.
func (p *Palette) labels() []string {
	out := []string{}
	for _, sc := range p.filtered {
		out = append(out, sc.command.Name)
	}
	return out
}

func (p *Palette) labelAt(i int) string {
	if i < 0 || i >= len(p.filtered) {
		return "<out of range>"
	}
	return p.filtered[i].command.Name
}

// ---------------------------------------------------------------------------
// P1: the palette is invisible to keys while it is closed
// ---------------------------------------------------------------------------

// Scenario: Cerrada, la paleta NO se come ninguna tecla.
//
// The palette is an overlay: while it is closed every key has to reach the grid, editor
// or explorer underneath it. Swallowing them would make the app feel dead rather than
// wrong, which is why this is checked for the whole key set and not for one key.
func TestPalette_AClosedPaletteSwallowsNothing(t *testing.T) {
	for _, key := range []string{"a", "z", "esc", "enter", "up", "down", "tab", "backspace", "space", "ctrl+u"} {
		t.Run(key, func(t *testing.T) {
			p := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
			p.SetWidth(100)
			p.SetHeight(30)
			if p.IsVisible() {
				t.Fatal("a palette that was never shown reports itself visible")
			}
			if got := p.View(); got != "" {
				t.Errorf("a closed palette rendered %q, want the empty string", got)
			}

			p.Update(tea.KeyPressMsg{Code: keyCode(key), Text: textOf(key)})

			if p.IsVisible() {
				t.Errorf("the key %q opened a closed palette", key)
			}
			if p.query != "" {
				t.Errorf("the key %q wrote %q into a closed palette's query", key, p.query)
			}
			if got := p.View(); got != "" {
				t.Errorf("after %q a closed palette rendered %q", key, got)
			}
		})
	}
}

// Scenario: Abrirla es un reset, y cerrarla la deja cerrada.
//
// Show has to forget the previous session: a palette that remembered its query would
// open already filtered, and one that remembered its cursor would open scrolled. Both are
// asserted from the same state so neither can be fixed without the other.
func TestPalette_OpeningStartsFromScratchAndClosingEndsTheSession(t *testing.T) {
	p := newPalette(t)

	// Dirty it: a query, a moved cursor.
	for _, k := range []string{"r", "e", "f"} {
		typeKey(t, p, k)
	}
	typeKey(t, p, "down")
	if p.query == "" || p.cursor == 0 {
		t.Fatalf("the fixture did not get dirty: query=%q cursor=%d", p.query, p.cursor)
	}

	p.Show()
	if p.query != "" {
		t.Errorf("Show left the previous query %q in place", p.query)
	}
	if p.cursor != 0 {
		t.Errorf("Show left the cursor at %d, want 0", p.cursor)
	}
	if !p.IsVisible() {
		t.Error("Show did not make the palette visible")
	}
	if got := len(p.labels()); got != len(p.commands) {
		t.Errorf("an empty query offered %d commands, want all %d", got, len(p.commands))
	}

	// And closing forgets the query too, so a second session cannot inherit it.
	typeKey(t, p, "e")
	p.Hide()
	if p.IsVisible() {
		t.Error("Hide did not make the palette invisible")
	}
	if p.query != "" {
		t.Errorf("Hide left the query %q behind", p.query)
	}

	// Escape closes it from the keyboard, which is the path a user takes.
	p.Show()
	_, handled := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	if !handled {
		t.Error("escape was not reported as handled")
	}
	if p.IsVisible() {
		t.Error("escape did not close the palette")
	}
}

// ---------------------------------------------------------------------------
// P2: the query is text, not bytes
// ---------------------------------------------------------------------------

// Scenario: Backspace quita UN CARACTER, no un byte.
//
// The query is whatever the user typed, and a terminal delivers an accented character as
// one keystroke carrying several bytes. A backspace that slices one byte off leaves half
// a rune behind: the line then renders as a replacement character, and the filter is
// matching against bytes that no longer spell anything.
//
// This is the same defect the grid's cell editor and the ask prompt both had, found a
// third time here by the same shape: a byte length guarding a rune-indexed string.
func TestPalette_BackspaceRemovesOneWholeCharacter(t *testing.T) {
	for _, tc := range []struct {
		name       string
		typeIt     []string // one entry per keystroke, in order
		backspaces int
		want       string
	}{
		{"an ascii letter", []string{"a"}, 1, ""},
		{"two ascii letters", []string{"r", "e"}, 1, "r"},
		{"a two-byte accent", []string{"é"}, 1, ""},
		{"a two-byte accent after ascii", []string{"r", "é"}, 1, "r"},
		{"an accent between ascii", []string{"a", "é", "b"}, 1, "aé"},
		{"a three-byte CJK character", []string{"表"}, 1, ""},
		{"a CJK character after ascii", []string{"r", "表"}, 1, "r"},
		{"a CJK character between ascii", []string{"a", "表", "b"}, 1, "a表"},
		{"an emoji, which is four bytes and two runes", []string{"🙂"}, 1, ""},
		{"backspace on an empty query is a no-op", nil, 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPalette(t)
			for _, ch := range tc.typeIt {
				runeKey(t, p, ch)
			}
			for range tc.backspaces {
				typeKey(t, p, "backspace")
			}
			if p.query != tc.want {
				t.Errorf("after typing %q and %d backspaces the query is %q (valid UTF-8: %v), want %q",
					strings.Join(tc.typeIt, ""), tc.backspaces, p.query, isValidUTF8(p.query), tc.want)
			}
			// And the rendered line must not carry a replacement character,
			// which is how half a rune reaches the screen.
			if strings.ContainsRune(p.View(), '�') {
				t.Errorf("the palette rendered a replacement character for the query %q", p.query)
			}
		})
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' && !strings.Contains(s, "�") {
			return false
		}
	}
	return !strings.Contains(s, "\x80") && !strings.Contains(s, "\xc0") &&
		!strings.Contains(s, "\xf0\x9f") && len(s) == len([]rune(s)) && s != ""
}

// Scenario: Escribir, borrar y vaciar la linea.
//
// The three editing keys, asserted together because they share the query and each of them
// can be right while the others are wrong. ctrl+u is the coarse one and backspace the
// fine one, and a line that only supports one of them is a trap.
func TestPalette_TypingFilteringAndClearingTheQuery(t *testing.T) {
	t.Run("typing narrows the list", func(t *testing.T) {
		p := newPalette(t)
		all := len(p.labels())

		typeKey(t, p, "r")
		got := len(p.labels())
		if got >= all {
			t.Errorf("typing %q left %d of %d commands: it filtered nothing", "r", got, all)
		}
		if got == 0 {
			t.Fatalf("typing %q matched nothing at all", "r")
		}
		// A label that does not contain the query can still survive, because
		// the ALIAS carries the keybinding — "copy (ctrl+y)" contains an "r".
		// That is why typing one letter barely narrows the list, and it is
		// pinned here so a change to how aliases are built shows up as a
		// change to what a search returns rather than as a mystery.
		for _, sc := range p.filtered {
			inName := strings.Contains(strings.ToLower(sc.command.Name), "r")
			inAlias := strings.Contains(strings.ToLower(sc.command.Alias), "r")
			if !inName && !inAlias {
				t.Errorf("%q survived the filter for %q and neither its name nor its alias %q contains it",
					sc.command.Name, "r", sc.command.Alias)
			}
		}
		if n := len(p.labels()); n == all {
			t.Errorf("one letter matched every command: the aliases all carry a keybinding, so a single letter is not a filter")
		}
	})

	t.Run("a query that matches nothing empties the list and says so", func(t *testing.T) {
		p := newPalette(t)
		for _, k := range []string{"z", "z", "z", "z"} {
			typeKey(t, p, k)
		}
		if len(p.labels()) != 0 {
			t.Errorf("a nonsense query still offers %v", p.labels())
		}
		if !strings.Contains(p.View(), "No matching commands") {
			t.Error("an empty list does not say that it is empty")
		}
	})

	t.Run("backspace widens the list again", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "r")
		narrow := len(p.labels())
		typeKey(t, p, "backspace")
		if len(p.labels()) <= narrow {
			t.Errorf("backspace left the list at %d commands, want more than the %d it had",
				len(p.labels()), narrow)
		}
	})

	t.Run("ctrl+u empties the line in one press", func(t *testing.T) {
		p := newPalette(t)
		for _, k := range []string{"r", "e", "f"} {
			typeKey(t, p, k)
		}
		typeKey(t, p, "ctrl+u")
		if p.query != "" {
			t.Errorf("ctrl+u left the query %q", p.query)
		}
		if got := len(p.labels()); got != len(p.commands) {
			t.Errorf("after clearing, %d of %d commands are offered", got, len(p.commands))
		}
	})

	t.Run("space is a character of the query, not a navigation key", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "r")
		typeKey(t, p, "space")
		if p.query != "r " {
			t.Errorf("the query is %q after a space, want %q", p.query, "r ")
		}
	})

	t.Run("tab is swallowed without changing anything", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "r")
		before, cursor := p.query, p.cursor
		_, handled := p.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: ""})
		if !handled {
			t.Error("tab was not reported as handled, so it would reach the view below")
		}
		if p.query != before || p.cursor != cursor {
			t.Errorf("tab changed the palette: query %q->%q cursor %d->%d",
				before, p.query, cursor, p.cursor)
		}
	})
}

// ---------------------------------------------------------------------------
// P3: the cursor is a position in the filtered list
// ---------------------------------------------------------------------------

// Scenario: El cursor se mueve dentro de la lista FILTRADA y nunca se sale.
//
// Two things have to hold at once: pressing down at the end stays put, and filtering the
// list down to fewer entries than the cursor's position pulls the cursor back to the last
// one. The second is the one that traps a user — filter to nothing and the highlight
// points at a row that is not there.
func TestPalette_TheCursorStaysInsideTheFilteredList(t *testing.T) {
	t.Run("down stops at the last entry and up at the first", func(t *testing.T) {
		p := newPalette(t)
		n := len(p.filtered)

		for range n + 5 {
			typeKey(t, p, "down")
		}
		if p.cursor != n-1 {
			t.Errorf("pressing down %d times over %d entries left the cursor at %d, want %d",
				n+5, n, p.cursor, n-1)
		}
		for range n + 5 {
			typeKey(t, p, "up")
		}
		if p.cursor != 0 {
			t.Errorf("pressing up %d times left the cursor at %d, want 0", n+5, p.cursor)
		}
	})

	t.Run("k and j are the same as up and down", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "j")
		typeKey(t, p, "j")
		if p.cursor != 2 {
			t.Errorf("two j presses left the cursor at %d, want 2", p.cursor)
		}
		typeKey(t, p, "k")
		if p.cursor != 1 {
			t.Errorf("a k press left the cursor at %d, want 1", p.cursor)
		}
	})

	t.Run("filtering below the cursor pulls it back to the last entry", func(t *testing.T) {
		p := newPalette(t)
		for range len(p.filtered) {
			typeKey(t, p, "down")
		}
		far := p.cursor
		if far < 3 {
			t.Skipf("only %d commands to walk, not enough to make this meaningful", far+1)
		}

		// Now narrow the list to one entry and keep narrowing: the cursor has to
		// come with it.
		typeKey(t, p, "z")
		// The invariant is that the cursor indexes a row that EXISTS, which
		// for an empty list means "not past the end" rather than "inside":
		// updateFiltered clamps a negative cursor to 0, and 0 is one past the
		// end of nothing. Both bounds are checked so the clamp cannot be
		// dropped in one direction without the test noticing.
		if p.cursor < 0 {
			t.Errorf("after filtering to %d entries the cursor went negative: %d", len(p.filtered), p.cursor)
		}
		if len(p.filtered) > 0 && p.cursor >= len(p.filtered) {
			t.Errorf("after filtering to %d entries the cursor is at %d, past the end", len(p.filtered), p.cursor)
		}
		if len(p.filtered) == 0 && p.cursor != 0 {
			t.Errorf("with nothing to select the cursor is %d, want 0: View and the enter arm both test the length first, so 0 is the safe resting place", p.cursor)
		}
	})

	t.Run("filtering to nothing leaves the cursor at zero, not minus one", func(t *testing.T) {
		p := newPalette(t)
		for range len(p.filtered) {
			typeKey(t, p, "down")
		}
		for range 4 {
			runeKey(t, p, "z")
		}
		if p.cursor != 0 {
			t.Errorf("with an empty list the cursor is %d, want 0 — a negative index here is what renders a row from the end", p.cursor)
		}
	})
}

// ---------------------------------------------------------------------------
// P4: enter selects, and the selection is the highlighted row
// ---------------------------------------------------------------------------

// Scenario: Enter elige lo que esta RESALTADO, y devuelve un comando ejecutable.
//
// The returned tea.Cmd is the only output of the whole widget, so it is asserted by
// running it: a command that returns nil, or the wrong action, or a message that does not
// carry the highlighted command, all fail the user the same way — the palette closes and
// nothing happens.
func TestPalette_EnterRunsTheHighlightedCommand(t *testing.T) {
	t.Run("the first entry, with no movement", func(t *testing.T) {
		p := newPalette(t)
		want := p.filtered[0].command
		cmd, handled := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		if !handled {
			t.Fatal("enter was not reported as handled")
		}
		if cmd == nil {
			t.Fatal("enter returned no command")
		}
		msg, ok := cmd().(CommandSelectedMsg)
		if !ok {
			t.Fatalf("enter produced a %T, want a CommandSelectedMsg", cmd())
		}
		if msg.Action != want.Action {
			t.Errorf("enter selected %q, want %q (%s)", msg.Action, want.Action, want.Name)
		}
		if p.IsVisible() {
			t.Error("the palette stayed open after a selection")
		}
	})

	t.Run("the entry the arrows moved to", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "down")
		typeKey(t, p, "down")
		want := p.labelAt(2)
		cmd, _ := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		if cmd == nil {
			t.Fatal("enter returned no command")
		}
		msg := cmd().(CommandSelectedMsg)
		if msg.Action != p.filtered[2].command.Action {
			t.Errorf("enter selected %q, want the action of %q", msg.Action, want)
		}
	})

	t.Run("an empty list selects nothing but still closes", func(t *testing.T) {
		p := newPalette(t)
		for range 4 {
			runeKey(t, p, "z")
		}
		if len(p.filtered) != 0 {
			t.Fatalf("the fixture matched %d commands, want none", len(p.filtered))
		}
		cmd, handled := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		if !handled {
			t.Error("enter on an empty list was not reported as handled")
		}
		if cmd != nil {
			t.Error("enter on an empty list returned a command to run")
		}
		// It stays OPEN, which is the right call and is not what the code
		// beside it suggests: the branch that has something to select calls
		// Hide() and the fallthrough does not. Filtering to nothing and
		// pressing enter should leave you able to fix the query, not close
		// the palette you opened to fix it with.
		if !p.IsVisible() {
			t.Error("enter on an empty list closed the palette, leaving nothing to fix the query with")
		}
	})

	t.Run("the selection is independent of the query afterwards", func(t *testing.T) {
		// Hide forgets the query but the command was already chosen; a palette
		// that remembered the query would re-filter on reopen and highlight a
		// different row, which is the bug Show's reset exists to prevent.
		p := newPalette(t)
		typeKey(t, p, "down")
		p.Hide()
		p.Show()
		if p.cursor != 0 {
			t.Errorf("reopening left the cursor at %d, want 0", p.cursor)
		}
	})
}

// ---------------------------------------------------------------------------
// P4: what is drawn
// ---------------------------------------------------------------------------

// Scenario: Los comandos se agrupan por SECCION, en un orden fijo.
//
// The grouping is the palette's whole navigability: a flat list of twenty entries is a
// list you have to read, and a grouped one is a list you can jump around. The order is a
// constant inside View rather than a property of the data — the FILTERED list is in score
// order and interleave sections, so an assertion about the data would be asserting about
// something else entirely.
//
// Two viewport sizes, because at 67 columns and below the twelve-row window only reaches
// the first two sections, and a fixture that only ever looks at a tall terminal is a
// fixture that never sees the scroll.
func TestPalette_TheListIsGroupedBySectionInAFixedOrder(t *testing.T) {
	// headersOf returns the section headers that reached the screen, in the order
	// they were painted, together with the index each was painted at.
	headersOf := func(view string) ([]CommandSection, []int) {
		var out []CommandSection
		var at []int
		for _, section := range []CommandSection{SectionDatabase, SectionQuery, SectionUI, SectionNav} {
			for i, line := range strings.Split(ansi.Strip(view), "\n") {
				if strings.Contains(strings.TrimSpace(line), string(section)) {
					out = append(out, section)
					at = append(at, i)
					break
				}
			}
		}
		return out, at
	}

	t.Run("the sections that are painted come in the fixed order", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			query string
		}{
			{"no query at all", ""},
			{"a query that keeps many sections", "e"},
			{"a query that reaches only the first section", "refresh"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := newPalette(t)
				for _, ch := range tc.query {
					runeKey(t, p, string(ch))
				}
				sections, _ := headersOf(p.View())
				if len(sections) < 2 {
					t.Skipf("the query %q left %d section header(s) on screen; nothing to compare", tc.query, len(sections))
				}
				for i := 1; i < len(sections); i++ {
					if rank(sections[i]) <= rank(sections[i-1]) {
						t.Errorf("painted order is %v, want Database < Query < UI < Navigation", sections)
						break
					}
				}
			})
		}
	})

	t.Run("every group after the first is introduced by a blank line", func(t *testing.T) {
		p := newPalette(t)
		lines := strings.Split(ansi.Strip(p.View()), "\n")
		seen := 0
		for i, line := range lines {
			// The header sits inside the frame, so the line is "│  Database  │"
			// and has to be reduced to the text before it can be compared.
			trimmed := strings.Trim(strings.TrimSpace(line), "│ ")
			isHeader := false
			for _, section := range []CommandSection{SectionDatabase, SectionQuery, SectionUI, SectionNav} {
				if trimmed == string(section) {
					isHeader = true
					break
				}
			}
			if !isHeader {
				continue
			}
			seen++
			if seen == 1 {
				continue // the first group needs no separator
			}
			// The separator is a blank line INSIDE the frame, so it is
			// "│      │" and not an empty string.
			if prev := strings.Trim(strings.TrimSpace(lines[i-1]), "│ "); prev != "" {
				t.Errorf("the %q header at line %d is preceded by %q, want a blank line",
					trimmed, i, prev)
			}
		}
		if seen < 2 {
			t.Errorf("only %d section header(s) reached the screen; the grouping is not being exercised", seen)
		}
	})
}

// rank puts the sections in their fixed render order, so an assertion can compare two of
// them without spelling the whole sequence out again.
func rank(s CommandSection) int {
	switch s {
	case SectionDatabase:
		return 0
	case SectionQuery:
		return 1
	case SectionUI:
		return 2
	case SectionNav:
		return 3
	}
	return -1
}

// Scenario: La caja tiene un ancho CON FORMULA, con suelo y techo.
//
// View takes 60% of the viewport, then clamps that to [40, 80] and hands it to the border
// style, which draws two cells of frame. The interesting part is the FLOOR: it swallows
// the 60% rule completely for every viewport narrower than 67 columns, so the palette is
// the same 38 cells wide in a 40-column terminal and in a 67-column one. That is pinned
// rather than described, because a change to either number is a visible layout change
// that no assertion about "it fits" would catch.
//
// And it OVERFLOWS below 38 columns: the floor wins over the viewport. Terminals that
// narrow are rare enough that a floor is a reasonable trade, but a floor that overflows
// is a decision, and an unasserted decision is one that gets reversed by accident.
func TestPalette_TheFrameHasAFormulaWithAFloorAndACeiling(t *testing.T) {
	wantOuter := func(w int) int {
		modalW := w * 6 / 10
		if modalW < 40 {
			modalW = 40
		}
		if modalW > 80 {
			modalW = 80
		}
		return modalW - 2 // the border style draws its own frame inside Width()
	}

	for _, w := range []int{20, 38, 40, 50, 67, 100, 200, 400} {
		p := newPalette(t)
		p.SetWidth(w)
		lines := strings.Split(p.View(), "\n")
		got := ansi.StringWidth(lines[0])
		if want := wantOuter(w); got != want {
			t.Errorf("at viewport %d the popup is %d cells wide, want %d", w, got, want)
		}
		// Every line of the popup is the same width, borders included: a
		// ragged frame is the classic lipgloss bug and it is invisible in a
		// screenshot at a glance.
		for i, line := range lines {
			if n := ansi.StringWidth(line); n != got {
				t.Errorf("at viewport %d line %d is %d cells wide, want %d like the rest: %q",
					w, i, n, got, ansi.Strip(line))
			}
		}
	}

	t.Run("below the floor the popup overflows the viewport", func(t *testing.T) {
		// Pinned as the behaviour it is. A fix would be to drop the floor or to
		// clamp it against the viewport, and either one has to change this test
		// on purpose rather than by accident.
		for _, w := range []int{20, 30, 37} {
			p := newPalette(t)
			p.SetWidth(w)
			got := ansi.StringWidth(strings.Split(p.View(), "\n")[0])
			if got <= w {
				t.Errorf("at viewport %d the popup is %d cells, so the floor no longer overflows — this test is now wrong about the contract", w, got)
			}
		}
	})
}

// Scenario: El desplazamiento lo calcula View(), no Update().
//
// This is the least obvious thing about the widget and the test that pins it. The keys
// reset the scroll offset to zero — updateFiltered does it on every keystroke — and the
// offset is recomputed from scratch inside View, from the cursor's position in the list.
// So the offset is a RENDER-TIME side effect: reading p.scrollOffset after a keystroke
// tells you nothing about what is on screen, and View has to be called before the state
// means anything.
//
// That is also why View is asserted to be IDEMPOTENT here. A render that mutates the
// state it reads can render two different things on two consecutive calls, which shows up
// as a palette that flickers and as a test that passes or fails depending on how many
// times somebody looked at it.
func TestPalette_TheScrollOffsetIsComputedByTheRenderAndTheRenderIsIdempotent(t *testing.T) {
	p := newPalette(t)
	if len(p.filtered) <= 12 {
		t.Skipf("only %d commands, the window is 12 and nothing has to scroll", len(p.filtered))
	}

	walk := func(key string) {
		for range len(p.filtered) + 3 {
			typeKey(t, p, key)
		}
	}

	// At the top, and the offset is zero because no render has happened yet.
	if p.scrollOffset != 0 {
		t.Fatalf("the fixture starts with the scroll offset at %d", p.scrollOffset)
	}
	first := p.View()
	if p.scrollOffset != 0 {
		t.Errorf("rendering the top of the list moved the scroll offset to %d, want 0", p.scrollOffset)
	}
	if again := p.View(); again != first {
		t.Error("View returned a different string the second time with nothing changed between the calls")
	}

	// Walk to the end. The keys leave the offset at zero, and the RENDER is what
	// scrolls — which is the whole point of this test.
	walk("down")
	if p.scrollOffset != 0 {
		t.Errorf("walking with the keys moved the scroll offset to %d; the offset is supposed to be a render-time value", p.scrollOffset)
	}
	endView := p.View()
	if p.scrollOffset == 0 {
		t.Error("rendering the end of a longer-than-window list left the scroll offset at zero")
	}
	if again := p.View(); again != endView {
		t.Error("View returned a different string the second time at the end of the list")
	}

	// And the promise the scroll exists to keep: the highlighted command is a row
	// the user can actually see, at both ends.
	for _, tc := range []struct {
		where  string
		cursor int
	}{
		{"the last entry", len(p.filtered) - 1},
		{"the first entry", 0},
	} {
		p.cursor = tc.cursor
		view := ansi.Strip(p.View())
		highlighted := p.filtered[p.cursor].command.Name
		if !strings.Contains(view, highlighted) {
			t.Errorf("at %s the highlighted command %q is not on screen:\n%s", tc.where, highlighted, view)
		}
		if n := len(strings.Split(view, "\n")); n > 17 {
			t.Errorf("at %s the popup rendered %d lines; the input plus a twelve-row window is seventeen", tc.where, n)
		}
	}
}

// Scenario: Lo que NO es una tecla pasa de largo, y una tecla sin texto tambien.
//
// Two different fall-throughs, both of which decide whether a key reaches the view
// underneath. A non-key message has to be ignored while the palette is open, or the app's
// own messages would be swallowed by an overlay. And a key that arrives with no text — a
// bare modifier, a function key the switch does not name — must be reported as NOT
// handled, which is what lets it fall through. A key that reports handled=false after
// doing nothing is how a palette stays out of the way.
func TestPalette_WhatIsNotATextKeyFallsThrough(t *testing.T) {
	t.Run("a message that is not a key press is ignored", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "r")
		before := p.query

		cmd, handled := p.Update(someOtherMsg{})
		if handled {
			t.Error("a non-key message was reported as handled")
		}
		if cmd != nil {
			t.Error("a non-key message produced a command")
		}
		if p.query != before {
			t.Errorf("a non-key message wrote %q into the query, want it left at %q", p.query, before)
		}
		if !p.IsVisible() {
			t.Error("a non-key message closed the palette")
		}
	})

	t.Run("a key with no text is not handled", func(t *testing.T) {
		p := newPalette(t)
		typeKey(t, p, "r")
		before := p.query

		for _, code := range []rune{tea.KeyF1, tea.KeyHome, tea.KeyEnd} {
			cmd, handled := p.Update(tea.KeyPressMsg{Code: code})
			if handled {
				t.Errorf("the bare key %q was reported as handled, so it would not reach the view below", code)
			}
			if cmd != nil {
				t.Errorf("the bare key %q produced a command", code)
			}
		}
		if p.query != before {
			t.Errorf("a bare modifier wrote %q into the query, want it left at %q", p.query, before)
		}
	})

	t.Run("the space KEY types one space, however it is reported", func(t *testing.T) {
		// A key event whose Code is the space key reports String() == "space"
		// whatever its Text says, so both shapes land on the named arm and add
		// exactly one space.
		for _, tc := range []struct {
			name string
			msg  tea.KeyPressMsg
		}{
			{"with its text", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
			{"with no text at all", tea.KeyPressMsg{Code: tea.KeySpace}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := newPalette(t)
				p.Update(tc.msg)
				if p.query != " " {
					t.Errorf("the query is %q, want exactly one space", p.query)
				}
				// Twice is two spaces, not one: the named arm has to be
				// reachable twice for a query with a space in the middle.
				p.Update(tea.KeyPressMsg{Code: tea.KeySpace})
				if p.query != "  " {
					t.Errorf("two spaces produced the query %q, want two", p.query)
				}
			})
		}
	})

	t.Run("another key carrying a space as text types nothing", func(t *testing.T) {
		// The `msg.Text != " "` guard in the default arm. Without it a
		// terminal that reports the CHARACTER rather than the key would insert
		// a space on top of the named arm's, and a query with doubled spaces
		// matches nothing at all — the palette would look broken while the
		// filter reported zero results.
		for _, tc := range []struct {
			name string
			msg  tea.KeyPressMsg
		}{
			{"a letter code with a space as text", tea.KeyPressMsg{Code: 'x', Text: " "}},
			{"an unknown code with a space as text", tea.KeyPressMsg{Code: rune(0), Text: " "}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := newPalette(t)
				_, handled := p.Update(tc.msg)
				if p.query != "" {
					t.Errorf("the query is %q, want it left empty", p.query)
				}
				if handled {
					t.Error("a refused space was reported as handled, so it would not reach the view below")
				}
			})
		}
	})
}

// someOtherMsg stands in for any of the app's own messages. The palette must not react
// to it, and using a type that is obviously not a key is the point: this is about the
// type switch, not about one particular message.
type someOtherMsg struct{ N int }

// ---------------------------------------------------------------------------
// A property, over many states rather than a handful
// ---------------------------------------------------------------------------

// Scenario: Despues de CUALQUIER secuencia de teclas, el palette sigue siendo usable.
//
// The scenario tests above each reach one state. This one walks a lot of them — a
// deterministic pseudo-random walk, so a failure is reproducible from the printed seed —
// and checks the invariants that make the widget usable rather than merely correct on the
// happy path:
//
//   - the cursor never indexes a row that does not exist
//   - the scroll offset stays inside the list
//   - every line of the popup is the width the formula says, at any viewport
//   - the highlighted command is on screen
//   - the query is always valid UTF-8, which is the invariant the byte-wise backspace
//     broke and the reason that bug was worth a test of its own
//
// It exists because two of the branches left uncovered by the scenario tests are guards
// that no sequence reaches, and "no sequence reaches them" is only worth believing if
// something looked.
func TestPalette_AfterAnyKeySequenceThePaletteIsStillUsable(t *testing.T) {
	keys := []string{"a", "b", "c", "r", "e", "f", "z", "up", "down", "k", "j",
		"backspace", "space", "tab", "ctrl+u", "enter", "esc", "é", "表"}
	widths := []int{40, 67, 100, 200}

	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		p := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		p.SetWidth(widths[int(seed)%len(widths)])
		p.SetHeight(30)

		for step := range 60 {
			key := keys[rng.Intn(len(keys))]
			if key == "esc" {
				p.Show() // keep it open so the walk stays inside one session
			}
			if len([]rune(key)) == 1 && key != " " {
				runeKey(t, p, key)
			} else {
				typeKey(t, p, key)
			}

			if !utf8.ValidString(p.query) {
				t.Fatalf("seed %d step %d: the query %q is not valid UTF-8 after the key %q",
					seed, step, p.query, key)
			}
			if p.cursor < 0 {
				t.Fatalf("seed %d step %d: the cursor went negative (%d) after the key %q",
					seed, step, p.cursor, key)
			}
			if len(p.filtered) > 0 && p.cursor >= len(p.filtered) {
				t.Fatalf("seed %d step %d: the cursor is %d with %d entries after the key %q",
					seed, step, p.cursor, len(p.filtered), key)
			}
			if !p.IsVisible() {
				continue // a closed palette renders nothing and has no cursor to speak of
			}

			view := p.View()
			want := popupWidth(p.width)
			for i, line := range strings.Split(view, "\n") {
				if n := ansi.StringWidth(line); n != want {
					t.Fatalf("seed %d step %d: line %d is %d cells wide, want %d, after the key %q\n%s",
						seed, step, i, n, want, key, ansi.Strip(view))
				}
			}
			if p.scrollOffset < 0 || (len(p.filtered) > 0 && p.scrollOffset > len(p.filtered)) {
				t.Fatalf("seed %d step %d: the scroll offset %d is outside 0..%d after the key %q",
					seed, step, p.scrollOffset, len(p.filtered), key)
			}
			if len(p.filtered) > 0 {
				highlighted := p.filtered[p.cursor].command.Name
				if !strings.Contains(ansi.Strip(view), highlighted) {
					t.Fatalf("seed %d step %d: the highlighted %q is not on screen after the key %q\n%s",
						seed, step, highlighted, key, ansi.Strip(view))
				}
			}
			if n := len(strings.Split(view, "\n")); n > 17 {
				t.Fatalf("seed %d step %d: the popup rendered %d lines after the key %q, want at most 17",
					seed, step, n, key)
			}
		}
	}
}

// popupWidth is the formula View uses, extracted so the property test and the scenario
// test agree on it by construction rather than by being written the same way twice.
func popupWidth(viewport int) int {
	modalW := viewport * 6 / 10
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 80 {
		modalW = 80
	}
	return modalW - 2
}
