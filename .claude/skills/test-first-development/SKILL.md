---
name: test-first-development
description: Use before writing implementation code for any fix or feature in this repo — write the failing test first, not after.
---

# Test-First Development for yaymlq

## Overview

This project requires test-first TDD for all code changes: write the
failing test before the implementation, not after.

**REQUIRED SUB-SKILL:** Use `superpowers:test-driven-development` for the
full red-green-refactor process.

## What this looks like in this repo

- New CLI behavior: write the `cmd/*_test.go` case (using the `execute`
  helper from `cmd/root_test.go`) that exercises the desired behavior,
  run it, confirm it fails, then implement.
- New query/edit-engine behavior: write the `internal/*_test.go` case
  first, against the package's existing test style.
- Bug fix: write a test that reproduces the bug — it should fail against
  the current code — before touching the fix itself.
- Output change: also plan to regenerate the affected golden file
  (`go test ./cmd -run TestGolden -update`) once the fix is in, per
  CLAUDE.md's existing golden-test convention.

This complements, not replaces, CLAUDE.md's "every change to the query
engine or CLI flags gets a test" rule — the difference is ordering (test
written and run first), not whether a test exists at all.

## Common Mistakes

- Writing the implementation, then adding a test afterward "to cover
  it" — a test that exists is not the same as TDD; the point is watching
  it fail first, against the right cause.
- Skipping the "confirm it fails for the expected reason" step — a test
  that was never run red proves nothing about what it actually covers.
