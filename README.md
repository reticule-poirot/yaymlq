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

| Expression      | Meaning                                        |
|-----------------|------------------------------------------------|
| `.a.b.c`        | walk map keys (the leading dot is optional)    |
| `a[0].b`        | slice index                                    |
| `a.0.b`         | a bare numeric segment is also a slice index   |
| `a[-1]`         | negative index counts from the end             |
| `"a.b".c`       | quote a segment that contains a literal dot    |
| `` (empty), `.` | the whole document                             |

### Flags

| Flag              | Description                                             |
|-------------------|--------------------------------------------------------|
| `-o, --output`    | output format: `yaml` (default), `json`, `raw`         |
| `--raw`           | shorthand for `--output raw` (unquoted scalars)        |
| `--doc N`         | query document `N` in a multi-document stream          |
| `--all-docs`      | query every document in the stream                     |
| `--version`       | print version                                          |

## Development

```sh
make test     # go test ./...
make cover    # coverage summary
make lint     # golangci-lint (if installed)
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
