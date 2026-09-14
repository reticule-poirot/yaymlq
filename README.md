# yaymlq

[![CI](https://github.com/reticule-poirot/yaymlq/actions/workflows/ci.yml/badge.svg)](https://github.com/reticule-poirot/yaymlq/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/reticule-poirot/yaymlq/badge)](https://scorecard.dev/viewer/?uri=github.com/reticule-poirot/yaymlq)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Yet Another YAML Query** — a tiny Go CLI that reads (and writes) values in a
YAML document using a `jq`-ish path expression.

> **Note:** this is a learning project — built to explore AI-assisted Go
> development end to end (design, tests, fuzzing, security review, CI). It is
> small and functional, but `yq` is the mature tool if you need one.

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

### Shell completion

```console
$ source <(yaymlq completion bash)     # current session
$ yaymlq completion zsh --help         # full instructions per shell
```

`bash`, `zsh`, `fish`, and `powershell` are supported; `yaymlq completion
<shell> --help` prints the exact setup command for that shell (where to write
the script for it to load automatically in new sessions).

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

For a script consuming multiple results, `-0/--print0` NUL-separates them (no
separator after the last one) instead of newline, so a result containing a
newline can't be split wrong:

```console
$ yaymlq -0 'services.*.image' docker-compose.yml | xargs -0 -n1 docker pull
```

### Flags

| Flag                | Description                                                  |
|---------------------|-------------------------------------------------------------|
| `-o, --output`      | output format: `yaml` (default), `json`, `raw`              |
| `--raw`             | shorthand for `--output raw` (unquoted scalars)             |
| `-0, --print0`      | NUL- instead of newline-separate multiple results (`xargs -0`); implies `--raw` |
| `--doc N`           | query document `N` in a multi-document stream               |
| `--all-docs`        | query every document in the stream                          |
| `--default VALUE`   | print `VALUE` (parsed as YAML) when the path has no match   |
| `-e, --exit-status` | exit `1` with no output when the path has no match          |
| `-q, --quiet`       | no output either way; exit `0` on a match, `1` otherwise (`grep -q`) |
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
$ yaymlq -q '.feature.enabled' cfg.yaml && echo present || echo absent
present
```

With `--default`, `-e`, or `-q`, *any* unresolved path — missing key, wrong
type, out-of-range index — counts as "no match". `-q` additionally suppresses
the matched value itself — a presence check that prints nothing on either
branch, the same way `grep -q` does (a hard error like a missing file or
malformed YAML still prints to stderr).

Exit codes are distinct per error class, so a script can tell "nothing
matched" apart from "something's actually broken":

| Code | Meaning                                                        |
|------|-----------------------------------------------------------------|
| `0`  | success                                                          |
| `1`  | no match (`-e`/`-q`), or `validate`'s aggregate failure          |
| `2`  | the input YAML didn't parse                                     |
| `3`  | a bad flag, argument count, path expression, or value            |
| `4`  | a file or stream couldn't be read or written                    |

With `-o json`, a failure writes one JSON object to stderr instead of a
prose line, so a script can branch on it without parsing English:

```console
$ yaymlq -o json '.services.web.nope' docker-compose.yml
{"error":"path not found: services.web.nope","kind":"no-match","path":"services.web.nope"}
$ echo 'a: [1, 2' | yaymlq -o json '.a'
{"error":"parsing YAML: yaml: line 1: did not find expected ',' or ']'","kind":"parse","line":1}
```

`kind` is one of `no-match`/`parse`/`usage`/`io`, matching the exit-code
table above. `line` (a parse failure) and `path` (an unresolved path) are
included when known, omitted otherwise — never guessed. Text-mode output
(the default) is unchanged, and `-e`/`-q`'s deliberate silence on a soft "no
match" holds no matter what `-o` is.

## Inspecting: `keys`, `len`, `type`

Read-only helpers that report *about* the node at a path rather than its value.
Output defaults to `raw`; `-o json` / `-o yaml`, `--doc`, `--all-docs`, and
`-0/--print0` work as with `get`.

```console
$ yaymlq keys .services docker-compose.yml     # mapping keys (sorted), one per line
db
web
$ yaymlq len .services.web.ports docker-compose.yml
2
$ yaymlq type .services docker-compose.yml
object
```

- **`keys`** — a mapping's keys (sorted, like wildcard order), or `0..n-1` for a
  list. Errors on a scalar.
- **`len`** — entry count of a mapping/list, rune count of a string, `0` for
  null. Errors on a number or boolean.
- **`type`** — `null`, `boolean`, `number`, `string`, `array`, or `object`.

## Validating: `yaymlq validate`

```
yaymlq validate [--require PATH]... [file...]
```

Checks that each input is well-formed YAML — syntax only, no schema. With no
arguments it reads stdin; with one or more files, every one is checked (even
after an earlier one fails), and the exit status is nonzero if any of them
did.

```console
$ yaymlq validate docker-compose.yml
$ yaymlq validate *.yaml && echo "all valid"
$ echo 'a: [1, 2' | yaymlq validate
stdin: parsing YAML: yaml: line 1: did not find expected ',' or ']'
```

`--require PATH` (repeatable) additionally asserts a path resolves in at
least one document of each source — a well-formed file that's missing a
required field is reported the same way a parse failure is:

```console
$ yaymlq validate --require .image.tag --require .replicas deployment.yaml
deployment.yaml: missing required path(s): .replicas
```

## Editing: `yaymlq set`

```
yaymlq set [flags] <path> <value> [file]
```

Sets the value at `<path>` and prints the whole document; comments, key order,
quoting, and blank lines are preserved. Missing intermediate mapping keys are
created. Wildcards are not allowed.

> A run of blank lines collapses to one, and the space between a value and its
> trailing `# comment` is normalized to one — limitations of the underlying
> `gopkg.in/yaml.v3` re-serializer. `set`, `append`, `delete`, and `rename`
> all behave this way. CRLF line endings are preserved when the source
> consistently uses them; a source that mixes `\r\n` and `\n` is written back
> as plain `\n`.

```console
$ yaymlq set '.services.web.image' nginx:1.28 docker-compose.yml
$ yaymlq set -i '.spec.replicas' 5 deployment.yaml      # rewrite the file
$ cat cfg.yaml | yaymlq set --string '.build' 007       # keep "007" a string
$ yaymlq set '.services.web.labels' '{team: infra}' docker-compose.yml
```

`<value>` is parsed as YAML: a scalar (`8080` → int, `true` → bool), or a
collection (`{a: 1}`, `[80, 443]`). `-s/--string` takes the whole argument
verbatim as a string. `-i/--in-place` rewrites the file instead of printing — atomically
(temp file + rename), so a crash can't leave a truncated file, and the target's
mode is preserved. A symlinked path is replaced rather than written through.
`--indent N` sets spaces per level; left unset, it's auto-detected from the
source (a 4-space file stays 4-space) and falls back to 2 for a flat document.
`--diff`/`--dry-run` prints a unified diff instead of writing or printing —
see "Previewing a change" below.

## Appending: `yaymlq append`

```
yaymlq append [flags] <path> <value> [file]
```

Adds `<value>` as the last element of the list at `<path>`. The path must
already resolve to a list. Same `<value>` parsing and same flags as `set`
(`-s/--string`, `-i/--in-place`, `--doc`, `--max-bytes`, `--indent`,
`--diff`/`--dry-run`).

```console
$ yaymlq append '.services.web.ports' '"9090:9090"' docker-compose.yml
$ yaymlq append -i '.spec.template.spec.containers' '{name: proxy, image: envoy}' k8s.yaml
$ cat cfg.yaml | yaymlq append .tags newtag
```

## Deleting: `yaymlq delete`

```
yaymlq delete [flags] <path> [file]     # aliases: del, rm
```

Removes the mapping key or list element at `<path>` and prints the whole
document; comments and key order on everything that remains are preserved.
Wildcards are not allowed, and deleting a path that isn't there is an error.
Shares `set`'s `-i/--in-place`, `--doc`, `--max-bytes`, `--indent`, and
`--diff`/`--dry-run` flags and its atomic write path.

```console
$ yaymlq delete '.services.web.environment.APP_ENV' docker-compose.yml
$ yaymlq delete -i '.spec.template.spec.containers[1]' deployment.yaml
$ cat cfg.yaml | yaymlq rm .debug
```

## Renaming: `yaymlq rename`

```
yaymlq rename [flags] <path> <newkey> [file]
```

Renames the mapping key at `<path>` to `<newkey>` and prints the whole
document; the key's position, value, and comments are untouched. `<path>`
must resolve to a mapping key — not a list index or a wildcard. Renaming to a
name that already exists as a sibling is an error; renaming a key to its own
name is a no-op. Shares `set`'s `-i/--in-place`, `--doc`, `--max-bytes`,
`--indent`, and `--diff`/`--dry-run` flags and its atomic write path.

```console
$ yaymlq rename '.services.web' webapp docker-compose.yml
$ yaymlq rename -i '.metadata.labels."app"' name k8s.yaml
$ cat cfg.yaml | yaymlq rename .oldName newName
```

### Previewing a change: `--diff`/`--dry-run`

`set`/`append`/`delete`/`rename` all accept `--diff` (or its alias
`--dry-run`): instead of writing (`-i`) or printing the whole document,
print a unified diff of just what would change. Useful for checking an edit
before it lands — an agent shelling out to `yaymlq` can verify the change
without a separate read-before/read-after/diff-them-yourself step.

```console
$ yaymlq set --diff '.services.web.image' nginx:1.28 docker-compose.yml
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@ -2,7 +2,7 @@

 services:
   web:
-    image: nginx:1.27
+    image: nginx:1.28
     ports:
       - "80:80"
       - "443:443"
```

It works with or without `-i`: without it, `--diff` replaces printing the
whole new document; with it, `--diff` replaces the write — the file is
never touched. Exit code is `0` whether or not there were changes; it's a
preview, not an assertion. Identical input/output prints nothing at all,
the same way `diff -u` does on two identical files.

## Batch editing: `yaymlq apply`

```
yaymlq apply -f <edits> [flags] [file]
```

Runs a batch of `set`/`append`/`delete`/`rename` edits in one parse/mutate/
serialize pass — useful when you have several changes to make and don't want
one invocation (and one re-serialize, and with `-i` one atomic write) per
edit. `-f`/`--edits` points at a script file, or `-` to read it from stdin
(only one of the script or the document can come from stdin at a time —
point `-f` at a real file when reading the document from a pipe).

```console
$ cat edits.txt
# bump the image, drop a stale env var
set .services.web.image = nginx:1.28
delete .services.web.environment.APP_ENV

$ yaymlq apply -f edits.txt docker-compose.yml
$ yaymlq apply -f edits.txt -i docker-compose.yml    # rewrite the file
$ printf 'set .a = 1\ndelete .b\n' | yaymlq apply -f - config.yaml
```

Script format — one operation per line, blank lines and `#` comments ignored:

| Line                          | Same as                              |
|--------------------------------|---------------------------------------|
| `set <path> = <value>`         | `yaymlq set <path> <value>`           |
| `append <path> = <value>`      | `yaymlq append <path> <value>`        |
| `delete <path>`                | `yaymlq delete <path>`                |
| `rename <path> = <newkey>`     | `yaymlq rename <path> <newkey>`       |

`<value>` is parsed as YAML, exactly like `set`/`append`'s own argument;
`<newkey>` is literal, exactly like `rename`'s own argument. If any op
fails, nothing is written — the whole batch applies to the same in-memory
document before a single encode/write, so a failure partway through never
leaves a partial edit. Shares `set`'s `-i/--in-place`, `--doc`,
`--max-bytes`, `--indent`, and `--diff`/`--dry-run` flags and its atomic
write path.

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
make fuzz     # short fuzz run: path parser, resolver, set/append/delete writer, whole CLI
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
cmd/                     cobra commands (get, set, append, delete, rename, apply, keys/len/type), I/O
internal/path/           path expression parser (shared)
internal/query/          read-only resolver: path -> value(s)
internal/ymledit/        comment-preserving writer for `set` and `delete`
internal/editscript/     `apply`'s batch-edit script parser
```

## Project

- [CHANGELOG.md](CHANGELOG.md) — release notes
- [CONTRIBUTING.md](CONTRIBUTING.md) — dev workflow and PR checklist
- [SECURITY.md](SECURITY.md) — threat model and how to report a vulnerability

CI runs tests (with `-race`) on Linux, macOS, and Windows, plus `golangci-lint`,
`govulncheck`, CodeQL, a coverage floor, and a fuzz smoke on every push.

## License

MIT — see [LICENSE](LICENSE).
