package cmd

import (
	"strings"
	"testing"
)

// TestEditKeepsACommentOnAnAnchoredMappingKey guards #172: when a key is
// anchored *and* its value is a block collection, yaml.v3's decoder records
// the key's line comment against the collection's first child instead, two
// lines from where it was written. The encoder then puts it where it was told,
// so the comment ends up documenting something else and loses its alignment —
// on any edit, anywhere in the document.
func TestEditKeepsACommentOnAnAnchoredMappingKey(t *testing.T) {
	in := "parent: &p   # note about parent\n" +
		"  x: 1\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p   # note about parent\n" +
		"  x: 1\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("comment on an anchored mapping key moved:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditKeepsACommentOnAnAnchoredSequenceKey is the same bug in its
// sequence flavor, which the issue didn't cover: the comment lands on the
// first *element* rather than the first child key.
func TestEditKeepsACommentOnAnAnchoredSequenceKey(t *testing.T) {
	in := "parent: &p   # note about seq\n" +
		"  - 1\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p   # note about seq\n" +
		"  - 1\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("comment on an anchored sequence key moved:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditKeepsBothAParentAndAChildComment is the worst form of #172 and the
// reason it is more than cosmetic. With a comment on the anchored key *and*
// one on the first child, the decoder leaves the child's own comment on that
// pair's value node and puts the parent's on the pair's key node — and the
// encoder, handed a line comment on both halves of one pair, emits the
// value's and defers the key's to the next line. The parent's comment lands
// on an unrelated sibling two lines away.
func TestEditKeepsBothAParentAndAChildComment(t *testing.T) {
	in := "parent: &p   # parent note\n" +
		"  x: 1   # child note\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p   # parent note\n" +
		"  x: 1   # child note\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("parent and child comments not both kept in place:\nwant:\n%q\ngot:\n%q", want, got)
	}
	if strings.Contains(got, "z: 10 # parent note") {
		t.Error("the parent's comment migrated onto an unrelated sibling")
	}
}

// TestEditKeepsASequenceParentAndElementComment is the most tangled shape:
// with a comment on the anchored key *and* on the first element of a block
// sequence, the decoder concatenates both into that element's single
// LineComment ("# parent note\n# elem note"), which the encoder emits as a
// line comment plus a dangling full-line one. Only the leading segment
// belongs to the key, so the fix has to split rather than move wholesale.
func TestEditKeepsASequenceParentAndElementComment(t *testing.T) {
	in := "parent: &p   # parent note\n" +
		"  - 1   # elem note\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p   # parent note\n" +
		"  - 1   # elem note\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("concatenated sequence comments not split back:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesASequenceElementsOwnCommentAlone is the sequence counterpart
// of the mapping guard below. A sequence has no key/value split, so an
// element's own comment sits on exactly the node a misattributed one would —
// only the source line tells them apart, and nothing should move here.
func TestEditLeavesASequenceElementsOwnCommentAlone(t *testing.T) {
	in := "parent: &p\n" +
		"  - 1   # elem note\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p\n" +
		"  - 1   # elem note\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("a sequence element's own comment was disturbed:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesAChildsOwnCommentAlone is the case the fix must not break: an
// anchored key with no comment of its own, whose first child does have one.
// Nothing is misattributed here, so nothing should move. A child's own comment
// is recorded against its pair's value node, which is what distinguishes it
// from a misattributed one landing on the key node.
func TestEditLeavesAChildsOwnCommentAlone(t *testing.T) {
	in := "parent: &p\n" +
		"  x: 1   # child note\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p\n" +
		"  x: 1   # child note\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("a child's own comment was disturbed:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesASecondChildsCommentAlone pins the two apart in one document:
// the first child carries the misattributed parent comment while a later
// child carries its own.
func TestEditLeavesASecondChildsCommentAlone(t *testing.T) {
	in := "parent: &p   # parent note\n" +
		"  x: 1\n" +
		"  y: 2   # y note\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent: &p   # parent note\n" +
		"  x: 1\n" +
		"  y: 2   # y note\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("comments not each kept in place:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesAPlainMappingKeyCommentAlone is the unanchored case, already
// correct today: the decoder records the comment on the key node where it
// belongs, so the fix must be a no-op here.
func TestEditLeavesAPlainMappingKeyCommentAlone(t *testing.T) {
	in := "parent:   # note plain\n" +
		"  x: 1\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "parent:   # note plain\n" +
		"  x: 1\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("plain mapping key comment disturbed:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesAFlowCollectionCommentAlone covers the other already-correct
// case: a flow collection's content is on the key's own line, so the decoder
// keeps the comment on the anchored node itself. Only a block collection,
// whose content starts on the next line, is misattributed.
func TestEditLeavesAFlowCollectionCommentAlone(t *testing.T) {
	in := "parent: &p {x: 1}   # note flow\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "{x: 1}   # note flow") {
		t.Fatalf("flow collection comment disturbed, got:\n%q", got)
	}
}

// TestEditKeepsACommentOnAnAnchoredKeyBelowAHeadComment checks the line
// bookkeeping holds when the document doesn't start at line 1, since the fix
// matches a comment against the source line it was written on.
func TestEditKeepsACommentOnAnAnchoredKeyBelowAHeadComment(t *testing.T) {
	in := "# head\n" +
		"parent: &p   # parent note\n" +
		"  x: 1\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "# head\n" +
		"parent: &p   # parent note\n" +
		"  x: 1\n" +
		"z: 10\n"
	if got != want {
		t.Fatalf("comment moved below a head comment:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditKeepsACommentOnAnAnchoredKeyInASecondDocument checks the fix runs
// per decoded document, the same as normalizeMergeTags — a later document's
// node lines are numbered from the start of the stream, so a per-document
// source slice would look at the wrong lines.
func TestEditKeepsACommentOnAnAnchoredKeyInASecondDocument(t *testing.T) {
	in := "first: 1\n" +
		"---\n" +
		"parent: &p   # parent note\n" +
		"  x: 1\n" +
		"z: 9\n"

	got, err := execute(t, in, "set", "--doc", "1", ".z", "10")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "parent: &p   # parent note") {
		t.Fatalf("comment moved in the second document, got:\n%q", got)
	}
	if strings.Contains(got, "x: 1 # parent note") {
		t.Fatalf("comment landed on the child in the second document, got:\n%q", got)
	}
}
