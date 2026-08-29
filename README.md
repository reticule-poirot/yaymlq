# yaymlq

**Yet Another YAML Query** — a tiny Go CLI that pulls a single value out of a
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

| Flag              | Description                                             |
|-------------------|--------------------------------------------------------|
| `-o, --output`    | output format: `yaml` (default), `json`, `raw`         |
| `--raw`           | shorthand for `--output raw` (unquoted scalars)        |
| `--doc N`         | query document `N` in a multi-document stream          |
| `--all-docs`      | query every document in the stream                     |
| `--max-bytes N`   | max input bytes to buffer (default 64 MiB; `0` = off)  |
| `--version`       | print version                                          |

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
make lint     # golangci-lint (if installed)
make fuzz     # short fuzz run over the path parser + resolver
make all      # fmt + vet + test + build
```

## Layout

```
main.go                 entrypoint
cmd/                     cobra command, I/O, output rendering
internal/query/          path parser + resolver (the interesting bit)
```

## License

MIT — see [LICENSE](LICENSE).
