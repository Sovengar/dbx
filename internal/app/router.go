package app

import "github.com/buble/dbx/internal/config"

type Router struct {
	focus    FocusPane
	panes    []FocusPane
	keybinds config.Resolver
}

func NewRouter(kb config.Resolver) *Router {
	return &Router{
		focus:    FocusExplorer,
		panes:    []FocusPane{FocusExplorer, FocusGrid},
		keybinds: kb,
	}
}

func (r *Router) Focus() FocusPane {
	return r.focus
}

func (r *Router) FocusPane(p FocusPane) {
	r.focus = p
}

func (r *Router) CycleFocus() FocusPane {
	for i, p := range r.panes {
		if p == r.focus {
			r.focus = r.panes[(i+1)%len(r.panes)]
			break
		}
	}
	return r.focus
}

func (r *Router) FocusByName(name string) FocusPane {
	switch name {
	case "explorer":
		r.focus = FocusExplorer
	case "grid":
		r.focus = FocusGrid
	case "grid-preview":
		r.focus = FocusGridPreview
	case "explorer-preview":
		r.focus = FocusExplorerPreview
	}
	return r.focus
}

func (r *Router) Context() string {
	return r.focus.String()
}

// KeyFor returns the primary key bound to an action, or "" when unbound.
func (r *Router) KeyFor(id config.ActionID) string {
	if r.keybinds == nil {
		return ""
	}
	return r.keybinds.PrimaryKey(id)
}
