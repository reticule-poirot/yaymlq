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
- `make fuzz` — short fuzz run (path, query, ymledit, cmd)
- `make run ARGS="'.a.b' testdata/compose.yml"`

## Layout

- `main.go` — entrypoint, calls `cmd.Execute()` (returns the process exit code)
- `cmd/` — cobra commands: root/get in `root.go`, `set` in `set.go`, `append`
  in `append.go` (both via `runValueEdit` / `bindValueEditFlags` in `set.go`),
  `delete` in `delete.go`, `rename` in `rename.go` (also on the `editOpts`/
  `applyEdit` pipeline), the read-only `keys`/`len`/`type` verbs in
  `inspect.go` (`newInspectCommand` factory), the read-only `validate` verb in
  `validate.go` (checks the whole stream parses, `--require` also checks a
  path resolves via `internal/query`; not built on `newInspectCommand` since
  it takes no single path); shared edit plumbing (`edit.go`:
  `applyEdit` read→mutate→write pipeline, `writeFileAtomic`, `decodeNodes`,
  `detectIndent` sniffs the source's indent width for `--indent`'s default;
  `blanklines.go`: `preserveBlankLines` re-inserts source blank lines yaml.v3
  drops, `tidyBlankLines` cleans the encoder's indented blanks; `crlf.go`:
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
  Whole-CLI fuzz target (`fuzz_test.go`: `FuzzCLI`, drives `NewRootCommand()`
  end to end via `get`).
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
  `cmd/blanklines.go`, not here). Fuzzed (`FuzzSet`, `FuzzAppend`,
  `FuzzDelete`, `FuzzRename`).

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
