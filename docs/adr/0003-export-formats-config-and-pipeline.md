# ADR 0003 — Export formats: config keys and the compression boundary

- **Status**: Accepted
- **Date**: 2026-10-18
- **Context**: feature `export-formats`

## Context

The grid's export picker (`x`) covered SQL, JSON and CSV, went to the clipboard for a
single row and to a file for several, and fixed the delimited format to a comma. Three
capabilities were missing: a Markdown table, a delimiter the user can choose (`;`,
tab, `|`), and compression of the files it writes.

Two questions had no obvious answer and shaped the whole surface:

1. **Where do the delimiter and the compression live?** A config key, an interactive
   second step inside the picker, or a flag.
2. **Where does compression happen?** Inside each exporter, or at the boundary where
   the file is written.

The answers are hard to change later: they decide the exporters' signatures, who owns
the filename, and whether the grid has to read config.

## Decision

### 1. Preferences, not a picker step: `ui.export_delimiter` and `ui.export_compress`

Both live in `internal/config` under `[ui]`, next to `ui.yank_max_rows`, the key that
already governs the export flow. The picker stays a one-step list of formats; it gains
a fourth option (Markdown) and nothing else. `ExportSelectedMsg` is unchanged: it still
carries format, data and `YankMode` only, and the grid still reads no config.

`ExportDelimiterRune(value string) rune` normalises the value to the single rune
`encoding/csv` accepts: one literal character, or the aliases `tab` / `\t`. Anything
else — empty, multi-character, or a rune csv rejects (`"`, `\r`, `\n`, NUL,
`utf8.RuneError`) — degrades to `,` instead of failing, the same way `theme.Resolve`
falls back to a safe value. A delimiter a user mistyped must not silently produce an
empty export, which is exactly what csv does with an invalid `Comma` (it reports an
error the exporters ignore).

### 2. The delimited exporters take a `rune`

`exportAsCSV(result, comma)` / `exportRowAsCSV(columns, row, comma)` assign the rune
to `csv.Writer.Comma`. Quoting stays `encoding/csv`'s job; nothing is hand-rolled. The
extension follows the same normalised rune: tab → `.tsv`, any other delimiter → `.csv`.

### 3. Compression at the write boundary, once

Exporters stay pure and `string`-returning. `handleExport` reads the config once,
serialises, and then either copies (clipboard / yank, never compressed) or names the
file, gzips the bytes and writes them. `gzipExport([]byte) []byte` is a pure in-memory
compression with no streaming writer and no error branch beyond `os.WriteFile`.

### 4. One owner for the filename

`exportFilename(schema, table, format, comma, compress)` decides the extension
(`.sql`, `.json`, `.csv`, `.tsv`, `.md`) and appends `.gz` after it. Name and content
are built from the same format and the same rune, so they cannot diverge.

### 5. `ExportMarkdown` is appended to the enum

The numeric values `ExportSQL`/`ExportJSON`/`ExportCSV` (0/1/2) do not move, so any
message in flight keeps its meaning. `updateDescriptions` derives each option's wording
from its own `format` instead of `options[0..2]`, so a new option cannot inherit the
previous one's description.

## Alternatives considered

- **A delimiter step inside the picker.** A second screen is a state machine, more
  surface to test and exactly the picker redesign the spec rules out. Rejected.
- **A new `[export]` config section.** `ui.yank_max_rows` already governs the export
  flow and the grid is a UI surface; two config boundaries for one component is the
  kind of split this repository has already paid for. Rejected.
- **A flag on the export message carrying the delimiter/compression.** Pushes config
  into the grid and changes a closed message the sweep and picker tests round-trip.
  Rejected.
- **Compressing inside each exporter.** Four copies of the same decision and an
  `[]byte` return that the clipboard path does not want; gzip is orthogonal to the
  format. Rejected.
- **A streaming gzip writer around `os.WriteFile`.** More moving parts for a buffer
  that already exists in memory; the serialised export is a string. Rejected.
- **Aborting on an invalid delimiter.** A bad preference would break exports entirely;
  degrading to `,` keeps the data flowing. Rejected.
- **Storing a delimiter string in the exporters.** `encoding/csv` takes one rune;
  normalising once at the config boundary means the exporters cannot hold a value csv
  cannot use. Rejected.

## Consequences

- Adding a format is six sites: one enum value, one `noun()` case, one picker option,
  both `handleExport` content switches, and one `exportFilename` extension arm.
  `updateDescriptions` derives its wording from `noun()` and needs no edit.
- The SQL/JSON/CSV outputs under the default configuration are byte-for-byte unchanged
  (pinned by a literal-output test) and their extensions are the historical ones (pinned
  separately by the filename test).
- Clipboard copies and yanks are never compressed by construction: only the file branch
  of `handleExport` calls `gzipExport`.
- `os.WriteFile` still overwrites, and a compressed export can sit next to an
  uncompressed one. That is pre-existing behaviour — overwriting the same name is
  documented in `docs/CONFIG.md` — and collision handling is out of scope.
- `docs/FEATURES.md`, `docs/CONFIG.md`, the `export` keybind description and the global
  dbx skill describe the new surface.
