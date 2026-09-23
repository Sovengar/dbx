# ADR 0002 — Keybind display groups and the widget-local boundary

- **Status**: Accepted
- **Date**: 2026-09-23
- **Context**: refactor `keybind-groups-and-audit`
- **Builds on**: `docs/adr/0001-keybind-registry-single-source.md`

## Context

ADR 0001 made the keybind registry (`defaultActions()`) the single source of truth and
derived display and dispatch from it. Two gaps remained:

1. **The pane wasted space**: `KeybindsPane.renderLines()` emitted one segment per
   action, so sibling actions (`navigate_down/up/left/right`, `go_first/go_last`,
   `half_page_up/down`, `goto_page_1..9`) filled the bar with near-duplicate text.
2. **Raw-key bypasses survived**: the editor and explorer matched shortcut keys against
   raw strings, the mouse wheel/double-click injected raw `j`/`k`/`enter` into
   components, the help modal closed on raw `?`/`q`/`esc`, and the grid accepted a raw
   `0-9` multi-digit page jump while the registry advertised `f1-f9` for `goto_page_N`.

Both gaps let display and dispatch drift, which is exactly what ADR 0001 set out to
prevent.

## Decision

### 1. Display groups as registry metadata, aggregated in `config`

Add `Group` and `GroupLabel` to `Action` (static inventory data, like `Section`/`Owner`).
A **pure aggregator in `internal/config`** turns the actions active in a context into
display groups (combined primary keys, label, member IDs). The pane **and** the help
modal both call it, so grouping logic exists once and cannot diverge.

- The pane shows **primary keys only**; the modal shows **all keys** (aliases included).
- Primary keys are joined at render time: runes concatenate (`hjkl`), a contiguous digit
  range with a common prefix compresses (`f1-f9`), otherwise `/` (`g/G`, `ctrl+u/ctrl+d`).
  Nothing is stored, so overrides stay consistent.
- A group with fewer than two active members degrades to a plain action segment.
- `maxKeybindsPerLine` counts segments (groups), not keys.
- Grouping is derived from `ActionsFor(context)`; **no new `Resolver` method** is added.

### 2. Widget-local interaction stays out of the registry

Keys that are interaction *inside* a focused widget rather than configurable app actions
are **not** promoted into the registry:

- Editor/body text entry and mode keys (EDIT/FILTER/WHERE) — unchanged from ADR 0001.
- The help modal's close/scroll keys (`?`/`q`/`esc`, `j/k/g/G/ctrl+u/ctrl+d`) are a
  **documented overlay exception**; no `modal` context is introduced.
- The grid's `0-9` multi-digit page jump is **removed** (see below).

### 3. Bypasses dispatch through resolved action IDs

The editor gains a `Resolver` and dispatches editor-owned actions through it; the dead
raw-key switch in `explorer/tree.go` is deleted. Mouse wheel and double-click map to
semantic action IDs (`navigate_up`/`navigate_down`/`edit_cell`) and dispatch through the
same handlers — a mouse event is not a key, so no `KeyPressMsg` is synthesized.

### 4. Grid digit page-jump removed

The `0-9` handler and `pendingDigits` are removed; pagination is F-keys only. This is an
**intentional behavior change**: the handler was reachable, and typing a digit in the
grid becomes a no-op. It was preferred over registering `0-9` because a stateful
multi-digit prefix cannot be expressed as a static single-key registry entry and would
make display and dispatch ambiguous.

## Alternatives considered

- **Group label derived from the first member's `Description`** — produces wrong labels
  (`Navigate Down` instead of `Navigate`, `Go to Page 1` instead of `Go to Page`).
  Rejected; an explicit `GroupLabel` with an equality test is honest.
- **Group label in a separate map in the registry package** — a second place to edit per
  group, i.e. a hidden parallel list. Rejected.
- **Store a precomputed group-key string** — goes stale on rebind. Rejected; compute per
  render.
- **Grouping logic duplicated in the pane and the modal** — the parallel-list smell ADR
  0001 forbids. Rejected in favour of one `config` aggregator.
- **Add a `Resolver.Grouped(context)` method** — unnecessary interface churn; grouping is
  derivable from `ActionsFor`. Rejected.
- **Introduce a `modal`/overlay context** — would require routing ahead of
  `Router.Context` and would promote widget interaction into the registry, re-opening
  what ADR 0001 rejected. Rejected; documented exception instead.
- **Synthesize `KeyPressMsg` for mouse events** — a second dispatch path that bypasses
  `Resolve` and breaks under rebinding. Rejected in favour of action-ID dispatch.
- **Register `0-9` as keys of `goto_page_N`** — breaks multi-digit entry (the resolver
  matches the first digit) and cannot represent `goto_page_10+`. Rejected; feature
  removed instead.
- **Keep digit jump as a documented widget-local mode** — considered, but the user chose
  removal for a simpler, unambiguous model.

## Consequences

- The pane is denser and self-describing; aliases remain discoverable in the `?` modal.
- One grouping implementation serves both surfaces; the registry stays the single source.
- Constructor contracts are unchanged (`Group`/`GroupLabel` are additive), so the blast
  radius is limited to the pane, the modal, and the specific Feature 2 sites.
- Pane tests that counted key segments must be updated to count segments; new tests cover
  grouping, partial groups, primary-only pane, alias-bearing modal, and the Feature 2 fixes.
- Removing the grid digit jump is a user-visible behavior change and is recorded here and
  in the commit.
