# yaymlq

[![CI](https://github.com/reticule-poirot/yaymlq/actions/workflows/ci.yml/badge.svg)](https://github.com/reticule-poirot/yaymlq/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/reticule-poirot/yaymlq.svg)](https://pkg.go.dev/github.com/reticule-poirot/yaymlq)
[![Go Report Card](https://goreportcard.com/badge/github.com/reticule-poirot/yaymlq)](https://goreportcard.com/report/github.com/reticule-poirot/yaymlq)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/reticule-poirot/yaymlq/badge)](https://scorecard.dev/viewer/?uri=github.com/reticule-poirot/yaymlq)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Yet Another YAML Query** — a tiny Go CLI that reads (and writes) values in a
YAML document using a `jq`-ish path expression.

```console
$ cat docker-compose.yml
services:
  web:
    image: nginx:1.27
    ports: [80, 443]

$ yaymlq '.services.web.image' docker-compose.yml
nginx:1.27

$ cat docker-compose.yml | yaymlq -o json '.services.web.ports'
[
  80,
  443
]
```

## Install

```sh
go install github.com/reticule-poirot/yaymlq@latest
```

Or build from source:

```sh
git clone https://github.com/reticule-poirot/yaymlq
cd yaymlq
make build      # produces bin/yaymlq
```

## Usage

```
yaymlq [flags] <path> [file]
```

Input comes from `file`, or from stdin when `file` is omitted or `-`.

### Path syntax

| Expression      | Meaning                                            |
|-----------------|----------------------------------------------------|
| `.a.b.c`        | walk map keys (the leading dot is optional)        |
| `a[0].b`        | slice index                                        |
| `a.0.b`         | a bare numeric segment is also a slice index       |
| `a[-1]`         | negative index counts from the end                |
| `a.*.b`         | wildcard — every value of a mapping or list        |
| `a[].b`, `a[*]` | wildcard, jq-style                                 |
| `"a.b".c`       | quote a segment with a literal dot (never special) |
| `` (empty), `.` | the whole document                                 |

A wildcard can produce **multiple results**, printed one after another. Map
values come out sorted by key. Once a wildcard has matched, a missing key or
out-of-range index on an individual branch is skipped rather than an error, so
`services.*.image` quietly returns only the services that set `image`:

```console
$ yaymlq 'services.*.image' docker-compose.yml
nginx:1.27
postgres:16
```

### Flags

| Flag                | Description                                                  |
|---------------------|-------------------------------------------------------------|
| `-o, --output`      | output format: `yaml` (default), `json`, `raw`              |
| `--raw`             | shorthand for `--output raw` (unquoted scalars)             |
| `--doc N`           | query document `N` in a multi-document stream               |
| `--all-docs`        | query every document in the stream                          |
| `--default VALUE`   | print `VALUE` (parsed as YAML) when the path has no match   |
| `-e, --exit-status` | exit `1` with no output when the path has no match          |
| `--max-bytes N`     | max input bytes to buffer (default 64 MiB; `0` = off)       |
| `--version`         | print version                                               |

### Missing paths, defaults, exit codes

By default an unresolved path is an error (exit `1`, message on stderr). For
scripting, opt into softer behaviour:

```console
$ yaymlq -e '.feature.enabled' cfg.yaml && echo on || echo off
off
$ yaymlq --default 0 '.replicas' cfg.yaml
0
```

With `--default` or `-e`, *any* unresolved path — missing key, wrong type,
out-of-range index — counts as "no match". Exit codes: `0` on success, `1` on
no match (`-e`) or any error.

## Editing: `yaymlq set`

```
yaymlq set [flags] <path> <value> [file]
```

Sets the value at `<path>` and prints the whole document; comments, key order,
and formatting are preserved. Missing intermediate mapping keys are created.
Wildcards are not allowed.

```console
$ yaymlq set '.services.web.image' nginx:1.28 docker-compose.yml
$ yaymlq set -i '.spec.replicas' 5 deployment.yaml      # rewrite the file
$ cat cfg.yaml | yaymlq set --string '.build' 007       # keep "007" a string
```

`<value>` is parsed as YAML (`8080` → int, `true` → bool); `-s/--string` forces
a string. `-i/--in-place` rewrites the file instead of printing — atomically
(temp file + rename), so a crash can't leave a truncated file, and the target's
mode is preserved. A symlinked path is replaced rather than written through.

### Handling untrusted input

- Input is capped at `--max-bytes` (64 MiB by default) before parsing, so an
  oversized file or stream can't exhaust memory. Raise it with `--max-bytes` or
  disable with `--max-bytes 0`.
- Without `--all-docs`, the stream is parsed only far enough to reach `--doc N`;
  documents after the one you asked for are never decoded.
- YAML alias-expansion bombs ("billion laughs") are rejected by the parser
  (`gopkg.in/yaml.v3` ≥ v3.0.1) with an "excessive aliasing" error.

## Development

```sh
make test     # go test ./...
make cover    # coverage summary
make lint     # golangci-lint (pinned, via `go run` — no install needed)
make fuzz     # short fuzz run over the path parser + resolver
make all      # fmt + vet + test + build
```

Golden-file tests in `cmd/` run the real command against `testdata/*.y*ml` and
compare output to `testdata/golden/*.golden`. Regenerate them after an
intentional change with:

```sh
go test ./cmd -run TestGolden -update
```

## Layout

```
main.go                 entrypoint
cmd/                     cobra commands (get + set), I/O, output rendering, exit codes
internal/path/           path expression parser (shared)
internal/query/          read-only resolver: path -> value(s)
internal/ymledit/        comment-preserving writer for `set`
```

## Project

- [CHANGELOG.md](CHANGELOG.md) — release notes
- [CONTRIBUTING.md](CONTRIBUTING.md) — dev workflow and PR checklist
- [SECURITY.md](SECURITY.md) — threat model and how to report a vulnerability

CI runs tests (with `-race`) on Linux, macOS, and Windows, plus `golangci-lint`,
`govulncheck`, CodeQL, a coverage floor, and a fuzz smoke on every push.

## License

MIT — see [LICENSE](LICENSE).
