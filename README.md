# dbx

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases, built with [Bubbletea v2](https://github.com/charmbracelet/bubbletea) and designed for both humans and AI agents.

## Features

- 🎨 **Beautiful UI** — Auto-adapts to your terminal theme, modern design with Lipgloss v2
- 🖱️ **Full mouse support** — Click to navigate, edit cells, scroll, everything is clickable
- ⌨️ **Intuitive keybinds** — First-letter convention (`a`sk, `c`hange, `d`elete, `u`pdate)
- 🤖 **AI-native** — Natural language to SQL, session logs, CLI for agents
- 🐘 **PostgreSQL** — Full support with schemas, tables, views, indexes
- 📊 **Data grid** — Inline editing, sorting, filtering, pagination
- 🔍 **Schema explorer** — Interactive tree with click-to-expand
- ✏️ **SQL editor** — Syntax highlighting, autocomplete, execute queries
- 📦 **CLI mode** — Query, export, and automate from scripts
- 🎭 **Multiple themes** — System, dark, light, nord, gruvbox, catppuccin

## Dependencies

- **Go 1.22+**
- **PostgreSQL** (via pgx)
- **Clipboard support** (optional, for copy to clipboard):
  - **Linux (Wayland)**: `wl-clipboard` (`wl-copy`/`wl-paste`)
  - **Linux (X11)**: `xclip` or `xsel`
  - **macOS**: built-in `pbcopy`
  - **Windows**: built-in `clip.exe`

```bash
# Wayland (Hyprland, Sway, GNOME, etc.)
sudo apt install wl-clipboard    # Ubuntu/Debian
sudo pacman -S wl-clipboard      # Arch

# X11
sudo apt install xclip           # Ubuntu/Debian
sudo pacman -S xclip             # Arch
```

## Installation

```bash
# From source
go install github.com/buble/dbx@latest

# Or download from releases
gh release download buble/dbx
```

## Quick Start

```bash
# Connect to PostgreSQL
dbx postgres://localhost/mydb

# Or use connection name from config
dbx --connection local-dev

# Query mode (no TUI)
dbx query "SELECT * FROM users LIMIT 10" --json

# Natural language
dbx ask "show me all active users"
```

## Keybinds

dbx uses an **action-based keybind system** where the primary key is the first letter of the action:

| Key | Action |
|-----|--------|
| `a` | Ask AI (Natural language to SQL) |
| `c` | Change (edit cell) |
| `d` | Delete (row or table) |
| `e` | Export data |
| `f` | Filter |
| `g` | Go to (first/last) |
| `i` | Insert |
| `n` | Next page |
| `o` | Open/Insert row |
| `p` | Previous page |
| `q` | Quit |
| `r` | Refresh |
| `s` | Sort |
| `u` | Update |
| `v` | View DDL |
| `y` | Yank (copy) |

Press `?` anywhere to see all keybinds.

## Configuration

Config file: `~/.config/dbx/config.toml`

```toml
[theme]
mode = "system"

[keybindings]
mode = "vim"  # or "modern" or "emacs"

[[connections]]
name = "local-dev"
provider = "postgres"
url = "postgres://localhost/mydb"

[ai]
provider = "anthropic"
```

See [docs/CONFIG.md](docs/CONFIG.md) for full reference.

## AI Integration

### Natural Language to SQL

Press `a` or run:

```bash
dbx ask "show me users who signed up last week"
```

dbx generates SQL, shows it for review, then executes on confirmation.

### Session Logs

Every query is logged to `~/.config/dbx/sessions/`:

```bash
# View session
cat ~/.config/dbx/sessions/2026-09-11.log

# Replay session
dbx replay ~/.config/dbx/sessions/2026-09-11.log
```

### Schema Context for LLMs

Export schema for AI agents:

```bash
dbx context --json > schema.json
```

## CLI Reference

```bash
# Query with JSON output
dbx query "SELECT count(*) FROM users" --json

# List tables
dbx list tables --json

# Export table
dbx export users --format csv --output users.csv

# Pipe mode
echo "SELECT 1" | dbx pipe --json
```

See [docs/CLI.md](docs/CLI.md) for full reference.

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — Design decisions and patterns
- [Keybinds](docs/KEYBINDS.md) — Complete keybind reference
- [Config](docs/CONFIG.md) — Configuration reference
- [CLI](docs/CLI.md) — CLI command reference
- [Plan](docs/PLAN.md) — Development plan and phases

## Development

```bash
# Clone
git clone https://github.com/buble/dbx.git
cd dbx

# Build
make build

# Run
make run

# Test
make test

# Lint
make lint
```

## Roadmap

- [ ] Phase 0: Setup and foundation
- [ ] Phase 1: Core PostgreSQL (explorer, grid, editor)
- [ ] Phase 2: AI integration (NL→SQL, session logs)
- [ ] Phase 3: Multi-database (MySQL, SQLite, MongoDB)
- [ ] Phase 4: Advanced features (SSH tunnels, COPY, etc.)

See [docs/PLAN.md](docs/PLAN.md) for detailed phases.

## License

MIT

## Acknowledgments

- [Bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — Styling
- [Bubbles](https://github.com/charmbracelet/bubbles) — Components
- [lazysql](https://github.com/jorgerojas26/lazysql) — Inspiration
- [pgsavvy](https://github.com/davesavic/pgsavvy) — Keybind ideas
