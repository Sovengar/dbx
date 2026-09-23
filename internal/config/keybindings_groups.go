package config

import (
	"fmt"
	"sort"
	"strings"
)

// DisplayGroup is one render unit for the keybinds pane and the help modal. A
// grouped entry collapses sibling actions that share Action.Group into a single
// segment with their combined primary keys; an ungrouped entry wraps exactly
// one action. It is computed at render time (never stored) so an override is
// always reflected.
type DisplayGroup struct {
	Grouped bool
	Label   string   // group label when Grouped, action description otherwise
	Keys    []string // primary keys of the members (one per member)
	KeyText string   // Keys joined for display
	Members []Action
}

// GroupActions aggregates the actions active in a context into display groups.
// Actions that declare the same Group collapse into one DisplayGroup when at
// least two of them are present; a lone member degrades to a plain action
// segment. Members are assumed to be adjacent in the registry, which keeps the
// output in registry order. primary resolves an action's primary key at render
// time, so overrides are always reflected.
func GroupActions(actions []Action, primary func(ActionID) string) []DisplayGroup {
	var out []DisplayGroup
	for i := 0; i < len(actions); {
		a := actions[i]
		if a.Group == "" {
			out = append(out, plainGroup(a, primary))
			i++
			continue
		}

		j := i
		for j < len(actions) && actions[j].Group == a.Group {
			j++
		}
		members := actions[i:j]
		if len(members) >= 2 {
			keys := make([]string, 0, len(members))
			for _, m := range members {
				if k := primary(m.ID); k != "" {
					keys = append(keys, k)
				}
			}
			out = append(out, DisplayGroup{
				Grouped: true,
				Label:   a.GroupLabel,
				Keys:    keys,
				KeyText: joinPrimaryKeys(keys),
				Members: members,
			})
		} else {
			out = append(out, plainGroup(a, primary))
		}
		i = j
	}
	return out
}

func plainGroup(a Action, primary func(ActionID) string) DisplayGroup {
	key := primary(a.ID)
	return DisplayGroup{
		Label:   a.Description,
		Keys:    []string{key},
		KeyText: key,
		Members: []Action{a},
	}
}

// joinPrimaryKeys renders the combined primary keys of a group. A run of three
// or more single lowercase letters concatenates ("hjkl"), a contiguous numeric
// range with a shared prefix compresses ("f1-f9"), and anything else joins with
// "/" ("g/G", "ctrl+u/ctrl+d"). Two single letters keep the slash so a pair
// stays readable ("j/k").
func joinPrimaryKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0]
	}
	if prefix, lo, hi, ok := digitRange(keys); ok {
		return fmt.Sprintf("%s%d-%s%d", prefix, lo, prefix, hi)
	}
	if len(keys) >= 3 && allSingleLowerLetters(keys) {
		sorted := append([]string(nil), keys...)
		sort.Strings(sorted)
		return strings.Join(sorted, "")
	}
	return strings.Join(keys, "/")
}

func allSingleLowerLetters(keys []string) bool {
	for _, k := range keys {
		if len(k) != 1 || k[0] < 'a' || k[0] > 'z' {
			return false
		}
	}
	return true
}

// digitRange reports whether every key is a shared prefix followed by digits
// and those digits form a contiguous ascending range. It returns the prefix and
// the low/high digit so the caller can compress the run ("f1-f9").
func digitRange(keys []string) (prefix string, lo, hi int, ok bool) {
	for i, k := range keys {
		p, n, valid := splitTrailingDigits(k)
		if !valid {
			return "", 0, 0, false
		}
		if i == 0 {
			prefix, lo, hi = p, n, n
			continue
		}
		if p != prefix {
			return "", 0, 0, false
		}
		if n < lo {
			lo = n
		}
		if n > hi {
			hi = n
		}
	}
	if len(keys) != hi-lo+1 {
		return "", 0, 0, false
	}
	return prefix, lo, hi, true
}

func splitTrailingDigits(k string) (prefix string, n int, ok bool) {
	i := len(k)
	for i > 0 && k[i-1] >= '0' && k[i-1] <= '9' {
		i--
	}
	if i == len(k) {
		return "", 0, false
	}
	num := 0
	for _, c := range k[i:] {
		num = num*10 + int(c-'0')
	}
	return k[:i], num, true
}
