---
name: maintaining-the-changelog
description: Use when writing or editing an entry in this project's CHANGELOG.md — as part of a fix or feature's own PR, or when cutting the next dated version section.
---

# Maintaining the Changelog

## Overview

This project's `CHANGELOG.md` follows Keep a Changelog + strict SemVer on
the surface, but two things aren't obvious from the file alone: entries
land under `[Unreleased]` first (never a version number you assign
yourself), and entries say what changed, not why.

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

Say only what changed, in as few lines as that takes. Leave out why: the root
cause, the symptom and the reasoning belong in the issue or PR the entry
closes. "Instead of X" is fine when it says what the old behavior was, and a
breaking change says what to do instead.

```
# Explains why — avoid
- `set`/`append`/`delete`/`rename`/`apply` no longer collapse every inline
  comment's gutter spacing to a single space document-wide. `yaml.v3`
  discards the number of spaces before an inline `#` comment at parse time
  and its encoder always re-emits exactly one. Closes #102.

# Actual project style (from this file's own [0.10.2] entry)
- Edits no longer collapse every inline comment's gutter to one space. Closes #102.
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
- Explaining why the change was made.
- `(#N)` instead of `Closes #N.`
- Touching the footer's compare-link block when you're not the one cutting
  a version.
- Cutting a version section in the same PR as a code change.
