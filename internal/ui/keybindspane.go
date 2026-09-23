package ui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
)

// maxKeybindsPerLine caps how many keybind segments the pane packs into a
// single line. A group (e.g. "hjkl Navigate") counts as one segment. Extra
// segments flow onto a new line below.
const maxKeybindsPerLine = 7

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
}

func NewKeybindsPane(styles *theme.Styles, keybinds config.Resolver) *KeybindsPane {
	return &KeybindsPane{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (s *KeybindsPane) SetWidth(w int)                { s.width = w }
func (s *KeybindsPane) SetHeight(h int)               { s.height = h }
func (s *KeybindsPane) SetFocus(f string)             { s.focus = f }
func (s *KeybindsPane) SetEditorOpen(open bool)       { s.editorOpen = open }
func (s *KeybindsPane) SetAutocompleteReady(r bool)   { s.autocompleteReady = r }
func (s *KeybindsPane) SetQueryBrowserOpen(open bool) { s.queryBrowserOpen = open }
func (s *KeybindsPane) SetTxPending(pending bool)     { s.txPending = pending }
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
	if s.width <= 0 {
		return ""
	}

	lines := s.renderLines()

	var parts []string
	for _, l := range lines {
		parts = append(parts, s.styles.Help.Render(l))
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

// renderLines builds the segments for the active context and wraps them to the
// pane width. It is also what the panel tests assert against.
func (s *KeybindsPane) renderLines() []string {
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
		active = append(active, a)
	}

	// One segment per display group; the pane shows primary keys only (the help
	// modal is where aliases live).
	var segments []string
	for _, g := range config.GroupActions(active, s.keybinds.PrimaryKey) {
		if g.KeyText == "" {
			continue
		}
		segments = append(segments, g.KeyText+" "+g.Label)
	}
	if s.txPending {
		segments = append(segments, "tx pending")
	}

	if len(segments) == 0 {
		return []string{""}
	}

	if s.width <= 0 {
		return []string{strings.Join(segments, " · ")}
	}

	// Wrap segments into lines. A line breaks either when it reaches
	// maxKeybindsPerLine entries or when the next segment would overflow the
	// pane width, whichever comes first.
	var lines []string
	current := ""
	count := 0
	for _, seg := range segments {
		if current != "" && (count >= maxKeybindsPerLine || lipgloss.Width(current+" · "+seg) > s.width-2) {
			lines = append(lines, current)
			current = seg
			count = 1
			continue
		}
		if current == "" {
			current = seg
		} else {
			current += " · " + seg
		}
		count++
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
