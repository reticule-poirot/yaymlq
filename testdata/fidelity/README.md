# Source-fidelity corpus

Every file here is run through a **no-op edit** — `rename .nop nop`, renaming a
key to itself — by `TestFidelityCorpus` in `cmd/fidelity_test.go`. Spliced
(`cmd/splice.go`), it must reproduce every file **byte for byte**.

`TestFidelityCorpusForcedRerender` then re-renders (`cmd/rerender.go`) each
collection and document of every file in turn and checks what a re-render
promises: the same values, every byte outside the re-rendered node unchanged,
no comment lost or duplicated, and a fixed point on a second run.

Each fixture carries a top-level `nop` key for the edit to land on, and
exercises one construct so a failure names the cause. Most are constructs
the YAML encoder cannot write back as the source spelled them, which is why
they are worth holding the splice to.

These are hand-written, not collected: a fixture has to be readable next to the
code it guards, and a corpus anyone can reproduce from the repo is worth more
than a bigger one nobody else can.
