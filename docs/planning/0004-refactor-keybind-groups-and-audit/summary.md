# Summary: keybind-groups-and-audit

## Metadata
- **Completed:** 2026-09-23 22:40
- **Duration:** 63 minutes (first commit 21:35 → last commit 22:38)
- **Plan Number:** 0004
- **Type:** refactor
- **Branch:** `refactor/keybind-registry-hardening`
- **Base:** `main` @ `af111b8` (branch cut) — `main` has since advanced 5 docs-only commits to `7b50ea5`
- **Commits:** 16 ahead of `af111b8`

## Scenarios
Source: `behavior.feature` (18 scenarios). Status from the committed tests + green suite.

### Feature 1 — Grouped display
| Scenario (behavior.feature) | Status | Covering test |
|-----------------------------|--------|---------------|
| Sibling actions render as one segment with combined primary keys | ✅ Passed | `TestKeybindsPane_GridRendersGroups` |
| The pane shows only the primary key of an action | ✅ Passed | `TestKeybindsPane_ShowsPrimaryKeysOnly` |
| A partially active group shows only current-context members | ✅ Passed | `TestKeybindsPane_ExplorerPartialGroup`, `TestGroupActions_ExplorerPartialGroup` |
| A group with <2 active members degrades to a plain action segment | ✅ Passed | `TestGroupActions_SingleMemberDegrades` |
| The per-line cap counts segments, not keys | ✅ Passed | `TestKeybindsPane_MaxSevenKeybindsPerLine` |
| Actions are not grouped by Section | ✅ Passed | `TestGroupActions_DoesNotGroupBySection` |
| Rebinding a member keeps the group collapsed | ✅ Passed | `TestGroupActions_RebindKeepsGroupCollapsed` |
| Help modal shows every key (aliases included) and groups by the same field | ✅ Passed | `TestHelpModal_GridShowsAliasesAndGroups` |
| Conditionally hidden actions do not break grouping | ✅ Passed | `TestKeybindsPane_TxPendingKeepsGroups` |

### Feature 2 — Raw-key bypass closure (audit A + C)
| Scenario (behavior.feature) | Status | Covering test |
|-----------------------------|--------|---------------|
| Editor shortcuts dispatch through the resolved action ID | ✅ Passed | `TestSQLEditor_HistoryPrevDispatchesViaRegistry` |
| Widget-local text entry in the editor is untouched | ✅ Passed | `TestSQLEditor_TextKeysDoNotResolve`, `TestSQLEditor_TabIndentsWhenNoCompletion` |
| Dead raw-key handling in the explorer tree is removed | ✅ Passed | `TestExplorer_TreeNoLongerHandlesRawKeys` |
| Help modal close keys are a documented overlay exception | ✅ Passed | `TestKeybindRegistry_ResolveCloseEditor` + ADR 0002 §2 |
| Mouse wheel scrolls through resolved navigation actions | ✅ Passed | `TestMouseWheel_DispatchesNavigateDown` |
| Rebinding a navigation action changes the wheel behavior | ✅ Passed | `TestMouseWheel_ReboundNavigationActionRuns` |
| Double-click dispatches the resolved edit action | ✅ Passed | `TestMouseDoubleClick_DispatchesEditCell` |
| Digit page-jump removed; only F-keys jump pages | ✅ Passed | `TestGrid_DigitKeyIsNoOpButFKeyJumps` |

### Invariant guard
| Scenario (behavior.feature) | Status | Covering test |
|-----------------------------|--------|---------------|
| Display and dispatch cannot diverge | ✅ Passed | `TestRegistry_GroupLabelsAreConsistent`, `TestRegistry_GroupMembersAreConsecutive`, existing collision + coverage tests |

## Commits
- `0e5699b` fix(keybinds): register editor Esc and cancel autocomplete before closing
- `b3aa392` feat(keybinds): cap the keybinds pane at 7 entries per line
- `1d1724b` chore: add keybind-groups-and-audit plan
- `2d4288d` feat(config): add keybind display groups and pure aggregator
- `023797c` refactor(ui): render pane from grouped display segments
- `4b6e7cf` refactor(ui): group help modal actions by the registry field
- `a7dc1c3` refactor(editor): dispatch editor actions through the registry
- `6beaba0` refactor(grid): dispatch mouse by action ID and drop digit page-jump
- `47839e8` docs(readme): describe grouped keybind pane
- `9938ebd` test(ui): cover tx-pending keeping grouped segments
- `ccedb72` refactor(editor): extract acceptSelectedCompletion
- `d701ae2` docs(editor): correct SetKeybinds nil-resolver comment
- `e4babac` fix(explorer): no-op HandleAction while filtering
- `df4c64f` test(config): assert group members are consecutive
- `de0a86a` test(editor): exercise space/backspace/enter as local text entry
- `3d8649f` refactor(gridpreview): dispatch preview cursor via action IDs

## Files

### Created
- `internal/config/keybindings_groups.go` — pure `GroupActions` aggregator + primary-key joining
- `internal/config/keybindings_groups_test.go`
- `internal/app/mouse_dispatch_test.go`
- `internal/ui/components/editor/keybind_test.go`
- `internal/ui/components/explorer/explorer_test.go`
- `internal/ui/components/grid/action_test.go`
- `internal/ui/components/gridpreview/preview_test.go`
- `docs/adr/0002-keybind-groups-and-widget-local-boundary.md`
- `docs/planning/0004-refactor-keybind-groups-and-audit/{issue.md,behavior.feature,plan.md,context.md,diagrams/feature-flow.md,diagrams/process-flow.md}`

### Modified
- `internal/config/keybindings.go` — `Group` / `GroupLabel` added to `Action` (additive)
- `internal/config/keybindings_actions.go` — assign the four sibling groups
- `internal/config/keybindings_test.go`
- `internal/ui/keybindspane.go` — render from grouped segments; cap counts segments
- `internal/ui/keybindspane_test.go`
- `internal/ui/modal.go` — group modal by the registry field; overlay-close doc
- `internal/ui/modal_test.go`
- `internal/ui/components/editor/sql.go` — `SetKeybinds` resolver + registry dispatch
- `internal/ui/components/editor/autocomplete_test.go`
- `internal/ui/components/explorer/tree.go` — delete dead raw-key switch
- `internal/ui/components/explorer/explorer.go` — filtering guard on `HandleAction`
- `internal/ui/components/grid/table.go` — `HandleAction` + remove `0-9`/`pendingDigits`
- `internal/ui/components/gridpreview/preview.go` — `HandleAction` for preview cursor
- `internal/app/app.go` — mouse dispatch by resolved action ID
- `internal/app/keybind_behavior_test.go`
- `internal/app/dml_rollback_helpers_test.go`
- `README.md` — grouped-pane note

> Note: `docs/CONFIG.md` and `docs/FEATURES.md` show as changed in `git diff main..HEAD`, but the branch does **not** touch them — those deltas are `main`'s own docs rewrite appearing reversed because the branch predates it (see Risks).

## Tests
- Added: 37 new test functions (across config/app/ui/editor/explorer/grid/gridpreview)
- Suite: `go build ./... && go vet ./... && go test ./...` — ✅ all green
- Binary: `make install` run (parent-reported)

## Documentation
- Changelog: N/A — the repo does not maintain a `CHANGELOG.md` (verified; none exists)
- Docs updated: `README.md` (grouped pane vs `?` overlay)
- ADR: ✅ Created — `docs/adr/0002-keybind-groups-and-widget-local-boundary.md` (Accepted)

## Code Review Issues
- No standalone adversarial-review artifact was produced for this branch.
- Audit findings A + C (from `issue.md`) are all closed by committed tests; audit B (widget text-entry keys) explicitly out of scope.
- Two extra hardening fixes landed during the branch beyond the plan's literal scope:
  - `0e5699b` — editor `Esc` moved into the registry (`close_editor`), cancelling autocomplete first.
  - `e4babac` — `Explorer.HandleAction` gained the missing filtering guard (wheel could move the tree while filtering).
  - `3d8649f` — grid-preview cursor dispatch converted from raw `j`/`k` injection to action IDs (context.md had marked this adjacent bypass as out-of-scope; it was pulled in).
- Commit `6beaba0` carries a `BREAKING CHANGE:` footer for the grid digit page-jump removal; recorded in ADR 0002 §4.

## Risks
- **`main` drift (action needed before push):** the branch is based on `af111b8`; `main` has since advanced 5 docs-only commits (`3b2bb6b` → `7b50ea5`, README/CONFIG/FEATURES rewrite + plan roadmap). The branch does **not** contain them. Opening a PR without syncing would make the diff look like it reverts `main`'s docs work. Recommend rebasing onto `origin/main` before pushing.
- **User-visible BREAKING change:** grid `0-9` multi-digit page-jump removed; digits in the grid are now no-ops (F-keys only). Documented in `6beaba0` + ADR 0002 §4.
- **`maxKeybindsPerLine` semantics changed** keys → segments; pane tests updated accordingly.

## Next Step
Rebase `refactor/keybind-registry-hardening` onto `origin/main` (`7b50ea5`), then push and open the PR against `main`. Archiving `docs/planning/0004-.../` to `docs/planning/archived/` is pending (deferred until after the PR is merged, so the plan stays live during review).
