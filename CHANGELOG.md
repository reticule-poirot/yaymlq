# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- Initial development. Version numbers below track notable changes; releases
  are not yet published.

### Added

- `yaymlq delete <path> [file]` (aliases `del`, `rm`) — remove a mapping key or
  list element, preserving comments and key order on everything that remains.
  Shares `set`'s flags (`-i/--in-place`, `--doc`, `--max-bytes`) and atomic
  write path. Wildcards are rejected; deleting a path that does not exist is an
  error.

## [0.1.0] - 2026-08-30

### Added

- `yaymlq <path> [file]` — read a value from a YAML document (or stdin) by a
  `jq`-ish path expression, output as `yaml` (default), `json`, or `raw`.
- Path syntax: map keys, slice indices (`a[0]`, `a.0`, `a[-1]`), wildcards
  (`a.*`, `a[]`, `a[*]`), and quoted segments (`"a.b".c`).
- Wildcards fan out to multiple results; map values are emitted sorted by key.
- `--doc N` / `--all-docs` for multi-document streams.
- `--default VALUE` and `-e/--exit-status` for script-friendly missing-path
  handling.
- `--max-bytes` input cap (default 64 MiB) guarding against oversized input;
  early-stop stream decoding; YAML alias-bomb rejection.
- `yaymlq set <path> <value> [file]` — set a value, preserving comments, key
  order, and formatting. `-i/--in-place` writes atomically (temp file + rename,
  symlink-safe, mode-preserving); `-s/--string` forces a string value.
