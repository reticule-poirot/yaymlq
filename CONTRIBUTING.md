# Contributing

Thanks for taking a look. This is a small, deliberately-focused tool; the bar is
readability and test coverage over feature count.

## Development

```sh
make all        # fmt + vet + test + build
make test       # go test ./...
make lint       # golangci-lint (pinned, via `go run` — no install needed)
make cover      # coverage summary
make fuzz       # short fuzz run over the path parser + resolver
```

`go` 1.25+ is required. There is no other toolchain dependency.

## Before opening a pull request

- `make lint` and `make test` pass.
- New behaviour has a test. CLI behaviour gets a `cmd` test that drives the
  command through `NewRootCommand()` with buffers (see `cmd/root_test.go`).
- Ran `gofmt -w .`; `go mod tidy` leaves `go.mod`/`go.sum` unchanged.
- If output changed on purpose, regenerate golden files and review the diff:
  `go test ./cmd -run TestGolden -update`.
- If you touched file I/O, the parser, or `set`'s write path, re-read
  [SECURITY.md](SECURITY.md) and sanity-check the change.
- Add a bullet to the `[Unreleased]` section of [CHANGELOG.md](CHANGELOG.md).

## Dependencies

Standard library plus `spf13/cobra` and `gopkg.in/yaml.v3` only. Open an issue to
discuss before adding anything else.

## Commit messages

Short imperative summary line, a blank line, then the "why". One logical change
per commit.

## Conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
