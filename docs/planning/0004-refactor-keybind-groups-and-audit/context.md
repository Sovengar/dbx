---
feature: 0004-refactor-keybind-groups-and-audit
freshness: b3aa392fbb78e6a83e6f42092d268b96cc90e764
codegraph: not_initialized
generated_by: codebase-researcher
---

# Context: Keybind grouping and registry hardening

Executor-facing navigation. Read the spec trio (`issue.md`, `behavior.feature`,
`plan.md`) and ADRs 0001/0002; this file only tells you WHERE and WHY, never WHAT.

## Scope
- In: add `Group`/`GroupLabel` to the registry `Action`; one pure aggregator in
  `internal/config`; pane renders groups + primary keys only; modal renders groups
  + all keys; close raw-key bypasses in editor/explorer tree/help modal; mouse
  wheel + double-click dispatch by resolved action ID; remove grid `0-9` page jump.
- Out: widget-local text entry (~118 keys, "B"); grouping by `Section`; a `modal`
  registry context; changing key semantics or the `KeybindingsConfig.Custom`
  mechanism.

## Files to Touch
| Symbol / Area | File | Lines | Why |
|---------------|------|-------|-----|
| `Action` struct | internal/config/keybindings.go | 28-36 | Add `Group string` + `GroupLabel string` (additive, zero value = ungrouped). Do NOT touch `ActionID` (23) or `Resolver` (41-47) — no new method. |
| `ActionsFor` | internal/config/keybindings.go | 122-133 | Aggregator input: already filters by context and by `len(Keys)>0`; partial groups fall out for free. Reference, do not change. |
| `PrimaryKey` / `KeysFor` | internal/config/keybindings.go | 106-120 | Aggregator reads primaries from `PrimaryKey`; modal keeps `KeysFor`/`Action.Keys`. Reference. |
| `defaultActions()` | internal/config/keybindings_actions.go | 16-187 | Assign `Group`/`GroupLabel` to the four sibling sets. Keep members ADJACENT (they already are). |
| navigate group | internal/config/keybindings_actions.go | 46-61 | `navigate_down`(46-49), `navigate_up`(48-49), `navigate_left`(50-51, grid-only), `navigate_right`(52-53, grid-only). `Group:"nav"`, `GroupLabel:"Navigate"`. |
| go_first/go_last | internal/config/keybindings_actions.go | 54-57 | `Group:"first_last"`, `GroupLabel:"First/Last"`. |
| half page | internal/config/keybindings_actions.go | 58-61 | `half_page_up`/`half_page_down` (grid + grid-preview). `Group:"half_page"`, `GroupLabel:"Half Page"`. |
| goto_page_1..9 | internal/config/keybindings_actions.go | 80-97 | Contiguous block, f1..f9. `Group:"goto_page"`, `GroupLabel:"Go to Page"`. |
| **NEW** aggregator | internal/config/keybindings_groups.go | new | Pure function over `[]Action` → display groups (combined primary keys + label + member actions). Called by BOTH pane and modal. Suggested API: `GroupActions(actions []Action, primary func(ActionID) string) []DisplayGroup` where `DisplayGroup{Label string; Keys []string; Members []Action; Grouped bool}`. Joining rules: single-rune primaries concatenate (`hjkl`), digit run with common prefix compresses (`f1-f9`), else `"/"` (`g/G`, `ctrl+u/ctrl+d`). `Grouped` only when ≥2 active members; else degrade to the plain action. Nothing stored (rebind-safe). |
| `renderLines` | internal/ui/keybindspane.go | 97-153 | Replace per-action loop (104-116) with aggregator over `ActionsFor(ctx)`. Primary keys only. Keep conditional skips BEFORE grouping: `rollback` unless `txPending` (105-107), `autocomplete` unless `editorOpen && !autocompleteReady` (108-110). Keep `tx pending` trailing segment (117-119). Cap counts SEGMENTS (`maxKeybindsPerLine` const at 17; wrap loop 135-148) — unchanged code if each group is one segment. |
| `maxKeybindsPerLine` | internal/ui/keybindspane.go | 17 | Semantics change keys→segments; comment update. |
| `View` section loop | internal/ui/modal.go | 108-206 (loop 125-131) | Group each section's `ActionsFor` through the SAME aggregator; render group label + ALL keys (`Action.Keys`, aliases included). Static prose sections 133-177 (Query Browser, Connection Picker, EDIT/FILTER/WHERE, Mouse) stay as-is. |
| `renderAction` | internal/ui/modal.go | 208-214 | Today joins every key with `", "`. Reuse for group members; a grouped member still shows all its aliases. |
| overlay close exception | internal/ui/modal.go | 44-93 (close at 52-55) | `?`/`q`/`esc` stay raw; add a doc comment stating this is a deliberate overlay exception (ADR 0002 §2), NOT a registry context. Do not promote modal scroll keys (56-79). |
| `SQLEditor` struct | internal/ui/components/editor/sql.go | 14-32 | Add `keybinds config.Resolver` field. |
| `NewSQLEditor` | internal/ui/components/editor/sql.go | 34-42 | **Unchanged signature** (user decision). Add a `SetKeybinds(config.Resolver)` setter and wire it from `app.go:131`; do NOT add a constructor param (avoids churning ~14 call sites and honours ADR 0002 "constructor contracts unchanged"). |
| `handleKey` | internal/ui/components/editor/sql.go | 156-283 | Add `Resolve(key, config.ContextEditor)`; dispatch editor-owned IDs only (`autocomplete`, `history_prev`, `history_next` per `handled.go`). Delete dead raw cases: `ctrl+enter` (160-161), `ctrl+r` (163-164), and `ctrl+y`(170-174)/`ctrl+u`(176-179) which the app intercepts first. KEEP `tab` dual behavior (248-257): accept completion if visible, else indent. KEEP all local text/motion handling (195-272) as the fall-through. `ctrl+space` (166-168) is widget-local, keep. |
| `HandledActions` | internal/ui/components/editor/handled.go | 9-13 | Already lists `autocomplete`, `history_prev`, `history_next`. Unchanged unless editor claims more. |
| `Tree.handleKey` | internal/ui/components/explorer/tree.go | 152-178 | DELETE the raw switch (`j/down`,`k/up`,`enter/l`,`backspace/h`,`g`,`G`). Reachable only when `keybinds == nil`; production always passes a resolver (`app.go:1166`). Explorer keeps dispatching via `handleAction`. |
| `Explorer.Update` / `handleAction` | internal/ui/components/explorer/explorer.go | 82-104 / 106-183 | Model to imitate. `Resolve` at 94, dispatch switch at 108. For mouse-by-ID you need an exported entry point (see Integration). |
| `Grid.pendingDigits` | internal/ui/components/grid/table.go | 141 | Remove the field. |
| `Grid.handleKey` digit block | internal/ui/components/grid/table.go | 629-650 | Keep the `goto_page_N` loop 629-637; REMOVE the `0-9` branch 639-649 and the trailing `g.pendingDigits = ""` (650). Preserve pending-cancel block 608-621 and `hasRows` gate 623-627. |
| exported grid dispatch (NEW) | internal/ui/components/grid/table.go | near 544 | Add an action-ID entry point (e.g. `HandleAction(config.ActionID) (tea.Cmd, bool)`) factored out of the `switch action` at 652-762, so the app can dispatch `navigate_down`/`navigate_up`/`edit_cell` without synthesizing a `KeyPressMsg`. |
| `MouseWheelMsg` | internal/app/app.go | 1648-1702 | Grid branch 1686-1694 and explorer branch 1695-1700 currently inject `tea.KeyPressMsg{Code:'k'/'j'}`. Replace with resolved action-ID dispatch (`navigate_up`/`navigate_down`). Keep the `syncGridSidebarPreviewForCursor` call (1692-1694). |
| `MouseClickMsg` double-click | internal/app/app.go | 1730-1736 | Replace `m.grid.Update(tea.KeyPressMsg{Code: 13})` (1734) with action-ID dispatch of `edit_cell`. |
| editor interception | internal/app/app.go | 1832-1841 | App resolves `ContextEditor` and dispatches app-owned actions (1833) before `editor.Update` (1837). This is why editor's ctrl+enter/ctrl+r/ctrl+y/ctrl+u are dead. Reference. |
| `appActions` / `hasAppAction` / `dispatchAction` / `HandledActions` | internal/app/app.go | 1929-1962, 1944-2151, 2155-2162 | The single app dispatch table. Mouse handlers must dispatch through resolved IDs; app-owned IDs go through `dispatchAction`, component-owned IDs must reach the component (Integration). |
| preview_cursor raw inject | internal/app/app.go | 2047-2058 | `preview_cursor_up/down` (Keys nil) still inject raw `k`/`j` into gridPreview. ADJACENT bypass, NOT in scope; do not touch unless asked. |
| README keybinds prose | README.md | 176-184 | Docs touch-up only if wording changes. |

## Contracts
- `config.Resolver` — `internal/config/keybindings.go:41-47`. **No new method.** Grouping is a pure function over `[]Action`; `ActionsFor(context)` already does context + `len(Keys)>0` filtering, so partial groups are automatic.
- `config.Action` — `internal/config/keybindings.go:28-36`. New `Group`/`GroupLabel` are additive; `Owner`/`Pending` semantics unchanged. Validation invariant to add: every action with a non-empty `Group` must carry a non-empty `GroupLabel`, and all members of the same `Group` must share the identical `GroupLabel`.
- `appActions()` map — `internal/app/app.go:1944-2151`. The action↔handler coverage tests key off it plus each component's `HandledActions()`.
- Component `HandledActions()` — `grid/handled.go`, `explorer/handled.go:6-12`, `editor/handled.go:9-13`. `internal/app/keybind_coverage_test.go` asserts: every non-`Pending` action is handled by exactly its declared `Owner`; no handler exists without a declared action.
- App→component ordering (must be preserved): `app.go:1832` resolves app-owned actions BEFORE calling `component.Update` (1837, 1898-1913). Components resolve only their own context afterwards. Do not reorder.
- `AppContent`: app resolves FIRST for app-owned IDs; component-owned IDs never enter `dispatchAction` (it would toast `"Command: <id>"`, see 1940).

## Pattern to Follow
- Editor resolver + dispatch: model on `internal/ui/components/explorer/explorer.go:82-104` (Resolve → `handleAction`) and `:108-183` (dispatch switch returning `handled=false` to fall through).
- Registry metadata: model on `Section` (`keybindings_actions.go:4-12`) for the validation test shape, and on `Owner`/`Pending` for static inventory fields.
- Tests: registry invariants in `internal/config/keybindings_test.go:79-136`; coverage invariants in `internal/app/keybind_coverage_test.go:57-103`; pane assertions against `renderLines()` in `internal/ui/keybindspane_test.go`; modal against `View()` in `internal/ui/modal_test.go`.

## Tests
Existing affected (must be reviewed/updated):
- `internal/ui/keybindspane_test.go`
  - `TestKeybindsPane_MaxSevenKeybindsPerLine` (126-142): counts `" · "`; still valid IF each group is one segment, but the "first line exactly 7" assertion can shift once actions collapse — verify against the new segment list.
  - `TestKeybindsPane_ExplorerShowsFilterTablesKey` (27-32) expects `"/ Filter Tables"`: `filter_tables` is ungrouped, should still pass.
  - `TestKeybindsPane_CustomKeyChangesDisplay` (35-48): override on ungrouped `filter_tables`, should pass.
  - `TestKeybindsPane_ViewChangeChangesContent` (65-78): grid still contains `"Edit Cell"`; fine.
  - `TestKeybindsPane_EditorShowsCloseKey` (117-123), `_ShowsAskKeybind` (98-102), `_TxPendingShowsRollback` (105-114): ungrouped; fine.
- `internal/ui/modal_test.go`: `TestHelpModal_EditorSection_IncludesCopySQL` (53-62), `TestHelpModal_GlobalSection_IncludesAsk` (40-51), `TestHelpModal_CustomKeyOverride` (98-107) assert exact `"  %-14s"` formatting for ungrouped actions — should keep passing; grid section becomes grouped.
- `internal/ui/components/editor/sql_test.go`: `TestSQLEditor_CtrlY_*` (14-85) call `ed.Update(ctrl+y)` directly and expect `CopySQLMsg`. If `ctrl+y` is removed/routed through the resolver without emitting `CopySQLMsg`, these break — either keep the case or update the tests. `copy_sql` is app-owned (`app.go:2113`), so the editor path is dead in production.
- `internal/app/copy_sql_test.go` (25-64): tests `m.handleCopySQL()`, not the key path; unaffected.
- Constructor call sites if `NewSQLEditor` gains a param: `internal/app/app.go:131`, `internal/app/copy_sql_test.go:17`, `internal/app/dml_rollback_helpers_test.go:89`, `internal/ui/components/editor/autocomplete_test.go` (223,242,255,277,291), `internal/ui/components/editor/sql_test.go` (16,38,52,74,88,103,114). A `SetKeybinds` setter avoids all of these.
- `internal/app/keybind_coverage_test.go`: `coverageModel` (15-23) and `newRollbackTestModel` (`dml_rollback_helpers_test.go:81-99`) must stay wired if editor gains a resolver.

New tests to add:
- `internal/config/keybindings_test.go`: group label consistency (same non-empty label per group); aggregator grouping/join rules; ≥2-member threshold; single-member degradation; no grouping by `Section`; rebind keeps group collapsed.
- `internal/ui/keybindspane_test.go`: `"hjkl Navigate"`, `"g/G First/Last"`, `"ctrl+u/ctrl+d Half Page"`, `"f1-f9 Go to Page"` on grid; explorer partial `"j/k Navigate"` and absence of half-page/goto-page; primary-only (`"n Next Page"`, never `"ctrl+right"` nor `"n/]/ctrl+right"`).
- `internal/ui/modal_test.go`: Grid section lists `next_page` aliases and groups under a label.
- `internal/ui/components/editor/`: history_prev dispatches via registry; text keys never resolve (negative); `tab` dual behavior (accept completion when visible, else indent).
- `internal/app` and/or grid/explorer: mouse wheel dispatches action ID and rebinding `navigate_down` changes it; double-click dispatches `edit_cell`; grid digit key is a no-op while `f1-f9` still jump.

Runner: `make test` (wraps `go test ./...`); build/vet before install. `make install` is MANDATORY after code changes (AGENTS.md) — the user runs `~/.local/bin/dbx`.

## Conventions & Boundaries
- `internal/config` owns the keybind MODEL + grouping aggregation; `internal/ui` (pane/modal) owns PRESENTATION only. No display list, no second group map, no per-surface grouping.
- Grouping must derive from the single registry entry; overrides replace `Keys` and the render-time join must reflect the new key (never stored).
- Imports: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`; comments in English.
- Key events are `tea.KeyPressMsg`; `Update` returns `(tea.Cmd, bool)`; components take `config.Resolver`, not a raw map.

## Integration Points (non-obvious)
- **Mouse-by-ID needs a public component entry point.** `Grid.handleKey` (private) and `Explorer.handleAction` (private) are the only dispatchers. Since a mouse event must NOT synthesize a `KeyPressMsg` (ADR 0002 §3), you must export an action-ID dispatch on `Grid` and `Explorer` (e.g. `HandleAction(id)`) and have `app.go` wheel/double-click call it. This is the single biggest structural change not spelled out by line number in the plan.
- **Pending-cancel side effects must survive.** `grid/table.go:608-621` clears `discardPending`/`commitPending`/`refreshPending` on any key before the movement switch. If the new action-ID entry point bypasses this block, wheel-driven navigation silently stops cancelling pending confirmations. Factor the action switch so the new entry goes through the same prelude, or replicate it.
- **Wheel targets the pane under the cursor, not the focused pane** (`app.go:1677-1701`). So `HandleAction` must work even when the grid/explorer is not focused; note `Grid.Update`/`handleKey` short-circuit on `!g.focused` (519-522) while the wheel path calls `Update` directly and still works because... verify: `handleKey` itself has no focused guard, only `Update` does — dispatching via a method that skips the `Update` focused check is what makes the current wheel work. Preserve that.
- **Explorer tree reachability.** `explorer.go:93-101` falls through to `tree.Update` only when `Resolve` fails or `handleAction` returns false. With a non-nil resolver, every key the tree switch handles is already claimed by `handleAction`, so the switch is dead. Production constructors always pass a resolver (`app.go:1166`); there is no `keybinds == nil` path in the app. Safe to delete.
- **Editor interception order.** `app.go:1832-1841` resolves `ContextEditor` and dispatches app-owned actions BEFORE `editor.Update`. So editor-owned dispatch is only for `autocomplete`/`history_prev`/`history_next`; `execute_query`/`copy_sql`/`clear_editor`/`close_editor` remain app-owned and must not be duplicated in the editor.
- **Modal sections are registry-driven only for the five `modalSections`** (`modal.go:97-106`); Query Browser / Connection Picker / EDIT / FILTER / WHERE / Mouse (133-177) are static prose and out of scope.
- **`focus_preview`/`explorer_open_preview`/`preview_back` share key `tab` across contexts** (`keybindings_actions.go:62-67`) and are NOT part of the required groups; leave them ungrouped.
- **Override replaces all keys** (`keybindings.go:58-67`): a rebound grouped member (`next_page:"X"`) must still collapse into its group (if any) with the overridden primary. None of the four required groups includes aliased actions except `navigate_*` (which have `down/up/left/right` aliases); the pane must show only `j/k/h/l`, never the arrows.

## Risks / Assumptions
- Removing the grid `0-9` jump is an intentional, user-approved behavior change (currently reachable) — call it out in the commit; a grid digit becomes a no-op.
- `tab` dual semantics is the highest regression risk (silent loss of indent); keep one `autocomplete` action with both behaviors and test both.
- `maxKeybindsPerLine` keys→segments can subtly shift which segments land on line 1.
- Editor `ctrl+y` tests assert `CopySQLMsg` from a path that is dead in production; decide keep-vs-update and keep tests green.
- Constructor churn avoided by decision: use the `SetKeybinds` setter, keep `NewSQLEditor` unchanged, so none of the call sites listed above change.
- `Group`/`GroupLabel` must be added to every member of a group or the label-consistency test fails.
