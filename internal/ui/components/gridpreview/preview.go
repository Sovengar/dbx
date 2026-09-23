package gridpreview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
	"github.com/itchyny/gojq"
)

const maxJQHistory = 50
const maxJQSuggestions = 8

type JQSuggestion struct {
	Path string
	Type string
}

type GridPreviewExpandFKMsg struct {
	Column    string
	Path      string // dotted path for nested FKs (e.g. "author_id.country_id")
	Value     interface{}
	RefSchema string
	RefTable  string
	RefColumn string
}

type GridPreviewExpandFKResultMsg struct {
	Column      string
	Path        string
	Row         map[string]interface{}
	ForeignKeys []postgres.ForeignKeyInfo
	Err         error
}

type GridPreview struct {
	styles   *theme.Styles
	keybinds config.Resolver
	lines    []string
	scrollY  int
	width    int
	height   int
	expanded bool
	focused  bool

	rawJSON []byte
	jqExpr  string
	jqMode  bool

	jqInput  string
	jqCursor int

	jqSugs        []JQSuggestion
	jqSugSelected int
	jqSugVisible  bool

	jqHistory    []string
	jqHistoryIdx int

	// FK expansion
	cursorLine  int
	columns     []string
	rowData     map[string]interface{}
	foreignKeys []postgres.ForeignKeyInfo
	expandedFKs map[string]interface{}
	nestedFKs   map[string][]postgres.ForeignKeyInfo // FK metadata per dotted path
}

func New(styles *theme.Styles, keybinds config.Resolver) *GridPreview {
	p := &GridPreview{
		styles:   styles,
		keybinds: keybinds,
	}
	p.loadJQHistory()
	return p
}

func (p *GridPreview) SetWidth(w int)   { p.width = w }
func (p *GridPreview) SetHeight(h int)  { p.height = h }
func (p *GridPreview) Focus()           { p.focused = true }
func (p *GridPreview) Blur()            { p.focused = false; p.jqMode = false }
func (p *GridPreview) IsFocused() bool  { return p.focused }
func (p *GridPreview) IsExpanded() bool { return p.expanded }
func (p *GridPreview) IsJQMode() bool   { return p.jqMode }

func (p *GridPreview) SetForeignKeys(fks []postgres.ForeignKeyInfo) {
	p.foreignKeys = fks
	if p.expandedFKs == nil {
		p.expandedFKs = make(map[string]interface{})
	}
	if p.nestedFKs == nil {
		p.nestedFKs = make(map[string][]postgres.ForeignKeyInfo)
	}
}

func (p *GridPreview) SetRow(columns []string, row []interface{}) {
	if row == nil || len(columns) == 0 {
		p.lines = nil
		p.rawJSON = nil
		p.columns = nil
		p.rowData = nil
		p.expandedFKs = make(map[string]interface{})
		p.nestedFKs = make(map[string][]postgres.ForeignKeyInfo)
		return
	}

	p.columns = columns
	p.rowData = make(map[string]interface{})
	for i, col := range columns {
		if i < len(row) {
			p.rowData[col] = row[i]
		}
	}
	p.expandedFKs = make(map[string]interface{})
	p.nestedFKs = make(map[string][]postgres.ForeignKeyInfo)
	p.cursorLine = 0
	p.scrollY = 0

	jsonBytes, err := json.MarshalIndent(p.rowData, "", "  ")
	if err != nil {
		p.lines = []string{fmt.Sprintf("Error: %v", err)}
		p.rawJSON = nil
		return
	}

	p.rawJSON = jsonBytes

	if p.jqExpr != "" {
		p.applyJQ()
	} else {
		p.lines = strings.Split(string(jsonBytes), "\n")
	}
}

func (p *GridPreview) ToggleExpand() {
	p.expanded = !p.expanded
}

// ── FK Expansion ──────────────────────────────────────────

func (p *GridPreview) rebuildLines() {
	data := p.displayData()
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		p.lines = []string{fmt.Sprintf("Error: %v", err)}
		return
	}
	p.rawJSON = jsonBytes
	p.lines = strings.Split(string(jsonBytes), "\n")
}

func (p *GridPreview) displayData() map[string]interface{} {
	return p.buildDisplayData(p.rowData, "")
}

func (p *GridPreview) buildDisplayData(data map[string]interface{}, parentPath string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		path := joinPath(parentPath, k)
		if expanded, ok := p.expandedFKs[path]; ok {
			if objMap, isMap := expanded.(map[string]interface{}); isMap {
				result[k] = p.buildDisplayData(objMap, path)
			} else {
				result[k] = expanded
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func (p *GridPreview) ExpandFK(column string, path string, obj map[string]interface{}, nestedForeignKeys []postgres.ForeignKeyInfo) {
	if p.expandedFKs == nil {
		p.expandedFKs = make(map[string]interface{})
	}
	if p.nestedFKs == nil {
		p.nestedFKs = make(map[string][]postgres.ForeignKeyInfo)
	}
	p.expandedFKs[path] = obj
	if len(nestedForeignKeys) > 0 {
		p.nestedFKs[path] = nestedForeignKeys
	}
	p.rebuildLines()
	p.ensureCursorVisible()
}

func (p *GridPreview) CollapseFK(path string) {
	prefix := path + "."
	for k := range p.expandedFKs {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(p.expandedFKs, k)
		}
	}
	for k := range p.nestedFKs {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(p.nestedFKs, k)
		}
	}
	p.rebuildLines()
	p.ensureCursorVisible()
}

func (p *GridPreview) parseKeyFromLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, `"`) {
		return ""
	}
	end := strings.Index(trimmed[1:], `"`)
	if end == -1 {
		return ""
	}
	return trimmed[1 : end+1]
}

// isExpandableFK checks if a dotted path refers to an unexpanded FK column.
func (p *GridPreview) isExpandableFK(path string) *postgres.ForeignKeyInfo {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil
	}

	// Root level: check p.foreignKeys
	if len(parts) == 1 {
		key := parts[0]
		for i := range p.foreignKeys {
			if p.foreignKeys[i].Column == key {
				if _, expanded := p.expandedFKs[key]; expanded {
					return nil
				}
				return &p.foreignKeys[i]
			}
		}
		return nil
	}

	// Nested level: find the parent path and check its nested FKs
	parentPath := strings.Join(parts[:len(parts)-1], ".")
	childKey := parts[len(parts)-1]

	fks, ok := p.nestedFKs[parentPath]
	if !ok {
		return nil
	}
	for i := range fks {
		if fks[i].Column == childKey {
			if _, expanded := p.expandedFKs[path]; expanded {
				return nil
			}
			return &fks[i]
		}
	}
	return nil
}

// resolveValue gets the value at a dotted path from rowData/displayData.
func (p *GridPreview) resolveValue(path string) interface{} {
	data := p.displayData()
	parts := strings.Split(path, ".")
	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func (p *GridPreview) handleExpand() (tea.Cmd, bool) {
	if p.cursorLine >= len(p.lines) {
		return nil, false
	}

	// Build path context from all lines up to cursor
	tracker := &fkPathTracker{}
	for i := 0; i <= p.cursorLine; i++ {
		l := p.lines[i]
		k := p.parseKeyFromLine(l)
		indent := countIndent(l)
		if k != "" {
			tracker.onKey(k, indent)
		} else if strings.Contains(l, "}") || strings.Contains(l, "]") {
			tracker.onClose(indent)
		}
	}
	path := tracker.currentPath()

	if path == "" {
		return nil, false
	}

	// Check if this path is an expandable FK
	if fkInfo := p.isExpandableFK(path); fkInfo != nil {
		val := p.resolveValue(path)
		if val == nil {
			return nil, false
		}
		col := fkInfo.Column
		refSchema := fkInfo.RefSchema
		return func() tea.Msg {
			return GridPreviewExpandFKMsg{
				Column:    col,
				Path:      path,
				Value:     val,
				RefSchema: refSchema,
				RefTable:  fkInfo.RefTable,
				RefColumn: fkInfo.RefColumn,
			}
		}, true
	}

	// Check if already expanded → collapse
	if _, expanded := p.expandedFKs[path]; expanded {
		p.CollapseFK(path)
		return nil, true
	}

	return nil, false
}

// ── Cursor Navigation ──────────────────────────────────────────

func (p *GridPreview) cursorUp() {
	if p.cursorLine > 0 {
		p.cursorLine--
		p.ensureCursorVisible()
	}
}

func (p *GridPreview) cursorDown() {
	if p.cursorLine < len(p.lines)-1 {
		p.cursorLine++
		p.ensureCursorVisible()
	}
}

func (p *GridPreview) ensureCursorVisible() {
	contentHeight := p.height - 6
	if contentHeight <= 0 {
		return
	}
	if p.cursorLine >= p.scrollY+contentHeight {
		p.scrollY = p.cursorLine - contentHeight + 1
	}
	if p.cursorLine < p.scrollY {
		p.scrollY = p.cursorLine
	}
	maxScroll := len(p.lines) - contentHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollY > maxScroll {
		p.scrollY = maxScroll
	}
}

// ── JQ Command Mode ──────────────────────────────────────────

func (p *GridPreview) EnterJQMode() {
	p.jqMode = true
	p.jqInput = p.jqExpr
	p.jqCursor = len([]rune(p.jqInput))
	p.jqHistoryIdx = len(p.jqHistory)
	p.updateJQSuggestions()
}

func (p *GridPreview) ExitJQMode() {
	p.jqMode = false
	p.jqInput = ""
	p.jqCursor = 0
	p.jqSugVisible = false
	p.jqSugs = nil
}

func (p *GridPreview) applyJQ() {
	if p.rawJSON == nil {
		return
	}

	if p.jqExpr == "" {
		p.lines = strings.Split(string(p.rawJSON), "\n")
		return
	}

	query, err := gojq.Parse(p.jqExpr)
	if err != nil {
		p.lines = []string{fmt.Sprintf("jq parse error: %v", err)}
		return
	}

	code, err := gojq.Compile(query)
	if err != nil {
		p.lines = []string{fmt.Sprintf("jq compile error: %v", err)}
		return
	}

	var input interface{}
	if err := json.Unmarshal(p.rawJSON, &input); err != nil {
		p.lines = []string{fmt.Sprintf("json unmarshal error: %v", err)}
		return
	}

	iter := code.Run(input)
	var results []interface{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			p.lines = []string{fmt.Sprintf("jq runtime error: %v", err)}
			return
		}
		results = append(results, v)
	}

	if len(results) == 0 {
		p.lines = []string{"(empty result)"}
		return
	}

	var output interface{}
	if len(results) == 1 {
		output = results[0]
	} else {
		output = results
	}

	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		p.lines = []string{fmt.Sprintf("jq output error: %v", err)}
		return
	}

	p.lines = strings.Split(string(jsonBytes), "\n")
	p.scrollY = 0
}

func (p *GridPreview) handleJQInput(msg tea.Msg) (tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, false
	}

	key := keyMsg.String()

	if key == "space" {
		key = " "
	}

	switch key {
	case "esc":
		if p.jqSugVisible {
			p.jqSugVisible = false
			return nil, true
		}
		p.ExitJQMode()
		return nil, true

	case "enter":
		if p.jqSugVisible && len(p.jqSugs) > 0 {
			p.acceptJQSuggestion()
			return nil, true
		}
		p.jqExpr = p.jqInput
		if p.jqExpr != "" {
			p.addToHistory(p.jqExpr)
		}
		p.applyJQ()
		p.ExitJQMode()
		return nil, true

	case "tab":
		if p.jqSugVisible && len(p.jqSugs) > 0 {
			p.acceptJQSuggestion()
			return nil, true
		}
		return nil, true

	case "ctrl+space":
		p.updateJQSuggestions()
		p.jqSugVisible = !p.jqSugVisible
		return nil, true

	case "up":
		if p.jqSugVisible && len(p.jqSugs) > 0 {
			p.jqSugSelected--
			if p.jqSugSelected < 0 {
				p.jqSugSelected = len(p.jqSugs) - 1
			}
			return nil, true
		}
		p.jqHistoryNavigate(-1)
		return nil, true

	case "down":
		if p.jqSugVisible && len(p.jqSugs) > 0 {
			p.jqSugSelected++
			if p.jqSugSelected >= len(p.jqSugs) {
				p.jqSugSelected = 0
			}
			return nil, true
		}
		p.jqHistoryNavigate(1)
		return nil, true

	case "ctrl+p":
		p.jqHistoryNavigate(-1)
		return nil, true

	case "ctrl+n":
		p.jqHistoryNavigate(1)
		return nil, true

	case "ctrl+u":
		p.jqInput = ""
		p.jqCursor = 0
		p.updateJQSuggestions()
		return nil, true

	case "backspace":
		if p.jqCursor > 0 {
			runes := []rune(p.jqInput)
			p.jqInput = string(runes[:p.jqCursor-1]) + string(runes[p.jqCursor:])
			p.jqCursor--
			p.updateJQSuggestions()
		}
		return nil, true

	case "delete":
		runes := []rune(p.jqInput)
		if p.jqCursor < len(runes) {
			p.jqInput = string(runes[:p.jqCursor]) + string(runes[p.jqCursor+1:])
			p.updateJQSuggestions()
		}
		return nil, true

	case "left":
		if p.jqCursor > 0 {
			p.jqCursor--
		}
		return nil, true

	case "right":
		runes := []rune(p.jqInput)
		if p.jqCursor < len(runes) {
			p.jqCursor++
		}
		return nil, true

	case "home", "ctrl+a":
		p.jqCursor = 0
		return nil, true

	case "end", "ctrl+e":
		p.jqCursor = len([]rune(p.jqInput))
		return nil, true

	default:
		if len(key) == 1 {
			ch := rune(key[0])
			if unicode.IsPrint(ch) {
				runes := []rune(p.jqInput)
				p.jqInput = string(runes[:p.jqCursor]) + key + string(runes[p.jqCursor:])
				p.jqCursor++
				p.updateJQSuggestions()
				return nil, true
			}
		}
	}

	return nil, false
}

// ── Autocomplete ──────────────────────────────────────────

func (p *GridPreview) updateJQSuggestions() {
	p.jqSugs = nil
	p.jqSugSelected = 0
	p.jqSugVisible = false

	if p.rawJSON == nil {
		return
	}

	var input interface{}
	if err := json.Unmarshal(p.rawJSON, &input); err != nil {
		return
	}

	input = normalizeJSONTypes(input)

	cursor := p.jqInput[:p.jqCursor]

	dotPos := strings.LastIndex(cursor, ".")
	if dotPos == -1 {
		p.jqSugs = p.rootSuggestions(input)
	} else {
		parentPath := cursor[:dotPos]
		prefix := cursor[dotPos+1:]
		parent := p.navigateJSON(input, parentPath)
		if parent != nil {
			p.jqSugs = p.childSuggestions(parent, prefix)
		}
	}

	if len(p.jqSugs) > maxJQSuggestions {
		p.jqSugs = p.jqSugs[:maxJQSuggestions]
	}

	if len(p.jqSugs) > 0 {
		p.jqSugVisible = true
		p.jqSugSelected = 0
	}
}

func (p *GridPreview) rootSuggestions(input interface{}) []JQSuggestion {
	switch v := input.(type) {
	case map[string]interface{}:
		var sugs []JQSuggestion
		for key, val := range v {
			sugs = append(sugs, JQSuggestion{
				Path: "." + key,
				Type: jsonType(val),
			})
		}
		return sugs
	case []interface{}:
		return []JQSuggestion{
			{Path: ".[]", Type: "array-iter"},
			{Path: ".[0]", Type: jsonType(indexArray(v, 0))},
		}
	}
	return nil
}

func (p *GridPreview) childSuggestions(parent interface{}, prefix string) []JQSuggestion {
	switch v := parent.(type) {
	case map[string]interface{}:
		var sugs []JQSuggestion
		for key, val := range v {
			if strings.HasPrefix(key, prefix) || prefix == "" {
				sugs = append(sugs, JQSuggestion{
					Path: key,
					Type: jsonType(val),
				})
			}
		}
		return sugs
	case []interface{}:
		var sugs []JQSuggestion
		if strings.HasPrefix("[]", prefix) {
			sugs = append(sugs, JQSuggestion{Path: "[]", Type: "array-iter"})
		}
		if strings.HasPrefix("0", prefix) || prefix == "" {
			sugs = append(sugs, JQSuggestion{Path: "[0]", Type: jsonType(indexArray(v, 0))})
		}
		if strings.HasPrefix("length", prefix) {
			sugs = append(sugs, JQSuggestion{Path: "length", Type: "number"})
		}
		return sugs
	}
	return nil
}

func (p *GridPreview) navigateJSON(input interface{}, path string) interface{} {
	if path == "" || path == "." {
		return input
	}

	path = strings.TrimPrefix(path, ".")
	parts := strings.Split(path, ".")
	current := input

	for _, part := range parts {
		if current == nil {
			return nil
		}

		if bracketIdx := strings.Index(part, "["); bracketIdx != -1 {
			key := part[:bracketIdx]
			rest := part[bracketIdx:]

			if key != "" {
				if m, ok := current.(map[string]interface{}); ok {
					current = m[key]
				} else {
					return nil
				}
			}

			for rest != "" {
				if !strings.HasPrefix(rest, "[") {
					return nil
				}
				end := strings.Index(rest, "]")
				if end == -1 {
					return nil
				}
				idxStr := rest[1:end]
				rest = rest[end+1:]

				if arr, ok := current.([]interface{}); ok {
					if idxStr == "" || idxStr == ":" {
						if len(arr) > 0 {
							current = arr[0]
						} else {
							return nil
						}
					} else {
						idx := 0
						_, _ = fmt.Sscanf(idxStr, "%d", &idx)
						if idx >= 0 && idx < len(arr) {
							current = arr[idx]
						} else {
							return nil
						}
					}
				} else {
					return nil
				}
			}
		} else {
			if m, ok := current.(map[string]interface{}); ok {
				current = m[part]
			} else if arr, ok := current.([]interface{}); ok {
				idx := 0
				_, _ = fmt.Sscanf(part, "%d", &idx)
				if idx >= 0 && idx < len(arr) {
					current = arr[idx]
				} else {
					return nil
				}
			} else {
				return nil
			}
		}
	}

	return current
}

func (p *GridPreview) acceptJQSuggestion() {
	if p.jqSugSelected < 0 || p.jqSugSelected >= len(p.jqSugs) {
		return
	}

	sug := p.jqSugs[p.jqSugSelected]

	runes := []rune(p.jqInput)
	left := string(runes[:p.jqCursor])
	right := string(runes[p.jqCursor:])

	dotPos := strings.LastIndex(left, ".")
	if dotPos == -1 {
		p.jqInput = left + sug.Path + right
		p.jqCursor += len([]rune(sug.Path))
	} else {
		prefix := left[:dotPos+1]
		p.jqInput = prefix + sug.Path + right
		p.jqCursor = len([]rune(prefix + sug.Path))
	}

	p.jqSugVisible = false
	p.updateJQSuggestions()
}

// ── History ──────────────────────────────────────────

func (p *GridPreview) jqHistoryNavigate(delta int) {
	if len(p.jqHistory) == 0 {
		return
	}

	newIdx := p.jqHistoryIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx > len(p.jqHistory) {
		newIdx = len(p.jqHistory)
	}

	p.jqHistoryIdx = newIdx

	if newIdx == len(p.jqHistory) {
		p.jqInput = ""
	} else {
		p.jqInput = p.jqHistory[newIdx]
	}
	p.jqCursor = len([]rune(p.jqInput))
	p.updateJQSuggestions()
}

func (p *GridPreview) addToHistory(expr string) {
	for i, h := range p.jqHistory {
		if h == expr {
			p.jqHistory = append(p.jqHistory[:i], p.jqHistory[i+1:]...)
			break
		}
	}

	p.jqHistory = append(p.jqHistory, expr)
	if len(p.jqHistory) > maxJQHistory {
		p.jqHistory = p.jqHistory[len(p.jqHistory)-maxJQHistory:]
	}

	p.saveJQHistory()
}

func (p *GridPreview) historyFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "dbx", "jq_history.json")
}

func (p *GridPreview) loadJQHistory() {
	path := p.historyFilePath()
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var history []string
	if err := json.Unmarshal(data, &history); err != nil {
		return
	}

	p.jqHistory = history
}

func (p *GridPreview) saveJQHistory() {
	path := p.historyFilePath()
	if path == "" {
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	data, err := json.MarshalIndent(p.jqHistory, "", "  ")
	if err != nil {
		return
	}

	// Best-effort persistence: the in-memory history is already updated.
	_ = os.WriteFile(path, data, 0o644)
}

// ── Scrolling ──────────────────────────────────────────

func (p *GridPreview) halfPageUp() {
	p.scrollY -= p.height / 2
	if p.scrollY < 0 {
		p.scrollY = 0
	}
}

func (p *GridPreview) halfPageDown() {
	p.scrollY += p.height / 2
	maxScroll := len(p.lines) - p.height + 6
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollY > maxScroll {
		p.scrollY = maxScroll
	}
}

// ── Update ──────────────────────────────────────────

func (p *GridPreview) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !p.focused {
		return nil, false
	}

	if p.jqMode {
		return p.handleJQInput(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()

		if p.keybinds == nil {
			return nil, false
		}
		action, ok := p.keybinds.Resolve(key, config.ContextGridPreview)
		if !ok {
			return nil, false
		}
		return p.dispatchAction(action)
	}

	return nil, false
}

// HandleAction dispatches an action ID without synthesizing a key press, so
// callers (palette commands, mouse) respect rebinding. It skips Update's
// focused guard but keeps the JQ-mode guard: while typing a filter the preview
// owns the keys.
func (p *GridPreview) HandleAction(id config.ActionID) (tea.Cmd, bool) {
	if p.jqMode {
		return nil, false
	}
	return p.dispatchAction(id)
}

// dispatchAction is the shared action switch for key and action-ID dispatch.
func (p *GridPreview) dispatchAction(action config.ActionID) (tea.Cmd, bool) {
	switch action {
	case "navigate_up":
		p.cursorUp()
		return nil, true
	case "navigate_down":
		p.cursorDown()
		return nil, true
	case "go_first":
		p.cursorLine = 0
		p.ensureCursorVisible()
		return nil, true
	case "go_last":
		if len(p.lines) > 0 {
			p.cursorLine = len(p.lines) - 1
		}
		p.ensureCursorVisible()
		return nil, true
	case "half_page_up":
		p.halfPageUp()
		return nil, true
	case "half_page_down":
		p.halfPageDown()
		return nil, true
	case "jq_filter":
		p.EnterJQMode()
		return nil, true
	case "expand_fk":
		return p.handleExpand()
	}

	return nil, false
}

// ── Render ──────────────────────────────────────────

func (p *GridPreview) Render() string {
	if p.width <= 0 {
		return ""
	}

	contentHeight := p.height - 4
	if p.jqMode {
		contentHeight -= 2
	} else if p.jqExpr != "" {
		contentHeight--
	}

	if len(p.lines) == 0 {
		if p.jqMode {
			return p.renderJQPrompt() + "\n" + p.styles.Text.Render("  No data")
		}
		return p.styles.Text.Render("  No data")
	}

	start := p.scrollY
	end := start + contentHeight
	if end > len(p.lines) {
		end = len(p.lines)
	}

	var rendered []string
	var tracker fkPathTracker
	for i, line := range p.lines[start:end] {
		lineIdx := start + i
		highlighted := p.highlightJSON(line)

		key := p.parseKeyFromLine(line)
		indent := countIndent(line)

		var currentPath string
		if key != "" {
			currentPath = tracker.onKey(key, indent)
		} else if strings.Contains(line, "}") || strings.Contains(line, "]") {
			tracker.onClose(indent)
		}

		if currentPath != "" {
			if fkInfo := p.isExpandableFK(currentPath); fkInfo != nil {
				highlighted += p.styles.Help.Render(" →")
			} else if _, expanded := p.expandedFKs[currentPath]; expanded {
				highlighted += p.styles.Help.Render(" ↓")
			}
		}

		if lineIdx == p.cursorLine {
			highlighted = p.styles.Selected.Width(p.width - 4).Render(highlighted)
		}

		rendered = append(rendered, highlighted)
	}

	content := strings.Join(rendered, "\n")

	var jqBar string
	if p.jqMode {
		jqBar = p.renderJQPrompt() + "\n"
	} else if p.jqExpr != "" {
		jqBar = p.styles.Help.Render(fmt.Sprintf("  jq: %s", p.jqExpr)) + "\n"
	}

	return jqBar + content
}

func (p *GridPreview) renderJQPrompt() string {
	prefix := p.styles.Help.Render(" jq │ ")

	runes := []rune(p.jqInput)
	left := string(runes[:p.jqCursor])
	cursorChar := "█"
	right := string(runes[p.jqCursor:])

	inputText := p.styles.Text.Render(left) +
		p.styles.Primary.Render(cursorChar) +
		p.styles.Text.Render(right)

	bar := prefix + inputText

	barWidth := lipgloss.Width(bar)
	if barWidth < p.width-4 {
		bar += strings.Repeat(" ", p.width-4-barWidth)
	}

	if p.jqSugVisible && len(p.jqSugs) > 0 {
		bar += "\n" + p.renderJQSuggestions()
	}

	return bar
}

func (p *GridPreview) renderJQSuggestions() string {
	maxWidth := p.width - 8
	if maxWidth < 30 {
		maxWidth = 30
	}

	pathWidth := maxWidth - 14
	if pathWidth < 15 {
		pathWidth = 15
	}

	var lines []string
	for i, sug := range p.jqSugs {
		isSelected := i == p.jqSugSelected

		path := sug.Path
		if lipgloss.Width(path) > pathWidth {
			path = ansi.Truncate(path, pathWidth, "...")
		}
		pathPad := pathWidth - lipgloss.Width(path)
		if pathPad > 0 {
			path += strings.Repeat(" ", pathPad)
		}

		typeStr := p.styles.TextMuted.Render(sug.Type)
		typePad := 14 - lipgloss.Width(typeStr)
		if typePad > 0 {
			typeStr += strings.Repeat(" ", typePad)
		}

		line := "    " + p.styles.Text.Render(path) + " " + typeStr

		if isSelected {
			line = p.styles.Selected.Width(maxWidth).Render(line)
		}

		lines = append(lines, line)
	}

	popup := strings.Join(lines, "\n")
	return p.styles.BorderActive.Width(maxWidth).Render(popup)
}

// ── JSON Highlighting ──────────────────────────────────

func (p *GridPreview) highlightJSON(line string) string {
	var result strings.Builder
	i := 0

	for i < len(line) {
		ch := line[i]

		if ch == '"' {
			end := strings.IndexByte(line[i+1:], '"')
			if end == -1 {
				result.WriteString(p.styles.Text.Render(line[i:]))
				break
			}
			end += i + 1

			rest := strings.TrimSpace(line[end+1:])
			if len(rest) > 0 && rest[0] == ':' {
				result.WriteString(p.styles.Primary.Render(line[i : end+1]))
			} else {
				result.WriteString(p.styles.Success.Render(line[i : end+1]))
			}
			i = end + 1
			continue
		}

		if ch >= '0' && ch <= '9' || ch == '-' {
			j := i + 1
			for j < len(line) && (line[j] >= '0' && line[j] <= '9' || line[j] == '.' || line[j] == '-' || line[j] == 'e' || line[j] == 'E' || line[j] == '+' || line[j] == 't' || line[j] == 'Z') {
				j++
			}
			result.WriteString(p.styles.Info.Render(line[i:j]))
			i = j
			continue
		}

		if strings.HasPrefix(line[i:], "null") {
			result.WriteString(p.styles.TextMuted.Render("null"))
			i += 4
			continue
		}

		if strings.HasPrefix(line[i:], "true") {
			result.WriteString(p.styles.Info.Render("true"))
			i += 4
			continue
		}

		if strings.HasPrefix(line[i:], "false") {
			result.WriteString(p.styles.Info.Render("false"))
			i += 5
			continue
		}

		result.WriteByte(ch)
		i++
	}

	return result.String()
}

// ── Helpers ──────────────────────────────────────────

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else {
			break
		}
	}
	return count
}

// fkPathTracker builds dotted paths by tracking JSON nesting via indentation.
type fkPathTracker struct {
	stack []string
	depth []int
}

func (t *fkPathTracker) onKey(key string, indent int) string {
	for len(t.depth) > 0 && indent <= t.depth[len(t.depth)-1] {
		t.stack = t.stack[:len(t.stack)-1]
		t.depth = t.depth[:len(t.depth)-1]
	}
	t.stack = append(t.stack, key)
	t.depth = append(t.depth, indent)
	return strings.Join(t.stack, ".")
}

func (t *fkPathTracker) onClose(indent int) {
	for len(t.depth) > 0 && indent <= t.depth[len(t.depth)-1] {
		t.stack = t.stack[:len(t.stack)-1]
		t.depth = t.depth[:len(t.depth)-1]
	}
}

func (t *fkPathTracker) currentPath() string {
	return strings.Join(t.stack, ".")
}

func jsonType(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "unknown"
	}
}

func indexArray(arr []interface{}, idx int) interface{} {
	if idx >= 0 && idx < len(arr) {
		return arr[idx]
	}
	return nil
}

func normalizeJSONTypes(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeJSONTypes(v2)
		}
		return val
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeJSONTypes(v2)
		}
		return val
	default:
		return v
	}
}
