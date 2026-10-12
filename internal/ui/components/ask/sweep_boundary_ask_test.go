package ask

// Boundary proofs for ask.go CONDITIONALS_BOUNDARY mutants (report.json lines
// 207, 240, 243, 248, 287).
//
// Every mutant here is EQUIVALENT, and the reason is one of two shapes:
//
//   - the branch body assigns the value the variable already holds at the
//     boundary (the width and height clamps: modalW = 50 when it is 50,
//     modalH = 10 when it is 10), so the extra branch is a no-op;
//   - the equality makes the branch body a concatenation of the empty string
//     (line 207: `a.input += msg.Text` with msg.Text == "") or a reslice of
//     the whole slice (line 287: `lines[len(lines)-(innerH-1):]` with
//     len(lines) == innerH-1 is `lines[0:]`), both of which are identities.
//
// Each test builds the equality, asserts the original's observable behaviour
// (the assertion that stays green under the mutant — which is exactly what
// documents that nothing observable changes), and pins the neighbour on the
// other side of the boundary so the fixture proves the equality is where
// claimed.
//
// Verified with /tmp/opencode/sweep/verify.py.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func uicoreAsk(width, height int) *Ask {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(width)
	a.SetHeight(height)
	a.Show()
	return a
}

func uicoreWidestLine(t *testing.T, view string) int {
	t.Helper()
	var widest int
	for _, l := range strings.Split(ansi.Strip(view), "\n") {
		if n := lipgloss.Width(l); n > widest {
			widest = n
		}
	}
	return widest
}

// LINE 207: `if len(msg.Text) > 0 && msg.Text != " " || key == "space"` -> `>=`.
// Boundary: a key with empty Text (enter, arrows — keys that carry no text).
// The mutant's first conjunct is always true, so it enters the branch where
// the original does not; the body then runs `a.input += msg.Text`, which for
// the empty string is the identity. The input and the returned handled flag
// are the same on both arms.
func TestAskBoundaryAKeyWithEmptyTextAppendsNothing(t *testing.T) {
	// Keys that fall through to the default branch carrying no text. enter,
	// esc, backspace and space all have their own cases BEFORE the mutated
	// line, so they never reach it — these are the keys the line decides for.
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'x', Text: ""},
		{Code: 'Z', Text: ""},
		{Code: '1', Text: ""},
	} {
		a := uicoreAsk(100, 40)
		a.input = "ab"
		cmd, handled := a.Update(msg)
		if !handled {
			t.Errorf("key %+v was not handled while typing", msg)
		}
		if cmd != nil {
			t.Errorf("key %+v produced a command %v, want none", msg, cmd)
		}
		if got := a.Input(); got != "ab" {
			t.Errorf("key %+v changed the input to %q, want it unchanged at %q (appending the empty text is the identity)", msg, got, "ab")
		}
	}
}

// LINE 240: `if modalW < 50` -> `<=`. Boundary: width 67 gives modalW == 50
// exactly (67*3/4). The body assigns 50, already held.
func TestAskBoundaryWidthExactlyAtTheFloor(t *testing.T) {
	a := uicoreAsk(67, 40)
	if got := uicoreWidestLine(t, a.View()); got != 50 {
		t.Errorf("width 67: the modal is %d columns wide, want exactly the %d floor", got, 50)
	}
	// Neighbours: 66 clamps up to the same 50, 68 escapes the floor.
	if got := uicoreWidestLine(t, uicoreAsk(66, 40).View()); got != 50 {
		t.Errorf("width 66: the modal is %d columns wide, want the clamped %d", got, 50)
	}
	if got := uicoreWidestLine(t, uicoreAsk(68, 40).View()); got != 51 {
		t.Errorf("width 68: the modal is %d columns wide, want %d", got, 51)
	}
}

// LINE 243: `if modalW > 90` -> `>=`. Boundary: width 120 gives modalW == 90
// exactly (120*3/4). The body assigns 90, already held.
func TestAskBoundaryWidthExactlyAtTheCeiling(t *testing.T) {
	a := uicoreAsk(120, 40)
	if got := uicoreWidestLine(t, a.View()); got != 90 {
		t.Errorf("width 120: the modal is %d columns wide, want exactly the %d ceiling", got, 90)
	}
	// Neighbours: 119 stays under the ceiling, 121 clamps down to the same 90.
	if got := uicoreWidestLine(t, uicoreAsk(119, 40).View()); got != 89 {
		t.Errorf("width 119: the modal is %d columns wide, want %d", got, 89)
	}
	if got := uicoreWidestLine(t, uicoreAsk(121, 40).View()); got != 90 {
		t.Errorf("width 121: the modal is %d columns wide, want the clamped %d", got, 90)
	}
}

// LINE 248: `if modalH < 10` -> `<=`. Boundary: height 13 gives modalH == 10
// exactly (13*8/10). The body assigns 10, already held, so the frame is the
// same 10 rows.
func TestAskBoundaryHeightExactlyAtTheFloor(t *testing.T) {
	// RenderWithTitleEx pads the content to exactly modalH rows.
	for _, tc := range []struct{ height, want int }{
		{12, 10}, // 12*8/10 = 9, clamped up to 10
		{13, 10}, // exactly 10, the boundary
		{14, 11}, // one over
	} {
		a := uicoreAsk(100, tc.height)
		if got := len(strings.Split(ansi.Strip(a.View()), "\n")); got != tc.want {
			t.Errorf("height %d: rendered %d lines, want %d", tc.height, got, tc.want)
		}
	}
}

// LINE 287: `if len(lines) > innerH-1` -> `>=`. Boundary: the transcript lines
// are exactly innerH-1, which is where an empty transcript lands by
// construction (1 input line + innerH-2 pads). The mutant reslices
// lines[0:] — the whole slice — which is the identity, so the frame is the
// same: innerH-1 transcript rows, then the footer, inside the modalH-row box.
func TestAskBoundaryTranscriptExactlyAtTheInnerLimit(t *testing.T) {
	// height 13 -> modalH 10 -> innerH 8 -> innerH-1 = 7.
	a := uicoreAsk(100, 13)
	view := ansi.Strip(a.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 10 {
		t.Fatalf("rendered %d lines, want the 10-row frame (modalH)", len(lines))
	}
	// Row 0 is the top border, rows 1..8 the content, row 9 the bottom border.
	// The first transcript row is the input prompt, so the frame really holds
	// the typing line and not a shifted window.
	if !strings.Contains(lines[1], "> ") {
		t.Errorf("the first content row is %q, want the input prompt \"> \"", lines[1])
	}
	// And the footer closes the content as its last row. keydisplay.Key turns
	// "Enter"/"Esc" into nerd-font glyphs, so the plain words are "send"/"close".
	last := lines[len(lines)-2]
	if !strings.Contains(last, "send") || !strings.Contains(last, "close") {
		t.Errorf("the last content row is %q, want the footer with the key hints", last)
	}
}
