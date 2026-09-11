# dbx Configuration Reference

## Location

Default: `~/.config/dbx/config.toml`

Can be overridden with `--config` flag or `DBX_CONFIG` env var.

## Complete Config Example

```toml
# ═══════════════════════════════════════════════════════════════
# dbx Configuration File
# ═══════════════════════════════════════════════════════════════

# ─── Version ───────────────────────────────────────────────────
# Config version (for migrations)
version = "1.0"

# ─── Theme ─────────────────────────────────────────────────────
[theme]
# Theme mode: "system", "dark", "light", "nord", "gruvbox", "catppuccin"
# "system" auto-detects terminal colors
mode = "system"

# Force dark/light mode (overrides system detection)
# mode_lock = "dark"

# Custom color overrides (optional)
# [theme.colors]
# primary = "#89b4fa"
# secondary = "#f38ba8"
# accent = "#a6e3a1"
# background = "#1e1e2e"
# foreground = "#cdd6f4"
# error = "#f38ba8"
# warning = "#fab387"
# success = "#a6e3a1"
# info = "#89dceb"

# ─── Keybindings ───────────────────────────────────────────────
[keybindings]
# Mode: "vim", "modern", "emacs"
mode = "vim"

# Custom keybind overrides
# Format: "action.name" = "key" or ["key1", "key2"]
[keybindings.custom]
# Global
# "global.quit" = "q"
# "global.help" = "?"
# "global.palette" = ":"
# "global.ask" = "a"
# "global.export" = "e"
# "global.refresh" = "r"

# Explorer
# "explorer.expand" = ["enter", "l"]
# "explorer.toggle_columns" = "space"
# "explorer.collapse" = ["backspace", "h"]
# "explorer.new" = "n"
# "explorer.drop" = "d"
# "explorer.ddl" = "v"
# "explorer.filter" = "f"

# Grid
# "grid.edit" = ["enter", "i"]
# "grid.delete" = "d"
# "grid.insert" = "o"
# "grid.yank" = "y"
# "grid.sort" = "s"
# "grid.filter" = "/"
# "grid.next_page" = "n"
# "grid.prev_page" = "p"

# Editor
# "editor.execute" = ["ctrl+enter", "ctrl+r"]
# "editor.clear" = "ctrl+u"
# "editor.external" = "ctrl+g"
# "editor.history_prev" = "ctrl+p"
# "editor.history_next" = "ctrl+n"

# ─── Connections ───────────────────────────────────────────────
# Connections can be defined here or via CLI

[[connections]]
name = "local-dev"
provider = "postgres"
host = "localhost"
port = 5432
database = "myapp_dev"
user = "postgres"
# Password: use password_env to read from environment variable
password_env = "PGPASSWORD"
# Or use password_file to read from file
# password_file = "/path/to/password"

[[connections]]
name = "staging"
provider = "postgres"
# Full URL overrides individual fields
url = "postgres://user:pass@staging.example.com:5432/myapp"
# Read-only mode blocks INSERT, UPDATE, DELETE, DROP
read_only = true

[[connections]]
name = "local-sqlite"
provider = "sqlite"
# Path to database file
path = "./data/local.db"

# ─── SSH Tunnels ───────────────────────────────────────────────
# Define reusable tunnel configs

[tunnels]
[tunnels.bastion]
host = "bastion.example.com"
user = "deploy"
key = "~/.ssh/id_rsa"

[tunnels.k8s]
host = "k8s-master.example.com"
user = "admin"
key = "~/.ssh/k8s_key"
port_forward = true

# Reference tunnels in connections
# [[connections]]
# name = "prod-via-bastion"
# provider = "postgres"
# url = "postgres://..."
# tunnel = "bastion"

# ─── AI Configuration ─────────────────────────────────────────
[ai]
# Provider: "anthropic", "openai", "deepseek", "qwen"
# If not set, auto-detects from available API keys
provider = "anthropic"

# Default model (can be overridden per provider)
model = "claude-sonnet-4-20250514"

# Temperature for generation (0.0 - 1.0)
temperature = 0.3

# Max tokens for response
max_tokens = 2048

# Schema context inclusion
# "full" = all tables, columns, types, indexes
# "smart" = relevant tables only (based on prompt)
# "none" = no schema context
schema_context = "smart"

# Provider-specific config
[ai.providers.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4-20250514"
# base_url = "https://api.anthropic.com"  # For proxies

[ai.providers.openai]
api_key_env = "OPENAI_API_KEY"
model = "gpt-4o"
# base_url = "https://api.openai.com/v1"

[ai.providers.deepseek]
api_key_env = "DEEPSEEK_API_KEY"
model = "deepseek-chat"
# base_url = "https://api.deepseek.com"

[ai.providers.qwen]
api_key_env = "DASHSCOPE_API_KEY"
model = "qwen-turbo"

# ─── Session Logs ──────────────────────────────────────────────
[session]
# Enable/disable session logging
enabled = true

# Log directory
dir = "~/.config/dbx/sessions"

# Retention in days (0 = keep forever)
retention_days = 30

# Log format: "jsonl" (JSON Lines) or "json"
format = "jsonl"

# Include SQL in logs (set false for sensitive data)
include_sql = true

# Include query results in logs
include_results = false

# ─── UI Configuration ─────────────────────────────────────────
[ui]
# Show contextual statusbar at bottom
statusbar = true

# Show keybind hints in statusbar
statusbar_help = true

# Default page size for grids
page_size = 100

# Maximum page size
max_page_size = 10000

# Show row numbers in grid
row_numbers = true

# Truncate long cell values (true) or wrap (false)
truncate_cells = true

# Maximum cell width before truncation
max_cell_width = 50

# Show NULL as special value
show_null = true

# NULL display string
null_display = "NULL"

# Date format
date_format = "2006-01-02 15:04:05"

# Enable mouse support
mouse = true

# Animation enabled
animations = true

# Border style: "rounded", "normal", "double", "thick", "none"
border_style = "rounded"

# ─── Explorer ──────────────────────────────────────────────────
[explorer]
# Sort tables by: "name", "rows", "size"
sort_by = "name"

# Show column details in tree
show_columns = false

# Show indexes in tree
show_indexes = false

# Show foreign keys in tree
show_foreign_keys = false

# Auto-expand first schema
auto_expand_first = false

# Icon set: "unicode", "nerd", "ascii"
icons = "unicode"

# ─── Grid ──────────────────────────────────────────────────────
[grid]
# Auto-refresh interval (seconds, 0 = disabled)
auto_refresh = 0

# Show filters row
show_filters = true

# Show sort indicators
show_sort = true

# Zebra striping (alternate row colors)
zebra = true

# Selected row highlight style: "line", "background", "both"
highlight_style = "both"

# Edit mode: "inline", "modal"
edit_mode = "inline"

# Confirm before delete
confirm_delete = true

# ─── Editor ────────────────────────────────────────────────────
[editor]
# Syntax highlighting
highlight = true

# Tab size
tab_size = 4

# Auto-complete enabled
autocomplete = true

# Auto-complete trigger characters
autocomplete_trigger = 3

# External editor command (if empty, disabled)
# Uses $SQL_EDITOR, $EDITOR, or $VISUAL
external_editor = ""

# Max history size
history_size = 100

# Preserve editor content on execute
preserve_on_execute = false

# ─── Export ────────────────────────────────────────────────────
[export]
# Default format: "csv", "json", "sql", "markdown"
default_format = "csv"

# CSV options
[export.csv]
delimiter = ","
quote = '"'
escape = '"'
header = true

# JSON options
[export.json]
indent = 2
pretty = true

# SQL options
[export.sql]
# INSERT, UPDATE, or COPY
type = "INSERT"
# Include column names
columns = true

# ─── Query ─────────────────────────────────────────────────────
[query]
# Default limit if none specified
default_limit = 1000

# Maximum rows to fetch
max_rows = 100000

# Query timeout in seconds
timeout = 30

# Auto-commit transactions
auto_commit = true

# ─── Logging ───────────────────────────────────────────────────
[logging]
# Level: "debug", "info", "warn", "error"
level = "info"

# Log file (empty = stderr)
file = ""

# Max size in MB before rotation
max_size = 10

# Max rotations to keep
max_rotations = 5

# ─── Advanced ──────────────────────────────────────────────────
[advanced]
# Connection pool size
pool_size = 5

# Connection timeout in seconds
connect_timeout = 10

# SSH timeout in seconds
ssh_timeout = 30

# Use prepared statements
prepared_statements = true

# Binary format for data transfer
binary_format = false
```

## Environment Variables

All config values can be overridden with env vars:

```bash
# General
DBX_CONFIG=/path/to/config.toml
DBX_THEME=dark
DBX_KEYBINDINGS_MODE=modern

# Connection
DBX_CONNECTION_NAME=local-dev
DBX_DATABASE_URL=postgres://...

# AI
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
DEEPSEEK_API_KEY=...
DASHSCOPE_API_KEY=...

# Session
DBX_SESSION_DIR=/path/to/sessions

# UI
DBX_UI_MOUSE=false
DBX_UI_STATUSBAR=true
```

## Config Priority

1. CLI flags (highest)
2. Environment variables
3. Config file
4. Built-in defaults (lowest)

## Example Configs

### Minimal
```toml
[theme]
mode = "system"

[[connections]]
name = "dev"
provider = "postgres"
url = "postgres://localhost/mydb"
```

### Full Featured
See complete example above.

### CI/CD Mode
```toml
[theme]
mode = "dark"

[ui]
statusbar = false
mouse = false

[session]
enabled = false

[query]
timeout = 5
```
