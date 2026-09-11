package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type Theme struct {
	Name string

	Background color.Color
	Foreground color.Color
	Primary    color.Color
	Secondary  color.Color

	Success color.Color
	Warning color.Color
	Error   color.Color
	Info    color.Color

	Border       color.Color
	BorderActive color.Color

	BackgroundPanel    color.Color
	BackgroundElement  color.Color
	BackgroundSelected color.Color

	Text       color.Color
	TextMuted  color.Color
	TextBright color.Color
}

func Resolve(mode string) *Theme {
	switch mode {
	case "dark":
		return darkTheme()
	case "light":
		return lightTheme()
	case "nord":
		return nordTheme()
	case "gruvbox":
		return gruvboxTheme()
	case "catppuccin":
		return catppuccinTheme()
	case "system":
		return detectSystem()
	default:
		return detectSystem()
	}
}

func (t *Theme) Styles() *Styles {
	return NewStyles(t)
}

func c(hex string) color.Color {
	return lipgloss.Color(hex)
}
