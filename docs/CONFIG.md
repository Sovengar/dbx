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

Every executed statement is appended to `<dir>/YYYY-MM-DD.jsonl`, one NDJSON
object per line: `query` entries (`sql`, `duration_ms`, `rows`) and `error`
entries. The TUI, `dbx query`, `dbx pipe` and `dbx ask` all write; `connect`
entries come from the TUI only. `dbx replay` reads the same files.

`enabled = false` disables the writer without disabling any command.
`retention_days` prunes older daily files (the TUI sweeps at startup, the CLI as
it opens the log). A log that cannot be opened disables logging and never fails
the command.

### `ui`

```toml
[ui]
page_size = 100        # rows per grid page
yank_max_rows = 10     # clipboard if the selection is <= this, else a file
export_delimiter = ","  # separator of the delimited export: one char, or "tab"/"\t"
export_compress = false # gzip exports that go to a file (never the clipboard)
nerd_font = true       # Nerd Font glyphs for key hints
```

Defaults: `page_size = 100`, `yank_max_rows = 10`, `export_delimiter = ","`,
`export_compress = false`, `nerd_font = true`. Set
`nerd_font = false` to render key hints as plain text (`enter`/`esc`/`tab`/
`ctrl+d`) for terminals without a Nerd Font.

`export_delimiter` accepts one literal character (`;`, `|`, `,`) or the aliases
`"tab"` / `"\t"` for the tab character; a tab writes a real tab byte and names
the file `.tsv`, any other delimiter keeps the `.csv` extension. A value that is
empty, longer than one character, or unusable for CSV (`"`, newline, carriage
return, NUL, invalid UTF-8) falls back to `,` instead of failing the export.

`export_compress = true` gzips every export that goes to a **file** (SQL, JSON,
delimited and Markdown) and appends `.gz` after the format extension, so a CSV
becomes `schema_table.csv.gz`. Clipboard copies and yanks are never compressed.
Repeating an export overwrites the same name.

A delimited cell whose string form begins with `=`, `+`, `-`, `@`, a tab or a
carriage return is prefixed with an apostrophe **in the exported file only**, because
a spreadsheet would otherwise execute it as a formula. Clipboard copies and the `y`
yank keep the value byte-faithful, so yanking `-1` copies `-1`. The cost is accepted
and recorded in ADR 0004: `-1` exports as `'-1` and no longer round-trips byte for
byte, the escape is not injective (`-1` and a literal `'-1` both export as `'-1`),
and the same applies to a column name that begins with one of those characters. SQL,
JSON and Markdown are not spreadsheet input and are not escaped.

The exported file name is built from the schema and table; path separators and
control characters in either name are replaced with `_`. A name that had to be changed,
or whose unchanged text already ends in `-` plus six hex digits, carries a `-` plus a
short digest: ordinary identifiers keep their historical names, `a/b`, `a_b`,
`a<TAB>b` and the crafted `a_b-c14cdd` stay distinct, and a residual 24-bit collision
between crafted names is still possible. A name too long for the filesystem is
truncated and keeps the digest. The resolved file is refused (and logged) if it would
fall outside the project directory, or if the destination is a symlink (Unix refuses
to follow it with `O_NOFOLLOW`; Windows checks it with `Lstat` before opening). Hard
links, FIFOs and symlinked parent directories remain known residual risks. Exported
files are written owner-only (`0600`), including when an existing file is overwritten.

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
