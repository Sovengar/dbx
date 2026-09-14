Feature: ERE Diagram Viewer
  As a dbx user exploring a PostgreSQL schema
  I want an Entity-Relationship diagram tab
  So that I can understand table relationships at a glance

  Background:
    Given the user is connected to a PostgreSQL database
    And the explorer preview is focused on a table

  # --- Diagram Rendering ---

  Scenario: ERE tab renders diagram for table with relationships
    Given the current table "orders" has foreign keys to "users" and "products"
    And the table "order_items" has a foreign key to "orders"
    When the user presses "6" to switch to the ERE tab
    Then a text diagram is rendered using box-drawing characters
    And the center box shows "orders" with all its columns
    And the column "user_id" is marked as "FK"
    And the column "id" is marked as "PK"
    And a neighbor box "users" is connected to "orders" via "user_id"
    And a neighbor box "products" is connected to "orders" via "product_id"
    And a neighbor box "order_items" is connected to "orders" via "order_id"
    And edge labels show cardinality "N:1" for outgoing FKs
    And edge labels show cardinality "1:N" for incoming FKs

  Scenario: ERE tab shows empty state for table without relationships
    Given the current table "settings" has no foreign keys
    And no other table references "settings"
    When the user presses "6" to switch to the ERE tab
    Then the message "No relationships for this table" is displayed

  # --- Navigation ---

  Scenario: Navigate to related table via Enter
    Given the ERE diagram shows "orders" as center table
    And "users" is a neighbor box connected via FK
    When the user presses "↓" to select "users"
    And the user presses "Enter"
    Then the diagram re-centers on "users"
    And the center box now shows "users" with all its columns
    And the explorer tree selection updates to "users"
    And the diagram shows "users"'s relationships

  Scenario: Navigate through multiple neighbors
    Given the ERE diagram shows "orders" as center table
    And there are 3 neighbor boxes: "users", "products", "shippers"
    When the user presses "↓" three times
    Then the cursor moves through all three neighbors
    And the last neighbor "shippers" is selected

  # --- Scrolling ---

  Scenario: Diagram scrolls when exceeding pane height
    Given the current table "orders" has 15 incoming FK relationships
    And the pane height is 20 lines
    When the user switches to the ERE tab
    Then the diagram renders with a scroll offset
    And the user can scroll down with "↓" to see more neighbors
    And the user can scroll up with "↑" to see earlier neighbors

  Scenario: Scroll resets on table change
    Given the ERE diagram is scrolled to line 10
    When the user navigates to a different table via Enter
    Then the scroll offset resets to 0

  # --- Hub Table Handling ---

  Scenario: Hub table caps rendered neighbors
    Given the current table "events" has 25 incoming FK relationships
    When the user switches to the ERE tab
    Then at most 10 incoming neighbor boxes are rendered
    And a "+15 more" indicator is shown at the bottom

  # --- Edge Cases ---

  Scenario: Cross-schema foreign key reference
    Given the current table "orders" has a FK to "audit.logs" in a different schema
    When the user switches to the ERE tab
    Then the neighbor box shows "audit.logs" (schema included)

  Scenario: Long table name truncation
    Given the current table has a name longer than 40 characters
    When the user switches to the ERE tab
    Then the table name in the center box is truncated with "…"

  Scenario: Long column name truncation
    Given the current table has a column name longer than 30 characters
    When the user switches to the ERE tab
    Then the column name in the center box is truncated with "…"

  # --- Theme ---

  Scenario: Diagram uses theme styles
    Given the user has a custom theme configured
    When the user switches to the ERE tab
    Then the diagram colors and styles match the active theme
    And no hardcoded colors are used
