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

- `main.go` — entrypoint, calls `cmd.NewRootCommand()`
- `cmd/` — cobra command wiring, stdin/file I/O, output rendering (`render.go`)
- `internal/query/` — the core: `path.go` parses a path into segments,
  `query.go` walks a decoded `any` document. Start here for behavior changes.

## Conventions

- Standard library + `cobra` + `gopkg.in/yaml.v3` only. Discuss before adding deps.
- Every change to the query engine or CLI flags gets a test in the matching
  `_test.go` file. `cmd` tests drive the command via `NewRootCommand()` with
  buffers for in/out.
- Errors from the query engine wrap `query.ErrNotFound` where appropriate; keep
  that contract.
- Run `gofmt -w .` before committing.
