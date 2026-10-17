package theme

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain pins the one signal system detection reads from the process environment before
// any test runs.
//
// Resolve("system") is reached all over this package — and by the contract test, which
// compares the fallback against the literal "system" mode — so without a pin the result
// would be whatever the developer's own terminal reports. COLORFGBG is set to a dark
// background here, which keeps the real path deterministic; the detection branches
// themselves are exercised through detectSystemWith with fake probes below.
func TestMain(m *testing.M) {
	if err := os.Setenv("COLORFGBG", "15;0"); err != nil {
		fmt.Fprintf(os.Stderr, "cannot pin COLORFGBG for the suite: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestTerminalBackground pins the COLORFGBG half of the detection: which background
// indices count as light, which as dark, and which are not an answer at all.
func TestTerminalBackground(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		light bool
		ok    bool
	}{
		{"dark background 0", "15;0", false, true},
		{"dark background 6", "15;6", false, true},
		{"bright black 8 is dark", "7;8", false, true},
		{"light background 7", "0;7", true, true},
		{"light background 9", "0;9", true, true},
		{"light background 15", "0;15", true, true},
		{"out of range 16", "15;16", false, false},
		{"negative index", "15;-1", false, false},
		{"empty", "", false, false},
		{"no separator", "15", false, false},
		{"non numeric background", "15;default", false, false},
		{"only a separator", ";", false, false},
		{"separator at index 0", ";8", false, true},
		{"two digits are not a colour", "0;100", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			light, ok := terminalBackground(tc.value)
			if light != tc.light || ok != tc.ok {
				t.Errorf("terminalBackground(%q) = (%v, %v), want (%v, %v)",
					tc.value, light, ok, tc.light, tc.ok)
			}
		})
	}
}

// TestDetectSystemPrefersTheTerminal is the precedence test: the surface being drawn on
// outranks the desktop setting, and when it answers the desktop is never asked.
func TestDetectSystemPrefersTheTerminal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		light bool
	}{
		{"light terminal", "0;15", true},
		{"dark terminal", "15;0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := detectSystemWith(systemProbe{
				getenv: func(key string) string {
					if key == "COLORFGBG" {
						return tc.value
					}
					return ""
				},
				run: func(name string, args ...string) (string, error) {
					t.Fatalf("detection asked the desktop (%s %v) although the terminal answered", name, args)
					return "", nil
				},
				goos: "linux",
			})
			assertSystemPalette(t, got, tc.light)
		})
	}
}

// TestDetectSystemUsesTheDesktopAnswer closes the loop between the two halves: with no
// terminal answer, the palette the desktop reports is the palette that is returned.
func TestDetectSystemUsesTheDesktopAnswer(t *testing.T) {
	got := detectSystemWith(systemProbe{
		getenv: func(string) string { return "" },
		run: func(name string, args ...string) (string, error) {
			return "'prefer-light'\n", nil
		},
		goos: "linux",
	})
	assertSystemPalette(t, got, true)
}

// TestDetectSystemAsksTheDesktop covers the environments where COLORFGBG is absent, which
// is the common case on kitty, wezterm, alacritty and tmux.
func TestDetectSystemAsksTheDesktop(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goos   string
		args   []string
		answer string
		err    error
		light  bool
		wantOK bool
	}{
		{
			name: "gnome prefers dark", goos: "linux",
			args:   []string{"gsettings", "get", "org.gnome.desktop.interface", "color-scheme"},
			answer: "'prefer-dark'\n", light: false, wantOK: true,
		},
		{
			name: "gnome prefers light", goos: "linux",
			args:   []string{"gsettings", "get", "org.gnome.desktop.interface", "color-scheme"},
			answer: "'prefer-light'\n", light: true, wantOK: true,
		},
		{
			name: "gnome default is light", goos: "linux",
			args:   []string{"gsettings", "get", "org.gnome.desktop.interface", "color-scheme"},
			answer: "'default'\n", light: true, wantOK: true,
		},
		{
			name: "gnome unavailable", goos: "linux",
			args:  []string{"gsettings", "get", "org.gnome.desktop.interface", "color-scheme"},
			err:   errors.New("exec: \"gsettings\": executable file not found in $PATH"),
			light: false, wantOK: false,
		},
		{
			name: "macos dark mode", goos: "darwin",
			args:   []string{"defaults", "read", "-g", "AppleInterfaceStyle"},
			answer: "Dark\n", light: false, wantOK: true,
		},
		{
			name: "macos light mode has no key", goos: "darwin",
			args: []string{"defaults", "read", "-g", "AppleInterfaceStyle"},
			err:  errors.New("The domain/default pair of (kCFPreferencesAnyApplication, AppleInterfaceStyle) does not exist"),
			// The absent key IS light mode on macOS, so it answers rather than falling
			// through to the dark default.
			light: true, wantOK: true,
		},
		{
			name: "macos answers something else", goos: "darwin",
			args:   []string{"defaults", "read", "-g", "AppleInterfaceStyle"},
			answer: "Blue\n", light: true, wantOK: true,
		},
		{
			name: "no desktop to ask", goos: "windows",
			args: nil, light: false, wantOK: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			light, ok := desktopBackground(systemProbe{
				getenv: func(string) string { return "" },
				run: func(name string, args ...string) (string, error) {
					if tc.args == nil {
						t.Fatalf("detection ran %s on %s, an OS it does not support", name, tc.goos)
					}
					if got := append([]string{name}, args...); !equalStrings(got, tc.args) {
						t.Fatalf("detection ran %v, want %v", got, tc.args)
					}
					return tc.answer, tc.err
				},
				goos: tc.goos,
			})
			if light != tc.light || ok != tc.wantOK {
				t.Errorf("desktopBackground on %s = (%v, %v), want (%v, %v)",
					tc.goos, light, ok, tc.light, tc.wantOK)
			}
		})
	}
}

// TestDetectSystemFallsBackToDark pins the behaviour of an environment that answers
// nothing: the same dark palette the stub returned, so nobody's colours change on upgrade.
func TestDetectSystemFallsBackToDark(t *testing.T) {
	got := detectSystemWith(systemProbe{
		getenv: func(string) string { return "" },
		run: func(name string, args ...string) (string, error) {
			t.Fatalf("detection ran %s, which is not an OS query it supports", name)
			return "", nil
		},
		goos: "windows",
	})
	assertSystemPalette(t, got, false)
}

// TestSystemThemeKeepsTheNameAndThePalette checks the seam the picker depends on: the
// palette follows the detection, the name stays "system".
func TestSystemThemeKeepsTheNameAndThePalette(t *testing.T) {
	for _, light := range []bool{false, true} {
		t.Run(fmt.Sprintf("light=%v", light), func(t *testing.T) {
			got := systemTheme(light)
			assertSystemPalette(t, got, light)
		})
	}
}

// TestRunCommandReadsStdout covers the real probe the tests above replace: the query
// itself, both when it succeeds and when it fails.
func TestRunCommandReadsStdout(t *testing.T) {
	out, err := runCommand("sh", "-c", "printf light")
	if err != nil {
		t.Fatalf("runCommand on a working command: %v", err)
	}
	if out != "light" {
		t.Errorf("runCommand returned %q, want %q", out, "light")
	}

	if _, err := runCommand("sh", "-c", "exit 3"); err == nil {
		t.Error("runCommand on a failing command reported no error; a broken query would be read as an answer")
	}
}

// TestIsDark pins the desktop answer parser against the shapes the two OSes produce.
func TestIsDark(t *testing.T) {
	for _, tc := range []struct {
		answer string
		want   bool
	}{
		{"Dark\n", true},
		{"dark", true},
		{" DARK ", true},
		{"Light", false},
		{"", false},
		{"Darkish", false},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			if got := isDark(tc.answer); got != tc.want {
				t.Errorf("isDark(%q) = %v, want %v", tc.answer, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------

// assertSystemPalette checks both halves of the contract: the detected palette and the
// name the picker shows.
func assertSystemPalette(t *testing.T, got *Theme, wantLight bool) {
	t.Helper()
	if got == nil {
		t.Fatal("detection returned nil")
	}
	if got.Name != "system" {
		t.Errorf("the detected theme is named %q, want %q", got.Name, "system")
	}
	want := darkTheme()
	if wantLight {
		want = lightTheme()
	}
	if colourKey(got) != colourKey(want) {
		t.Errorf("the detected theme is not the %s palette", map[bool]string{true: "light", false: "dark"}[wantLight])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}
