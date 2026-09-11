package grid

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

const maxPopupHeight = 8

var sqlOperators = []Suggestion{
	{Label: "=  ", InsertText: "= ", Kind: "operator", Detail: "equals"},
	{Label: "!= ", InsertText: "!= ", Kind: "operator", Detail: "not equals"},
	{Label: "<> ", InsertText: "<> ", Kind: "operator", Detail: "not equals"},
	{Label: "<  ", InsertText: "< ", Kind: "operator", Detail: "less than"},
	{Label: "<= ", InsertText: "<= ", Kind: "operator", Detail: "less or equal"},
	{Label: ">  ", InsertText: "> ", Kind: "operator", Detail: "greater than"},
	{Label: ">= ", InsertText: ">= ", Kind: "operator", Detail: "greater or equal"},
	{Label: "LIKE ", InsertText: "LIKE ", Kind: "operator", Detail: "pattern match"},
	{Label: "ILIKE ", InsertText: "ILIKE ", Kind: "operator", Detail: "case-insensitive"},
	{Label: "IN ", InsertText: "IN (", Kind: "operator", Detail: "set membership"},
	{Label: "NOT IN ", InsertText: "NOT IN (", Kind: "operator", Detail: "exclusion"},
	{Label: "IS ", InsertText: "IS ", Kind: "operator", Detail: "null check"},
	{Label: "IS NOT ", InsertText: "IS NOT ", Kind: "operator", Detail: "not null"},
	{Label: "BETWEEN ", InsertText: "BETWEEN ", Kind: "operator", Detail: "range"},
}

var sqlKeywords = []Suggestion{
	{Label: "AND ", InsertText: "AND ", Kind: "keyword", Detail: "logical"},
	{Label: "OR ", InsertText: "OR ", Kind: "keyword", Detail: "logical"},
	{Label: "NOT ", InsertText: "NOT ", Kind: "keyword", Detail: "negation"},
	{Label: "( )", InsertText: "(", Kind: "keyword", Detail: "grouping"},
}

var nullSuggestions = []Suggestion{
	{Label: "NULL", InsertText: "NULL", Kind: "value", Detail: "null"},
	{Label: "TRUE", InsertText: "TRUE", Kind: "value", Detail: "boolean"},
	{Label: "FALSE", InsertText: "FALSE", Kind: "value", Detail: "boolean"},
	{Label: "( )", InsertText: "(", Kind: "keyword", Detail: "grouping"},
}

var isSuggestions = []Suggestion{
	{Label: "NULL", InsertText: "NULL", Kind: "value", Detail: "null"},
	{Label: "NOT ", InsertText: "NOT ", Kind: "keyword", Detail: "negation"},
}

var isNotSuggestions = []Suggestion{
	{Label: "NULL", InsertText: "NULL", Kind: "value", Detail: "null"},
}

type Suggestion struct {
	Label      string
	InsertText string
	Kind       string // "column", "operator", "keyword", "value"
	Detail     string
}

type WhereFilter struct {
	styles         *theme.Styles
	columns        []postgres.ColumnInfo
	input          string
	cursor         int // rune-based position
	suggestions    []Suggestion
	allSuggestions []Suggestion
	selected       int
	showPopup      bool
	visible        bool
	scrollOffset   int
	maxWidth       int
	shouldApply    bool
	applyInput     string
}

func NewWhereFilter(styles *theme.Styles, columns []postgres.ColumnInfo, maxWidth int) *WhereFilter {
	wf := &WhereFilter{
		styles:   styles,
		columns:  columns,
		maxWidth: maxWidth,
	}
	wf.updateSuggestions()
	return wf
}

func (wf *WhereFilter) Show() {
	wf.visible = true
	wf.shouldApply = false
	wf.updateSuggestions()
}

func (wf *WhereFilter) Hide() {
	wf.visible = false
	wf.showPopup = false
	wf.shouldApply = false
}

func (wf *WhereFilter) Visible() bool {
	return wf.visible
}

func (wf *WhereFilter) ShouldApply() bool {
	return wf.shouldApply
}

func (wf *WhereFilter) Input() string {
	return wf.applyInput
}

func (wf *WhereFilter) SetInput(input string) {
	wf.input = input
	wf.cursor = len([]rune(input))
	wf.updateSuggestions()
}

func (wf *WhereFilter) HandleKey(msg interface{ String() string }) (bool, bool) {
	key := msg.String()

	// Normalize space key
	if key == "space" {
		key = " "
	}

	switch key {
	case "esc":
		if wf.showPopup {
			wf.showPopup = false
			return true, true
		}
		// If input is empty, close filter entirely
		if wf.input == "" {
			wf.Hide()
			return true, true
		}
		// Clear input
		wf.input = ""
		wf.cursor = 0
		wf.updateSuggestions()
		return true, true

	case "enter":
		if wf.showPopup && len(wf.suggestions) > 0 {
			wf.acceptSuggestion()
			return true, true
		}
		// Apply filter
		wf.shouldApply = true
		wf.applyInput = wf.input
		wf.visible = false
		return true, true

	case "tab":
		if wf.showPopup && len(wf.suggestions) > 0 {
			wf.acceptSuggestion()
			return true, true
		}
		return false, false

	case "up", "k":
		if wf.showPopup && len(wf.suggestions) > 0 {
			wf.moveSelection(-1)
			return true, true
		}
		return false, false

	case "down", "j":
		if wf.showPopup && len(wf.suggestions) > 0 {
			wf.moveSelection(1)
			return true, true
		}
		return false, false

	case "ctrl+u":
		wf.input = ""
		wf.cursor = 0
		wf.updateSuggestions()
		return true, true

	case "backspace":
		if wf.cursor > 0 {
			runes := []rune(wf.input)
			wf.input = string(runes[:wf.cursor-1]) + string(runes[wf.cursor:])
			wf.cursor--
			wf.updateSuggestions()
		}
		return true, true

	case "delete":
		runes := []rune(wf.input)
		if wf.cursor < len(runes) {
			wf.input = string(runes[:wf.cursor]) + string(runes[wf.cursor+1:])
			wf.updateSuggestions()
		}
		return true, true

	case "left":
		if wf.cursor > 0 {
			wf.cursor--
		}
		return true, true

	case "right":
		runes := []rune(wf.input)
		if wf.cursor < len(runes) {
			wf.cursor++
		}
		return true, true

	case "home", "ctrl+a":
		wf.cursor = 0
		return true, true

	case "end", "ctrl+e":
		wf.cursor = len([]rune(wf.input))
		return true, true

	default:
		// Insert character
		if len(key) == 1 {
			ch := rune(key[0])
			if isPrintable(ch) {
				runes := []rune(wf.input)
				wf.input = string(runes[:wf.cursor]) + key + string(runes[wf.cursor:])
				wf.cursor++
				wf.updateSuggestions()
				return true, true
			}
		}
	}

	return false, false
}

func isPrintable(ch rune) bool {
	return unicode.IsPrint(ch)
}

func (wf *WhereFilter) acceptSuggestion() {
	if wf.selected < 0 || wf.selected >= len(wf.suggestions) {
		return
	}
	sug := wf.suggestions[wf.selected]
	runes := []rune(wf.input)
	wf.input = string(runes[:wf.cursor]) + sug.InsertText + string(runes[wf.cursor:])
	wf.cursor += len([]rune(sug.InsertText))
	wf.updateSuggestions()
}

func (wf *WhereFilter) moveSelection(delta int) {
	if len(wf.suggestions) == 0 {
		return
	}
	wf.selected += delta
	if wf.selected < 0 {
		wf.selected = len(wf.suggestions) - 1
	}
	if wf.selected >= len(wf.suggestions) {
		wf.selected = 0
	}
	// Ensure selected is visible in popup
	wf.ensureVisible()
}

func (wf *WhereFilter) ensureVisible() {
	if wf.selected < wf.scrollOffset {
		wf.scrollOffset = wf.selected
	}
	if wf.selected >= wf.scrollOffset+maxPopupHeight {
		wf.scrollOffset = wf.selected - maxPopupHeight + 1
	}
}

// ── Context Detection ──────────────────────────────────────────

func (wf *WhereFilter) updateSuggestions() {
	wf.allSuggestions = wf.detectContext()
	wf.filterSuggestions()
	wf.selected = 0
	wf.scrollOffset = 0
	wf.showPopup = len(wf.suggestions) > 0
}

func (wf *WhereFilter) detectContext() []Suggestion {
	input := strings.TrimSpace(wf.input)
	if input == "" {
		return wf.allColumnsWithKeywords()
	}

	// Find the last token boundary (after AND/OR/NOT/open paren)
	lastClause := wf.extractLastClause(input)
	lastClauseUpper := strings.ToUpper(strings.TrimSpace(lastClause))

	// After AND/OR/NOT → columns + keywords
	if lastClauseUpper == "AND" || lastClauseUpper == "OR" || lastClauseUpper == "NOT" {
		return wf.allColumnsWithKeywords()
	}

	// After "IS NOT" → NULL
	if strings.HasSuffix(lastClauseUpper, "IS NOT") || lastClauseUpper == "IS NOT" {
		return isNotSuggestions
	}

	// After "IS" → NULL, NOT
	if strings.HasSuffix(lastClauseUpper, "IS") {
		return isSuggestions
	}

	// Check if last token is a column name
	tokens := wf.tokenize(lastClause)
	if len(tokens) == 0 {
		return wf.allColumnsWithKeywords()
	}

	lastToken := tokens[len(tokens)-1]

	// Check if last token is a column name
	colIdx := wf.findColumn(lastToken)
	if colIdx >= 0 {
		// After column → operators
		return sqlOperators
	}

	// Check if we have col + operator pattern
	if len(tokens) >= 2 {
		secondLast := tokens[len(tokens)-2]
		colIdx2 := wf.findColumn(secondLast)
		if colIdx2 >= 0 {
			// After operator → values
			return nullSuggestions
		}
	}

	// Check for IN ( or NOT IN ( → values
	upper := strings.ToUpper(lastClause)
	if strings.HasSuffix(upper, "IN (") || strings.HasSuffix(upper, "IN(") {
		return wf.valueSuggestions()
	}

	// Default: columns + keywords
	return wf.allColumnsWithKeywords()
}

func (wf *WhereFilter) extractLastClause(input string) string {
	upper := strings.ToUpper(input)

	// Find last occurrence of AND, OR, NOT (as standalone words)
	bestPos := -1
	for _, keyword := range []string{" AND ", " OR ", " NOT "} {
		pos := strings.LastIndex(upper, keyword)
		if pos >= 0 {
			end := pos + len(keyword)
			if end > bestPos {
				bestPos = end
			}
		}
	}

	if bestPos >= 0 {
		return input[bestPos:]
	}
	return input
}

func (wf *WhereFilter) tokenize(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var tokens []string
	var current strings.Builder

	for _, ch := range input {
		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func (wf *WhereFilter) findColumn(name string) int {
	upper := strings.ToUpper(name)
	for i, col := range wf.columns {
		if strings.ToUpper(col.Name) == upper {
			return i
		}
	}
	return -1
}

func (wf *WhereFilter) allColumnsWithKeywords() []Suggestion {
	var sugs []Suggestion

	// Add columns
	for _, col := range wf.columns {
		sugs = append(sugs, Suggestion{
			Label:      col.Name,
			InsertText: col.Name + " ",
			Kind:       "column",
			Detail:     col.DataType,
		})
	}

	// Add keywords
	sugs = append(sugs, sqlKeywords...)

	return sugs
}

func (wf *WhereFilter) valueSuggestions() []Suggestion {
	return []Suggestion{
		{Label: "NULL", InsertText: "NULL", Kind: "value", Detail: "null"},
		{Label: "TRUE", InsertText: "TRUE", Kind: "value", Detail: "boolean"},
		{Label: "FALSE", InsertText: "FALSE", Kind: "value", Detail: "boolean"},
	}
}

// ── Fuzzy Matching ──────────────────────────────────────────

func (wf *WhereFilter) filterSuggestions() {
	// Get the current token being typed (last word)
	currentToken := wf.currentInputToken()
	if currentToken == "" {
		wf.suggestions = wf.allSuggestions
		return
	}

	upper := strings.ToUpper(currentToken)
	var filtered []Suggestion
	for _, sug := range wf.allSuggestions {
		if fuzzyMatch(strings.ToUpper(sug.Label), upper) {
			filtered = append(filtered, sug)
		}
	}
	wf.suggestions = filtered
}

func (wf *WhereFilter) currentInputToken() string {
	input := wf.input
	runes := []rune(input)

	// Find the last space before cursor
	lastSpace := -1
	for i := wf.cursor - 1; i >= 0; i-- {
		if runes[i] == ' ' {
			lastSpace = i
			break
		}
	}

	if lastSpace == -1 {
		return string(runes[:wf.cursor])
	}
	return string(runes[lastSpace+1 : wf.cursor])
}

func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}
	textIdx := 0
	for _, qch := range query {
		found := false
		for textIdx < len(text) {
			if rune(text[textIdx]) == qch {
				textIdx++
				found = true
				break
			}
			textIdx++
		}
		if !found {
			return false
		}
	}
	return true
}

// ── Distinct Values ──────────────────────────────────────────

// DistinctValues extracts unique values for a column from loaded rows.
func DistinctValues(colIndex int, rows [][]interface{}, limit int) []string {
	seen := make(map[string]bool)
	var values []string

	for _, row := range rows {
		if colIndex >= len(row) {
			continue
		}
		val := row[colIndex]
		if val == nil {
			continue
		}
		s := fmt.Sprintf("%v", val)
		if !seen[s] {
			seen[s] = true
			values = append(values, s)
			if len(values) >= limit {
				break
			}
		}
	}
	return values
}

// ── Rendering ──────────────────────────────────────────

func (wf *WhereFilter) RenderInput() string {
	prefix := wf.styles.Help.Render(" WHERE │ ")
	runes := []rune(wf.input)

	left := string(runes[:wf.cursor])
	cursorChar := "█"
	right := string(runes[wf.cursor:])

	inputText := wf.styles.Text.Render(left) +
		wf.styles.Primary.Render(cursorChar) +
		wf.styles.Text.Render(right)

	bar := prefix + inputText

	// Pad to maxWidth
	barWidth := lipgloss.Width(bar)
	if barWidth < wf.maxWidth-4 {
		bar += strings.Repeat(" ", wf.maxWidth-4-barWidth)
	}

	return bar
}

func (wf *WhereFilter) RenderPopup() string {
	if !wf.showPopup || len(wf.suggestions) == 0 {
		return ""
	}

	// Calculate popup dimensions
	popupWidth := wf.maxWidth - 6
	if popupWidth < 40 {
		popupWidth = 40
	}

	// Determine visible items
	start := wf.scrollOffset
	end := start + maxPopupHeight
	if end > len(wf.suggestions) {
		end = len(wf.suggestions)
	}

	// Column widths for grid layout
	kindWidth := 10
	detailWidth := 14
	labelWidth := popupWidth - kindWidth - detailWidth - 6
	if labelWidth < 15 {
		labelWidth = 15
	}

	var lines []string
	for i := start; i < end; i++ {
		sug := wf.suggestions[i]
		isSelected := i == wf.selected

		// Render label with fuzzy match highlighting
		label := wf.renderLabel(sug.Label, labelWidth)

		// Render kind with color
		kind := wf.renderKind(sug.Kind, kindWidth)

		// Render detail
		detail := wf.styles.TextMuted.Render(sug.Detail)
		detailWidth := lipgloss.Width(detail)
		if detailWidth > 14 {
			detail = ansi.Truncate(detail, 14, "...")
			detailWidth = 14
		}
		detailPad := 14 - detailWidth
		if detailPad > 0 {
			detail += strings.Repeat(" ", detailPad)
		}

		line := "  " + label + " " + kind + " " + detail

		if isSelected {
			line = wf.styles.Selected.Width(popupWidth).Render(line)
		} else {
			line = wf.styles.Text.Render(line)
		}
		lines = append(lines, line)
	}

	// Scroll indicators
	if wf.scrollOffset > 0 {
		lines = append([]string{wf.styles.TextMuted.Render("  ↑")}, lines...)
	}
	if end < len(wf.suggestions) {
		lines = append(lines, wf.styles.TextMuted.Render("  ↓"))
	}

	// Border
	popup := strings.Join(lines, "\n")
	return wf.styles.BorderActive.
		Width(popupWidth).
		Render(popup)
}

func (wf *WhereFilter) renderLabel(label string, maxWidth int) string {
	rendered := wf.styles.Text.Render(label)
	width := lipgloss.Width(rendered)
	if width > maxWidth {
		rendered = ansi.Truncate(rendered, maxWidth, "...")
		return rendered
	}
	pad := maxWidth - width
	if pad > 0 {
		rendered += strings.Repeat(" ", pad)
	}
	return rendered
}

func (wf *WhereFilter) renderKind(kind string, width int) string {
	var rendered string
	switch kind {
	case "column":
		rendered = wf.styles.Success.Render("column")
	case "operator":
		rendered = wf.styles.Warning.Render("operator")
	case "keyword":
		rendered = wf.styles.Info.Render("keyword")
	case "value":
		rendered = wf.styles.Primary.Render("value")
	default:
		rendered = wf.styles.TextMuted.Render(kind)
	}

	w := lipgloss.Width(rendered)
	if w < width {
		rendered += strings.Repeat(" ", width-w)
	}
	return rendered
}
