# dbx

[![CI](https://github.com/Sovengar/dbx/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Sovengar/dbx/actions/workflows/ci.yml)

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases designed for both humans and AI agents.

## Features

The complete feature inventory and behavior reference lives in
[docs/FEATURES.md](docs/FEATURES.md), organized by CLI command, TUI pane, and
cross-cutting conventions. For the current keybindings, press `?` inside dbx.

## Dependencies

- **Go 1.26+**
- **PostgreSQL** (via pgx)
- **Clipboard support** (optional, for copy to clipboard):
  - **Linux (Wayland)**: `wl-clipboard` (`wl-copy`/`wl-paste`)
  - **Linux (X11)**: `xclip` or `xsel`
  - **macOS**: built-in `pbcopy`
  - **Windows**: built-in `clip.exe`

## Installation

```bash
# From source
go install github.com/buble/dbx@latest

# Or download from releases
gh release download buble/dbx
```

## Quick Start

```bash
# Launch the TUI — the connection is chosen from the picker
dbx

# The picker lists the projects found by scanning ~/dev for .dbx.toml

# Query mode (no TUI)
dbx query "SELECT * FROM users LIMIT 10" --json

# Bulk COPY export/import (headered CSV)
dbx export public.users -o users.csv.gz
dbx import public.users users.csv.gz
cat users.csv | dbx import public.users

# Natural language
dbx ask "show me all active users"
```

## Keybinds

Press `?` inside dbx to open the help overlay. It lists every keybind, grouped
by view, plus the widget modes (EDIT, FILTER, WHERE FILTER, query browser,
connection picker) and mouse actions.

The same registry that renders that overlay also drives execution, so what you
see is exactly what runs. Override any action under `[keybindings.custom]` in
your config, keyed by action ID.

The status-bar keybind pane shows the primary key of each action and collapses
sibling actions into one segment (e.g. `hjkl Navigate`, `f1-f9 Go to Page`);
the `?` overlay is where every alias stays visible.

## Configuration

### Project config (`.dbx.toml`)

Place in your project root:

```toml
[connections.local-dev]
driver = "postgres"
dsn = "postgres://localhost/mydb"

[connections.staging]
driver = "postgres"
dsn = "postgres://staging.example.com/mydb"
ssh_tunnel = "staging-bastion"
```

`ssh_tunnel` is an OpenSSH host or alias. dbx forwards to the host and port in
the DSN through that target, then connects PostgreSQL to an ephemeral loopback
address. Configure keys, agent access, host verification, and any `ProxyJump`
in `~/.ssh/config`; authentication must be non-interactive.

### Global config (`~/.config/dbx/config.toml`)

```toml
[theme]
mode = "system"

[keybindings.custom]
"ask" = "ctrl+a"

[ai]
provider = "anthropic"

[session]
enabled = true
retention_days = 30
```

See [docs/CONFIG.md](docs/CONFIG.md) for full reference.

## CLI Reference

```bash
# Query with JSON output
dbx query "SELECT count(*) FROM users" --json

# List tables
dbx list tables --json

# Export the full schema as JSON for agents
dbx context --json > schema.json

# Pipe mode
echo "SELECT 1" | dbx pipe --json
```

See [Features → Part 1 — CLI](docs/FEATURES.md) for the command reference.

## Documentation

- [Features](docs/FEATURES.md) — CLI and TUI features by pane, plus conventions
- [Architecture](docs/ARCHITECTURE.md) — Design decisions and patterns
- [Decisions (ADR)](docs/decisions/) — Architecture decision records
- [Config](docs/CONFIG.md) — Configuration reference

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

### Server-enforced READ ONLY verification

The ASK pane's SELECT-only guarantee is only *authoritative* when PostgreSQL
rejects mutations inside the READ ONLY transaction — the client-side validator
is defense in depth, not the guarantee.

The proving tests (`TestAsk_ReadOnlyTxRejectsDML`,
`TestAsk_ReadOnlyTxRejectsSelectBasedMutation`) run **automatically**: when
`DBX_TEST_DSN` is unset they start a real PostgreSQL via
[testcontainers-go](https://golang.testcontainers.org/) (`postgres:16-alpine`),
so a plain `go test ./...` verifies the guarantee wherever Docker is available
(including CI ubuntu runners, which ship Docker). If Docker is unavailable the
tests skip with a clear message.

To reuse an existing database instead of starting a container, set
`DBX_TEST_DSN` (fast path, no container):

```bash
DBX_TEST_DSN='postgres://user:pass@localhost:5432/db' go test ./internal/app -run TestAsk -v
```

Either way this is a **mandatory verification step**, not optional.

**Accepted tradeoff:** testcontainers-go pulls ~50 indirect Go modules for a
test-only need, and the tests require Docker at run time. When Docker is
unavailable they skip with a clear message instead of failing, so the suite
still runs on machines without it. The alternative (a hand-rolled Docker
invocation or a committed Postgres service) was rejected as more brittle.
