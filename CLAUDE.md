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
  it takes no single path); shared edit plumbing (`edit.go`:
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
  for `-0/--print0`'s NUL-joined buffering), input handling (`input.go`: `--max-bytes` cap
  + early-stop stream decoding), exit-code handling (`execute.go`, `silentExit`);
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
  silent) — writes `{"error", "kind", "line"?, "path"?}` to stderr via
  `writeJSONError` instead of leaving it for `exitCode`'s prose line. `line`
  is regex-extracted from a parse-class message (yaml.v3's own "line N"
  text); `path` comes from `query.NotFoundError` (see below), not text.
  `diff.go`: hand-rolled Myers O(ND) line diff + unified-diff rendering
  (no dep — chosen so a large document with a small edit stays fast, not
  O(N·M)); `editOpts.diff`, set via `bindDiffFlag` (`--diff`/`--dry-run`,
  same bool) on all four editing subcommands (five once `apply` is
  counted), makes `applyEdit` print `unifiedDiff(...)` instead of
  writing/printing. `--diff-format text|json` (default `text`, also on
  `bindDiffFlag`) picks the rendering: `unifiedDiff` as today, or
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
  list, so it can't drift as flags change. The two things cobra can't
  expose via reflection — each command's positional arg min/max, and which
  commands emit structured `-o json` errors (see `jsonerr.go` below) — are
  small hand-maintained tables in `schema.go` itself, each with a test that
  fails if a new command is missing an entry.
- `internal/path/` — path expression parser, `Parse` -> `[]Segment` (keys,
  indices, wildcards); a bad expression comes back as a `*path.SyntaxError`
  (`errors.As`) so callers can tell it apart from a resolution failure.
  Shared by query and ymledit. Fuzzed.
- `internal/query/` — read-only resolver: `Run(doc any, expr) ([]any, error)`,
  wildcards fan out. A non-wildcard miss wraps `ErrNotFound` in
  `*NotFoundError`, carrying the resolved-so-far `[]path.Segment` trail
  structured (`errors.As`) for a caller like `cmd`'s `-o json` error output
  that wants it without re-parsing `Error()`'s text. Fuzzed.
- `internal/ymledit/` — `Set`, `Append`, `Delete`, and `Rename` edit a
  `*yaml.Node` tree preserving comments and key order; back the `set` /
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
  (`<path> = <value>`, `#` comments, blank lines ignored). Doesn't call
  `internal/path` or `internal/ymledit` itself — `Op.Path`/`Value` are raw
  text, parsed/applied by `cmd/apply.go`, the same division of labor as the
  single-op commands' own `<path>`/`<value>` CLI arguments. Fuzzed.

## Conventions

- Standard library + `cobra` + `gopkg.in/yaml.v3` only. Discuss before adding deps.
- Every change to the query engine or CLI flags gets a test in the matching
  `_test.go` file. `cmd` tests drive the command via `NewRootCommand()` with
  buffers for in/out (`execute` helper in `root_test.go`). After an intentional
  output change, regenerate goldens: `go test ./cmd -run TestGolden -update`.
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

## Definition of done

Before saying a change is complete:
- `make lint` passes (the CI lint job is real and pinned; don't skip it)
- `make test` passes; new behavior has a test, CLI behavior has a `cmd` test
- `gofmt -w .` run, `go mod tidy` leaves go.mod/go.sum unchanged
- output changed on purpose? regenerate goldens and eyeball the diff
- touched file I/O, parsing, or `set`'s write path? do a security-review pass

## Facts, not guesses

Version numbers, CVE status, and library behavior get checked against source or
`go doc` / `govulncheck` — never asserted from memory. Show the evidence.
