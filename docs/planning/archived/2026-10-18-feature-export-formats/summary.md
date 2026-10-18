# Summary: export-formats

## Metadata
- **Completed:** 2026-10-18 16:34
- **Duration:** ~50 minutes
- **Plan Number:** 0005
- **Change:** PR #34 (`feat/export-formats` → `main`)

## Scenarios
| Scenario (behavior.feature) | Status |
|-----------------------------|--------|
| Markdown export of a single row goes to the clipboard as a table | ✅ Passed |
| Markdown export of several rows writes a .md table file | ✅ Passed |
| Markdown escapes pipes and newlines and renders NULL as an empty cell | ✅ Passed |
| Markdown truncates ragged rows so the table stays valid | ✅ Passed |
| The delimited export uses the configured delimiter | ✅ Passed |
| A tab delimiter writes real tab bytes and a .tsv file | ✅ Passed |
| A value containing the delimiter is quoted | ✅ Passed |
| An unknown or empty delimiter falls back to a comma | ✅ Passed |
| Compression writes a valid gzip file with a .gz name | ✅ Passed |
| Compression is off by default and leaves files untouched | ✅ Passed |
| Compression applies to every file format | ✅ Passed |
| Clipboard and yank exports are never compressed | ✅ Passed |
| The existing SQL, JSON and CSV defaults are unchanged | ✅ Passed |

13/13 scenarios PASS.

## Commits
- `chore: add the export-formats plan`
- `feat(config): add export delimiter and compression keys`
- `feat(export): add Markdown, custom delimiters and gzip file exports`
- `docs(planning): update run progress`

History finalised to one unit per concern (default shape policy): `""`, `config`,
`export`, `planning`. Content tree identical to the reviewed tip `603c030`.

## Files
- Created:
  - `docs/adr/0003-export-formats-config-and-pipeline.md`
  - `docs/planning/0005-feature-export-formats/` (behavior.feature, context.md, issue.md, plan.md, diagrams/feature-flow.md, diagrams/process-flow.md)
  - `internal/config/export_options.go`
  - `internal/config/export_options_contract_test.go`
- Modified:
  - `README.md`, `docs/CONFIG.md`, `docs/FEATURES.md`, `docs/PLAN.md`
  - `internal/app/app.go`, `internal/app/export_contract_test.go`, `internal/app/handle_export_contract_test.go`
  - `internal/config/config.go`, `internal/config/config_contract_test.go`, `internal/config/keybindings_actions.go`
  - `internal/ui/components/grid/export_picker.go`, `internal/ui/components/grid/export_picker_test.go`
  - `~/.agents/skills/dbx/SKILL.md` (outside the repo)

## Tests
- Added: 20 (16 in `internal/app`, 1 in `internal/config`, 3 in `internal/ui/components/grid`)
- System Tests: ✅ Passed (`make test` — 25/25 packages, 100% statement coverage, race enabled)

## Documentation
- Changelog: ✅ Updated (`CHANGELOG.md`, Unreleased → Added)
- Docs: `docs/FEATURES.md`, `docs/CONFIG.md`, `docs/PLAN.md`, `README.md`, `~/.agents/skills/dbx/SKILL.md`
- ADR: ✅ Created — `docs/adr/0003-export-formats-config-and-pipeline.md`

## Code Review Issues
- Critical Found: 0
- High Found: 0
- Medium Found: 0 (gating)
- User Decision: Approved to continue

### Remaining findings — LOW, record-only (not fixed, by decision)
- **N1** `docs/adr/0003-export-formats-config-and-pipeline.md:39` — still says csv's
  invalid-`Comma` error is one "the exporters ignore"; they now log it.
- **N2** `docs/adr/0003:52` — still says `gzipExport` has no error branch beyond
  `os.WriteFile`; the `writeGzip` seam now logs write/close errors.
- **L1** `docs/CONFIG.md:122` — reject list omits U+FFFD (rejected by
  `validDelimiter` despite being valid UTF-8).
- **L2** `internal/app/export_contract_test.go:993` — asserts the failing sink
  path but never observes the log; deleting both `appDebugLog` calls leaves it green.
- **L2'** `internal/app/export_contract_test.go:1017` — asserts only the empty
  return, not the log line.
- **L3** `internal/app/app.go:3145,3148` — `writeGzip` logs under the tag
  `gzipExport:`, naming a different function.
- **L4** `internal/app/app.go:3113` — the comment on `case grid.ExportSQL:` states
  the rationale backwards.
- **L5** `docs/planning/0005-feature-export-formats/context.md:25+` — absolute
  paths point at the `main` checkout, not this worktree.
- **N3** `internal/app/app.go:3100-3117` — `exportFilename`'s `default` logs but
  still returns a `.sql` name.
- **L6** `README.md:25` / `plan.md` — documentation beyond the approved plan
  scope (accepted, harmless and accurate).

### Pre-existing on `main` — NOT part of this change (needs a separate issue)
- Path traversal via unsanitised schema/table in `exportFilename` →
  `filepath.Join` (reproduced: schema `x/../../y` writes outside the project dir).
- CSV/TSV formula injection (`=`, `+`, `-`, `@`).
- `os.WriteFile(..., 0644)` on exported data.
- Byte-identical shape on `main`; the delta adds no reachability. **Do not fold
  into #34** — file a separate issue against `main`.

## Next Step
Merge approval: integrate `feat/export-formats` into `main` (PR #34) once the
user confirms. The branch is synced onto current `origin/main` first.
