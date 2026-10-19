package cmd

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// When a key is anchored *and* its value is a block collection, yaml.v3's
// decoder records the key's line comment against the collection's first child
// instead of the key:
//
//	parent: &p   # note        kind=8 value="parent" line=1 lineComment=""
//	  x: 1                     kind=4 anchor="p"     line=1 lineComment=""
//	                             kind=8 value="x"    line=2 lineComment="# note"
//
// Drop the anchor and the same comment lands on the key, where it belongs.
// The encoder then emits it where it was told, so every edit moved the
// comment a line down and left it documenting something else (#172).
//
// commentgutters.go can't recover from this: it restores a gutter's *width*
// by matching comment text, which is no help for a comment that needs
// relocating — nothing on the key's line claims it any more.
//
// Three shapes have to be told apart, and only the raw source can do it:
//
//   - a misattributed key comment, which the source shows on the key's line
//   - a child's own comment, which the source shows on the child's line. In a
//     mapping it also sits on the child pair's *value* node rather than its
//     key, but a sequence has no such split, so the source is the only signal
//     that works for both.
//   - both at once, which the decoder concatenates into one LineComment
//     separated by a newline ("# parent note\n# elem note"). Only the leading
//     segment belongs to the key, so this splits rather than moving wholesale.
//
// Anything ambiguous — the same comment text on both lines — is left alone.
// A flow collection (`&p {x: 1}`) is already correct, since its content is on
// the key's own line and the decoder keeps the comment on the anchored node.

// restoreAnchoredKeyComments walks n and moves each misattributed comment
// back onto the key that carries the anchor. lines is the whole input split
// into 1-indexed source lines; node lines are absolute across a document
// stream, so one split covers every document.
func restoreAnchoredKeyComments(n *yaml.Node, lines [][]byte) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			reattachKeyComment(n.Content[i], n.Content[i+1], lines)
		}
	}
	for _, c := range n.Content {
		restoreAnchoredKeyComments(c, lines)
	}
}

// reattachKeyComment moves the leading segment of value's first child's line
// comment onto key, when the source shows it was written on key's line.
func reattachKeyComment(key, value *yaml.Node, lines [][]byte) {
	if value.Anchor == "" || key.LineComment != "" {
		return
	}
	if value.Kind != yaml.MappingNode && value.Kind != yaml.SequenceNode {
		return
	}
	if len(value.Content) == 0 {
		return
	}
	first := value.Content[0]
	if first.LineComment == "" || first.Line == key.Line {
		// Same line means a flow collection, which the decoder gets right.
		return
	}

	lead, rest, _ := strings.Cut(first.LineComment, "\n")
	if !lineHasComment(lines, key.Line, lead) {
		// Not the key's comment — the child wrote it itself.
		return
	}
	if lineHasComment(lines, first.Line, lead) {
		// The same text on both lines: no way to tell which this is, so
		// changing nothing is the one safe answer.
		return
	}
	key.LineComment = lead
	first.LineComment = rest
}

// lineHasComment reports whether the 1-indexed source line contains comment.
func lineHasComment(lines [][]byte, line int, comment string) bool {
	if line < 1 || line > len(lines) || comment == "" {
		return false
	}
	return bytes.Contains(lines[line-1], []byte(comment))
}

// Putting the comment back on the key is only half the job, because the
// encoder cannot render it there: handed a line comment on a key whose value
// is an anchored block collection, it emits the anchor alone on the next line.
// Measured at every nesting depth, the artifact is always the same shape —
// the anchor by itself, at column 0, directly below the key:
//
//	outer:
//	  inner:   # note
//	&p
//	    x: 1
//
// (The other two candidate homes are worse and unrepairable: a comment on the
// anchored value node migrates onto an unrelated sibling, and leaving it where
// the decoder put it is the bug itself.)
//
// So the comment goes back on the key, and the one artifact that creates is
// spliced out here. Same division of labor as blanklines.go and
// commentgutters.go: the node tree carries what it can, and the rest is
// restored in the encoded bytes.
var (
	loneAnchorLine = regexp.MustCompile(`^\s*(&\S+)$`)
	keyThenComment = regexp.MustCompile(`^(\s*.*?:)\s*(#.*)$`)
)

// rejoinAnchorLines splices an anchor the encoder stranded on its own line
// back onto the key above it. src is the pre-edit document: an anchor is only
// rejoined when src shows that anchor and that comment together on one line,
// so a document that genuinely wrote the anchor on a line of its own is left
// as it was rather than quietly reformatted.
func rejoinAnchorLines(out, src []byte) []byte {
	if !bytes.Contains(out, []byte("&")) {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	kept := make([][]byte, 0, len(lines))
	for _, line := range lines {
		anchor := loneAnchorLine.FindSubmatch(line)
		if anchor == nil || len(kept) == 0 {
			kept = append(kept, line)
			continue
		}
		prev := keyThenComment.FindSubmatch(kept[len(kept)-1])
		if prev == nil || !sourceHadAnchoredComment(src, anchor[1], prev[2]) {
			kept = append(kept, line)
			continue
		}
		kept[len(kept)-1] = fmt.Appendf(nil, "%s %s %s", prev[1], anchor[1], prev[2])
	}
	return bytes.Join(kept, []byte("\n"))
}

// sourceHadAnchoredComment reports whether any line of src carries both the
// anchor and the comment, which is the `key: &anchor  # comment` shape this
// repair exists to put back.
func sourceHadAnchoredComment(src, anchor, comment []byte) bool {
	for _, line := range bytes.Split(src, []byte("\n")) {
		if bytes.Contains(line, anchor) && bytes.Contains(line, comment) {
			return true
		}
	}
	return false
}
