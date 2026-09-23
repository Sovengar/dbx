package config

import "testing"

func findGroup(groups []DisplayGroup, label string) *DisplayGroup {
	for i := range groups {
		if groups[i].Grouped && groups[i].Label == label {
			return &groups[i]
		}
	}
	return nil
}

func gridGroups(t *testing.T, r *KeybindRegistry) []DisplayGroup {
	t.Helper()
	return GroupActions(r.ActionsFor(ContextGrid), r.PrimaryKey)
}

// Scenario: Sibling actions of a group render as one segment with combined keys.
func TestGroupActions_GridCollapsesSiblingSets(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	groups := gridGroups(t, r)

	want := map[string]string{
		"Navigate":   "hjkl",
		"First/Last": "g/G",
		"Half Page":  "ctrl+u/ctrl+d",
		"Page":       "n/p/P/N",
		"Go to Page": "f1-f9",
	}
	for label, keyText := range want {
		g := findGroup(groups, label)
		if g == nil {
			t.Fatalf("grid has no group %q; groups=%v", label, groups)
		}
		if g.KeyText != keyText {
			t.Errorf("group %q KeyText = %q, want %q", label, g.KeyText, keyText)
		}
	}

	// The nav group must contain all four members.
	if g := findGroup(groups, "Navigate"); len(g.Members) != 4 {
		t.Errorf("Navigate group has %d members, want 4", len(g.Members))
	}
}

// Scenario: The pane shows only the primary key of an action.
func TestGroupActions_UngroupedUsesPrimaryKey(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	groups := gridGroups(t, r)

	var edit *DisplayGroup
	for i := range groups {
		if groups[i].Members[0].ID == "edit_cell" {
			edit = &groups[i]
		}
	}
	if edit == nil {
		t.Fatal("edit_cell is missing from the grid groups")
	}
	if edit.Grouped {
		t.Error("edit_cell must not be grouped")
	}
	if edit.KeyText != "enter" {
		t.Errorf("edit_cell KeyText = %q, want %q (primary only)", edit.KeyText, "enter")
	}
}

// Scenario: A partially active group shows only the members of the current context.
func TestGroupActions_ExplorerPartialGroup(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})
	groups := GroupActions(r.ActionsFor(ContextExplorer), r.PrimaryKey)

	nav := findGroup(groups, "Navigate")
	if nav == nil {
		t.Fatal("explorer has no Navigate group")
	}
	if nav.KeyText != "j/k" {
		t.Errorf("explorer Navigate KeyText = %q, want %q", nav.KeyText, "j/k")
	}
	if findGroup(groups, "Half Page") != nil {
		t.Error("explorer rendered the half-page group, but none of its members apply")
	}
	if findGroup(groups, "Go to Page") != nil {
		t.Error("explorer rendered the goto-page group, but none of its members apply")
	}
}

// Scenario: A group with fewer than two active members degrades to a plain action.
func TestGroupActions_SingleMemberDegrades(t *testing.T) {
	actions := []Action{
		{ID: "solo", Description: "Solo", Group: "g", GroupLabel: "The Group"},
	}
	groups := GroupActions(actions, func(id ActionID) string { return "s" })
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].Grouped {
		t.Error("a lone member must not render as a group")
	}
	if groups[0].Label != "Solo" || groups[0].KeyText != "s" {
		t.Errorf("degraded segment = %q/%q, want Solo/s", groups[0].Label, groups[0].KeyText)
	}
}

// Scenario: Actions are not grouped by Section.
func TestGroupActions_DoesNotGroupBySection(t *testing.T) {
	actions := []Action{
		{ID: "one", Description: "One", Section: SectionData},
		{ID: "two", Description: "Two", Section: SectionData},
	}
	groups := GroupActions(actions, func(id ActionID) string { return string(id)[:1] })
	if len(groups) != 2 {
		t.Fatalf("got %d segments, want 2 (Section must not group)", len(groups))
	}
	for _, g := range groups {
		if g.Grouped {
			t.Errorf("action %q was grouped by Section", g.Members[0].ID)
		}
	}
}

// Scenario: Rebinding a member keeps the group collapsed.
func TestGroupActions_RebindKeepsGroupCollapsed(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{Custom: map[string]string{"navigate_down": "x"}})
	nav := findGroup(gridGroups(t, r), "Navigate")
	if nav == nil {
		t.Fatal("rebound navigate_down broke the Navigate group")
	}
	if !nav.Grouped {
		t.Error("group no longer collapsed after a rebind")
	}
	if nav.KeyText != "hklx" {
		t.Errorf("KeyText = %q, want %q (overridden primary reflected)", nav.KeyText, "hklx")
	}
}

func TestJoinPrimaryKeys(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"single", []string{"n"}, "n"},
		{"four lowercase letters", []string{"h", "j", "k", "l"}, "hjkl"},
		{"pair keeps slash", []string{"j", "k"}, "j/k"},
		{"case pair keeps slash", []string{"g", "G"}, "g/G"},
		{"modifier pair keeps slash", []string{"ctrl+u", "ctrl+d"}, "ctrl+u/ctrl+d"},
		{"digit range compresses", []string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9"}, "f1-f9"},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinPrimaryKeys(tt.keys); got != tt.want {
				t.Errorf("joinPrimaryKeys(%v) = %q, want %q", tt.keys, got, tt.want)
			}
		})
	}
}
