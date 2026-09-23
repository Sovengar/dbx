# language: en
Feature: Keybind grouping and registry hardening

  The keybind registry (`internal/config/keybindings_actions.go` → `defaultActions()`)
  remains the single source of truth: the pane, the help modal and dispatch all derive
  from the same entry. This feature adds display grouping on top of that entry and
  closes the raw-key bypasses that made display and dispatch diverge.

  # ─────────────────────────────────────────────────────────────
  # Feature 1 — Group keybinds in the pane
  # ─────────────────────────────────────────────────────────────

  Background:
    Given the keybind registry is the only keybind inventory
    And no display surface keeps a parallel key list

  Scenario: Sibling actions of a group render as one segment with combined primary keys
    Given the grid view is focused
    When the keybinds pane renders
    Then navigate_down, navigate_up, navigate_left and navigate_right appear as one segment "hjkl Navigate"
    And go_first and go_last appear as one segment "g/G First/Last"
    And half_page_up and half_page_down appear as one segment "ctrl+u/ctrl+d Half Page"
    And goto_page_1 through goto_page_9 appear as one segment "f1-f9 Go to Page"

  Scenario: The pane shows only the primary key of an action
    Given the grid view is focused
    When the keybinds pane renders
    Then next_page appears as "n Next Page"
    And the pane does not contain "ctrl+right"
    And the pane does not contain "n/]/ctrl+right"

  Scenario: A partially active group shows only the members of the current context
    Given the explorer view is focused
    When the keybinds pane renders
    Then the navigation group shows only the active members "j/k"
    And the half-page group is not rendered because none of its members apply to the explorer
    And the goto-page group is not rendered because none of its members apply to the explorer

  Scenario: A group with fewer than two active members renders as a plain action segment
    Given a group whose context activates a single member
    When the keybinds pane renders
    Then that member renders as its own action segment (primary key and description)
    And the group label is not used

  Scenario: The per-line cap counts segments, not keys
    Given a pane wide enough that only the count cap forces a wrap
    And the grid view is focused
    When the keybinds pane renders
    Then each line contains at most seven segments
    And the count includes one segment per group

  Scenario: Actions are not grouped by Section
    Given two actions that share a Section but do not share a Group
    When the keybinds pane renders
    Then they render as two separate segments

  Scenario: Rebinding a member keeps the group collapsed
    Given a custom override that rebinds a key of a grouped action
    When the keybinds pane renders
    Then the group still renders as one segment
    And the segment shows the overridden primary key

  Scenario: The help modal shows every key, including aliases, and groups by the same field
    Given the help modal is open
    When the Grid section renders
    Then next_page lists all of its keys, including the alias keys
    And grouped members are shown under their group label
    And the grouping comes from the same registry field the pane uses

  Scenario: Conditionally hidden actions do not break grouping
    Given a transaction is not pending
    Then the rollback action is hidden from the pane
    And grouped segments are unaffected
    And when a transaction is pending, the "tx pending" segment is still appended

  # ─────────────────────────────────────────────────────────────
  # Feature 2 — Close raw-key bypasses (audit A + C)
  # ─────────────────────────────────────────────────────────────

  Scenario: Editor shortcuts dispatch through the resolved action ID
    Given the editor is focused
    When the user presses a key bound to an editor action such as history_prev
    Then the editor dispatches that action through the registry
    And the editor no longer matches that shortcut against a raw key string

  Scenario: Widget-local text entry in the editor is untouched
    Given the editor is focused
    When the user presses a letter, a digit, space, backspace or enter
    Then the key is handled as local text entry
    And it does not resolve to a registry action

  Scenario: Dead raw-key handling in the explorer tree is removed
    Given the explorer dispatches keys through the registry
    Then the tree no longer handles j, k, g, G, enter or backspace by itself
    And the explorer keeps dispatching those actions through resolved action IDs

  Scenario: The help modal close keys are a documented overlay exception
    Given the help modal is visible
    When the user presses "?", "q" or "esc"
    Then the modal closes
    And this overlay exception is documented rather than modelled as a registry context

  Scenario: Mouse wheel scrolls through resolved navigation actions
    Given the grid pane is under the cursor
    When the user scrolls the wheel down
    Then navigate_down is dispatched through its resolved action ID
    And no raw key press is injected into the grid

  Scenario: Rebinding a navigation action changes the wheel behavior
    Given the user rebinds navigate_down
    When the user scrolls the wheel down over the grid
    Then the rebound action is the one that runs

  Scenario: Double-click dispatches the resolved edit action
    Given the grid is focused
    When the user double-clicks a cell
    Then edit_cell is dispatched through its resolved action ID
    And no raw enter key press is injected into the grid

  Scenario: Digit page-jump is removed; only F-keys jump pages
    Given the grid is focused
    When the user types a digit key
    Then nothing happens (the grid does not jump to a page)
    And only f1 through f9 jump to a page
    And the pane never advertises digits as a keybind

  # ─────────────────────────────────────────────────────────────
  # Invariant guard
  # ─────────────────────────────────────────────────────────────

  Scenario: Display and dispatch cannot diverge
    Given any action with at least one key
    When its displayed key is pressed in a context where it applies
    Then the same action is dispatched
    And the registry reports no key collision within that context
