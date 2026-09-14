package editor

import (
	"fmt"
	"os"
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

var defaultSelectItems = []CompletionItem{
	{Name: "*", Kind: CompletionKeyword, Detail: "all columns"},
	{Name: "DISTINCT", Kind: CompletionKeyword, Detail: "unique rows"},
	{Name: "COUNT", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "SUM", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "AVG", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "MIN", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "MAX", Kind: CompletionFunction, Detail: "aggregate"},
	{Name: "COALESCE", Kind: CompletionFunction, Detail: "null handling"},
	{Name: "CAST", Kind: CompletionFunction, Detail: "type conversion"},
}

type completionContext struct {
	kind          CompletionKind
	prefix        string
	schema        string
	table         string
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

func (a *AutocompleteState) SetKeywords(keywords, functions map[string]bool) {
	for kw := range keywords {
		a.all = append(a.all, CompletionItem{
			Name: kw,
			Kind: CompletionKeyword,
		})
	}
	for fn := range functions {
		a.all = append(a.all, CompletionItem{
			Name: fn,
			Kind: CompletionFunction,
		})
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

func (a *AutocompleteState) UpdateContext(ctx completionContext) {
	a.currentCtx = ctx
	a.prefix = ctx.prefix
	a.rebuildContextItems()
	a.selected = 0
	a.applyFilter()
	a.visible = len(a.filtered) > 0
	autocompleteDebugLog("UpdateContext: kind=%d prefix=%q schema=%q table=%q selectCtx=%v filtered=%d visible=%v",
		ctx.kind, ctx.prefix, ctx.schema, ctx.table, ctx.selectContext, len(a.filtered), a.visible)
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
		if a.currentCtx.schema != "" {
			for _, item := range a.all {
				if item.Kind == CompletionTable && strings.EqualFold(item.Schema, a.currentCtx.schema) {
					a.contextItems = append(a.contextItems, item)
				}
			}
		} else {
			for _, item := range a.all {
				if item.Kind == CompletionSchema || item.Kind == CompletionTable {
					a.contextItems = append(a.contextItems, item)
				}
			}
		}
	case CompletionColumn:
		if a.currentCtx.schema != "" && a.currentCtx.table != "" {
			for _, item := range a.all {
				if item.Kind == CompletionColumn &&
					strings.EqualFold(item.Schema, a.currentCtx.schema) &&
					strings.EqualFold(item.Table, a.currentCtx.table) {
					a.contextItems = append(a.contextItems, item)
				}
			}
		} else if a.currentCtx.table != "" {
			for _, item := range a.all {
				if item.Kind == CompletionColumn && strings.EqualFold(item.Table, a.currentCtx.table) {
					a.contextItems = append(a.contextItems, item)
				}
			}
		} else {
			for _, item := range a.all {
				if item.Kind == CompletionColumn {
					a.contextItems = append(a.contextItems, item)
				}
			}
		}
	case CompletionKeyword:
		if a.currentCtx.selectContext {
			a.contextItems = append(a.contextItems, defaultSelectItems...)
		} else {
			for _, item := range a.all {
				if item.Kind == CompletionKeyword || item.Kind == CompletionFunction {
					a.contextItems = append(a.contextItems, item)
				}
			}
		}
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
	autocompleteDebugLog("rebuildContextItems: kind=%d → %d contextItems (from %d total)", a.currentCtx.kind, len(a.contextItems), len(a.all))
}

func (a *AutocompleteState) applyFilter() {
	if a.prefix == "" {
		a.filtered = make([]CompletionItem, len(a.contextItems))
		copy(a.filtered, a.contextItems)
		autocompleteDebugLog("applyFilter: empty prefix, showing all %d contextItems", len(a.contextItems))
		return
	}

	q := strings.ToLower(a.prefix)
	var result []CompletionItem
	for _, item := range a.contextItems {
		if fuzzyMatchAutocomplete(q, strings.ToLower(item.Name)) {
			result = append(result, item)
		}
	}
	autocompleteDebugLog("applyFilter: prefix=%q matched %d of %d contextItems", q, len(result), len(a.contextItems))
	a.filtered = result
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

func (a *AutocompleteState) detectContext(line string, col int) completionContext {
	autocompleteDebugLog("detectContext: ENTER line=%q col=%d allItems=%d", line, col, len(a.all))
	if col > len(line) {
		col = len(line)
	}
	before := line[:col]

	dotIdx := strings.LastIndex(before, ".")
	if dotIdx >= 0 {
		afterDot := before[dotIdx+1:]
		afterDotUpper := strings.ToUpper(strings.TrimSpace(afterDot))
		dotHasKeywordAfter := false
		dotKeywords := []string{"WHERE", "AND", "OR", "ON", "HAVING", "FROM", "JOIN", "SELECT", "ORDER", "GROUP", "SET", "INTO", "VALUES", "LIMIT", "OFFSET"}
		for _, kw := range dotKeywords {
			if strings.HasPrefix(afterDotUpper, kw+" ") || afterDotUpper == kw {
				dotHasKeywordAfter = true
				break
			}
			if strings.Contains(afterDotUpper, " "+kw+" ") || strings.HasSuffix(afterDotUpper, " "+kw) {
				dotHasKeywordAfter = true
				break
			}
		}

		if !dotHasKeywordAfter {
			qualifier := extractIdentifierBefore(before, dotIdx)

			if qualifier == "" {
				autocompleteDebugLog("detectContext: dot empty qualifier, prefix=%q", afterDot)
				return completionContext{
					kind:   CompletionKeyword,
					prefix: afterDot,
				}
			}

			dot2 := strings.LastIndex(qualifier, ".")
			if dot2 >= 0 {
				schema := qualifier[:dot2]
				table := qualifier[dot2+1:]
				autocompleteDebugLog("detectContext: double-dot schema=%q table=%q prefix=%q", schema, table, afterDot)
				return completionContext{
					kind:   CompletionColumn,
					prefix: afterDot,
					schema: schema,
					table:  table,
				}
			}

			if a.isKnownSchema(qualifier) || a.hasTablesInSchema(qualifier) {
				return completionContext{
					kind:   CompletionTable,
					prefix: afterDot,
					schema: qualifier,
				}
			}

			return completionContext{
				kind:   CompletionColumn,
				prefix: afterDot,
				table:  qualifier,
			}
		}
	}

	trimmed := strings.TrimRight(before, " \t\n\r")
	upperTrimmed := strings.ToUpper(trimmed)
	currentWord := extractLastWord(upperTrimmed)
	lastKeyword := extractLastSQLKeyword(upperTrimmed)
	hasTrailingSpace := len(before) > len(trimmed)
	autocompleteDebugLog("detectContext: lastKeyword=%q currentWord=%q hasTrailingSpace=%v line=%q", lastKeyword, currentWord, hasTrailingSpace, before)

	if lastKeyword == "FROM" && (hasTrailingSpace || currentWord != "FROM") {
		prefix := currentWord
		if isFullSQLKeyword(prefix) {
			prefix = ""
		}
		autocompleteDebugLog("detectContext: FROM → schemas, prefix=%q", prefix)
		return completionContext{
			kind:   CompletionSchema,
			prefix: prefix,
		}
	}

	contextKeywords := map[string]bool{
		"FROM": true, "JOIN": true, "INNER JOIN": true, "LEFT JOIN": true,
		"RIGHT JOIN": true, "FULL JOIN": true, "FULL OUTER JOIN": true,
		"LEFT OUTER JOIN": true, "RIGHT OUTER JOIN": true, "CROSS JOIN": true,
		"LEFT OUTER": true, "RIGHT OUTER": true, "FULL OUTER": true,
		"WHERE": true, "AND": true, "OR": true, "ON": true, "HAVING": true,
		"ORDER BY": true, "GROUP BY": true, "SET": true, "DISTINCT": true,
		"INTO": true, "UPDATE": true, "TABLE": true, "ALTER TABLE": true,
		"CREATE TABLE": true, "DROP TABLE": true, "TRUNCATE TABLE": true,
		"VALUES": true, "RETURNING": true, "AS": true,
	}

	if currentWord != "" && !isFullSQLKeyword(currentWord) && isSQLKeywordPrefix(currentWord) && !contextKeywords[lastKeyword] && !hasTrailingSpace {
		autocompleteDebugLog("detectContext: keywordPrefixCheck HIT → CompletionKeyword prefix=%q", currentWord)
		return completionContext{
			kind:   CompletionKeyword,
			prefix: currentWord,
		}
	}

	if lastKeyword == "SELECT" {
		if strings.EqualFold(currentWord, "SELECT") {
			return completionContext{kind: CompletionEmpty}
		}
		return completionContext{
			kind:          CompletionKeyword,
			prefix:        currentWord,
			selectContext: true,
		}
	}

	switch lastKeyword {
	case "WHERE", "AND", "OR", "ON", "HAVING":
		if !hasTrailingSpace && currentWord == lastKeyword {
			return completionContext{
				kind:   CompletionKeyword,
				prefix: currentWord,
			}
		}
		if isOperatorContext(upperTrimmed) {
			autocompleteDebugLog("detectContext: WHERE → operator ctx → values")
			return completionContext{
				kind:   CompletionValue,
				prefix: "",
			}
		}
		if isColumnContext(upperTrimmed, a.all) && hasTrailingSpace {
			autocompleteDebugLog("detectContext: WHERE → column ctx → operators")
			return completionContext{
				kind:   CompletionOperator,
				prefix: "",
			}
		}
		if hasOperatorBefore(upperTrimmed) {
			autocompleteDebugLog("detectContext: WHERE → has operator before → values")
			return completionContext{
				kind:   CompletionValue,
				prefix: currentWord,
			}
		}
		schema, table := a.findTableInFROM(upperTrimmed)
		prefix := currentWord
		if isFullSQLKeyword(prefix) {
			prefix = ""
		}
		autocompleteDebugLog("detectContext: WHERE → default columns, schema=%q table=%q prefix=%q", schema, table, prefix)
		return completionContext{
			kind:   CompletionColumn,
			prefix: prefix,
			schema: schema,
			table:  table,
		}

	case "JOIN", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN",
		"FULL JOIN", "FULL OUTER JOIN", "LEFT OUTER JOIN", "RIGHT OUTER JOIN",
		"CROSS JOIN", "LEFT OUTER", "RIGHT OUTER", "FULL OUTER":
		prefix := currentWord
		if isFullSQLKeyword(prefix) {
			prefix = ""
		}
		return completionContext{
			kind:   CompletionTable,
			prefix: prefix,
		}

	case "INTO", "UPDATE", "TABLE", "ALTER TABLE",
		"CREATE TABLE", "DROP TABLE", "TRUNCATE TABLE":
		prefix := currentWord
		if isFullSQLKeyword(prefix) {
			prefix = ""
		}
		return completionContext{
			kind:   CompletionTable,
			prefix: prefix,
		}

	case "ORDER BY", "GROUP BY", "SET", "DISTINCT",
		"VALUES", "RETURNING", "AS":
		prefix := currentWord
		if isFullSQLKeyword(prefix) {
			prefix = ""
		}
		return completionContext{
			kind:   CompletionColumn,
			prefix: prefix,
		}

	case "IN", "NOT IN", "EXISTS":
		return completionContext{
			kind:   CompletionKeyword,
			prefix: currentWord,
		}

	case "NULL", "TRUE", "FALSE":
		return completionContext{kind: CompletionEmpty}

	default:
		return completionContext{
			kind:   CompletionKeyword,
			prefix: currentWord,
		}
	}
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

func (a *AutocompleteState) findTableInFROM(line string) (schema, table string) {
	upper := strings.ToUpper(line)

	var fromKeywords = []string{
		"LEFT OUTER JOIN", "RIGHT OUTER JOIN", "FULL OUTER JOIN",
		"INNER JOIN", "LEFT JOIN", "RIGHT JOIN", "FULL JOIN",
		"CROSS JOIN", "JOIN", "FROM",
	}

	bestPos := -1
	for _, kw := range fromKeywords {
		idx := strings.LastIndex(upper, " "+kw+" ")
		if idx >= 0 && idx > bestPos {
			bestPos = idx + len(kw) + 1
		}
	}

	if bestPos < 0 {
		return "", ""
	}

	rest := strings.TrimSpace(line[bestPos:])
	rest = strings.TrimRight(rest, " \t\n\r")
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return "", ""
	}

	tableName := parts[0]
	tableName = strings.TrimRight(tableName, ",);")

	if strings.Contains(tableName, ".") {
		dots := strings.SplitN(tableName, ".", 2)
		return dots[0], dots[1]
	}

	for _, item := range a.all {
		if item.Kind == CompletionTable && strings.EqualFold(item.Name, tableName) {
			return item.Schema, item.Name
		}
	}

	return "", tableName
}

func isOperatorContext(upperTrimmed string) bool {
	tokens := strings.Fields(upperTrimmed)
	if len(tokens) < 2 {
		return false
	}

	operators := map[string]bool{
		"=": true, "!=": true, "<>": true, "<": true, "<=": true,
		">": true, ">=": true, "LIKE": true, "ILIKE": true,
		"IN": true, "NOT": true, "IS": true, "BETWEEN": true,
	}

	last := tokens[len(tokens)-1]
	if operators[last] {
		return true
	}

	if len(tokens) >= 2 {
		twoWord := tokens[len(tokens)-2] + " " + last
		if twoWord == "IS NOT" || twoWord == "NOT IN" || twoWord == "NOT LIKE" || twoWord == "NOT ILIKE" {
			return true
		}
	}

	return false
}

func isColumnContext(upperTrimmed string, allItems []CompletionItem) bool {
	tokens := strings.Fields(upperTrimmed)
	if len(tokens) < 2 {
		return false
	}

	last := tokens[len(tokens)-1]

	if isOperatorContext(upperTrimmed) {
		return false
	}

	if sqlKeywordSet[last] {
		return false
	}

	for _, item := range allItems {
		if item.Kind == CompletionColumn && strings.EqualFold(item.Name, last) {
			return true
		}
	}

	return false
}

func hasOperatorBefore(upperTrimmed string) bool {
	tokens := strings.Fields(upperTrimmed)
	operators := map[string]bool{
		"=": true, "!=": true, "<>": true, "<": true, "<=": true,
		">": true, ">=": true, "LIKE": true, "ILIKE": true,
		"IN": true, "IS": true, "BETWEEN": true,
	}
	for i := len(tokens) - 2; i >= 0; i-- {
		if operators[tokens[i]] {
			return true
		}
		if i > 0 {
			twoWord := tokens[i-1] + " " + tokens[i]
			if twoWord == "IS NOT" || twoWord == "NOT IN" || twoWord == "NOT LIKE" || twoWord == "NOT ILIKE" {
				return true
			}
		}
	}
	return false
}

func extractLastSQLKeyword(s string) string {
	s = strings.TrimRight(s, " \t\n\r")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}

	last := parts[len(parts)-1]
	if len(parts) >= 2 {
		twoWord := parts[len(parts)-2] + " " + last
		switch twoWord {
		case "ORDER BY", "GROUP BY", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN",
			"FULL JOIN", "LEFT OUTER", "RIGHT OUTER", "FULL OUTER",
			"CROSS JOIN", "NOT IN", "CREATE TABLE", "DROP TABLE",
			"ALTER TABLE", "TRUNCATE TABLE":
			return twoWord
		}
	}
	if len(parts) >= 3 {
		threeWord := parts[len(parts)-3] + " " + parts[len(parts)-2] + " " + last
		switch threeWord {
		case "LEFT OUTER JOIN", "RIGHT OUTER JOIN", "FULL OUTER JOIN":
			return threeWord
		}
	}

	if sqlKeywordSet[last] || isSQLKeywordPrefix(last) {
		return last
	}

	for i := len(parts) - 2; i >= 0; i-- {
		if sqlKeywordSet[parts[i]] {
			return parts[i]
		}
	}

	return last
}

var sqlKeywordSet = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "AND": true, "OR": true,
	"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true, "SET": true,
	"DELETE": true, "CREATE": true, "TABLE": true, "ALTER": true, "DROP": true,
	"INDEX": true, "VIEW": true, "JOIN": true, "LEFT": true, "RIGHT": true,
	"INNER": true, "OUTER": true, "ON": true, "AS": true, "ORDER": true,
	"BY": true, "GROUP": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"DISTINCT": true, "UNION": true, "ALL": true, "EXCEPT": true, "INTERSECT": true,
	"IN": true, "NOT": true, "NULL": true, "IS": true, "LIKE": true,
	"BETWEEN": true, "EXISTS": true, "ANY": true, "SOME": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"ASC": true, "DESC": true, "TRUE": true, "FALSE": true,
	"RETURNING": true, "WITH": true, "RECURSIVE": true,
	"GRANT": true, "REVOKE": true, "COMMIT": true, "ROLLBACK": true,
	"BEGIN": true, "TRANSACTION": true, "SAVEPOINT": true,
	"PRIMARY": true, "KEY": true, "FOREIGN": true, "REFERENCES": true,
	"UNIQUE": true, "CHECK": true, "DEFAULT": true, "CONSTRAINT": true,
	"IF": true, "REPLACE": true, "TRUNCATE": true,
	"ANALYZE": true, "VACUUM": true, "EXPLAIN": true,
	"FULL": true, "CROSS": true, "NATURAL": true,
	"PROCEDURE": true, "FUNCTION": true, "TRIGGER": true,
}

func isSQLKeywordPrefix(word string) bool {
	if word == "" {
		return false
	}
	upper := strings.ToUpper(word)
	if sqlKeywordSet[upper] {
		return true
	}
	for kw := range sqlKeywordSet {
		if strings.HasPrefix(kw, upper) {
			return true
		}
	}
	return false
}

func isFullSQLKeyword(word string) bool {
	return sqlKeywordSet[strings.ToUpper(word)]
}

func autocompleteDebugLog(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/dbx_autocomplete_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "Autocomplete: "+format+"\n", args...)
}

func extractLastWord(s string) string {
	s = strings.TrimRight(s, " \t\n\r")
	if len(s) == 0 {
		return ""
	}
	i := len(s) - 1
	for i >= 0 && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
		i--
	}
	return s[i+1:]
}

func extractIdentifierBefore(s string, pos int) string {
	if pos == 0 {
		return ""
	}
	end := pos
	for end > 0 {
		ch := s[end-1]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			end--
		} else if ch == '.' && end-1 > 0 {
			prevCh := s[end-2]
			if (prevCh >= 'a' && prevCh <= 'z') || (prevCh >= 'A' && prevCh <= 'Z') || (prevCh >= '0' && prevCh <= '9') || prevCh == '_' {
				end--
			} else {
				break
			}
		} else {
			break
		}
	}
	if end == pos {
		return ""
	}
	return s[end:pos]
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
