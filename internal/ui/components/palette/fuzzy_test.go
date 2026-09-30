package palette

import "testing"

// Scenario: Una búsqueda vacía coincide con todo y no puntúa.
//
// This is the state the palette opens in: every command is a candidate, and
// none of them is better than any other, so the list must come out in the order
// the registry defined. A negative score or a reordering here would shuffle the
// palette every time it opened.
func TestFuzzyMatch_EmptyQueryMatchesEverythingWithNoScore(t *testing.T) {
	for _, target := range []string{"", "a", "Refresh Schema", "ÉLÈVE", "  "} {
		score, ok := fuzzyMatch("", target)
		if !ok {
			t.Errorf("fuzzyMatch(\"\", %q) reported no match, want a match", target)
		}
		if score != 0 {
			t.Errorf("fuzzyMatch(\"\", %q) scored %d, want 0", target, score)
		}
	}
}

// Scenario: La búsqueda es una subsecuencia, y el orden de la puntuación está
// fijado por contrato.
//
// Every score here is arithmetic, not a ranking hint: the palette shows these
// results in the order the scores produce, so a change in any of them silently
// reorders the menu. The four contributions are
//
//	+10  the character matched at all
//	 +5  it matched the character right before it
//	 +8  it started a word: position 0, or after a space, an underscore, a dash
//	+10  the whole match started at position 0
//
// and the "+3 for an upper-case letter" is discussed further down: it is dead.
func TestFuzzyMatch_ScoresAreFixedByContract(t *testing.T) {
	for _, tc := range []struct {
		query  string
		target string
		want   int
		why    string
	}{
		{"a", "abc", 28, "10 base + 8 word start + 10 whole match at 0"},
		{"a", "a", 28, "same, with nothing after it"},
		{"b", "abc", 10, "10 base only: not a word start, match not at 0"},
		{"b", "a bc", 18, "10 base + 8 for starting a word after a space"},
		{"b", "a_b", 18, "+ 8 after an underscore"},
		{"b", "a-b", 18, "+ 8 after a dash"},
		{"ab", "abc", 43, "10 + 8, then 10 + 5 consecutive, then + 10 at 0"},
		{"ac", "abc", 38, "the gap costs the 5 consecutive bonus"},
		{"abc", "abc", 58, "10+8, then 15, then 15, then +10 at 0"},
		{"A", "abc", 28, "the query is lower-cased, so it scores as 'a'"},
		{"A", "ABC", 28, "the target is lower-cased too, so the case never matters"},
	} {
		t.Run(tc.query+"/"+tc.target, func(t *testing.T) {
			score, ok := fuzzyMatch(tc.query, tc.target)
			if !ok {
				t.Fatalf("fuzzyMatch(%q, %q) reported no match, want a match", tc.query, tc.target)
			}
			if score != tc.want {
				t.Errorf("fuzzyMatch(%q, %q) = %d, want %d (%s)", tc.query, tc.target, score, tc.want, tc.why)
			}
		})
	}
}

// Scenario: Los caracteres tienen que aparecer en orden, y en su totalidad.
//
// A subsequence match, not a substring one: "bd" is not a match for "abcdef",
// and neither is "dc". A prefix of the query that runs out of target is not a
// match either, which is what keeps half-typed queries from matching everything
// as the user types.
func TestFuzzyMatch_RequiresTheQueryInOrder(t *testing.T) {
	for _, tc := range []struct {
		query, target string
		want          bool
	}{
		{"abc", "abc", true},
		{"ac", "abc", true},   // gap allowed
		{"ab", "ba", false},   // out of order
		{"abd", "abc", false}, // runs out of target mid-query
		{"xyz", "abc", false},
		{"a", "", false},
		{"a", "b", false},
		{"b", "abc", true},
		{"c", "abc", true},
		{"d", "abc", false},
	} {
		t.Run(tc.query+"/"+tc.target, func(t *testing.T) {
			score, ok := fuzzyMatch(tc.query, tc.target)
			if ok != tc.want {
				t.Fatalf("fuzzyMatch(%q, %q) matched = %v, want %v", tc.query, tc.target, ok, tc.want)
			}
			if !ok && score != 0 {
				t.Errorf("fuzzyMatch(%q, %q) = %d, want 0 for a miss", tc.query, tc.target, score)
			}
		})
	}
}

// DEFECTO CONOCIDO, no corregido: la bonificación de camelCase nunca se paga.
//
// The intent is clear: a match on a capital letter should score higher, so
// typing "cc" finds "CamelCase" above "concat". The check reads
// `t[ti] >= 'A' && t[ti] <= 'Z'`, but t is the target already lower-cased on
// line 11, so t[ti] is never upper case and the +3 is unreachable.
//
// This is pinned as a defect rather than left implicit, because the number it
// affects is load-bearing for the ranking: "c" against "camelCase" scores 28,
// which is 10 + 8 + 10, and would be 31 with the bonus alive. Any future change
// that makes t case-preserving will change these scores, and this test is what
// will notice.
func TestFuzzyMatch_CamelCaseBonusIsUnreachable(t *testing.T) {
	for _, tc := range []struct {
		query, target string
		want          int
	}{
		// 'c' at position 0 of a camelCase name. With the bonus alive it would
		// be 31.
		{"c", "camelCase", 28},
		// 'c' in the middle of one, with no word start.
		{"c", "PascalCase", 10},
		// The same name in lower case scores identically, which is the proof
		// that the capital letter is invisible to the scorer.
		{"c", "camelcase", 28},
		{"c", "pascalcase", 10},
	} {
		t.Run(tc.query+"/"+tc.target, func(t *testing.T) {
			score, ok := fuzzyMatch(tc.query, tc.target)
			if !ok {
				t.Fatalf("fuzzyMatch(%q, %q) reported no match", tc.query, tc.target)
			}
			if score != tc.want {
				t.Errorf("fuzzyMatch(%q, %q) = %d, want %d", tc.query, tc.target, score, tc.want)
			}
		})
	}

	// The explicit statement of the defect: an all-caps name is no better than a
	// lower-case one, which is the opposite of what the +3 was for.
	upper, okU := fuzzyMatch("x", "XMLHttp")
	lower, okL := fuzzyMatch("x", "xmlhttp")
	if !okU || !okL {
		t.Fatalf("the probe queries did not match: %v %v", okU, okL)
	}
	if upper != lower {
		t.Errorf("an upper-case target scores %d and a lower-case one %d; the camelCase bonus IS live, so the allowlist proof for lines 31-32 is wrong", upper, lower)
	}
}

// Scenario: Un rune multibyte se empareja byte a byte y sin perder nada.
//
// The scan is over bytes, so a multi-byte rune is matched as its own run of
// bytes, in order. The consequence worth pinning is that the consecutive-match
// bonus applies across the whole rune, not just its first byte, so a query
// typed as one accented character scores the same as its ASCII equivalent would
// in the same position.
func TestFuzzyMatch_MultiByteRunesMatchAsThemselves(t *testing.T) {
	for _, tc := range []struct {
		query, target string
		want          int
		why           string
	}{
		{"é", "café", 25, "the two bytes of é match consecutively: 10 + 10 + 5, match not at 0"},
		{"fé", "café", 40, "three consecutive byte matches: 10 + 15 + 15, match not at 0"},
		{"afé", "café", 55, "55 = 10 + 15 + 15 + the 15 for the whole match starting at 0"},
		{"É", "café", 25, "the query is lower-cased, so the accent matches"},
	} {
		t.Run(tc.query+"/"+tc.target, func(t *testing.T) {
			score, ok := fuzzyMatch(tc.query, tc.target)
			if !ok {
				t.Fatalf("fuzzyMatch(%q, %q) reported no match", tc.query, tc.target)
			}
			if score != tc.want {
				t.Errorf("fuzzyMatch(%q, %q) = %d, want %d (%s)", tc.query, tc.target, score, tc.want, tc.why)
			}
		})
	}

	// LIMITACIÓN CONOCIDA, no un defecto: la búsqueda no ignora acentos. There is
	// no Unicode normalisation anywhere in the path, so an unaccented query does
	// not find an accented name, and vice versa. A user searching "cafe" finds
	// nothing in a palette full of "café". Pinned so the behaviour is a decision
	// on record rather than a surprise.
	if _, ok := fuzzyMatch("cafe", "café"); ok {
		t.Error("an unaccented query matched an accented name; the search has become accent-insensitive")
	}
	if _, ok := fuzzyMatch("é", "cafe"); ok {
		t.Error("an accented query matched an unaccented name; the search has become accent-insensitive")
	}

	// A different rune must not match, byte overlap notwithstanding: é is
	// C3 A9 and © is C2 A9, so a byte-wise scan could in principle pair the A9.
	// It does not, because the first bytes already disagree and the scan needs
	// them in order.
	if _, ok := fuzzyMatch("©", "é"); ok {
		t.Error("fuzzyMatch(\"©\", \"é\") matched; the byte scan is not confining itself to rune boundaries")
	}
}

// Scenario: La lista sale ordenada de mayor a menor puntuación.
//
// The palette shows the result in the order fuzzySort returns it, so a sort
// that silently stops working shows the user a plausible-looking but wrong
// order with no error anywhere.
func TestFuzzySort_OrdersByScoreDescending(t *testing.T) {
	// Scored 10, 28 and 18 for the query "b", deliberately out of order.
	items := []command{{Name: "xb"}, {Name: "b c"}, {Name: "a b"}}

	got := fuzzySort("b", items)
	if len(got) != 3 {
		t.Fatalf("got %d results, want all 3: the query matches every one of them", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].score < got[i].score {
			t.Errorf("result %d scores %d, before result %d at %d: not descending",
				i-1, got[i-1].score, i, got[i].score)
		}
	}
	// And the exact order, because "descending" alone does not say WHICH of the
	// two middle items lands first.
	wantOrder := []string{"b c", "a b", "xb"}
	for i, want := range wantOrder {
		if got[i].command.Name != want {
			t.Errorf("result %d is %q, want %q (full order: %v)", i, got[i].command.Name, want, wantOrder)
		}
	}
	// The scores travel with their command, so the renderer cannot mix them up.
	if got[0].score != 28 || got[1].score != 18 || got[2].score != 10 {
		t.Errorf("scores = %d, %d, %d, want 28, 18, 10", got[0].score, got[1].score, got[2].score)
	}
}

// Scenario: Las opciones empatadas conservan el orden en que se definieron.
//
// The sort only swaps on a strictly greater score, so it is stable. That is
// what keeps the palette from reshuffling every time the user retypes the same
// query: two equally good commands must not trade places.
func TestFuzzySort_EqualScoresKeepTheirInputOrder(t *testing.T) {
	// Both score 28 for the query "a": a match at position 0 plus the whole
	// match bonus, with no consecutive run to tell them apart.
	for _, tc := range []struct {
		name  string
		input []string
	}{
		{"aaa then axa", []string{"aaa", "axa"}},
		{"axa then aaa", []string{"axa", "aaa"}},
		{"three equal", []string{"aaa", "axa", "awa"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var items []command
			for _, n := range tc.input {
				items = append(items, command{Name: n})
			}
			for _, s := range fuzzySort("a", items) {
				if s.score != 28 {
					t.Fatalf("fixture is wrong: %q scored %d, want 28 for all of them", s.command.Name, s.score)
				}
			}
			got := fuzzySort("a", items)
			for i, want := range tc.input {
				if got[i].command.Name != want {
					t.Errorf("result %d is %q, want %q: equal scores must not be reordered (%v)", i, got[i].command.Name, want, tc.input)
				}
			}
		})
	}
}

// Scenario: El alias rescues un comando cuyo nombre no coincide.
//
// Plenty of commands are named for the menu ("Refresh Schema") and typed by
// their short form ("rs"). Without the alias those would be unreachable, so
// this fallback is what makes the palette usable at all.
func TestFuzzySort_AliasRescuesACommandWhoseNameDoesNotMatch(t *testing.T) {
	// "rl" is not a subsequence of "refresh schema" but it is of "reload".
	if _, ok := fuzzyMatch("rl", "Refresh Schema"); ok {
		t.Fatal("the fixture is wrong: the name does match, so the alias path is not being exercised")
	}
	items := []command{
		{Name: "Refresh Schema", Alias: "reload"},
		{Name: "Quit", Alias: "q"},
	}

	got := fuzzySort("rl", items)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: only the aliased command matches", len(got))
	}
	if got[0].command.Name != "Refresh Schema" {
		t.Errorf("kept %q, want %q", got[0].command.Name, "Refresh Schema")
	}
	// The score is the alias's, so the ranking reflects how the user typed it.
	if aliasScore, _ := fuzzyMatch("rl", "reload"); got[0].score != aliasScore {
		t.Errorf("score = %d, want the alias score %d", got[0].score, aliasScore)
	}
}

// Scenario: El alias es un suplente, no una máxima.
//
// The alias is consulted only when the NAME misses, so a command whose name
// matches poorly is not rescued by a better-scoring alias. Taking the maximum
// instead would be a different, arguably better behaviour, so the current one
// is pinned rather than assumed.
func TestFuzzySort_AliasOnlyRescuesAFullMiss(t *testing.T) {
	// Name "rlx" scores 43; alias "r l" would score 46, because the space starts
	// a word. The result keeps 43.
	nameScore, _ := fuzzyMatch("rl", "rlx")
	aliasScore, _ := fuzzyMatch("rl", "r l")
	if nameScore != 43 || aliasScore != 46 {
		t.Fatalf("the fixture is wrong: name %d, alias %d, want 43 and 46", nameScore, aliasScore)
	}

	got := fuzzySort("rl", []command{{Name: "rlx", Alias: "r l"}})
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].score != nameScore {
		t.Errorf("score = %d, want the NAME score %d: the alias is a fallback, not a maximum", got[0].score, nameScore)
	}
}

// Scenario: Un comando que no coincide en ninguno de los dos nombres desaparece.
//
// Filtering is what turns a list of every command into a search result, so a
// command that matches nothing must not be shown as a fallback row.
func TestFuzzySort_DropsWhatMatchesNeitherNameNorAlias(t *testing.T) {
	items := []command{
		{Name: "Refresh Schema", Alias: "reload"},
		{Name: "Quit", Alias: "q"},
		{Name: "", Alias: ""},
	}

	if got := fuzzySort("zzz", items); len(got) != 0 {
		t.Errorf("got %d results for a query matching nothing: %v", len(got), got)
	}
	if got := fuzzySort("b", []command{{Name: "", Alias: ""}}); len(got) != 0 {
		t.Errorf("an empty name and alias matched: %v", got)
	}
}

// Scenario: Una búsqueda vacía deja la lista entera en su orden original.
//
// The palette opens this way, and every score is 0, so "descending" is a tie
// across the board and stability is the whole contract.
func TestFuzzySort_EmptyQueryKeepsEverythingInRegistryOrder(t *testing.T) {
	items := []command{
		{Name: "Refresh Schema", Alias: "reload"},
		{Name: "Quit", Alias: "q"},
		{Name: "Toggle Theme", Alias: "tt"},
	}

	got := fuzzySort("", items)
	if len(got) != len(items) {
		t.Fatalf("got %d results, want all %d", len(got), len(items))
	}
	for i, item := range items {
		if got[i].command.Name != item.Name {
			t.Errorf("result %d is %q, want %q", i, got[i].command.Name, item.Name)
		}
		if got[i].score != 0 {
			t.Errorf("result %d (%q) scored %d, want 0 for an empty query", i, got[i].command.Name, got[i].score)
		}
	}
}

// Scenario: El comando viaja entero, con su acción y su sección.
//
// fuzzySort reorders; it must not rebuild the commands. A copy that lost the
// Action would make the palette run the wrong thing, and one that lost the
// Section would file the entry under the wrong heading.
func TestFuzzySort_CarriesTheWholeCommandThrough(t *testing.T) {
	original := command{Name: "Refresh Schema", Alias: "reload", Action: "refresh_schema", Section: SectionDatabase}

	got := fuzzySort("ref", []command{original})
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	c := got[0].command
	if c.Name != original.Name || c.Alias != original.Alias || c.Action != original.Action || c.Section != original.Section {
		t.Errorf("the command came back as %+v, want %+v", c, original)
	}
}
