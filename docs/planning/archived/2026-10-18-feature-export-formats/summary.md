# Summary: export-formats

## Metadata
- **Completed:** 2026-10-18
- **Plan Number:** 0005
- **Change:** PR #34 (`feat/export-formats` → `main`)
- **Head:** `f232fe1`
- **Scope note:** one coherent delivery (feature + its hardening), 3802 insertions
  across 36 files against the ~400-line advisory. The split was offered and
  declined by the user: the hardening closes defects in the same export surface
  the feature introduces (`handleExport`'s write boundary), so a separate PR
  would have merged the feature with the defects still live.

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
| A DB-controlled schema or table cannot write outside the project | ✅ Passed |
| Sanitised and digest-shaped names stay distinct | ✅ Passed |
| A symlink at the export destination is refused | ✅ Passed |
| A very long identifier still exports | ✅ Passed |
| Delimited cells cannot inject a spreadsheet formula | ✅ Passed |
| Exported files are readable only by their owner | ✅ Passed |

19/19 scenarios PASS.

## Commits
Feature:
- `chore: add the export-formats plan`
- `feat(config): add export delimiter and compression keys`
- `feat(export): add Markdown, custom delimiters and gzip file exports`
- `docs(planning): update run progress`

Hardening (added after review; user decision):
- `fix(export): harden file paths, formula escaping and file mode`
- `docs(export): record the hardening addendum`
- `fix(export): close the symlink, mode, collision and clipboard gaps`
- `docs(export): record the second hardening review`
- `fix(export): restore the Windows build for symlink refusal`
- `fix(export): reserve the digest-shaped filename namespace`
- `test(cli): measure the context file mode under a known umask`
- `test(export): pin that the formula escape runs before gzip`
- `docs(export): record the platform fallback and the digest guarantee`
- `test(export): cover the non-hex digest-shape rejection`

## Files
- Created: `docs/adr/0003-export-formats-config-and-pipeline.md`,
  `docs/adr/0004-export-hardening-sanitisation-and-escaping.md`,
  `internal/config/export_options.go` (+ contract test),
  `internal/app/export_hardening_contract_test.go`,
  `internal/app/export_open_unix.go`, `internal/app/export_open_windows.go`
  (+ Windows test), `internal/cli/umask_{unix,windows}_test.go`,
  the archived planning directory.
- Modified: `internal/app/app.go` (+ export contract tests),
  `internal/config/{config,keybindings_actions}.go`,
  `internal/ui/components/grid/export_picker.go`,
  `README.md`, `docs/{PLAN,FEATURES,CONFIG}.md`, `CHANGELOG.md`,
  `~/.agents/skills/dbx/SKILL.md` (outside the repo).

## Gates
- `go build ./...`, `go vet ./...` — green.
- Cross-compiles: `GOOS=windows GOARCH=amd64` and `GOOS=darwin GOARCH=arm64` — green
  (the Windows one caught a real regression introduced by the symlink fix).
- `go test -count=1 ./...` with Docker — 25/25 packages.
- `make lint` (gofmt + golangci-lint v2.13.2) — 0 issues.
- Coverage: diff vs `main` 100.00% (364/364), project total 100.00% (6036/6036).
- `make install` — binary deployed.

## Security hardening (why it is in this PR)
- **Path containment:** DB-controlled `schema`/`table` are sanitised; a changed
  component gets a deterministic `-<hex6>` digest; the resolved destination is
  containment-checked; the file is opened with `O_NOFOLLOW` on Unix (Windows
  falls back to an `Lstat` check) and written through the fd.
- **Formula injection:** CSV/TSV cells starting with `= + - @ TAB CR` get a
  leading apostrophe, applied **only at the file-write boundary**; the clipboard
  and yank payloads stay byte-faithful.
- **File mode:** exports are created `0600` and re-`chmod`ed on overwrite;
  `dbx context -o` keeps its documented `0644`.

## Review
Four rounds (behavior + code/security/performance/docs lenses), each finding
verified empirically:
- Round 1: REJECT — 3 gating MEDIUM (unescaped Markdown headers, lone `\r`,
  ADR cost claim). Fixed.
- Round 2: PASS WITH FINDINGS.
- Round 3: 6 gating (symlink bypass HIGH, `0600` on overwrite, name collisions,
  clipboard leak) plus LOWs. Fixed; the user chose the apostrophe escape and
  keeping the fixes in #34.
- Round 4: adversarial verification found a HIGH regression (the `O_NOFOLLOW`
  fix broke the Windows build) and a digest-namespace collision. Fixed.
- Final verification by the orchestrator: build, vet, both cross-compiles,
  targeted hardening tests and the `umask 077` mode pin all green.

### Accepted trade-offs (deliberate, documented)
- A file export of `-1` becomes `'-1`, so formula-leading values do not
  round-trip exactly. The user approved this; numeric values are **not**
  special-cased.
- The escape is not injective (`-1` and a literal `'-1` both export as `'-1`).
- Ordinary identifiers keep their historical byte-for-byte file names; only
  changed or digest-shaped ones are renamed.

### Residual risks (documented in ADR 0004, not code changes)
Hard links and FIFOs at the destination, symlinked *parent* directories, the
narrow `Lstat`-then-open race on Windows, and 24-bit digest collisions.

### Pre-existing, still out of scope
SQL identifiers quoted with `%q` instead of doubled double quotes, the unguarded
`m.project` dereference in `handleExport`, and `os.WriteFile` overwriting a
same-named export.

## Next Step
Merge approval: integrate `feat/export-formats` into `main` (PR #34) once the
user confirms. The branch is republished onto current `origin/main` first.
