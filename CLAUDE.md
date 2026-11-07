# CLAUDE.md

`yaymlq` is a small Go CLI that extracts a value from a YAML document by path
expression. A learning project: keep it small, readable and well-tested. How a
thing works is in the doc comment next to it.

## Layout and commands

- `cmd/`: verbs, output, exit codes, splice. Edits run `edit.go`'s `applyEdit`
- `internal/`: `path`, `query` (reads), `ymledit` (edits), `editscript` (`apply`)
- `make all`, `ci`, `test`, `cover`, `lint`, `vulncheck`, `fuzz`, and
  `make run ARGS="'.a.b' testdata/compose.yml"`

## Cross-file contracts

- Exit codes: 0 ok, 1 no-match, 2 parse, 3 usage, 4 I/O. A new `cmd` error wraps
  `parseErr`/`usageErr`/`ioErr` if clearly one of those, else stays unclassified.
- Other cross-file rules live in doc comments at the code that owns them, such
  as `docMisses`, `Segment.Fans`, `annotateNotFound`, `encode.go` and `lines.go`.

## Conventions

- Stdlib, `cobra`, `spf13/pflag` and `go.yaml.in/yaml/v4` only; discuss new deps.
- Every change to the query engine or CLI flags gets a test in the matching
  `_test.go` file. Goldens: `go test ./cmd -run TestGolden -update`.
- New splice construct: add a `testdata/fidelity` fixture. Flag needing another
  flag: add a `flagConflicts` row. New command: add its `schema.go` table entries.
- `main` is protected: `<type>/<name>` branch, then a squash-merged PR.
- Issues: set labels, milestone and blockers at creation.
- Skills: `test-first-development`; `maintaining-the-changelog`.
- Done: lint and tests pass, coverage at least 80%, `gofmt -w .`, tidy `go mod`.
  User-visible change: update `CHANGELOG.md`, check `contrib/claude-skill/SKILL.md`.
  Touched I/O or parsing: security-review pass. Verify facts, never assert from memory.
