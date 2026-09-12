package app

import "github.com/buble/dbx/internal/config"

type Router struct {
	focus       FocusPane
	panes       []FocusPane
	keybindings map[string]string
}

func NewRouter(kb map[string]string) *Router {
	return &Router{
		focus:       FocusExplorer,
		panes:       []FocusPane{FocusExplorer, FocusGrid},
		keybindings: kb,
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
	}
	return r.focus
}

func (r *Router) Context() string {
	return r.focus.String()
}

func (r *Router) KeyFor(action string) string {
	if k := r.keybindings[action]; k != "" {
		return k
	}
	return config.DefaultKeybindings()[action]
}
