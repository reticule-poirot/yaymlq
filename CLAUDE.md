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
- `make fuzz` — short fuzz run; covers every fuzz target in the tree, which a test asserts
- `make run ARGS="'.a.b' testdata/compose.yml"`

## Layout

Where things live, and the contracts no single file owns. Everything else —
how a thing works and why it works that way — is in the doc comment next to
it. Don't restate those here; a second telling is a second thing to keep
true.

- `main.go` — entrypoint, calls `cmd.Execute()` (returns the process exit code)
- `cmd/` — the cobra commands and everything CLI-shaped: flag wiring, output
  rendering, exit-code classification, diffing, the source-fidelity fixups
  yaml.v3 forces, and the `schema` manifest
- `internal/path/` — path expressions: `Parse` -> `[]Segment`, `Format` back
  to text. `Parse(Format(segs)) == segs` is a property `--paths` depends on.
  Shared by query and ymledit. Fuzzed
- `internal/query/` — read-only resolution against decoded `any` values;
  `*` fans out over one level, `**` over every level. `Run` returns values,
  `RunMatches` values with their paths. Fuzzed
- `internal/ymledit/` — the edits (`Set`/`SetMustExist`, `Append`, `Delete`,
  `Rename`) against a `*yaml.Node` tree, preserving comments and key order.
  `EditIndex` caches per-document lookup state for a batch. Fuzzed
- `internal/editscript/` — `apply`'s script format: `Parse` -> `[]Op`, one op
  per line. Holds no path or YAML logic — `Op.Path`/`Value` are raw text,
  parsed by `cmd/apply.go`, like the single-op verbs' own CLI arguments.
  Fuzzed

### Verbs

| verb | file | kind |
|---|---|---|
| root / `get` | `root.go` | read |
| `keys` / `len` / `type` | `inspect.go` (`newInspectCommand`) | read |
| `validate` | `validate.go` | read, per source, many files |
| `schema` | `schema.go` (`newSchemaCommand`) | read, introspection |
| `set` / `append` | `set.go` (`runValueEdit`) / `append.go` | edit |
| `delete` / `rename` | `delete.go` / `rename.go` | edit |
| `apply` | `apply.go` | edit, batch |

All five editing verbs run on `edit.go`'s `editOpts`/`applyEdit`
read → mutate → write pipeline.

### Cross-file contracts

- **Exit codes.** 0 ok, 1 no-match (a path that didn't resolve, `-e`/`-q`,
  `validate`'s aggregate failure), 2 parse, 3 usage, 4 I/O. `errors.go` tags
  an error with its class without changing its message; `codeFor` reads it
  back; `exitCode` (`execute.go`) and `writeJSONError` (`jsonerr.go`) both
  call `codeFor`, so text and `-o json` can't disagree about a failure. A new
  error stays unclassified unless it is clearly parse/usage/I/O —
  unclassified means 1.
- **A recovery fact is reported once, in one form.** The keys a miss could
  have matched and the "did you mean" go into the message in text mode
  (`annotateNotFound`) or into the object under `-o json` (`writeJSONError`),
  never both, so a JSON consumer never parses prose it already holds as data.
- **Two resolution halves, two error types, two sentinels.**
  `query.NotFoundError`/`ErrNotFound` for reads, `ymledit.KeyError`/
  `ErrNotResolved` for edits. They share no resolution code and must not
  depend on each other; `cmd/notfound.go` handles both. `ErrUnsupported` and
  `ErrAnchored` deliberately carry no `ErrNotResolved` — they are wrong about
  the edit, not about the document.
- **`--all-docs` is a selector everywhere.** Read and edit alike: skip a
  document the path doesn't resolve in, fail only if none matched, report the
  first miss. Mutually exclusive with `--doc N`. The strict "every document
  has it" reading exists in exactly one place,
  `validate --require PATH --all-docs`. A creating verb has nothing to skip,
  so `set --all-docs` (and `apply`'s `set` ops) lands the path in every
  document and `--must-exist` is its selector form — which is why
  `bindEditFlags` takes the `--all-docs` help text as a parameter rather than
  stating one promise for all five verbs (#202). The rule is stated once in
  code too: `docMisses` (`leniency.go`), which each half drives with its own
  "didn't resolve" sentinel — it had been written out three times, in `run`,
  `runInspect` and `editTargets` (#212).
- **`bindEditFlags` is the single registration point** for the flags every
  editing verb shares, so none of them can drift to a different subset.
- **A forced output format is checked before it is assigned.**
  `resolveOutputFormat` (`input.go`) owns `-o` against `--print0`/`--paths`;
  reversing the two steps compares `"raw"` with `"raw"` and the check never
  fires, which is what #123 was. Any future flag that forces a format goes
  through there.
- **`--paths` is the only route from a fan-out query to an edit.** Every
  editing verb refuses `*` and `**` — they test `Segment.Fans()`, so a new
  fan-out kind can't be accepted by default — and `apply` ops take literal
  paths, so the path list is the bridge. Across a stream it needs `-o json`:
  a bare path repeats per document and only that form carries the index
  (#159).
- **A reserved path spelling has to be quoted by `path.Format`.** `*` and
  `**` are tokens, and both are legal YAML keys, so a key spelled either way
  must come back quoted or a rendered path means something else to whatever
  reads it — re-interpreted rather than truncated, which is #157/#158 one
  level up. `quoteKey`'s list is the one place that decides, and
  `FuzzParse` plus `FuzzPathThroughEditScript` are what keep it honest.
- **`path` and `editscript` must agree where a path ends.** `editscript`
  splits an op on the first `=` outside a quoted run, skipping what a
  backslash escapes; `path.Parse` reads the same quoting. A disagreement
  truncates the path and the edit lands on a key that never existed (#157,
  #158). `cmd`'s `FuzzPathThroughEditScript` checks the two against each
  other rather than trusting either.
- **Source fidelity is restored around the encode, never during it.** yaml.v3
  drops blank lines, discards inline-comment gutter widths, normalizes CRLF,
  retags `<<` keys, and misattributes a comment on an anchored key.
  `blanklines.go`, `commentgutters.go`, `crlf.go`, `mergekeys.go` and
  `anchorcomments.go` each record the fact from the *raw source* before
  mutation and restore it after. Only the raw source distinguishes a detail
  yaml.v3 dropped from one the author wrote, so a new fixup takes the same
  shape.
- **One `EditIndex` per document, shared across that document's ops.** It
  caches key positions for the nodes of one tree, so handing one document's
  ops another document's index answers from the wrong document
  (`cmd/apply.go`).
- **A template is refused, not edited.** A Go/Helm `{{ ... }}` directive is a
  valid YAML flow mapping, so it parses. `input.go`'s `rejectMappingKeys`
  holds the node path to the same refusal the map path already made, which is
  what stops an editing verb rewriting a directive into explicit-key form
  over the original (#150).
- **`schema` is reflected, not written down.** Command, flag, type, default
  and description all come from the live cobra tree. The two things cobra
  can't expose — positional arg bounds, and which verbs emit `-o json`
  errors — are hand-maintained tables in `schema.go`, each with a test that
  fails when a new command has no entry.

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
- `go test -fuzz` takes a **regexp** and refuses a pattern matching more than
  one target, so `make fuzz` anchors each (`'^FuzzRun$$'`). Adding a target
  whose name extends an existing one breaks the *shorter* target's line, not
  the new one. `TestMakeFuzzRunsEveryFuzzTarget` and
  `TestMakeFuzzPatternsMatchExactlyOneTarget` (repo root) catch both a target
  nobody wired up and a pattern that became ambiguous.
- Issues: set labels, milestone and blocking relations *at creation*. `bug`
  never carries an `effort:` label, `enhancement` always does; `agent-ux`
  marks scripting/agent ergonomics; `breaking-change` marks a documented CLI
  behavior change — which also means it can't ship in a patch release, so it
  belongs in a minor milestone; `blocked` only alongside a real
  `--add-blocked-by`. `gh` has no `milestone` command — use `gh api
  repos/:owner/:repo/milestones`. Blocking relations read back via `gh api
  repos/:owner/:repo/issues/N/dependencies/blocked_by` (the GraphQL
  `Issue.blockedByIssues` field does not exist).
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
- CI fails under 80% total coverage; `make cover` to check the current figure
- `gofmt -w .` run, `go mod tidy` leaves go.mod/go.sum unchanged
- output changed on purpose? regenerate goldens and eyeball the diff
- user-visible CLI change? `contrib/claude-skill/SKILL.md` documents the
  surface for *other* repos and isn't auto-loaded here, so nothing prompts
  you to open it — check whether it needs the same edit (#155)
- a doc example naming a file in this repo (`testdata/*`,
  `.github/workflows/ci.yml`) is pasted from a real run, never written by
  hand: two README examples were wrong on arrival (#176)
- touched file I/O, parsing, or `set`'s write path? do a security-review pass
- user-visible change? add a `CHANGELOG.md` entry under `[Unreleased]` — no
  version number (assigning one is a separate release step)

## Facts, not guesses

Version numbers, CVE status, and library behavior get checked against source or
`go doc` / `govulncheck` — never asserted from memory. Show the evidence.
