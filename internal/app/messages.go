package app

type FocusPane int

const (
	FocusExplorer FocusPane = iota
	FocusGrid
	FocusEditor
	FocusGridPreview
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

type NavigationEntry struct {
	Schema    string
	Table     string
	Where     string
	CursorRow int
	CursorCol int
	ScrollCol int
}
