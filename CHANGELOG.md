# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `--require <path>` on `validate` (repeatable) — after a source parses,
  also assert each path resolves in at least one of its documents. A
  well-formed source missing a required path is reported the same way a
  parse failure is (labeled by source, checking continues, exit status
  nonzero).
- `--indent N` on `set` / `append` / `delete` — spaces per indent level.
  Left unset, it's auto-detected from the source document (a 4-space file
  stays 4-space instead of being silently reflowed to yaml.v3's default of
  2), falling back to 2 for a flat document with nothing to detect from.
- `-0`/`--print0` on `get`/`keys`/`len`/`type` — NUL-separate multiple
  results instead of newline (no separator after the last one), for
  `xargs -0`. Implies raw output; combining it with an explicit non-raw
  `-o` is an error.
- `yaymlq rename <path> <newkey> [file]` — rename a mapping key in place,
  keeping its position, value, and comments. `<path>` must resolve to a
  mapping key; renaming to an existing sibling name is an error, and
  renaming a key to its own name is a no-op. Shares `set`'s
  `-i/--in-place`, `--doc`, and `--max-bytes` flags and its atomic write
  path.
- `-q`/`--quiet` on `get` — no output either way, exit `0` on a match or `1`
  otherwise (mirrors `grep -q`). Implies the same soft missing-path handling
  as `-e`.
- `set` / `append` / `delete` / `rename` now preserve CRLF line endings. A
  source that consistently uses `\r\n` is re-serialized with `\r\n`; a
  source that already uses plain `\n`, or mixes the two, is unaffected.

### Changed

- **Breaking:** distinct process exit codes per error class, instead of
  everything but `-e`'s "no match" collapsing to a flat `1`. `0` success,
  `1` no match / soft failure (`-e`, `validate`'s aggregate failure —
  unchanged), `2` the input YAML didn't parse, `3` a bad flag, argument
  count, path expression, or value, `4` a file or stream couldn't be read
  or written. A script that only checked "exit code `0`" is unaffected; one
  that relied on every failure being exit `1` needs updating.

## [0.5.0] - 2026-09-14

### Added

- `yaymlq validate [file...]` — check that each input is well-formed YAML
  (syntax only, no schema). Reads stdin with no arguments; with multiple
  files, all are checked even after an earlier one fails, and the exit
  status is nonzero if any did.

### Changed

- `yaymlq` with no arguments now prints help and exits `1`, instead of
  cobra's generic "accepts between 1 and 2 arg(s), received 0" error.

## [0.4.1] - 2026-09-01

### Changed

- `set` / `append` / `delete` now keep the blank lines from the source
  document. `gopkg.in/yaml.v3` discards them on decode; yaymlq re-inserts a
  blank line before any node that had one above it in the input (a run of blank
  lines collapses to one). Comment text, key order, and quoting were already
  preserved; comment-to-value spacing is still normalized to a single space.

## [0.4.0] - 2026-09-01

### Added

- `yaymlq append <path> <value> [file]` — add `<value>` as the last element of
  the list at `<path>`. Shares `set`'s `-i/--in-place`, `-s/--string`, `--doc`,
  and `--max-bytes` flags and its atomic write path. The path must already
  resolve to a list; wildcards are rejected.

## [0.3.0] - 2026-09-01

### Added

- `yaymlq keys <path> [file]` — a mapping's keys (sorted) or a list's indices.
- `yaymlq len <path> [file]` — entry count of a mapping/list, rune count of a
  string, `0` for null.
- `yaymlq type <path> [file]` — `null` / `boolean` / `number` / `string` /
  `array` / `object`.

  All three are read-only, share `get`'s `--doc` / `--all-docs` / `-o` flags,
  and default to `raw` output.

### Changed

- `set <path> <value>` — `<value>` is now documented and tested to accept a YAML
  collection (`set .labels '{team: infra}'`, `set .ports '[80, 443]'`), not only
  a scalar. `-s/--string` still takes the argument verbatim.
- `path.Parse` rejects a path expression that is not valid UTF-8 up front,
  instead of failing later at the YAML encode step. Surfaced by fuzzing the
  `set` / `delete` writer.

## [0.2.0] - 2026-09-01

### Added

- `yaymlq delete <path> [file]` (aliases `del`, `rm`) — remove a mapping key or
  list element, preserving comments and key order on everything that remains.
  Shares `set`'s flags (`-i/--in-place`, `--doc`, `--max-bytes`) and atomic
  write path. Wildcards are rejected; deleting a path that does not exist is an
  error.

## [0.1.0] - 2026-08-30

### Added

- `yaymlq <path> [file]` — read a value from a YAML document (or stdin) by a
  `jq`-ish path expression, output as `yaml` (default), `json`, or `raw`.
- Path syntax: map keys, slice indices (`a[0]`, `a.0`, `a[-1]`), wildcards
  (`a.*`, `a[]`, `a[*]`), and quoted segments (`"a.b".c`).
- Wildcards fan out to multiple results; map values are emitted sorted by key.
- `--doc N` / `--all-docs` for multi-document streams.
- `--default VALUE` and `-e/--exit-status` for script-friendly missing-path
  handling.
- `--max-bytes` input cap (default 64 MiB) guarding against oversized input;
  early-stop stream decoding; YAML alias-bomb rejection.
- `yaymlq set <path> <value> [file]` — set a value, preserving comments, key
  order, and formatting. `-i/--in-place` writes atomically (temp file + rename,
  symlink-safe, mode-preserving); `-s/--string` forces a string value.

[Unreleased]: https://github.com/reticule-poirot/yaymlq/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/reticule-poirot/yaymlq/releases/tag/v0.1.0
