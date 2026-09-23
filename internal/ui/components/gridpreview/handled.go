package gridpreview

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the grid preview dispatches itself.
func (p *GridPreview) HandledActions() []config.ActionID {
	return []config.ActionID{
		"navigate_up", "navigate_down", "go_first", "go_last",
		"half_page_up", "half_page_down", "jq_filter", "expand_fk",
	}
}
