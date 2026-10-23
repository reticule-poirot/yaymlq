---
name: using-yaymlq
description: Use when reading, querying, or editing a YAML file (docker-compose, Kubernetes manifests, CI configs, etc.) programmatically — before hand-rolling a sed/awk/python edit, before a manual parse-mutate-dump round-trip that risks losing comments or reordering keys, or before guessing a YAML CLI's flags from --help.
---

# Using yaymlq

Path-based YAML CLI (github.com/reticule-poirot/yaymlq): read or edit a
value by a jq-like path — get, set, append, delete, rename — in place,
preserving comments, key order and formatting.

**Don't use it for:** a Go/Helm `{{ ... }}` template — refused with exit 2,
so render it first or edit the chart's `values.yaml`; a transform that is
neither a path edit nor reachable through `--paths` (reordering a list by a
computed key, say) — use a YAML library.

## Commands

| Task | Command |
|---|---|
| Read a value | `yaymlq '.services.web.image' compose.yml` |
| Read, JSON out | `yaymlq -o json '.spec.replicas' deploy.yaml` |
| List paths a wildcard matched | `yaymlq --paths '.services.*.image' compose.yml` |
| List keys / count / type | `yaymlq keys .services compose.yml` |
| Check a file parses | `yaymlq validate config.yaml` |
| Require a path in every document | `yaymlq validate --require .spec.replicas --all-docs manifests.yaml` |
| Set in place | `yaymlq set -i '.spec.replicas' 5 deploy.yaml` |
| Set only if the path exists | `yaymlq set -i --must-exist '.spec.replicas' 5 deploy.yaml` |
| Preview without writing (`--dry-run` is an alias) | `yaymlq set --diff '.spec.replicas' 5 deploy.yaml` |
| Write and report the diff | `yaymlq set -i --show-diff '.spec.replicas' 5 deploy.yaml` |
| Append to a list | `yaymlq append -i '.services.web.ports' '"8080:8080"' compose.yml` |
| Delete a key/element | `yaymlq delete -i '.services.web.environment.DEBUG' compose.yml` |
| Rename a key | `yaymlq rename -i '.services.web' webapp compose.yml` |
| Same edit in every document | `yaymlq set -i --all-docs '.metadata.labels.team' platform manifests.yaml` |
| Batch edits, one write | `yaymlq apply -f edits.txt -i compose.yml` |
| A verb's own flags, as JSON | `yaymlq schema --command set` |

For anything not in this table use `schema --command <verb>` (~4KB,
reflected from the live command tree). A bare `yaymlq schema` is the whole
~27KB surface — don't fetch it to answer one question. Don't parse `--help`.

## Behavior the flag names don't tell you

- `set` **creates** a path it can't find, so a typo silently adds a key.
  `--must-exist` refuses instead, and requires every segment to resolve.
  `delete`/`rename`/`append` already fail on a missing path.
- `--all-docs` is a **selector** on every verb: a document without the path
  is skipped, only a run matching *nothing* fails. The strict "every document
  must have it" reading is `validate --require PATH --all-docs` alone.
- `--doc N` and `--all-docs` are mutually exclusive.
- Editing verbs refuse a wildcard path. The route from a wildcard to an edit
  is `--paths` → literal paths → `apply`.
- An `apply` script is one op per line (`<path> = <value>`); each op may
  carry `--doc N` before the path, one that doesn't follows the invocation's
  scope. One parse, one write; if any op fails nothing is written.
- Without `-i`/`--in-place` the result goes to stdout; the file is untouched.
- A `<value>` starting with `#` parses as a comment, so quote it or pass
  `-s`/`--string`.
- `--diff-format json` is the machine-readable form of `--diff`/`--show-diff`.
- Exit codes: 0 ok, 1 no match, 2 parse, 3 usage, 4 I/O.

## Shapes to parse

A miss carries what you need to recover — don't follow it with a `keys` call:

```console
$ yaymlq -o json '.services.web.imagee' compose.yml
{"error":"path not found: services.web.imagee","kind":"no-match","path":"services.web.imagee","available":["environment","image","ports"],"suggestion":"image"}
```

`available` is the mapping's full key list, uncapped. Text mode puts the
same facts in the message: `path not found: services.web.imagee (did you
mean "image"?)`; an editing verb's wording leads with the path instead
(`services.web.imagee: no such key (did you mean "image"?)`).

## Find then edit

```console
$ yaymlq --paths '.services.*.image' compose.yml
services.db.image
services.web.image
$ yaymlq --paths '.services.*.image' compose.yml | sed 's/^/set /; s/$/ = nginx:1.28/' > edits.txt
$ yaymlq apply -f edits.txt -i compose.yml
```

Across a stream add `-o json`: it is the only form carrying the document
index, and a bare path repeats per document, so a script built from the text
form edits document 0 over and over.

```console
$ yaymlq --paths --all-docs -o json '.spec.template.spec.containers[*].image' manifests.yaml
{"doc":1,"path":"spec.template.spec.containers[0].image"}
```

Pipe that through `jq -r '"set --doc \(.doc) \(.path) = nginx:1.28"'` to get
an `apply` script. When the value is the *same* everywhere, `--all-docs` on
the editing verb is one call instead of three.
