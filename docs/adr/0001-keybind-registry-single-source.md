# ADR 0001 — Keybind registry as the single source of truth

- **Status**: Accepted
- **Date**: 2026-09-23
- **Context**: refactor `keybinds-single-source`

## Context

Keybind information lived in four parallel sources that drifted apart:

1. **Duplicated registry** — `DefaultKeybindings()` (primary key only) and
   `defaultBindings()` (all keys) in `internal/config/keybindings.go` encoded the
   same information twice.
2. **Hardcoded display** — `renderActions()`/`renderContextual()` (statusbar),
   `modal.go`, `palette/commands.go`, and the tables in `README.md` /
   `docs/KEYBINDS.md` repeated keys, labels and order by hand.
3. **App dispatch** — `app.go` compared `m.keybinds["<action>"]` directly and
   dispatched with a hardcoded `switch` in `handlePaletteCommand`;
   `KeybindRegistry.Match()` was only used by tests.
4. **Component dispatch** — `grid`, `explorer`, `gridpreview` and
   `explorerpreview` matched **raw key strings** (`case "j", "down":`,
   `case "esc":`), using the registry map only partially.

Adding or moving a key meant touching up to eight places. The `inContext()`
"global fallback" made global actions fire in views where the key meant
something else (e.g. `q` as quit vs. a literal character in the editor), hiding
collisions.

## Decision

Model the keybinds as **one flat slice of `Action`** in `internal/config`
(Model A), and derive both **display and dispatch** from it.

```go
type Action struct {
    ID          ActionID // semantic, view-agnostic (e.g. navigate_down)
    Keys        []string // primary + aliases
    Section     string   // display grouping
    Description string   // panel/modal label
    Contexts    []string // views where it applies
    Owner       string   // module that dispatches it
    Pending     bool     // declared but no TUI handler yet
}
```

The registry exposes a minimal read interface (`Resolver`: `Resolve`,
`PrimaryKey`, `KeysFor`, `ActionsFor`, `All`) that components and the app depend
on, so display and dispatch cannot diverge. The concept of a "global" keybind is
removed: every action lists the contexts it applies to, and `Resolve(key,
context)` has no fallback. Custom overrides (`KeybindingsConfig.Custom`, keyed
by action ID) replace all keys of an action, so both surfaces change together.

## Alternatives considered

- **Model B — the view declares its actions (`map[view][]actionID`).** Better
  ergonomics when adding a whole view, but reintroduces a second structure that
  can desynchronize (exactly the problem being removed), duplicates shared IDs
  per view, and makes collision detection require joins. Rejected.
- **Keep the map plus a separate display table.** Rejected: that is the status
  quo with extra steps.
- **Promote widget-mode keys (EDIT/FILTER/WHERE, picker, query browser) into the
  registry.** Rejected: they are interaction *inside* a focused widget, not
  configurable app actions; promoting them would bloat the registry and couple
  edit modes to the model. They are documented as static prose in the help
  overlay instead.

## Consequences

- Single edit point for a keybind; the rendered panel, the `?` overlay, the
  palette and execution all come from the same entry.
- Constructor contracts change: `grid`, `gridpreview`, `explorerpreview`
  (+ tab bars), `explorer`, `palette`, `HelpModal`, `KeybindsPane`, and `Router`
  take a `config.Resolver` instead of `map[string]string`.
- `StatusBar` is renamed to **`KeybindsPane`** (file `keybindspane.go`).
- `DefaultKeybindings()`, `defaultBindings()`, `Match()`, `Flatten()` and the
  `global.*` action IDs are deleted. Action IDs are now view-agnostic
  (`rollback`, `copy_sql`, `navigate_down`, …).
- Actions gain `Owner`/`Pending`, enabling two regression tests:
  **no key collisions within a view** and **every non-pending action has a
  handler** (centralized in `internal/app`).
- The TUI is the living reference: `docs/KEYBINDS.md` is deleted; the README
  keeps only a "press `?`" pointer, and non-keybind prose moves to
  `docs/FEATURES.md`.
- `ask` (`a`) is preserved with `Pending: true` because its TUI handler is out
  of scope (the CLI flow in `internal/cli/ask.go` remains).
