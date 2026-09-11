package app

type FocusPane int

const (
	FocusExplorer FocusPane = iota
	FocusGrid
	FocusEditor
)

func (f FocusPane) String() string {
	switch f {
	case FocusExplorer:
		return "explorer"
	case FocusGrid:
		return "grid"
	case FocusEditor:
		return "editor"
	default:
		return "unknown"
	}
}

type (
	FocusChangedMsg struct {
		Pane FocusPane
	}
)
