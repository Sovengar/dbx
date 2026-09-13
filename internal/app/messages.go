package app

import "github.com/buble/dbx/internal/drivers/postgres"

type FocusPane int

const (
	FocusExplorer FocusPane = iota
	FocusGrid
	FocusEditor
	FocusGridPreview
	FocusExplorerPreview
)

func (f FocusPane) String() string {
	switch f {
	case FocusExplorer:
		return "explorer"
	case FocusGrid:
		return "grid"
	case FocusEditor:
		return "editor"
	case FocusGridPreview:
		return "grid-preview"
	case FocusExplorerPreview:
		return "explorer-preview"
	default:
		return "unknown"
	}
}

type (
	FocusChangedMsg struct {
		Pane FocusPane
	}
)

type metadataLoadedMsg struct {
	schema      string
	table       string
	constraints []constraintInfo
	foreignKeys []foreignKeyInfo
	indexes     []indexInfo
	overview    *postgres.TableOverview
	err         error
}

type constraintInfo struct {
	Name    string
	Type    string
	Columns string
}

type foreignKeyInfo struct {
	Name      string
	Column    string
	RefSchema string
	RefTable  string
	RefColumn string
}

type indexInfo struct {
	Name    string
	Columns string
	Unique  bool
}

type GridSidebarFKPreviewLookupResultMsg struct {
	Columns  []string
	Row      []interface{}
	CacheKey string
	CacheVal interface{}
	RefTable string
	Token    int
	Err      error
}

type NavigationEntry struct {
	Schema    string
	Table     string
	Where     string
	CursorRow int
	CursorCol int
	ScrollCol int
}

type explorerPreviewDataMsg struct {
	schema      string
	table       string
	columns     []postgres.ColumnInfo
	constraints []postgres.ConstraintInfo
	foreignKeys []postgres.ForeignKeyInfo
	indexes     []postgres.IndexInfo
	overview    *postgres.TableOverview
}
