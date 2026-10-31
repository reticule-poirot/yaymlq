package cmd

import (
	"bytes"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
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
// The mark that does it is the one on the item's first child, not the item:
// markBlankLines recurses into the item, and its first key shares the dash's
// line, so the blank above the dash is above the key too. A newline head comment
// on the item node alone is rendered as a blank line above a compact item; on
// the first child, the encoder has to write it between the dash and the key, and
// can't. Dropping that second mark would work only until a fixup needs the first
// child's head comment for something else — which lone dashes below do — so the
// repair belongs in the encoded bytes, the same division of labor
// rejoinStrandedTokens makes for the anchor the encoder strands below a
// commented key: the node tree carries what it can and the output pass fixes up
// what it cannot.
//
// The body needs no re-indentation. The encoder indents an item's contents to
// the dash's column plus the indent width whether or not the dash shares their
// line, so folding the first line up is the whole repair and the lines below it
// are already where the compact form wants them.
//
// The inverse, a dash the source wrote alone, is restored below by a marker
// comment rather than by a strand, so the two never read each other's output.

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

// The encoder writes every block sequence item compactly, so a dash the source
// put on a line of its own, with the item's body below it, was pulled down onto
// the body by any edit:
//
//	builds:              builds:
//	  -          ->        - env:
//	    env:                   - CGO_ENABLED=0
//	      - CGO_ENABLED=0
//
// The form is not representable in yaml.Node, but one thing the encoder does
// puts it back: it writes the head comment of an item's first child on the dash
// line, after the dash. So the first child of every such item gets a marker
// comment, and the output pass turns "- # <marker>" back into a bare "-" with
// the body already below it at the indentation it had. A marker rather than the
// bare-newline strand rejoinStrandedDashes repairs, because a strand reads as
// "  - " — the same bytes that pass folds — and which one wins would come down
// to pass order. Neither "- # <marker>" nor the bare "-" it becomes matches
// strandedDashLine.
//
// Only a mapping or sequence item is marked. A scalar's first child is itself;
// an empty item has none; a flow item is masked to a scalar before the marks are
// made; and an anchor or tag on the item is written on the dash line by the
// encoder already, which is a different construct and one it keeps.
//
// The record also answers a second question: which line is "above" a lone-dash
// item. Its node's Line is the body's first line, so blanklines.go measured from
// there and a blank line above the dash was two lines up and dropped, while one
// between the dash and the body was moved above the dash. top gives blankAbove
// the dash's line instead.

const loneDashMarkerCore = "yaymlqLoneDash"

// loneDashes is the record: the source line of the dash of every item whose
// dash was alone, and the marker the stream's marks are written with.
type loneDashes struct {
	dash   map[*yaml.Node]int
	marker string
}

func recordLoneDashes(docs []*yaml.Node, lines []string, source []byte) *loneDashes {
	ld := &loneDashes{dash: make(map[*yaml.Node]int)}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.SequenceNode && n.Style&yaml.FlowStyle == 0 {
			for _, c := range n.Content {
				if d := loneDashLine(n, c, lines); d > 0 {
					ld.dash[c] = d
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, d := range docs {
		walk(d)
	}
	// One marker serves the whole stream: every mark is restored the same way,
	// so nothing has to tell them apart — only from the source's own text.
	ld.marker = loneDashMarkerCore
	for bytes.Contains(source, []byte(ld.marker)) {
		ld.marker += "z"
	}
	return ld
}

// loneDashLine returns the source line of item's dash when the source wrote
// that dash alone on a line above the item's content, and 0 otherwise. An item
// node's Line is where its content starts, so a compact item has its dash on
// that line; a lone one has it above, with nothing between the two but blank
// lines and comments. A dash line carrying anything else — a comment, an anchor
// or tag — is not a lone dash: those are where the decoder already puts the
// item's Line, or a comment the encoder would move.
func loneDashLine(seq, item *yaml.Node, lines []string) int {
	col := seq.Column - 1
	isDash := func(line string) bool {
		return len(line) > col && line[col] == '-' && strings.TrimLeft(line[:col], " ") == ""
	}
	if item.Line < 1 || item.Line > len(lines) || isDash(lines[item.Line-1]) {
		return 0
	}
	for n := item.Line - 1; n >= 1 && n >= seq.Line; n-- {
		line := lines[n-1]
		if isDash(line) {
			if strings.TrimRight(line[col:], " \t") != "-" {
				return 0
			}
			return n
		}
		if text := strings.TrimSpace(line); text != "" && text[0] != '#' {
			return 0
		}
	}
	return 0
}

// top returns the source line n begins on for blankAbove's purposes: its dash,
// for a lone-dash item, and its own Line otherwise. Safe on a nil record, for
// callers that never made one.
func (ld *loneDashes) top(n *yaml.Node) int {
	if ld != nil {
		if d, ok := ld.dash[n]; ok {
			return d
		}
	}
	return n.Line
}

// mark writes the marker onto the first child of every recorded item that can
// carry it. It runs after the mutation, so an item the edit replaced is a node
// the record has never seen and comes out compact, and after every mask, since
// a comment on a child changes what the masks compare against the source.
func (ld *loneDashes) mark() {
	for item := range ld.dash {
		if item.Kind != yaml.MappingNode && item.Kind != yaml.SequenceNode {
			continue
		}
		if item.Style&(yaml.FlowStyle|yaml.TaggedStyle) != 0 || item.Anchor != "" || len(item.Content) == 0 {
			continue
		}
		first := item.Content[0]
		first.HeadComment = "# " + ld.marker + "\n" + first.HeadComment
	}
}

// restore turns each marked dash line back into a bare dash. The marker is
// expected only at the end of a dash line; anywhere else it is removed too,
// whole line or trailing comment, so a placement the encoder has never been seen
// to make costs a line break rather than a stray comment in the file.
func (ld *loneDashes) restore(out []byte) []byte {
	if len(ld.dash) == 0 || !bytes.Contains(out, []byte(ld.marker)) {
		return out
	}
	comment := []byte("# " + ld.marker)
	lines := bytes.Split(out, []byte("\n"))
	kept := lines[:0]
	for _, line := range lines {
		i := bytes.Index(line, comment)
		if i < 0 || i+len(comment) != len(line) {
			kept = append(kept, line)
			continue
		}
		if rest := bytes.TrimRight(line[:i], " "); len(bytes.TrimLeft(rest, " ")) > 0 {
			kept = append(kept, rest)
		}
	}
	return bytes.Join(kept, []byte("\n"))
}
