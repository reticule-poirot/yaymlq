<!-- Keep it short. Delete sections that don't apply. -->

## What & why

## Checklist

- [ ] `make lint` and `make test` pass
- [ ] New behaviour has a test (CLI behaviour has a `cmd` test)
- [ ] `gofmt -w .` run; `go mod tidy` is clean
- [ ] Golden files regenerated if output changed on purpose
- [ ] Touched file I/O, the parser, or `set`'s write path? `SECURITY.md` re-read
- [ ] User-visible CLI change? `contrib/claude-skill/SKILL.md` checked
- [ ] Doc examples naming a repo file pasted from a real run
- [ ] `CHANGELOG.md` `[Unreleased]` updated
