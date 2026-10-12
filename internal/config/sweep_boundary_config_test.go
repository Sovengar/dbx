package config

import (
	"path/filepath"
	"testing"
)

// Scenario: digitRange when a later key repeats the running minimum — the exact
// boundary of `if n < lo`. Relaxed to `n <= lo` the arm runs and assigns lo = n,
// the value it already holds, so prefix/lo/hi/ok cannot move. The repeated
// minimum is also the only way n == lo arises among these inputs: the run is
// not contiguous, the function reports the zero tuple, and it reports the same
// zero tuple under the mutant.
func TestConfigBoundaryDigitRangeRepeatMinimumIsIdentity(t *testing.T) {
	prefix, lo, hi, ok := digitRange([]string{"f2", "f1", "f1"})
	if prefix != "" || lo != 0 || hi != 0 || ok {
		t.Errorf("digitRange(f2,f1,f1) = %q,%d,%d,%v, want the zero tuple: a repeated minimum is not a contiguous run", prefix, lo, hi, ok)
	}
	// The healthy run never compares n against lo, so it is unaffected too.
	prefix, lo, hi, ok = digitRange([]string{"f1", "f2", "f3"})
	if prefix != "f" || lo != 1 || hi != 3 || !ok {
		t.Errorf("digitRange(f1,f2,f3) = %q,%d,%d,%v, want f,1,3,true", prefix, lo, hi, ok)
	}
}

// Scenario: digitRange when a later key repeats the running maximum — the exact
// boundary of `if n > hi`. Relaxed to `n >= hi` the arm runs and assigns hi = n,
// the value it already holds. Same identity argument as the minimum.
func TestConfigBoundaryDigitRangeRepeatMaximumIsIdentity(t *testing.T) {
	prefix, lo, hi, ok := digitRange([]string{"f1", "f2", "f2"})
	if prefix != "" || lo != 0 || hi != 0 || ok {
		t.Errorf("digitRange(f1,f2,f2) = %q,%d,%d,%v, want the zero tuple: a repeated maximum is not a contiguous run", prefix, lo, hi, ok)
	}
	prefix, lo, hi, ok = digitRange([]string{"f1", "f2"})
	if prefix != "f" || lo != 1 || hi != 2 || !ok {
		t.Errorf("digitRange(f1,f2) = %q,%d,%d,%v, want f,1,2,true", prefix, lo, hi, ok)
	}
}

// Scenario: the sort's Name tiebreak stays unreachable, and the Path
// comparator at its own boundary is what keeps it that way. Two survivors can
// only share a Path if they came from the same directory, and the same
// directory means the same .dbx.toml and therefore the same FoundProject.Name
// — so `Path <= Path` at an equal Path swaps two indistinguishable entries,
// and with distinct Paths the short-circuit never asks Name anything. The scan
// below pins the invariant the dedup establishes: the duplicate collapses
// before the sort runs, and the output is the sorted pair.
func TestConfigBoundaryScannerPathTiebreakKeepsNameUnreachable(t *testing.T) {
	root := t.TempDir()
	dup := filepath.Join(root, "dup")
	writeConnections(t, dup, map[string]string{"p": "postgres://x/y"})
	other := filepath.Join(root, "other")
	writeConnections(t, other, map[string]string{"q": "postgres://x/z"})

	same := dbxPath(dup)
	s := NewScanner(root)
	s.findFiles = func(string) []string { return []string{same, same, dbxPath(other)} }

	got := s.Scan()
	if len(got) != 2 {
		t.Fatalf("the seam reported one path twice and the scan returned %d projects, want 2: dedup collapses the duplicate", len(got))
	}
	if got[0].Path != dup || got[1].Path != other {
		t.Errorf("order = [%q, %q], want [%q, %q] (sorted by path)", got[0].Path, got[1].Path, dup, other)
	}
}

// Scenario: isBetterSurvivor at the exact boundary of `len(a) < len(b)` — and
// the boundary is UNREACHABLE, because the return sits inside an
// `if len(a) != len(b)` guard. When the lengths differ, `<` and `<=` answer
// identically; when they are equal, the guard skips the return entirely. So
// the relaxed `len(a) <= len(b)` cannot change any answer the comparator can
// give. The test exercises the path where the return is actually evaluated —
// distinct lengths — and pins both outcomes, so the equivalence is asserted
// on the live arm and not on the skipped one.
func TestConfigBoundarySurvivorLengthBoundaryIsUnreachable(t *testing.T) {
	// Distinct lengths, the shorter first: true under both readings.
	if !isBetterSurvivor(cand("/a/b", false), cand("/a/aa", false)) {
		t.Error("isBetterSurvivor(/a/b, /a/aa) = false, want true: the shorter path wins")
	}
	// Distinct lengths, the longer first: false under both readings.
	if isBetterSurvivor(cand("/a/aa", false), cand("/a/b", false)) {
		t.Error("isBetterSurvivor(/a/aa, /a/b) = true, want false: the longer path does not win")
	}
	// Equal lengths skip the return and fall through to the lexicographic
	// tiebreak, which is the other reason the boundary cannot be observed.
	if isBetterSurvivor(cand("/a/zz", false), cand("/a/aa", false)) {
		t.Error("isBetterSurvivor(/a/zz, /a/aa) = true at equal length, want false: the lexicographically smaller path wins")
	}
}
