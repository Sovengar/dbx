# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Grid export (`x`) can now write a Markdown table, use a configurable delimiter (`ui.export_delimiter`, with `.tsv` for tab) and gzip file exports (`ui.export_compress`); clipboard and yank exports are never compressed.
- Session logging now covers the CLI: `dbx query`, `dbx pipe` and `dbx ask` append the statements they execute to the daily session log, so `dbx replay` sees CLI sessions too.

### Fixed
- Empty the SQL editor buffer after a successful run so the first `ctrl+p` recalls the just-executed query.
- Render the explorer pane at exactly its assigned height instead of overflowing by two lines.

### Security
- Harden grid export against database-controlled names and spreadsheet formulas: path separators and control characters in the schema/table file name are replaced with `_` and a destination outside the project is refused (and logged), a delimited cell that begins with `=`, `+`, `-`, `@`, tab or CR is prefixed with an apostrophe (so `-1` exports as `'-1` and no longer round-trips byte for byte), and exported files are written owner-only (`0600`).
- Tighten the grid export hardening: the write now opens the destination with `O_NOFOLLOW` and writes through that descriptor, so a symlink planted at the path is refused (closing the check→write TOCTOU) and `ELOOP` takes the same refusal path; re-exporting applies `0600` to an existing file instead of leaving its old mode; a sanitised schema/table component carries a short digest so distinct names cannot collide and an overlong component is truncated instead of failing with `ENAMETOOLONG`; and the spreadsheet formula escape moved to the file only, so clipboard and yank payloads stay byte-faithful.
