package theme

func detectSystem() *Theme {
	bg, fg := detectTerminalColors()
	return generateFromColors(bg, fg)
}

func detectTerminalColors() (string, string) {
	return "#1e1e2e", "#cdd6f4"
}

func generateFromColors(bg, fg string) *Theme {
	return &Theme{
		Name:       "system",
		Background: c(bg),
		Foreground: c(fg),
		Primary:    c("#89b4fa"),
		Secondary:  c("#cba6f7"),

		Success: c("#a6e3a1"),
		Warning: c("#f9e2af"),
		Error:   c("#f38ba8"),
		Info:    c("#89dceb"),

		Border:       c("#585b70"),
		BorderActive: c("#89b4fa"),

		BackgroundPanel:    c("#181825"),
		BackgroundElement:  c("#313244"),
		BackgroundSelected: c("#45475a"),
		BackgroundCursor:   c("#585b70"),

		Text:       c(fg),
		TextMuted:  c("#6c7086"),
		TextBright: c("#f5f5f5"),
	}
}
