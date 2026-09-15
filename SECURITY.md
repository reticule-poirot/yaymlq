# Security Policy

## Supported versions

The latest tagged release receives security fixes. This is a young project;
until `1.0.0` only the newest `0.x` line is supported.

## Reporting a vulnerability

Please report suspected vulnerabilities privately via GitHub's
["Report a vulnerability"](https://github.com/reticule-poirot/yaymlq/security/advisories/new)
form rather than a public issue.

Expect an acknowledgement within a few days. Once a fix is available it will be
released and the advisory published with credit unless you prefer otherwise.

## Threat model

`yaymlq` is a local command-line tool. It reads YAML from a file or stdin that
the invoking user chose, and (with `set` / `append` / `delete` / `rename` /
`apply`) writes back to a path the user named. It makes no network
connections and runs no subprocesses.

Hardening already in place:

- Input is buffered through a `--max-bytes` cap (64 MiB default) before
  parsing. This bounds the raw input read, not peak memory: decoding, and
  especially `--diff`/`--dry-run` (which briefly holds both the before and
  after document), can use tens to a few hundred times the input size in
  RSS. See README's "Handling untrusted input" for the measured range.
- Without `--all-docs`, a multi-document stream is parsed only as far as the
  requested document.
- YAML alias-expansion bombs are rejected by `gopkg.in/yaml.v3` (≥ v3.0.1);
  a regression test guards this.
- `set` / `append` / `delete` / `rename` / `apply` with `--in-place` write
  atomically (temp file + `rename`, both fsync'd — the temp file's content
  before the rename, its directory afterward), never leaving a truncated
  file or silently losing a completed edit to power loss, and replace a
  symlinked path rather than writing through it — the edit is read from
  the link's target but the result lands at the link's own path, so the
  link is gone afterward and the file it pointed at is left untouched
  (any hardlinks to the target are broken the same way, expected for an
  atomic-replace write). A one-line note is printed to stderr for the
  symlink case. Only POSIX permission bits carry over to the replacement
  file; ownership, ACLs, and extended attributes (an SELinux label, a
  Windows DACL with inheritance disabled) do not, since the file is
  replaced rather than modified in place. There's no signal handler:
  interrupting an in-place edit can leave a `.<name>.yaymlq-<random>`
  sibling file holding a copy of the document as of that moment — never
  looser-permissioned than the target, and safe to delete.

Out of scope: protecting against a YAML file the user has chosen to process but
does not trust to the point of not wanting its size or structure to affect
`yaymlq`'s resource use beyond the `--max-bytes` bound.
