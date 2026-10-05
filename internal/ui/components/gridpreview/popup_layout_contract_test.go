package gridpreview

// Scenario: The suggestion popup's layout, the jq history's edges, and the two guards that
// describe a fact the reader already knows.
//
// Three groups, because they fail three different ways:
//
//   - The popup's LAYOUT is a set of floors and subtractions that only disagree at one width
//     each, so the cases sit on those widths. A popup that is two columns too wide at every
//     width except the boundary looks perfect in every other test.
//   - The history's edges are an index that walks off both ends, where "off the end" is a
//     meaningful state rather than a mistake: the index equal to the length is the sentinel
//     for "recalled nothing", which is why the clamp is `>` and not `>=`.
//   - The last two are the guards this round removed -- a condition that restated what the
//     reader already knew -- and they are checked from the other side, by showing the state
//     each one was protecting.

import (
	"fmt"
	"strings"
	"testing"
)

// popupLines is the popup as rows, WITH the colours still in it.
//
// Raw rather than stripped, and that is load-bearing: which row is highlighted is carried by
// the BACKGROUND, so stripping the escapes makes a highlighted row identical to a plain one
// and every highlight assertion silently passes. The first version of this file stripped them
// and reported "0 rows are highlighted" for all four selections, which reads like a rendering
// bug and was a bug in the test.
//
// popupText is the same thing with the colours removed, for assertions about what a row SAYS.
func popupLines(p *GridPreview) []string {
	return strings.Split(p.renderJQSuggestions(), "\n")
}

func popupText(p *GridPreview) []string {
	return strings.Split(ansi_Strip(p.renderJQSuggestions()), "\n")
}

// The width floor.
//
// `maxWidth := p.width - 8; if maxWidth < 30 { maxWidth = 30 }`. The floor is 30 and the
// subtraction is 8, so the boundary is a panel 38 wide: at 38 the arithmetic gives exactly 30
// and the floor must leave it alone, and at 37 it gives 29 and the floor must raise it.
//
// Getting either side wrong produces a popup that is one column narrower or wider than it
// should be at exactly one panel width, which is a width nobody tests and everybody has.
func TestThePopupHasAWidthFloor(t *testing.T) {
	sug := func(path, typ string) JQSuggestion { return JQSuggestion{Path: path, Type: typ} }

	for _, tc := range []struct {
		name     string
		width    int
		wantWide int
	}{
		{"far above the floor, arithmetic wins", 100, 100 - 8},
		{"four columns above the floor", 42, 42 - 8},
		{"one above the floor", 39, 39 - 8},
		{"exactly at the floor", 38, 30},
		{"one below the floor, the floor wins", 37, 30},
		{"far below the floor", 10, 30},
		{"a zero-width panel still renders", 0, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := previewOf(t, jqDoc)
			p.SetWidth(tc.width)
			p.SetHeight(20)
			p.EnterJQMode()
			p.jqInput = "."
			p.jqCursor = 1
			p.updateJQSuggestions()
			if len(p.jqSugs) == 0 {
				t.Fatal("precondition failed: no suggestions to render")
			}
			p.jqSugs = []JQSuggestion{sug("user", "object")}

			// Every rendered line of the box, which is the widest thing here, is the popup's
			// width. lipgloss pads to Width, so a line shorter than the box means the floor
			// or the subtraction is off.
			for i, line := range popupText(p) {
				if got := len([]rune(line)); got != tc.wantWide {
					t.Errorf("box line %d is %d columns, want the popup's %d (width %d)",
						i, got, tc.wantWide, tc.width)
				}
			}
		})
	}
}

// The path column's width, and the exact-fit case.
//
// pathWidth is maxWidth-14 with NO floor of its own, and that is correct precisely because
// maxWidth has a floor of 30 three lines above: 30-14 == 16, which is wide enough for the
// four-space indent plus a type column. The comment in the source says this is the fourth
// time in this file that a second floor was added for a quantity that already had one, so the
// case below pins it: at the floor the path column is 16 and nothing panics.
//
// The truncation boundary is here too: a path exactly pathWidth wide is not truncated, and one
// character more is. `>` versus `>=` is only visible there.
func TestThePathColumnIsTruncatedOnlyWhenItIsTooWide(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(38) // maxWidth floors to 30, so pathWidth is 16
	p.SetHeight(20)
	p.EnterJQMode()

	rows := func(path string) []string {
		p.jqSugVisible = true
		p.jqSugSelected = 0
		p.jqSugs = []JQSuggestion{{Path: path, Type: "string"}}
		return popupText(p)
	}

	const pathWidth = 16

	t.Run("a path exactly as wide as the column is not truncated", func(t *testing.T) {
		path := strings.Repeat("x", pathWidth)
		lines := rows(path)
		if !strings.Contains(strings.Join(lines, "\n"), path) {
			t.Errorf("a path of exactly %d columns was altered.\n got:\n%s", pathWidth,
				strings.Join(lines, "\n"))
		}
		if strings.Contains(strings.Join(lines, "\n"), "...") {
			t.Errorf("a path of exactly %d columns was truncated anyway.\n got:\n%s", pathWidth,
				strings.Join(lines, "\n"))
		}
	})

	t.Run("one column wider is truncated with an ellipsis", func(t *testing.T) {
		path := strings.Repeat("x", pathWidth+1)
		lines := rows(path)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "...") {
			t.Errorf("a path of %d columns in a %d column field was not truncated.\n got:\n%s",
				pathWidth+1, pathWidth, joined)
		}
	})

	t.Run("a path far too wide is truncated too", func(t *testing.T) {
		lines := rows(strings.Repeat("y", 200))
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "...") {
			t.Errorf("a 200 column path was not truncated.\n got:\n%s", joined)
		}
		// And the box did not grow to fit it: the floor is a floor.
		for i, line := range lines {
			if got := len([]rune(line)); got != 30 {
				t.Errorf("box line %d is %d columns, want 30 -- a long path must not widen the box",
					i, got)
			}
		}
	})
}

// The type column, and the closed vocabulary it draws from.
//
// `typePad := 14 - lipgloss.Width(typeStr); if typePad > 0`. The pad is added when positive and
// skipped when zero, and both arms are arithmetic on one constant -- so a type exactly 14 wide
// is the boundary between them. That value cannot occur: the Type field is built in this
// package from jsonType, which returns one of seven literals, plus the two the array arms add.
// The longest is `array-iter` at ten columns.
//
// So the exact-fit case below asserts the vocabulary and its longest member rather than
// inventing a 14-column type, and the wrapping that a longer one would cause is pinned
// separately as an unreachable state.
func TestTheTypeColumnDrawsFromAClosedVocabulary(t *testing.T) {
	// The whole vocabulary, and the width of the widest of them. If jsonType grows a member
	// this long the type column stops being padded correctly, and this is where it shows.
	for _, tc := range []struct {
		value interface{}
		want  string
	}{
		{nil, "null"},
		{map[string]interface{}{}, "object"},
		{[]interface{}{}, "array"},
		{"s", "string"},
		{1.5, "number"},
		{true, "bool"},
		{struct{}{}, "unknown"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := jsonType(tc.value); got != tc.want {
				t.Fatalf("jsonType(%v) is %q, want %q", tc.value, got, tc.want)
			}
		})
	}

	// The two the array arms add by hand, which jsonType never produces.
	for _, typ := range []string{"array-iter", "length"} {
		if len([]rune(typ)) >= 14 {
			t.Errorf("%q is %d columns, which does not leave the type column room", typ, len([]rune(typ)))
		}
	}

	// And the padding itself, on a real member: the type is followed by spaces out to the
	// column's width, so two rows of the popup are the same length whichever type they carry.
	// That is what makes the box square.
	p := previewOf(t, jqDoc)
	p.SetWidth(100)
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1 // so nothing is highlighted and the rows differ only by content

	widths := map[string]int{}
	for _, typ := range []string{"array-iter", "object", "number", "length"} {
		p.jqSugs = []JQSuggestion{{Path: "k", Type: typ}}
		for i, line := range popupText(p) {
			widths[fmt.Sprintf("%s line %d", typ, i)] = len([]rune(line))
		}
	}
	for what, got := range widths {
		if got != 92 {
			t.Errorf("%s is %d columns, want the box's 92 -- the type column is padded to 14, "+
				"so every row must be the same length", what, got)
		}
	}
}

// Which suggestion is highlighted, and that it is exactly one.
//
// `isSelected := i == p.jqSugSelected` decides the highlight, and the whole popup is invisible
// without it working -- but a popup where EVERY row is highlighted, or none, still renders
// every line at every width, so only the colour tells them apart.
func TestExactlyOneRowOfThePopupIsHighlighted(t *testing.T) {
	for _, selected := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("row %d of four", selected), func(t *testing.T) {
			p := previewOf(t, jqDoc)
			p.SetWidth(100)
			p.SetHeight(20)
			p.EnterJQMode()
			p.jqInput = "."
			p.jqCursor = 1
			p.updateJQSuggestions()
			if len(p.jqSugs) < 4 {
				t.Fatalf("precondition failed: %d suggestions, want at least four", len(p.jqSugs))
			}
			p.jqSugSelected = selected

			// Diff against the same popup with nothing selected. Same technique as
			// highlightedIndices, and for the same reason: the styles decide the bytes, not us.
			marked := popupLines(p)
			saved := p.jqSugSelected
			p.jqSugSelected = -1
			plain := popupLines(p)
			p.jqSugSelected = saved

			diff := 0
			for i := range marked {
				if marked[i] != plain[i] {
					diff++
				}
			}
			if diff != 1 {
				t.Errorf("%d rows are highlighted with row %d selected, want exactly one", diff, selected)
			}
		})
	}
}

// The history's two ends.
//
// The index equal to len(history) is the SENTINEL for "nothing recalled", which is why the
// clamp is `>` and not `>=`: at the length, the input is emptied, and the length itself is
// reachable by walking up past the first entry. Both facts are asserted, and the up-past-the-
// top case is the one that proves the sentinel is reachable rather than theoretical.
func TestWalkingTheHistoryStopsAtBothEndsAndKeepsTheSentinel(t *testing.T) {
	build := func(t *testing.T, entries int) *GridPreview {
		t.Helper()
		// The history is a FILE: addToHistory writes ~/.config/dbx/jq_history.json, and a
		// panel reads it when it is built. previewOf does not redirect HOME, so without this
		// the cases below would start from the developer's saved expressions AND leave a
		// hundred .k0042 entries behind. That is not a hypothetical -- it happened while
		// these cases were being written.
		isolate(t)
		p := previewOf(t, jqDoc)
		p.EnterJQMode()
		for i := 0; i < entries; i++ {
			p.addToHistory(fmt.Sprintf(".k%d", i))
		}
		return p
	}

	t.Run("down walks to the sentinel and empties the input", func(t *testing.T) {
		p := build(t, 3)
		if len(p.jqHistory) != 3 {
			t.Fatalf("precondition failed: %d entries, want 3", len(p.jqHistory))
		}
		p.jqHistoryIdx = 0
		p.jqInput = p.jqHistory[0]

		for i := 1; i <= 3; i++ {
			p.jqHistoryNavigate(1)
			if p.jqHistoryIdx != i {
				t.Fatalf("after %d steps down the index is %d, want %d", i, p.jqHistoryIdx, i)
			}
		}
		if p.jqInput != "" {
			t.Errorf("at the sentinel the input is %q, want it empty", p.jqInput)
		}
		// One more step changes nothing: the sentinel is the bottom.
		p.jqHistoryNavigate(1)
		if p.jqHistoryIdx != 3 || p.jqInput != "" {
			t.Errorf("stepping past the sentinel gave index %d input %q", p.jqHistoryIdx, p.jqInput)
		}
	})

	t.Run("up past the first entry stops at the first, not the sentinel", func(t *testing.T) {
		p := build(t, 3)
		p.jqHistoryIdx = len(p.jqHistory) // start at the sentinel
		p.jqInput = ""

		for want := 2; want >= 0; want-- {
			p.jqHistoryNavigate(-1)
			if p.jqHistoryIdx != want {
				t.Fatalf("the index is %d, want %d", p.jqHistoryIdx, want)
			}
			if p.jqInput != p.jqHistory[want] {
				t.Errorf("the input is %q, want %q", p.jqInput, p.jqHistory[want])
			}
		}

		// One more step up: index 0 is the top, and stepping up from there must NOT wrap to the
		// bottom. Wrapping is the reading a `newIdx < 0` clamp with the wrong constant gives,
		// and it is the difference between a history and a carousel.
		p.jqHistoryNavigate(-1)
		if p.jqHistoryIdx != 0 {
			t.Errorf("stepping up past the first entry gave the index %d, want 0", p.jqHistoryIdx)
		}
		if p.jqInput != p.jqHistory[0] {
			t.Errorf("the input is %q, want the first entry %q", p.jqInput, p.jqHistory[0])
		}
	})

	t.Run("an empty history is not walked at all", func(t *testing.T) {
		p := build(t, 0)
		p.jqHistoryNavigate(1)
		p.jqHistoryNavigate(-1)
		if p.jqHistoryIdx != 0 || p.jqInput != "" {
			t.Errorf("walking an empty history gave index %d input %q", p.jqHistoryIdx, p.jqInput)
		}
	})
}

// The history's cap, at the boundary.
//
// `len(p.jqHistory) > maxJQHistory` drops from the FRONT, so the entries that survive are the
// most recent ones. At exactly the cap nothing is dropped; one more and the oldest is gone.
func TestTheHistoryKeepsTheMostRecentEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		add     int
		wantLen int
	}{
		{"one below the cap", maxJQHistory - 1, maxJQHistory - 1},
		{"exactly the cap", maxJQHistory, maxJQHistory},
		{"one above the cap", maxJQHistory + 1, maxJQHistory},
		{"far above the cap", maxJQHistory * 2, maxJQHistory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t) // the history is a file; see the note above
			p := previewOf(t, jqDoc)
			p.EnterJQMode()
			for i := 0; i < tc.add; i++ {
				p.addToHistory(fmt.Sprintf(".k%04d", i))
			}

			if len(p.jqHistory) != tc.wantLen {
				t.Fatalf("%d entries kept, want %d", len(p.jqHistory), tc.wantLen)
			}
			// The survivors are the RECENT ones. The first is not entry zero unless nothing
			// was dropped, and the last is always the one just added.
			wantFirst := tc.add - tc.wantLen
			if got := p.jqHistory[0]; got != fmt.Sprintf(".k%04d", wantFirst) {
				t.Errorf("the oldest kept entry is %q, want .k%04d -- the cap drops from the FRONT", got, wantFirst)
			}
			if got := p.jqHistory[len(p.jqHistory)-1]; got != fmt.Sprintf(".k%04d", tc.add-1) {
				t.Errorf("the newest entry is %q, want the one just added", got)
			}
		})
	}

	t.Run("and re-adding an entry moves it to the end instead of duplicating it", func(t *testing.T) {
		// addToHistory removes a match before appending, so the list is a set ordered by
		// recency. A duplicate would make the cap hold fewer distinct entries than it claims.
		isolate(t)
		p := previewOf(t, jqDoc)
		p.EnterJQMode()
		p.addToHistory(".a")
		p.addToHistory(".b")
		p.addToHistory(".a")

		if len(p.jqHistory) != 2 {
			t.Fatalf("%d entries, want 2 -- .a was added twice", len(p.jqHistory))
		}
		if p.jqHistory[0] != ".b" || p.jqHistory[1] != ".a" {
			t.Errorf("the history is %v, want [.b .a] with the re-added entry last", p.jqHistory)
		}
	})
}

// A type name longer than the type column WRAPS, and the box grows a row for it.
//
// Pinned, not fixed. The exact wiring point is the row assembly in renderJQSuggestions:
//
//	typeStr := p.styles.TextMuted.Render(sug.Type)
//	typePad := 14 - lipgloss.Width(typeStr)
//	if typePad > 0 {                          <- padding only; nothing ever CLIPS
//	    typeStr += strings.Repeat(" ", typePad)
//	}
//	line := "    " + p.styles.Text.Render(path) + " " + typeStr
//
// A type of 23 columns makes a 42-column row, and lipgloss's bordered box WRAPS it: the popup
// comes back five rows tall instead of three, and the continuation rows start hard against the
// left border with none of the four-space indent. Compare the path column, four lines above,
// which DOES clip:
//
//	if lipgloss.Width(path) > pathWidth {
//	    path = ansi.Truncate(path, pathWidth, "...")
//	}
//
// So one column of the same row clips and the other wraps, and the reason is visible in the
// source: the path column has a truncate and the type column does not.
//
// It cannot happen today, and the reason is the closed vocabulary: every JQSuggestion in this
// package is built from jsonType's seven literals or from the two the array arms add, and the
// longest of those is `array-iter` at ten columns. That is what this case holds -- the wrap is
// unreachable, not absent -- so if jsonType ever grows a member wider than fourteen, this test
// fails and says why the popup will start growing rows.
func TestATypeWiderThanItsColumnWrapsTheBox(t *testing.T) {
	// The vocabulary first, so the wrap below is read against the fact that makes it
	// unreachable rather than as a claim about what users see.
	for _, typ := range []string{"null", "object", "array", "string", "number", "bool", "unknown", "array-iter", "length"} {
		if w := len([]rune(typ)); w >= 14 {
			t.Errorf("the vocabulary member %q is %d columns, so the popup can wrap after all", typ, w)
		}
	}

	p := previewOf(t, jqDoc)
	p.SetWidth(100)
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1

	// What a type inside the column looks like: one content row.
	p.jqSugs = []JQSuggestion{{Path: "k", Type: "object"}}
	fits := len(popupText(p))
	if fits != 3 { // top border, one row, bottom border
		t.Fatalf("a type inside the column produced a %d row box, want 3", fits)
	}

	// And what one outside it looks like, which nothing in the package can produce.
	const tooWide = "a-fairly-long-type-name"
	p.jqSugs = []JQSuggestion{{Path: "k", Type: tooWide}}
	wrapped := len(popupText(p))
	if wrapped <= fits {
		t.Skip("a wide type no longer wraps -- the type column clips now, so this pin is stale")
	}
	// The continuation carries no indent, which is the visible half of the problem.
	last := popupText(p)[wrapped-2]
	if strings.HasPrefix(last, "│    ") {
		t.Errorf("the wrapped row is indented like a real one, so the wrapping looks intentional:\n%q", last)
	}

	// And the box is still as wide as its width floor asked for, so the wrap costs HEIGHT.
	if w := len([]rune(popupText(p)[0])); w != 92 {
		t.Errorf("the box is %d columns wide, want 92 -- wrapping must not widen it either", w)
	}
}
