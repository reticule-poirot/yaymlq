---
name: using-yaymlq
description: Use when reading, querying, or editing a YAML file (docker-compose, Kubernetes manifests, CI configs, etc.) programmatically — before hand-rolling a sed/awk/python edit, before a manual parse-mutate-dump round-trip that risks losing comments or reordering keys, or before guessing a YAML CLI's flags from --help.
---

# Using yaymlq

## Overview

`yaymlq` (github.com/reticule-poirot/yaymlq) is a path-based YAML CLI:
get/set/append/delete/rename a value by a jq-like path expression, editing
in place while preserving comments, key order, and formatting. Prefer it
over sed/awk on YAML (fragile against indentation/quoting) or a
parse-then-dump round-trip (loses comments, may reorder keys) whenever the
change is a value at a path.

## When to Use

- Reading one value from YAML by path (`.services.web.image`,
  `.spec.replicas`) instead of grepping or writing a parser.
- Changing/adding/deleting/renaming while the file must stay diffable
  (comments, key order, indentation preserved).
- Several related edits, applied atomically in one parse/write pass.
- The same edit across every document of a multi-document file.
- Finding what to edit first: a wildcard query prints the paths it matched,
  and each one is a literal path an editing command accepts.
- Checking a YAML file is well-formed, or that a path resolves, before
  trusting it (e.g. a pre-commit or CI step).
- Scripting against it reliably: distinct exit codes per failure class,
  `-o json` structured errors and diffs, `yaymlq schema --command <verb>`
  for a verb's own flags as JSON — don't parse `--help` text.

**Skip it for:** a trivial one-off tweak not worth checking `yaymlq` is
installed; a Go/Helm chart template — a `{{ ... }}` directive is refused
with exit 2, so render it first (`helm template`) or edit the chart's
`values.yaml`; a transform that is neither get/set/append/delete/rename at a
path nor reachable by discovering paths first (e.g. reordering list elements
by a computed key) — use a real YAML library instead.

## Quick Reference

| Task | Command |
|---|---|
| Read a value | `yaymlq '.services.web.image' compose.yml` |
| Read, JSON out | `yaymlq -o json '.spec.replicas' deploy.yaml` |
| List the paths a wildcard matched | `yaymlq --paths '.services.*.image' compose.yml` |
| Set a value in place | `yaymlq set -i '.spec.replicas' 5 deploy.yaml` |
| Preview a change first | `yaymlq set --diff '.spec.replicas' 5 deploy.yaml` |
| Write *and* report what changed | `yaymlq set -i --show-diff '.spec.replicas' 5 deploy.yaml` |
| Refuse to create a missing path | `yaymlq set -i --must-exist '.spec.replicas' 5 deploy.yaml` |
| Append to a list | `yaymlq append -i '.services.web.ports' '"8080:8080"' compose.yml` |
| Delete a key/element | `yaymlq delete -i '.services.web.environment.DEBUG' compose.yml` |
| Rename a key | `yaymlq rename -i '.services.web' webapp compose.yml` |
| Same edit in every document | `yaymlq set -i --all-docs '.metadata.labels.team' platform manifests.yaml` |
| Batch edits, one write | `yaymlq apply -f edits.txt -i compose.yml` |
| List keys / count / type | `yaymlq keys .services compose.yml` |
| Require a path in *every* document | `yaymlq validate --require .spec.replicas --all-docs manifests.yaml` |
| Look up one command's flags | `yaymlq schema --command set` |

Flags change; this table doesn't track them. Ask the binary — but ask it
narrowly: `yaymlq schema --command set` is ~4KB, the full `yaymlq schema`
manifest is ~27KB (≈7,000 tokens) to answer one question about one verb.
`--command` is repeatable.

## Multi-document files

Kubernetes manifests and `---`-separated streams are the common case, and
every command addresses them the same way: `--doc N` picks one document,
`--all-docs` means all of them.

`--all-docs` is a **selector, not an assertion**: a document that doesn't
have the path is skipped, and only a run that matched *nothing* fails
(exit 1). That holds on both sides — `get`/`keys`/`len`/`type` and
`set`/`append`/`delete`/`rename`/`apply` — so the same edit can be pointed
at a heterogeneous manifest without first splitting out the documents that
happen to have the field:

```console
$ yaymlq set -i --show-diff --all-docs --must-exist \
    '.spec.template.spec.containers[0].image' nginx:1.28 manifests.yaml
--- a/manifests.yaml
+++ b/manifests.yaml
@@ -15,7 +15,7 @@
     spec:
       containers:
         - name: web
-          image: nginx:1.27
+          image: nginx:1.28
 ---
 apiVersion: v1
 kind: Service
```

The ConfigMap and the Service have no containers; they were skipped, not
rewritten and not an error.

The strict "every document must have it" reading is `validate`'s, and lives
only there — without `--all-docs`, `--require` passes when *any one*
document has the path, which on a manifest is rarely what the caller means:

```console
$ yaymlq validate --require .spec.replicas manifests.yaml
$ echo $?
0                                           # only the Deployment has it
$ yaymlq validate --require .spec.replicas --all-docs manifests.yaml
manifests.yaml: required path(s) missing from at least one document: .spec.replicas
$ echo $?
1
```

An `apply` script spans a stream too: any op may carry its own `--doc N`
before the path, and an op that names none follows the invocation's scope.

## Find then edit

Every editing command refuses a wildcard, so a wildcard match used to be a
dead end. `--paths` is the bridge: it prints each match's resolved path
instead of its value, and those lines are literal paths.

```console
$ yaymlq --paths '.services.*.image' compose.yml
services.db.image
services.web.image
$ yaymlq --paths '.services.*.image' compose.yml | sed 's/^/set /; s/$/ = nginx:1.28/' > edits.txt
$ yaymlq apply -f edits.txt -i compose.yml
```

Across a stream, use `-o json` — the only form that carries the document
index, and the only `-o` besides the implied `raw` that `--paths` accepts at
all. A bare path repeats across documents, so a script built from the text
form would edit the first document over and over:

```console
$ yaymlq --paths --all-docs -o json '.spec.template.spec.containers[*].image' manifests.yaml
{"doc":1,"path":"spec.template.spec.containers[0].image"}
$ yaymlq --paths --all-docs -o json '.spec.template.spec.containers[*].image' manifests.yaml \
    | jq -r '"set --doc \(.doc) \(.path) = nginx:1.28"' > edits.txt
$ yaymlq apply -f edits.txt -i manifests.yaml
```

Reach for this when the edit differs per match, or when the matches need
inspecting first. When it's the *same* value everywhere, `--all-docs` on the
editing command itself is one call instead of three.

## When a path doesn't resolve

A wrong path is the most likely way a call fails, and the error already
carries what's needed to recover — no second `keys` call:

```console
$ yaymlq '.services.web.imagee' compose.yml
Error: path not found: services.web.imagee (did you mean "image"?)
$ yaymlq delete '.services.web.imagee' compose.yml
Error: services.web.imagee: no such key (did you mean "image"?)
```

Under `-o json` the same facts come back structured, which is the form worth
using in a script — `available` is the full key list, uncapped:

```console
$ yaymlq -o json '.services.web.imagee' compose.yml
{"error":"path not found: services.web.imagee","kind":"no-match","path":"services.web.imagee","available":["environment","image","ports"],"suggestion":"image"}
```

Exit 1 is "no match" specifically; 2 is a parse failure, 3 a usage error,
4 I/O. Every `schema` output carries the full `exitCodes` list, narrowed by
`--command` or not, so one targeted call answers both questions.

## Common Mistakes

- **Editing without `-i`/`--in-place`** — output goes to stdout, the file
  is untouched. Preview with `--diff`/`--dry-run`, or use `--show-diff` to
  write and report in the same call rather than re-reading the file to see
  what happened. `--diff-format json` is the machine-readable form of both.
- **Assuming `set` fails on a typo** — it *creates* what it can't find, so
  `.spec.replicass` quietly adds a key. Pass `--must-exist` when the path is
  meant to exist already (every segment must resolve, intermediates
  included). `delete`/`rename`/`append` behave that way without a flag.
- **A `<value>` starting with `#`** reads as a YAML comment (empty value)
  unless quoted or passed with `-s`/`--string`.
- **One `set` call per edit** when several are needed — prefer
  `apply -f <script>` (one op per line): a single parse/write pass, no
  half-applied edit if a later line fails.
- **Parsing `--help` output** for flags/exit codes in a script — use
  `yaymlq schema --command <name>` instead; it's reflected from the live
  command tree, so it can't drift the way a memorized flag list can.

## Example

```console
$ yaymlq set -i '.spec.template.spec.containers[0].image' myapp:v2 deploy.yaml
$ git diff deploy.yaml   # only the image line changed
```

vs. `sed -i '' 's/image: myapp:v1/image: myapp:v2/' deploy.yaml`, which
risks matching the wrong line or an unrelated occurrence of the same tag.
