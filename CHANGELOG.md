# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/reticule-poirot/yaymlq/releases/tag/v0.1.0
