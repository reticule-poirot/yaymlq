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
- `make fuzz` — short fuzz run (path + query)
- `make run ARGS="'.a.b' testdata/compose.yml"`

## Layout

- `main.go` — entrypoint, calls `cmd.Execute()` (returns the process exit code)
- `cmd/` — cobra commands: root/get in `root.go`, `set` in `set.go`; output
  rendering (`render.go`), input handling (`input.go`: `--max-bytes` cap +
  early-stop stream decoding), exit-code handling (`execute.go`, `silentExit`).
- `internal/path/` — path expression parser, `Parse` -> `[]Segment` (keys,
  indices, wildcards). Shared by query and ymledit. Fuzzed.
- `internal/query/` — read-only resolver: `Run(doc any, expr) ([]any, error)`,
  wildcards fan out. Fuzzed.
- `internal/ymledit/` — `Set` edits a `*yaml.Node` tree preserving comments and
  key order; backs `yaymlq set`.

## Conventions

- Standard library + `cobra` + `gopkg.in/yaml.v3` only. Discuss before adding deps.
- Every change to the query engine or CLI flags gets a test in the matching
  `_test.go` file. `cmd` tests drive the command via `NewRootCommand()` with
  buffers for in/out (`execute` helper in `root_test.go`). After an intentional
  output change, regenerate goldens: `go test ./cmd -run TestGolden -update`.
- Errors from the query engine wrap `query.ErrNotFound` where appropriate; keep
  that contract.
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
