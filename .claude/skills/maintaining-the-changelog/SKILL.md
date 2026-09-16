---
name: maintaining-the-changelog
description: Use when writing or editing an entry in this project's CHANGELOG.md — after a fix or feature merges to main, or when cutting the next dated version section.
---

# Maintaining the Changelog

## Overview

This project's `CHANGELOG.md` follows Keep a Changelog + strict SemVer on
the surface, but two things aren't obvious from the file alone: entries
land under `[Unreleased]` first (never a version number you assign
yourself), and entries are short explanatory paragraphs, not one-line
commit-message echoes.

## Two-step lifecycle

1. **Adding an entry** (this is almost always your task): add it under the
   existing `## [Unreleased]` heading, under the right `### Category`.
   Never invent a version number or date — that's a separate decision made
   later, by someone deciding what ships together.
2. **Cutting a release** (a distinct, separate task): rename `[Unreleased]`
   to `## [X.Y.Z] - YYYY-MM-DD`, add a fresh empty `## [Unreleased]` above
   it, and update the footer's compare-link block (`[Unreleased]: .../compare/vX.Y.Z...HEAD`,
   plus a new `[X.Y.Z]: .../compare/vPREV...vX.Y.Z` line). Don't do this
   unless that's specifically what you were asked to do.

## Entry depth

Write a short paragraph: what changed, *why* (root cause or the symptom a
user hit), and any consequence worth flagging (compatibility, performance,
security). Not a terse commit-message bullet.

```
# Too terse — avoid
- Inline comment gutters could collapse to one space. Closes #102.

# Actual project style (from this file's own [0.10.2] entry)
- `set`/`append`/`delete`/`rename`/`apply` no longer collapse every inline
  comment's gutter spacing to a single space document-wide. `yaml.v3`
  discards the number of spaces before an inline `#` comment at parse time
  and its encoder always re-emits exactly one — previously visible on
  every hand-aligned comment in the file, not just the one on the line
  being edited. The original width is now recovered from the raw source
  before mutation and restored in the output, matched by comment text.
  Standalone full-line comments were never affected. Closes #102.
```

## Category headings

Standard Keep a Changelog categories only: `### Added`, `### Changed`,
`### Fixed`, `### Removed`, `### Deprecated`, `### Security`. Include only
the headings that have an entry under them.

## Issue references

If the change closes a tracked issue, end the entry with its own sentence:
`Closes #N.` — not a parenthetical `(#N)`. Most entries reference no issue
at all; don't invent one just to have a reference.

## Common Mistakes

- Assigning a version number/date when adding an entry — that's a separate
  step, not yours unless asked.
- A one-line bullet with no explanation of cause or impact.
- `(#N)` instead of `Closes #N.`
- Touching the footer's compare-link block when you're not the one cutting
  a version.
