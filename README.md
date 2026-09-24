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
| `"a\nb"`        | inside quotes, `\` escapes: `\\` `\"` `\'` `\n` `\t` `\r` |
| `` (empty), `.` | the whole document                                 |

Quoting and escaping exist so that every key a YAML document can hold is
addressable, and so that a path this tool *prints* can be fed back to it —
including keys containing a dot, a quote character, a line break or a
backslash. Outside quotes a backslash is an ordinary character; inside them an
unrecognised escape is a syntax error rather than a silently dropped
backslash, so a mistake can't resolve somewhere unintended.

A wildcard can produce **multiple results**, printed one after another. Map
values come out sorted by key. Once a wildcard has matched, a missing key or
out-of-range index on an individual branch is skipped rather than an error, so
`services.*.image` quietly returns only the services that set `image`:

```console
$ yaymlq 'services.*.image' docker-compose.yml
nginx:1.27
postgres:16
```

A wildcard says *what* matched, never *where*. `--paths` prints each match's
resolved path instead of its value, which is what turns a query into an edit:

```console
$ yaymlq --paths '.jobs.*.steps[*].with.go-version' .github/workflows/ci.yml
jobs.lint.steps[1].with.go-version
jobs.test.steps[1].with.go-version

$ yaymlq --paths '.jobs.*.steps[*].with.go-version' ci.yml \
    | sed 's/^/set /; s/$/ = "1.27"/' > bump.txt
$ yaymlq apply -i -f bump.txt ci.yml
```

Across a multi-document stream a bare path is ambiguous — two documents can
hold the same path, and nothing in the text output says which is which, so
`-o json` carries the document index alongside it:

```console
$ yaymlq --paths --all-docs -o json '.image' manifests.yml
{"doc":0,"path":"image"}
{"doc":1,"path":"image"}

$ yaymlq --paths --all-docs -o json '.image' manifests.yml \
    | jq -r '"--doc \(.doc) \(.path)"'
--doc 0 image
--doc 1 image
```

`doc` is exactly the value to pass to `--doc`. In text mode `--paths
--all-docs` prints a note to stderr saying it can't tell the documents apart;
stdout and the exit code are unchanged, so an existing pipe keeps working.

`set`/`append`/`delete`/`rename` all refuse a wildcard path, and `apply`
scripts take literal paths only, so `--paths` is the bridge between the two.
Each path is printed as an expression that parses back to the same place — a
key containing a `.`, or one that looks like an index, comes out quoted.

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
| `-0, --print0`      | NUL- instead of newline-separate multiple results (`xargs -0`); implies `-o raw` |
| `--paths`           | print each match's resolved path instead of its value; implies `-o raw`, or `-o json` for `{"doc","path"}` objects |
| `--doc N`           | query document `N` in a multi-document stream               |
| `--all-docs`        | query every document in the stream                          |
| `--default VALUE`   | print `VALUE` (parsed as YAML) when the path has no match   |
| `-e, --exit-status` | exit `1` with no output when the path has no match          |
| `-q, --quiet`       | no output either way; exit `0` on a match, `1` otherwise (`grep -q`) |
| `--max-bytes N`     | max input bytes to buffer (default 64 MiB; `0` = off)       |
| `--max-suggestions N` | max keys a "path not found" error lists (default 10; `0` = all) |
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

A key that misses a mapping says what was there, so a typo doesn't cost a
second call to find out:

```console
$ yaymlq '.jobs.tset.steps' .github/workflows/ci.yml
Error: path not found: jobs.tset (did you mean "test"?)

$ yaymlq '.jobs.replicas' .github/workflows/ci.yml
Error: path not found: jobs.replicas (available keys: changes, govulncheck, hygiene, lint, test)
```

The nearest key is offered when the miss looks like a typo (one edit away,
or differing only in case — swapping two neighbours counts as one edit);
otherwise the keys themselves are listed. `--max-suggestions N` caps how many
are listed (default 10, `0` for all of them) so a wide mapping can't bury the
error it came with — the search for a near match always covers every key
regardless. Only a key lookup against a mapping gets this: an out-of-range
index or a scalar in the way already says so. `-e`/`-q`/`--default` stay
silent, as always.

`delete`, `rename`, `append` and `apply` report a missing key the same way:

```console
$ yaymlq delete .jobs.tset ci.yml
Error: jobs.tset: no such key (did you mean "test"?)
$ yaymlq apply -f edits.txt ci.yml
Error: line 2: jobs.tset: no such key (did you mean "test"?)
```

`set` is the exception, and not by omission: it *creates* a key it can't
find, so a typo there produces a new key rather than an error. Check with
`--diff` before writing with `-i`.

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
{"error":"path not found: services.web.nope","kind":"no-match","path":"services.web.nope","available":["environment","image","ports"]}
$ yaymlq -o json '.services.web.imag' docker-compose.yml
{"error":"path not found: services.web.imag","kind":"no-match","path":"services.web.imag","available":["environment","image","ports"],"suggestion":"image"}
$ echo 'a: [1, 2' | yaymlq -o json '.a'
{"error":"parsing YAML: yaml: line 1: did not find expected ',' or ']'","kind":"parse","line":1}
```

`kind` is one of `no-match`/`parse`/`usage`/`io`, matching the exit-code
table above. `line` (a parse failure) and `path` (an unresolved path) are
included when known, omitted otherwise — never guessed. `available` carries
the keys as an array rather than repeating them in `error`'s prose, with
`availableTotal` present only when `--max-suggestions` cut the list, and
`suggestion` only when one key is a plausible typo — the same facts as the
prose line above, as data instead of a sentence, so the message itself stays
un-annotated here. `-e`/`-q`'s deliberate silence on a soft "no match" holds
no matter what `-o` is.

## Inspecting: `keys`, `len`, `type`

Read-only helpers that report *about* the node at a path rather than its value.
Output defaults to `raw`; `-o json` / `-o yaml`, `--doc`, `--all-docs`,
`-0/--print0`, and `--max-suggestions` work as with `get`.

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

On a multi-document stream — which in a Kubernetes repo is most files — "at
least one" is often the weaker check than you want. `--all-docs` makes
`--require` strict, and `--doc N` narrows it to one document:

```console
$ cat k8s.yaml                  # a ConfigMap, then a Deployment
$ yaymlq validate --require .spec.replicas k8s.yaml
$ echo $?
0                               # the Deployment has it; the default is satisfied

$ yaymlq validate --all-docs --require .metadata.name k8s.yaml
$ echo $?
0                               # every document names itself

$ yaymlq validate --all-docs --require .spec.replicas k8s.yaml
k8s.yaml: required path(s) missing from at least one document: .spec.replicas
$ echo $?
1

$ yaymlq validate --doc 1 --require .spec.replicas k8s.yaml
$ echo $?
0                               # checked against the Deployment alone
```

Both only scope `--require`; `validate` always parses the whole stream, so
neither weakens the syntax check. Passing either without `--require` is a
usage error (exit 3) rather than a flag that quietly does nothing, and a
`--doc` beyond the end of a source is that source failing (exit 1), not a bad
command line — `validate` reports per source and keeps checking the rest.

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
verbatim as a string — needed for a value that starts with `#` (`#ffffff`),
since YAML always reads a leading `#` as a comment; quoting the value
(`'"#ffffff"'`) works too. `-i/--in-place` rewrites the file instead of printing — atomically
(temp file + rename, both fsync'd, including the directory entry), so a
crash or power loss can't leave a truncated file or silently lose a
completed edit, and the target's
permission bits are preserved (ownership, ACLs, and extended attributes are
not — the file is replaced, not modified in place). A symlinked path is
replaced rather than written through: the edit is read from whatever the
link points at, but written to a new file at the link's own path, so the
link is gone afterward and the file it pointed at is untouched. yaymlq
prints a one-line note to stderr when this happens; there's no flag to
follow the link instead. The rename also breaks any hardlinks to the
target — expected for an atomic-replace write, but a different file
identity than an in-place modification would give you. Interrupting an
in-place edit (Ctrl-C, a killed
process) can leave a `.<name>.yaymlq-<random>` sibling file next to the
target, holding a full copy of the document as of that moment — yaymlq
doesn't install a signal handler to clean it up. It's always at least as
restrictive a permission as the target (0600 if the interrupt lands
before permissions are copied over) and safe to delete.
`--indent N` sets spaces per level; left unset, it's auto-detected from the
source (a 4-space file stays 4-space) and falls back to 2 for a flat document.
`--diff`/`--dry-run` prints a unified diff instead of writing or printing,
and `--diff-format text|json` picks how that preview is rendered — see
"Previewing a change" below.

## Appending: `yaymlq append`

```
yaymlq append [flags] <path> <value> [file]
```

Adds `<value>` as the last element of the list at `<path>`. The path must
already resolve to a list. Same `<value>` parsing and same flags as `set`
(`-s/--string`, `-i/--in-place`, `--doc`, `--max-bytes`, `--indent`,
`--diff`/`--dry-run`, `--show-diff`, `--diff-format`).

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
Shares `set`'s `-i/--in-place`, `--doc`, `--max-bytes`, `--indent`,
`--diff`/`--dry-run`, `--show-diff`, and `--diff-format` flags and its atomic
write path.

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
`--indent`, `--diff`/`--dry-run`, `--show-diff`, and `--diff-format` flags and its atomic
write path.

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

#### Writing *and* reporting: `--show-diff`

`--diff` previews without writing and `-i` writes without saying anything,
so getting both used to cost two invocations — or a write followed by
re-reading the file to see what changed. `--show-diff` does them in one:

```console
$ yaymlq set -i --show-diff '.spec.replicas' 5 deployment.yaml
--- deployment.yaml
+++ deployment.yaml
@@ -4,7 +4,7 @@
 spec:
-  replicas: 2
+  replicas: 5
$ grep replicas deployment.yaml
  replicas: 5
```

It requires `-i` — without it the edited document already goes to stdout,
and a diff there would be interleaved with the output it describes.
`--diff` and `--show-diff` together is a usage error (exit 3): one previews
without writing, the other writes and then reports. The file is written
first and the diff printed second, so a failed write never prints a diff
describing a change that didn't happen. `--diff-format json` applies here
too, and a no-op edit prints nothing, the same as `--diff`.

#### Structured output: `--diff-format json`

`--diff-format` selects how the preview is rendered: `text` (the default,
the unified diff above) or `json`, for a caller that would otherwise have to
parse that text. It only applies alongside `--diff`/`--dry-run`; passing it
on its own is a usage error rather than a silently ignored flag.

```console
$ printf 'a: 1\nb: 2\n' | yaymlq set --diff --diff-format json .a 9
{"file":"stdin","changed":true,"hunks":[{"aStart":1,"aCount":2,"bStart":1,"bCount":2,
 "lines":[{"op":"del","text":"a: 1","aLine":1},
          {"op":"add","text":"a: 9","bLine":1},
          {"op":"same","text":"b: 2","aLine":2,"bLine":2}]}]}
```

(wrapped here for readability — the real output is a single line.)

- `file` is the path being edited, or `stdin`.
- `changed` answers "would this edit do anything?" without diffing the
  text yourself.
- `hunks` carries the same `@@` numbers the text format prints, and each
  line's `op` is `same`, `add`, or `del`.
- `aLine`/`bLine` give that line's 1-indexed position on each side, omitted
  where the line doesn't exist on that side — so you never have to count
  from the hunk header. `aNoNewline`/`bNoNewline` mark a side whose last
  line has no trailing newline (the text format's
  `\ No newline at end of file`), named per side because a context line
  belongs to both.

Unlike text mode, a no-op edit still prints a complete object rather than
nothing, so a consumer doesn't need a special case for empty input:

```console
$ printf 'a: 1\n' | yaymlq set --diff --diff-format json .a 1
{"file":"stdin","changed":false,"hunks":[]}
```

`hunks` is always an array, never `null`.

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
| `<verb> --doc N <path> …`      | `yaymlq <verb> --doc N <path> …`      |

The `=` that separates path from value is the first one the path isn't
quoting, so a key containing one is addressable as long as it's quoted —
`set "a = b" = new`. This is what `--paths` emits for such a key, so a
generated script stays correct.

`--doc N` before the path names the document an op applies to; without it an
op follows `apply`'s own `--doc`. One script can therefore edit several
documents of a stream in a single pass, which is what closes the loop from a
listing to a batch edit:

```console
$ yaymlq --paths --all-docs -o json '.image' manifests.yml \
    | jq -r '"set --doc \(.doc) \(.path) = \"nginx:1.28\""' > bump.txt
$ cat bump.txt
set --doc 0 image = "nginx:1.28"
set --doc 1 image = "nginx:1.28"
$ yaymlq apply -i -f bump.txt manifests.yml
```

A key spelled like a flag is quoted by `--paths` (`"--doc"`) so a generated
script can't mistake it for one.

`<value>` is parsed as YAML, exactly like `set`/`append`'s own argument —
there's no per-op `-s/--string`, so a value that starts with `#` needs
quoting (`set .color = "#ffffff"`), the same reason `set`'s own CLI argument
does. `<newkey>` is literal, exactly like `rename`'s own argument. If any op
fails, nothing is written — the whole batch applies to the same in-memory
document before a single encode/write, so a failure partway through never
leaves a partial edit. Shares `set`'s `-i/--in-place`, `--doc`,
`--max-bytes`, `--indent`, `--diff`/`--dry-run`, `--diff-format`, and
`--max-suggestions` flags and its atomic write path. `--max-bytes` bounds the edit script (`-f`/`--edits`) too, not
just the document being edited.

### Handling untrusted input

- Input is capped at `--max-bytes` (64 MiB by default) before parsing. Raise it
  with `--max-bytes` or disable it with `--max-bytes 0`.
- `--max-bytes` bounds the bytes read, not peak memory: a decoded document, and
  especially a `--diff`/`--dry-run` preview (which briefly holds both the
  before and after document in memory), can use tens of times that in RSS.
  Pick a conservative `--max-bytes` on a memory-constrained host rather than
  relying on the default to also cap memory use.
- Without `--all-docs`, the stream is parsed only far enough to reach `--doc N`;
  documents after the one you asked for are never decoded.
- YAML alias-expansion bombs ("billion laughs") are rejected by the parser
  (`gopkg.in/yaml.v3` ≥ v3.0.1) with an "excessive aliasing" error.

## Introspection: `yaymlq schema`

Prints a JSON manifest of yaymlq's own commands, flags, argument-count
constraints, and exit-code scheme — for a script or an LLM agent to consume
instead of parsing `--help` text or guessing what an exit code means.

```console
$ yaymlq schema | jq '.commands[].name'
"get"
"append"
"apply"
...
$ yaymlq schema | jq '.exitCodes'
[
  {"code": 0, "name": "success", "description": "the command completed successfully"},
  ...
]
```

`--command NAME` (repeatable) limits the manifest to the commands you name,
for when you only need one verb's contract rather than the whole surface:

```console
$ yaymlq schema --command set | jq '.commands[0].flags[].name'
"diff"
"diff-format"
...
```

The full manifest is ~21KB; a single command is ~3.1KB, so a targeted
lookup costs a fraction of the whole. Commands come back in tree order
whichever order you name them, and `version`/`exitCodes` are always included
— a caller asking about one verb still needs the exit-code table to interpret
what that verb returns. An unrecognized name is a usage error (exit 3) that
lists the valid ones.

The manifest is built by reflecting over the live command tree, so it can't
drift from the CLI's real flags as they change; `manifestVersion` is bumped
if the JSON shape itself ever changes incompatibly.

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
- [contrib/claude-skill/SKILL.md](contrib/claude-skill/SKILL.md) — a
  [Claude Code](https://claude.com/claude-code) skill that teaches it to
  reach for `yaymlq` instead of sed/awk/hand-rolled parsing when editing
  YAML; copy it to `~/.claude/skills/using-yaymlq/SKILL.md` to use it

CI runs tests (with `-race`) on Linux, macOS, and Windows, plus `golangci-lint`,
`govulncheck`, CodeQL, a coverage floor, and a fuzz smoke on every push.

## License

MIT — see [LICENSE](LICENSE).
