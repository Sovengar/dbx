# Plan — Keybind grouping and registry hardening

- **Slug**: `keybind-groups-and-audit`
- **Tipo**: refactor
- **Branch**: `refactor/keybind-registry-hardening`
- **Base**: `main` @ `af111b8`
- **adr_required**: `true` — grouping aggregation boundary (config vs UI duplication) and the
  decision NOT to promote widget-local/modal/digit keys into the registry, plus an additive
  `Action` data-contract change. ADR: `docs/adr/0002-keybind-groups-and-widget-local-boundary.md`.

## Outcome

1. **Grouping in the pane**: sibling actions collapse into one segment with their
   combined **primary** keys (`hjkl Navigate`, `g/G First/Last`, `ctrl+u/ctrl+d Half
   Page`, `f1-f9 Go to Page`). The pane never shows aliases.
2. **Help modal** `?` shows **all** keys (aliases included) and groups using the
   same registry field.
3. **Bypasses closed**: editor and explorer dispatch through the registry; mouse
   wheel/double-click dispatch by resolved action ID; the grid digit page-jump is
   removed (F-keys only).

## Approach (high level)

### Feature 1 — Grouping
- Extend the registry model with `Group` + `GroupLabel` on `Action`. Both are static
  inventory data, same tier as `Section`/`Owner`. A validation test asserts every
  member of a group carries the same non-empty `GroupLabel`.
- Add a **pure aggregator in `internal/config`** that turns the context's actions into
  display groups (combined primary keys + label + members). It is called by **both** the
  pane and the modal, so grouping logic exists once. No new `Resolver` method: grouping
  is derived from `ActionsFor(context)`, which already filters by context, so partial
  groups fall out for free.
- **Primary-key joining** is computed at render time (rebind-safe, never stored):
  single printable runes concatenate (`hjkl`), a contiguous digit range with a common
  prefix compresses (`f1-f9`), otherwise keys join with `/` (`g/G`, `ctrl+u/ctrl+d`).
- A group renders as a group only when **≥2 members are active in the context**;
  otherwise it degrades to a plain action segment. Group order follows registry order
  (current ordering already keeps group members adjacent).
- Conditional skips (`rollback` unless txPending, `autocomplete` unless ready) are
  applied **before** grouping; `tx pending` stays a synthetic trailing segment.
- `maxKeybindsPerLine` now counts **groups/segments**, not keys.
- No grouping by `Section`; ungrouped actions render exactly as today.

### Feature 2 — Bypass closure
- **Editor**: gains a `config.Resolver` **via a `SetKeybinds` setter** (the `NewSQLEditor`
  signature is deliberately unchanged to keep constructor contracts stable and avoid
  churning ~14 call sites) and resolves in `ContextEditor`, dispatching only editor-owned
  actions. `tab` keeps its single action (`autocomplete`) with its dual behavior (accept
  completion when visible, else indent); dead raw cases (`ctrl+enter`, `ctrl+r`) are
  deleted. The default branch remains widget-local text entry (out-of-scope B), guarded
  by a negative test that text keys never resolve.
- **Explorer tree**: delete the raw `handleKey` switch (`j/k/g/G/enter/backspace`),
  which is unreachable when `keybinds != nil`; the explorer keeps dispatching through
  resolved action IDs.
- **Help modal**: `?`/`q`/`esc` stay raw and are documented as an **overlay exception**;
  no modal context is added to the registry.
- **Mouse**: the wheel and double-click handlers resolve to action IDs
  (`navigate_up`/`navigate_down`/`edit_cell`) and dispatch through the same handlers —
  never synthesizing a raw `KeyPressMsg`.
- **Grid digits**: the `0-9` multi-digit page-jump handler and the `pendingDigits`
  field are **removed**. This is a small intentional behavior change (the handler is
  currently reachable): pagination is F-keys only, and a digit key in the grid becomes
  a no-op. Recorded in the ADR.

## Key decisions

- Grouping metadata lives in the registry; aggregation is a pure `config` function
  shared by pane and modal — one grouping path, no parallel display list.
- `Group`/`GroupLabel` are additive to `Action` (zero value = ungrouped); no `Resolver`
  interface change, so constructor contracts stay untouched.
- Widget-local text entry, modal scroll keys, and digit entry are **not** promoted to
  the registry (consistent with ADR 0001's rejection of mode keys).
- Digit page-jump removal over registering digits: registering `0-9` cannot express a
  stateful multi-digit prefix and would create display/dispatch ambiguity.

## Risks

- **`maxKeybindsPerLine` semantics change** (keys → segments): existing pane tests that
  count `" · "` must be updated to count segments.
- **`tab` dual semantics** in the editor: highest regression risk (silent loss of
  indent). Mitigated by keeping one action with both behaviors and a dedicated test.
- **Grid pending-cancel side effects** run on any key; dispatching mouse actions
  directly must preserve them or behavior drifts.
- **Digit removal**: intentional, user-approved behavior change; must be called out in
  the commit/ADR, not silently bundled.
- **Tree deletion**: confirm no `keybinds == nil` construction path before removing the
  fallback branch.
- **Override interaction**: a custom override replaces `Keys`; grouping keys off
  `Group`, so a rebound action still collapses — cover with a test.

## Ordering (coarse)

1. Registry model + aggregator + validation/grouping tests.
2. Pane rendering (primary-only, grouped, cap counts segments) + test updates.
3. Help modal grouping (all keys) + tests.
4. Feature 2A: editor resolver + tree deletion + editor text-entry negative test.
5. Feature 2C: mouse action dispatch + grid digit removal + tests.
6. ADR + docs touch-ups; verify `go build ./... && go vet ./... && go test ./...` and
   `make install`.

## Out of scope

- Widget text-entry raw keys (B, ~118).
- Grouping by `Section`.
- A registry "modal" context; mouse configurability; promoting modal scroll keys.
- Changing key semantics or the `KeybindingsConfig.Custom` override mechanism.
