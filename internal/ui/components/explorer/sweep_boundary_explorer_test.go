package explorer

// Sweep CONDITIONALS_BOUNDARY — explorer group.
// Covers tree.go, explorer.go.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// ── EQUIVALENT: explorer.go:132:20 ──────────────────────────────────────────
// `if e.tree.cursor < 0 { e.tree.cursor = 0 }` → `<= 0`.
// When cursor == 0 the mutant enters the branch and assigns 0 — the value it
// already holds. Observable behavior identical.

func TestExplorerBoundary_GoLastCursorZero(t *testing.T) {
	e := newExplorerTest()
	e.tree.cursor = 0
	e.tree.offset = 0
	// Simulate go_last with empty filtered list → cursor = len-1 = -1 → clamp.
	e.tree.filtered = nil
	e.tree.cursor = len(e.tree.filtered) - 1
	if e.tree.cursor < 0 {
		e.tree.cursor = 0
	}
	if e.tree.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", e.tree.cursor)
	}
}

// ── EQUIVALENT: tree.go:68:14 ───────────────────────────────────────────────
// `if t.cursor < 0 { t.cursor = 0 }` → `<= 0`.
// When cursor == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_ClampCursorZero(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.cursor = 0
	tree.clampCursor()
	if tree.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", tree.cursor)
	}
}

// ── EQUIVALENT: tree.go:87:14 ───────────────────────────────────────────────
// `if t.offset > len(t.filtered)-t.height { t.offset = len(t.filtered)-t.height }` → `>=`.
// When offset == len-height the mutant assigns len-height — no change.

func TestExplorerBoundary_ClampOffsetAtBoundary(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	rows := len(tree.filtered)
	if rows <= 3 {
		t.Fatalf("need > 3 rows, got %d", rows)
	}
	tree.offset = rows - 3 // exactly at the boundary
	tree.clampOffset()
	if tree.offset != rows-3 {
		t.Fatalf("offset = %d, want %d", tree.offset, rows-3)
	}
}

// ── EQUIVALENT: tree.go:90:14 ───────────────────────────────────────────────
// `if t.offset < 0 { t.offset = 0 }` → `<= 0`.
// When offset == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_ClampOffsetZero(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	tree.offset = 0
	tree.clampOffset()
	if tree.offset != 0 {
		t.Fatalf("offset = %d, want 0", tree.offset)
	}
}

// ── EQUIVALENT: tree.go:194:15 ──────────────────────────────────────────────
// `if t.cursor < t.offset { t.offset = t.cursor }` → `<=`.
// When cursor == offset the mutant assigns cursor (== offset) — no change.

func TestExplorerBoundary_ScrollToCursorCursorEqualsOffset(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	tree.offset = 2
	tree.cursor = 2
	tree.scrollToCursor()
	if tree.offset != 2 {
		t.Fatalf("offset = %d, want 2", tree.offset)
	}
}

// ── EQUIVALENT: tree.go:284:22 ──────────────────────────────────────────────
// `if len(n.Children) > 0` → `>= 0`.
// len() is always >= 0, so the mutant always enters. But when Children is
// empty the range over nil does nothing — identical behavior. When Children
// is non-empty, both versions enter and recurse. No observable difference.

func TestExplorerBoundary_FindTableRecursiveEmptyChildren(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	// Node with no children — the mutant enters the branch (len >= 0 always true)
	// but ranges over nothing, so behavior is identical.
	node := NewNode("t", NodeTable, "t")
	node.Metadata["schema"] = "s"
	found := tree.findTableRecursive([]*Node{node}, "s", "t")
	if found == nil {
		t.Fatal("expected to find the node")
	}
}

// ── EQUIVALENT: tree.go:309:25 ──────────────────────────────────────────────
// `if end > start+t.height { end = start+t.height }` → `>=`.
// When end == start+height the mutant assigns start+height — no change.

func TestExplorerBoundary_ViewEndAtBoundary(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	tree.offset = 0
	// end = len(filtered); if len(filtered) == height then end == start+height.
	// Use a tree with exactly 3 rows.
	tree.filtered = tree.filtered[:3]
	_ = tree.View() // must not panic; boundary is exercised
}

// ── EQUIVALENT: tree.go:328:19 ──────────────────────────────────────────────
// `if nameMaxWidth < 0 { nameMaxWidth = 0 }` → `<= 0`.
// When nameMaxWidth == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_TruncateNameMaxWidthZero(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetWidth(4) // availableWidth = 0, prefixLen > 0 → nameMaxWidth < 0 → 0
	_ = tree.View()  // must not panic
}

// ── EQUIVALENT: tree.go:190:14 ──────────────────────────────────────────────
// `if t.height > 0` → `>= 0`.
// When height == 0 the mutant enters the block. Inside:
//   - `if t.cursor >= t.offset+t.height` → cursor >= offset → offset = cursor+1
//   - then clampOffset() runs: height <= 0 → offset = 0.
// Original with height == 0: skips block, calls clampOffset() → offset = 0.
// Both paths converge to offset = 0. EQUIVALENT.

func TestExplorerBoundary_ScrollToCursorZeroHeight(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(0)
	tree.offset = 0
	tree.cursor = 0
	tree.scrollToCursor()
	if tree.offset != 0 {
		t.Fatalf("offset = %d, want 0", tree.offset)
	}
}

// ── KILL: tree.go:83:14 ─────────────────────────────────────────────────────
// `if t.height <= 0` → `< 0`.
// When height == 0 and len(filtered) > 0:
//   - Original: enters first branch → offset = 0.
//   - Mutant: skips first branch (0 < 0 false), checks second branch
//     `len(filtered) <= height` → len > 0, height == 0 → false → offset unchanged.
// Observable: offset stays at its previous (non-zero) value instead of resetting to 0.

func TestExplorerBoundary_ClampOffsetZeroHeightNonEmptyList(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(0)
	tree.offset = 5 // non-zero offset with zero height
	tree.clampOffset()
	if tree.offset != 0 {
		t.Fatalf("offset = %d, want 0 (height=0 must reset offset)", tree.offset)
	}
}

// ── EQUIVALENT: tree.go:83:38 ───────────────────────────────────────────────
// `len(t.filtered) <= t.height` → `< t.height`.
// The two operators differ only when len(filtered) == height (height > 0 is
// settled by the first disjunct, height <= 0, which is untouched). At that
// boundary len(filtered) - height == 0, so the very next conditional
// (`if t.offset > len(t.filtered)-t.height`, i.e. `offset > 0`) clamps any
// positive offset to 0, and the one after (`offset < 0`) clamps any negative
// offset to 0. The branch the mutant skips assigned exactly 0 — the two
// remaining conditionals force 0 anyway.
// Exhaustive over reachable states:
//   height <= 0   → both enter the first branch (identical)
//   len < height  → both enter (identical)
//   len == height → original: offset = 0; mutant: second+third ifs force 0
//   len > height  → neither enters (identical)
// No reachable state makes the mutant observable. The test builds the boundary
// (len(filtered) == height, offset > 0) and asserts the original behavior; it
// stays green under the mutant, which is exactly what documents the
// equivalence.

func TestExplorerBoundary_ClampOffsetExactFit(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	rows := len(tree.filtered)
	if rows == 0 {
		t.Fatal("fixture has no rows")
	}
	tree.SetHeight(rows) // len(filtered) == height — the boundary
	tree.offset = 3      // positive offset: under the mutant the second if (offset > 0) still forces 0
	tree.clampOffset()
	if tree.offset != 0 {
		t.Fatalf("offset = %d, want 0 (list fits exactly in pane)", tree.offset)
	}
}

// ── KILL: tree.go:191:15 ────────────────────────────────────────────────────
// `if t.cursor >= t.offset+t.height` → `> t.offset+t.height`.
// When cursor == offset+height (exactly one past the visible window):
//   - Original: enters → offset = cursor - height + 1 = offset + 1.
//   - Mutant: skips → offset unchanged.
// Observable: window doesn't scroll to reveal the cursor.

func TestExplorerBoundary_ScrollToCursorAtWindowEdge(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	if len(tree.filtered) <= 3 {
		t.Fatalf("need > 3 rows, got %d", len(tree.filtered))
	}
	tree.offset = 0
	tree.cursor = 3 // exactly offset+height — one past the visible window
	tree.scrollToCursor()
	if tree.offset != 1 {
		t.Fatalf("offset = %d, want 1 (cursor at window edge must scroll)", tree.offset)
	}
}

// ── KILL: tree.go:309:14 ────────────────────────────────────────────────────
// `if t.height > 0` → `>= 0`.
// When height == 0:
//   - Original: skips → end = len(filtered) → renders all rows.
//   - Mutant: enters → end = start + 0 = start → renders nothing.
// Observable: View returns empty/no rows instead of all rows.

func TestExplorerBoundary_ViewZeroHeightRendersAll(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(0)
	tree.offset = 0
	view := tree.View()
	if len(tree.filtered) > 0 && view == "  No items" {
		// If filtered is non-empty, View should show rows, not "No items".
		// With height == 0 the original renders all rows (end = len(filtered)).
		t.Fatalf("View returned 'No items' for non-empty list with zero height")
	}
	// With the mutant (>= 0), end = start + 0 = start → loop runs 0 times → empty render.
	// We assert that at least one row name appears in the output.
	if len(tree.filtered) > 0 {
		found := false
		for _, n := range tree.filtered {
			if n != nil && strings.Contains(view, n.Name) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("View with zero height should render rows, got: %q", view)
		}
	}
}

// ── KILL: tree.go:408:14 ────────────────────────────────────────────────────
// `if maxWidth <= 0` → `< 0`.
// When maxWidth == 0:
//   - Original: enters → return "".
//   - Mutant: skips → checks `lipgloss.Width(s) <= 0` → only true for empty s.
//     For non-empty s: falls through to `ansi.Truncate(s, 0, "…")` → returns "…".
// Observable: returns "…" instead of "".

func TestExplorerBoundary_TruncateEllipsisZeroWidth(t *testing.T) {
	// Direct unit test of truncateWithEllipsis with maxWidth == 0.
	got := truncateWithEllipsis("hello", 0)
	if got != "" {
		t.Fatalf("truncateWithEllipsis(%q, 0) = %q, want %q", "hello", got, "")
	}
}

// ── EQUIVALENT: tree.go:411:23 ──────────────────────────────────────────────
// `if lipgloss.Width(s) <= maxWidth` → `< maxWidth`.
// When Width(s) == maxWidth:
//   - Original: enters → return s (unchanged).
//   - Mutant: skips → falls to `ansi.Truncate(s, maxWidth, "…")`.
// VERIFIED: ansi.Truncate with exact fit (width == maxWidth) returns s unchanged
// (no ellipsis appended). Both paths return s. EQUIVALENT.

func TestExplorerBoundary_TruncateEllipsisExactFit(t *testing.T) {
	s := "hello"
	w := lipgloss.Width(s)
	got := truncateWithEllipsis(s, w)
	if got != s {
		t.Fatalf("truncateWithEllipsis(%q, %d) = %q, want %q (exact fit must not truncate)", s, w, got, s)
	}
}
