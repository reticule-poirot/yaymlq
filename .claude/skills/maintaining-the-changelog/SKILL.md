---
name: maintaining-the-changelog
description: Use when writing or editing an entry in this project's CHANGELOG.md — as part of a fix or feature's own PR, or when cutting the next dated version section.
---

# Maintaining the Changelog

## Overview

This project's `CHANGELOG.md` follows Keep a Changelog + strict SemVer on
the surface, but two things aren't obvious from the file alone: entries
land under `[Unreleased]` first (never a version number you assign
yourself), and entries stay short: what changed for the user, not how it was
found.

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

Write one to three wrapped lines, four for a breaking change: what changed from
the user's side, plus the symptom or consequence if it isn't obvious. The root
cause and the investigation belong in the issue or PR the entry closes.

```
# Too long — avoid
- Edits no longer collapse every inline comment's gutter to one space.
  `yaml.v3` discards the number of spaces before an inline `#` at parse time
  and its encoder always re-emits exactly one, so the original width is now
  recovered from the raw source before mutation and matched by comment text,
  which was previously visible on every hand-aligned comment in the file.
  Standalone full-line comments were never affected. Closes #102.

# Actual project style (this file's [0.10.2] entry)
- Edits no longer collapse every inline comment's gutter to one space. The width
  is recovered from the source. Closes #102.
```

## A release cut is its own PR

A version section gathers everything in `[Unreleased]`, however many unrelated
changes that is: 0.24.0 has 24 entries. Each entry lands in its own change's
PR, and the cut lands in a PR that touches only `CHANGELOG.md`, as #359 did for
0.24.0. Mixing a cut with a code change makes the PR's diff describe two things
and complicates review/revert.

## Category headings

Standard Keep a Changelog categories only: `### Added`, `### Changed`,
`### Fixed`, `### Removed`, `### Deprecated`, `### Security`. Include only
the headings that have an entry under them.

## Issue references

If the change closes a tracked issue, end the entry with its own sentence:
`Closes #N.` — not a parenthetical `(#N)`. Every PR has a tracking issue, so
recent entries nearly all carry one; an entry that really has none goes without
rather than inventing one.

## Common Mistakes

- Assigning a version number/date when adding an entry — that's a separate
  step, not yours unless asked.
- A one-line bullet with no explanation of cause or impact.
- `(#N)` instead of `Closes #N.`
- Touching the footer's compare-link block when you're not the one cutting
  a version.
- Cutting a version section in the same PR as a code change.
