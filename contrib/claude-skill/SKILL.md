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
change is a value at a known path.

## When to Use

- Reading one value from YAML by path (`.services.web.image`,
  `.spec.replicas`) instead of grepping or writing a parser.
- Changing/adding/deleting/renaming while the file must stay diffable
  (comments, key order, indentation preserved).
- Several related edits, applied atomically in one parse/write pass.
- Checking a YAML file is well-formed, or that a path resolves, before
  trusting it (e.g. a pre-commit or CI step).
- Scripting against it reliably: distinct exit codes per failure class,
  `-o json` structured errors, `yaymlq schema` for the full surface as
  JSON — don't parse `--help` text.

**Skip it for:** a trivial one-off tweak not worth checking `yaymlq` is
installed; a transform that isn't get/set/append/delete/rename at a path
(e.g. reordering list elements by a computed key) — use a real YAML
library instead.

## Quick Reference

| Task | Command |
|---|---|
| Read a value | `yaymlq '.services.web.image' compose.yml` |
| Read, JSON out | `yaymlq -o json '.spec.replicas' deploy.yaml` |
| Set a value in place | `yaymlq set -i '.spec.replicas' 5 deploy.yaml` |
| Preview a change first | `yaymlq set --diff '.spec.replicas' 5 deploy.yaml` |
| Append to a list | `yaymlq append -i '.services.web.ports' '"8080:8080"' compose.yml` |
| Delete a key/element | `yaymlq delete -i '.services.web.environment.DEBUG' compose.yml` |
| Rename a key | `yaymlq rename -i '.services.web' webapp compose.yml` |
| Batch edits, one write | `yaymlq apply -f edits.txt -i compose.yml` |
| List keys / count / type | `yaymlq keys .services compose.yml` |
| Validate + require a path | `yaymlq validate --require .image.tag deploy.yaml` |
| Discover the CLI's own surface | `yaymlq schema \| jq '.commands[].name'` |

Full flag/example reference: `yaymlq schema` (JSON, always current — don't
memorize flags here).

## Common Mistakes

- **Editing without `-i`/`--in-place`** — output goes to stdout, the file
  is untouched. Preview with `--diff`/`--dry-run` first on a file you
  care about.
- **A `<value>` starting with `#`** reads as a YAML comment (empty value)
  unless quoted or passed with `-s`/`--string`.
- **One `set` call per edit** when several are needed — prefer
  `apply -f <script>` (one op per line): a single parse/write pass, no
  half-applied edit if a later line fails.
- **Parsing `--help` output** for flags/exit codes in a script — use
  `yaymlq schema` instead; it's reflected from the live command tree, so
  it can't drift the way a memorized flag list can.

## Example

```console
$ yaymlq set -i '.spec.template.spec.containers[0].image' myapp:v2 deploy.yaml
$ git diff deploy.yaml   # only the image line changed
```

vs. `sed -i '' 's/image: myapp:v1/image: myapp:v2/' deploy.yaml`, which
risks matching the wrong line or an unrelated occurrence of the same tag.
