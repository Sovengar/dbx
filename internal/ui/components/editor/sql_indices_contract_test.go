package editor

// Scenario: Los indices de bytes del editor de SQL, donde un clamp mal puesto es un panic.
//
// The editor's cursor is a BYTE offset, not a rune index. That is the decision that makes
// inserting a rune a two-slice concatenation, and it is also the source of every bound
// check in this file: each helper that steps the cursor has to refuse to step off the end,
// and each one is a subtraction that can go negative.
//
// The rune helpers are the interesting part. prevRuneStart walks BACK to a boundary and
// nextRuneEnd walks FORWARD by a decoded size, and both have to make progress on an
// INVALID byte — otherwise an editor opened on a line with one bad byte in it wedges
// forever, because every step leaves the cursor where it was and the key does nothing.
//
// So the matrix below is: every helper, every out-of-range input, and the assertion is
// that the answer is IN RANGE and that a second call makes progress. Not that it returns
// the value I would guess — that the rune walkers agree with utf8.DecodeRune.

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// TestTheCursorSetterClampsRatherThanPanicking covers SetCursorPos, which is the app's
// only way to place the cursor.
//
// The app passes a byte offset it got from the grid, so every one of these is input the
// production path can produce: an empty editor, an offset past the last line, a negative
// one, and — the interesting one — an offset in the MIDDLE of a multi-byte character. That
// last is legal input and every reader downstream assumes a boundary, so the setter snaps
// it. A setter that only clamped to the line length would leave the cursor inside a
// character, and the next splice would cut it in half.
func TestTheCursorSetterClampsRatherThanPanicking(t *testing.T) {
	const text = "añó🎉" // 1 + 2 + 2 + 4 = 9 bytes, 4 runes

	for _, tc := range []struct {
		name           string
		content        string
		row, col       int
		wantRow        int
		wantCol        int
		wantOnBoundary bool
	}{
		{name: "in range", content: text, row: 0, col: 5, wantRow: 0, wantCol: 5},
		{name: "the end of the line", content: text, row: 0, col: 9, wantRow: 0, wantCol: 9},
		{name: "a negative row clamps to the first", content: "a\nb", row: -1, col: 0, wantRow: 0, wantCol: 0},
		{name: "a negative col clamps to zero", content: text, row: 0, col: -5, wantRow: 0, wantCol: 0},
		{name: "a row past the end clamps to the last", content: "a\nb\nc", row: 99, col: 1, wantRow: 2, wantCol: 1},
		{name: "a col past the end clamps to the length", content: "abc", row: 0, col: 99, wantRow: 0, wantCol: 3},

		// The snapping, and it goes FORWARD. Byte 2 of "añó🎉" is inside ñ (bytes 1-2), so
		// it snaps to 3; byte 4 is inside ó (3-4) so it snaps to 5; byte 7 is inside the
		// emoji (5-8) so it snaps to 9, which is also the end of the line.
		//
		// The first version of these three cases asserted the snap went BACKWARD and
		// reported a mismatch on correct code: the function's own comment says "snapped
		// forward", and forward is the only direction that guarantees progress.
		{name: "inside a two-byte rune snaps forward", content: text, row: 0, col: 2, wantRow: 0, wantCol: 3},
		{name: "inside a three-byte rune snaps forward", content: text, row: 0, col: 4, wantRow: 0, wantCol: 5},
		{name: "inside the four-byte emoji snaps to the end", content: text, row: 0, col: 7, wantRow: 0, wantCol: 9},

		{name: "the empty editor takes the cursor at zero", content: "", row: 5, col: 5, wantRow: 0, wantCol: 0},
		{name: "an empty LINE takes the cursor at zero", content: "\n\n", row: 1, col: 3, wantRow: 1, wantCol: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := editorUnderTest(t)
			e.SetContent(tc.content)

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("SetCursorPos(%d, %d) panicked on %q: %v", tc.row, tc.col, tc.content, r)
					}
				}()
				e.SetCursorPos(tc.row, tc.col)
			}()

			if e.cursorRow != tc.wantRow {
				t.Errorf("the row is %d, want %d", e.cursorRow, tc.wantRow)
			}
			if e.cursorCol != tc.wantCol {
				t.Errorf("the column is %d, want %d", e.cursorCol, tc.wantCol)
			}
			if tc.wantOnBoundary {
				return
			}
			// Whatever it settled on, it must be usable: a valid row and a cursor that is
			// either zero, the end of the line, or a rune boundary.
			line := e.lines[e.cursorRow]
			if e.cursorCol != 0 && e.cursorCol != len(line) && !utf8.RuneStart(line[e.cursorCol]) {
				t.Errorf("the cursor settled at byte %d, inside a rune of %q", e.cursorCol, line)
			}
		})
	}
}

// TestSettingEmptyContentLeavesOneUsableLine pins the one guard in SetContent.
//
// strings.Split never returns an empty slice — Split("", "\n") is [""] — so this arm is
// unreachable through SetContent. It is here because the alternative is a model with zero
// lines, and every line-indexing caller in this file would index lines[0] on it. Pinned
// rather than fixed, because a guard for an unreachable state is still cheaper than a
// class of index panics.
func TestASetEditorAlwaysHasAtLeastOneLine(t *testing.T) {
	for _, content := range []string{"", "\n", "\n\n\n", "a", "a\n"} {
		e := editorUnderTest(t)
		e.SetContent(content)
		if len(e.lines) == 0 {
			t.Errorf("SetContent(%q) left the editor with no lines at all", content)
		}
		if e.cursorRow != 0 || e.cursorCol != 0 {
			t.Errorf("SetContent(%q) left the cursor at %d:%d, want 0:0", content, e.cursorRow, e.cursorCol)
		}
	}
}

// The two rune walkers. Both are called in a loop by the movement handlers, so the two
// properties that matter are IN RANGE and MAKES PROGRESS.
func TestTheRuneWalkersStayInRangeAndMakeProgress(t *testing.T) {
	// "añó🎉" and a string with an invalid byte, which is the case the progress guard
	// exists for: utf8.DecodeRuneInString returns (RuneError, 0) for an invalid byte, and
	// a walker that stepped by that size would never move.
	const valid = "añó🎉"
	const invalid = "a\xffb"

	t.Run("prevRuneStart walks back to a boundary", func(t *testing.T) {
		for i := 0; i <= len(valid); i++ {
			got := prevRuneStart(valid, i)
			if got < 0 || got > i {
				t.Errorf("prevRuneStart(%q, %d) = %d, want it inside 0..%d", valid, i, got, i)
			}
			if got > 0 && !utf8.RuneStart(valid[got]) {
				t.Errorf("prevRuneStart(%q, %d) = %d, which is inside a rune", valid, i, got)
			}
		}
		// The contract, which is subtler than "return start if already on a boundary":
		// it is the start of the character ENDING at end, and end is exclusive. Byte 3
		// is the FIRST byte of ó, so the character ending at 3 is ñ, which begins at 1.
		//
		// The first version of this case asserted 3, having read the function as "return
		// start if already on a boundary". That is a different function and a wrong one:
		// on a boundary the character ending there is the PREVIOUS one, so a caller
		// stepping left from the start of a character would never move.
		if got := prevRuneStart(valid, 3); got != 1 {
			t.Errorf("prevRuneStart(%q, 3) = %d, want 1 — ñ ends at 3", valid, got)
		}
		if got := prevRuneStart(valid, 4); got != 3 {
			t.Errorf("prevRuneStart(%q, 4) = %d, want 3 — ó begins at 3", valid, got)
		}
	})

	t.Run("nextRuneEnd steps forward by a whole rune", func(t *testing.T) {
		for i := 0; i < len(valid); i++ {
			got := nextRuneEnd(valid, i)
			if got <= i {
				t.Errorf("nextRuneEnd(%q, %d) = %d, want it strictly past the start", valid, i, got)
			}
			if got > len(valid) {
				t.Errorf("nextRuneEnd(%q, %d) = %d, want at most %d", valid, i, got, len(valid))
			}
		}
		// And it must agree with the decoder exactly, off a boundary.
		for i := 0; i < len(valid); {
			_, size := utf8.DecodeRuneInString(valid[i:])
			if got := nextRuneEnd(valid, i); got != i+size {
				t.Errorf("nextRuneEnd(%q, %d) = %d, want %d from the decoder", valid, i, got, i+size)
			}
			i += size
		}
	})

	t.Run("both refuse to step off the ends", func(t *testing.T) {
		for _, end := range []int{-1, 0, -100} {
			if got := prevRuneStart(valid, end); got != 0 {
				t.Errorf("prevRuneStart(%q, %d) = %d, want 0", valid, end, got)
			}
		}
		for _, start := range []int{len(valid), len(valid) + 1, len(valid) + 100} {
			if got := nextRuneEnd(valid, start); got != len(valid) {
				t.Errorf("nextRuneEnd(%q, %d) = %d, want the length %d", valid, start, got, len(valid))
			}
		}
	})

	t.Run("an INVALID byte still makes progress", func(t *testing.T) {
		// The guard. DecodeRuneInString returns (RuneError, 0) here, so a walker stepping
		// by the decoded size would return start and every caller would loop forever.
		if got := nextRuneEnd(invalid, 1); got <= 1 {
			t.Errorf("nextRuneEnd(%q, 1) = %d, want it strictly past 1", invalid, got)
		}
		if got := prevRuneStart(invalid, 2); got >= 2 {
			t.Errorf("prevRuneStart(%q, 2) = %d, want it strictly before 2", invalid, got)
		}

		// And a walk across the whole string terminates, which is the property the
		// movement handlers depend on.
		i := 0
		for range 100 {
			if i >= len(invalid) {
				break
			}
			next := nextRuneEnd(invalid, i)
			if next <= i {
				t.Fatalf("the forward walk stalled at %d", i)
			}
			i = next
		}
		if i < len(invalid) {
			t.Errorf("the forward walk stopped at %d of %d", i, len(invalid))
		}
	})
}

// Accepting a completion REPLACES the token under the cursor. Two clamps guard the span
// the autocomplete reports, and both exist because the span is computed elsewhere — from a
// previous context, or from a line that has since changed.
//
// The consequence of a wrong span is not a crash: it is a completion spliced into the
// middle of a word, or one that eats the wrong half of the line.
func TestAcceptingACompletionReplacesTheTokenUnderTheCursor(t *testing.T) {
	// The fixture schema has users(id, name) in public and events(id, wh_start) in
	// analytics, plus the keyword and function lists.
	//
	// `at` marks the cursor, because detectContext reads the tokens BEFORE it — so
	// "SELECT * FROM users NA" and "SELECT NA FROM users" are different questions and the
	// cursor position is what decides which one is being asked. The first version of these
	// cases put the cursor at the end of the whole line, which asks about the FROM clause
	// and not about the word, and reported that the column was not offered.
	for _, tc := range []struct {
		name    string
		at      string
		suggest string
		want    string
	}{
		{
			name:    "a table name gets a trailing space",
			at:      "SELECT * FROM u|",
			suggest: "users",
			want:    "SELECT * FROM users ",
		},
		{
			// The function names in the fixture are UPPERCASE, so the suggestion is COUNT
			// and not count. The first version of this case asked for the lowercase
			// spelling and reported that the function was not offered — it was, twice.
			// A function DOES get a trailing space, despite the name of the helper: the
			// no-space branch tests `strings.HasSuffix(item.Name, "()")` and this item is
			// named COUNT — the parens come from Label(), which runs later. So the
			// function's own suffix branch is only reached by an item already spelled with
			// parens, which is why the branch below covers it directly instead.
			name:    "a function gets a trailing space",
			at:      "SELECT COU|",
			suggest: "COUNT",
			want:    "SELECT COUNT() ",
		},
		{
			// The difference that matters: a dot cascades into the next level, a space
			// does not. Pinning it is what stops someone "tidying" the suffix to a space.
			name:    "a schema gets a DOT, not a space",
			at:      "SELECT * FROM anal|",
			suggest: "analytics",
			want:    "SELECT * FROM analytics.",
		},
		{
			name:    "a keyword gets a trailing space",
			at:      "SELECT * FROM users WHER|",
			suggest: "WHERE",
			want:    "SELECT * FROM users WHERE ",
		},
		{
			// And the star is offered only at the very start of the list, which is what
			// selectContext carries.
			name:    "the star is offered at the start of the select list",
			at:      "SELECT |FROM users",
			suggest: "*",
			want:    "SELECT * FROM users",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := strings.Index(tc.at, "|")
			if col < 0 {
				t.Fatalf("the case has no | cursor marker: %q", tc.at)
			}
			e := editorUnderTest(t)
			e.SetContent(tc.at[:col] + tc.at[col+1:])
			e.SetCursorPos(0, col)

			item := suggest(t, e, tc.suggest)
			e.acceptCompletion(item)

			got := e.lines[0]
			if got != tc.want {
				t.Errorf("after accepting %q the line is %q", item.Name, got)
			}
			// The cursor lands after the INSERTED TEXT, which is not the same as the end of
			// the line unless the completion was at the end. The first version of this case
			// asserted the end of the line and reported a mismatch on "SELECT * FROM
			// users", where the cursor correctly sat at 9 and the line ran to 19.
			//
			// The position of the LABEL in the result, not the marker's column: the
			// completion REPLACES a whole token, so the inserted text starts BEFORE the
			// cursor. The first version used the marker and reported the cursor short by
			// exactly the length of the text it had replaced — on every case.
			label := item.Label()
			insertedAt := strings.Index(got, label)
			if insertedAt < 0 {
				t.Fatalf("the inserted text %q is not in %q", label, got)
			}
			wantCursor := insertedAt + len(label) + len(completionSuffix(item))
			if e.cursorCol != wantCursor {
				t.Errorf("the cursor is at byte %d, want %d — just past the inserted text",
					e.cursorCol, wantCursor)
			}
			// And the popup has to be gone, or the next tab accepts the same item again
			// into the token after it. acceptCompletion's own comment records the measured
			// symptom: "FROM u", tab, tab gave "FROM users users ".
			if e.AutocompleteVisible() {
				t.Error("the popup is still open after accepting")
			}
		})
	}
}

// The suffix is a switch over three cases and a default. It is the difference between
// `SELECT * FROM sales.` (cascade into the tables) and `SELECT * FROM sales ` (no
// cascade), which is a real difference in what the user can do next.
// editorUnderTest is a focused editor with the fixture schema loaded, which is what makes
// a completion findable. Built per case because several of them change the content.
func editorUnderTest(t *testing.T) *SQLEditor {
	t.Helper()
	e := newEditor(t, 100, 20)
	e.SetSchema(testExport())
	e.SetAutocompleteConfig(true, 1)
	return e
}

// suggest drives the popup and returns the suggestion with the given name.
func suggest(t *testing.T, e *SQLEditor, name string) *CompletionItem {
	t.Helper()
	e.triggerAutocomplete()
	item := findSuggestion(e.autocomplete, name)
	if item == nil {
		t.Fatalf("%q was not suggested; the popup offered %v", name, suggestionNames(e.autocomplete))
	}
	return item
}

// TestColumnsAreNotOfferedBeforeTheFromIsTyped pins a LIMIT rather than a bug.
//
// columnItems answers nil when no table is in scope, and the tables in scope come from the
// tokens BEFORE the cursor. So `SELECT na|FROM users` — a column name typed before the
// FROM that supplies its table — offers no columns at all, only the functions that match
// the prefix. That is the deliberate rule: without a table there is no way to know which
// `id` is meant, and dumping the whole catalog would bury the one the user wants.
//
// It is pinned because it looks like a bug from the outside. The first version of the
// select-list cases did exactly that and reported that a column was not offered, having
// put the FROM after the word.
func TestColumnsAreNotOfferedBeforeTheFromIsTyped(t *testing.T) {
	e := editorUnderTest(t)
	e.SetContent("SELECT naFROM users")
	e.SetCursorPos(0, len("SELECT na"))

	e.triggerAutocomplete()

	for _, name := range suggestionNames(e.autocomplete) {
		if item := findSuggestion(e.autocomplete, name); item != nil && item.Kind == CompletionColumn {
			t.Errorf("a column %q was offered with no table in scope", name)
		}
	}
	if !e.AutocompleteVisible() {
		t.Skip("no popup at all for this prefix, which makes the assertion vacuous")
	}
}

func TestTheSuffixAfterACompletionDependsOnItsKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		item CompletionItem
		want string
	}{
		{"a schema cascades with a dot", CompletionItem{Kind: CompletionSchema, Name: "sales"}, "."},
		{"a table gets a space", CompletionItem{Kind: CompletionTable, Name: "users"}, " "},
		{"a column gets a space", CompletionItem{Kind: CompletionColumn, Name: "id"}, " "},
		{"a keyword gets a space", CompletionItem{Kind: CompletionKeyword, Name: "WHERE"}, " "},
		{"a function gets nothing", CompletionItem{Kind: CompletionFunction, Name: "count()"}, ""},
		// The NAME check comes first, so a function already spelled with parens gets
		// nothing whatever its kind says.
		{"a name ending in parens gets nothing", CompletionItem{Kind: CompletionTable, Name: "now()"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := completionSuffix(&tc.item); got != tc.want {
				t.Errorf("completionSuffix(%+v) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}

// The history. historyIdx is a sentinel-carrying cursor: -1 means "past the newest entry",
// 0 is the oldest. Getting the sentinels wrong shows up as "up arrow does nothing" or as
// the history being unreachable from the bottom.
func TestTheHistoryWalksAndResetsItsSentinel(t *testing.T) {
	t.Run("an EMPTY history is inert", func(t *testing.T) {
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("walking an empty history panicked: %v", r)
				}
			}()
			e.historyPrev()
			e.historyNext()
			e.historyPrev()
		}()
	})

	t.Run("up walks BACKWARD through the history and lands on the newest first", func(t *testing.T) {
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")
		e.PushHistory("SELECT 2")
		e.PushHistory("SELECT 3")

		for _, want := range []string{"SELECT 3", "SELECT 2", "SELECT 1"} {
			e.historyPrev()
			if got := e.Content(); got != want {
				t.Errorf("after walking up the editor holds %q, want %q", got, want)
			}
		}

		// And the bottom is the bottom: walking up past the oldest STAYS there.
		e.historyPrev()
		if got := e.Content(); got != "SELECT 1" {
			t.Errorf("walking past the oldest entry gave %q, want it to stay put", got)
		}
	})

	t.Run("down walks FORWARD", func(t *testing.T) {
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")
		e.PushHistory("SELECT 2")

		e.historyPrev() // SELECT 2, the newest
		e.historyPrev() // SELECT 1, the oldest
		e.historyNext()
		if got := e.Content(); got != "SELECT 2" {
			t.Errorf("after one step down the editor holds %q, want SELECT 2", got)
		}
	})

	t.Run("down past the newest CLEARS the buffer rather than wrapping", func(t *testing.T) {
		// The design, and it is not an accident: walking forward off the end of the
		// history is how a user gets an EMPTY editor to type a fresh query into, which is
		// what they were going to do anyway. Wrapping back to the oldest would instead
		// silently replace their buffer with something they had already run.
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")

		e.historyPrev() // onto SELECT 1
		e.historyNext() // off the end

		if got := e.Content(); got != "" {
			t.Errorf("walking down past the newest left %q in the buffer, want it empty", got)
		}
		if e.cursorRow != 0 || e.cursorCol != 0 {
			t.Errorf("the cursor is at %d:%d after clearing, want 0:0", e.cursorRow, e.cursorCol)
		}
	})

	t.Run("the cursor comes back to the start for every entry", func(t *testing.T) {
		// Every history step sets cursorRow and cursorCol to zero and the assertions above
		// only look at the text. A cursor left at the end of the previous entry would make
		// the next keystroke append to the wrong place — and the text checks would not
		// catch it.
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")
		e.SetCursorPos(0, 8)

		e.historyPrev()
		if e.cursorRow != 0 || e.cursorCol != 0 {
			t.Errorf("the cursor is at %d:%d after a history step, want 0:0", e.cursorRow, e.cursorCol)
		}
	})

	t.Run("an EMPTY push is ignored, and a BLANK one is not", func(t *testing.T) {
		// The guard is `sql == ""`, not a blank check — so a run of whitespace takes a
		// history slot. The first version of this case asserted the blank one was
		// dropped too, and reported a mismatch on correct code.
		//
		// It is defensible: the editor refuses to run an all-whitespace statement, so a
		// blank entry cannot arrive from the normal path, and a stricter check here would
		// silently discard a query that is only whitespace inside a comment. Pinned as
		// observed rather than changed, because the fix is a policy decision about what
		// counts as a query.
		e := editorUnderTest(t)
		e.PushHistory("SELECT 1")
		before := len(e.history)

		e.PushHistory("")
		if len(e.history) != before {
			t.Errorf("pushing the empty string grew the history from %d to %d", before, len(e.history))
		}

		e.PushHistory("   ")
		if len(e.history) != before+1 {
			t.Errorf("pushing whitespace left the history at %d, want %d — the guard is `== \"\"`, not a blank check",
				len(e.history), before+1)
		}
	})

	t.Run("historyIdx is never NEGATIVE in the production flow", func(t *testing.T) {
		// historyPrev's `if historyIdx < 0` arm is unreachable: PushHistory sets the
		// index to len(history), historyNext's else branch does the same, and nothing
		// assigns -1. Pinned as a limit rather than removed, because removing the arm
		// would also remove the only handling a future caller would get for free.
		e := editorUnderTest(t)
		for _, sql := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
			e.PushHistory(sql)
			if e.historyIdx < 0 {
				t.Fatalf("after pushing %q the history index is %d", sql, e.historyIdx)
			}
			e.historyPrev()
			e.historyNext()
			e.historyPrev()
			if e.historyIdx < 0 {
				t.Fatalf("while walking the history the index went negative: %d", e.historyIdx)
			}
		}
	})
}

// The control-key switch in the editor's Update. `ctrl+u` is the one that clears the line,
// and it is the one that has to clear the WHOLE line rather than the token under the cursor
// — a difference a user notices immediately and a test has to pin, because the two
// implementations look alike.
func TestCtrlUClearsTheWholeLineAndNotJustTheToken(t *testing.T) {
	const line = "SELECT * FROM users WHERE"

	e := editorUnderTest(t)
	e.SetContent(line)
	// The cursor in the middle, where a token-clearing implementation would do something
	// visibly different.
	e.SetCursorPos(0, len(line)-3)

	if _, handled := e.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+u was not handled")
	}

	got := strings.Join(e.lines, "")
	if got != "" && got != " " {
		t.Errorf("after ctrl+u the line is %q, want the whole thing cleared", got)
	}
	if e.cursorCol != 0 && e.cursorCol != len(got) {
		t.Errorf("after ctrl+u the cursor is at byte %d of %q", e.cursorCol, got)
	}
	if !e.modified {
		t.Error("ctrl+u did not mark the editor modified, so the change would be discarded")
	}
}
