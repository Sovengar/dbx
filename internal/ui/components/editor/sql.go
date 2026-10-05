package editor

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

// CopySQLMsg is emitted when the user presses Ctrl+Y in the editor.
type CopySQLMsg struct{}

type SQLEditor struct {
	styles       *theme.Styles
	lines        []string
	cursorRow    int
	cursorCol    int
	width        int
	height       int
	focused      bool
	history      []string
	historyIdx   int
	modified     bool
	autocomplete *AutocompleteState
	schema       *context.SchemaExport
	schemaLoaded bool
	commitOnRun  bool

	autocompleteEnabled   bool
	autocompleteMinPrefix int

	keybinds config.Resolver
}

func NewSQLEditor(styles *theme.Styles) *SQLEditor {
	return &SQLEditor{
		styles:                styles,
		lines:                 []string{""},
		autocomplete:          NewAutocompleteState(),
		autocompleteEnabled:   true,
		autocompleteMinPrefix: 1,
	}
}

// SetKeybinds injects the registry resolver the editor uses to dispatch its own
// actions (autocomplete, history_prev, history_next). It is a setter rather than
// a constructor parameter to keep NewSQLEditor's contract stable. A nil resolver
// leaves those shortcuts unhandled by design: the raw key cases were removed so
// a rebind cannot be bypassed, and production always wires a resolver.
func (e *SQLEditor) SetKeybinds(k config.Resolver) { e.keybinds = k }

// SetAutocompleteConfig enables or disables real-time suggestions and sets the
// minimum token length before the popup opens on its own. Manual triggering is
// unaffected by the minimum.
func (e *SQLEditor) SetAutocompleteConfig(enabled bool, minPrefix int) {
	e.autocompleteEnabled = enabled
	if minPrefix < 0 {
		minPrefix = 0
	}
	e.autocompleteMinPrefix = minPrefix
	if !enabled {
		e.autocomplete.Cancel()
	}
}

func (e *SQLEditor) SetSchema(export *context.SchemaExport) {
	schemas := 0
	if export != nil {
		schemas = len(export.Schemas)
	}
	autocompleteDebugLog("SetSchema: export=%v schemas=%d", export != nil, schemas)
	e.schema = export
	e.schemaLoaded = true
	e.autocomplete = NewAutocompleteState()
	e.autocomplete.LoadSchema(export)
	e.autocomplete.SetKeywords(sqlKeywords, sqlFunctions)
	autocompleteDebugLog("SetSchema: done, allItems=%d", len(e.autocomplete.all))
}

func (e *SQLEditor) SetWidth(w int)             { e.width = w }
func (e *SQLEditor) SetHeight(h int)            { e.height = h }
func (e *SQLEditor) Focus()                     { e.focused = true }
func (e *SQLEditor) Blur()                      { e.focused = false }
func (e *SQLEditor) AutocompleteVisible() bool  { return e.autocomplete.Visible() }
func (e *SQLEditor) AutocompleteReady() bool    { return e.schemaLoaded }
func (e *SQLEditor) AutocompleteItemCount() int { return len(e.autocomplete.filtered) }

// CancelAutocomplete dismisses an open autocomplete popup and reports whether
// one was actually open. Callers (e.g. the close_editor action) use the return
// value to fall through to closing the editor only when nothing was cancelled.
func (e *SQLEditor) CancelAutocomplete() bool {
	if e.autocomplete.Visible() {
		e.autocomplete.Cancel()
		return true
	}
	return false
}

func (e *SQLEditor) Content() string {
	return strings.Join(e.lines, "\n")
}

// SetCommitOnRun marks whether running the current content must also commit
// the pending DML transaction. The grid sets it when it dumps draft SQL into
// the editor, so the changes the user reviewed become durable when executed.
func (e *SQLEditor) SetCommitOnRun(v bool) { e.commitOnRun = v }

// CommitOnRun reports whether running the current content must commit the
// pending transaction once it finishes.
func (e *SQLEditor) CommitOnRun() bool { return e.commitOnRun }

func (e *SQLEditor) Clear() {
	e.lines = []string{""}
	e.cursorRow = 0
	e.cursorCol = 0
	e.modified = false
	e.commitOnRun = false
	e.autocomplete.Cancel()
}

func (e *SQLEditor) SetContent(s string) {
	// No empty-slice guard, and there used to be one: strings.Split always returns at least
	// one element, so for "" it returns [""]. The editor's invariant — one line, possibly
	// blank — comes from that, and TestSetContentAlwaysHasALine is what holds it.
	e.lines = strings.Split(s, "\n")
	e.cursorRow = 0
	e.cursorCol = 0
	e.modified = false
	e.autocomplete.Cancel()
	// New content carries no commit intent unless the caller sets it after.
	e.commitOnRun = false
}

func (e *SQLEditor) SetCursorPos(row, col int) {
	if row < 0 {
		row = 0
	}
	if row >= len(e.lines) {
		row = len(e.lines) - 1
	}
	if col < 0 {
		col = 0
	}
	if col > len(e.lines[row]) {
		col = len(e.lines[row])
	}
	// Snapped forward to a rune boundary. This setter takes a byte offset from the
	// caller — the app passes one from the grid — and clamping it to the line length
	// is not enough: an offset in the middle of a multi-byte character is legal input
	// and every reader downstream assumes a boundary.
	if col > 0 && col < len(e.lines[row]) && !utf8.RuneStart(e.lines[row][col]) {
		for col < len(e.lines[row]) && !utf8.RuneStart(e.lines[row][col]) {
			col++
		}
	}
	e.cursorRow = row
	e.cursorCol = col
}

func (e *SQLEditor) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !e.focused {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return e.handleKey(msg)
	}

	return nil, false
}

func (e *SQLEditor) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	// Registry-owned shortcuts first: the editor dispatches only the actions it
	// declares in HandledActions. App-owned editor actions (execute_query,
	// copy_sql, clear_editor, close_editor) are intercepted before Update and
	// fall through here, keeping the widget-local switch below as the default.
	if e.keybinds != nil {
		if action, ok := e.keybinds.Resolve(key, config.ContextEditor); ok {
			if cmd, handled := e.handleAction(action); handled {
				return cmd, handled
			}
		}
	}

	switch key {
	case "ctrl+space":
		e.triggerAutocomplete()
		return nil, true

	case "ctrl+y":
		if e.Content() == "" {
			return nil, false
		}
		return func() tea.Msg { return CopySQLMsg{} }, true

	case "ctrl+u":
		e.Clear()
		// Clear() marks the editor unmodified, which is right for the clear_editor
		// action — that one DISCARDS the buffer rather than editing it. ctrl+u is an
		// edit: the user deleted a line and expects the editor to be dirty afterwards.
		// Leaving it unmodified means an app holding dumped draft SQL treats the
		// cleared buffer as untouched.
		e.modified = true
		e.autocomplete.Cancel()
		return nil, true

	case "up":
		if e.autocomplete.Visible() {
			e.autocomplete.SelectPrev()
			return nil, true
		}
		if e.cursorRow > 0 {
			e.cursorRow--
			e.clampCol()
		}
		return nil, true

	case "down":
		if e.autocomplete.Visible() {
			e.autocomplete.SelectNext()
			return nil, true
		}
		if e.cursorRow < len(e.lines)-1 {
			e.cursorRow++
			e.clampCol()
		}
		return nil, true

	case "left":
		e.autocomplete.Cancel()
		e.moveLeft()
		return nil, true

	case "right":
		e.autocomplete.Cancel()
		e.moveRight()
		return nil, true

	case "home", "0":
		e.autocomplete.Cancel()
		e.cursorCol = 0
		return nil, true

	case "end", "$":
		e.autocomplete.Cancel()
		e.cursorCol = len(e.lines[e.cursorRow])
		return nil, true

	case "enter":
		if e.acceptSelectedCompletion() {
			return nil, true
		}
		e.insertNewline()
		return nil, true

	case "backspace":
		e.deleteBackward()
		e.updateAutocompleteAfterEdit()
		return nil, true

	case "delete":
		e.deleteForward()
		e.updateAutocompleteAfterEdit()
		return nil, true

	case "space":
		e.insertText(" ")
		e.updateAutocompleteAfterEdit()
		return nil, true

	default:
		if len(msg.Text) > 0 {
			e.insertText(msg.Text)
			e.updateAutocompleteAfterEdit()
			return nil, true
		}
	}

	return nil, false
}

// handleAction dispatches an editor-owned action resolved from the registry.
// Returns handled=false for actions the editor does not own, so they fall
// through to the widget-local handling.
func (e *SQLEditor) handleAction(action config.ActionID) (tea.Cmd, bool) {
	switch action {
	case "history_prev":
		if e.autocomplete.Visible() {
			e.autocomplete.Cancel()
		}
		e.historyPrev()
		return nil, true
	case "history_next":
		if e.autocomplete.Visible() {
			e.autocomplete.Cancel()
		}
		e.historyNext()
		return nil, true
	case "autocomplete":
		// One action, dual behavior: accept a visible completion, else indent.
		if e.acceptSelectedCompletion() {
			return nil, true
		}
		e.insertText("    ")
		return nil, true
	}
	return nil, false
}

// acceptSelectedCompletion accepts the highlighted suggestion when the popup is
// open and reports whether it consumed the key.
func (e *SQLEditor) acceptSelectedCompletion() bool {
	if !e.autocomplete.Visible() {
		return false
	}
	if item := e.autocomplete.SelectedItem(); item != nil {
		e.acceptCompletion(item)
	}
	return true
}

// The cursor column is a BYTE index, and it always sits on a RUNE BOUNDARY. Those two
// facts have to hold together, and the helpers below are the only place either is
// allowed to be violated.
//
// This is the sixth time in this repository that a byte length has been applied to a
// rune-indexed string — the grid cell editor, ask.go, the palette, the jq line, the
// explorer's filter, and here — and it is the one that matters most, because unlike the
// filters this is the QUERY TEXT. What it did, all four measured:
//
//	typed "é", pressed backspace   -> the line held "\xc3", which is not valid UTF-8
//	typed "表", pressed backspace   -> "\xe8\xa1", likewise
//	delete in the middle of "aéb"  -> "a\xa9b", likewise
//	typed "é" and RENDERED it      -> the continuation byte read on its own is U+00A9,
//	                                  so the editor showed a COPYRIGHT SIGN
//
// So the fix is not "make the column a rune index": every other line in this file
// slices with it, and changing the representation would touch all of them for no gain.
// It is to keep the byte index and never let it land mid-character, which is what these
// two helpers are for.

// prevRuneStart returns the byte index where the character ending at end begins. For an
// index already on a boundary that is end-1; for one in the middle of a multi-byte
// character it walks back to that character's first byte.
func prevRuneStart(s string, end int) int {
	if end <= 0 {
		return 0
	}
	i := end - 1
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// nextRuneEnd returns the byte index just past the character starting at start.
func nextRuneEnd(s string, start int) int {
	if start >= len(s) {
		return len(s)
	}
	// No special case for an invalid byte, and there used to be one. utf8.DecodeRuneInString
	// returns (RuneError, 1) for EVERY invalid encoding — a lone continuation byte, a
	// truncated multi-byte character, anything at all — so size is never 0 and the loop that
	// adds this answer always advances. The old branch returned start+1 for an invalid byte,
	// which is the same value, and the comment claimed it prevented a wedge that the standard
	// library had already prevented.
	//
	// Measured, not assumed: "", "Ã", "â", "ð" and "ÿþ" all give
	// size 1. TestTheByteHelpersWalkACorruptLine is what holds the property now.
	_, size := utf8.DecodeRuneInString(s[start:])
	return start + size
}

func (e *SQLEditor) moveLeft() {
	line := e.lines[e.cursorRow]
	if e.cursorCol > 0 {
		e.cursorCol = prevRuneStart(line, e.cursorCol)
	} else if e.cursorRow > 0 {
		e.cursorRow--
		e.cursorCol = len(e.lines[e.cursorRow])
	}
}

func (e *SQLEditor) moveRight() {
	line := e.lines[e.cursorRow]
	if e.cursorCol < len(line) {
		e.cursorCol = nextRuneEnd(line, e.cursorCol)
	} else if e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.cursorCol = 0
	}
}

func (e *SQLEditor) insertText(s string) {
	line := e.lines[e.cursorRow]
	e.lines[e.cursorRow] = line[:e.cursorCol] + s + line[e.cursorCol:]
	e.cursorCol += len(s)
	e.modified = true
}

func (e *SQLEditor) insertNewline() {
	line := e.lines[e.cursorRow]
	rest := line[e.cursorCol:]
	e.lines[e.cursorRow] = line[:e.cursorCol]
	e.cursorRow++
	e.lines = append(e.lines[:e.cursorRow], append([]string{rest}, e.lines[e.cursorRow:]...)...)
	e.cursorCol = 0
	e.modified = true
}

func (e *SQLEditor) deleteBackward() {
	if e.cursorCol > 0 {
		line := e.lines[e.cursorRow]
		cut := prevRuneStart(line, e.cursorCol)
		e.lines[e.cursorRow] = line[:cut] + line[e.cursorCol:]
		e.cursorCol = cut
		e.modified = true
	} else if e.cursorRow > 0 {
		prevLen := len(e.lines[e.cursorRow-1])
		e.lines[e.cursorRow-1] += e.lines[e.cursorRow]
		e.lines = append(e.lines[:e.cursorRow], e.lines[e.cursorRow+1:]...)
		e.cursorRow--
		e.cursorCol = prevLen
		e.modified = true
	}
}

func (e *SQLEditor) deleteForward() {
	line := e.lines[e.cursorRow]
	if e.cursorCol < len(line) {
		end := nextRuneEnd(line, e.cursorCol)
		e.lines[e.cursorRow] = line[:e.cursorCol] + line[end:]
		e.modified = true
	} else if e.cursorRow < len(e.lines)-1 {
		e.lines[e.cursorRow] += e.lines[e.cursorRow+1]
		e.lines = append(e.lines[:e.cursorRow+1], e.lines[e.cursorRow+2:]...)
		e.modified = true
	}
}

func (e *SQLEditor) clampCol() {
	max := len(e.lines[e.cursorRow])
	if e.cursorCol > max {
		e.cursorCol = max
	}
}

// runeLenAt is how many bytes the character starting at i occupies, or 0 at the end of
// the string.
func runeLenAt(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	// No `size <= 0` arm, and there used to be one: DecodeRuneInString returns 1 for an
	// invalid byte, never 0. The only zero is the one above, at the end of the string.
	_, size := utf8.DecodeRuneInString(s[i:])
	return size
}

func (e *SQLEditor) triggerAutocomplete() {
	e.refreshAutocomplete(true)
}

func (e *SQLEditor) updateAutocompleteAfterEdit() {
	e.refreshAutocomplete(false)
}

func (e *SQLEditor) refreshAutocomplete(force bool) {
	if !e.schemaLoaded {
		autocompleteDebugLog("refreshAutocomplete: schema not loaded, skipping")
		return
	}
	if !e.autocompleteEnabled {
		e.autocomplete.Cancel()
		return
	}
	line := e.lines[e.cursorRow]
	ctx := e.autocomplete.detectContext(line, e.cursorCol)
	if !force && e.autocompleteMinPrefix > 0 && ctx.tokenText != "" && len(ctx.tokenText) < e.autocompleteMinPrefix {
		autocompleteDebugLog("refreshAutocomplete: token %q shorter than min=%d, hiding", ctx.tokenText, e.autocompleteMinPrefix)
		e.autocomplete.Cancel()
		return
	}
	e.autocomplete.UpdateContext(ctx)
	autocompleteDebugLog("refreshAutocomplete: force=%v line=%q col=%d visible=%v filtered=%d",
		force, line, e.cursorCol, e.autocomplete.Visible(), len(e.autocomplete.filtered))
}

// acceptCompletion replaces the token under the cursor with the chosen item,
// which keeps acceptance idempotent: accepting the keyword you already typed
// never duplicates it.
func (e *SQLEditor) acceptCompletion(item *CompletionItem) {
	// No span clamps here, and there used to be two. The span cannot be stale by the time
	// this runs: acceptCompletion is reached only through acceptSelectedCompletion, which
	// requires the popup to be VISIBLE, and a visible popup means a context was installed by
	// refreshAutocomplete — which is called after every mutation of the line (newline,
	// backspace, delete, space, a typed character). The one edit that does not refresh is the
	// indent, and it is only reached when the popup is NOT visible, which is the other half
	// of the same condition.
	//
	// So the span always describes the current line. TestTheSpanIsNeverStaleWhenAccepting is
	// what holds that: it drives every key that can mutate a line and asserts, at each
	// accept, that the span is inside the line. A new edit path that skipped the refresh
	// would fail there rather than be caught by a clamp that this file no longer has.
	line := e.lines[e.cursorRow]
	start, end := e.autocomplete.CompletionSpan()

	completion := item.Label()
	suffix := completionSuffix(item)
	e.lines[e.cursorRow] = line[:start] + completion + suffix + line[end:]
	e.cursorCol = start + len(completion) + len(suffix)
	e.modified = true
	// CANCEL, not re-trigger. This function's own doc comment promises that
	// accepting is idempotent — "accepting the keyword you already typed never
	// duplicates it" — and re-triggering broke exactly that. A completion ends
	// with a separator (a space, or a dot after a schema), so recomputing the
	// context puts the cursor past the word, in a context with something else to
	// offer; the popup came straight back up over the NEW context, and a second
	// tab accepted the same item into the empty token AFTER it. Measured:
	//
	//	"FROM u", tab  ->  "FROM users "
	//	          tab  ->  "FROM users users "
	//
	// Cancelling makes the second tab fall through to indenting, which is what
	// pressing tab twice on a finished word should do. Cascading completion —
	// accept a table, then its columns — still works: the next keystroke
	// re-triggers and offers the children.
	e.autocomplete.Cancel()
}

func completionSuffix(item *CompletionItem) string {
	if strings.HasSuffix(item.Name, "()") {
		return ""
	}
	switch item.Kind {
	case CompletionSchema:
		return "."
	default:
		return " "
	}
}

func (e *SQLEditor) historyPrev() {
	if len(e.history) == 0 {
		return
	}
	if e.historyIdx < 0 {
		e.historyIdx = len(e.history) - 1
	} else if e.historyIdx > 0 {
		e.historyIdx--
	}
	if e.historyIdx >= 0 && e.historyIdx < len(e.history) {
		e.lines = strings.Split(e.history[e.historyIdx], "\n")
		e.cursorRow = 0
		e.cursorCol = 0
	}
}

func (e *SQLEditor) historyNext() {
	if len(e.history) == 0 || e.historyIdx < 0 {
		return
	}
	if e.historyIdx < len(e.history)-1 {
		e.historyIdx++
		e.lines = strings.Split(e.history[e.historyIdx], "\n")
		e.cursorRow = 0
		e.cursorCol = 0
	} else {
		e.historyIdx = len(e.history)
		e.lines = []string{""}
		e.cursorRow = 0
		e.cursorCol = 0
	}
}

func (e *SQLEditor) PushHistory(sql string) {
	if sql == "" {
		return
	}
	e.history = append(e.history, sql)
	if len(e.history) > 100 {
		e.history = e.history[len(e.history)-100:]
	}
	e.historyIdx = len(e.history)
}

func (e *SQLEditor) View() string {
	if e.width <= 0 || e.height <= 0 {
		return ""
	}

	var lines []string
	for i, line := range e.lines {
		rendered := HighlightSQL(line, e.styles)
		if i == e.cursorRow && e.focused {
			// Add cursor indicator
			if e.cursorCol <= len(line) {
				before := line[:e.cursorCol]
				cursor := " "
				after := ""
				// The WHOLE character under the cursor, not one byte of it.
				// Reading a single byte gave U+00A9 for the tail of "é", so the
				// editor rendered a COPYRIGHT SIGN where the user had typed an
				// accent, and `after` swallowed the rest of the character.
				if size := runeLenAt(line, e.cursorCol); size > 0 {
					after = line[e.cursorCol+size:]
					cursor = line[e.cursorCol : e.cursorCol+size]
				}
				rendered = HighlightSQL(before, e.styles) +
					e.styles.Selected.Render(cursor) +
					HighlightSQL(after, e.styles)
			}
		}
		lines = append(lines, rendered)
	}

	// Pad to fill height
	for len(lines) < e.height {
		lines = append(lines, "")
	}

	// Truncate to height
	if len(lines) > e.height {
		lines = lines[:e.height]
	}

	editorContent := strings.Join(lines, "\n")

	if e.focused && e.autocomplete.Visible() {
		popup := e.autocomplete.Render(e.styles, e.width)
		if popup != "" {
			popupLines := strings.Split(popup, "\n")
			editorContent = editorContent + "\n" + strings.Join(popupLines, "\n")
		}
	}

	return editorContent
}
