package cmd

import (
	"strings"
	"testing"
)

// #256 is two defects that meet in one place: a line comment on a key whose
// value is a block collection carrying an anchor, a tag, or both.
//
// yaml.v3's decoder records that comment against the collection's first child
// (#172), and its encoder, handed one on the key, emits the token alone on the
// next line at column 0. keycomments.go repaired both halves for an anchor
// written on the key's line, and nothing else: a tag falls through the decoder
// repair, and a token the source put on a line of its own is emitted where no
// document can hold it.

// TestEditKeepsAKeyCommentOnATaggedKey is the first defect. The anchor case
// already passes — it is in the table to show the two are the same shape and
// that one of them was handled.
func TestEditKeepsAKeyCommentOnATaggedKey(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"anchor", "k: &p # c\n  v: 1\nafter: 1\n"},
		{"tag", "k: !tag # c\n  v: 1\nafter: 1\n"},
		{"anchor and tag", "k: &p !tag # c\n  v: 1\nafter: 1\n"},
		{"tag, sequence value", "k: !tag # c\n  - one\nafter: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("the key's comment did not stay on the key\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditKeepsADocumentParseableWithATokenOnItsOwnLine is the second, and the
// one that matters more: at column 0 the token is not part of any document, so
// an -i run writes a file that no longer loads. Anchors and tags both.
//
// Asserted by reading the result back through the tool rather than on the
// bytes: the promise is that the output is a document, and separately that it
// says the same thing.
func TestEditKeepsADocumentParseableWithATokenOnItsOwnLine(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"anchor", "k: # c\n  &p\n  v: 1\nafter: 1\n"},
		{"tag", "k: # c\n  !tag\n  v: 1\nafter: 1\n"},
		{"anchor and tag", "k: # c\n  &p !tag\n  v: 1\nafter: 1\n"},
		{"token indented one space", "k: # c\n !tag\n v: 1\nafter: 1\n"},
		{"nested", "outer:\n  k: # c\n    !tag\n    v: 1\nafter: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if _, err := execute(t, got, "-o", "json", "."); err != nil {
				t.Fatalf("the edited document no longer parses: %v\n%q", err, got)
			}
			for _, line := range strings.Split(got, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || strings.HasPrefix(trimmed, "#") {
					continue
				}
				if line == trimmed && (strings.HasPrefix(line, "&") || strings.HasPrefix(line, "!")) {
					t.Errorf("a token was left alone at column 0:\n%q", got)
				}
			}
		})
	}
}

// TestEditKeepsATokenOnItsOwnLineThere: the repair indents the stranded line
// rather than folding it onto the key, so a document that deliberately wrote
// the token on a line of its own keeps that shape. Folding it up would be the
// other valid answer and the wrong one — it rewrites a line the edit never
// touched, which is what this whole layer exists to avoid.
func TestEditKeepsATokenOnItsOwnLineThere(t *testing.T) {
	in := "k: # c\n  !tag\n  v: 1\nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestIndentLikeDeclinesWhenThereIsNothingToMeasure covers the two ways the
// re-indent has no answer. Neither can arise from a document the encoder
// produced — a token is always followed by the collection it introduces — so
// they are checked here rather than through the CLI, and both return the token
// untouched rather than guessing an indentation.
func TestIndentLikeDeclinesWhenThereIsNothingToMeasure(t *testing.T) {
	lines := [][]byte{[]byte("k: # c"), []byte("!tag"), []byte("v: 1")}
	if got := string(indentLike([]byte("!tag"), lines, len(lines))); got != "!tag" {
		t.Errorf("past the last line: got %q, want %q", got, "!tag")
	}
	if got := string(indentLike([]byte("!tag"), lines, 2)); got != "!tag" {
		t.Errorf("next line unindented: got %q, want %q", got, "!tag")
	}
	if got := string(indentLike([]byte("!tag"), [][]byte{nil, []byte("    v: 1")}, 1)); got != "    !tag" {
		t.Errorf("next line indented: got %q, want %q", got, "    !tag")
	}
}

// TestEditStillLeavesAnUnrelatedLoneTokenAlone is the guard the repair must
// not break: a line holding only an anchor or tag, with no commented key above
// it, is nothing this knows how to move.
func TestEditStillLeavesAnUnrelatedLoneTokenAlone(t *testing.T) {
	in := "k:\n  !tag\n  v: 1\nafter: 1\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := execute(t, got, "-o", "json", "."); err != nil {
		t.Fatalf("the edited document no longer parses: %v\n%q", err, got)
	}
	if !strings.Contains(got, "!tag") {
		t.Errorf("the tag was dropped: %q", got)
	}
}
