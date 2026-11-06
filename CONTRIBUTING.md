# Contributing

Thanks for taking a look. This is a small, deliberately-focused tool; the bar is
readability and test coverage over feature count.

## Development

```sh
make all        # fmt + vet + lint + test + build
make test       # go test ./...
make lint       # golangci-lint (pinned, via `go run` — no install needed)
make cover      # coverage summary
make fuzz       # short fuzz run: every fuzz target in the tree (a test asserts that)
```

`go` 1.25+ is required. There is no other toolchain dependency.

## Branching & pull requests

`main` is protected — all changes land through a pull request that passes CI.
A PR touching only Markdown/`LICENSE`/`.gitignore` skips the Go-specific work
(build/test/lint/vulncheck/fuzz/CodeQL) automatically — the required checks
still run and report, just fast, since one changing only `.go`/`go.mod`/
workflow files etc. still runs everything (see `ci.yml`'s `changes` job).

```sh
git checkout main && git pull            # start from the latest main
git checkout -b <type>/<short-name>      # e.g. fix/wildcard-index, docs/branching
# ...work, committing in logical steps...
git push -u origin <type>/<short-name>
# open the PR from the URL git prints; let CI finish; then "Squash and merge"
git checkout main && git pull            # sync; the branch is auto-deleted on merge
```

Branch name prefixes: `feat/`, `fix/`, `docs/`, `test/`, `ci/`, `refactor/`,
`chore/`. Keep a branch scoped to one change. Rebase on `main` (or use the PR's
"Update branch" button) if it falls behind.

## Before opening a pull request

- `make lint` and `make test` pass.
- New behaviour has a test. CLI behaviour gets a `cmd` test that drives the
  command through `NewRootCommand()` with buffers (see `cmd/root_test.go`).
- Total coverage stays at 80% or above, or CI fails; `make cover` shows it.
- Ran `gofmt -w .`; `go mod tidy` leaves `go.mod`/`go.sum` unchanged.
- If output changed on purpose, regenerate golden files and review the diff:
  `go test ./cmd -run TestGolden -update`.
- If you touched file I/O, the parser, or `set`'s write path, re-read
  [SECURITY.md](SECURITY.md) and sanity-check the change.
- If a CLI change is user-visible, check whether
  [contrib/claude-skill/SKILL.md](contrib/claude-skill/SKILL.md) needs the same
  edit.
- A doc example naming a file in this repo is pasted from a real run, not
  written by hand.
- Add an entry to the `[Unreleased]` section of [CHANGELOG.md](CHANGELOG.md),
  with no version number.

## Dependencies

Standard library plus `spf13/cobra` and `go.yaml.in/yaml/v4` only — plus
`spf13/pflag`, cobra's own flag package, which `cmd/schema.go` imports
directly to reflect over the command tree. Open an issue to discuss before
adding anything else.

## Commit messages

Short imperative summary line, a blank line, then the "why". One logical change
per commit.

## Conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
