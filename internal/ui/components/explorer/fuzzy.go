package explorer

import "strings"

// fuzzyScore reports whether query is a subsequence of target and scores how
// good the match is: a higher score is a better match.
//
// A subsequence is the fuzzy half: every character of the query must appear in
// the target in order, but not necessarily next to each other, so "tms" finds
// "items" where the old strings.Contains could not. The score rewards matches
// that start early, sit next to each other, and begin a word, which keeps a
// contiguous hit ahead of a scattered one.
//
// Both sides are lowercased, so the filter stays case-insensitive exactly as
// the strings.Contains it replaced was.
func fuzzyScore(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}

	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(target))
	if len(q) > len(t) {
		return 0, false
	}

	qi := 0
	score := 0
	prevMatch := false
	matchStart := -1

	for ti, r := range t {
		if qi >= len(q) {
			break
		}
		if q[qi] != r {
			prevMatch = false
			continue
		}
		if matchStart == -1 {
			matchStart = ti
		}
		score += 10
		if prevMatch {
			score += 5
		}
		if ti == 0 || t[ti-1] == ' ' || t[ti-1] == '_' || t[ti-1] == '-' {
			score += 8
		}
		qi++
		prevMatch = true
	}

	if qi < len(q) {
		return 0, false
	}
	if matchStart == 0 {
		score += 10
	}
	return score, true
}
