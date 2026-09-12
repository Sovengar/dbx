package theme

func darkTheme() *Theme {
	return &Theme{
		Name:       "dark",
		Background: c("#1e1e2e"),
		Foreground: c("#cdd6f4"),
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

		Pending: c("#4ade80"),

		Text:       c("#cdd6f4"),
		TextMuted:  c("#6c7086"),
		TextBright: c("#f5f5f5"),
	}
}

func lightTheme() *Theme {
	return &Theme{
		Name:       "light",
		Background: c("#eff1f5"),
		Foreground: c("#4c4f69"),
		Primary:    c("#1e66f5"),
		Secondary:  c("#8839ef"),

		Success: c("#40a02b"),
		Warning: c("#df8e1d"),
		Error:   c("#d20f39"),
		Info:    c("#209fb5"),

		Border:       c("#bcc0cc"),
		BorderActive: c("#1e66f5"),

		BackgroundPanel:    c("#e6e9ef"),
		BackgroundElement:  c("#ccd0da"),
		BackgroundSelected: c("#bcc0cc"),
		BackgroundCursor:   c("#9ca0b0"),

		Pending: c("#2e7d32"),

		Text:       c("#4c4f69"),
		TextMuted:  c("#9ca0b0"),
		TextBright: c("#dc8a78"),
	}
}

func nordTheme() *Theme {
	return &Theme{
		Name:       "nord",
		Background: c("#2e3440"),
		Foreground: c("#d8dee9"),
		Primary:    c("#88c0d0"),
		Secondary:  c("#b48ead"),

		Success: c("#a3be8c"),
		Warning: c("#ebcb8b"),
		Error:   c("#bf616a"),
		Info:    c("#81a1c1"),

		Border:       c("#4c566a"),
		BorderActive: c("#88c0d0"),

		BackgroundPanel:    c("#242933"),
		BackgroundElement:  c("#3b4252"),
		BackgroundSelected: c("#434c5e"),
		BackgroundCursor:   c("#4c566a"),

		Pending: c("#a3be8c"),

		Text:       c("#d8dee9"),
		TextMuted:  c("#616e88"),
		TextBright: c("#eceff4"),
	}
}

func gruvboxTheme() *Theme {
	return &Theme{
		Name:       "gruvbox",
		Background: c("#282828"),
		Foreground: c("#ebdbb2"),
		Primary:    c("#d79921"),
		Secondary:  c("#b16286"),

		Success: c("#98971a"),
		Warning: c("#fabd2f"),
		Error:   c("#fb4934"),
		Info:    c("#83a598"),

		Border:       c("#504945"),
		BorderActive: c("#d79921"),

		BackgroundPanel:    c("#1d2021"),
		BackgroundElement:  c("#3c3836"),
		BackgroundSelected: c("#504945"),
		BackgroundCursor:   c("#665c54"),

		Pending: c("#98971a"),

		Text:       c("#ebdbb2"),
		TextMuted:  c("#928374"),
		TextBright: c("#fbf1c7"),
	}
}

func catppuccinTheme() *Theme {
	return &Theme{
		Name:       "catppuccin",
		Background: c("#1e1e2e"),
		Foreground: c("#cdd6f4"),
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

		Pending: c("#a6e3a1"),

		Text:       c("#cdd6f4"),
		TextMuted:  c("#6c7086"),
		TextBright: c("#f5f5f5"),
	}
}
