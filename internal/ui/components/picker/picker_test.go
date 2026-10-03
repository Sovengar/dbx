package picker

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/keydisplay"
	"github.com/charmbracelet/x/ansi"
)

func newPickerForTest() *Picker {
	return New(theme.Resolve("dark").Styles())
}

// project builds a discovered project. The port comes out of the DSN, so the DSN
// is the thing under test in getPort and the driver/path are decoration.
func project(name, path, driver, dsn string, active bool) config.FoundProject {
	return config.FoundProject{
		Name:   name,
		Path:   path,
		Active: active,
		Connection: config.ProjectConnection{
			Driver: driver,
			DSN:    dsn,
		},
	}
}

// press sends one key and returns what the picker answered: the message the
// command produces (nil if the command is nil) and whether the key was consumed.
func press(p *Picker, key string) (tea.Msg, bool) {
	cmd, handled := p.Update(keyMsg(key))
	if cmd == nil {
		return nil, handled
	}
	return cmd(), handled
}

func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "j":
		return tea.KeyPressMsg{Code: 'j', Text: "j"}
	case "k":
		return tea.KeyPressMsg{Code: 'k', Text: "k"}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "q":
		return tea.KeyPressMsg{Code: 'q', Text: "q"}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		return tea.KeyPressMsg{Code: 'x', Text: key}
	}
}

// pressed reports whether a key was consumed, without running its command.
func pressed(p *Picker, key string) bool {
	_, handled := p.Update(keyMsg(key))
	return handled
}

// pickerFooter is what the footer line holds once the key labels have gone through
// keydisplay. Built the same way the view builds it, so a change to the glyphs
// breaks this test with a clear message instead of a wall of diff.
func pickerFooter() string {
	return strings.TrimSpace(keydisplay.Key("j/k ↑↓   space toggle   enter select   q quit"))
}

// rows are the project lines of the view, in order, with the title, the blank
// lines and the footer dropped. Going through this rather than through raw line
// numbers is what makes a cursor assertion mean "row 1" instead of "line 3",
// where 0 is the title and 1 is the blank under it.
func rows(view string) []string {
	footer := pickerFooter()
	var out []string
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "dbx" || trimmed == footer {
			continue
		}
		out = append(out, line)
	}
	return out
}

// cursorRow is the index of the project row carrying the cursor arrow, read out of
// the rendered view. The picker's cursor is unexported and this is what the user
// can actually see, so every cursor assertion goes through here rather than through
// the field. A cursor of -1 does not render as a wrong row, it renders as NO row
// having the arrow, which is why this returns -1 and why a test that wanted a row
// can tell the two apart.
func cursorRow(t *testing.T, view string) int {
	t.Helper()
	found := -1
	for i, line := range rows(ansi.Strip(view)) {
		if strings.Contains(line, "▸") {
			if found != -1 {
				t.Fatalf("the view has a cursor arrow on rows %d and %d:\n%s", found, i, view)
			}
			found = i
		}
	}
	return found
}

// --- getPort ----------------------------------------------------------------

// Scenario: El puerto sale del DSN, y solo de ahí.
//
// The picker shows `driver:port` so a user with three local databases can tell
// which is which without opening the file. The port is read out of the DSN by
// hand, so the rules are: the FIRST @, then the FIRST : after it, then the digits
// immediately after that colon. Each of those three steps can fail, and each
// failure has to be an empty result rather than a wrong number — a wrong port
// points the user at the wrong database.
func TestGetPort_FindsThePortAfterTheFirstAt(t *testing.T) {
	p := newPickerForTest()

	for _, tc := range []struct {
		name string
		dsn  string
		want string
	}{
		// The shape the picker is written for.
		{"a full postgres DSN", "postgres://user:pass@localhost:5432/mydb", "5432"},
		{"a mysql DSN", "mysql://root@127.0.0.1:3306/app", "3306"},
		{"a DSN with no path", "postgres://user:pass@host:5432", "5432"},
		{"a DSN with a query string", "postgres://u:p@h:5432/db?sslmode=disable", "5432"},
		{"a DSN with no scheme", "@host:5432", "5432"},
		{"an empty host", "@:5432/db", "5432"},
		{"a port of zeros", "@h:0000", "0000"},
		{"a one digit port", "@h:0", "0"},
		{"a four digit port", "@h:9999", "9999"},

		// No @ at all: nothing after the credentials, so there is no host:port.
		{"no at sign", "host:5432", ""},
		{"no at sign in a full DSN", "postgres://user:pass@", ""},
		{"empty", "", ""},

		// An @ but no colon after it.
		{"at with no colon", "postgres://u:p@host/db", ""},
		{"at and nothing else", "@", ""},
		{"at then colon", "@:", ""},

		// A colon after the @ but no digits, which is the case that has to come
		// back empty instead of empty-looking garbage.
		{"colon then a letter", "@h:abc/db", ""},
		{"colon then nothing", "@h:/db", ""},

		// The FIRST @ wins, and the FIRST colon after it. A password may contain
		// colons; a second @ means the first one was not the credentials
		// separator. Both are pinned because picking the wrong one gives the
		// wrong port rather than none.
		{"a colon in the password is skipped", "postgres://u:p:a:ss@h:5432/db", "5432"},
		{"the first at sign wins", "a@b:1@c:2", "1"},

		// Digits stop at the first non-digit. "12a" is a port of 12 followed by
		// junk, and reading it as 12 is better than reading it as nothing only
		// by accident — this pins the intent.
		{"digits stop at a letter", "@h:12a", "12"},

		// A colon inside the host segment is not a port separator, and the digits
		// rule then rejects what follows. This is a KNOWN LIMITATION, recorded
		// below in its own test; the entry here is what the code does.
		{"a colon in the host gives up", "postgres://u:p@ho:st:5432/db", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.getPort(tc.dsn); got != tc.want {
				t.Errorf("getPort(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}
}

// Scenario: Un puerto con dos puntos de más NO se inventa uno.
//
// KNOWN LIMITATION, pinned because it is a real gap and the failure is silent.
//
// The parser takes the first colon after the @ and requires digits right after it.
// A DSN whose host segment itself contains a colon — the shape a bracketed IPv6
// literal or a `host:alias` has — therefore yields no port at all, even though the
// real port is sitting further along the string. The picker then shows the bare
// driver name with no port, which is a degraded display and not a wrong one, so it
// is recorded rather than fixed. What matters here is that it is a decision on
// record: a change that starts handling this will fail this test rather than pass
// unnoticed.
func TestGetPort_GivesUpOnAHostThatContainsAColon(t *testing.T) {
	p := newPickerForTest()

	// Note what is NOT here: `host:5432` parses fine, because the first colon
	// after the @ is followed by digits. The parser only gives up when the
	// segment before the first colon is not a host — that is, when there is
	// something else in it.
	for _, dsn := range []string{
		"postgres://u:p@ho:st:5432/db",
		"postgres://u:p@[::1]:5432/db",
		"postgres://u:p@a:b:5432/db",
	} {
		if got := p.getPort(dsn); got != "" {
			t.Errorf("getPort(%q) = %q, want \"\": this parser is recorded as giving up on a colon in the host", dsn, got)
		}
	}
}

// --- centerText -------------------------------------------------------------

// Scenario: El texto se centra a la mitad del ancho, sin rellenar a la derecha.
//
// The picker is the first thing a user sees, so a title that hugs the left edge
// looks broken. Centring is a plain half-difference, which means the leftover cell
// goes to the RIGHT — a text one cell short of the width comes out with its padding
// floored, not rounded up. Both parities are in the table because a rounding mutant
// only shows on one of them.
func TestCenterText_PadsByHalfTheDifference(t *testing.T) {
	p := newPickerForTest()

	for _, tc := range []struct {
		width int
		text  string
		want  string
	}{
		// Fits: untouched. The `>=` boundary is here, where the two branches
		// produce the same string — the padding would be zero.
		{3, "abc", "abc"},
		{4, "abc", "abc"}, // one cell of slack, floored to no padding
		{2, "abc", "abc"}, // wider than the box: returned whole, not cut
		{0, "abc", "abc"},
		{-5, "abc", "abc"},

		// Odd slack: floored, so the text sits one cell left of true centre.
		{5, "abc", " abc"},   // slack 2
		{6, "abc", " abc"},   // slack 3 -> 1
		{9, "abc", "   abc"}, // slack 6
		{11, "abc", "    abc"},

		// Even slack: exactly centred.
		{7, "abc", "  abc"},   // slack 4
		{10, "abc", "   abc"}, // slack 7 -> 3
		{20, "abc", "        abc"},

		// A one cell string, where every slack is width-1. Two cells wide is a
		// slack of ONE, which floors to no padding at all: a single cell of
		// slack is not enough to move a one cell string, so it stays flush left.
		{1, "x", ""},
		{2, "x", ""},
		{3, "x", " x"},
		{4, "x", " x"},
		{5, "x", "  x"},
		{6, "x", "  x"},

		// An empty string centres to pure padding.
		{4, "", "  "},
		{5, "", "  "},
	} {
		t.Run(fmt.Sprintf("width=%d text=%q", tc.width, tc.text), func(t *testing.T) {
			p.SetWidth(tc.width)
			got := p.centerText(tc.text)
			if tc.text == "" {
				if got != tc.want {
					t.Errorf("centerText(%q) at width %d = %q, want %q", tc.text, tc.width, got, tc.want)
				}
				return
			}
			// Build the expectation from the rule rather than repeating the
			// strings, so the table above reads as a claim about the rule.
			slack := tc.width - lipgloss.Width(tc.text)
			var want string
			if slack <= 0 {
				want = tc.text
			} else {
				want = strings.Repeat(" ", slack/2) + tc.text
			}
			if got != want {
				t.Errorf("centerText(%q) at width %d = %q, want %q", tc.text, tc.width, got, want)
			}
			if tc.want != "" && got != tc.want {
				t.Errorf("centerText(%q) at width %d = %q, and the table says %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// Scenario: El ancho se mide en celdas, y el texto puede traer estilos.
//
// The text being centred is rendered, so it carries SGR escapes, and the title is
// `dbx` in the user's own theme. Measuring bytes would push the styled title far to
// the right and measuring runes would misplace anything double-width.
func TestCenterText_MeasuresCellsAndSeesThroughStyling(t *testing.T) {
	p := newPickerForTest()
	styles := theme.Resolve("dark").Styles()

	styled := styles.Error.Render("Error: boom")
	plain := ansi.Strip(styled)
	if plain != "Error: boom" {
		t.Fatalf("the fixture is %q, not %q: the escapes are load-bearing for this test", plain, "Error: boom")
	}

	// Eleven cells of text in a width of twenty: a slack of nine, so four of
	// padding. The escapes are nineteen bytes long and would move the text by
	// nineteen cells if they were counted, so a byte-based measurement cannot
	// produce the right answer here by accident.
	if got, want := lipgloss.Width(styled), len(plain); got != want {
		t.Fatalf("the styled fixture is %d cells wide but its text is %d: fix the case, not the code", got, want)
	}
	// The padding goes in front of the STYLED text, not inside it: the escapes
	// stay attached to the words they colour.
	p.SetWidth(20)
	got := p.centerText(styled)
	if want := "    " + styled; got != want {
		t.Errorf("centring styled text gave %q, want %q", got, want)
	}

	// And a double-width string: eight cells of text, six of padding at width 20.
	wide := "日本語だ" // four runes, eight cells
	if got, want := lipgloss.Width(wide), 8; got != want {
		t.Fatalf("the fixture is %d cells wide, not %d: fix the case, not the code", got, want)
	}
	p.SetWidth(20)
	if got, want := p.centerText(wide), "      "+wide; got != want {
		t.Errorf("centring %q gave %q, want %q", wide, got, want)
	}
}

// --- the key state machine --------------------------------------------------

// Scenario: Cada tecla hace lo que su nombre dice, y solo consume las que hace.
//
// The picker's whole interaction is this switch, and the second return value is the
// part that matters: it says whether the key was consumed, so a key the picker does
// not understand can fall through to something else. Every combination is tabled
// rather than sampled, because the interesting cases are the ones where a key is
// pressed on an empty list or on the wrong kind of row and must NOT be consumed.
func TestHandleKey_EachKeyDoesItsOwnThing(t *testing.T) {
	t.Run("navigation is always consumed, even with nothing to navigate", func(t *testing.T) {
		for _, key := range []string{"j", "down", "k", "up"} {
			p := newPickerForTest()
			if !pressed(p, key) {
				t.Errorf("%q on an empty list was not consumed; the cursor cannot move, but the key is still the user's", key)
			}
		}
	})

	t.Run("space on an empty list is NOT consumed", func(t *testing.T) {
		// There is no row to toggle. Consuming the key would swallow it silently
		// and the user would see nothing happen, which is the one outcome worse
		// than "not mine".
		for _, key := range []string{"space"} {
			p := newPickerForTest()
			if pressed(p, key) {
				t.Errorf("%q on an empty list was consumed, but there is no project to toggle", key)
			}
		}
	})

	t.Run("enter on an empty list is NOT consumed", func(t *testing.T) {
		p := newPickerForTest()
		if pressed(p, "enter") {
			t.Error(`"enter" on an empty list was consumed, but there is nothing to select`)
		}
	})

	t.Run("enter on an INACTIVE project is NOT consumed", func(t *testing.T) {
		// This is the load-bearing one. Enter means "connect to this". A project
		// that is switched off is not a connection, so selecting it would start
		// a session the user has not chosen.
		p := newPickerForTest()
		p.SetProjects([]config.FoundProject{project("beta", "/b", "mysql", "mysql://r@h:3306/a", false)})
		if pressed(p, "enter") {
			t.Error(`"enter" on an inactive project was consumed: an inactive project is not a connection`)
		}
	})

	t.Run("enter on an active project selects it", func(t *testing.T) {
		want := project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true)
		p := newPickerForTest()
		p.SetProjects([]config.FoundProject{want})

		msg, handled := press(p, "enter")
		if !handled {
			t.Fatal(`"enter" on an active project was not consumed`)
		}
		sel, ok := msg.(ConnectionSelectedMsg)
		if !ok {
			t.Fatalf("enter produced %T, want ConnectionSelectedMsg", msg)
		}
		if sel.Project != want {
			t.Errorf("enter selected %+v, want %+v", sel.Project, want)
		}
	})

	t.Run("q and ctrl+c quit", func(t *testing.T) {
		for _, key := range []string{"q", "ctrl+c"} {
			p := newPickerForTest()
			msg, handled := press(p, key)
			if !handled {
				t.Errorf("%q was not consumed", key)
				continue
			}
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Errorf("%q produced %T, want tea.QuitMsg", key, msg)
			}
		}
	})

	t.Run("every other key falls through", func(t *testing.T) {
		for _, key := range []string{"a", "x", "tab", "left", "right", "home", "end", "?"} {
			p := newPickerForTest()
			p.SetProjects([]config.FoundProject{project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true)})
			if cmd, handled := p.Update(keyMsg(key)); handled || cmd != nil {
				t.Errorf("%q was consumed by the picker (handled=%v, cmd=%v), but it is not one of its keys", key, handled, cmd != nil)
			}
		}
	})
}

// Scenario: El mensaje de toggle lleva el proyecto ANTES del cambio.
//
// KNOWN BEHAVIOUR, and the reason is a copy, not an oversight.
//
// The handler takes `proj := p.projects[p.cursor]` — a COPY — works out the new
// state, writes the new state into the slice, and then builds the message from the
// copy. So `ProjectToggledMsg.Project.Active` is the value the project had BEFORE
// the key, and only the message's own `Active` field carries the new one.
//
// A consumer that reads `Project.Active` to learn the new state reads the old one.
// It is pinned because the two fields disagree by construction, and a reader of the
// struct has no way to tell that without this comment.
func TestProjectToggledMsg_CarriesTheStateFromBeforeTheToggle(t *testing.T) {
	for _, wasActive := range []bool{true, false} {
		name := "turning an inactive project on"
		if wasActive {
			name = "turning an active project off"
		}
		t.Run(name, func(t *testing.T) {
			p := newPickerForTest()
			p.SetProjects([]config.FoundProject{project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", wasActive)})

			msg, handled := press(p, "space")
			if !handled {
				t.Fatal(`"space" was not consumed`)
			}
			tog, ok := msg.(ProjectToggledMsg)
			if !ok {
				t.Fatalf("space produced %T, want ProjectToggledMsg", msg)
			}

			if tog.Active == wasActive {
				t.Errorf("the message says Active=%v, which is what it already was", tog.Active)
			}
			if tog.Project.Active != wasActive {
				t.Errorf("the message's copy of the project says Active=%v, want the state from BEFORE the toggle (%v)", tog.Project.Active, wasActive)
			}
			// And the picker itself did change: the field the message does not
			// promise is the one that moved.
			if got := p.projects[0].Active; got == wasActive {
				t.Errorf("the picker's own project is still Active=%v, so nothing was toggled", wasActive)
			}
		})
	}
}

// Scenario: El cursor se queda en los extremos en vez de salirse.
//
// The list is the only navigation, and the two edges are the whole contract: j at
// the bottom does nothing rather than running off the end, and k at the top does
// nothing rather than going negative. A cursor of -1 is not a row that renders
// wrong, it is a row that renders no arrow at all, so the assertion is on what the
// view shows rather than on the number.
func TestCursor_StopsAtBothEnds(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(60)
	three := []config.FoundProject{
		project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true),
		project("beta", "/b", "mysql", "mysql://r@h:3306/a", false),
		project("gamma", "/c", "sqlite", "sqlite:///tmp/x.db", false),
	}
	p.SetProjects(three)

	// Down past the end.
	for range 10 {
		press(p, "j")
	}
	if got := cursorRow(t, ansi.Strip(p.View())); got != 2 {
		t.Errorf("after 10 presses of j the arrow is on row %d, want the last row (2)", got)
	}

	// Up past the top.
	for range 20 {
		press(p, "k")
	}
	if got := cursorRow(t, ansi.Strip(p.View())); got != 0 {
		t.Errorf("after 20 presses of k the arrow is on row %d, want the first row (0)", got)
	}

	// And down and back up lands where it started.
	press(p, "j")
	press(p, "j")
	press(p, "k")
	if got := cursorRow(t, ansi.Strip(p.View())); got != 1 {
		t.Errorf("j j k put the arrow on row %d, want row 1", got)
	}
}

// Scenario: Cargar una lista nueva devuelve el cursor al principio.
//
// The picker reloads when the filesystem watcher fires, and the user may be halfway
// down the old list. Carrying that cursor into a different list would put the arrow
// on whatever now occupies that index — a different project, silently highlighted.
func TestSetProjects_PutsTheCursorBackAtTheTop(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(60)
	p.SetProjects([]config.FoundProject{
		project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true),
		project("beta", "/b", "mysql", "mysql://r@h:3306/a", false),
	})
	press(p, "j")
	if got := cursorRow(t, ansi.Strip(p.View())); got != 1 {
		t.Fatalf("the arrow is on row %d before the reload, want 1: this test needs a moved cursor", got)
	}

	p.SetProjects([]config.FoundProject{
		project("gamma", "/c", "sqlite", "sqlite:///tmp/y.db", false),
		project("delta", "/d", "postgres", "postgres://u:p@h:5433/db", false),
	})
	if got := cursorRow(t, ansi.Strip(p.View())); got != 0 {
		t.Errorf("after SetProjects the arrow is on row %d, want row 0", got)
	}
}

// Scenario: Un mensaje que no es una tecla no lo es.
//
// Update takes any message and answers two questions: is this mine, and what should
// happen. Only a key press is the picker's; everything else — the scan finishing, a
// project list arriving, a resize — belongs to somebody else and has to fall through
// unconsumed or the picker swallows another component's message.
func TestUpdate_IgnoresEverythingThatIsNotAKey(t *testing.T) {
	p := newPickerForTest()
	p.SetProjects([]config.FoundProject{project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true)})

	for _, msg := range []tea.Msg{
		nil,
		tea.WindowSizeMsg{Width: 100, Height: 40},
		ConnectionSelectedMsg{},
		ProjectToggledMsg{},
		"a bare string",
		42,
	} {
		cmd, handled := p.Update(msg)
		if handled || cmd != nil {
			t.Errorf("Update(%T) answered handled=%v cmd=%v, but only key presses are the picker's", msg, handled, cmd != nil)
		}
	}
}

// --- the view ---------------------------------------------------------------

// Scenario: Cada fila enseña flecha, nombre, estado, driver:puerto y ruta.
//
// This is the whole list rendering, asserted as exact lines rather than as
// substrings. The spacing is part of the contract — a column one space out of place
// makes the driver and the path read as one run-on field — and so is which row
// carries the arrow and which rows carry the off tag.
func TestView_EachRowShowsItsPartsInOrder(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(120)

	// One project with a port in its DSN, one without, one switched off. The
	// three variations have to be visible in one render to be worth anything.
	p.SetProjects([]config.FoundProject{
		project("alpha", "/home/u/alpha", "postgres", "postgres://u:p@h:5432/db", true),
		project("beta", "/home/u/beta", "mysql", "mysql://r@h:3306/a", false),
		project("gamma", "/home/u/gamma", "sqlite", "sqlite:///tmp/gamma.db", true),
	})
	// Cursor on the middle row, so the arrow is not on the first line.
	press(p, "j")

	got := rows(ansi.Strip(p.View()))
	want := []string{
		// First row: no arrow, so the placeholder stands in for it. The
		// placeholder is as WIDE AS THE ARROW rather than a fixed two spaces, and
		// the alignment check below is what holds that to account.
		"    alpha  postgres:5432  /home/u/alpha",
		// Middle row: the arrow, the off tag, and a port from a mysql DSN.
		"  ▸ beta [off]  mysql:3306  /home/u/beta",
		// Last row: no port in the DSN at all, so the driver stands alone
		// instead of being rendered with an empty ":".
		"    gamma  sqlite  /home/u/gamma",
	}

	if len(got) != len(want) {
		t.Fatalf("the view has %d project rows, want %d:\n%q", len(got), len(want), got)
	}
	names := []string{"alpha", "beta", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d is\n%q\nwant\n%q", i, got[i], want[i])
		}
	}

	// Every name starts at the SAME display column, whether or not the row is the
	// cursor's, and that is what fixes the width of the placeholder.
	//
	// The arrow is ONE cell. A placeholder written as two spaces — which is what
	// this used to be, and which is the obvious thing to write for a wide glyph —
	// made the cursor's row a cell narrower than every other row, so the driver
	// and path columns jumped left by one cell every time the cursor moved onto
	// a row.
	//
	// The measurement below is the check that catches it, and it is the only one
	// that does: a test comparing whole lines accepts both spellings, because
	// "    alpha" and "     alpha" are both plausible-looking strings and only one
	// of them lines up with "  ▸ alpha".
	//
	// This is asserted by measuring, not by counting the literals above, because
	// a test that just restates the expected string cannot notice that the two
	// spellings differ in length.
	for i, line := range got {
		name := names[i]
		before, _, found := strings.Cut(line, name)
		if !found {
			t.Errorf("row %d has no %q in it: %q", i, name, line)
			continue
		}
		// Four cells: the two of leading indent the format asks for, then the
		// arrow or its placeholder, then the single space before the name.
		if got, want := lipgloss.Width(before), 4; got != want {
			t.Errorf("row %d has its name at display column %d, want %d: %q", i, got, want, line)
		}
	}
}

// Scenario: El estado de la vista decide qué se ve, y escanear gana.
//
// There are three screens and they are checked in one order, which is the order
// that matters: while the scan is running there is nothing to list and no error to
// report, so the loading text wins over both. An error with no projects and no scan
// is next, and the empty list is last.
//
// A scan that fails after it started would otherwise show the error mid-scan and
// then overwrite it with "scanning", which is the kind of flicker a user reads as
// the tool losing its mind.
func TestView_TheScreenIsChosenInOrder(t *testing.T) {
	t.Run("scanning", func(t *testing.T) {
		p := newPickerForTest()
		p.SetWidth(60)
		p.SetLoading(true)
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "Scanning for .dbx.toml files...") {
			t.Errorf("while loading the view is %q, want the scanning text", got)
		}
	})

	t.Run("an error", func(t *testing.T) {
		p := newPickerForTest()
		p.SetWidth(60)
		p.SetError(errors.New("permission denied on /home/u"))
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "Error: permission denied on /home/u") {
			t.Errorf("with an error the view is %q, want it to name the error", got)
		}
	})

	t.Run("no projects", func(t *testing.T) {
		p := newPickerForTest()
		p.SetWidth(60)
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "No .dbx.toml files found") {
			t.Errorf("with no projects the view is %q, want the empty-list text", got)
		}
	})

	t.Run("scanning wins over an error", func(t *testing.T) {
		p := newPickerForTest()
		p.SetWidth(60)
		p.SetLoading(true)
		p.SetError(errors.New("boom"))
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "Scanning for .dbx.toml files...") {
			t.Errorf("scanning with an error shows %q, want the scanning text to win", got)
		}
		if strings.Contains(got, "boom") {
			t.Errorf("scanning with an error shows the error too: %q", got)
		}
	})

	t.Run("an error wins over an empty list", func(t *testing.T) {
		p := newPickerForTest()
		p.SetWidth(60)
		p.SetError(errors.New("boom"))
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "boom") {
			t.Errorf("an error with no projects shows %q, want the error", got)
		}
		if strings.Contains(got, "No .dbx.toml files found") {
			t.Errorf("an error with no projects also shows the empty-list text: %q", got)
		}
	})
}

// Scenario: La vista lista lleva título y pie, y el pie se centra.
//
// The title tells the user what program they are in before they press anything, and
// the footer is the only place the picker's keys are written down. Both are
// centred, and the footer goes through keydisplay so the key names come out as
// glyphs on a Nerd Font terminal — the expectation is built the same way, because a
// test that hardcodes the glyphs breaks the day the font is missing.
func TestView_ListsHaveATitleAndAFooter(t *testing.T) {
	p := newPickerForTest()
	p.SetWidth(60)
	p.SetProjects([]config.FoundProject{project("alpha", "/a", "postgres", "postgres://u:p@h:5432/db", true)})

	view := ansi.Strip(p.View())
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		t.Fatalf("the view is %d lines, too few to hold a title, a row and a footer:\n%q", len(lines), view)
	}
	if strings.TrimSpace(lines[0]) != "dbx" {
		t.Errorf("the first line is %q, want the title dbx", lines[0])
	}
	// The title is centred in the width, so it starts a third of the way in.
	if !strings.HasPrefix(lines[0], strings.Repeat(" ", 28)) {
		t.Errorf("the title %q is not centred at width 60: it starts with %d spaces", lines[0], len(lines[0])-len(strings.TrimLeft(lines[0], " ")))
	}

	footer := lines[len(lines)-1]
	want := keydisplay.Key("j/k ↑↓   space toggle   enter select   q quit")
	if strings.TrimSpace(footer) != want {
		t.Errorf("the footer is %q, want %q", strings.TrimSpace(footer), want)
	}
	// And it is centred the same way, so its padding is half the slack.
	slack := 60 - lipgloss.Width(want)
	if got, expect := footer, strings.Repeat(" ", slack/2)+want; got != expect {
		t.Errorf("the footer is not centred:\n%q\nwant\n%q", got, expect)
	}
}
