package theme

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// systemProbe is every observation system detection is allowed to make.
//
// It is a value rather than a set of direct calls to os.Getenv and exec.Command so the
// suite can inject a fake environment: a detection that read the developer's real terminal
// would pass or fail by machine, and the detection itself could never be tested at all.
type systemProbe struct {
	// getenv reads one environment variable.
	getenv func(string) string
	// run executes an external query and returns its stdout.
	run func(name string, args ...string) (string, error)
	// goos is the operating system to ask, spelled like runtime.GOOS.
	goos string
}

// detectSystem is the entry point used by Resolve.
func detectSystem() *Theme {
	return detectSystemWith(systemProbe{
		getenv: os.Getenv,
		run:    runCommand,
		goos:   runtime.GOOS,
	})
}

// runCommand is the real external query behind systemProbe.run.
func runCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// detectSystemWith resolves the system theme from the injected environment.
//
// Precedence: the terminal's own answer first, because that is the surface being drawn on;
// then the desktop colour scheme; then dark, which is the palette the old stub always
// returned, so an environment that answers nothing keeps the colours users already have.
func detectSystemWith(p systemProbe) *Theme {
	if light, ok := terminalBackground(p.getenv("COLORFGBG")); ok {
		return systemTheme(light)
	}
	if light, ok := desktopBackground(p); ok {
		return systemTheme(light)
	}
	return systemTheme(false)
}

// terminalBackground parses COLORFGBG, the "fg;bg" pair that rxvt, konsole and others
// export. Only the last field, the background index, matters: ANSI 0-6 and 8 are the dark
// half of the palette, 7 and 9-15 the light half. Anything else — a missing variable, a
// non-numeric field, an out-of-range index — reports "unknown" so the caller falls through
// instead of guessing.
func terminalBackground(colorfgbg string) (light, ok bool) {
	i := strings.LastIndexByte(colorfgbg, ';')
	if i < 0 {
		return false, false
	}
	bg, err := strconv.Atoi(colorfgbg[i+1:])
	if err != nil {
		return false, false
	}
	switch {
	case bg >= 0 && bg <= 6, bg == 8:
		return false, true
	case bg == 7, bg >= 9 && bg <= 15:
		return true, true
	}
	return false, false
}

// desktopBackground asks the desktop environment for its colour scheme.
//
// A failed query is "unknown", except on macOS, where the absence of AppleInterfaceStyle
// is exactly how light mode is spelled: `defaults read -g` errors when the key is unset.
func desktopBackground(p systemProbe) (light, ok bool) {
	switch p.goos {
	case "darwin":
		out, err := p.run("defaults", "read", "-g", "AppleInterfaceStyle")
		if err != nil {
			return true, true
		}
		return !isDark(out), true
	case "linux":
		out, err := p.run("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
		if err != nil {
			return false, false
		}
		// The schema answers "prefer-dark", "prefer-light" or the older "default"; the
		// last two are both light, so only the presence of "dark" needs checking.
		return !strings.Contains(out, "dark"), true
	}
	return false, false
}

// isDark reports whether a desktop answer is the dark one, tolerating the trailing newline
// `defaults` prints and the capitalisation macOS uses.
func isDark(out string) bool {
	return strings.EqualFold(strings.TrimSpace(out), "dark")
}

// systemTheme returns the designed palette that matches the detected background.
//
// It is the dark or light theme under the name "system", because the theme picker has to
// keep showing which mode the user selected. Reusing the designed palettes keeps a single
// hand-tuned accent set per background instead of a second one that would drift.
func systemTheme(light bool) *Theme {
	t := darkTheme()
	if light {
		t = lightTheme()
	}
	t.Name = "system"
	return t
}
