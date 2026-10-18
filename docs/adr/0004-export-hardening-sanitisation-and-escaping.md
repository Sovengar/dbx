# ADR 0004 — Export hardening: name sanitisation, containment at open time and file-boundary escaping

- **Status**: Accepted
- **Date**: 2026-10-18
- **Context**: feature `export-formats` (hardening addendum)

## Context

The export-formats change ([ADR 0003](0003-export-formats-config-and-pipeline.md)) was
reviewed and approved, but the review left three security-shaped findings on record
against `main`. They are not defects in the new surface — they predate it — but the PR
touches this code and the export path is where they land, so they are fixed here before
the merge:

1. **Path traversal.** `exportFilename` builds `<schema>_<table>.<ext>` and
   `handleExport` joins it onto `m.project.Path`. Both components come from the
   database. A schema of `x/../../y` escaped the project (reproduced: it wrote
   `/home/u/y_t.csv`), which is an arbitrary file write driven by the queried data.
2. **Spreadsheet formula injection.** A CSV/TSV cell whose string form begins with
   `=`, `+`, `-`, `@`, TAB or CR is executed as a formula when the file is opened in
   Excel, LibreOffice or Sheets. A dbx export carries data the user did not author.
3. **World-readable files.** Exported files were written `0644`.

A second review on that fix found it incomplete in three places, folded into the
decisions below: containment was lexical but the write followed a symlink at the
destination, the `0600` permission applied only at creation, and the sanitiser collided
distinct identifiers. The formula escape also leaked into the clipboard, which is a
correctness bug rather than a security one.

The questions worth recording are not "is this a bug" but "how many independent
defences, and what does escaping cost".

## Decision

### 1. Three independent layers against path traversal

Sanitisation makes the name safe, containment makes an unsafe name harmless even if
sanitisation is ever wrong, and the open refuses to follow a link at the destination.

- `sanitizeExportComponent` replaces every path separator (`/`, `\`) and every control
  character in a schema or table name with `_`, maps a component that sanitises to
  exactly `.` or `..` to `_`, and truncates a component longer than 100 bytes. A
  component that had to be changed, or whose unchanged text already ends in `-` plus
  six hex digits, carries `-` plus the first six hex characters of the sha256 of the
  ORIGINAL name. The digest shape is reserved, so `a/b`, `a_b`, `a<TAB>b` and the
  unchanged `a_b-c14cdd` stay distinct, and a long name still fits the filesystem
  limit. An ordinary (non-digest-shaped) identifier keeps its historical byte-for-byte
  name, so `public_users.csv` is still exactly `public`/`users`.
- After the join, `writeExportFile` requires the destination to stay under the project
  directory, expressed with `filepath.Rel`: a result of `..` or one starting with `..`
  plus a separator is outside. A violation is refused through the same error the handler
  already shows and is logged through `debuglog`; nothing is written.
- The write opens the destination itself through `openNoFollow` and writes through that
  descriptor. On Unix `O_NOFOLLOW` makes the kernel refuse a symlink at the destination
  with `ELOOP`; on Windows, which has no such flag, the destination is checked with
  `os.Lstat` and refused if it is a symlink before a normal open. `isSymlinkRefusal`
  maps both refusals to the same path as a lexical escape, and the bytes land in the
  file that was checked rather than the file a link points at. On Unix this closes the
  check→write TOCTOU window a lexical-only check leaves open; the Windows two-step
  leaves a narrow race, recorded below.

None of the layers is redundant. Sanitisation is what keeps a legitimate export working
under a name that is merely odd; containment is the backstop for a separator or a
platform quirk the sanitiser did not know about; the open-time check is what a lexical
check cannot see.

### 2. Formula escaping is an apostrophe, at the file boundary only

A delimited cell whose string form starts with `=`, `+`, `-`, `@`, TAB or CR gets one
leading `'` when the export is written to a FILE. That is what spreadsheet documentation
recommends and what makes the cell literal text without dropping or rewriting the value.
The rule applies to every field the file serialiser writes, header included, because a
column name is also a cell and is also database-controlled.

The escape is applied at the write boundary (`exportAsEscapedCSV`, before gzip), not in
the exporter. The clipboard and `y` yank payloads stay byte-faithful, so pasting `-1`
into a SQL editor is `-1`, not `'-1`. There is no config key. The safe behaviour is the
default, not something a user has to find; a key would trade a security property for an
opt-in. SQL, JSON and Markdown are not spreadsheet input and are not touched — prefixing
them would corrupt the payload and, for SQL, break the statement.

The escape is deliberately NOT type-aware: a numeric `-1` is escaped exactly like a
string `"-1"`, because both are equally executable as a formula. It is also not
injective: `-1` and the literal string `'-1` both export as `'-1`. Both are accepted
costs, recorded in the Consequences.

### 3. Owner-only file mode

Grid exports are written `0600` instead of `0644`. `dbx context -o` is a different
surface and is deliberately left alone; this decision is about the data the grid
exports.

## Alternatives considered

- **Sanitisation only, no containment check.** A sanitiser has to know every separator
  on every platform, and a single miss is an arbitrary write. The whole point of the
  layers is that one of them can be incomplete. Rejected.
- **Containment only, no sanitisation.** A traversal name would be refused and the user
  loses the export; a merely odd but safe name would also be refused. Sanitisation is
  what keeps the export working. Rejected.
- **Reject the export on any suspicious name.** A schema called `a/b` is a legitimate
  (if unusual) identifier, and turning a successful query into a failed export is worse
  than a sanitised file name. Rejected.
- **Prefix a TAB or a space instead of an apostrophe.** Those are the other commonly
  suggested escapes, but they change the value more and are not universally honoured by
  spreadsheets. Rejected.
- **Strip or quote the value.** Quoting does not stop a spreadsheet from evaluating the
  formula; stripping destroys data. The apostrophe is the least destructive option that
  works. Rejected.
- **Escape only `=`, not `+`/`-`/`@`.** `+`, `-` and `@` are documented formula lead-ins
  too, and `-1` is a realistic value from a numeric column. Rejected.
- **Make the escape type-aware (`-` only for a string).** The value arrives as
  `interface{}`, and a string `"-1"` from a text column is exactly as executable as a
  numeric `-1`; a type check would either still escape the string (no change) or leak the
  numeric case. Rejected. The non-injectivity this leaves is recorded below.
- **A config toggle for escaping.** An opt-in security control is off where it matters.
  Rejected.
- **Extend ADR 0003 instead of a new record.** 0003 answers "where do the delimiter and
  the compression live, and where does compression happen" — a config and pipeline
  boundary. This is a different decision: which side of the trust boundary the database
  name and the exported value are on, and how many layers guard the filesystem. Folding
  it into 0003 would blur its title and make the two concerns harder to find. Rejected.

## Consequences

- **Round-trip cost, accepted and documented.** `-1` is exported to a FILE as `'-1`, so a
  numeric string that begins with `-` no longer round-trips byte for byte through a dbx
  CSV/TSV export and re-import. The same applies to a value or column name beginning with
  `=`, `+`, `@`, TAB or CR. This is the price of not executing the cell; it is stated in
  `docs/CONFIG.md` and the changelog.
- **The escape is not injective, also accepted.** `-1` and the literal string `'-1` both
  export as `'-1`, so a round trip through the file cannot tell them apart. Making the
  rule type-aware would re-open the injection for a string that merely starts with a
  digit, and dropping it for strings would drop the protection, so the non-injectivity is
  the recorded trade.
- **The clipboard is exempt.** The escape runs at the file boundary only, so `y`/copy of
  `-1` puts `-1` on the clipboard and the paste-into-editor flow is not corrupted.
- The historical names under ordinary identifiers are unchanged: `public_users.csv`,
  `.tsv`, `.md`, `.json`, `.sql` and their `.gz` forms still come from the same one
  helper. A component that had to be changed, or that already had the digest shape, gets
  `-` plus a six-hex digest. The suffix is 24 bits, so two crafted original names can
  still collide; the guarantee is that ordinary identifiers keep their names and the
  digest-shaped namespace stays reserved, not that no collision is possible. A component
  over 100 bytes is truncated and keeps the digest so a long identifier exports instead
  of failing with `ENAMETOOLONG`.
- Files written before this change keep their `0644` mode only if they are never
  rewritten; a re-export chmods the file to `0600`, because the permission argument alone
  applies only at creation.
- Adding a format is still the six sites listed in ADR 0003; the hardening lives inside
  the existing exporters, the filename helper and the write boundary, so it adds none.

### Residual filesystem risks

Containment is not airtight, and this ADR does not claim it is. The open guards the
final component; it deliberately does not change these pre-existing behaviours:

- **Hard links.** A hard link at the destination is a regular file to both `open` and
  `os.Lstat`, so neither `O_NOFOLLOW` nor the Windows check refuses it. The write lands
  in the linked inode.
- **FIFOs.** A FIFO at the destination is not a regular file; opening or writing it can
  block or fail. The export path does not special-case it.
- **Symlinked parent directories.** Only the final component is inspected. A project
  directory reached through a link, or a destination whose parent is a link, is outside
  this check; the lexical check still applies to the name inside the project.
- **The Windows fallback race.** Windows has no `O_NOFOLLOW`, so `Lstat` and the normal
  open are separate operations. A link swapped in between them is a narrow race the
  Unix path does not have.
- **The 24-bit digest.** A crafted pair of original names can share the six-hex suffix.
  Reserving the digest shape prevents the easy crafted case; it does not widen the
  suffix.
