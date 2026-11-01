package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/ui/keydisplay"
)

// Scenario: The key of a keybind hint uses the theme accent while its
// description keeps the colour it had before.
func TestKeybindsPane_KeyUsesAccentAndDescriptionKeepsHelpColour(t *testing.T) {
	p := paneAt(config.ContextExplorer, 200)
	st := p.styles

	key := keydisplay.Key("/")
	label := "Filter Tables"
	want := st.Primary.Render(key) + " " + st.Help.Render(label)

	view := p.View()
	if !strings.Contains(view, want) {
		t.Errorf("the rendered pane does not contain the accent key and help description %q:\n%s", want, view)
	}
	if strings.Contains(view, st.Primary.Render(label)) {
		t.Errorf("the description %q was painted with the accent colour:\n%s", label, view)
	}
	if strings.Contains(view, st.Help.Render(key+" "+label)) {
		t.Errorf("the key and description are still painted together with the help colour:\n%s", view)
	}
}

// Scenario: A non-keybind note in the pane keeps the help colour.
func TestKeybindsPane_NonKeySegmentKeepsHelpColour(t *testing.T) {
	p := paneAt(config.ContextGrid, 200)
	p.SetTxPending(true)
	st := p.styles

	if want := st.Help.Render("tx pending"); !strings.Contains(p.View(), want) {
		t.Errorf("the tx note is not painted with the help colour %q:\n%s", want, p.View())
	}
	if strings.Contains(p.View(), st.Primary.Render("tx pending")) {
		t.Errorf("the tx note was painted with the accent colour:\n%s", p.View())
	}
}

// Scenario: A segment keeps its plain projection when only one of its parts is
// present, which is what renderLines uses for width arithmetic.
func TestKeybindSegment_PlainHandlesEmptyParts(t *testing.T) {
	for _, tc := range []struct {
		name string
		seg  keybindSegment
		want string
	}{
		{"label only", keybindSegment{label: "tx pending"}, "tx pending"},
		{"key only", keybindSegment{key: "x"}, "x"},
		{"key and label", keybindSegment{key: "x", label: "Yank"}, "x Yank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.seg.plain(); got != tc.want {
				t.Errorf("plain() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Scenario: Styling a segment with only one part paints that part with the same
// colour it has in a full key and description pair.
func TestKeybindsPane_StyledSegmentHandlesEmptyParts(t *testing.T) {
	p := paneAt(config.ContextExplorer, 200)
	st := p.styles

	for _, tc := range []struct {
		name string
		seg  keybindSegment
		want string
	}{
		{"label only", keybindSegment{label: "tx pending"}, st.Help.Render("tx pending")},
		{"key only", keybindSegment{key: "x"}, st.Primary.Render("x")},
		{"key and label", keybindSegment{key: "x", label: "Yank"}, st.Primary.Render("x") + " " + st.Help.Render("Yank")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.renderStyledSegment(tc.seg); got != tc.want {
				t.Errorf("renderStyledSegment() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Scenario: Styling never enters the width arithmetic: renderLines stays plain
// text and the rendered box still fits the width it was given.
func TestKeybindsPane_StyledViewKeepsPlainWidthArithmetic(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		p := paneAt(config.ContextExplorer, width)

		for i, line := range p.renderLines() {
			if strings.ContainsRune(line, '\x1b') {
				t.Errorf("width %d: renderLines line %d contains ANSI escapes: %q", width, i, line)
			}
		}

		rows := strings.Split(p.View(), "\n")
		if len(rows) != p.Height() {
			t.Errorf("width %d: the styled view has %d rows, Height() reserves %d", width, len(rows), p.Height())
		}
		for i, row := range rows {
			if got := lipgloss.Width(row); got > width {
				t.Errorf("width %d: rendered row %d is %d columns wide: %q", width, i, got, row)
			}
		}
	}
}
