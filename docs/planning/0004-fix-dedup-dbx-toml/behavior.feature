Feature: Robust scan of .dbx.toml — no crash, deterministic pick, deduplication
  As a dbx user with multiple projects and git worktrees
  I want the project scanner to be crash-proof, deterministic and free of duplicates
  So that the picker shows exactly one entry per real connection

  Background:
    Given the scanner walks a root directory looking for ".dbx.toml" files
    And the scanner has a deterministic, fd-independent file discovery seam in tests

  # --- Problem 1: crash on files without connections ---

  Scenario: A .dbx.toml without connections does not crash the scan
    Given a ".dbx.toml" file with no "[connections]" section
    And another ".dbx.toml" file with a valid connection
    When the scanner scans the root
    Then the scan completes without panicking
    And the file without connections is skipped
    And the valid project is still returned

  Scenario: Only files without connections exist
    Given the root contains only ".dbx.toml" files with no connections
    When the scanner scans the root
    Then the scan completes without panicking
    And the result is empty

  # --- Problem 2: deterministic connection selection ---

  Scenario: A file with several connections always picks the same one
    Given a ".dbx.toml" file defining connections "zeta" and "alpha"
    When the scanner scans the root
    Then exactly one project should be returned for that file
    And its name should be "alpha"
    And repeating the scan should always return "alpha"

  # --- Problem 3: deduplication ---

  Scenario: Same repo, same connection name and same DSN collapse to one entry
    Given two project directories inside the same git repository
    And both contain a ".dbx.toml" with connection "main" and the same resolved DSN
    When the scanner scans the root
    Then exactly one project should be returned for that connection
    And the surviving project should be the one with the shortest path

  Scenario: Same repo and name but different resolved DSN are both kept
    Given two project directories inside the same git repository
    And both contain a ".dbx.toml" with connection "main" but different DSNs
    When the scanner scans the root
    Then two projects should be returned
    And both should be kept

  Scenario: Different connection name in the same repo does not collapse
    Given two project directories inside the same git repository
    And each ".dbx.toml" defines a different connection name
    When the scanner scans the root
    Then both projects should be returned

  Scenario: Git worktrees of the same repository share one repo identity
    Given a main repository directory
    And a git worktree of that repository whose ".git" is a file pointing to the main repository's common git dir
    And both contain a ".dbx.toml" with connection "main" and the same resolved DSN
    When the scanner scans the root
    Then exactly one project should be returned
    And the surviving project should be the main repository directory
    And the worktree should not survive even if its path is shorter

  Scenario: Directories that are not a git repository do not deduplicate
    Given two directories outside any git repository
    And both contain a ".dbx.toml" with connection "main" and the same resolved DSN
    When the scanner scans the root
    Then two projects should be returned

  Scenario: The DSN is compared after environment expansion
    Given two entries whose raw DSNs differ as "${env:DBX_DSN}" versus the expanded value
    And the environment variable resolves to the same value
    When the scanner scans the root
    Then they should be treated as the same DSN and collapse to one entry

  # --- Determinism of output ---

  Scenario: Scan output order is stable
    Given several project directories
    When the scanner scans the root twice
    Then both scans should return the projects in the same order

  # --- Active/inactive state ---

  Scenario: Toggling state still works after deduplication
    Given a duplicated connection collapsed into one surviving project
    When the user marks the surviving project inactive
    Then the surviving project should be inactive
    And the next scan should reflect that state
