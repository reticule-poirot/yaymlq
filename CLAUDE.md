# CLAUDE.md

Guidance for working in this repo.

## What this is

`yaymlq` ("Yet Another YAML Query") is a small Go CLI that extracts a value from
a YAML document by path expression. It is a learning project — keep it small,
readable, and well-tested rather than feature-complete.

## Commands

- `make all` — fmt, vet, lint, test, build
- `make ci` — what CI runs: vet, lint, vulncheck, test, build
- `make test` / `go test ./...`
- `make cover` — coverage summary
- `make lint` — golangci-lint, pinned (config in `.golangci.yml`)
- `make vulncheck` — govulncheck
- `make fuzz` — short fuzz run (path, query, ymledit, cmd, editscript)
- `make run ARGS="'.a.b' testdata/compose.yml"`

## Layout

- `main.go` — entrypoint, calls `cmd.Execute()` (returns the process exit code)
- `cmd/` — cobra commands: root/get in `root.go`, `set` in `set.go`, `append`
  in `append.go` (both via `runValueEdit` / `bindValueEditFlags` in `set.go`),
  `delete` in `delete.go`, `rename` in `rename.go`, `apply` in `apply.go`
  (all four on the `editOpts`/`applyEdit` pipeline), the read-only `keys`/`len`/`type` verbs in
  `inspect.go` (`newInspectCommand` factory), the read-only `validate` verb in
  `validate.go` (checks the whole stream parses, `--require` also checks a
  path resolves via `internal/query`; not built on `newInspectCommand` since
  it takes no single path. `--all-docs`/`--doc N` scope `--require` only —
  every document, or one — since the stream is always parsed in full either
  way; they're rejected without `--require` rather than silently doing
  nothing, and an out-of-range `--doc` is a per-source failure (exit 1) not a
  usage error, because `validate` reports per source across many files); shared edit plumbing (`edit.go`:
  `applyEdit` read→mutate→write pipeline, `writeFileAtomic`, `decodeNodes`,
  `detectIndent` sniffs the source's indent width for `--indent`'s default;
  `blanklines.go`: `preserveBlankLines` re-inserts source blank lines yaml.v3
  drops, `tidyBlankLines` cleans the encoder's indented blanks;
  `commentgutters.go`: `recordCommentGutters`/`widenCommentGutters` do the
  same for inline comment spacing — yaml.v3's `Node.LineComment` only ever
  stores `"# text"` (the leading-space count before `#` is discarded at
  parse time) and its encoder always re-emits exactly one space, so without
  this every edit would collapse every hand-aligned comment gutter in the
  document, not just the one on the line being edited; matched by comment
  text in order of appearance, same simplification-under-duplication
  tradeoff as blank lines; `crlf.go`:
  `hasCRLF`/`restoreCRLF` round-trip CRLF line endings the same way);
  output rendering (`render.go`: `render`/`renderRaw` per result, `resultWriter`
  for `-0/--print0`'s NUL-joined buffering; the `--print0`-implies-`-o raw`
  resolution and its conflict check live in one place, `input.go`'s
  `resolveOutputFormat`, shared by `root.go` and `inspect.go` — the check has
  to run before the assignment or it compares `"raw"` against `"raw"` and
  never fires, which is the shape of #123, whose `--raw` alias was removed in
  #138; `--paths` forces raw the same way, through the same check), input
  handling (`input.go`: `--max-bytes` cap
  + early-stop stream decoding; `templateDirectiveLine`/`rejectMappingKeys`/
  `explainParseError` handle Go/Helm template directives, which are
  syntactically valid YAML flow mappings and so parse — decoding into
  `map[string]any` then trips yaml.v3's "map used as a map key" check
  (what made `get` refuse a chart), while decoding into a `*yaml.Node`
  applied no such check, so the editing commands accepted a template and
  rewrote the directive into explicit-key form over the original;
  `rejectMappingKeys` brings the node path in line with the map path, and
  `explainParseError` replaces yaml.v3's `%#v` dump — the one parse error
  that carries no line number — with the directive's line), `--paths`
  (root/`get` only: prints each match's resolved path via `path.Format`
  instead of its value, the one route out of "found four matches, can't edit
  any of them" — every editing command refuses a wildcard and `apply`
  scripts take literal paths, so the path list is the bridge between a
  wildcard query and a batch edit; rejects `--default`, whose value by
  definition isn't in the document and so has no path),
  exit-code handling (`execute.go`, `silentExit`);
  `errors.go`: `parseErr`/`usageErr`/`ioErr` tag an error with its exit-code
  class (2/3/4) without changing its message, `pathErr` classifies a
  `path.SyntaxError` as usage, `usageArgs` wraps a cobra arg-count validator
  the same way, `codeFor` reads the class back off (default exit 1 for
  anything left unclassified — query/ymledit's "path didn't resolve" family,
  `validate`'s aggregate failure); `exitCode` (`execute.go`) and
  `writeJSONError` (`jsonerr.go`) both call `codeFor`, so text- and
  `-o json`-mode error reporting always agree on the exit code. `jsonerr.go`:
  `get`/`keys`/`len`/`type`'s `RunE` runs their error through `handleErr`,
  which — only under `-o json`, and never for a `silentExit` (`-e`/`-q` stay
  silent) — writes `{"error", "kind", "line"?, "path"?, "available"?,
  "availableTotal"?, "suggestion"?}` to stderr via `writeJSONError` instead
  of leaving it for `exitCode`'s prose line. `line`
  is regex-extracted from a parse-class message (yaml.v3's own "line N"
  text); `path` comes from `query.NotFoundError` (see below), not text.
  The last three carry #137's recovery facts as data; in text mode the same
  facts go into the message instead (`annotateNotFound`), never both, so a
  JSON consumer never has to parse prose it already holds structurally.
  `suggest.go`: `nearest` picks the key behind "did you mean ...?" — a
  case-insensitive match wins outright, otherwise the closest candidate
  within `maxEdits` (1 for keys of 3 runes or fewer, else 2), and a tie
  loses, because two plausible keys read worse than none. `editDistance` is
  optimal string alignment (Levenshtein plus adjacent transposition as a
  single edit, since swapped neighbours are the typo plain Levenshtein
  scores worst), compared over runes. `notfound.go`: `notFoundKeys` pulls
  the facts out for both modes and returns the *displayed* keys and the full
  list separately, because `--max-suggestions` (default 10, 0 = all, a
  negative value rejected the same way `--max-bytes` rejects one) bounds how
  much an error prints, not how hard it looks — searching only the displayed
  keys doesn't merely miss a suggestion for the 29th key of 30, it
  confidently offers the closest of the first ten instead.
  `diff.go`: hand-rolled Myers O(ND) line diff + unified-diff rendering
  (no dep — chosen so a large document with a small edit stays fast, not
  O(N·M)); `editOpts.diff`, set via `bindEditFlags` (`--diff`/`--dry-run`,
  same bool, plus `--max-suggestions`) on all four editing subcommands (five
  once `apply` is counted) — one registration point, so none of them can end
  up offering a different subset, makes `applyEdit` print `unifiedDiff(...)` instead of
  writing/printing. `--show-diff` (same `bindEditFlags`) is the other
  composition: with `-i` it writes the file *and then* prints the diff, so a
  caller doesn't have to spend a second invocation — or re-read the file — to
  learn what changed. It requires `-i` (without it the document already goes
  to stdout) and conflicts with `--diff`; both checks run before any write.
  The write comes first and the diff second, so a failed write never prints a
  diff describing a change that didn't happen. Both branches render through
  one `emitDiff`, so preview and report can't drift. `--diff-format text|json` (default `text`, also on
  `bindEditFlags`) picks the rendering: `unifiedDiff` as today, or
  `writeDiffJSON`'s one-line `{"file","changed","hunks"}` object for a
  caller that shouldn't have to parse unified-diff text. Both renderers
  share one `hunkInfo` (computed once by `computeHunkInfo`, so the `@@`
  header numbers and the JSON `aStart/aCount/bStart/bCount` can't drift
  apart) and one `lineNoNewline`. JSON's no-change case is a valid object
  (`{"changed":false,"hunks":[]}`, never `null` hunks), deliberately unlike
  text mode's empty string. Fuzzed (`FuzzDiff` in `fuzz_test.go`,
  round-trip property: replaying the edit script against the original must
  reproduce the target exactly, plus both renderers agreeing on whether
  anything changed at all). `apply.go`: `apply -f <edits> [file]` batches
  `set`/`append`/`delete`/`rename` ops from `internal/editscript` into one
  `applyEdit` `mutate` call (`runScriptOps` dispatches each parsed `Op` to
  the matching `ymledit` function, `path.Parse`d fresh per op, all sharing
  one `*ymledit.EditIndex` across the whole batch) — first op to fail
  aborts before any write, same as a single edit failing; the
  script itself comes from `-f`/`--edits` (a real file — opened directly in
  `runApply`, not passed through another function, to avoid gosec G304) or
  `-` for stdin, then read through `readCapped` same as the document, so
  `--max-bytes` bounds both. A read failure surfacing as
  `editscript.ErrRead` (a scanner-level error, not a syntax mistake in an
  otherwise-readable line) is `ioErr`; anything else `editscript.Parse`
  returns is `usageErr`.
  Whole-CLI fuzz target (`fuzz_test.go`: `FuzzCLI`, drives `NewRootCommand()`
  end to end via `get`). The read-only `schema` verb in `schema.go`
  (`newSchemaCommand` factory) prints a JSON manifest of yaymlq's own
  command/flag/exit-code surface for a script or agent to introspect;
  built by reflecting over the live `*cobra.Command` tree (`Flags().VisitAll`
  for flag name/type/default/description) rather than a hand-duplicated
  list, so it can't drift as flags change. `--command NAME` (repeatable)
  narrows it to the named commands — the full manifest is ~21KB, which is
  a lot to read to answer one question about one verb; `version`/`exitCodes`
  stay regardless, and commands come out in tree order whatever order they
  were named, so output is byte-stable. The two things cobra can't
  expose via reflection — each command's positional arg min/max, and which
  commands emit structured `-o json` errors (see `jsonerr.go` below) — are
  small hand-maintained tables in `schema.go` itself, each with a test that
  fails if a new command is missing an entry.
- `internal/path/` — path expression parser, `Parse` -> `[]Segment` (keys,
  indices, wildcards); a bad expression comes back as a `*path.SyntaxError`
  (`errors.As`) so callers can tell it apart from a resolution failure.
  `Format` renders a trail back to expression text and quotes any key whose
  bare form would parse as something else (contains `.`/`[`/a quote, is `*`,
  is an integer, or is empty) — `Parse(Format(segs)) == segs` is a property
  `get --paths` depends on and `FuzzParse` asserts. It also quotes a key
  containing whitespace or `=`, which Parse itself doesn't care about: that
  rule is about the *consumers* of a rendered path, which read it a line and
  a word at a time and split an apply op on its first unquoted `=` (#158 —
  an unquoted key holding either was cut in half downstream and the edit
  landed on a key that never existed). `cmd`'s `FuzzPathThroughEditScript`
  checks that list against the script grammar directly instead of trusting
  it.
  `Segment.String` stays the unquoted display form. The one key that can't
  round-trip is one holding both quote characters, since the grammar has no
  escape syntax. Shared by query and ymledit. Fuzzed.
- `internal/query/` — read-only resolver: `Run(doc any, expr) ([]any, error)`,
  wildcards fan out. A non-wildcard key miss against a mapping also carries
  that mapping's keys as `NotFoundError.Available`, sorted and uncapped, so a
  caller can say what *was* there instead of sending the user back for a
  second query (#137). It is non-nil exactly when the miss was a key lookup
  against a mapping — empty mapping included — which is how `cmd` tells "this
  mapping has no keys" from "this miss has no keys to offer"; an index miss or
  a scalar in the way leaves it nil, since their own messages already explain
  themselves (pinned by
  `TestNotFoundErrorAvailableIsNonNilForAnEmptyMapping`). `RunMatches` is the same walk returning `[]Match`
  (`Path` + `Value`) instead of values alone — what `get --paths` prints, and
  the reason each match's trail is `slices.Clone`d at the moment it matches:
  `extend` reuses the trail's spare capacity, so a retained trail would be
  overwritten by the next sibling (`TestRunMatchesPathsAreNotAliased` fails
  with every path reporting the last key without the copy). A non-wildcard miss wraps `ErrNotFound` in
  `*NotFoundError`, carrying the resolved-so-far `[]path.Segment` trail
  structured (`errors.As`) for a caller like `cmd`'s `-o json` error output
  that wants it without re-parsing `Error()`'s text. Fuzzed.
- `internal/ymledit/` — `Set`, `Append`, `Delete`, and `Rename` edit a
  `*yaml.Node` tree preserving comments and key order. A key the mapping
  doesn't have comes back as a `*KeyError` carrying the trail and that
  mapping's keys (sorted and deduplicated, matching what `keys` prints), so
  an edit miss recovers the same way a read miss does (#163); `Set` never
  produces one, since it creates a missing key instead of failing (#164).
  Deliberately a different type from `query.NotFoundError` — one walks
  decoded `any` values, the other a node tree, and they share no resolution
  code — with `cmd`'s `notFoundKeys` handling both rather than the two
  packages depending on each other. back the `set` /
  `append` / `delete` / `rename` commands (blank-line preservation lives in
  `cmd/blanklines.go`, not here). Each takes a trailing `*EditIndex`: nil for
  a single-op command (falls back to a fresh per-call scan, same as always),
  or one shared across a batch of calls against the same doc (`apply`'s use
  case) so repeated lookups against the same mapping, and repeated
  anchor-reference checks, aren't each a fresh O(document size) walk —
  `EditIndex`'s own doc comment has the incremental-update details (why key
  removal is the one case that's cheaper to drop and rebuild than to track
  precisely). Fuzzed (`FuzzSet`, `FuzzAppend`, `FuzzDelete`, `FuzzRename`,
  and `FuzzEditIndexMatchesUncached`, a differential fuzzer asserting a
  shared index never changes the outcome from not using one).
- `internal/editscript/` — `apply`'s batch-edit script format: `Parse(io.Reader)
  ([]Op, error)`, one `set`/`append`/`delete`/`rename` op per line
  (`<path> = <value>`, `#` comments, blank lines ignored). The separator is
  the first `=` outside a quoted run (`indexUnquoted`), not the first `=`
  anywhere: a key may contain one, and the truncated path left by a naive
  split still parsed, so the op silently edited a different key (#158). Doesn't call
  `internal/path` or `internal/ymledit` itself — `Op.Path`/`Value` are raw
  text, parsed/applied by `cmd/apply.go`, the same division of labor as the
  single-op commands' own `<path>`/`<value>` CLI arguments. Fuzzed.

## Conventions

- Standard library + `cobra` + `gopkg.in/yaml.v3` only (plus `spf13/pflag`,
  cobra's own flag package, used directly in `schema.go` to reflect over
  flags). Discuss before adding deps.
- Every change to the query engine or CLI flags gets a test in the matching
  `_test.go` file. `cmd` tests drive the command via `NewRootCommand()` with
  buffers for in/out (`execute` helper in `root_test.go`). After an intentional
  output change, regenerate goldens: `go test ./cmd -run TestGolden -update`.
- A new flag that's only valid alongside another one (or only valid with
  certain `-o` values) needs a row in `cmd/flagconflict_test.go`'s
  `flagConflicts` table, not just a per-command test. The table is driven off
  the live cobra tree, so it covers whichever commands offer the flags — and
  it asserts a rejected invocation leaves the file byte-identical, which is
  the half that catches a validation firing *after* the write.
- Errors from the query engine wrap `query.ErrNotFound` where appropriate; keep
  that contract. A new error a `cmd` command can return needs an exit-code
  class too: wrap it with `parseErr`/`usageErr`/`ioErr` (`cmd/errors.go`) if
  it's clearly one of those, otherwise leave it unclassified — it falls
  through to exit 1, alongside `query`/`ymledit`'s "path didn't resolve"
  family.
- Run `gofmt -w .` before committing.
- `main` is protected: never commit to it directly. Work on a
  `<type>/<name>` branch, push, open a PR, squash-merge once CI is green.
  See [CONTRIBUTING.md](CONTRIBUTING.md#branching--pull-requests).

## Project skills

`.claude/skills/` holds skills that auto-load when working in this repo and
carry conventions this file only summarizes:
- `test-first-development` — write the failing test *before* the
  implementation. This sharpens the testing bullet above: the rule isn't just
  "a test exists," it's that the test was watched failing first.
- `maintaining-the-changelog` — `CHANGELOG.md` entry conventions, and the
  one-PR-per-version-section rule.

`contrib/claude-skill/` is a separate, distributable copy for *other* projects
that use yaymlq — it is not auto-loaded here.

## Definition of done

Before saying a change is complete:
- `make lint` passes (the CI lint job is real and pinned; don't skip it)
- `make test` passes; new behavior has a test, CLI behavior has a `cmd` test
- CI fails under 80% total coverage (currently ~92%); `make cover` to check
- `gofmt -w .` run, `go mod tidy` leaves go.mod/go.sum unchanged
- output changed on purpose? regenerate goldens and eyeball the diff
- touched file I/O, parsing, or `set`'s write path? do a security-review pass
- user-visible change? add a `CHANGELOG.md` entry under `[Unreleased]` — no
  version number (assigning one is a separate release step)

## Facts, not guesses

Version numbers, CVE status, and library behavior get checked against source or
`go doc` / `govulncheck` — never asserted from memory. Show the evidence.
