---
feature: 0005-feature-export-formats
freshness: cf7ca7ac4ae36a71aa3c8049f89449fecd75dcbe
codegraph: ready
generated_by: codebase-researcher
---

# Context: Export formats — Markdown, custom delimiter, file compression

All paths absolute. Every line number below was read at commit
`cf7ca7ac4ae36a71aa3c8049f89449fecd75dcbe` (branch `feature/export-formats`).

## Scope

- **In**: a 4th picker option (Markdown) with descriptions derived from `ExportFormat`; a
  `ui.export_delimiter` rune fed to `encoding/csv` (`.tsv` when tab); `ui.export_compress`
  gzip'ing file writes only, with `.gz` in the name; tests; docs.
- **Out**: any CLI surface, any redesign of `startExport`/`startYank`/`yankMaxRows`,
  any change to the SQL/JSON/CSV byte output under default config, new dependencies.

## Files to Touch

| Symbol / Area | File | Lines | Why |
|---|---|---|---|
| `UIConfig` | `/home/buble/dev/projects/dbx/internal/config/config.go` | 106–131 (fields at 107–129) | Add `ExportDelimiter string \`mapstructure:"export_delimiter"\`` and `ExportCompress bool \`mapstructure:"export_compress"\`` next to `YankMaxRows` (L109). |
| `setDefaults` | `/home/buble/dev/projects/dbx/internal/config/config.go` | 187–206 (`ui.*` block 197–203) | `v.SetDefault("ui.export_delimiter", ",")` and `v.SetDefault("ui.export_compress", false)` beside `ui.yank_max_rows` (L201). Defaults are what keeps today's bytes unchanged. |
| **new** pure normalizer, suggested `ExportDelimiterRune(value string) rune` | **new file**: `/home/buble/dev/projects/dbx/internal/config/export_options.go` (config already splits by concern: `project.go`, `scanner.go`, `repoid.go`) | new | Literal rune, or aliases `tab` / `\t`, else fallback `,`. Must reject what `encoding/csv` rejects — see Risks. |
| `ExportFormat` enum | `/home/buble/dev/projects/dbx/internal/ui/components/grid/export_picker.go` | 14–20 | `ExportMarkdown` **after** `ExportCSV` (numeric values 0/1/2 must not move). |
| `ExportSelectedMsg` | same file | 22–30 | **Do not change.** The message carries `Format` only; delimiter/compression come from config in `app`. |
| `NewExportPicker` options slice | same file | 54–64 (options literal 58–62) | Add the 4th `exportOption` `{label: "Markdown", format: ExportMarkdown}`; label must be exactly 8 chars (see Integration Points). |
| `updateDescriptions` | same file | 66–81 | Rewrite to derive the description from `opt.format` + mode (verb `Copy`/`Save`, noun per format: SQL→`INSERT`, JSON→`JSON`, CSV→`CSV`, Markdown→`Markdown`). Do **not** index `options[0..3]`. |
| `handleKey` / `enter` | same file | 146–178 (`enter` arm 162–177) | No change expected (`selected := ep.options[ep.cursor]` already generalises), but it is the boundary that must keep emitting `Format`. |
| `View` (label width, footer) | same file | 180–238 (`Width(8)` L212, footer L223) | Read only: label column is `Width(8)` and `"Markdown"` is exactly 8; footer says `enter copy` even in file mode (pre-existing wart, tests do not assert it — leave it). |
| `Model.handleExport` | `/home/buble/dev/projects/dbx/internal/app/app.go` | 2719–2787 | Becomes orchestration: read config nil-safely (suggested helpers `exportDelimiter() rune`, `exportCompress() bool`), pass the rune to the delimited exporters, add Markdown arms, gzip at the write boundary. |
| — single-row `Format` switch | same | 2722–2737 | Add `case grid.ExportMarkdown`; pass comma to `exportRowAsCSV`; **never compress here** (clipboard). |
| — multi-row `Format` switch | same | 2740–2757 | Add `case grid.ExportMarkdown`; pass comma to `exportAsCSV`. |
| — YankMode clipboard | same | 2759–2765 | Untouched behaviour; must stay uncompressed. |
| — filename switch + write | same | 2767–2783 | Replace the 3-arm `fmt.Sprintf("%s_%s.csv", …)` switch (2768–2776) with one helper (suggested `exportFilename(schema, table string, format grid.ExportFormat, comma rune, compress bool) string`); gzip `[]byte(content)` before `os.WriteFile` at L2779. |
| `usableWidth` | same | 2790–2818 | Reused as-is by Markdown (header + separator + rows all truncated to this width). |
| `rowWidth` | same | 2820–2823 | Reused as-is by `exportRowAsMarkdown`. |
| `exportAsCSV` **signature change** | same | 2894–2919 | `func exportAsCSV(result *postgres.QueryResult, comma rune) string`; set `writer.Comma = comma`. Callers: `app.go:2756` and `/home/buble/dev/projects/dbx/internal/app/export_contract_test.go:477,497,516,537,556,588`. No other caller in the repo. |
| `exportRowAsCSV` **signature change** | same | 2953–2965 | `func exportRowAsCSV(columns []string, row []interface{}, comma rune) string`. Callers: `app.go:2730` and `export_contract_test.go:567`. No other caller. |
| **new** `exportAsMarkdown` / `exportRowAsMarkdown` / cell escaper | same (insert after L2965, beside the other pure exporters) | new | Mirror the CSV pair: `usableWidth`/`rowWidth`, header row, separator row, one row per record, `\` → `\\`, `|` → `\|`, newline → `<br>`, `nil` → empty cell. Return `string`, ignore builder errors. |
| **new** `gzipExport(data []byte) []byte` | same (same insertion point) | new | Pure `compress/gzip` over `[]byte` (`bytes.Buffer`). No streaming, no error branch. |
| **new** `exportFilename` | same | new | Owns extension (`sql`/`json`/`csv`/`tsv`/`md`) and appends `gz` after it → `public_users.csv.gz`. Must not be duplicated inside the switch. |
| `exportDoneMsg` | same | 3064–3069 | No new fields: the compressed name already travels in `filename`, and the toast at 1672–1685 renders it. |
| export keybind description | `/home/buble/dev/projects/dbx/internal/config/keybindings_actions.go` | 135 | `Description: "Export (SQL/JSON/CSV)"` → include Markdown. Registry is the single source for the `?` modal, palette and pane; no parallel lists (AGENTS.md). |
| Grid feature entry | `/home/buble/dev/projects/dbx/docs/FEATURES.md` | 169–183 (stale claim at 178–179) | "writes SQL/JSON/CSV" → SQL/JSON/CSV/Markdown + delimiters + compression; mention the two `ui.*` keys and that clipboard/yank are never compressed. |
| `[ui]` config reference | `/home/buble/dev/projects/dbx/docs/CONFIG.md` | 98–109 (block 100–105, defaults sentence 107–109) | Add the two active keys with comments + defaults. **Not** the "inert keys" block at 122–131. |
| dbx skill | `/home/buble/.agents/skills/dbx/SKILL.md` | 14–30 (Hard Rules), 32–42 (Decision Gates) | No export surface exists there today (`grep -i export` matches only the frontmatter/references). Add one concise bullet: `x` picker → SQL/JSON/CSV/Markdown, `ui.export_delimiter` (`,`/`;`/`\|`/`tab`/`\t`, tab ⇒ `.tsv`), `ui.export_compress` (gzip, `.gz`, files only). |
| optional, not in the approved plan | `/home/buble/dev/projects/dbx/README.md` | 25–26 | Still says "Copy the selected row (SQL/JSON/CSV)". Docs-only; flag to the planner rather than expanding scope silently. |

Test files (absolute paths):

| File | Lines | What |
|---|---|---|
| `/home/buble/dev/projects/dbx/internal/app/export_contract_test.go` | helpers at 56–70 (`fakeClipboard`), CSV block 471–590, CSV helpers tail 600–666 | Update the 7 `exportAsCSV`/`exportRowAsCSV` calls for the new `rune`; add Markdown (`TestTheExportedMarkdownIsValidTable`) and gzip (`TestGzipRoundTrips`) cases here. |
| `/home/buble/dev/projects/dbx/internal/app/handle_export_contract_test.go` | `clipboardStub` 27–41, `clipboardContents` 43–50, `exportModel` 60–66, single-row table 55–130 (CSV row 85–96), multi-row table 132–218 (`wantExt` rows 139/155/165), yank 219–275, `foundProjectIn` 349–352 | Add Markdown to both tables; add `.tsv`/`.gz`/`delimiter`/`nil-config` cases. Mutate the model's config in-place (`m := exportModel(t); m.config.UI.ExportDelimiter = ";"`, `m.config.UI.ExportCompress = true`) — `NewModel` keeps the pointer, `handleExport` reads it per call, so no new constructor is needed. |
| `/home/buble/dev/projects/dbx/internal/ui/components/grid/export_picker_test.go` | 39 (`const last = 2`), 80 (`ep.cursor = 2`), 114 (`len(ep.options)` — already dynamic), 138 (`cursor < 3`), 156 (`labels := []string{"SQL","JSON","CSV"}`), 170 (`wantFormats` map), 330–334 (`Copy`/`Save` word lists), 366 (`ep.cursor = 2`) | All the hardcoded 3s become 4; add `Copy Markdown to clipboard` / `Save Markdown to file` to the word lists; add a test that the CSV/Markdown descriptions do not desync from their option (the positional bug). |
| `/home/buble/dev/projects/dbx/internal/config/config_contract_test.go` | 169–190 (defaults table, `ui.yank_max_rows` at 184) | Add `ui.export_delimiter`/`ui.export_compress` to the defaults table. |
| `/home/buble/dev/projects/dbx/internal/config/export_options_contract_test.go` (new) | — | Table-driven normalizer cases: `""`, `","`, `";"`, `"|"`, `"tab"`, `"\t"` (two chars), a literal tab, `"ab"`, `"\n"`, `"` + `"`, `"\\"`. |
| `/home/buble/dev/projects/dbx/internal/app/handlers_contract_test.go` | 39–45 (`withClipboard` seam), 128–175, 546–566 (`usableWidth`) | Reference for the clipboard seam; `usableWidth` tests stay as-is. |

## Contracts

1. **Exporters are pure and `string`-returning** (`app.go:2825/2875/2894/2921/2943/2953`).
   No writer, no `error` return, no compression inside them. Errors from
   `strings.Builder` / `csv.Writer` stay ignored, with the existing comment style.
2. **`ExportSelectedMsg` is a closed contract** (`export_picker.go:22–30`): grid → app
   carries format + data + `YankMode` only. Config is *not* plumbed through it.
3. **The grid never reads config.** `Grid` holds `yankMaxRows` (set via
   `SetYankMaxRows`, `table.go:1392–1398`); anything new is read in `app`, which owns
   `m.config` (`app.go:65`, wired at `app.go:131–185`).
4. **`handleExport` must be nil-safe on `m.config`** (`", "` / `false`). All present
   fixtures pass `&config.Config{}` (zero `UI`), so an empty delimiter is a *reachable*
   value in the existing suite and must degrade to `,`.
5. **Filename/extension rules**: `.sql`, `.json`, `.csv` today (`app.go:2768–2776`);
   `.md` new; normalized rune `'\t'` ⇒ `.tsv`, every other delimiter ⇒ `.csv`;
   compression appends `.gz` after the base extension. Extension and content are
   decided from the same normalized rune, in one helper.
6. **Three description branches stay three** (`yankMode`, `fileMode`, clipboard
   fallback) but the *content* comes from the format, not the index; the verb is
   `Copy` for yank/clipboard and `Save` for file mode.
7. **No new CLI surface**: `internal/cli` untouched; `dbx context` (schema export) untouched.

## Pattern to Follow

- **Ragged rows** → `usableWidth` (`app.go:2790–2818`) / `rowWidth` (2820–2823) and its
  use in `exportAsCSV` (2894–2919: header truncated to `width`, each record
  `make([]string, width)`). Markdown's header, separator and every row must be built at
  that same width.
- **Config key wiring** → `ui.yank_max_rows`: field at `config.go:109`, default at
  `config.go:201`, test at `config_contract_test.go:184`. Config normalisation with a
  safe fallback → `theme.Resolve`'s `default:` arm (`/home/buble/dev/projects/dbx/internal/theme/theme.go:38–55`).
- **Delimited output** → `exportAsCSV` itself: `csv.NewWriter(&sb)`, `writer.Flush()`,
  `sb.String()`. The only added line is `writer.Comma = comma`; do not hand-roll quoting.
- **Contract test style** → `/home/buble/dev/projects/dbx/internal/app/handle_export_contract_test.go`
  (one table per behaviour, `t.Run` per scenario, a `// Scenario: …` comment quoting
  `behavior.feature`), plus `exportModel`/`clipboardStub`/`clipboardContents`/
  `foundProjectIn` for I/O. Config→effect (both halves of a switch) →
  `/home/buble/dev/projects/dbx/internal/app/config_wiring_test.go:1–30` and its
  "off draws nothing / on draws it" pairing.
- **Gzip round-trip test**: `gzip.NewReader` + `io.ReadAll` and compare with
  `exportAsMarkdown(...)` output; assert the `.gz` suffix from `filepath.Ext`.

## Tests

- **Existing tests that will break and must be updated**: every `exportAsCSV` /
  `exportRowAsCSV` call in `/home/buble/dev/projects/dbx/internal/app/export_contract_test.go`
  (L477, 497, 516, 537, 556, 567, 588) and the picker's hardcoded option counts
  (`export_picker_test.go:39,80,138,156,170,366`).
- **Must keep passing untouched**: `TestTheExportedSQLIsRunnable` (198–319),
  `TestTheExportedJSONIsReadable` (383–472), `TestTheUsableWidthIsTheNarrowestRow`
  (`handlers_contract_test.go:546–566`), the message sweep folding
  `grid.ExportSelectedMsg` (`/home/buble/dev/projects/dbx/internal/app/message_contract_test.go:130–137`),
  `exportDoneMsg` folding (`message_translation_contract_test.go:307–330`), and
  `/home/buble/dev/projects/dbx/internal/app/routing_and_layout_contract_test.go:58,463,592–600`.
- **Framework / runners** (run from `/home/buble/dev/projects/dbx`):
  - `go test ./internal/app/... ./internal/ui/components/grid/... ./internal/config/...`
  - `go test ./internal/app/ -run 'Export|handleExport|UsableWidth|Yank' -count=1`
    (export tests never call `testDSN`, so no container is started for them)
  - `go test ./...` / `make test` (`-race -count=1 -cover`)
  - `make lint` (gofmt + golangci-lint v2.13.2), `go vet ./...`
  - coverage: `make coverage` then `scripts/diff-coverage.sh coverage.out main`
    (or `make coverage-check`); total floor `scripts/coverage-floor` = `100.00`
  - mutation: `make mutate-diff` (`scripts/mutate.sh --diff`); survivors go to
    `/home/buble/dev/projects/dbx/.mutation-allowlist` **by `file:line`**, hangs to
    `.mutation-timeouts` with a per-file ceiling.
- **Integration infra**: unit only for all export tests. `internal/app`'s shared
  PostgreSQL container is started lazily by `testDSN`
  (`/home/buble/dev/projects/dbx/internal/app/testdb_test.go:92–134`) and only by the
  DML/load integration tests. `DBX_SKIP_DOCKER=1` is the explicit opt-out for the whole
  package when Docker is unavailable; `TestMain` (`testdb_test.go:30–36`) does *not*
  start a container, it only sandboxes the clipboard.
- **Suggested mapping scenario → test**:
  - Markdown single row / multi-row / escaping / raggedness → `export_contract_test.go`
    (pure) + `handle_export_contract_test.go` (clipboard vs `.md` file).
  - delimiter / tab / quoting / fallback → `export_options_contract_test.go` +
    `export_contract_test.go` (read back with `csv.Reader{Comma: …}`) +
    `handle_export_contract_test.go` (`wantExt` `.tsv`).
  - compression on/off/all formats/clipboard-never → `handle_export_contract_test.go`
    (gzip round-trip on `done.filename`) + a `m.config = nil` case for the nil arm.
  - backwards compatibility → existing SQL/JSON/CSV tests plus the `wantExt` rows.

## Conventions & Boundaries

- `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `tea.KeyPressMsg`,
  `View()` returns `tea.View` — that is for the app layer only; the picker's `View()`
  returns `string` (component, rendered into the overlay at `app.go:3224–3228`).
- Test naming: `TestSubject_BehaviorInASentence` / `TestASentenceLongEnoughToRead`;
  a `// Scenario: …` comment above the test quoting `behavior.feature` (English there),
  then English explanatory prose (the linter runs `misspell`; every prose comment in the
  repo is English even when the `Scenario:` line is Spanish).
- `interface{}`, not `any`, in the export code (`[]interface{}`, `map[string]interface{}`).
- New imports needed in `app.go` (top block, `app.go:3–30`): `bytes`, `compress/gzip`
  (stdlib only, no `go.mod` change).
- Keybind changes: registry entry only (`keybindings_actions.go`); nothing else, or
  `/home/buble/dev/projects/dbx/internal/config/keybindings_test.go` and
  `internal/app/keybind_coverage_test.go` fail.
- Debug logging is never removed; not needed for this feature.

## Integration Points (non-obvious)

1. **`updateDescriptions` is positional today** (`export_picker.go:66–81`): three
   `options[0..2]` assignments, and `View()` calls it on every render (`L185`). A 4th
   option would silently leave Markdown with its constructor-era "Copy …" text in file
   mode. Deriving from `opt.format` inside a loop fixes it structurally.
2. **Label column is `Width(8)`** (`export_picker.go:212`) inside a `Width(48)` bordered
   box (`L230`). `"Markdown"` is exactly 8 chars and fits; descriptions must stay short
   (`Copy Markdown to clipboard` = 26 chars is the longest and fits the 46 usable
   columns). A longer label would reflow the box.
3. **Single row always goes to the clipboard** (`msg.Row != nil`, `app.go:2722`). The grid
   picks the picker mode before the message exists (`table.go:1057–1076`): exactly one
   selected row → `Show` (clipboard mode, L1072); more than one, or none (export all) →
   `ShowFileMode` (L1064/L1075) with `Rows` set and `Row` nil. So a row that reaches the
   clipboard arm never touches the file branch: compression is file-only *by
   construction*; only `app.go:2767–2783` compresses.
4. **`handleExport` runs inside `Update`** (`app.go:1669–1670`) and is folded by the
   message sweep on a model with **no project** and a sandboxed clipboard
   (`message_contract_test.go:204–250`). That path takes the single-row clipboard arm and
   must not reach `m.project.Path` or the compress branch.
5. **All fixtures pass `NewModel(&config.Config{})`** (`routerModel`,
   `router_contract_test.go:42–49`), so `UI.ExportDelimiter` is `""` in every existing
   test: the `""` → `","` fallback is load-bearing for the current suite, not just for
   user typos.
6. **`encoding/csv` silently rejects a bad `Comma`**: `writer.Write` returns an error that
   this code ignores (`app.go:2906, 2914`), so an invalid rune yields an **empty export**
   rather than a crash. The normalizer must exclude what `csv.validDelim` excludes: `0`,
   `'"'`, `'\r'`, `'\n'`, `utf8.RuneError`, plus any multi-rune value. `'"'` is the
   surprising one.
7. **`yankMaxRows` decides yank-vs-file in the grid** (`table.go:1003`) — untouched, but it
   is why a many-row yank arrives with `YankMode: true` and must skip compression.
8. **The clipboard is sandboxed for the whole `internal/app` package** (`TestMain` →
   `sandboxClipboard`, `testdb_test.go:39–75`): a copy succeeds silently and writes
   nothing. Any "clipboard is uncompressed" assertion must inspect payloads through
   `clipboardStub` (`handle_export_contract_test.go:27–41`) or replace the
   `copyToClipboard` seam via `withClipboard` (`handlers_contract_test.go:39–45`).
9. **`SKILL.md` has no export surface at all today** — the doc change is an addition, not
   an edit of an existing bullet.
10. **Docs drift already present**: `docs/PLAN.md:88` ("Grid export covers SQL, JSON and
    CSV … Missing") and `README.md:25–26`. Neither is in the approved doc list.

## Risks / Assumptions

- **Enum order**: `ExportMarkdown` must be appended (value 3). Inserting it earlier would
  reinterpret any message in flight (`ExportSelectedMsg.Format` is an int).
- **Do not add fields to `ExportSelectedMsg`** (e.g. delimiter/compress): the message sweep
  and picker tests enumerate/round-trip it, and the grid would then need config.
- **Empty delimiter is not a user error in tests** — see Integration Point 5. If the
  normalizer returns something other than `,` for `""`, the entire existing CSV suite
  changes behaviour.
- **Ragged Markdown**: header, separator and rows must all be built at `usableWidth`, or
  the table does not render (behavior.feature scenario 4). A separator row with the wrong
  cell count is the same failure and is not caught by a `strings.Contains` assertion —
  count cells per line.
- **`gzip` determinism and mutation**: `gzip.NewWriter` writes a zero `ModTime` header, so
  output is reproducible; a round-trip test (decompress and compare) is what kills a
  mutant that drops `Write`/`Close`.
- **Diff coverage 100% is a hard gate**: the `m.config == nil` arm *and* the `'"'`/multi-rune
  fallback arms need explicit tests, or the gate goes red on lines nobody thought were
  reachable.
- **`os.WriteFile` overwrites** (existing behaviour): repeating an export rewrites
  `public_users.csv`, and `public_users.csv.gz` can coexist with a plain `.csv`. Documenting,
  not fixing, is the approved scope.
- **Keybind description length** is rendered in the `?` modal, palette and keybinds pane
  (`keybindings_groups.go:68` uses `a.Description` as the grouped `Label`). No test pins the exact string, but keep it
  short (`Export (SQL/JSON/CSV/Markdown)`), and re-run `make lint`/grid tests after.
- **Picker footer** still reads `enter copy` in file mode (L223). Changing it is out of
  scope and would touch an assertion-free but user-visible string.
- **`docs/FEATURES.md`** carries a prose test (`internal/app/docs_test.go:38–46`) that only
  checks for `DML Transactions` / `Action Naming Convention`, so the grid entry can be
  edited freely — but keep both strings.
