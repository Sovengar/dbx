package theme

import "charm.land/lipgloss/v2"

type Styles struct {
	Border       lipgloss.Style
	BorderActive lipgloss.Style

	Panel    lipgloss.Style
	Element  lipgloss.Style
	Selected lipgloss.Style
	Cursor   lipgloss.Style

	Text       lipgloss.Style
	TextMuted  lipgloss.Style
	TextBright lipgloss.Style

	Primary lipgloss.Style
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style

	Header    lipgloss.Style
	StatusBar lipgloss.Style
	Help      lipgloss.Style
	Sep       lipgloss.Style
	Title     lipgloss.Style

	TabActive   lipgloss.Style
	TabInactive lipgloss.Style
}

func NewStyles(t *Theme) *Styles {
	return &Styles{
		Border: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(t.Border),

		BorderActive: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(t.BorderActive),

		Panel: lipgloss.NewStyle().
			Background(t.BackgroundPanel),

		Element: lipgloss.NewStyle().
			Background(t.BackgroundElement),

		Selected: lipgloss.NewStyle().
			Background(t.BackgroundSelected).
			Foreground(t.Foreground),

		Cursor: lipgloss.NewStyle().
			Background(t.BackgroundCursor).
			Foreground(t.Foreground),

		Text: lipgloss.NewStyle().
			Foreground(t.Text),

		TextMuted: lipgloss.NewStyle().
			Foreground(t.TextMuted),

		TextBright: lipgloss.NewStyle().
			Foreground(t.TextBright),

		Success: lipgloss.NewStyle().
			Foreground(t.Success),

		Warning: lipgloss.NewStyle().
			Foreground(t.Warning),

		Error: lipgloss.NewStyle().
			Foreground(t.Error),

		Info: lipgloss.NewStyle().
			Foreground(t.Info),

		Primary: lipgloss.NewStyle().
			Foreground(t.Primary),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),

		StatusBar: lipgloss.NewStyle().
			Background(t.BackgroundElement).
			Foreground(t.TextMuted).
			Padding(0, 1),

		Help: lipgloss.NewStyle().
			Foreground(t.TextMuted).
			Faint(true),

		Sep: lipgloss.NewStyle().
			Foreground(t.TextMuted),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary).
			Background(t.BackgroundPanel).
			Padding(0, 1),

		TabActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(t.Primary).
			Padding(0, 1),

		TabInactive: lipgloss.NewStyle().
			Foreground(t.TextMuted).
			Padding(0, 1),
	}
}
