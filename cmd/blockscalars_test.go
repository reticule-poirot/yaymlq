package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blockForms are the six headers a block scalar can carry. Each fixture below
// uses the same body and the same single trailing blank line, so the six are
// compared on equal terms: under keep chomping (+) that blank line is part of
// the value, under the other four it is spacing before the next key.
var blockForms = []string{"|", "|-", "|+", ">", ">-", ">+"}

func blockDoc(header string) string {
	return "f: " + header + "\n  one\n  two\n\nafter: 1\n"
}

// TestEditKeepsAnUntouchedBlockScalarByteIdentical is #250 stated as the
// promise the five existing fixups already make: an edit changes the lines it
// was asked to change and nothing else. A block scalar is checked whole —
// header, body line breaks and trailing blank — because every part of it was
// being rewritten, and because its text *is* its value: a `|+` block gaining a
// line break is a changed value, not a changed layout.
func TestEditKeepsAnUntouchedBlockScalarByteIdentical(t *testing.T) {
	for _, header := range blockForms {
		t.Run(header, func(t *testing.T) {
			in := blockDoc(header)
			want := strings.Replace(in, "after: 1", "after: 2", 1)
			got, err := execute(t, in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("an edit to .after rewrote the block scalar\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditKeepsAKeepChompedValueExact is the corruption half, asserted
// through the tool's own reader rather than on the text: a caller who never
// looks at the file still gets a different value out of it after an edit to an
// unrelated key. Two trailing blank lines rather than one, since under keep
// chomping a run of them is content and must not collapse the way spacing
// does.
func TestEditKeepsAKeepChompedValueExact(t *testing.T) {
	for _, header := range []string{"|+", ">+"} {
		t.Run(header, func(t *testing.T) {
			in := "f: " + header + "\n  one\n  two\n\n\nafter: 1\n"
			before, err := execute(t, in, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			edited, err := execute(t, in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			after, err := execute(t, edited, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read after: %v", err)
			}
			if before != after {
				t.Errorf("editing .after changed .f's value\n before %s  after %s", before, after)
			}
			if want := strings.Replace(in, "after: 1", "after: 2", 1); edited != want {
				t.Errorf("a run of content blank lines was not kept\n--- want\n%q\n--- got\n%q", want, edited)
			}
		})
	}
}

// TestEditStillCollapsesBlankLinesAfterAClippedBlock keeps the two halves
// apart. Blank lines following a `|`, `|-`, `>` or `>-` block are spacing, not
// content, so blanklines.go's documented rule still applies to them — a run
// collapses to one. Only the keep-chomped forms claim them.
func TestEditStillCollapsesBlankLinesAfterAClippedBlock(t *testing.T) {
	in := "f: |\n  one\n\n\n\nafter: 1\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "f: |\n  one\n\nafter: 2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestEditInPlaceIsIdempotentWithABlockScalar: a tool run repeatedly over a
// repo is what -i is for, so a fixup that adds a line per run grows the file
// without bound. The second run must be a no-op on the bytes.
func TestEditInPlaceIsIdempotentWithABlockScalar(t *testing.T) {
	for _, header := range blockForms {
		t.Run(header, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "d.yml")
			if err := os.WriteFile(name, []byte(blockDoc(header)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := execute(t, "", "set", "-i", ".after", "9", name); err != nil {
				t.Fatalf("first edit: %v", err)
			}
			first, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := execute(t, "", "set", "-i", ".after", "9", name); err != nil {
				t.Fatalf("second edit: %v", err)
			}
			second, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if string(first) != string(second) {
				t.Errorf("the same edit twice is not a no-op\n--- first\n%q\n--- second\n%q", first, second)
			}
		})
	}
}

// TestEditKeepsABlockScalarHeaderWhole covers the rest of the header grammar
// and the places a block scalar sits that aren't a plain mapping value. The
// indentation indicator is the one part yaml.v3 recomputes rather than drops,
// so `|2` came back as `|4` with the body reflowed to match — the same value,
// a diff over lines nobody edited.
func TestEditKeepsABlockScalarHeaderWhole(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		in   string
	}{
		{"indent indicator", nil, "f: |2\n    indented\n    body\nafter: 1\n"},
		{"indicator after chomping", nil, "f: |-2\n    indented\n    body\nafter: 1\n"},
		{"chomping after indicator", nil, "f: |2-\n    indented\n    body\nafter: 1\n"},
		{"indented continuation", nil, "f: |2\n    one\n  two\nafter: 1\n"},
		{"folded line breaks", nil, "f: >\n  one\n  two\n  three\nafter: 1\n"},
		{"interior blank line", nil, "f: |\n  one\n\n  two\nafter: 1\n"},
		// A value opening with a line break is the one yaml.v3's encoder
		// cannot render at all: it emitted the block without the leading
		// blank, dropping a character of the value. Reachable without any
		// exotic chomping — a script in a ConfigMap that starts with a blank
		// line is enough.
		{"leading blank line", nil, "f: |\n\n  one\nafter: 1\n"},
		{"leading blank line, folded", nil, "f: >\n\n  one\nafter: 1\n"},
		// A tab is never indentation in YAML, so a line holding one holds
		// content — and in a block scalar that tab is a character of the
		// value, not an empty line the block happens to span.
		{"tab as content", nil, "f: >\n  one\n  \t\n  two\nafter: 1\n"},
		// The blank line below a keep-chomped block is its value, so the
		// restore has to own that boundary whatever the blank-line marker did
		// — and the marker's own arithmetic lands a line off when a head
		// comment sits between the block and the next key.
		{"keep chomped above a comment", nil, "f: |+\n\n# note\nafter: 1\n"},
		{"anchored", nil, "f: &a |\n  one\nafter: 1\n"},
		{"tagged", nil, "f: !!str |\n  one\nafter: 1\n"},
		// Binary data is normally written as a block scalar, and the tag has
		// to survive alongside the header: the mask clears the block style and
		// nothing else, so the encoder still emits the tag it was given.
		{"binary", nil, "f: !!binary |\n  aGVsbG8K\nafter: 1\n"},
		{"custom tag", nil, "f: !custom |\n  one\nafter: 1\n"},
		{"header comment", nil, "f: | # why\n  one\nafter: 1\n"},
		{"in a sequence", nil, "f:\n  - |\n    one\n  - plain\nafter: 1\n"},
		{"nested", nil, "a:\n  b: |\n    one\n  c: 1\nafter: 1\n"},
		{"last node in the document", nil, "after: 1\nf: |+\n  one\n\n"},
		{"4-space source", nil, "a:\n    b: |\n        one\nafter: 1\n"},
		{"second document", []string{"--doc", "1"}, "a: 1\n---\nf: |+\n  one\n\n\nafter: 1\n"},
		{"untouched document", []string{"--doc", "0"}, "after: 1\n---\nf: >\n  one\n  two\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			args := append(append([]string{"set"}, tc.args...), ".after", "2")
			got, err := execute(t, tc.in, args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("block scalar not restored\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditFoldsALoneBlockHeaderOntoItsKey is the one source spelling this
// doesn't reproduce, recorded rather than left to be discovered: a header on
// its own line comes back on the key's line. The restore replaces the scalar
// the encoder emitted, and the encoder only ever emits it inline. The value is
// intact either way, which is the half that matters.
func TestEditFoldsALoneBlockHeaderOntoItsKey(t *testing.T) {
	in := "f:\n  |\n  one\n  two\nafter: 1\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "f: |\n  one\n  two\nafter: 2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestEditingABlockScalarItselfRewritesIt settles the question #250 left open.
// The fixups restore what the edit didn't touch; the value of the key the
// caller named is the caller's, so it is re-emitted for what it now is rather
// than forced back into a header describing text that is gone.
func TestEditingABlockScalarItselfRewritesIt(t *testing.T) {
	in := "f: |\n  one\n  two\nafter: 1\n"
	got, err := execute(t, in, "set", ".f", "replaced")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "f: replaced\nafter: 1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDeletingABlockScalarTakesItsBodyWithIt: the restore is keyed to the node
// in the tree, so one the edit removed leaves nothing behind. A splice driven
// by source order instead would put the body back under whatever key landed in
// its place.
func TestDeletingABlockScalarTakesItsBodyWithIt(t *testing.T) {
	in := "f: |\n  one\n  two\nafter: 1\n"
	got, err := execute(t, in, "delete", ".f")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "after: 1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenamingABlockScalarsKeyKeepsItsBody: rename moves the key, not the
// value node, so the block has to follow it.
func TestRenamingABlockScalarsKeyKeepsItsBody(t *testing.T) {
	in := "f: |2\n    one\nafter: 1\n"
	got, err := execute(t, in, "rename", ".f", "g")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "g: |2\n    one\nafter: 1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestEditLeavesAnEmptyBlockToTheEncoder records a block with no body as
// unchanged behavior rather than as something this fixup addresses: there is
// no body to record, so nothing is restored and yaml.v3 writes the empty
// string the way it always did. The value is the same either way.
func TestEditLeavesAnEmptyBlockToTheEncoder(t *testing.T) {
	got, err := execute(t, "f: |\nafter: 1\n", "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "f: \"\"\nafter: 2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestBlockReproducesRefusesARecordThatDoesNotParseBack is the safety gate
// this file rests on, tested where it lives. Everything else about the restore
// is a judgement about raw text — where a block ends, what its indentation is
// measured against — and this is what makes a wrong judgement harmless: a
// record that does not re-parse to the value the decoder produced is never
// applied, so the scalar falls through to the encoder exactly as it did before
// this fixup existed.
func TestBlockReproducesRefusesARecordThatDoesNotParseBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		body   []string
		parent int
		value  string
		want   bool
	}{
		{"exact", "|", []string{"  one", "  two"}, 0, "one\ntwo\n", true},
		{"keeps the trailing breaks", "|+", []string{"  one", ""}, 0, "one\n\n", true},
		{"nested, dedented by the parent", "|", []string{"    one"}, 2, "one\n", true},
		{"truncated body", "|", []string{"  one"}, 0, "one\ntwo\n", false},
		{"wrong chomping", "|-", []string{"  one"}, 0, "one\n", false},
		{"body not indented past the parent", "|", []string{"  one"}, 2, "one\n", false},
		{"body that cannot be a block", "|", []string{"one"}, 0, "one\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockReproduces(tc.header, tc.body, tc.parent, tc.value); got != tc.want {
				t.Errorf("blockReproduces(%q, %q, %d, %q) = %v, want %v",
					tc.header, tc.body, tc.parent, tc.value, got, tc.want)
			}
		})
	}
}

// TestEditShiftsABlockScalarWithItsKey covers the one case the body has to
// move for: --indent re-indents the document, and what carries a block
// scalar's value is its body's indentation *relative to its key*, not the
// absolute column the source wrote it at.
func TestEditShiftsABlockScalarWithItsKey(t *testing.T) {
	for _, tc := range []struct {
		name, indent, in, want string
	}{
		{
			"wider", "4",
			"a:\n  b: |\n    one\n    two\n  c: 1\n",
			"a:\n    b: |\n      one\n      two\n    c: 2\n",
		},
		{
			"narrower", "2",
			"a:\n    b: |\n        one\n        two\n    c: 1\n",
			"a:\n  b: |\n      one\n      two\n  c: 2\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", "--indent", tc.indent, ".a.c", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			// The body moved, so the value is what has to be checked, not the
			// bytes: a shift that overshoots changes where the content starts.
			before, err := execute(t, tc.in, "-o", "json", ".a.b")
			if err != nil {
				t.Fatal(err)
			}
			after, err := execute(t, got, "-o", "json", ".a.b")
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Errorf("re-indenting changed the block's value\n before %s  after %s", before, after)
			}
		})
	}
}

// TestEditMovesABlockScalarWithAReindentedSequence is the case that made
// `bound` the dash's column rather than the item's. yaml.v3 re-indents a
// sequence written flush with its key, and it normalises a wide dash gutter —
// so the body's shift is the dash's own move, not the item content's. Measured
// from the item it came out a gutter-width short, which left the body no more
// indented than its sequence: valid-looking text that no longer parses.
func TestEditMovesABlockScalarWithAReindentedSequence(t *testing.T) {
	for _, in := range []string{
		"items:\n-   |\n    one\n    two\nafter: 1\n",
		"items:\n-  >\n   one\nafter: 1\n",
		"items:\n- |\n  one\nafter: 1\n",
	} {
		t.Run(in, func(t *testing.T) {
			before, err := execute(t, in, "-o", "json", ".items[0]")
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			got, err := execute(t, in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			after, err := execute(t, got, "-o", "json", ".items[0]")
			if err != nil {
				t.Fatalf("the edited document no longer parses: %v\n%q", err, got)
			}
			if before != after {
				t.Errorf("re-indenting the sequence changed the block\n--- out\n%q\n before %s  after %s", got, before, after)
			}
		})
	}
}

// TestEditKeepsABlockScalarBesideItsOwnMarkerText guards the one way the
// stand-in marker could be mistaken for the document's own text: a document
// that already contains it. The prefix is extended until it appears nowhere in
// source, so the document's copy stays a value and the block still comes back.
func TestEditKeepsABlockScalarBesideItsOwnMarkerText(t *testing.T) {
	in := "note: " + blockMarkerCore + "0\nf: |\n  one\nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestBlockHeaderRejectsWhatIsNotAHeader pins the fail-safe side of the
// parser. Every rejection leaves the scalar to the encoder, which is the
// behavior that shipped before this fixup existed — so getting one wrong costs
// fidelity, while accepting a run that isn't a header would splice a body into
// a line that cannot hold one.
func TestBlockHeaderRejectsWhatIsNotAHeader(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		col        int
	}{
		{"no indicator", "f: plain", 4},
		{"column past the line", "f: |", 99},
		{"column before the line", "f: |", 0},
		{"two indentation indicators", "f: |23", 4},
		{"indicator inside a comment", "f: 1 # a|b", 4},
		{"text after the run", "f: |x", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, ok := blockHeader(tc.line, tc.col); ok {
				t.Errorf("blockHeader(%q, %d) accepted a line that is not a block header", tc.line, tc.col)
			}
		})
	}
}

// TestBlockHeaderReadsTheWholeRun is the other half: the indicator run is
// taken whole and nothing beside it on the line is.
func TestBlockHeaderReadsTheWholeRun(t *testing.T) {
	for _, tc := range []struct {
		line          string
		col           int
		want          string
		wantIndicator int
	}{
		{"f: |", 4, "|", 0},
		{"f: >-", 4, ">-", 0},
		{"f: |2+", 4, "|2+", 2},
		{"f: |+2", 4, "|+2", 2},
		{"f: &a | # note", 4, "|", 0},
		{"f: !!str >", 4, ">", 0},
		{"f: |\t", 4, "|", 0},
	} {
		t.Run(tc.line, func(t *testing.T) {
			got, _, indicator, ok := blockHeader(tc.line, tc.col)
			if !ok {
				t.Fatalf("blockHeader(%q, %d) rejected a real header", tc.line, tc.col)
			}
			if got != tc.want || indicator != tc.wantIndicator {
				t.Errorf("got (%q, %d), want (%q, %d)", got, indicator, tc.want, tc.wantIndicator)
			}
		})
	}
}

// TestEditKeepsABlockScalarUnderCRLF checks the restore lands before the line
// endings are reapplied, rather than splicing LF bodies into a CRLF document.
func TestEditKeepsABlockScalarUnderCRLF(t *testing.T) {
	in := "f: |\r\n  one\r\n  two\r\nafter: 1\r\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestEditKeepsASignificantWhitespaceLineInABlockScalar is why the restore
// runs after tidyBlankLines rather than before it: a whitespace-only line in a
// literal block is content, and tidyBlankLines empties whitespace-only lines.
func TestEditKeepsASignificantWhitespaceLineInABlockScalar(t *testing.T) {
	in := "f: |\n  one\n   \n  two\nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
