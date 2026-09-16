Feature: DML Rollback with U key
  As a database user
  I want to rollback DML statements before they are committed
  So that I can undo mistakes without data loss

  Background:
    Given the user is connected to a PostgreSQL database
    And the editor is open

  # --- DML Transaction Mode ---

  Scenario: UPDATE opens a transaction that stays open
    When the user executes "UPDATE users SET name='test' WHERE id=1"
    Then a transaction should be opened (BEGIN sent)
    And the changes should NOT be committed to the database yet
    And the statusbar should show "tx pending" indicator

  Scenario: INSERT opens a transaction that stays open
    When the user executes "INSERT INTO users (name) VALUES ('new_user')"
    Then a transaction should be opened (BEGIN sent)
    And the changes should NOT be committed to the database yet

  Scenario: DELETE opens a transaction that stays open
    When the user executes "DELETE FROM users WHERE id=999"
    Then a transaction should be opened (BEGIN sent)
    And the changes should NOT be committed to the database yet

  # --- Rollback with U ---

  Scenario: U rolls back the pending transaction
    Given the user has executed an UPDATE that opened a transaction
    When the user presses "U"
    Then a ROLLBACK should be sent to the database
    And the changes should be undone
    And the toast should show "Transaction rolled back"
    And the statusbar should no longer show "tx pending"

  Scenario: U with no pending transaction shows info toast
    Given there is no pending transaction
    When the user presses "U"
    Then no ROLLBACK should be sent
    And the toast should show "No pending transaction"

  # --- Auto-commit Before New Statement ---

  Scenario: New DML auto-commits previous transaction
    Given the user has executed an UPDATE that opened a transaction
    When the user executes another "UPDATE users SET name='test2' WHERE id=2"
    Then a COMMIT should be sent for the first transaction
    And a new transaction should be opened for the second statement

  Scenario: DDL auto-commits previous transaction
    Given the user has executed an UPDATE that opened a transaction
    When the user executes "CREATE TABLE test_rollback (id SERIAL PRIMARY KEY)"
    Then a COMMIT should be sent for the pending transaction
    And the DDL should execute in autocommit mode

  # --- SELECT and DDL Stay in Autocommit ---

  Scenario: SELECT does not open a transaction
    When the user executes "SELECT * FROM users LIMIT 10"
    Then no transaction should be opened
    And the results should be returned immediately

  Scenario: DDL does not open a transaction
    When the user executes "CREATE TABLE test_no_tx (id INT)"
    Then no transaction should be opened
    And the DDL should execute in autocommit mode

  # --- Multiple DML Batch ---

  Scenario: Batch of DML statements in single execution
    When the user executes "UPDATE users SET name='a' WHERE id=1; UPDATE users SET name='b' WHERE id=2"
    Then a single transaction should wrap all statements
    And all statements should execute within the same transaction
    And pressing "U" should rollback ALL statements in the batch

  # --- Palette and Help ---

  Scenario: Rollback command available in palette
    When the user opens the command palette
    Then "Rollback" command should be listed with alias "rollback"

  Scenario: Rollback action shown in help modal
    When the user opens the help modal
    Then "Rollback Last Transaction" action should be visible

  # --- Statusbar ---

  Scenario: Statusbar shows rollback keybind when transaction pending
    Given the user has executed an UPDATE that opened a transaction
    Then the statusbar should include "U rollback" in the keybinds area

  Scenario: Statusbar hides rollback indicator when no transaction
    Given there is no pending transaction
    Then the statusbar should NOT show "tx pending" indicator
