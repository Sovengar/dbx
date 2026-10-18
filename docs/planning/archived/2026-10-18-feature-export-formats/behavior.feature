Feature: Grid export — Markdown, custom delimiters and file compression
  As a dbx user looking at a query result in the grid
  I want to export the rows as Markdown, choose the delimiter of the delimited
  format, and compress file exports
  So that I can paste results into docs and issues, feed local tools, and keep
  big exports small

  Background:
    Given the grid has a result with columns "id" and "name"
    And the export picker is the only entry point for exporting (key "x")

  # --- Markdown ---

  Scenario: Markdown export of a single row goes to the clipboard as a table
    Given the user has selected a single row
    When the user picks "Markdown" in the export picker
    Then the clipboard holds a Markdown table
    And the table has a header row with "id" and "name"
    And the table has a separator row
    And the table has one data row

  Scenario: Markdown export of several rows writes a .md table file
    Given the user has selected several rows
    When the user picks "Markdown" in the export picker
    Then a file named "<schema>_<table>.md" is written in the project directory
    And the file holds a Markdown table with a header, a separator and one row
      per selected row

  Scenario: Markdown escapes pipes and newlines and renders NULL as an empty cell
    Given a result whose cell value contains a pipe, a newline and a NULL
    When the user exports it as Markdown
    Then the pipe in the value is escaped as "\|"
    And the newline in the value is rendered as "<br>"
    And the NULL cell is rendered empty

  Scenario: Markdown truncates ragged rows so the table stays valid
    Given a result whose rows have fewer values than columns
    When the user exports it as Markdown
    Then every row of the table has the same number of cells as the header
    And no cell is invented for a value the query did not return

  # --- Custom delimiter ---

  Scenario: The delimited export uses the configured delimiter
    Given the configured export delimiter is ";"
    And the user has selected several rows
    When the user picks the delimited format in the export picker
    Then the written file separates fields with ";"
    And the file is named "<schema>_<table>.csv"

  Scenario: A tab delimiter writes real tab bytes and a .tsv file
    Given the configured export delimiter is the tab character
    When the user exports several rows in the delimited format
    Then the written file separates fields with a real tab byte
    And the file is named "<schema>_<table>.tsv"

  Scenario: A value containing the delimiter is quoted
    Given the configured export delimiter is "|"
    And a cell value contains "|"
    When the user exports the delimited format
    Then the value is quoted so the delimiter stays part of the value

  Scenario: An unknown or empty delimiter falls back to a comma
    Given the configured export delimiter is not a single usable character
    When the user exports the delimited format
    Then the export uses "," as the delimiter
    And the export does not fail

  # --- Compression ---

  Scenario: Compression writes a valid gzip file with a .gz name
    Given file compression is enabled
    When the user exports several rows to a file
    Then the written file is valid gzip
    And decompressing it yields exactly the uncompressed export
    And the file name ends with ".gz"

  Scenario: Compression is off by default and leaves files untouched
    Given the default configuration
    When the user exports several rows to a file
    Then the file is written uncompressed
    And the file name has no ".gz" suffix

  Scenario: Compression applies to every file format
    Given file compression is enabled
    When the user exports several rows as SQL, JSON, CSV or Markdown
    Then each written file is valid gzip with a ".gz" suffix

  Scenario: Clipboard and yank exports are never compressed
    Given file compression is enabled
    When the user copies or yanks rows to the clipboard
    Then the clipboard holds plain, uncompressed content

  # --- Backwards compatibility ---

  Scenario: The existing SQL, JSON and CSV defaults are unchanged
    Given the default configuration
    When the user exports with the SQL, JSON or CSV option
    Then the content matches today's output byte for byte
    And the file extensions are still ".sql", ".json" and ".csv"

  # --- Hardening ---

  Scenario: A DB-controlled schema or table cannot write outside the project
    Given a schema or table name that contains a path separator or a traversal
    When the user exports to a file
    Then path separators and control characters in the name are replaced with "_"
    And a name that had to be changed carries a short digest
    And a ".." component can never be a traversal component of its own
    And the resolved destination must stay inside the project directory
    And a destination that escapes it is refused and logged

  Scenario: Sanitised names that would collide stay distinct
    Given the table names "a/b", "a_b" and "a<TAB>b"
    When the user exports each to a file
    Then each export has a distinct file name because a changed name carries a short digest
    And an unchanged "public"/"users" is still named "public_users.csv"

  Scenario: A symlink at the export destination is refused
    Given a destination name in the project that is a symlink pointing outside it
    When the user exports to a file
    Then the outside target is untouched
    And the export is refused with the refusal logged

  Scenario: A very long identifier still exports
    Given a schema or table name longer than the filesystem name limit
    When the user exports to a file
    Then the name is truncated and keeps the short digest
    And the export does not fail with ENAMETOOLONG

  Scenario: Delimited cells cannot inject a spreadsheet formula
    Given a cell whose string form starts with "=", "+", "-", "@", TAB or CR
    When the user exports the delimited format
    Then the cell is prefixed with a leading apostrophe in the file
    And the clipboard and yank payloads keep the value byte for byte
    And SQL, JSON and Markdown exports are unchanged

  Scenario: Exported files are readable only by their owner
    When the user exports rows to a file
    Then the file is written with mode 0600
    And re-exporting over a pre-existing file also leaves mode 0600
