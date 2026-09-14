Feature: Copy SQL to clipboard from editor
  As a dbx user
  I want to copy the SQL from the editor to my clipboard
  So I can paste it into an external tool or share it

  Scenario: Copy SQL with Ctrl+Y
    Given the SQL editor is open and focused
    And the editor contains "SELECT * FROM users WHERE id = 1"
    When I press Ctrl+Y
    Then the SQL "SELECT * FROM users WHERE id = 1" is copied to the system clipboard
    And a toast notification "SQL copied to clipboard" appears

  Scenario: Copy empty editor does nothing
    Given the SQL editor is open and focused
    And the editor is empty
    When I press Ctrl+Y
    Then no clipboard action is taken
    And no toast notification appears

  Scenario: Copy multi-line SQL
    Given the SQL editor is open and focused
    And the editor contains:
      """
      SELECT u.name, o.total
      FROM users u
      JOIN orders o ON o.user_id = u.id
      WHERE o.total > 100
      """
    When I press Ctrl+Y
    Then the full multi-line SQL is copied to the system clipboard
    And a toast notification "SQL copied to clipboard" appears

  Scenario: Copy SQL preserves editor content
    Given the SQL editor is open and focused
    And the editor contains "DELETE FROM temp WHERE 1=1"
    When I press Ctrl+Y
    Then the editor still contains "DELETE FROM temp WHERE 1=1"
    And the SQL is copied to the clipboard

  Scenario: Copy keybind is discoverable in help
    Given the user opens the help modal with ?
    Then the Editor section shows "Copy SQL" bound to Ctrl+Y
