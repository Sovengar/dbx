package ui

// Scenario: El panel de atajos dibuja exactamente las filas que dice que dibuja.
//
// Every existing test in this package asserted on `renderLines()` — the intermediate slice
// — and none of them called `View()`, the function that produces what the user actually
// sees. So the whole of View was untested here: the title, the border, the focused/unfocused
// border colour, and the joining of the lines.
//
// That matters more than a coverage number, because Height() was added to sit next to it.
// Height() is what the app's layout subtracts from the window to decide how many rows the
// content gets, and it is derived from renderLines() rather than measured from View(). The
// only thing keeping the two honest is the claim that a rendered pane occupies
// len(renderLines()) + 2 rows. Nothing asserted it, so the first theme change that added a
// border row, or a title that wrapped, would have silently moved the content height.
//
// So the test measures View() and compares it against Height() rather than comparing Height()
// against the formula that produced it.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

// paneAt is paneWith with an explicit width, since the width is what decides how many rows
// the pane takes and every assertion about the row count needs it pinned.
func paneAt(focus string, w int) *KeybindsPane {
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), testRegistry())
	p.SetWidth(w)
	p.SetFocus(focus)
	return p
}

// renderedRows is how many terminal rows a rendered pane occupies, ignoring trailing blank
// rows so that a trailing newline in the string is not counted as an empty row.
func renderedRows(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestThePaneRendersAsManyRowsAsItReserves(t *testing.T) {
	// A spread of widths, because the pane wraps and so its height is a function of
	// width. Testing one width would only ever prove that ONE width is right, and the
	// original bug was a layout constant that happened to be right for one window.
	for _, w := range []int{40, 60, 80, 100, 120, 160, 200} {
		p := paneAt(config.ContextExplorer, w)
		view := p.View()
		got := renderedRows(view)
		want := p.Height()
		if want == 0 {
			t.Errorf("at width %d the pane reserves no rows but rendered %d", w, got)
			continue
		}
		if got != want {
			t.Errorf("at width %d the pane reserves %d rows and renders %d:\n%s", w, want, got, view)
		}
	}
}

func TestThePaneReserveIsTheContentPlusTwoBorderRows(t *testing.T) {
	for _, w := range []int{60, 120, 200} {
		p := paneAt(config.ContextEditor, w)
		if p.Height() != len(p.renderLines())+2 {
			t.Errorf("at width %d Height() is %d for %d content lines, want the content plus two borders",
				w, p.Height(), len(p.renderLines()))
		}
	}
}

// Scenario: La altura MOSTRADA cambia con el ancho, porque el pane envuelve.
func TestThePaneHeightFollowsTheWidth(t *testing.T) {
	// If the height were constant, the layout bug this replaced would still be there
	// with a different constant in it: a fixed reserve cannot track a wrapping box.
	// 40 columns cannot hold seven segments, so it must take more rows than 200.
	narrow := paneAt(config.ContextExplorer, 40).Height()
	wide := paneAt(config.ContextExplorer, 200).Height()
	if narrow <= wide {
		t.Errorf("at width 40 the pane takes %d rows and at 200 it takes %d; wrapping is not being measured", narrow, wide)
	}
}

func TestViewRendersTheTitleAndTheKeybinds(t *testing.T) {
	p := paneAt(config.ContextExplorer, 200)
	view := p.View()

	if !strings.Contains(view, "Keybinds") {
		t.Errorf("the rendered pane has no title:\n%s", view)
	}
	// The content the slice had, so the join is asserted rather than assumed.
	joined := strings.Join(p.renderLines(), "\n")
	if strings.TrimSpace(joined) != "" && !strings.Contains(view, "Filter Tables") {
		t.Errorf("the rendered pane does not contain its own content %q:\n%s", joined, view)
	}
	// The border, which is what makes Height() worth two extra rows.
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╮") {
		t.Errorf("the rendered pane has no rounded border:\n%s", view)
	}
}

// Scenario: El borde distingue la vista enfocada de la que no lo esta.
//
// The colour is read off the TOP LINE only, and that restriction is the point.
//
// The first version compared whole renders and then asserted that the blurred one did
// not contain the active colour's escape sequence. It did — not because the blurred
// border was drawn with it, but because several styles in the dark theme share
// #89b4fa, so the sequence appears in the help text too. The assertion was about the
// whole frame when the claim was about the border, and it would have kept passing
// through a change that swapped the two border colours entirely.
//
// The top row of the box is the border and nothing else, so a colour found there is a
// colour that was chosen for the border.
func TestTheFocusedPaneHasADifferentBorder(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	active := sgrFor(styles.BorderActive.GetBorderTopForeground())
	normal := sgrFor(styles.Border.GetBorderTopForeground())

	if active == normal {
		t.Fatal("the theme gives the active and the normal border the same colour, so this test cannot tell them apart")
	}

	blurred := topLine(paneAt(config.ContextExplorer, 200).View())
	if !strings.Contains(blurred, normal) {
		t.Errorf("the blurred border is %q, want it drawn in the normal colour", blurred)
	}
	if strings.Contains(blurred, active) {
		t.Errorf("the blurred border is %q, drawn in the ACTIVE colour", blurred)
	}

	p := paneAt(config.ContextExplorer, 200)
	p.Focus()
	focused := topLine(p.View())
	if !strings.Contains(focused, active) {
		t.Errorf("the focused border is %q, want it drawn in the active colour", focused)
	}
	if strings.Contains(focused, normal) {
		t.Errorf("the focused border is %q, drawn in the normal colour", focused)
	}

	if blurred == focused {
		t.Error("focusing the pane did not change its border")
	}
}

// topLine is the first row of a rendered pane: the border, with no content on it.
func topLine(view string) string {
	if i := strings.Index(view, "\n"); i >= 0 {
		return view[:i]
	}
	return view
}

// sgrFor is the escape sequence lipgloss emits for a foreground colour. Comparing on this
// rather than on the whole render is what makes "which colour was chosen" assertable.
func sgrFor(c interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) string {
	r, g, b, a := c.RGBA()
	rendered := lipgloss.NewStyle().Foreground(lipgloss.Color(rgbHex(r, g, b, a))).Render("x")
	// Cut at the end of the opening escape sequence, so what is returned is the
	// prefix lipgloss emits rather than a hardcoded format string. If a dependency
	// upgrade changes how colours are encoded, this follows it instead of quietly
	// comparing a string no render will ever contain.
	if i := strings.Index(rendered, "m"); i >= 0 {
		return rendered[:i+1]
	}
	return rendered
}

// rgbHex renders 8-bit components as #RRGGBB.
//
// Six digits and not eight: RGBA also hands back alpha, and lipgloss does not parse an
// eight-digit hex as a colour — it yields NoColor, which renders as NO escape sequence at
// all. So the first version of this produced "x" for every colour, and the two border
// colours compared equal because the function was returning the same empty string twice.
// The test's own guard is what caught it, which is the argument for having that guard.
func rgbHex(r, g, b, _ uint32) string {
	const hex = "0123456789abcdef"
	var out strings.Builder
	out.WriteString("#")
	for _, x := range [3]uint32{r >> 8, g >> 8, b >> 8} {
		out.WriteByte(hex[x>>4])
		out.WriteByte(hex[x&0xf])
	}
	return out.String()
}

// Scenario: Visible y SetVisible governs BOTH the drawing and the reservation.
func TestVisibleGovernsDrawingAndReservationTogether(t *testing.T) {
	for _, tc := range []struct {
		name        string
		visible     bool
		wantDrawn   bool
		wantRows    int
		description string
	}{
		{"visible", true, true, -1, "drawn and occupying rows"},
		{"hidden", false, false, 0, "not drawn and occupying nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := paneAt(config.ContextExplorer, 120)
			p.SetVisible(tc.visible)

			if got := p.Visible(); got != tc.visible {
				t.Errorf("Visible() is %t after SetVisible(%t)", got, tc.visible)
			}
			drawn := p.View() != ""
			if drawn != tc.wantDrawn {
				t.Errorf("the pane %s; %s", drawnOrNot(drawn), tc.description)
			}
			if tc.wantRows >= 0 && p.Height() != tc.wantRows {
				t.Errorf("the pane reserves %d rows; %s", p.Height(), tc.description)
			}
		})
	}

	t.Run("a new pane is visible, because the default is what everyone had", func(t *testing.T) {
		// Every pane built before ui.statusbar_help existed drew its keybinds. A
		// zero-value struct would default to hidden and silently delete the help for
		// every caller that forgot the setter — and no caller existed yet.
		if !NewKeybindsPane(theme.Resolve("dark").Styles(), testRegistry()).Visible() {
			t.Error("a new pane is hidden; the zero value of the flag flipped the default")
		}
	})

	t.Run("toggling back and forth returns to the same render", func(t *testing.T) {
		p := paneAt(config.ContextExplorer, 120)
		before := p.View()
		p.SetVisible(false)
		p.SetVisible(true)
		if got := p.View(); got != before {
			t.Errorf("hiding and re-showing changed the render:\nbefore %q\nafter  %q", before, got)
		}
		if p.Height() != renderedRows(before) {
			t.Errorf("after toggling, Height() is %d and the render is %d rows", p.Height(), renderedRows(before))
		}
	})
}

// Scenario: Context decide que atajos se muestran, y tiene una tabla completa.
func TestContextIsATruthTable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		queryBrowser bool
		editor       bool
		focus        string
		want         string
	}{
		{"an unset focus is the explorer", false, false, "", config.ContextExplorer},
		{"the focus wins", false, false, config.ContextGrid, config.ContextGrid},
		{"the editor overrides the focus", false, true, config.ContextGrid, config.ContextEditor},
		{"the query browser overrides the editor", true, true, config.ContextGrid, config.ContextQueryBrowser},
		{"the query browser overrides an empty focus", true, false, "", config.ContextQueryBrowser},
		{"the editor overrides an empty focus", false, true, "", config.ContextEditor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := paneAt(config.ContextExplorer, 200)
			p.SetQueryBrowserOpen(tc.queryBrowser)
			p.SetEditorOpen(tc.editor)
			p.SetFocus(tc.focus)
			if got := p.Context(); got != tc.want {
				t.Errorf("Context() is %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("the context changes what is drawn", func(t *testing.T) {
		// The truth table above would all pass with a function that always returned
		// the explorer, so it is pinned against the render too.
		explorer := strings.Join(paneAt(config.ContextExplorer, 200).renderLines(), "\n")
		grid := strings.Join(paneAt(config.ContextGrid, 200).renderLines(), "\n")
		if explorer == grid {
			t.Error("the explorer and the grid render the same keybinds")
		}
	})
}

// Scenario: Los estados del panel se reflejan en lo que se dibuja.
func TestTheSettersAffectWhatIsDrawn(t *testing.T) {
	t.Run("a pending transaction shows the rollback key and a tx note", func(t *testing.T) {
		p := paneAt(config.ContextGrid, 200)
		p.SetTxPending(true)
		if !linesContain(p.renderLines(), "tx pending") {
			t.Errorf("a pending transaction does not show the tx note: %v", p.renderLines())
		}
	})

	t.Run("the query browser being open selects its context", func(t *testing.T) {
		p := paneAt(config.ContextExplorer, 200)
		p.SetQueryBrowserOpen(true)
		if got := p.Context(); got != config.ContextQueryBrowser {
			t.Errorf("Context() is %q with the browser open, want %q", got, config.ContextQueryBrowser)
		}
	})

	t.Run("autocomplete readiness is remembered", func(t *testing.T) {
		p := paneAt(config.ContextEditor, 200)
		p.SetAutocompleteReady(true)
		if !p.autocompleteReady {
			t.Error("SetAutocompleteReady(true) did not take")
		}
	})

	t.Run("the height is remembered", func(t *testing.T) {
		p := paneAt(config.ContextExplorer, 200)
		p.SetHeight(37)
		if p.height != 37 {
			t.Errorf("the pane height is %d after SetHeight(37)", p.height)
		}
	})

	t.Run("focus and blur flip the flag", func(t *testing.T) {
		p := paneAt(config.ContextExplorer, 200)
		if p.focused {
			t.Error("a new pane starts focused")
		}
		p.Focus()
		if !p.focused {
			t.Error("Focus() did not set the flag")
		}
		p.Blur()
		if p.focused {
			t.Error("Blur() did not clear the flag")
		}
	})

	t.Run("go-back state is remembered", func(t *testing.T) {
		p := paneAt(config.ContextGrid, 200)
		p.SetCanGoBack(true)
		if !p.canGoBack {
			t.Error("SetCanGoBack(true) did not take")
		}
	})
}

// Scenario: El panel no maneja mensajes: ignora todo lo que le llegue.
func TestUpdateHandlesNothing(t *testing.T) {
	p := paneAt(config.ContextExplorer, 120)
	for _, msg := range []tea.Msg{
		nil,
		tea.KeyPressMsg{Code: 'x'},
		tea.KeyPressMsg{Code: tea.KeyEnter},
		tea.WindowSizeMsg{Width: 10, Height: 10},
		"a string",
		42,
	} {
		cmd, handled := p.Update(msg)
		if cmd != nil {
			t.Errorf("Update(%T) returned a command", msg)
		}
		if handled {
			t.Errorf("Update(%T) claimed to handle the message", msg)
		}
	}
	// And it really is inert: a key press must not change what is drawn.
	before := p.View()
	p.Update(tea.KeyPressMsg{Code: 'q'})
	if p.View() != before {
		t.Error("a key press changed the pane")
	}
}

// Scenario: Un panel sin registro no rompe nada.
func TestAPaneWithNoRegistryIsEmpty(t *testing.T) {
	// NewKeybindsPane takes a Resolver interface, so nil is a legal argument and a
	// caller can produce it. renderLines returned nil for it, which View then joins
	// into "" — fine — but the assertion is that this path does not panic, because
	// the panic would happen during the first render, in the user's terminal.
	p := &KeybindsPane{styles: theme.Resolve("dark").Styles(), visible: true, width: 120}
	if got := p.renderLines(); got != nil {
		t.Errorf("a pane with no registry has lines %v, want none", got)
	}
	view := p.View()
	if renderedRows(view) != 0 && !strings.Contains(view, "Keybinds") {
		t.Errorf("a pane with no registry rendered unexpected content: %q", view)
	}
	if p.Height() != len(p.renderLines())+2 {
		t.Errorf("a pane with no registry reserves %d rows for %d content lines", p.Height(), len(p.renderLines()))
	}
}

// Scenario: Un grupo sin tecla primaria no ocupa una celda vacia.
func TestAGroupWithNoPrimaryKeyIsSkipped(t *testing.T) {
	// GroupActions can return a group whose KeyText is empty — an action with no
	// binding at all, or one whose only binding is an alias. Showing it would put
	// " Label" on the status bar with no key in front of it, which reads as a
	// rendering fault rather than as "this action has no key".
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{
		Custom: map[string]string{"filter_tables": ""},
	})
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), kb)
	p.SetWidth(200)
	p.SetFocus(config.ContextExplorer)

	for _, line := range p.renderLines() {
		if strings.HasPrefix(strings.TrimSpace(line), " ") {
			t.Errorf("a line starts with a space, so a keyless group was rendered: %q", line)
		}
		if strings.Contains(line, " ·  ") || strings.HasSuffix(line, " ·") {
			t.Errorf("a line has an empty segment beside the separator: %q", line)
		}
	}
}

// Scenario: Una vista sin ningun atajo produce una linea vacia, no un hueco.
func TestAViewWithNoActionsRendersOneEmptyLine(t *testing.T) {
	// The pane is a box with a border. A context with no actions must still occupy
	// the border, or the box collapses to nothing and the layout loses its rows —
	// so renderLines returns [""], a single empty line, rather than nil.
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{})
	p := NewKeybindsPane(theme.Resolve("dark").Styles(), kb)
	p.SetWidth(200)
	p.SetFocus("a-context-with-no-actions")

	lines := p.renderLines()
	if len(lines) != 1 {
		t.Errorf("a view with no actions has %d lines (%v), want one empty line", len(lines), lines)
	}
	if p.Height() != 3 {
		t.Errorf("a view with no actions reserves %d rows, want an empty line plus two borders", p.Height())
	}
	if p.View() == "" {
		t.Error("a view with no actions renders nothing at all")
	}
}

func drawnOrNot(drawn bool) string {
	if drawn {
		return "is drawn"
	}
	return "is not drawn"
}
