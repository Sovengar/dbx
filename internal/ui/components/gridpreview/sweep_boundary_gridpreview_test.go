package gridpreview

// Scenario: Los supervivientes CONDITIONALS_BOUNDARY de internal/ui/components/gridpreview/preview.go.
//
// The boundary mutant swaps an ordered comparison for its neighbour (`<`→`<=`, `>`→`>=`),
// which is only observable when a value lands EXACTLY on the boundary. Six variants die
// there: a clamp that re-assigns the value it was already equal to, a truncation whose
// count is zero, a sentinel that addresses nothing. Sixteen are equivalent, and each one
// below is proved rather than asserted: the test builds the boundary, pins the behaviour
// the original has there, and stays green under the mutant because the mutant cannot
// change anything observable at that point — the clamped value equals the one assigned,
// the truncated slice is the whole slice, and strings.Repeat(" ", 0) is "".
//
// The six that die fail three different ways: a map entry that must not exist (191), an
// index that walks off the end of a slice into a panic (290, 1276), a popup turned on with
// nothing to show (513), a selection that addresses nothing reported as addressing
// something (861), and a stable sort that reorders equal elements because `<=` is not a
// strict weak ordering (938).

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Fixture documents sized so the arithmetic lands exactly on a boundary: N keys marshal
// to N+2 indented lines, which is what makes a scroll window or a content height equal
// to the document's length.
const (
	gridPreviewBoundaryDoc8Keys  = `{"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8}`
	gridPreviewBoundaryDoc12Keys = `{"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8,"k9":9,"k10":10,"k11":11,"k12":12}`
)

// ---------------------------------------------------------------------------
// KILLED — ExpandFK records a nested-FK entry when there are no nested keys.
//
// 191 col 28: `if len(nestedForeignKeys) > 0` → `>=`. The guard decides whether the
// expanded object carries FK metadata for paths below it. With an empty list the original
// records nothing; the mutant records the path with a nil slice, so a later isExpandableFK
// lookup finds a parent that declares metadata it does not have. The observable is the map
// membership itself: the key must be absent.
func TestGridPreviewBoundaryExpandFKWithoutNestedKeys(t *testing.T) {
	p := newPreviewTest()
	p.ExpandFK("col", "path", map[string]interface{}{"x": 1}, nil)

	if _, ok := p.nestedFKs["path"]; ok {
		t.Errorf("ExpandFK recorded a nested-FK entry with no nested keys: %v", p.nestedFKs["path"])
	}
}

// ---------------------------------------------------------------------------
// KILLED — handleExpand refuses a cursor past the last line.
//
// 290 col 18: `if p.cursorLine >= len(p.lines)` → `>`. The guard is the only thing
// standing between the tracker loop and p.lines[cursorLine]. With the cursor exactly at
// len(lines) the original returns (nil, false); the mutant falls through and indexes the
// slice one past its end. The panic is the failure, so the test recovers to report it
// cleanly and still asserts the original's answer.
func TestGridPreviewBoundaryHandleExpandCursorAtEndOfLines(t *testing.T) {
	p := newPreviewTest()
	p.cursorLine = len(p.lines)

	var cmd tea.Cmd
	var handled bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleExpand with cursorLine == len(lines) panicked: %v", r)
			}
		}()
		cmd, handled = p.handleExpand()
	}()

	if cmd != nil {
		t.Errorf("handleExpand returned a cmd at the end of the document, want nil")
	}
	if handled {
		t.Error("handleExpand reported handled at the end of the document, want false")
	}
}

// ---------------------------------------------------------------------------
// KILLED — ctrl+space never turns the popup on with nothing to suggest.
//
// 513 col 49: `len(p.jqSugs) > 0` → `>=` in the ctrl+space arm. The arm computes
// `!wasVisible && len(p.jqSugs) > 0`: the popup opens only if there is something to show.
// An empty list with the popup already closed is the boundary — the original answers
// false, the mutant answers `!false && 0 >= 0` and turns a popup on with no rows in it.
// The flag is the observable: an empty document cannot produce suggestions, so it stays off.
func TestGridPreviewBoundaryCtrlSpaceWithNoSuggestions(t *testing.T) {
	p := newPreviewTest()
	p.SetRow(nil, nil) // no document → updateJQSuggestions returns with an empty list
	p.jqSugVisible = false

	p.handleJQInput(tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl})

	if p.jqSugVisible {
		t.Error("ctrl+space turned the popup on with no suggestions available")
	}
}

// ---------------------------------------------------------------------------
// KILLED (IN-ALLOWLIST) — moveJQSuggestionSelection stays put at zero.
//
// 861 col 21: `p.jqSugSelected < 0` → `<=`. The lower wrap sends a selection that walked
// before the first entry back to the last. Landing on 0 is the boundary: the original
// leaves the selection at 0, the mutant enters the wrap and jumps it to len-1 — the wrong
// end of the list for a selection that never left it. The fixture walks the selection onto
// 0 and asserts it stayed there.
func TestGridPreviewBoundaryMoveJQSuggestionSelectionStaysAtZero(t *testing.T) {
	p := newPreviewTest()
	p.jqSugs = []JQSuggestion{
		{Path: ".a", Type: "number"},
		{Path: ".b", Type: "string"},
	}
	p.jqSugSelected = 1

	p.moveJQSuggestionSelection(-1) // 1 - 1 == 0: exactly the lower-wrap boundary

	if p.jqSugSelected != 0 {
		t.Errorf("selection = %d, want 0 (zero is a legal selection, not a wrap)", p.jqSugSelected)
	}
}

// ---------------------------------------------------------------------------
// KILLED — sortJQSuggestions is stable on equal paths.
//
// 938 col 67: `sugs[i].Path < sugs[j].Path` → `<=`. SliceStable promises that equal
// elements keep their relative order, and the promise is load-bearing: the list is what
// tab accepts, so two equal entries swapping places changes which one is inserted. With
// `<` the comparison is a strict weak ordering and equal paths never swap; with `<=` it
// is not — insertion sort sees less(j, j-1) as true for equal neighbours and walks each
// one past the other, reversing them. Distinct paths cannot tell the two apart: <= and <
// answer the same on unequal strings, so the fixture duplicates a path on purpose.
func TestGridPreviewBoundarySortJQSuggestionsStableOnDuplicatePaths(t *testing.T) {
	sugs := []JQSuggestion{
		{Path: "dup", Type: "first"},
		{Path: "dup", Type: "second"},
	}

	sortJQSuggestions(sugs)

	if sugs[0].Type != "first" {
		t.Errorf("sort reordered equal paths: first entry is %q, want %q (stable)", sugs[0].Type, "first")
	}
}

// ---------------------------------------------------------------------------
// KILLED — highlightJSON recognises a 9 as part of a number.
//
// 1276 col 52: `line[j] <= '9'` → `<`. The scan that consumes the digits of a number
// starts AFTER the first character, so the boundary is only reachable by a 9 that
// FOLLOWS another digit: 59 is one number to the original and two to the mutant, which
// renders the 5 and the 9 separately — an extra style prefix between them. A 9 at the
// start or end of the number cannot tell the two apart (the loop never sees the first
// character, and at the end it has nowhere to advance), so the fixture puts the 9
// second and asserts the exact bytes the original produces.
func TestGridPreviewBoundaryHighlightJSONNumberEndingInNine(t *testing.T) {
	p := newPreviewTest()

	got := p.highlightJSON("\"a\": 59")
	want := p.styles.Primary.Render(`"a"`) + ": " + p.styles.Info.Render("59")
	if got != want {
		t.Errorf("highlightJSON returned %q, want %q (the 5 and the 9 highlighted as one number)", got, want)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — ensureCursorVisible with the cursor exactly at the scroll line.
//
// 365 col 18: `p.cursorLine < p.scrollY` → `<=`. The guard pulls the window up to the
// cursor when the cursor sits above it. When cursorLine == scrollY the cursor is already
// the first visible line: the original leaves the window alone, the mutant assigns
// scrollY = cursorLine, which is the value it already holds. Both leave the window where
// it was, so the fixture pins scrollY at the boundary and asserts it did not move.
func TestGridPreviewBoundaryEnsureCursorVisibleCursorEqualsScrollY(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetHeight(20)
	p.cursorLine = 3
	p.scrollY = 3

	p.ensureCursorVisible()

	if p.scrollY != 3 {
		t.Errorf("scrollY = %d, want 3 (cursor already visible at the boundary)", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — ensureCursorVisible with maxScroll exactly zero.
//
// 369 col 15: `maxScroll < 0` → `<=`. maxScroll is len(lines) - contentHeight, clamped
// at zero so a document shorter than the window cannot scroll past its end. A document
// exactly as tall as the window makes it 0: the original does not enter the clamp, the
// mutant assigns maxScroll = 0, the value it already holds. The fixture uses a 12-key
// document (14 lines) in a 20-row panel (contentHeight 14) and asserts the scroll is
// driven to 0 either way.
func TestGridPreviewBoundaryEnsureCursorVisibleMaxScrollZero(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc12Keys)
	p.SetHeight(20) // contentHeight 14 == len(lines) 14 → maxScroll == 0
	p.scrollY = 5

	p.ensureCursorVisible()

	if p.scrollY != 0 {
		t.Errorf("scrollY = %d, want 0 (maxScroll floors at 0)", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — ensureCursorVisible with scrollY exactly at maxScroll.
//
// 372 col 15: `p.scrollY > maxScroll` → `>=`. The clamp pulls a too-far window back to
// the last legal position. When scrollY == maxScroll the window is already there: the
// original leaves it, the mutant assigns scrollY = maxScroll, the same value. The fixture
// sits scrollY on the boundary and asserts it did not move.
func TestGridPreviewBoundaryEnsureCursorVisibleScrollYEqualsMaxScroll(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc12Keys)
	p.SetHeight(20) // maxScroll == 0
	p.cursorLine = 0
	p.scrollY = 0 // scrollY == maxScroll

	p.ensureCursorVisible()

	if p.scrollY != 0 {
		t.Errorf("scrollY = %d, want 0 (already at maxScroll)", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — the suggestion list truncated at exactly its cap.
//
// 635 col 19: `len(p.jqSugs) > maxJQSuggestions` → `>=`. The cap keeps the popup at
// maxJQSuggestions rows. A document with exactly 8 keys yields exactly 8 suggestions:
// the original does not truncate, the mutant re-slices to [:8] — the whole slice, same
// elements, same order. The fixture pins the full list of 8 paths, which holds under
// both.
func TestGridPreviewBoundaryUpdateJQSuggestionsExactlyMaxSuggestions(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc8Keys)
	p.jqInput = ""
	p.jqCursor = 0

	p.updateJQSuggestions()

	if len(p.jqSugs) != 8 {
		t.Fatalf("precondition failed: %d suggestions, want exactly 8", len(p.jqSugs))
	}
	want := map[string]bool{
		".k1": false, ".k2": false, ".k3": false, ".k4": false,
		".k5": false, ".k6": false, ".k7": false, ".k8": false,
	}
	for _, s := range p.jqSugs {
		if _, ok := want[s.Path]; !ok {
			t.Fatalf("unexpected suggestion %q in %v", s.Path, suggestionPaths(p))
		}
		want[s.Path] = true
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("suggestion %q is missing from the list of %d", path, len(p.jqSugs))
		}
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — jq history navigation landing on index zero.
//
// 910 col 12: `newIdx < 0` → `<=`. The lower clamp keeps a walk up from going before
// the first entry. Landing on 0 is the boundary: the original leaves the index, the
// mutant assigns 0, the value it already holds. Both then read history[0] into the input.
func TestGridPreviewBoundaryJQHistoryNavigateNewIdxZero(t *testing.T) {
	p := newPreviewTest()
	p.jqHistory = []string{".a"}
	p.jqHistoryIdx = 0

	p.jqHistoryNavigate(0)

	if p.jqHistoryIdx != 0 {
		t.Errorf("history index = %d, want 0", p.jqHistoryIdx)
	}
	if p.jqInput != ".a" {
		t.Errorf("input is %q, want the recalled %q", p.jqInput, ".a")
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — jq history navigation landing on the length sentinel.
//
// 913 col 12: `newIdx > len(p.jqHistory)` → `>=`. The upper clamp keeps a walk down
// from going past the last entry. The index equal to the length is meaningful — it is
// the "recalled nothing" sentinel, which empties the input — so the original refuses to
// clamp a value that is already there; the mutant assigns len(history), the same value.
// Both then take the sentinel branch and clear the input.
func TestGridPreviewBoundaryJQHistoryNavigateNewIdxAtLength(t *testing.T) {
	p := newPreviewTest()
	p.jqHistory = []string{".a"}
	p.jqHistoryIdx = 1

	p.jqHistoryNavigate(0)

	if p.jqHistoryIdx != 1 {
		t.Errorf("history index = %d, want 1 (the recalled-nothing sentinel)", p.jqHistoryIdx)
	}
	if p.jqInput != "" {
		t.Errorf("input is %q, want empty (nothing recalled at the sentinel)", p.jqInput)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — the history trimmed at exactly its cap.
//
// 950 col 22: `len(p.jqHistory) > maxJQHistory` → `>=`. addToHistory drops the oldest
// entry once the list exceeds maxJQHistory (50). A list of 49 accepting one more lands
// on exactly 50: the original does not trim, the mutant re-slices to history[50-50:],
// which is history[0:] — the whole list. The fixture pins the 50 entries, first and
// last, which hold under both.
func TestGridPreviewBoundaryAddToHistoryExactlyMaxJQHistory(t *testing.T) {
	p := newPreviewTest()
	history := make([]string, 0, 49)
	for i := 0; i < maxJQHistory-1; i++ {
		history = append(history, fmt.Sprintf(".h%d", i))
	}
	p.jqHistory = history

	p.addToHistory(".new")

	if len(p.jqHistory) != maxJQHistory {
		t.Fatalf("history holds %d entries, want %d", len(p.jqHistory), maxJQHistory)
	}
	if p.jqHistory[0] != ".h0" {
		t.Errorf("first entry is %q, want %q", p.jqHistory[0], ".h0")
	}
	if p.jqHistory[len(p.jqHistory)-1] != ".new" {
		t.Errorf("last entry is %q, want %q", p.jqHistory[len(p.jqHistory)-1], ".new")
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — halfPageUp with the scroll already at zero.
//
// 1007 col 15: `p.scrollY < 0` → `<=`. The clamp after the subtraction keeps a scroll
// above the document from going negative. A panel one row tall makes height/2 == 0, so
// the subtraction leaves scrollY at exactly 0: the original does not enter the clamp,
// the mutant assigns 0, the value it already holds. Both leave it where it was.
func TestGridPreviewBoundaryHalfPageUpScrollYZero(t *testing.T) {
	p := newPreviewTest()
	p.scrollY = 0
	p.height = 1 // height/2 == 0, so the subtraction lands scrollY on exactly 0

	p.halfPageUp()

	if p.scrollY != 0 {
		t.Errorf("scrollY = %d, want 0", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — halfPageDown with maxScroll exactly zero.
//
// 1015 col 15: `maxScroll < 0` → `<=`. maxScroll is len(lines) - height + 6, clamped
// at zero. A 12-key document (14 lines) in a 20-row panel makes it exactly 0: the
// original does not enter the clamp, the mutant assigns 0, the same value. Both then
// pull a too-far scroll back to the floor.
func TestGridPreviewBoundaryHalfPageDownMaxScrollZero(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc12Keys)
	p.SetHeight(20) // maxScroll = 14 - 20 + 6 == 0
	p.scrollY = 3

	p.halfPageDown()

	if p.scrollY != 0 {
		t.Errorf("scrollY = %d, want 0 (maxScroll floors at 0)", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — halfPageDown with scrollY exactly at maxScroll.
//
// 1018 col 15: `p.scrollY > maxScroll` → `>=`. The clamp pulls a too-far scroll back to
// the last legal position. A one-row panel makes height/2 == 0, so the addition leaves
// scrollY on the boundary it already equals: the original leaves it, the mutant assigns
// maxScroll, the same value. The fixture asserts the scroll did not move.
func TestGridPreviewBoundaryHalfPageDownScrollYEqualsMaxScroll(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc12Keys)
	p.height = 1   // height/2 == 0 → scrollY unchanged
	p.scrollY = 19 // maxScroll = 14 - 1 + 6 == 19

	p.halfPageDown()

	if p.scrollY != 19 {
		t.Errorf("scrollY = %d, want 19 (already at maxScroll)", p.scrollY)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — Render with the window end exactly at the document's end.
//
// 1120 col 9: `end > len(p.lines)` → `>=`. The clamp trims a window that would run past
// the document. When start + contentHeight == len(lines) the window already ends exactly
// at the last line: the original leaves end alone, the mutant assigns len(lines), the
// same value. Both render the whole document, so the fixture counts the rendered lines
// and asks for the document's closing brace.
func TestGridPreviewBoundaryRenderEndEqualsLines(t *testing.T) {
	p := previewOf(t, gridPreviewBoundaryDoc12Keys)
	p.SetHeight(20) // contentHeight 14 == len(lines) 14 → end == len(lines)
	p.scrollY = 0

	lines := strings.Split(ansi_Strip(p.Render()), "\n")
	if len(lines) != 14 {
		t.Fatalf("render has %d lines, want the document's 14:\n%s", len(lines), ansi_Strip(p.Render()))
	}
	if lines[len(lines)-1] != "}" {
		t.Errorf("last rendered line is %q, want the document's closing %q", lines[len(lines)-1], "}")
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — the jq prompt bar padded to exactly the panel's inner width.
//
// 1182 col 14: `barWidth < p.width-4` → `<=`. The padding fills the bar out to the
// panel's inner width. An empty input in an 11-wide panel makes the bar exactly 7 columns
// (6 for " jq │ ", 1 for the cursor block), which is width-4: the original does not pad,
// the mutant pads with strings.Repeat(" ", width-4-barWidth) — a count of zero, which
// appends "". The fixture asks for the bar to be exactly its text, with no filler.
func TestGridPreviewBoundaryRenderJQPromptBarWidthExact(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(11) // bar is 7 columns == width-4
	p.EnterJQMode()
	p.jqInput = ""
	p.jqCursor = 0
	p.jqSugVisible = false

	if got := ansi_Strip(p.renderJQPrompt()); got != " jq │ █" {
		t.Errorf("prompt bar is %q, want %q (exactly the panel's inner width, no filler)", got, " jq │ █")
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — the suggestion popup at exactly its width floor.
//
// 1206 col 9: `maxWidth < 30` → `<=`. The floor keeps a narrow panel from squeezing the
// popup below 30 columns. A 38-wide panel makes width-8 exactly 30: the original does not
// raise it, the mutant assigns 30, the value it already holds. Both draw the same box,
// so the fixture measures the popup at the boundary width.
func TestGridPreviewBoundaryRenderJQSuggestionsMaxWidthFloor(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(38) // maxWidth == 30 exactly
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	p.jqSugs = []JQSuggestion{{Path: "k", Type: "string"}}

	if w := lipgloss.Width(ansi_Strip(p.renderJQSuggestions())); w != 30 {
		t.Errorf("popup is %d columns wide, want 30", w)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — a suggestion path exactly as wide as its column.
//
// 1220 col 27: `lipgloss.Width(path) > pathWidth` → `>=`. A path longer than its column
// is truncated with a "..." tail. A path of exactly pathWidth is the boundary: the
// original leaves it whole, and the mutant calls ansi.Truncate(path, pathWidth, "...")
// — which returns its argument untouched when the width is <= the length, tail included
// or not. Nothing observable differs, so the fixture pins the whole path in the row.
func TestGridPreviewBoundaryRenderJQSuggestionsPathAtPathWidth(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(100) // maxWidth 92 → pathWidth 78
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	path := strings.Repeat("k", 78)
	p.jqSugs = []JQSuggestion{{Path: path, Type: "string"}}

	row := popupText(p)[1]
	if !strings.Contains(row, path) {
		t.Errorf("the %d-column path is not whole in %q", len(path), row)
	}
	if strings.Contains(row, "...") {
		t.Errorf("the path at exactly pathWidth gained a tail: %q", row)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — a path that fills its column gets no padding.
//
// 1224 col 14: `pathPad > 0` → `>=`. The path column is padded out to pathWidth so the
// type starts at a fixed offset. A path of exactly pathWidth makes the pad zero: the
// original adds nothing, the mutant adds strings.Repeat(" ", 0) — "". The row is the
// same, so the fixture asks for the path followed by exactly one space and the type.
func TestGridPreviewBoundaryRenderJQSuggestionsPathPadZero(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(100) // pathWidth 78
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	path := strings.Repeat("k", 78)
	p.jqSugs = []JQSuggestion{{Path: path, Type: "string"}}

	row := popupText(p)[1]
	if !strings.Contains(row, path+" string") {
		t.Errorf("row is %q, want the path followed by one space and the type", row)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENT — a type exactly as wide as its column gets no padding.
//
// 1230 col 14: `typePad > 0` → `>=`. The type column is padded out to 14 columns. A
// type of exactly 14 makes the pad zero: the original adds nothing, the mutant adds
// strings.Repeat(" ", 0) — "". The row ends the same either way, so the fixture asks for
// the row to end with the unpadded type.
func TestGridPreviewBoundaryRenderJQSuggestionsTypePadZero(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(100)
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	typ := strings.Repeat("t", 14)
	p.jqSugs = []JQSuggestion{{Path: "k", Type: typ}}

	// The row is 5 columns over the box by design (4 of indent, 1 of separator), so the
	// border wraps it and the type lands on a later line of the popup. What matters at
	// the boundary is that the type is there at all, whole and unpadded: the mutant's
	// strings.Repeat(" ", 0) appends nothing, so the same bytes hold under both.
	popup := strings.Join(popupText(p), "\n")
	if !strings.Contains(popup, typ) {
		t.Errorf("popup is %q, want the unpadded %d-column type in it", popup, len(typ))
	}
}
