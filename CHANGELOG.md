# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `set`/`append`/`apply`'s `<value>` is now rejected (instead of silently
  becoming `null`) when it starts with `#` and isn't quoted or passed with
  `-s`/`--string` — in YAML a leading `#` always opens a comment, so it was
  parsed as an all-comment (empty) document and discarded with no error,
  destroying the intended value (`set .color '#ffffff'` used to silently
  write `color: null`).
- `apply`'s edit script (`-f`/`--edits`) is now bounded by `--max-bytes` too
  — previously only the document being edited was capped, so an oversized
  or unbounded script (a file or a stream via `-f -`) could be read fully
  into memory regardless of the flag.
- A single edit-script line over the internal 1 MiB buffer cap is now
  classified as an I/O failure (exit `4`) instead of a usage error (exit
  `3`) — matching how any other failed read of `apply`'s input is
  classified, since it isn't a syntax mistake in the script's content.
- An unknown `-o`/`--output` value is now a usage error (exit `3`) reported
  up front, instead of surfacing later as an unclassified failure (exit
  `1`) — and it's now caught under `-q`/`--quiet` too, which previously
  skipped the check entirely and silently accepted the bad value.
- `get`/`keys`/`len`/`type` can now see into a mapping that has any
  non-string key (an int, bool, or null key alongside ordinary string ones,
  e.g. a port-number map) — previously the whole mapping, including its
  ordinary string keys, was unreachable (`expected a mapping, got
  map[interface {}]interface {}`), and under a wildcard the branch was
  silently skipped instead of erroring.
- `set`/`delete` on a mapping with duplicate keys now edit the *last*
  occurrence, matching `get`'s (and every YAML reader's) "last wins"
  semantics, instead of silently editing an already-shadowed key.
- `rename` on a non-string mapping key (`true:`, `2024:`) now resets the key
  node's tag along with its value, instead of leaving a stale `!!bool`/`!!int`
  tag on the new string name that fails to decode.
- `set`/`delete` now refuse (rather than silently corrupt) an edit that would
  discard a node whose YAML anchor (`&name`) is referenced elsewhere via an
  alias or merge key (`*name`, `<<: *name`) — previously the anchor was
  dropped and the file no longer parsed on the next read.

## [0.7.0] - 2026-09-14

### Added

- `-o json` on `get`/`keys`/`len`/`type`: a failure now writes a single JSON
  object to stderr instead of a prose line — `{"error": "...", "kind":
  "..."}`, with `line` (parse failures) and `path` (an unresolved path)
  added when known. `kind` is one of `parse`/`usage`/`io`/`no-match`, lining
  up with the process exit code. Text-mode output (the default) is
  unchanged; `-e`/`-q`'s deliberate silence on a soft "no match" holds
  regardless of `-o`.
- `--diff`/`--dry-run` on `set`/`append`/`delete`/`rename` — print a unified
  diff of the change instead of writing (`-i`) or printing the whole
  document. Works with or without `-i`; with it, the diff replaces the
  write rather than being shown in addition to it. Exits `0` whether or not
  there were changes — it's a preview, not an assertion. `--dry-run` is an
  alias for `--diff`. No new dependency: a hand-rolled Myers diff
  (`cmd/diff.go`), chosen over a diff library so a large document with only
  a small edit stays fast (O(N+D²), not O(N·M)).
- `--indent N` now also works on `rename` (it already did on
  `set`/`append`/`delete`; missed when `rename` was added).
- `yaymlq apply -f <edits> [file]` — run a batch of `set`/`append`/`delete`/
  `rename` edits in one parse/mutate/serialize pass instead of one
  invocation per edit. `-f`/`--edits` takes a script file (or `-` for
  stdin): one operation per line (`set <path> = <value>`, `append <path> =
  <value>`, `delete <path>`, `rename <path> = <newkey>`), blank lines and
  `#` comments ignored. Any failing op aborts before anything is written.
  Runs on the same pipeline as the single-op commands, so it also gets
  `-i`, `--doc`, `--max-bytes`, `--indent`, and `--diff`/`--dry-run`.

## [0.6.0] - 2026-09-14

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

[Unreleased]: https://github.com/reticule-poirot/yaymlq/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/reticule-poirot/yaymlq/releases/tag/v0.1.0
