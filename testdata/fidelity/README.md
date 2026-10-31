# Source-fidelity corpus

Every file here is run through a **no-op edit** — `rename .nop nop`, renaming a
key to itself — by `TestFidelityCorpus` in `cmd/fidelity_test.go`. The whole
read → mutate → encode pipeline runs, every fixup with it, and nothing is asked
to change, so any difference in the output is source fidelity lost.

- `<name>.yaml` alone: the no-op edit must reproduce it **byte for byte**.
- `<name>.yaml` with a `<name>.want` sibling: a construct yaml.v3 still
  re-renders, with the current output recorded. Closing the gap means deleting
  the `.want` file — the test says so when one becomes unnecessary.

Each fixture carries a top-level `nop` key for the edit to land on, and
exercises one construct so a failure names the cause.

These are hand-written, not collected: a fixture has to be readable next to the
fixup it guards, and a corpus anyone can reproduce from the repo is worth more
than a bigger one nobody else can.
