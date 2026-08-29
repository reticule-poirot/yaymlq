# CLAUDE.md

Guidance for working in this repo.

## What this is

`yaymlq` ("Yet Another YAML Query") is a small Go CLI that extracts a value from
a YAML document by path expression. It is a learning project — keep it small,
readable, and well-tested rather than feature-complete.

## Commands

- `make all` — fmt, vet, test, build
- `make test` / `go test ./...`
- `make cover` — coverage summary
- `make run ARGS="'.a.b' testdata/compose.yml"`
- `make lint` — golangci-lint (config in `.golangci.yml`)

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
