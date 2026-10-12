package store

import "testing"

// Scenario: trimOldest with n == 0 — the exact boundary of `if n > 0`. Relaxed
// to `n >= 0` the guard opens and the function returns
// entries[len(entries)-0:], an empty slice: a configured limit of zero would
// erase the history instead of meaning "no limit".
func TestStoreBoundaryTrimOldestZeroKeepsEverything(t *testing.T) {
	entries := []QueryEntry{{SQL: "a"}, {SQL: "b"}}
	got := trimOldest(entries, 0)
	if len(got) != 2 {
		t.Fatalf("trimOldest(2 entries, 0) = %d entries, want 2: zero means no limit", len(got))
	}
}

// Scenario: trimOldest with n == len(entries) — the exact boundary of
// `len(entries) > n`. Relaxed to `>=` the arm runs and returns
// entries[len(entries)-n:] = entries[0:], the same elements in the same
// order: the slice header moves, the contents do not.
func TestStoreBoundaryTrimOldestExactCapIsIdentity(t *testing.T) {
	entries := []QueryEntry{{SQL: "a"}, {SQL: "b"}, {SQL: "c"}}
	got := trimOldest(entries, len(entries))
	if len(got) != 3 {
		t.Fatalf("trimOldest at the cap = %d entries, want 3", len(got))
	}
	for i, e := range got {
		if e.SQL != entries[i].SQL {
			t.Errorf("entry %d = %q, want %q: the contents are unchanged at the boundary", i, e.SQL, entries[i].SQL)
		}
	}
}

// Scenario: All over an ODD number of entries — the exact boundary of `i < j`
// in the reversal loop. With an odd count the middle iteration has i == j;
// relaxed to `i <= j` the arm runs and swaps the middle entry with itself, a
// no-op. The three-entry run pins the full reversal, middle included.
func TestStoreBoundaryAllReversesOddCount(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("first")
	s.Add("second")
	s.Add("third")

	got := s.All()
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("All() = %d entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].SQL != w {
			t.Errorf("All()[%d] = %q, want %q", i, got[i].SQL, w)
		}
	}
}

// Scenario: Favorites over an odd number of favorites — the same `i < j`
// boundary in the filter's own reversal loop, and the same self-swap no-op
// when relaxed to `i <= j`.
func TestStoreBoundaryFavoritesReversesOddCount(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("first")
	s.Add("second")
	s.Add("third")
	s.ToggleFavorite(0)
	s.ToggleFavorite(1)
	s.ToggleFavorite(2)

	got := s.Favorites()
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("Favorites() = %d entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].SQL != w {
			t.Errorf("Favorites()[%d] = %q, want %q", i, got[i].SQL, w)
		}
	}
}
