package cmd

import (
	"bytes"
	"regexp"
)

// blanklines.go restores vertical spacing by prefixing a node's HeadComment
// with a newline, and the encoder cannot write a head comment on a dash line:
// handed one, it emits the dash alone and pushes the item's body to the next
// line, indented as if the dash were not there.
//
//	s:
//	  - a: 1          s:
//	                    - a: 1
//	  - a: 3    ->
//	    b: 4            -
//	                      a: 3
//	                      b: 4
//
// So a blank line between two sequence items cost the compact `- key:` form of
// the item below it — and the dash came out as "  - ", the tool writing white
// space at the end of a line it had just reformatted, one release after
// trailingspace.go taught it to preserve the author's.
//
// Nothing in the tree avoids this: the artifact appears for any head comment
// the encoder has to render above a dash whose item is a collection, on the
// item node or on its first child alike, and a comment with text in it is
// rendered correctly. Only the bare newline blanklines.go writes trips it. So
// the repair belongs in the encoded bytes, the same division of labor
// rejoinStrandedTokens makes for the anchor the encoder strands below a
// commented key: the node tree carries what it can and the output pass fixes
// up what it cannot.
//
// The body needs no re-indentation. The encoder indents an item's contents to
// the dash's column plus the indent width whether or not the dash shares their
// line, so folding the first line up is the whole repair and the lines below it
// are already where the compact form wants them.
//
// This has no inverse to collide with: a dash the *source* wrote alone is
// compacted by the encoder, which leaves no stranded dash to find. A blank
// line above such a dash is dropped rather than stranded, because blanklines.go
// measures "above" from the item's content line. Both are open gaps (#270),
// neither reachable from here.

// A dash alone on its line, with at least one space after it. The trailing
// space is the encoder's own tell and the only thing separating this from an
// empty sequence item, which it writes as a bare "-" and which must not be
// folded: doing so would swallow the item below.
var strandedDashLine = regexp.MustCompile(`^( *)-[ \t]+$`)

// rejoinStrandedDashes folds the body of a sequence item back onto the dash the
// encoder stranded above it. out is the encoder's own output, so splitting on
// "\n" is right — that is all it emits.
func rejoinStrandedDashes(out []byte) []byte {
	lines := bytes.Split(out, []byte("\n"))
	kept := make([][]byte, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		dash := strandedDashLine.FindSubmatch(lines[i])
		if dash == nil || i+1 >= len(lines) || !foldsOntoDash(lines[i+1], len(dash[1])) {
			kept = append(kept, lines[i])
			continue
		}
		folded := append([]byte{}, dash[1]...)
		folded = append(folded, "- "...)
		folded = append(folded, bytes.TrimLeft(lines[i+1], " ")...)
		kept = append(kept, folded)
		i++ // the body's first line went up onto the dash
	}
	return bytes.Join(kept, []byte("\n"))
}

// foldsOntoDash reports whether body is the first line of the item belonging to
// a dash at the given indent. It has to be indented past the dash to be inside
// it at all, and a comment is never folded: the encoder writes a comment above
// the dash, so one below it belongs to whatever comes next.
func foldsOntoDash(body []byte, indent int) bool {
	rest := bytes.TrimLeft(body, " ")
	if len(rest) == 0 || rest[0] == '#' {
		return false
	}
	return len(body)-len(rest) > indent+1
}
