package editor

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

type CompletionKind int

const (
	CompletionKeyword CompletionKind = iota
	CompletionFunction
	CompletionSchema
	CompletionTable
	CompletionColumn
	CompletionOperator
	CompletionValue
	CompletionEmpty
	CompletionSelectList
)

type CompletionItem struct {
	Name   string
	Kind   CompletionKind
	Detail string
	Schema string
	Table  string
}

func (c CompletionItem) Label() string {
	if c.Kind == CompletionFunction {
		return c.Name + "()"
	}
	return c.Name
}

func (c CompletionItem) KindLabel() string {
	switch c.Kind {
	case CompletionSchema:
		return "schema"
	case CompletionTable:
		return "table"
	case CompletionColumn:
		return c.Detail
	case CompletionKeyword:
		return "keyword"
	case CompletionFunction:
		return "function"
	case CompletionOperator:
		return "operator"
	case CompletionValue:
		return "value"
	}
	return ""
}

var defaultOperators = []CompletionItem{
	{Name: "=", Kind: CompletionOperator, Detail: "equals"},
	{Name: "!=", Kind: CompletionOperator, Detail: "not equals"},
	{Name: "<>", Kind: CompletionOperator, Detail: "not equals"},
	{Name: "<", Kind: CompletionOperator, Detail: "less than"},
	{Name: "<=", Kind: CompletionOperator, Detail: "less or equal"},
	{Name: ">", Kind: CompletionOperator, Detail: "greater than"},
	{Name: ">=", Kind: CompletionOperator, Detail: "greater or equal"},
	{Name: "LIKE", Kind: CompletionOperator, Detail: "pattern match"},
	{Name: "ILIKE", Kind: CompletionOperator, Detail: "case-insensitive"},
	{Name: "IN", Kind: CompletionOperator, Detail: "set membership"},
	{Name: "NOT IN", Kind: CompletionOperator, Detail: "exclusion"},
	{Name: "IS", Kind: CompletionOperator, Detail: "null check"},
	{Name: "IS NOT", Kind: CompletionOperator, Detail: "not null"},
	{Name: "BETWEEN", Kind: CompletionOperator, Detail: "range"},
	{Name: "IS DISTINCT FROM", Kind: CompletionOperator, Detail: "safe equality"},
}

var defaultValues = []CompletionItem{
	{Name: "NULL", Kind: CompletionValue, Detail: "null"},
	{Name: "TRUE", Kind: CompletionValue, Detail: "boolean"},
	{Name: "FALSE", Kind: CompletionValue, Detail: "boolean"},
}

// defaultSelectFunctions are the select-list staples that Postgres exposes as
// aggregates but that the generic keyword/function vocabularies do not cover.
var defaultSelectFunctions = []CompletionItem{
	{Name: "COUNT", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "SUM", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "AVG", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "MIN", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "MAX", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "COALESCE", Kind: CompletionFunction, Detail: "null handling"},
	{Name: "CAST", Kind: CompletionFunction, Detail: "type conversion"},
}

// tableRef is a table (optionally schema-qualified and aliased) referenced by
// the statement, used to narrow column suggestions.
type tableRef struct {
	schema string
	table  string
	alias  string
}

type completionContext struct {
	kind          CompletionKind
	prefix        string
	tokenText     string
	tokenStart    int
	tokenEnd      int
	schema        string
	table         string
	tables        []tableRef
	selectContext bool
}

type AutocompleteState struct {
	all          []CompletionItem
	contextItems []CompletionItem
	filtered     []CompletionItem
	selected     int
	prefix       string
	visible      bool
	maxItems     int
	currentCtx   completionContext
}

func NewAutocompleteState() *AutocompleteState {
	return &AutocompleteState{
		maxItems: 15,
	}
}

func (a *AutocompleteState) LoadSchema(export *context.SchemaExport) {
	a.all = a.all[:0]
	if export == nil {
		autocompleteDebugLog("LoadSchema: export is nil")
		return
	}
	autocompleteDebugLog("LoadSchema: %d schemas", len(export.Schemas))
	for _, schema := range export.Schemas {
		autocompleteDebugLog("LoadSchema: schema=%q tables=%d", schema.Name, len(schema.Tables))
		a.all = append(a.all, CompletionItem{
			Name: schema.Name,
			Kind: CompletionSchema,
		})
		for _, table := range schema.Tables {
			a.all = append(a.all, CompletionItem{
				Name:   table.Name,
				Kind:   CompletionTable,
				Schema: schema.Name,
			})
			for _, col := range table.Columns {
				a.all = append(a.all, CompletionItem{
					Name:   col.Name,
					Kind:   CompletionColumn,
					Detail: col.DataType,
					Schema: schema.Name,
					Table:  table.Name,
				})
			}
		}
	}
	autocompleteDebugLog("LoadSchema: total items=%d", len(a.all))
}

// SetKeywords merges the keyword and function vocabularies. Functions win when
// a name exists in both maps, so e.g. NOW is offered once as NOW(). The result
// is sorted so the suggestion order is deterministic across runs.
func (a *AutocompleteState) SetKeywords(keywords, functions map[string]bool) {
	byName := make(map[string]CompletionItem, len(keywords)+len(functions))
	for fn := range functions {
		key := strings.ToUpper(fn)
		byName[key] = CompletionItem{Name: fn, Kind: CompletionFunction}
	}
	for kw := range keywords {
		key := strings.ToUpper(kw)
		if _, ok := byName[key]; ok {
			continue
		}
		byName[key] = CompletionItem{Name: kw, Kind: CompletionKeyword}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a.all = append(a.all, byName[name])
	}
}

func (a *AutocompleteState) Visible() bool {
	return a.visible && len(a.filtered) > 0
}

func (a *AutocompleteState) SelectedItem() *CompletionItem {
	if !a.Visible() || a.selected >= len(a.filtered) {
		return nil
	}
	item := a.filtered[a.selected]
	return &item
}

func (a *AutocompleteState) SelectNext() {
	if len(a.filtered) == 0 {
		return
	}
	a.selected++
	if a.selected >= len(a.filtered) {
		a.selected = 0
	}
}

func (a *AutocompleteState) SelectPrev() {
	if len(a.filtered) == 0 {
		return
	}
	a.selected--
	if a.selected < 0 {
		a.selected = len(a.filtered) - 1
	}
}

func (a *AutocompleteState) Cancel() {
	a.visible = false
	a.filtered = nil
	a.contextItems = nil
	a.selected = 0
	a.prefix = ""
	a.currentCtx = completionContext{}
}

// CompletionSpan reports the byte range of the token currently under the
// cursor. Accepting a suggestion replaces exactly that range.
func (a *AutocompleteState) CompletionSpan() (start, end int) {
	return a.currentCtx.tokenStart, a.currentCtx.tokenEnd
}

func (a *AutocompleteState) UpdateContext(ctx completionContext) {
	a.currentCtx = ctx
	a.prefix = ctx.prefix
	a.rebuildContextItems()
	a.selected = 0
	a.applyFilter()
	a.visible = len(a.filtered) > 0
	autocompleteDebugLog("UpdateContext: kind=%d prefix=%q token=%q schema=%q table=%q tables=%d filtered=%d visible=%v",
		ctx.kind, ctx.prefix, ctx.tokenText, ctx.schema, ctx.table, len(ctx.tables), len(a.filtered), a.visible)
}

func (a *AutocompleteState) rebuildContextItems() {
	a.contextItems = a.contextItems[:0]

	switch a.currentCtx.kind {
	case CompletionSchema:
		for _, item := range a.all {
			if item.Kind == CompletionSchema {
				a.contextItems = append(a.contextItems, item)
			}
		}
	case CompletionTable:
		a.contextItems = append(a.contextItems, a.tableItems(a.currentCtx.schema)...)
	case CompletionColumn:
		a.contextItems = append(a.contextItems, a.columnItems(a.currentCtx)...)
	case CompletionSelectList:
		a.contextItems = append(a.contextItems, a.selectListItems(a.currentCtx)...)
	case CompletionOperator:
		a.contextItems = append(a.contextItems, defaultOperators...)
	case CompletionValue:
		a.contextItems = append(a.contextItems, defaultValues...)
	case CompletionEmpty:
	default:
		for _, item := range a.all {
			if item.Kind == CompletionKeyword || item.Kind == CompletionFunction {
				a.contextItems = append(a.contextItems, item)
			}
		}
	}

	// Keyword completion stays reachable from any clause: once the user is
	// mid-word on a keyword prefix, keyword/function items are appended after
	// the clause-specific ones. They rank below by prefix but are still
	// offered, so "SELECT * FR" can complete FROM without hijacking "FROM u".
	if a.currentCtx.kind != CompletionKeyword && a.currentCtx.kind != CompletionEmpty &&
		a.currentCtx.prefix != "" && isSQLKeywordPrefix(a.currentCtx.tokenText) {
		for _, item := range a.all {
			if item.Kind == CompletionKeyword || item.Kind == CompletionFunction {
				a.contextItems = append(a.contextItems, item)
			}
		}
	}

	a.contextItems = dedupeItems(a.contextItems)
	autocompleteDebugLog("rebuildContextItems: kind=%d → %d contextItems (from %d total)", a.currentCtx.kind, len(a.contextItems), len(a.all))
}

// tableItems returns tables; when a schema is given it is restricted to it,
// otherwise schemas are included so the user can still drill down.
func (a *AutocompleteState) tableItems(schema string) []CompletionItem {
	var items []CompletionItem
	for _, item := range a.all {
		switch item.Kind {
		case CompletionSchema:
			if schema == "" {
				items = append(items, item)
			}
		case CompletionTable:
			if schema == "" || strings.EqualFold(item.Schema, schema) {
				items = append(items, item)
			}
		}
	}
	return items
}

// columnItems narrows columns by the qualified name when present, falling back
// to every table referenced by the statement. Without any table it returns
// nothing rather than dumping the whole catalog.
func (a *AutocompleteState) columnItems(ctx completionContext) []CompletionItem {
	if ctx.schema == "" && ctx.table == "" && len(ctx.tables) == 0 {
		return nil
	}
	var items []CompletionItem
	seen := make(map[string]bool)
	for _, item := range a.all {
		if item.Kind != CompletionColumn {
			continue
		}
		if !columnMatches(item, ctx) {
			continue
		}
		key := strings.ToLower(item.Name) + "\x00" + strings.ToLower(item.Schema) + "\x00" + strings.ToLower(item.Table)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item)
	}
	return items
}

func columnMatches(item CompletionItem, ctx completionContext) bool {
	switch {
	case ctx.schema != "" && ctx.table != "":
		return strings.EqualFold(item.Schema, ctx.schema) && strings.EqualFold(item.Table, ctx.table)
	case ctx.table != "":
		return strings.EqualFold(item.Table, ctx.table)
	default:
		for _, ref := range ctx.tables {
			if strings.EqualFold(item.Table, ref.table) && (ref.schema == "" || strings.EqualFold(item.Schema, ref.schema)) {
				return true
			}
		}
		return false
	}
}

func (a *AutocompleteState) selectListItems(ctx completionContext) []CompletionItem {
	var items []CompletionItem
	if ctx.selectContext {
		items = append(items,
			CompletionItem{Name: "*", Kind: CompletionKeyword, Detail: "all columns"},
			CompletionItem{Name: "DISTINCT", Kind: CompletionKeyword, Detail: "unique rows"},
		)
	}
	items = append(items, a.columnItems(ctx)...)
	items = append(items, defaultSelectFunctions...)
	for _, item := range a.all {
		if item.Kind == CompletionFunction {
			items = append(items, item)
		}
	}
	return dedupeItems(items)
}

func dedupeItems(items []CompletionItem) []CompletionItem {
	if len(items) == 0 {
		return items
	}
	out := items[:0]
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%d\x00%s", item.Kind, strings.ToLower(item.Name))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// applyFilter ranks prefix matches ahead of fuzzy subsequence matches and drops
// the exact keyword the user already typed, so accepting a completion never
// duplicates text.
func (a *AutocompleteState) applyFilter() {
	if len(a.contextItems) == 0 {
		a.filtered = nil
		return
	}

	exactKeyword := ""
	if a.currentCtx.tokenText != "" && a.currentCtx.prefix == a.currentCtx.tokenText {
		exactKeyword = strings.ToLower(a.currentCtx.tokenText)
	}

	if a.prefix == "" {
		result := make([]CompletionItem, 0, len(a.contextItems))
		for _, item := range a.contextItems {
			if item.Kind == CompletionKeyword && strings.ToLower(item.Name) == exactKeyword {
				continue
			}
			result = append(result, item)
		}
		a.filtered = result
		return
	}

	query := strings.ToLower(a.prefix)
	var prefixHits, fuzzyHits []CompletionItem
	for _, item := range a.contextItems {
		name := strings.ToLower(item.Name)
		if item.Kind == CompletionKeyword && name == exactKeyword {
			continue
		}
		if strings.HasPrefix(name, query) {
			prefixHits = append(prefixHits, item)
			continue
		}
		if fuzzyMatchAutocomplete(query, name) {
			fuzzyHits = append(fuzzyHits, item)
		}
	}
	a.filtered = append(prefixHits, fuzzyHits...)
	autocompleteDebugLog("applyFilter: prefix=%q exactKeyword=%q matched %d of %d contextItems", query, exactKeyword, len(a.filtered), len(a.contextItems))
}

func fuzzyMatchAutocomplete(query, target string) bool {
	if query == "" {
		return true
	}
	qi := 0
	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if target[ti] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// detectContext resolves the suggestion context from the tokens of the current
// line. It ignores strings and comments, tracks the active clause and, when the
// cursor sits on a qualified name, narrows to tables or columns.
func (a *AutocompleteState) detectContext(line string, col int) completionContext {
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}

	rawTokens := tokenizeSQL(line)
	autocompleteDebugLog("detectContext: ENTER line=%q col=%d tokens=%d allItems=%d", line, col, len(rawTokens), len(a.all))

	if insideLiteralOrComment(rawTokens, col) {
		autocompleteDebugLog("detectContext: inside literal/comment → no suggestions")
		return completionContext{kind: CompletionEmpty, tokenStart: col, tokenEnd: col}
	}

	tokens := significantTokens(rawTokens)

	refs := a.findTablesInStatement(tokens)

	currentIdx := -1
	for i := range tokens {
		if tokens[i].start < col && col <= tokens[i].end && isWordToken(tokens[i]) {
			currentIdx = i
			break
		}
	}

	passed := make([]sqlToken, 0, len(tokens))
	for i := range tokens {
		if i == currentIdx {
			continue
		}
		if tokens[i].end <= col {
			passed = append(passed, tokens[i])
		}
	}
	if idx := lastIndexText(passed, ";"); idx >= 0 {
		passed = passed[idx+1:]
	}

	ctx := completionContext{tokenStart: col, tokenEnd: col}
	if currentIdx >= 0 {
		current := tokens[currentIdx]
		ctx.tokenStart = current.start
		ctx.tokenEnd = current.end
		ctx.tokenText = current.text
		ctx.prefix = line[current.start:col]
	}

	if kind, schema, table, ok := a.qualifiedContext(passed, refs); ok {
		ctx.kind = kind
		ctx.schema = schema
		ctx.table = table
		autocompleteDebugLog("detectContext: qualified kind=%d schema=%q table=%q prefix=%q", kind, schema, table, ctx.prefix)
		return ctx
	}

	clause, tail := lastClause(passed)
	autocompleteDebugLog("detectContext: clause=%q tail=%d prefix=%q tokenText=%q", clause, len(tail), ctx.prefix, ctx.tokenText)

	switch clause {
	case "FROM", "JOIN", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN",
		"LEFT OUTER JOIN", "RIGHT OUTER JOIN", "FULL OUTER JOIN", "LEFT OUTER", "RIGHT OUTER", "FULL OUTER",
		"INTO", "UPDATE", "TABLE", "CREATE TABLE", "DROP TABLE", "ALTER TABLE", "TRUNCATE TABLE":
		if expectingTable(tail) {
			ctx.kind = CompletionTable
		} else {
			ctx.kind = CompletionKeyword
		}
	case "SELECT":
		ctx.kind = CompletionSelectList
		ctx.selectContext = len(tail) == 0
		ctx.tables = refs
	case "WHERE", "AND", "OR", "ON", "HAVING":
		ctx.kind = predicateContext(tail)
		ctx.tables = refs
	case "ORDER BY", "GROUP BY", "SET", "RETURNING", "DISTINCT", "AS":
		ctx.kind = CompletionColumn
		ctx.tables = refs
	case "IN", "NOT IN", "EXISTS":
		ctx.kind = CompletionKeyword
	case "IS", "IS NOT", "BETWEEN", "LIKE", "ILIKE":
		ctx.kind = CompletionValue
	case "LIMIT", "OFFSET":
		ctx.kind = CompletionEmpty
	default:
		ctx.kind = CompletionKeyword
	}
	return ctx
}

// qualifiedContext handles `schema.`, `table.` and `schema.table.` prefixes.
func (a *AutocompleteState) qualifiedContext(passed []sqlToken, refs []tableRef) (CompletionKind, string, string, bool) {
	if len(passed) < 2 || passed[len(passed)-1].text != "." {
		return 0, "", "", false
	}
	ident := passed[len(passed)-2]
	if !isWordToken(ident) {
		return 0, "", "", false
	}

	if len(passed) >= 4 && passed[len(passed)-3].text == "." && isWordToken(passed[len(passed)-4]) {
		return CompletionColumn, passed[len(passed)-4].text, ident.text, true
	}
	if a.isKnownSchema(ident.text) || a.hasTablesInSchema(ident.text) {
		return CompletionTable, ident.text, "", true
	}
	if schema, table, ok := a.resolveTable(ident.text, refs); ok {
		return CompletionColumn, schema, table, true
	}
	return CompletionColumn, "", ident.text, true
}

func (a *AutocompleteState) resolveTable(name string, refs []tableRef) (string, string, bool) {
	for _, ref := range refs {
		if ref.alias != "" && strings.EqualFold(ref.alias, name) {
			return ref.schema, ref.table, true
		}
	}
	for _, ref := range refs {
		if strings.EqualFold(ref.table, name) {
			return ref.schema, ref.table, true
		}
	}
	return a.lookupTable(name)
}

func (a *AutocompleteState) lookupTable(name string) (string, string, bool) {
	for _, item := range a.all {
		if item.Kind == CompletionTable && strings.EqualFold(item.Name, name) {
			return item.Schema, item.Name, true
		}
	}
	return "", "", false
}

func (a *AutocompleteState) isKnownSchema(name string) bool {
	for _, item := range a.all {
		if item.Kind == CompletionSchema && strings.EqualFold(item.Name, name) {
			return true
		}
	}
	return false
}

func (a *AutocompleteState) hasTablesInSchema(schema string) bool {
	for _, item := range a.all {
		if item.Kind == CompletionTable && strings.EqualFold(item.Schema, schema) {
			return true
		}
	}
	return false
}

// findTablesInStatement collects the tables referenced by FROM/JOIN/UPDATE/
// INTO/DELETE/TABLE clauses, resolving schemas and aliases.
func (a *AutocompleteState) findTablesInStatement(tokens []sqlToken) []tableRef {
	var refs []tableRef
	expect := false
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		upper := strings.ToUpper(token.text)
		switch upper {
		case "FROM", "JOIN", "UPDATE", "INTO", "DELETE", "TABLE", ",":
			expect = true
			continue
		case "SELECT", "WHERE", "GROUP", "ORDER", "HAVING", "SET", "VALUES", "ON",
			"LIMIT", "OFFSET", "RETURNING", "UNION", "AS":
			expect = false
			continue
		}
		if !expect || !isWordToken(token) {
			continue
		}

		ref := tableRef{table: token.text}
		if i+2 < len(tokens) && tokens[i+1].text == "." && isWordToken(tokens[i+2]) {
			ref.schema = token.text
			ref.table = tokens[i+2].text
			i += 2
		}
		if ref.schema == "" {
			if schema, table, ok := a.lookupTable(ref.table); ok {
				ref.schema = schema
				ref.table = table
			}
		}

		j := i + 1
		if j < len(tokens) && strings.EqualFold(tokens[j].text, "AS") {
			j++
		}
		if j < len(tokens) && isWordToken(tokens[j]) && !isSQLKeyword(tokens[j].text) {
			ref.alias = tokens[j].text
			i = j
		}

		refs = append(refs, ref)
		expect = false
	}
	return refs
}

func lastClause(passed []sqlToken) (string, []sqlToken) {
	for i := len(passed) - 1; i >= 0; i-- {
		for _, phrase := range clausePhrases {
			if len(phrase) > len(passed)-i {
				continue
			}
			match := true
			for k, word := range phrase {
				if !strings.EqualFold(passed[i+k].text, word) {
					match = false
					break
				}
			}
			if match {
				return strings.Join(phrase, " "), passed[i+len(phrase):]
			}
		}
	}
	return "", passed
}

// clausePhrases is ordered longest-first so multi-word clauses win at the same
// start index. Single words that only exist inside a compound (BY, OUTER…) are
// intentionally omitted.
var clausePhrases = [][]string{
	{"LEFT", "OUTER", "JOIN"},
	{"RIGHT", "OUTER", "JOIN"},
	{"FULL", "OUTER", "JOIN"},
	{"CREATE", "TABLE"},
	{"DROP", "TABLE"},
	{"ALTER", "TABLE"},
	{"TRUNCATE", "TABLE"},
	{"ORDER", "BY"},
	{"GROUP", "BY"},
	{"INNER", "JOIN"},
	{"LEFT", "JOIN"},
	{"RIGHT", "JOIN"},
	{"FULL", "JOIN"},
	{"CROSS", "JOIN"},
	{"NOT", "IN"},
	{"IS", "NOT"},
	{"UNION", "ALL"},
	{"LEFT", "OUTER"},
	{"RIGHT", "OUTER"},
	{"FULL", "OUTER"},
	{"SELECT"},
	{"FROM"},
	{"WHERE"},
	{"AND"},
	{"OR"},
	{"ON"},
	{"HAVING"},
	{"JOIN"},
	{"INTO"},
	{"UPDATE"},
	{"DELETE"},
	{"SET"},
	{"VALUES"},
	{"RETURNING"},
	{"DISTINCT"},
	{"AS"},
	{"TABLE"},
	{"IN"},
	{"EXISTS"},
	{"IS"},
	{"LIKE"},
	{"ILIKE"},
	{"BETWEEN"},
	{"LIMIT"},
	{"OFFSET"},
}

func expectingTable(tail []sqlToken) bool {
	if len(tail) == 0 {
		return true
	}
	return tail[len(tail)-1].text == ","
}

// predicateContext walks the tokens after WHERE/AND/ON to decide whether the
// next token is a column, an operator or a value.
func predicateContext(tail []sqlToken) CompletionKind {
	if len(tail) == 0 {
		return CompletionColumn
	}
	if isOperatorToken(tail[len(tail)-1]) {
		return CompletionValue
	}
	for _, token := range tail {
		if isOperatorToken(token) {
			return CompletionKeyword
		}
	}
	return CompletionOperator
}

func isOperatorToken(token sqlToken) bool {
	switch strings.ToUpper(token.text) {
	case "=", "!=", "<>", "<", "<=", ">", ">=", "LIKE", "ILIKE", "IN", "IS", "BETWEEN", "NOT":
		return true
	}
	return false
}

func isSQLKeyword(word string) bool {
	return sqlKeywords[strings.ToUpper(word)]
}

// isSQLKeywordPrefix reports whether word could still become a SQL keyword,
// which lets any clause fall back to keyword completion.
func isSQLKeywordPrefix(word string) bool {
	if word == "" {
		return false
	}
	upper := strings.ToUpper(word)
	for keyword := range sqlKeywords {
		if strings.HasPrefix(keyword, upper) {
			return true
		}
	}
	return false
}

// isWordToken reports whether a token looks like an identifier, keyword,
// function or literal word that can sit under the cursor.
func isWordToken(token sqlToken) bool {
	if token.typ == tokenOperator || token.typ == tokenString {
		return false
	}
	switch token.text {
	case "", "(", ")", ",", ";", ".":
		return false
	}
	return true
}

func significantTokens(all []sqlToken) []sqlToken {
	tokens := make([]sqlToken, 0, len(all))
	for _, token := range all {
		if token.typ == tokenComment || strings.TrimSpace(token.text) == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func insideLiteralOrComment(tokens []sqlToken, col int) bool {
	for _, token := range tokens {
		if token.start >= col {
			break
		}
		switch token.typ {
		case tokenString:
			if col < token.end {
				return true
			}
			if col == token.end && !strings.HasSuffix(token.text, "'") {
				return true
			}
		case tokenComment:
			// A line comment always runs to the end of the line; a block
			// comment suppresses while the cursor sits before its close.
			if strings.HasPrefix(token.text, "--") {
				return true
			}
			if col < token.end || !strings.HasSuffix(token.text, "*/") {
				return true
			}
		}
	}
	return false
}

func lastIndexText(tokens []sqlToken, text string) int {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].text == text {
			return i
		}
	}
	return -1
}

func autocompleteDebugLog(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/dbx_autocomplete_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "Autocomplete: "+format+"\n", args...)
}

func (a *AutocompleteState) Render(styles *theme.Styles, width int) string {
	if !a.Visible() {
		return ""
	}

	maxNameLen := 0
	for _, item := range a.filtered {
		l := len(item.Label())
		if l > maxNameLen {
			maxNameLen = l
		}
	}

	popupW := maxNameLen + 25
	if popupW < 40 {
		popupW = 40
	}
	if popupW > width-4 {
		popupW = width - 4
	}
	kindW := 10

	start := 0
	end := len(a.filtered)
	if end > a.maxItems {
		end = a.maxItems
	}

	var lines []string

	if a.selected >= end {
		start = a.selected - a.maxItems + 1
		end = start + a.maxItems
	}
	if end > len(a.filtered) {
		end = len(a.filtered)
		start = end - a.maxItems
		if start < 0 {
			start = 0
		}
	}

	for i := start; i < end; i++ {
		item := a.filtered[i]
		name := item.Label()
		kind := item.KindLabel()

		nameMaxW := popupW - kindW - 5
		if nameMaxW < 10 {
			nameMaxW = 10
		}
		nameW := lipgloss.Width(name)
		if nameW > nameMaxW {
			name = ansi.Truncate(name, nameMaxW, "..")
			nameW = lipgloss.Width(name)
		}
		namePad := nameMaxW - nameW
		if namePad > 0 {
			name += strings.Repeat(" ", namePad)
		}

		kindWDisplay := lipgloss.Width(kind)
		if kindWDisplay > kindW {
			kind = ansi.Truncate(kind, kindW, "..")
			kindWDisplay = lipgloss.Width(kind)
		}
		kindPad := kindW - kindWDisplay
		if kindPad > 0 {
			kind += strings.Repeat(" ", kindPad)
		}

		var nameStyled string
		switch item.Kind {
		case CompletionKeyword:
			nameStyled = styles.Primary.Render(name)
		case CompletionFunction:
			nameStyled = styles.Info.Render(name)
		case CompletionSchema:
			nameStyled = styles.Warning.Render(name)
		case CompletionTable:
			nameStyled = styles.Success.Render(name)
		case CompletionColumn:
			nameStyled = styles.TextBright.Render(name)
		case CompletionOperator:
			nameStyled = styles.Warning.Render(name)
		case CompletionValue:
			nameStyled = styles.Info.Render(name)
		default:
			nameStyled = name
		}

		kindStyled := styles.TextMuted.Render(kind)

		line := "  " + nameStyled + kindStyled
		if i == a.selected {
			line = styles.Selected.Render("▸") + nameStyled + kindStyled
		} else {
			line = " " + line
		}

		lines = append(lines, line)
	}

	scrollInfo := ""
	if len(a.filtered) > a.maxItems {
		scrollInfo = styles.TextMuted.Render(
			fmt.Sprintf("  %d/%d", a.selected+1, len(a.filtered)),
		)
	}

	content := strings.Join(lines, "\n") + scrollInfo

	return styles.BorderActive.
		Width(popupW).
		Render(content)
}
