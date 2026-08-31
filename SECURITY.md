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
the invoking user chose, and (with `set` / `delete`) writes back to a path the
user named. It makes no network connections and runs no subprocesses.

Hardening already in place:

- Input is buffered through a `--max-bytes` cap (64 MiB default) before parsing,
  bounding memory use on oversized or hostile input.
- Without `--all-docs`, a multi-document stream is parsed only as far as the
  requested document.
- YAML alias-expansion bombs are rejected by `gopkg.in/yaml.v3` (≥ v3.0.1);
  a regression test guards this.
- `set --in-place` / `delete --in-place` write atomically (temp file +
  `rename`), never leaving a truncated file, and replace a symlinked path
  rather than writing through it.

Out of scope: protecting against a YAML file the user has chosen to process but
does not trust to the point of not wanting its size or structure to affect
`yaymlq`'s resource use beyond the `--max-bytes` bound.
