package editor

import (
	"testing"

	aiContext "github.com/buble/dbx/internal/ai/context"
)

func testExport() *aiContext.SchemaExport {
	return &aiContext.SchemaExport{
		Database: "test",
		Schemas: []aiContext.SchemaInfo{
			{
				Name: "public",
				Tables: []aiContext.TableInfo{
					{
						Name: "users",
						Columns: []aiContext.ColumnInfo{
							{Name: "id", DataType: "integer"},
							{Name: "name", DataType: "text"},
						},
					},
					{
						Name: "orders",
						Columns: []aiContext.ColumnInfo{
							{Name: "id", DataType: "integer"},
							{Name: "user_id", DataType: "integer"},
							{Name: "total", DataType: "numeric"},
						},
					},
				},
			},
			{
				Name: "analytics",
				Tables: []aiContext.TableInfo{
					{
						Name: "events",
						Columns: []aiContext.ColumnInfo{
							{Name: "id", DataType: "integer"},
							{Name: "wh_start", DataType: "timestamp"},
						},
					},
				},
			},
		},
	}
}

func newTestAutocomplete() *AutocompleteState {
	a := NewAutocompleteState()
	a.LoadSchema(testExport())
	a.SetKeywords(sqlKeywords, sqlFunctions)
	return a
}

func suggestionNames(a *AutocompleteState) []string {
	names := make([]string, len(a.filtered))
	for i, item := range a.filtered {
		names[i] = item.Name
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func findSuggestion(a *AutocompleteState, name string) *CompletionItem {
	for i := range a.filtered {
		if a.filtered[i].Name == name {
			item := a.filtered[i]
			return &item
		}
	}
	return nil
}

func TestDetectContext(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantKind    CompletionKind
		wantVisible bool
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "select star does not suggest star",
			line:        "SELECT *",
			wantKind:    CompletionSelectList,
			wantVisible: true,
			wantContain: "COUNT",
			wantAbsent:  "*",
		},
		{
			name:        "empty select list offers star",
			line:        "SELECT ",
			wantKind:    CompletionSelectList,
			wantVisible: true,
			wantContain: "*",
		},
		{
			name:        "from single letter offers tables",
			line:        "SELECT * FROM u",
			wantKind:    CompletionTable,
			wantVisible: true,
			wantContain: "users",
		},
		{
			name:        "from clause offers tables",
			line:        "SELECT * FROM ",
			wantKind:    CompletionTable,
			wantVisible: true,
			wantContain: "users",
			wantAbsent:  "*",
		},
		{
			name:        "partial keyword suggests WHERE",
			line:        "SELECT * FROM users WH",
			wantKind:    CompletionKeyword,
			wantVisible: true,
			wantContain: "WHERE",
		},
		{
			name:        "exact keyword is not suggested",
			line:        "SELECT * FROM users WHERE",
			wantKind:    CompletionKeyword,
			wantVisible: false,
		},
		{
			name:        "where prefix offers columns",
			line:        "SELECT * FROM users WHERE na",
			wantKind:    CompletionColumn,
			wantVisible: true,
			wantContain: "name",
		},
		{
			name:        "select list keyword continuation",
			line:        "SELECT * FR",
			wantKind:    CompletionSelectList,
			wantVisible: true,
			wantContain: "FROM",
		},
		{
			name:        "order by keyword continuation",
			line:        "SELECT * FROM users ORDER BY id LI",
			wantKind:    CompletionColumn,
			wantVisible: true,
			wantContain: "LIMIT",
		},
		{
			name:        "alias dot offers columns",
			line:        "SELECT * FROM users u WHERE u.",
			wantKind:    CompletionColumn,
			wantVisible: true,
			wantContain: "id",
		},
		{
			name:        "schema dot offers tables",
			line:        "SELECT * FROM public.",
			wantKind:    CompletionTable,
			wantVisible: true,
			wantContain: "users",
		},
		{
			name:        "unterminated string suppresses",
			line:        "SELECT * FROM users WHERE name = 'ab",
			wantKind:    CompletionEmpty,
			wantVisible: false,
		},
		{
			name:        "line comment suppresses",
			line:        "SELECT 1 -- note",
			wantKind:    CompletionEmpty,
			wantVisible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAutocomplete()
			ctx := a.detectContext(tt.line, len(tt.line))
			a.UpdateContext(ctx)

			if ctx.kind != tt.wantKind {
				t.Fatalf("kind = %d, want %d", ctx.kind, tt.wantKind)
			}
			if a.Visible() != tt.wantVisible {
				t.Fatalf("visible = %v, want %v (filtered=%v)", a.Visible(), tt.wantVisible, suggestionNames(a))
			}
			names := suggestionNames(a)
			if tt.wantContain != "" && !hasName(names, tt.wantContain) {
				t.Errorf("expected %q in suggestions %v", tt.wantContain, names)
			}
			if tt.wantAbsent != "" && hasName(names, tt.wantAbsent) {
				t.Errorf("did not expect %q in suggestions %v", tt.wantAbsent, names)
			}
		})
	}
}

func TestFromKeepsTablesAheadOfKeywordFallback(t *testing.T) {
	a := newTestAutocomplete()
	a.UpdateContext(a.detectContext("SELECT * FROM u", len("SELECT * FROM u")))

	if len(a.filtered) == 0 {
		t.Fatal("expected suggestions")
	}
	if a.filtered[0].Name != "users" {
		t.Fatalf("first suggestion = %q, want users (filtered=%v)", a.filtered[0].Name, suggestionNames(a))
	}
	if !hasName(suggestionNames(a), "UNION") {
		t.Errorf("expected keyword fallback to include UNION, got %v", suggestionNames(a))
	}
}

func TestAcceptCompletion_ReplacesToken(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetContent("SELECT * FROM users WH")
	ed.SetCursorPos(0, len("SELECT * FROM users WH"))
	ed.triggerAutocomplete()

	item := findSuggestion(ed.autocomplete, "WHERE")
	if item == nil {
		t.Fatalf("WHERE not suggested, got %v", suggestionNames(ed.autocomplete))
	}
	ed.acceptCompletion(item)

	if got, want := ed.Content(), "SELECT * FROM users WHERE "; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestCompleteKeywordIsNotSuggested(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetContent("SELECT * FROM users WHERE")
	ed.SetCursorPos(0, len("SELECT * FROM users WHERE"))
	ed.triggerAutocomplete()

	if ed.AutocompleteVisible() {
		t.Fatalf("complete keyword should not be suggested, got %v", suggestionNames(ed.autocomplete))
	}
}

func TestAutocompleteConfigDisables(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetAutocompleteConfig(false, 1)
	ed.SetContent("SELECT * FROM u")
	ed.SetCursorPos(0, len("SELECT * FROM u"))
	ed.updateAutocompleteAfterEdit()

	if ed.AutocompleteVisible() {
		t.Fatalf("autocomplete should be disabled, got %v", suggestionNames(ed.autocomplete))
	}
}

func TestAutocompleteMinPrefix(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetAutocompleteConfig(true, 3)
	ed.SetContent("SELECT * FROM users WH")
	ed.SetCursorPos(0, len("SELECT * FROM users WH"))
	ed.updateAutocompleteAfterEdit()

	if ed.AutocompleteVisible() {
		t.Fatalf("token shorter than min should be hidden, got %v", suggestionNames(ed.autocomplete))
	}

	ed.triggerAutocomplete()
	if !ed.AutocompleteVisible() {
		t.Fatal("manual trigger should ignore the minimum prefix length")
	}
}

func TestTokenizeSQLByteOffsets(t *testing.T) {
	line := "SELECT id FROM users"
	tokens := tokenizeSQL(line)
	if len(tokens) == 0 {
		t.Fatal("expected tokens")
	}
	for _, token := range tokens {
		if token.start < 0 || token.end > len(line) || token.start > token.end {
			t.Fatalf("token %q has invalid offsets [%d,%d] for %q", token.text, token.start, token.end, line)
		}
		if got := line[token.start:token.end]; got != token.text {
			t.Fatalf("token offsets [%d,%d] yield %q, want %q", token.start, token.end, got, token.text)
		}
	}
}
