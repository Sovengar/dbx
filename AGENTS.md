# dbx

> **Database x** — TUI database client with native AI integration.

## Build & Install

```bash
make build    # Compile to .local/bin/dbx
make install  # Copy to ~/.local/bin/dbx
make test     # Run tests
make lint     # Run linter
```

**IMPORTANT**: After ANY code change, run `make install` to rebuild and install the binary.

## Stack

- **Go 1.22+** with Bubbletea v2 (TUI)
- **Lipgloss v2** for styling
- **pgx v5** for PostgreSQL
- **Cobra** for CLI
- **Viper** for config

## Structure

```
cmd/dbx/          # CLI entry (cobra)
internal/
  app/            # App lifecycle, router, messages
  config/         # Viper config, keybindings
  theme/          # Theme system, styles
  ui/components/  # explorer, grid, editor, palette
  drivers/postgres/ # PostgreSQL driver
  ai/             # session logs, NL→SQL, context
  cli/            # CLI commands
pkg/client/       # Go library for agents
```

## Conventions

- Use `charm.land/*` import paths for bubbletea, lipgloss, bubbles v2
- View() returns `tea.View` struct, not string
- Mouse mode: `v.MouseMode = tea.MouseModeCellMotion`
- Key events: `tea.KeyPressMsg`
