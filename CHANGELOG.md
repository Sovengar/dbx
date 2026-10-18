# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Session logging now covers the CLI: `dbx query`, `dbx pipe` and `dbx ask` append the statements they execute to the daily session log, so `dbx replay` sees CLI sessions too.

### Fixed
- Empty the SQL editor buffer after a successful run so the first `ctrl+p` recalls the just-executed query.
- Render the explorer pane at exactly its assigned height instead of overflowing by two lines.
