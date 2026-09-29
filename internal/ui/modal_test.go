package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/keydisplay"
	"github.com/charmbracelet/x/ansi"
)

func teaKey(key string) tea.KeyPressMsg {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	default:
		return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
}

func modalWith(registry *config.KeybindRegistry) *HelpModal {
	m := NewHelpModal(theme.Resolve("dark").Styles(), registry)
	m.SetWidth(80)
	m.SetHeight(400)
	m.Show()
	return m
}

func TestHelpModal_IncludesRollback(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	if !strings.Contains(view, "Rollback Last Transaction") {
		t.Fatal("Help modal does not contain 'Rollback Last Transaction'")
	}
}

// Scenario: ASK action shown in the help modal
func TestHelpModal_GlobalSection_IncludesAsk(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	stripped := ansi.Strip(view)
	if !strings.Contains(stripped, "Ask AI (NL→SQL)") {
		t.Fatal("Help modal does not contain 'Ask AI (NL→SQL)' in Global section")
	}
	want := "  " + fmt.Sprintf("%-14s", "a") + "Ask AI (NL→SQL)"
	if !strings.Contains(stripped, want) {
		t.Fatalf("Help modal does not render key 'a' next to 'Ask AI (NL→SQL)'; want substring %q", want)
	}
}

func TestHelpModal_EditorSection_IncludesCopySQL(t *testing.T) {
	modal := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))
	view := modal.View()
	if !strings.Contains(view, "Copy SQL") {
		t.Fatal("Help modal does not contain 'Copy SQL' in Editor section")
	}
	if !strings.Contains(view, keydisplay.Key("ctrl+y")) {
		t.Fatal("Help modal does not show 'ctrl+y' keybind for Copy SQL")
	}
}

// Scenario: El modal de ayuda cubre todas las secciones operables.
func TestHelpModal_CoversAllOperableSections(t *testing.T) {
	view := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{})).View()
	for _, header := range []string{
		"Explorer", "Grid", "Grid Preview", "Explorer Preview",
		"Editor", "Query Browser", "Connection Picker",
		"EDIT Mode", "FILTER Mode", "WHERE FILTER Mode", "Mouse",
	} {
		if !strings.Contains(view, header) {
			t.Fatalf("Help modal is missing the %q section", header)
		}
	}
}

// Scenario: El modal de ayuda es navegable y se cierra.
func TestHelpModal_ClosesOnEscOrQuestion(t *testing.T) {
	for _, key := range []string{"esc", "?"} {
		styles := theme.Resolve("dark").Styles()
		modal := NewHelpModal(styles, config.NewKeybindRegistry(config.KeybindingsConfig{}))
		modal.SetWidth(80)
		modal.SetHeight(40)
		modal.Show()

		_, handled := modal.Update(teaKey(key))
		if !handled {
			t.Fatalf("Update(%q) not handled", key)
		}
		if modal.IsVisible() {
			t.Fatalf("modal still visible after %q", key)
		}
	}
}

// Scenario: Custom key override changes the modal key too.
func TestHelpModal_CustomKeyOverride(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"rollback": "ctrl+z"}})
	view := modalWith(kb).View()
	if !strings.Contains(view, keydisplay.Key("ctrl+z")) {
		t.Fatalf("modal does not show the custom rollback key: %q", view)
	}
	if strings.Contains(view, "U Rollback Last Transaction") {
		t.Fatalf("modal still shows the default rollback key after override")
	}
}

// widestLine returns the rendered width of the widest line, which for the
// bordered modal is the content width (lipgloss pads it out to Width()).
func widestLine(t *testing.T, view string) int {
	t.Helper()
	var widest int
	for _, l := range strings.Split(ansi.Strip(view), "\n") {
		if n := lipgloss.Width(l); n > widest {
			widest = n
		}
	}
	return widest
}

// Scenario: El modal ocupa el 70% del ancho, acotado a un mínimo y un máximo
// legibles, y descuenta 4 columnas de marco. The clamps and the -4 are the
// modal's whole layout contract, so they get pinned instead of eyeballed.
func TestHelpModal_WidthIsSeventyPercentClamped(t *testing.T) {
	clamp := func(w int) int {
		mw := w * 7 / 10
		if mw < 50 {
			mw = 50
		}
		if mw > 90 {
			mw = 90
		}
		return mw
	}
	// 40 -> below the floor, 100 -> the 70% case, 200 -> above the ceiling.
	for _, width := range []int{40, 100, 200} {
		m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		m.SetWidth(width)
		m.SetHeight(400)
		m.Show()

		want := clamp(width) - 4
		if got := widestLine(t, m.View()); got != want {
			t.Errorf("width %d: modal content width = %d, want %d (clamp(70%%)=%d, minus 4 for the border)",
				width, got, want, clamp(width))
		}
	}
}

// Scenario: El alto útil es la altura menos 10, con un mínimo de 5 líneas, y el
// modal se dibuja con exactamente esas líneas de contenido.
func TestHelpModal_VisibleLinesFollowHeight(t *testing.T) {
	for _, height := range []int{15, 20, 21, 40} {
		m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		m.SetWidth(100)
		m.SetHeight(height)
		m.Show()

		maxVisible := height - 10
		if maxVisible < 5 {
			maxVisible = 5
		}
		// +1 for the "Help" header, +2 for the top and bottom border.
		want := maxVisible + 3
		if got := len(strings.Split(ansi.Strip(m.View()), "\n")); got != want {
			t.Errorf("height %d: rendered %d lines, want %d (maxVisible=%d + header + 2 borders)",
				height, got, want, maxVisible)
		}
	}
}

// Scenario: Scrollear al final muestra el final del contenido, sin pasarse. The
// hint is the last line, so its presence proves the window reached the end
// instead of running past it.
func TestHelpModal_ScrollToEndShowsLastLine(t *testing.T) {
	const height = 20

	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(100)
	m.SetHeight(height)
	m.Show()

	if _, handled := m.Update(teaKey("G")); !handled {
		t.Fatal("G not handled by the help modal")
	}
	if m.scroll != 1000 {
		t.Fatalf("G did not jump to the end: scroll = %d", m.scroll)
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "scroll") {
		t.Fatal("scrolled to the end but the closing hint is not visible: the window overshot the content")
	}
	if m.scroll >= 1000 {
		t.Fatalf("scroll = %d, want it clamped back to the end of the content", m.scroll)
	}
	// Rendering again at the clamp must not move the window: the clamp is a
	// fixed point, not a one-shot adjustment.
	at := m.scroll
	m.View()
	if m.scroll != at {
		t.Errorf("second View() moved the scroll from %d to %d; the clamp is not stable", at, m.scroll)
	}
}

// Scenario: Un scroll dentro del rango "pasado pero no demasiado" sigue
// mostrando la ventana completa.
//
// The clamp compares the scroll against len(content)-maxVisible. A scroll that
// lands between that and len(content)+maxVisible must still be pulled back to
// the end, otherwise the window collapses to the last line and the modal renders
// empty. 17 steps of ctrl+d is exactly that gap: it overshoots the window but
// stays short of the "give up and clamp anyway" bound.
func TestHelpModal_ScrollInsideTheGapStillShowsFullWindow(t *testing.T) {
	const (
		height     = 20
		maxVisible = 10 // height - 10
		steps      = 17 // 17 * 10 = 170, which is past the window (160)
	)

	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(100)
	m.SetHeight(height)
	m.Show()

	for i := 0; i < steps; i++ {
		m.Update(teaKey("ctrl+d"))
	}
	if m.scroll != steps*10 {
		t.Fatalf("scroll = %d, want %d before rendering", m.scroll, steps*10)
	}

	view := ansi.Strip(m.View())
	// +1 for the "Help" header, +2 for the borders.
	if got := len(strings.Split(view, "\n")); got != maxVisible+3 {
		t.Errorf("rendered %d lines, want %d (the window must stay %d lines tall even when scrolled past its start)",
			got, maxVisible+3, maxVisible)
	}
	if !strings.Contains(view, "scroll") {
		t.Error("the closing hint is missing: the window collapsed instead of clamping to the end")
	}
}

// Scenario: Scrollear más allá del final no deja la ventana en negativo ni
// desborde el contenido. ctrl+d is the coarse step, so it overshoots easily.
func TestHelpModal_ScrollNeverExceedsContent(t *testing.T) {
	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(100)
	m.SetHeight(20)
	m.Show()

	for i := 0; i < 50; i++ {
		m.Update(teaKey("ctrl+d"))
	}
	view := m.View() // must not panic on an out-of-range slice
	if m.scroll < 0 {
		t.Errorf("scroll = %d, want it never negative", m.scroll)
	}
	if !strings.Contains(ansi.Strip(view), "scroll") {
		t.Error("after overscrolling, the closing hint should still be the last visible line")
	}
}

// Scenario: Scrollear hacia arriba desde arriba queda en 0, nunca en negativo.
func TestHelpModal_ScrollUpFromTopStaysAtZero(t *testing.T) {
	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(100)
	m.SetHeight(20)
	m.Show()

	for i := 0; i < 10; i++ {
		m.Update(teaKey("ctrl+u"))
	}
	if m.scroll != 0 {
		t.Fatalf("scroll = %d, want it clamped at 0", m.scroll)
	}
	m.View()
	if m.scroll != 0 {
		t.Fatalf("View() moved the scroll to %d, want 0", m.scroll)
	}
}

// Scenario: La rueda del ratón desplaza el modal, y llegar arriba frena en 0.
// The wheel is the one scroll path that clamps inside the handler rather than
// in View(), so both branches are exercised here.
func TestHelpModal_MouseWheelScrollsAndStopsAtTop(t *testing.T) {
	m := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))

	// Wheel down three times.
	for i := 0; i < 3; i++ {
		if _, handled := m.Update(tea.MouseWheelMsg{X: 0, Y: 1, Button: tea.MouseWheelDown}); !handled {
			t.Fatal("wheel down was not handled by the help modal")
		}
	}
	if m.scroll != 3 {
		t.Errorf("after 3 wheel-downs scroll = %d, want 3", m.scroll)
	}

	// Wheel up far past the top: must stop at 0, not go negative.
	for i := 0; i < 10; i++ {
		m.Update(tea.MouseWheelMsg{X: 0, Y: -1, Button: tea.MouseWheelUp})
	}
	if m.scroll != 0 {
		t.Errorf("after wheeling up past the top, scroll = %d, want 0", m.scroll)
	}
	m.View() // must not panic on a negative scroll
	if m.scroll != 0 {
		t.Errorf("View() moved the scroll to %d, want 0", m.scroll)
	}
}

// Scenario: Las teclas de flecha y sus equivalentes vi desplazan una línea.
// up/k and down/j are aliases, and each must move the same single line, with
// up stopping at 0 rather than going negative.
func TestHelpModal_ArrowKeysScrollOneLineAtATime(t *testing.T) {
	for _, down := range []string{"down", "j"} {
		for _, up := range []string{"up", "k"} {
			m := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))

			if _, handled := m.Update(teaKey(down)); !handled {
				t.Fatalf("%q was not handled", down)
			}
			if m.scroll != 1 {
				t.Errorf("%q moved the scroll to %d, want 1", down, m.scroll)
			}

			if _, handled := m.Update(teaKey(up)); !handled {
				t.Fatalf("%q was not handled", up)
			}
			if m.scroll != 0 {
				t.Errorf("%q left the scroll at %d, want 0", up, m.scroll)
			}
			// A second step up must not go negative.
			m.Update(teaKey(up))
			if m.scroll != 0 {
				t.Errorf("%q at the top moved the scroll to %d, want 0", up, m.scroll)
			}
		}
	}
}

// Scenario: "g" vuelve al principio, "G" salta al final, y ctrl+d/ctrl+u mueven
// diez líneas. The coarse steps and the jumps share the same scroll field, so a
// step landing past the end is pinned by View()'s clamp.
func TestHelpModal_CoarseScrollKeysAndJumps(t *testing.T) {
	m := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))

	// G jumps past the end. The clamp lives in View(), not in the handler, so
	// the jump itself is deliberately out of range until the next render.
	m.Update(teaKey("G"))
	if m.scroll != 1000 {
		t.Fatalf("G left the scroll at %d, want the raw 1000 jump", m.scroll)
	}
	m.View()
	if m.scroll >= 1000 {
		t.Errorf("View() left the scroll at %d; it must clamp back to the end", m.scroll)
	}

	// g returns to the top.
	m.Update(teaKey("g"))
	if m.scroll != 0 {
		t.Fatalf("g left the scroll at %d, want 0", m.scroll)
	}

	// ctrl+d steps ten lines, ctrl+u steps back.
	m.Update(teaKey("ctrl+d"))
	if m.scroll != 10 {
		t.Errorf("ctrl+d left the scroll at %d, want 10", m.scroll)
	}
	m.Update(teaKey("ctrl+u"))
	if m.scroll != 0 {
		t.Errorf("ctrl+u left the scroll at %d, want 0", m.scroll)
	}
}

// Scenario: "q" cierra el modal igual que "esc" y "?". The close keys are an
// overlay exception, not registry keys, so they are listed here explicitly.
func TestHelpModal_QClosesToo(t *testing.T) {
	m := modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{}))

	if _, handled := m.Update(teaKey("q")); !handled {
		t.Fatal("q was not handled")
	}
	if m.IsVisible() {
		t.Fatal("modal still visible after q")
	}
}

// Scenario: Un modal oculto ignora todo, incluso las teclas que sí gestiona
// cuando está visible. A hidden overlay must not eat the app's keystrokes.
func TestHelpModal_HiddenModalIgnoresEverything(t *testing.T) {
	m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	m.SetWidth(80)
	m.SetHeight(40)
	// Never shown.

	if got := m.View(); got != "" {
		t.Errorf("a hidden modal rendered %q, want empty", got)
	}
	for _, key := range []string{"esc", "down", "j", "ctrl+d", "G", "q"} {
		if _, handled := m.Update(teaKey(key)); handled {
			t.Errorf("a hidden modal consumed %q", key)
		}
	}
	if m.scroll != 0 {
		t.Errorf("a hidden modal changed the scroll to %d", m.scroll)
	}
}

// Scenario: The help modal shows every key, including aliases, and groups by
// the same registry field the pane uses.
func TestHelpModal_GridShowsAliasesAndGroups(t *testing.T) {
	view := ansi.Strip(modalWith(config.NewKeybindRegistry(config.KeybindingsConfig{})).View())

	// next_page is ungrouped, so every alias must be listed on its line.
	wantLine := "  " + fmt.Sprintf("%-14s", keydisplay.Key("n, ], ctrl+right")) + "Next Page"
	if !strings.Contains(view, wantLine) {
		t.Errorf("Grid section is missing the full next_page alias line; want %q", wantLine)
	}

	// Sibling sets collapse under their shared group label.
	for _, label := range []string{"Navigate", "First/Last", "Half Page", "Go to Page"} {
		if !strings.Contains(view, label) {
			t.Errorf("Grid section is missing the group label %q", label)
		}
	}
}
