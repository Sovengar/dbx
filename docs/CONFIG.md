# dbx Configuration Reference

Two different config files exist, with **different shapes**:

- **Global config** — `~/.config/dbx/config.toml` (respects the OS config dir).
  Created automatically on first run if missing. Documented below.
- **Project config** — `.dbx.toml` in a project directory. See
  [Project config](#project-config-dbxtoml).

## Global config

### `theme`

```toml
[theme]
# system | dark | light | nord | gruvbox | catppuccin
mode = "system"
```

Default: `system`. An unknown value also falls back to `system`.

### `keybindings.custom`

Override any default keybind, keyed by **action ID**:

```toml
[keybindings.custom]
"ask" = "ctrl+a"
"edit_cell" = "enter"
"navigate_down" = "ctrl+j"
```

- Each action maps to a **single key string** (not a list). The override
  **replaces every key** of that action.
- Map an action to `""` to unbind it.
- Press `?` inside dbx for the full action list. Unknown IDs are accepted but do
  nothing (they have no handler).

### `connections`

An **array of tables**. Used by the CLI (`-c/--connection`) and the project
scanner.

```toml
[[connections]]
name = "local-dev"
provider = "postgres"
url = "postgres://user:pass@localhost:5432/myapp"

[[connections]]
name = "staging"
provider = "postgres"
host = "staging.example.com"
port = 5432
database = "myapp"
user = "postgres"
password_env = "PGPASSWORD"   # read the password from an env var
read_only = true
```

`url` (full DSN) takes precedence over the individual `host`/`port`/… fields.

### `ai`

```toml
[ai]
provider = "opencode-go"   # default
model = "mimo-v2.5"        # default

[ai.providers.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4-20250514"

[ai.providers.openai]
api_key_env = "OPENAI_API_KEY"
model = "gpt-4o"
```

`ai.providers` is a free-form map; each entry declares the env var holding the
API key and an optional model. Used by both the TUI ASK pane and `dbx ask`.

### `session`

```toml
[session]
enabled = true
dir = "/tmp/dbx/sessions"
retention_days = 30
```

Defaults: `enabled = true`, `dir = $TMPDIR/dbx/sessions`, `retention_days = 30`.

> **Not implemented.** dbx does not write session logs. `dir` is only the search
> path used by `dbx replay`; `enabled` and `retention_days` are inert. Executed
> queries are recorded instead in the per-project query history that backs the
> query browser (`~/.local/state/dbx/projects/{project}/query_history.json`).

### `ui`

```toml
[ui]
page_size = 100        # rows per grid page
yank_max_rows = 10     # clipboard if the selection is <= this, else a file
nerd_font = true       # Nerd Font glyphs for key hints
```

Defaults: `page_size = 100`, `yank_max_rows = 10`, `nerd_font = true`. Set
`nerd_font = false` to render key hints as plain text (`enter`/`esc`/`tab`/
`ctrl+d`) for terminals without a Nerd Font.

With `nerd_font = true`, key hints render as glyphs in the keybinds pane, the
`?` help modal and the inline footers: `enter` `esc` `tab` `space` `backspace`
`delete` `up` `down` `left` `right` `home` `end` `pgup` `pgdn`, and modifier
combinations collapse to a symbol (`ctrl+enter` renders as `⌃`). F-keys
(`f1`–`f9`) stay as text because the pane compresses the run into one token.

The glyphs come from the Material Design set, so they require a Nerd Font that
ships the newer `nf-md-*` codepoints. Nerd Fonts v3+ builds do; older
"Complete" variants (for example `Caskaydia Cove Nerd Font Complete`) do not,
and the terminal will fall back to another installed Nerd Font for those cells.

The following keys are accepted but **inert** (kept for backward compatibility;
nothing reads them):

```toml
[ui]
statusbar = true          # the statusbar was replaced by the keybinds pane
statusbar_help = true
history_size = 100
query_history_path = "~/.local/state/dbx/query_history.json"
```

### `editor`

```toml
[editor]
autocomplete = true          # real-time autocomplete popup
autocomplete_trigger = 1     # min chars before it opens on its own (0 = always)
```

Defaults: `autocomplete = true`, `autocomplete_trigger = 1`. `Ctrl+Space` always
opens the popup manually. See
[Features → Editor](FEATURES.md) for the clause-aware behavior.

## Project config (`.dbx.toml`)

Placed in a project root. Uses a **different shape** from the global config:

```toml
[connections.local-dev]
driver = "postgres"
dsn = "postgres://localhost/mydb"
# ssh_tunnel = "bastion"   # optional
```

- Keyed by connection name (`[connections.<name>]`), with `driver` and `dsn`.
- The `dsn` supports env expansion: `${env:VAR}` and `$VAR`.
- This is the format the TUI's connection picker discovers by scanning `~/dev`.

## Environment variables

**Not supported.** `DBX_*` environment overrides are not applied — viper's
automatic env binding is not wired into the config unmarshal, so setting e.g.
`DBX_AI_PROVIDER` has no effect. Configure through the files above.

## Defaults & priority

For the global config there is a single source: the config file, with built-in
defaults for anything unset (no env or global-flag overrides). The only CLI-level
override is `-c/--connection`, which selects a connection per command.
