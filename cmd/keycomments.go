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

// restoreKeyComments walks n and moves each misattributed comment
// back onto the key that carries the anchor. lines comes from sourceLines, so
// its numbering is yaml.Node.Line's; node lines are absolute across a document
// stream, so one split covers every document.
func restoreKeyComments(n *yaml.Node, lines []string) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			reattachKeyComment(n.Content[i], n.Content[i+1], lines)
		}
	}
	for _, c := range n.Content {
		restoreKeyComments(c, lines)
	}
}

// reattachKeyComment moves the leading segment of value's first child's line
// comment onto key, when the source shows it was written on key's line.
func reattachKeyComment(key, value *yaml.Node, lines []string) {
	if !carriesSourceToken(value) || key.LineComment != "" {
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

// carriesSourceToken reports whether value opens with an anchor or a tag the
// source wrote, which is what puts the decoder in the state this file repairs.
// An anchor is only ever the author's. A tag is not: the decoder supplies
// "!!map"/"!!seq" for every collection, and TaggedStyle is what tells the two
// apart — set only when the tag was written down, which is also exactly when
// the encoder emits it again and strands it (#256).
func carriesSourceToken(value *yaml.Node) bool {
	return value.Anchor != "" || value.Style&yaml.TaggedStyle != 0
}

// lineHasComment reports whether the 1-indexed source line contains comment.
func lineHasComment(lines []string, line int, comment string) bool {
	if line < 1 || line > len(lines) || comment == "" {
		return false
	}
	return strings.Contains(lines[line-1], comment)
}

// Putting the comment back on the key is only half the job, because the
// encoder cannot render it there: handed a line comment on a key whose value
// is an anchored or tagged block collection, it emits that token alone on the
// next line. Measured at every nesting depth, the artifact is always the same
// shape — the token by itself, at column 0, directly below the key:
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
//
// At column 0 that line is not part of any document, whatever the source
// looked like, so there is always something to repair — the source only
// decides *which* repair (#256). Written on the key's line, the token goes
// back there. Written on a line of its own, it stays on one and is indented to
// the collection it introduces, because rewriting a line the edit never
// touched is the thing this layer exists to avoid.
var (
	loneTokenLine  = regexp.MustCompile(`^(\s*)((?:[&!]\S*)(?:\s+[&!]\S*)*)$`)
	keyThenComment = regexp.MustCompile(`^(\s*.*?:)\s*(#.*)$`)
)

// rejoinStrandedTokens repairs the anchor or tag the encoder stranded on its own
// line below a commented key. srcLines is the pre-edit document, which is what
// says whether the author wrote that token on the key's line or on one of its
// own.
func rejoinStrandedTokens(out []byte, srcLines []string) []byte {
	if !bytes.ContainsAny(out, "&!") {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	kept := make([][]byte, 0, len(lines))
	for i, line := range lines {
		token := loneTokenLine.FindSubmatch(line)
		if token == nil || len(kept) == 0 {
			kept = append(kept, line)
			continue
		}
		prev := keyThenComment.FindSubmatch(kept[len(kept)-1])
		if prev == nil {
			kept = append(kept, line)
			continue
		}
		if sourceHadTokenWithComment(srcLines, token[2], prev[2]) {
			kept[len(kept)-1] = fmt.Appendf(nil, "%s %s %s", prev[1], token[2], prev[2])
			continue
		}
		// Only a token at column 0 is the artifact; one the encoder indented
		// is already where it belongs, and a lone token under a key that
		// carries no comment was never moved.
		if len(token[1]) > 0 {
			kept = append(kept, line)
			continue
		}
		kept = append(kept, indentLike(token[2], lines, i+1))
	}
	return bytes.Join(kept, []byte("\n"))
}

// indentLike returns token indented to match lines[next], the collection the
// token introduces. If there is no such line to measure — the token was the
// last thing in the stream, which no valid document does — it is returned
// unchanged rather than guessed at.
func indentLike(token []byte, lines [][]byte, next int) []byte {
	if next >= len(lines) {
		return token
	}
	indent := lines[next][:len(lines[next])-len(bytes.TrimLeft(lines[next], " "))]
	if len(indent) == 0 {
		return token
	}
	return append(append([]byte{}, indent...), token...)
}

// sourceHadTokenWithComment reports whether any source line carries both the
// token and the comment, which is the `key: &anchor  # comment` shape — tag or
// anchor, or both in the order the source had them.
func sourceHadTokenWithComment(srcLines []string, token, comment []byte) bool {
	for _, line := range srcLines {
		if strings.Contains(line, string(token)) && strings.Contains(line, string(comment)) {
			return true
		}
	}
	return false
}
