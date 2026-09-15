# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `make lint`'s pinned `golangci-lint` bumped from v2.1.6 to v2.13.2. The
  old pin couldn't type-check code built with a Go toolchain newer than it
  knew about (it errored on the export-data format Go 1.27+ produces,
  before ever reaching real analysis) — purely a local-tooling problem,
  since CI pins an exact matching Go version and was unaffected, but it
  meant `make lint` was unusable on a newer local Go install. v2.13.2
  requires Go ≥1.26.0 (matching CI's own pin) and reports the same "0
  issues" against the current codebase. No linter behavior change intended.

## [0.10.0] - 2026-09-15

### Added

- New `yaymlq schema` command prints a JSON manifest of yaymlq's own
  commands, flags, argument-count constraints, and exit-code scheme — for a
  script or LLM agent to consume instead of parsing `--help` text or
  guessing what an exit code means. Built by reflecting over the live
  command tree (`cmd.Flags().VisitAll`), so it can't drift from the real
  CLI surface as flags change.

## [0.9.0] - 2026-09-15

### Added

- `--in-place` writes now fsync the containing directory after the rename,
  in addition to already fsyncing the temp file's content before it. The
  temp file's content was always durable before the rename; without the
  directory fsync, a power loss right after a successful edit could still
  leave the *old* content on disk — the command would have already exited
  0. Best-effort: errors from this step are ignored, and it's a no-op on
  filesystems/platforms (including Windows) where it isn't meaningful.

- `set`/`append`/`delete`/`rename`/`apply` with `--in-place` now print a
  one-line note to stderr when the target path is a symlink, e.g.
  `note: link.yaml is a symlink; replacing the link, not target.yaml`.
  `--in-place` has always replaced a symlinked path rather than writing
  through it — the edit is read from whatever the link points at, but the
  result lands at the link's own path, so the link is gone afterward and
  the file it pointed at is untouched. That was previously silent; README
  and SECURITY.md now spell out both halves explicitly too.

### Changed

- README and SECURITY.md now note that `--in-place`'s atomic rename also
  breaks any hardlinks to the target — expected behavior for a
  temp-file-and-rename write, previously undocumented alongside the
  detailed symlink note nearby.

- SECURITY.md's list of commands with an `--in-place` write path now
  includes `rename` and `apply` (it previously named only `set`/`append`/
  `delete`); README already had the full list. SECURITY.md's `--max-bytes`
  bullet also no longer claims it bounds memory use generally — matches
  the correction already made to README and the flags' help text.

- README and SECURITY.md now document that interrupting an `--in-place`
  edit (Ctrl-C, a killed process) can leave a `.<name>.yaymlq-<random>`
  sibling temp file next to the target, holding a copy of the document as
  of that moment. yaymlq doesn't install a signal handler to clean this
  up, in keeping with the project staying small; the leaked file is never
  more permissive than the target and is always safe to delete. No
  behavior change — this was already true and previously undocumented.

- `--max-bytes`'s help text and the README now say plainly that it bounds
  input bytes read, not peak memory — decoding, and especially
  `--diff`/`--dry-run` (which briefly holds both the before and after
  document), can use tens of times the input size in RSS. No behavior
  change; the cap was never a memory bound, only the docs implied it was.

- SECURITY.md now states precisely what `--in-place`'s permission
  preservation covers: POSIX mode bits only. Ownership, ACLs, and extended
  attributes (including an SELinux label, or a Windows DACL with
  inheritance disabled) are not carried over, since the write replaces the
  file rather than modifying it in place (README already said this, added
  alongside the symlink note above). No behavior change; the previous
  wording implied a broader guarantee than the code ever gave.

### Fixed

- `--in-place`'s error message, when the temp file it writes first can't be
  created (e.g. no write permission on the target's directory), no longer
  names that internal `.<name>.yaymlq-<random>` temp file — confusing to
  see for anyone who doesn't know `--in-place` writes a sibling file
  before replacing the target. It now names the target file instead,
  keeping the underlying cause (e.g. "permission denied").

- `--in-place` writes no longer invent a `0644` fallback permission (which
  ignores the process umask) when the target can't be stat'd at write time.
  Every caller has already opened the file successfully by that point, so a
  stat failure here means it was removed or replaced concurrently — the
  write now fails outright instead of guessing a permission for a file that
  isn't there anymore.

- `--in-place` writes (`set`/`append`/`delete`/`rename`/`apply`) now set the
  temp file's final permissions on the open file descriptor rather than by
  its path. `os.Chmod` follows symlinks; chmod-by-path left a window in
  which a symlink swapped in at the temp file's name — visible the instant
  it's created, to anything watching the directory — would have its
  permissions widened by the chmod, and then land at the edited document's
  path via the closing rename. Chmod-by-descriptor can't be redirected by a
  name change. No behavior change for the ordinary (non-adversarial) case.

- `apply` batch scripts that touch the same mapping or anchor-heavy
  document many times no longer redo full-document work on every op.
  `findValueIndex` (used to look up every mapping key) and the check that
  refuses to discard an anchored node still referenced elsewhere both used
  to walk from scratch on every single `set`/`append`/`delete`/`rename`
  call; `apply` now shares one cache across its whole batch, kept correct
  incrementally as ops mutate the document. Measured: 80,000 `set` ops
  against the same mapping dropped from ~10.65s to ~0.38s; an 8000-op batch
  against a document where every value is individually anchored dropped
  from double digits of seconds to well under a tenth of a second.

- `get`/`keys`/`len`/`type` no longer copy the whole path-so-far trail on
  every step of a walk — that trail is only needed to build an eventual
  "path not found" error message, but was rebuilt from scratch at every
  step regardless, amplified further by wildcard fan-out (every dead
  sibling branch paid the same cost before moving on). A wildcard query
  against a 13.5 MB document (8000 levels deep, 200 sibling values per
  level) dropped from ~53.8s CPU / 2.52 GB RSS to ~1.55s CPU / ~1.1 GB RSS
  — now close to the cost of just decoding the same document.

- `set`/`delete`/`append`/`rename` no longer format the path-so-far on
  every segment of the walk, even though it's only used in an error
  message — making `set` in particular (which auto-creates missing
  mapping keys, so it walks the full path length regardless of document
  size) `O(segments²)`. A path of 131,072 segments that used to take
  ~38s of CPU inside `set` now takes well under a tenth of a second.
  (Encoding a document nested that deep still costs proportionally more
  output, as any YAML encoder's block style would — this fixes the
  edit itself getting there, not the inherent size of writing out an
  extremely deep tree.)
- `set`/`append`/`delete`/`rename`/`apply` on a multi-document stream no
  longer rescans the entire raw input once per document to preserve its
  blank lines — that scan is now done once for the whole stream, not
  `O(documents × input size)`. A 180 KB file of 20,000 minimal documents
  used to take 8+ seconds of CPU; it's now well under a tenth of a second.
- `--max-bytes` at exactly `math.MaxInt64` no longer silently reads zero
  bytes — the internal cap-checking read used to compute `limit+1`, which
  overflows at that exact value; `validate --max-bytes 9223372036854775807`
  used to report a file "valid" having read none of it.
- A negative `--max-bytes` is now a usage error instead of silently meaning
  "unlimited" (only `0` does, as already documented).
- `validate` now errors on an empty input stream, matching every other
  command, instead of trivially reporting it "valid" — previously
  indistinguishable from the two failure modes above.
- `--doc` and `--all-docs` together is now a usage error instead of
  `--all-docs` silently overriding `--doc` with no warning.
- A negative `--doc` is now a usage error reported up front, instead of
  defeating `decodeDocs`' early-stop optimization (the whole stream got
  decoded before the out-of-range index was finally rejected).

## [0.8.0] - 2026-09-14

### Fixed

- `--diff`/`--dry-run`'s memory use for a large document actually matches
  the O(N+D²) this project has claimed since it was added: `myersTrace`
  was snapshotting a full copy of its O(N+M)-wide working array on every
  one of up to D rounds — O(D·(N+M)) memory, not O(D²) — so a document
  that gets substantially reformatted (a different `--indent`, say, or
  yaml.v3 re-quoting) could push D close to N+M and allocate gigabytes on
  an ordinary-sized file. Each round now records only the d+1 diagonal
  endpoints it actually touched, matching the space-efficient form of
  Myers' algorithm.
- `--diff`/`--dry-run` now shows a change consisting only of the encoder
  adding a document's final newline, instead of reporting "no change" for
  an edit that does rewrite the file (the source lacked a trailing
  newline; the write always adds one).
- `--diff`/`--dry-run`'s `\ No newline at end of file` marker is now
  emitted right after *each* side's actual last line, instead of only ever
  being checked at the diff's single final rendered line — previously a
  changed last line (removed then re-added) could drop the marker
  entirely, producing a diff `patch(1)` refuses to apply.
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

[Unreleased]: https://github.com/reticule-poirot/yaymlq/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/reticule-poirot/yaymlq/releases/tag/v0.1.0
