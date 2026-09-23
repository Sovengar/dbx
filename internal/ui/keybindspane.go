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
	var segments []string
	for _, a := range s.keybinds.ActionsFor(ctx) {
		if a.ID == "rollback" && !s.txPending {
			continue
		}
		if a.ID == "autocomplete" && s.editorOpen && !s.autocompleteReady {
			continue
		}
		key := s.keybinds.PrimaryKey(a.ID)
		if key == "" {
			continue
		}
		segments = append(segments, key+" "+a.Description)
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

	// Wrap segments into lines that fit the pane width.
	var lines []string
	current := ""
	for _, seg := range segments {
		candidate := seg
		if current != "" {
			candidate = current + " · " + seg
		}
		if current != "" && lipgloss.Width(candidate) > s.width-2 {
			lines = append(lines, current)
			current = seg
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
