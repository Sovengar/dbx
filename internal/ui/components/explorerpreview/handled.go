package explorerpreview

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the explorer preview dispatches itself.
func (e *ExplorerPreview) HandledActions() []config.ActionID {
	return []config.ActionID{
		"overview_tab", "columns_tab", "constraints_tab",
		"foreign_keys_tab", "indexes_tab", "ere_tab",
	}
}
