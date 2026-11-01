package ui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
	"github.com/buble/dbx/internal/ui/keydisplay"
)

// maxKeybindsPerLine caps how many keybind segments the pane packs into a
// single line. A group (e.g. "hjkl Navigate") counts as one segment. Extra
// segments flow onto a new line below.
const maxKeybindsPerLine = 7

// keybindSegment is one key hint, kept split so the key and the description can
// be painted independently without styling entering the plain-text width math.
type keybindSegment struct {
	key   string
	label string
}

func (s keybindSegment) plain() string {
	switch {
	case s.key == "":
		return s.label
	case s.label == "":
		return s.key
	default:
		return s.key + " " + s.label
	}
}

// KeybindsPane renders the keybinds of the currently focused view, derived
// entirely from the keybind registry (no hardcoded key strings).
type KeybindsPane struct {
	styles            *theme.Styles
	keybinds          config.Resolver
	width             int
	height            int
	focused           bool
	focus             string
	editorOpen        bool
	autocompleteReady bool
	queryBrowserOpen  bool
	txPending         bool
	canGoBack         bool
	visible           bool
}

// NewKeybindsPane builds the pane with the help VISIBLE, which is the default and was
// the only behaviour until ui.statusbar_help was wired. A caller that does not set
// visibility gets the pane it always had.
func NewKeybindsPane(styles *theme.Styles, keybinds config.Resolver) *KeybindsPane {
	return &KeybindsPane{
		styles:   styles,
		keybinds: keybinds,
		visible:  true,
	}
}

// SetVisible shows or hides the pane. Driven by ui.statusbar_help, which was declared
// with a default and read by nothing: a user who turned the help off kept getting it.
//
// Hidden is not the same as zero-height. Height reports 0 so the caller can give the
// space to the content, and View returns "" so nothing is drawn — both halves, because
// one without the other leaves either a blank strip at the bottom or a layout that still
// reserves the rows.
func (s *KeybindsPane) SetVisible(v bool) { s.visible = v }

// Visible reports whether the pane will draw.
func (s *KeybindsPane) Visible() bool { return s.visible }

// Height is how many terminal rows the pane occupies, borders included, or 0 when it
// will not draw.
//
// It exists because the layout reserved a HARD-CODED 7 rows for the status bar while the
// pane renders a variable number of wrapped lines — one per maxKeybindsPerLine segments,
// so it grows and shrinks with the width, the focused view and how many actions that view
// hides. Same arithmetic written in two places and left to drift, for the fourth time in
// this repository.
func (s *KeybindsPane) Height() int {
	if !s.visible || s.width <= 0 {
		return 0
	}
	// The rounded border contributes a top and a bottom row.
	return len(s.renderLines()) + 2
}

func (s *KeybindsPane) SetWidth(w int)                { s.width = w }
func (s *KeybindsPane) SetHeight(h int)               { s.height = h }
func (s *KeybindsPane) SetFocus(f string)             { s.focus = f }
func (s *KeybindsPane) SetEditorOpen(open bool)       { s.editorOpen = open }
func (s *KeybindsPane) SetAutocompleteReady(r bool)   { s.autocompleteReady = r }
func (s *KeybindsPane) SetQueryBrowserOpen(open bool) { s.queryBrowserOpen = open }
func (s *KeybindsPane) SetTxPending(pending bool)     { s.txPending = pending }
func (s *KeybindsPane) SetCanGoBack(can bool)         { s.canGoBack = can }
func (s *KeybindsPane) Focus()                        { s.focused = true }
func (s *KeybindsPane) Blur()                         { s.focused = false }

func (s *KeybindsPane) Update(msg tea.Msg) (tea.Cmd, bool) {
	return nil, false
}

// Context returns the view whose actions the pane renders. The editor and the
// query browser are logical contexts determined by their open state, not by
// the router focus.
func (s *KeybindsPane) Context() string {
	if s.queryBrowserOpen {
		return config.ContextQueryBrowser
	}
	if s.editorOpen {
		return config.ContextEditor
	}
	if s.focus == "" {
		return config.ContextExplorer
	}
	return s.focus
}

func (s *KeybindsPane) View() string {
	if !s.visible || s.width <= 0 {
		return ""
	}

	lines := s.renderSegmentLines()

	var parts []string
	for _, line := range lines {
		parts = append(parts, s.renderStyledSegments(line))
	}
	content := strings.Join(parts, "\n")

	border := lipgloss.RoundedBorder()
	var borderFg color.Color
	if s.focused {
		borderFg = s.styles.BorderActive.GetBorderTopForeground()
	} else {
		borderFg = s.styles.Border.GetBorderTopForeground()
	}

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Keybinds ", content, s.width, 0)
}

// renderStyledSegments paints a line's keys with the accent colour and its
// descriptions with the help colour, keeping the separator in the help colour.
func (s *KeybindsPane) renderStyledSegments(segments []keybindSegment) string {
	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		parts = append(parts, s.renderStyledSegment(seg))
	}
	return strings.Join(parts, s.styles.Help.Render(" · "))
}

func (s *KeybindsPane) renderStyledSegment(seg keybindSegment) string {
	switch {
	case seg.key == "":
		return s.styles.Help.Render(seg.label)
	case seg.label == "":
		return s.styles.Primary.Render(seg.key)
	default:
		return s.styles.Primary.Render(seg.key) + " " + s.styles.Help.Render(seg.label)
	}
}

// renderLines builds the plain-text segments for the active context and wraps
// them to the pane width. It is also what the panel tests assert against.
func (s *KeybindsPane) renderLines() []string {
	lines := s.renderSegmentLines()
	if lines == nil {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		parts := make([]string, len(line))
		for j, seg := range line {
			parts[j] = seg.plain()
		}
		out[i] = strings.Join(parts, " · ")
	}
	return out
}

// renderSegmentLines is the unstyled, wrapped source of renderLines.
func (s *KeybindsPane) renderSegmentLines() [][]keybindSegment {
	if s.keybinds == nil {
		return nil
	}

	ctx := s.Context()

	// Conditional skips are applied BEFORE grouping so a hidden member never
	// keeps a group alive on its own.
	var active []config.Action
	for _, a := range s.keybinds.ActionsFor(ctx) {
		if a.ID == "rollback" && !s.txPending {
			continue
		}
		if a.ID == "autocomplete" && s.editorOpen && !s.autocompleteReady {
			continue
		}
		if a.ID == "go_back" && !s.canGoBack {
			continue
		}
		active = append(active, a)
	}

	// One segment per display group; the pane shows primary keys only (the help
	// modal is where aliases live).
	var segments []keybindSegment
	for _, g := range config.GroupActions(active, s.keybinds.PrimaryKey) {
		if g.KeyText == "" {
			continue
		}
		segments = append(segments, keybindSegment{key: keydisplay.Key(g.KeyText), label: g.Label})
	}
	if s.txPending {
		segments = append(segments, keybindSegment{label: "tx pending"})
	}

	if len(segments) == 0 {
		return [][]keybindSegment{{}}
	}

	if s.width <= 0 {
		return [][]keybindSegment{segments}
	}

	// Wrap segments into lines. A line breaks either when it reaches
	// maxKeybindsPerLine entries or when the next segment would overflow the
	// pane width, whichever comes first.
	var lines [][]keybindSegment
	var current []keybindSegment
	currentText := ""
	count := 0
	for _, seg := range segments {
		if currentText != "" && (count >= maxKeybindsPerLine || lipgloss.Width(currentText+" · "+seg.plain()) > s.width-2) {
			lines = append(lines, current)
			current = []keybindSegment{seg}
			currentText = seg.plain()
			count = 1
			continue
		}
		if currentText == "" {
			current = []keybindSegment{seg}
			currentText = seg.plain()
		} else {
			current = append(current, seg)
			currentText += " · " + seg.plain()
		}
		count++
	}
	if currentText != "" {
		lines = append(lines, current)
	}
	return lines
}
