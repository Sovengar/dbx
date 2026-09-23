Feature: ASK — natural language to SQL chat pane
  As a dbx user who knows the data but not SQL
  I want to ask a question in natural language and see the result in the grid
  So that I can query the database without writing SQL by hand

  Background:
    Given the user is connected to a PostgreSQL database
    And a PostgreSQL provider is available

  # --- Opening the pane ---

  Scenario: `a` opens the ASK pane
    When the user presses "a"
    Then the ASK pane should be visible
    And the input line should have focus
    And the transcript should be empty

  Scenario: ASK is globally available regardless of focus
    Given the focus is on the explorer pane
    When the user presses "a"
    Then the ASK pane should be visible

  Scenario: `a` does not open ASK while the grid is editing or filtering
    Given the grid is editing a cell
    When the user presses "a"
    Then the ASK pane should NOT be visible
    And the "a" should be typed into the cell editor

  Scenario: `esc` closes the ASK pane without executing
    Given the ASK pane is open with a generated SQL under review
    When the user presses "esc"
    Then the ASK pane should NOT be visible
    And no query should be executed

  # --- Asking a question ---

  Scenario: Asking a question generates SQL and shows it for review
    Given the ASK pane is open
    When the user types "how many users are older than 18" and presses "enter"
    Then the question should appear in the transcript
    And the AI provider should be called with the question and the schema
    And the generated SQL should appear in the transcript
    And the pane should wait for confirmation without executing

  Scenario: Confirming executes the SQL and shows the result in the grid
    Given the ASK pane shows a generated SELECT under review
    When the user presses "enter"
    Then the SELECT should be executed
    And the grid should be populated with the query result
    And the ASK pane should close
    And a toast should report the number of rows returned

  Scenario: The transcript keeps the question and the executed SQL
    Given the user asked a question that was executed
    When the user presses "a" again
    Then the transcript should still show the previous question
    And it should show the SQL that was executed

  # --- Table context hint ---

  Scenario: The loaded table and its WHERE clause are passed as a context hint
    Given the grid is showing table "public.users"
    And the grid has a WHERE clause "active = true"
    When the user asks "how many are admins"
    Then the prompt sent to the AI should mention "public.users"
    And the prompt should mention the current WHERE clause as context

  Scenario: ASK works with no table loaded
    Given the grid has no table loaded
    When the user asks "list the 10 most recent orders"
    Then the prompt sent to the AI should include the full schema
    And no table context hint should be added

  # --- SELECT-only enforcement ---

  Scenario: A non-SELECT statement is rejected before execution
    Given the AI returns "DELETE FROM users"
    When the user confirms execution
    Then the statement should be rejected
    And an error should appear in the transcript
    And no query should be executed
    And the grid should be unchanged

  Scenario: The server rejects DML even if validation is bypassed
    Given a statement that passes validation but mutates data
    When it is executed through the ASK path
    Then the execution should run inside a READ ONLY transaction
    And PostgreSQL should reject the mutation

  # --- Error paths ---

  Scenario: AI provider failure
    Given the AI provider returns an error
    When the user submits a question
    Then an error should appear in the transcript
    And the ASK pane should stay open
    And no query should be executed

  Scenario: Invalid SQL from the AI
    Given the AI returns a syntactically invalid SELECT
    When the user confirms execution
    Then an error should appear in the transcript
    And the ASK pane should stay open

  Scenario: No database connection
    Given the database connection is not available
    When the user confirms execution of a generated SELECT
    Then an error should appear in the transcript
    And no query should be sent

  Scenario: No AI provider configured
    Given no AI provider is available
    When the user presses "a"
    Then the ASK pane should NOT open
    And an error toast should be shown

  Scenario: A pending DML transaction blocks ASK execution
    Given there is a pending DML transaction
    When the user confirms execution of a generated SELECT
    Then the execution should be refused
    And the transcript should tell the user to commit or roll back first
    And the grid should be unchanged

  # --- Discovery: palette, help, statusbar ---

  Scenario: ASK command available in the palette
    When the user opens the command palette
    Then an "Ask AI" command should be listed

  Scenario: ASK action shown in the help modal
    When the user opens the help modal
    Then the "Ask AI (NL→SQL)" action should be visible with key "a"

  Scenario: ASK keybind shown in the statusbar
    Given the user is in the main view
    Then the statusbar should include "a ask"
