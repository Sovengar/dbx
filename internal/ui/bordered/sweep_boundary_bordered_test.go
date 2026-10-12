package bordered

// Boundary proofs for bordered.go CONDITIONALS_BOUNDARY mutants (report.json
// lines 41, 73, 93, 96, 100, 147, 189, 209, 220).
//
// Every mutant here is EQUIVALENT, and the reason is one of two shapes:
//
//   - the branch body assigns the value the variable already holds at the
//     boundary (the width/innerWidth/targetContent clamps, the content truncate,
//     the zero padding), so the extra branch is a no-op; or
//   - at the equality the two arms of the branch produce the SAME line (the
//     `<=` content fit and its wrap fallback both emit the line with zero
//     padding, because wrapLine at exactly the width yields one chunk and
//     padding = innerWidth - width = 0).
//
// Each test builds the equality, asserts the original's byte-exact output (the
// assertion that stays green under the mutant — which is exactly what documents
// that nothing observable changes), and pins the neighbour on the other side of
// the boundary so the fixture proves the equality is where claimed.
//
// Verified with /tmp/opencode/sweep/verify.py.

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func uicoreBorder() lipgloss.Border { return lipgloss.RoundedBorder() }

// LINE 41: `if width < 2` -> `<=`. Boundary: width == 2. The body assigns 2,
// already held, so the render is identical.
func TestBorderedBoundaryWidthExactlyTwo(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "t", "x", 2, 0)
	want := "╭╮\n│x│\n╰╯"
	if got != want {
		t.Errorf("width 2:\n got %q\nwant %q", got, want)
	}
	// Neighbour: width 1 clamps up to the same 2-column box.
	if got := RenderWithTitle(uicoreBorder(), nil, "t", "x", 1, 0); got != want {
		t.Errorf("width 1:\n got %q\nwant %q (clamped to the same box)", got, want)
	}
}

// LINE 73: `if innerWidth < 0` -> `<=`. Boundary: innerWidth == 0, which is
// width 2 with the rounded border's two single-column corners. The body
// assigns 0, already held.
func TestBorderedBoundaryInnerWidthExactlyZero(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "t", "x", 2, 0)
	want := "╭╮\n│x│\n╰╯"
	if got != want {
		t.Errorf("a zero-inner-width box:\n get %q\nwant %q", got, want)
	}
	// Neighbour: width 3 gives one column of interior, eaten by the title.
	if got, want := RenderWithTitle(uicoreBorder(), nil, "t", "x", 3, 0), "╭t╮\n│x│\n╰─╯"; got != want {
		t.Errorf("width 3:\n got %q\nwant %q", got, want)
	}
}

// LINE 93: `if targetContent < 1` -> `<=`. Boundary: height == 3 gives
// targetContent == 1 (3 - 2 border rows). The body assigns 1, already held.
func TestBorderedBoundaryTargetContentExactlyOne(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "t", "x", 10, 3)
	want := "╭───t────╮\n│x       │\n╰────────╯"
	if got != want {
		t.Errorf("height 3:\n get %q\nwant %q", got, want)
	}
	// Neighbour: height 2 would compute 0 and is clamped to the same 1 row.
	if got := RenderWithTitle(uicoreBorder(), nil, "t", "x", 10, 2); got != want {
		t.Errorf("height 2:\n got %q\nwant %q (clamped to the same box)", got, want)
	}
}

// LINE 96: `for len(contentLines) < targetContent` -> `<=`. Boundary: the
// content has EXACTLY targetContent lines (3 lines, height 5). The mutant's
// loop appends one blank line, but the line-100 truncate (unmutated when only
// this mutant is applied) cuts it back: len > targetContent -> [:targetContent].
// The two compensate and the output is byte-identical.
func TestBorderedBoundaryContentExactlyFillsTheTarget(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "t", "a\nb\nc", 10, 5)
	want := "╭───t────╮\n│a       │\n│b       │\n│c       │\n╰────────╯"
	if got != want {
		t.Errorf("content exactly at the target:\n get %q\nwant %q", got, want)
	}
	// Neighbour: one line short of the target is padded with a blank row.
	if got, short := RenderWithTitle(uicoreBorder(), nil, "t", "a\nb", 10, 5), "╭───t────╮\n│a       │\n│b       │\n│        │\n╰────────╯"; got != short {
		t.Errorf("content one line short:\n got %q\nwant %q (padded to the same box)", got, short)
	}
}

// LINE 100: `if len(contentLines) > targetContent` -> `>=`. Boundary: the
// content has exactly targetContent lines. The body truncates to targetContent,
// which is the length it already has.
func TestBorderedBoundaryContentAtTargetIsNotTruncated(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "t", "a\nb\nc", 10, 5)
	want := "╭───t────╮\n│a       │\n│b       │\n│c       │\n╰────────╯"
	if got != want {
		t.Errorf("content exactly at the target:\n get %q\nwant %q", got, want)
	}
	// Neighbour: one line over IS truncated to the same 3 rows.
	if got := RenderWithTitle(uicoreBorder(), nil, "t", "a\nb\nc\nd", 10, 5); got != want {
		t.Errorf("content one line over:\n got %q\nwant %q (truncated to the same box)", got, want)
	}
}

// LINE 147: `if titleWidth > innerWidth` -> `>=`. Boundary: the title is
// exactly innerWidth wide (8 columns in a 10-wide box). The body truncates the
// title to its own width — a no-op — and both arms render it whole.
func TestBorderedBoundaryTitleExactlyTheInnerWidth(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "abcdefgh", "x", 10, 0)
	want := "╭abcdefgh╮\n│x       │\n╰────────╯"
	if got != want {
		t.Errorf("a title of exactly the inner width:\n get %q\nwant %q", got, want)
	}
	// Neighbour: one column over IS truncated to the same 8 columns.
	if got := RenderWithTitle(uicoreBorder(), nil, "abcdefghi", "x", 10, 0); got != want {
		t.Errorf("a title one column over:\n got %q\nwant %q (truncated to the same box)", got, want)
	}
}

// LINE 189: `if footerWidth > innerWidth` -> `>=`. Boundary: the footer is
// exactly innerWidth wide. Same no-op truncate as the title.
func TestBorderedBoundaryFooterExactlyTheInnerWidth(t *testing.T) {
	got := RenderWithTitleAndFooter(uicoreBorder(), nil, "t", "12345678", "x", 10, 0)
	want := "╭───t────╮\n│x       │\n╰12345678╯"
	if got != want {
		t.Errorf("a footer of exactly the inner width:\n get %q\nwant %q", got, want)
	}
	// Neighbour: one column over IS truncated to the same 8 columns.
	if got := RenderWithTitleAndFooter(uicoreBorder(), nil, "t", "123456789", "x", 10, 0); got != want {
		t.Errorf("a footer one column over:\n got %q\nwant %q (truncated to the same box)", got, want)
	}
}

// LINE 209: `if displayWidth <= innerWidth` -> `<`. Boundary: the content line
// is exactly innerWidth wide (4 columns in a 6-wide box). The mutant takes the
// wrap fallback, but wrapLine at exactly the width yields one chunk — the
// running width never exceeds it — and the chunk's padding is innerWidth - 4
// = 0, the same zero-padding line the original builds.
func TestBorderedBoundaryContentExactlyTheInnerWidth(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "", "1234", 6, 0)
	want := "╭────╮\n│1234│\n╰────╯"
	if got != want {
		t.Errorf("a content line of exactly the inner width:\n get %q\nwant %q", got, want)
	}
	// Neighbour: one column over wraps, and the first chunk is the same 4
	// columns with the same zero padding.
	if got := RenderWithTitle(uicoreBorder(), nil, "", "12345", 6, 0); got != "╭────╮\n│1234│\n│5   │\n╰────╯" {
		t.Errorf("a content line one column over:\n got %q", got)
	}
}

// LINE 220: `if padding < 0` -> `<=`. Boundary: padding == 0, which happens
// for every wrapped chunk that is exactly innerWidth wide. The body assigns 0,
// already held.
func TestBorderedBoundaryWrappedChunkExactlyTheInnerWidth(t *testing.T) {
	got := RenderWithTitle(uicoreBorder(), nil, "", "12345678", 6, 0)
	want := "╭────╮\n│1234│\n│5678│\n╰────╯"
	if got != want {
		t.Errorf("chunks of exactly the inner width:\n get %q\nwant %q", got, want)
	}
	// Neighbour: a final chunk one column short gets one column of padding.
	if got := RenderWithTitle(uicoreBorder(), nil, "", "123456789", 6, 0); got != "╭────╮\n│1234│\n│5678│\n│9   │\n╰────╯" {
		t.Errorf("a final chunk one column short:\n got %q", got)
	}
}
