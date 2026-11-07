# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.27.0] - 2026-11-07

### Changed

- Reads and writes YAML with `go.yaml.in/yaml/v4` instead of `v3`. Closes #387.
- **Breaking:** an alias to an anchor in an earlier document is refused with
  exit 2. Repeat the anchor in the document that uses it. Part of #387.
- **Breaking:** a tag spelled with a `[`, such as `![`, is refused with exit 2.
  Rename the tag. Part of #387.
- **Breaking:** a document marker after an unindented root block scalar, such as
  `--- |` then `---`, is refused with exit 2. Indent the body or give the header
  an indentation indicator. Part of #387.
- **Breaking:** parse errors have new wording and some have new line numbers.
  Part of #387.
- A root block scalar may have an unindented body when no document marker
  follows. Part of #387.
- Input valid under YAML 1.2.2 is read instead of refused: raw DEL and C1 controls
  in a quoted scalar, an empty implicit key such as `: b`, and a key over 1,024
  characters in a flow mapping. Closes #390.
- `%YAML 1.2` is read instead of refused, and a later 1.x version is read with a
  note on stderr. An edit keeps the directive as written. Closes #391.
- A block scalar in a re-render or `-o yaml` output is written with a different
  header in some cases, and stays a block where `v3` double-quoted some values.
  Part of #387.
- Decoding is slower: about 30% on a 6.5 MB file, about 60% on a single mapping
  of 20,000 or more keys. Part of #387.

### Fixed

- A document re-render keeps a comment directly after `---` when another comment
  sits before that `---`. Closes #394.
- A block scalar's trailing spaces-only line deeper than its content is kept
  through an edit under every chomping, instead of re-rendering the document.
  Part of #387.

- An edit reaches a key by the spelling a read prints as well as the one it is
  written with: `set '."1"' z` on `1.0: b` writes `1.0: z` instead of adding
  `"1": z`, and `delete`, `rename`, `append` and `--must-exist` find the key
  instead of saying `no such key`. Closes #381.
- A path two keys answer to, such as `."1"` with `1` and `1.0`, is refused with
  exit 1 by reads and edits. Reads returned either value and `set` edited `1`.
  Use `rename` on a key's written spelling to tell them apart. Part of #381.
- `rename` refuses a name another key answers to, such as `rename .c 1` beside
  `1.0`. Part of #381.
- An alias key is edited by its target's name, not its anchor's. Part of #381.
- A miss on a mapping with two such keys lists their shared spelling once.
  Part of #381.
- `*`, `**` and `-o json` return a NaN key's value instead of null. Closes #383.
- `-o yaml` and `-o raw` refuse with exit 3 a mapping whose keys would be written
  alike, such as `1` and `1.0`, instead of printing YAML that can't be read back.
  The `-o json` error for such keys suggests `rename` instead of `-o yaml`.
  Closes #385.

## [0.26.0] - 2026-11-06

### Changed

- The YAML library is now `go.yaml.in/yaml/v3` v3.0.6, the maintained fork of
  the archived `gopkg.in/yaml.v3`. Closes #371.
- `get` double-quotes a string or key spelled `<<` in its default output.
  `-o raw` and `-o json` are unchanged.
- `set` writes the value `<<` as `a: <<` instead of `a: !!merge <<`.
- A re-render can keep a value that opens with a line break in block style
  instead of double-quoting it.

### Fixed

- `*` and `**` visit keys that share a spelling, such as `1` and `1.0`, in the
  same order on every run. Closes #377.
- `-o json` names the same key on every run when a mapping has more than one
  key collision. Closes #378.
- YAML output orders a mapping's keys the same on every run. A mapping with a
  digit run past int64 or a non-ASCII digit in a key now orders its string keys
  by byte. Closes #372.
- A NaN key keeps its value in YAML output instead of printing `null`.
- A mapping key that is an alias to a mapping or sequence is refused with the
  same error as one written directly, instead of Go syntax or a panic.
  Closes #370.
- `get` keeps a leading line break in a value it renders as a block.
- `rename` and `set` can create a literal `<<` key, written `"<<"`, instead of
  refusing the edit.

## [0.25.0] - 2026-11-05

### Added

- `schema` marks the `get` entry `"defaultAction": true`. `manifestVersion` stays
  at 1. Part of #360.

### Changed

- The README and `SECURITY.md` say `--max-bytes` doesn't bound CPU: decoding is
  quadratic in a single mapping's width. No behavior change. Closes #363.

### Fixed

- `apply --all-docs` help and its `schema` description say only `set` ops create
  the path; `delete`, `rename` and `append` ops skip such a document. Part of #360.
- Docs and skills match the CLI: the skill says `--max-suggestions` caps
  `available`, and the README and `SECURITY.md` say edits decode every document in
  a stream. Closes #360.
- `--diff`, `--dry-run` and `--show-diff` no longer use memory quadratic in changed
  lines. Past 4096 deleted and added lines, the diff is one block of deletions
  followed by additions. The README's memory figures are corrected. Closes #362.

## [0.24.0] - 2026-11-05

### Added

- `get`/`keys`/`len`/`type` note on stderr, once per run, when a printed value
  carried a tag that doesn't round-trip (`!Ref`, `!!binary`), naming the tag and
  its line. `-q` suppresses it. Closes #343.
- `append` accepts an empty path when the root is a sequence. On a mapping or
  scalar root it is now a kind mismatch (exit 1), not an unsupported path (exit 3).
  Closes #341.

### Changed

- `make fuzz` runs its 20 targets concurrently (`FUZZ_JOBS`, default the core
  count): about 44 s, down from about 7 minutes. Closes #351.
- `set --help` and the skill say `set .x ''` writes null and `set -s .x ''` an
  empty string. Closes #349.
- `shiftBlockLine` returns `string`. No behavior change. Closes #337.
- Four near-duplicate helper pairs in `cmd/` are now one each. No behavior change.
  Closes #335.
- An edit splits its input into lines once instead of twice. No behavior change.
  Closes #333.
- `delete` finds the removed entry in O(n), not O(n²): deleting the last of 20,000
  keys took about 2 s and now takes well under one. Closes #331.
- `delete`/`rename` share `runPathEdit`, and `apply` and `editscript` lost their
  per-verb copies. No behavior change. Closes #325.
- Editing verbs validate `--show-diff`, `--diff-format`, `--indent` and
  `--max-suggestions` before reading the document. Closes #323.
- `query.walk`'s `*` and `**` share a `children` helper. No behavior change.
  Closes #319.
- `get` and `keys`/`len`/`type` share one `runRead` pipeline. No behavior change.
  Closes #317.
- `-o json` walks the value once, detecting key collisions as it builds the map.
  No behavior change. Closes #315.
- `-i`, `--doc`, `--max-bytes` and `--indent` are registered once, in
  `bindEditFlags`. No behavior change. Closes #311.

### Fixed

- Reads of a collection used as a mapping key give the editing verbs' refusal, not
  yaml.v3's panic-recovery text. Closes #355.
- `set`/`append` adding a block scalar as the last line of a source with no
  trailing newline no longer fail with "does not read back". Closes #353.
- A `%YAML` version other than 1.1 is reported by directive and line. Closes #347.
- A verb reading stdin because `[file]` is missing fails fast (exit 3, "no input")
  on a terminal instead of hanging. Pipes are unaffected. Closes #345.
- An edit through a plain alias names the anchor to edit instead. Closes #339.
- `set` on a plain alias value no longer re-renders the whole document. Closes #329.
- Inserting an entry no longer rewrites the previous entry's line ending on a
  mixed-ending source. Closes #327.
- A date-like scalar or key stays the string it was written as on reads, instead of
  decoding to `time.Time`. Closes #321.
- A wildcard or whole-document path an editing verb refuses now exits 3, not 1.
  Closes #313.
- `keys`/`len`/`type`'s `--print0` help matches `get`'s, and `bindReadFlags` now
  registers the read flags once. Closes #309.

## [0.23.0] - 2026-11-05

### Added

- `testdata/fidelity`, a fixture corpus: a no-op edit must reproduce each file byte
  for byte, and a forced re-render must change nothing outside its node.

### Changed

- `set`, `rename`, `delete` and `apply` splice what they can, rewriting only that
  node's bytes instead of re-encoding the stream. Blank-line runs, mixed endings,
  own-line block headers and NEL/LS/PS now survive. This is stage 0 of #294.
  Closes #295.
- The splice covers block scalars, wrapped scalars and dash-alone sequence items.
  Stage 1 of #294. Closes #296.
- **Breaking:** `--indent` sets only inserted lines' indentation and no longer
  re-indents the document. The splice now covers new keys, null-to-mapping,
  collection values and `append`. Stage 2 of #294. Closes #297.
- **Breaking:** an edit the splice can't place re-renders only the smallest node
  that can stand alone, with that node's layout becoming yaml.v3's. Output is
  verified per document; one that fails whole exits 1 and writes nothing. A BOM is
  kept. The re-encode-and-patch layer is removed. Stage 3 of #294. Closes #298.

### Fixed

- `-o json` refuses (exit 3) two keys with the same JSON name, such as `0` and
  `000.`, instead of keeping one at random. Closes #300.
- Reads refuse (exit 2) a key repeated in another spelling, such as `7` and `0o7`,
  instead of dropping one. Found on #300.
- Editing verbs refuse a duplicate key as reads do. Found on #300.
- A blank line at the end of a file or beside `---`/`...` survives an edit. Part of
  #270.
- A value spelled like a fidelity-layer marker no longer gets replaced by another
  node's text. Closes #286.
- Line-repair passes no longer rewrite block scalar bodies the encoder rendered.
  Closes #277.
- A quoted key over 1024 characters no longer makes the file unparseable.
  Closes #279.
- A tag spelled with a bracket inside a flow collection no longer makes the file
  unparseable. Closes #282.
- A whitespace-only block scalar line keeps its value spaces when the block's
  indentation moves. Closes #280.
- A block scalar the encoder renders is read back first and double-quoted if it
  doesn't survive. Closes #274.
- An empty null mapping key stays null instead of becoming `''`. Closes #273.
- A sequence dash alone on its line keeps that form, and a blank line above it
  survives. Part of #270.
- A blank line between block sequence items no longer splits the item below. Part
  of #270.
- An edit no longer adds a blank line below a document's comment header.
- An edit no longer moves `---` above the comment block written over it.
- A null in a flow collection no longer turns into `''` on an edit to another key.
  Closes #263.
- Edits no longer re-escape a quoted scalar they didn't touch. Part of #252.
- Edits no longer strip trailing white space from every line. Part of #252.
- Edits no longer re-spell a flow collection they didn't touch. Part of #252.
- Edits no longer drop a document's `---` and `...` markers. Part of #252.
- The README no longer says edits collapse comment gutters. Closes #251.
- A commented key over an anchored or tagged block collection no longer produces an
  unparseable file. Closes #256.
- Edits no longer misread sources using a line break other than `\n`, and bare-CR
  endings are written back. Closes #254.
- Edits no longer rewrite an untouched block scalar or change its value. Closes #250.

## [0.22.1] - 2026-10-30

### Fixed

- The skill documents `apply` ops with their verb, and tests bind it to the CLI.
  Closes #247.
- An edit on a key inherited through `<<` says so and names the anchor, instead of
  "no such key" plus a typo suggestion. Adds `ymledit.ErrMergedKey`. Closes #240.
- "Path not found" spells offered keys as a path expression would. `-o json` keeps
  raw names. Closes #241.
- `keys` spells a null key `null`, not `<nil>`, and sorts mixed keys as wildcards
  do. Closes #242.
- `-o json` renders bool, null and float keys as path text instead of failing.
  Closes #239.

## [0.22.0] - 2026-10-30

### Added

- `-s`/`--string` on the read verbs takes `--default` literally. It requires
  `--default`. Closes #232.

### Fixed

- The README and skill state the `schema` manifest's size as a ratio rather than a
  byte count, and a test checks it. Closes #234.
- `--help`, the README and the skill say input is read to EOF before any output.
  Behavior is unchanged. Closes #233.

## [0.21.0] - 2026-10-30

### Added

- `keys`, `len` and `type` take `-e`, `-q` and `--default`. A default is printed as
  given, not inspected, and a wrong-kind path is still an error. Closes #213.

### Changed

- **Breaking:** an empty or comment-only stream passes `validate` (exit 0). Other
  verbs report it at exit 1, not 2. `--require` still fails it. Closes #209.
- **Breaking:** `--default` with `-e` exits 0. To detect a miss while printing a
  default, drop `--default`. Closes #208.
- **Breaking:** `--default` with `-q` is a usage error (exit 3). Use `-q` alone or
  `-e --default X`. Closes #227.

### Fixed

- `--help` and `schema`'s `long` field document all thirteen path spellings, and a
  test holds them to the README's table. Closes #226.

## [0.20.1] - 2026-10-24

### Fixed

- `set`/`apply --all-docs` help no longer claims to skip documents without the path.
  Behavior is unchanged. Closes #202.
- Read errors name the YAML kind ("got mapping"), not the Go type.
- A null key is spelled `null` and addressed by `.null`, instead of `<nil>`.
  Closes #203.

## [0.20.0] - 2026-10-24

### Added

- `**` is a recursive-descent segment (`**.image`, `spec.**.image`, `a.**`). Editing
  verbs refuse it. Closes #174.
- A wildcard query matching nothing says so on stderr. Stdout and exit 0 are
  unchanged; `-e`, `-q` and `--default` suppress the note. Closes #173.

### Changed

- **Breaking:** a key spelled `**` must be quoted, and `--paths` renders it as
  `"**"`.

### Fixed

- README corrections: the `--paths` and `--show-diff` examples, `make all`,
  `make fuzz`, the manifest size, the layout listing and `manifests.yml`. Closes #176.

## [0.19.0] - 2026-10-24

### Added

- `--all-docs` on editing verbs applies an edit across a stream in one parse and one
  write, skipping documents without the path. `set` creates it in all unless
  `--must-exist` is given. Excludes `--doc N`. Closes #170.
- `set`/`apply --must-exist` fail on a path that doesn't fully resolve. Closes #164.

### Changed

- `--all-docs` on reads is a selector: documents without the path are skipped, and
  only no match at all fails. `validate --require PATH --all-docs` stays strict.
  Closes #175.
- The skill covers `schema --command`, find-then-edit, miss recovery and `--all-docs`
  edits. Closes #155.

## [0.18.1] - 2026-10-19

### Fixed

- Edits no longer rewrite every `<<:` as `!!merge <<:`, unless the source spelled the
  tag. Closes #171.
- A comment on an anchored block-collection key stays there instead of moving onto
  the first child. Closes #172.

## [0.18.0] - 2026-09-24

### Added

- An `apply` op may carry `--doc N`. `path.Format` quotes keys starting with `--`.
  Closes #159.
- `get --paths -o json` prints `{"doc":0,"path":"image"}` per line. Text mode with
  `--all-docs` notes on stderr that it can't tell documents apart. Part of the same
  change.
- `delete`, `rename`, `append` and `apply` report available keys and "did you mean"
  on a miss, and take `--max-suggestions`. Closes #163.
- "Path not found" lists the mapping's keys or a "did you mean", and `-o json` adds
  `available` and `suggestion`. `--max-suggestions` caps the list. Closes #137.

### Changed

- **Breaking:** `\` escapes inside a quoted path segment (`\\`, `\"`, `\'`, `\n`,
  `\t`, `\r`), and an unknown escape is exit 3. Closes #157.

### Fixed

- `apply` no longer truncates a path containing `=`. The separator is the first
  unquoted `=`, and `--paths` quotes such keys. Closes #158.

## [0.17.0] - 2026-09-24

### Added

- `get --paths` prints each match's resolved path. Implies `-o raw` and rejects
  `--default`. Closes #133.

### Fixed

- `path.Format` quotes keys that need it, so `Parse(Format(x)) == x`, except for a
  key holding both quote kinds.

## [0.16.0] - 2026-09-23

### Added

- `validate --all-docs` and `--doc N` scope `--require`. Either without `--require`
  is exit 3. Closes #132.
- `--show-diff` with `-i` writes the file and prints the diff. It conflicts with
  `--diff` and honours `--diff-format`. Closes #136.
- `schema --command NAME` (repeatable) limits the manifest to the named commands. An
  unknown name is exit 3. Closes #135.

### Removed

- **Breaking:** `--raw` is gone; use `-o raw`. Closes #138.

### Fixed

- Editing verbs refuse a Go/Helm template instead of rewriting its directives at
  exit 0. Quoted directives still work. Closes #150.
- A parse failure from template syntax says so, with a line number, instead of dumping
  a Go value. Closes #134.

## [0.15.1] - 2026-09-22

### Fixed

- `validate --require`'s flag text says "at least one document", not "every input".
  Closes #131.

## [0.15.0] - 2026-09-22

### Changed

- The README documents `--diff-format`. Closes #120.
- The JSON diff's `noNewline` becomes side-specific `aNoNewline`/`bNoNewline`.
  Breaking for anything reading `noNewline`. Text output is unchanged. Closes #119.

### Fixed

- `--diff-format` without `--diff`/`--dry-run` is exit 3, instead of writing the file
  under `-i`. Closes #118.
- `--raw` with a conflicting `-o` is exit 3, and no longer disables `--print0`'s same
  check. Closes #123.
- `validate --require` reports a malformed path as exit 3, not as a missing path.
  Closes #124.

## [0.14.0] - 2026-09-22

### Added

- `--diff-format text|json` on all five editing verbs. `json` emits one object with
  per-line `aLine`/`bLine`, and is still an object when nothing changed.

## [0.13.0] - 2026-09-16

### Added

- `.claude/skills/test-first-development/SKILL.md`, a project skill requiring the
  failing test before the implementation.

## [0.12.1] - 2026-09-16

### Changed

- `unifiedDiff` writes header pieces directly instead of concatenating first. No
  output change. Closes #107.

## [0.12.0] - 2026-09-16

### Added

- `.claude/skills/maintaining-the-changelog/SKILL.md`, a project skill documenting
  this file's conventions.

## [0.11.0] - 2026-09-15

### Added

- `contrib/claude-skill/SKILL.md`, a [Claude Code](https://claude.com/claude-code)
  skill for using `yaymlq`. Copy it to `~/.claude/skills/using-yaymlq/SKILL.md`.

## [0.10.2] - 2026-09-15

### Fixed

- Edits no longer collapse every inline comment's gutter to one space. Closes #102.

## [0.10.1] - 2026-09-15

### Changed

- `make lint`'s `golangci-lint` goes from v2.1.6 to v2.13.2, which requires Go 1.26
  or newer.

## [0.10.0] - 2026-09-15

### Added

- `yaymlq schema` prints a JSON manifest of commands, flags, argument bounds and exit
  codes, reflected from the live command tree.

## [0.9.0] - 2026-09-15

### Added

- `--in-place` also fsyncs the directory after the rename, best-effort.
- `--in-place` notes on stderr when the target is a symlink, which it replaces rather
  than writes through.

### Changed

- The README and `SECURITY.md` note that `--in-place` breaks hardlinks.
- `SECURITY.md` lists `rename` and `apply` as `--in-place` writers, and stops claiming
  `--max-bytes` bounds memory.
- The README and `SECURITY.md` document the temp file an interrupted `--in-place` can
  leave behind.
- `--max-bytes` help and the README say it bounds input read, not peak memory.
- `SECURITY.md` says `--in-place` preserves POSIX mode bits only, not ownership, ACLs
  or extended attributes.

### Fixed

- A failed temp-file creation names the target, not the internal temp file.
- `--in-place` fails if the target can't be stat'd, instead of inventing `0644`.
- `--in-place` chmods the temp file by descriptor, not by path.
- `apply` shares one lookup cache across a batch: 80,000 `set` ops on one mapping went
  from about 10.65 s to 0.38 s.
- Reads stop copying the path trail at every step: a 13.5 MB wildcard query went from
  about 53.8 s to 1.55 s of CPU.
- Edits stop formatting the path at every segment: a 131,072-segment `set` went from
  about 38 s to under 0.1 s of CPU.
- Edits scan a stream for blank lines once, not per document: 20,000 minimal documents
  went from 8+ s to under 0.1 s of CPU.
- `--max-bytes` at `math.MaxInt64` no longer reads zero bytes.
- A negative `--max-bytes` is a usage error instead of meaning unlimited.
- `validate` errors on an empty stream.
- `--doc` with `--all-docs` is a usage error instead of `--all-docs` winning.
- A negative `--doc` is a usage error reported up front.

## [0.8.0] - 2026-09-14

### Fixed

- `--diff` keeps only each round's d+1 endpoints, not a full array copy.
- `--diff` shows a change that only adds a final newline, instead of "no change".
- `--diff` emits `\ No newline at end of file` after each side's last line.
- A `<value>` starting with `#` is rejected unless quoted or `-s`, instead of becoming
  null.
- `apply`'s edit script is bounded by `--max-bytes` too.
- An edit-script line over the 1 MiB buffer is exit 4, not 3.
- An unknown `-o` value is exit 3 up front, under `-q` as well.
- Reads can see into a mapping with any non-string key.
- `set`/`delete` on duplicate keys edit the last occurrence.
- `rename` on a non-string key resets its tag.
- `set`/`delete` refuse to discard an anchor still referenced elsewhere.

## [0.7.0] - 2026-09-14

### Added

- `-o json` on `get`/`keys`/`len`/`type` writes failures as a JSON object with `error`
  and `kind`, plus `line` or `path` when known.
- `--diff`/`--dry-run` on `set`/`append`/`delete`/`rename` print a unified diff instead
  of writing, always at exit 0.
- `--indent N` also works on `rename`.
- `yaymlq apply -f <edits> [file]` runs a batch of edits in one pass. Any failing op
  aborts before anything is written.

## [0.6.0] - 2026-09-14

### Added

- `validate --require <path>` (repeatable) asserts each path resolves in at least one
  of a source's documents.
- `--indent N` on `set`/`append`/`delete`, auto-detected from the source when unset,
  falling back to 2.
- `-0`/`--print0` on `get`/`keys`/`len`/`type` NUL-separates results and implies raw
  output.
- `yaymlq rename <path> <newkey> [file]` renames a key in place, keeping position, value
  and comments. An existing sibling name is an error.
- `-q`/`--quiet` on `get`: no output, exit 0 on a match and 1 otherwise.
- `set`/`append`/`delete`/`rename` preserve consistent CRLF line endings.

### Changed

- **Breaking:** distinct exit codes: 0 success, 1 no match, 2 parse, 3 usage, 4 I/O.

## [0.5.0] - 2026-09-14

### Added

- `yaymlq validate [file...]` checks that each input is well-formed YAML, checking every
  file even after a failure.

### Changed

- `yaymlq` with no arguments prints help and exits 1.

## [0.4.1] - 2026-09-01

### Changed

- `set`/`append`/`delete` keep the source's blank lines, a run collapsing to one.

## [0.4.0] - 2026-09-01

### Added

- `yaymlq append <path> <value> [file]` adds to the list at `<path>`, which must already
  resolve to a list. Wildcards are rejected.

## [0.3.0] - 2026-09-01

### Added

- `yaymlq keys <path> [file]`: a mapping's sorted keys or a list's indices.
- `yaymlq len <path> [file]`: entry count, a string's rune count, or 0 for null.
- `yaymlq type <path> [file]`: `null`, `boolean`, `number`, `string`, `array` or
  `object`. All three are read-only and default to raw output.

### Changed

- `set`'s `<value>` is documented and tested to accept a YAML collection.
- `path.Parse` rejects a path that isn't valid UTF-8 up front.

## [0.2.0] - 2026-09-01

### Added

- `yaymlq delete <path> [file]` (aliases `del`, `rm`) removes a key or element, keeping
  comments and key order. Wildcards and missing paths are errors.

## [0.1.0] - 2026-08-30

### Added

- `yaymlq <path> [file]` reads a value by a `jq`-ish path, as `yaml`, `json` or `raw`.
- Path syntax: map keys, slice indices (`a[0]`, `a.0`, `a[-1]`), wildcards (`a.*`,
  `a[]`, `a[*]`) and quoted segments (`"a.b".c`).
- Wildcards fan out to multiple results, with map values sorted by key.
- `--doc N` and `--all-docs` for multi-document streams.
- `--default VALUE` and `-e/--exit-status` for missing-path handling.
- `--max-bytes` input cap (default 64 MiB), early-stop decoding and alias-bomb rejection.
- `yaymlq set <path> <value> [file]` sets a value, keeping comments, key order and
  formatting. `-i/--in-place` writes atomically; `-s/--string` forces a string.

[Unreleased]: https://github.com/reticule-poirot/yaymlq/compare/v0.27.0...HEAD
[0.27.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.26.0...v0.27.0
[0.26.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.25.0...v0.26.0
[0.25.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.24.0...v0.25.0
[0.24.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.23.0...v0.24.0
[0.23.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.22.1...v0.23.0
[0.22.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.22.0...v0.22.1
[0.22.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.21.0...v0.22.0
[0.21.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.20.1...v0.21.0
[0.20.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.20.0...v0.20.1
[0.20.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.19.0...v0.20.0
[0.19.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.18.1...v0.19.0
[0.18.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.18.0...v0.18.1
[0.18.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.17.0...v0.18.0
[0.17.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.16.0...v0.17.0
[0.16.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.15.1...v0.16.0
[0.15.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.12.1...v0.13.0
[0.12.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/reticule-poirot/yaymlq/compare/v0.10.2...v0.11.0
[0.10.2]: https://github.com/reticule-poirot/yaymlq/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/reticule-poirot/yaymlq/compare/v0.10.0...v0.10.1
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
