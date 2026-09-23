package config

// KeybindingsConfig holds user keybind overrides, keyed by action ID. An
// override replaces every key of the action.
type KeybindingsConfig struct {
	Custom map[string]string `mapstructure:"custom"`
}

// View contexts. These are the keys of Action.Contexts and the values the// router exposes through Router.Context(). An action is active only in the
// contexts it declares; there is no "global" fallback.
const (
	ContextExplorer        = "explorer"
	ContextGrid            = "grid"
	ContextGridPreview     = "grid-preview"
	ContextExplorerPreview = "explorer-preview"
	ContextEditor          = "editor"
	ContextQueryBrowser    = "query-browser"
	ContextPicker          = "picker"
)

// ActionID identifies a keybind action. IDs are semantic and view-agnostic:
// the views where an action applies live in Contexts, never in the ID.
type ActionID string

// Action is the single source of truth for one keybind: the keys that trigger
// it plus the metadata used to render it (panel, help modal, palette) and to
// validate the registry.
type Action struct {
	ID          ActionID
	Keys        []string
	Section     string
	Description string
	Contexts    []string
	Owner       string
	Pending     bool
}

// Resolver is the minimal read interface consumers depend on. Components take
// this instead of a raw map so display and dispatch always derive from the
// same registry.
type Resolver interface {
	Resolve(key, context string) (ActionID, bool)
	PrimaryKey(id ActionID) string
	KeysFor(id ActionID) []string
	ActionsFor(context string) []Action
	All() []Action
}

type KeybindRegistry struct {
	actions []Action
}

func NewKeybindRegistry(cfg KeybindingsConfig) *KeybindRegistry {
	r := &KeybindRegistry{
		actions: append([]Action(nil), defaultActions()...),
	}

	for id, key := range cfg.Custom {
		aid := ActionID(id)
		if i := r.indexOf(aid); i >= 0 {
			// The override replaces every key of the action.
			r.actions[i].Keys = []string{key}
			continue
		}
		// Preserve the legacy ability to bind an arbitrary action id.
		r.actions = append(r.actions, Action{ID: aid, Keys: []string{key}})
	}

	return r
}

func (r *KeybindRegistry) indexOf(id ActionID) int {
	for i := range r.actions {
		if r.actions[i].ID == id {
			return i
		}
	}
	return -1
}

// All returns a copy of every declared action.
func (r *KeybindRegistry) All() []Action {
	out := make([]Action, len(r.actions))
	copy(out, r.actions)
	return out
}

// Resolve maps a key press to the action that owns it in the given context.
func (r *KeybindRegistry) Resolve(key, context string) (ActionID, bool) {
	if key == "" {
		return "", false
	}
	for _, a := range r.actions {
		if !actionInContext(a, context) {
			continue
		}
		for _, k := range a.Keys {
			if k == key {
				return a.ID, true
			}
		}
	}
	return "", false
}

// PrimaryKey returns the first key of an action, or "" when it has none.
func (r *KeybindRegistry) PrimaryKey(id ActionID) string {
	if i := r.indexOf(id); i >= 0 && len(r.actions[i].Keys) > 0 {
		return r.actions[i].Keys[0]
	}
	return ""
}

// KeysFor returns all keys bound to an action.
func (r *KeybindRegistry) KeysFor(id ActionID) []string {
	if i := r.indexOf(id); i >= 0 {
		return append([]string(nil), r.actions[i].Keys...)
	}
	return nil
}

// ActionsFor returns the actions active in a context that have at least one
// key (palette-only actions are reachable through the palette, not the panel).
func (r *KeybindRegistry) ActionsFor(context string) []Action {
	var out []Action
	for _, a := range r.actions {
		if len(a.Keys) == 0 || !actionInContext(a, context) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func actionInContext(a Action, context string) bool {
	for _, c := range a.Contexts {
		if c == context {
			return true
		}
	}
	return false
}
