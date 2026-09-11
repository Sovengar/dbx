package palette

import "strings"

type match struct {
	score   int
	indices []int
}

func fuzzyMatch(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}

	q := strings.ToLower(query)
	t := strings.ToLower(target)

	qi := 0
	ti := 0
	score := 0
	prevMatch := false
	matchStart := -1

	for qi < len(q) && ti < len(t) {
		if q[qi] == t[ti] {
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
			if t[ti] >= 'A' && t[ti] <= 'Z' {
				score += 3
			}
			qi++
			prevMatch = true
		} else {
			prevMatch = false
		}
		ti++
	}

	if qi < len(q) {
		return 0, false
	}

	if matchStart == 0 {
		score += 10
	}

	return score, true
}

func fuzzySort(query string, items []command) []scoredCommand {
	var scored []scoredCommand

	for _, item := range items {
		score, ok := fuzzyMatch(query, item.Name)
		if !ok {
			score2, ok2 := fuzzyMatch(query, item.Alias)
			if ok2 {
				score = score2
				ok = true
			}
		}
		if ok {
			scored = append(scored, scoredCommand{
				command: item,
				score:   score,
			})
		}
	}

	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	return scored
}
