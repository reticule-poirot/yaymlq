package cmd

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 records that a collection was written in flow style and
// nothing else about how, so its encoder renders one from scratch: on a single
// line, exactly one space after each comma, none inside the brackets, no
// trailing comma. The author's own spelling is gone by then, so an edit to an
// unrelated key rewrites every flow collection in the document — "[ 1, 2 ]"
// becomes "[1, 2]", "[1,2]" becomes "[1, 2]", and one broken over four lines
// comes back on one (#252).
//
// recordFlowCollections / maskFlowCollections / restoreFlowCollections bridge
// that the way blockscalars.go bridges the block-scalar header, and for the same
// reason: a flow collection's spelling is not representable in yaml.Node, so
// there is nothing to put the fact back into. The encoder is handed a one-line
// plain marker where the collection was and the marker is swapped for the
// source's own text afterward, so the encoder never forms an opinion about a
// flow collection at all.
//
// Only the outermost collection of a nest is recorded: an inner one is already
// inside the outer one's text, and recording both would restore the same
// characters twice. Every record is re-parsed and must yield the value the
// decoder produced before it is trusted, which is what keeps a misread extent
// — a quoted bracket, a comment, a scalar carrying "#" — from splicing broken
// YAML over correct output.

// flowMarkerCore is the fixed part of the stand-in marker, so the restore can
// spot a candidate without knowing the per-stream prefix. Like
// blockMarkerCore it is plain-scalar-safe: the encoder emits it unquoted, on
// one line, wherever the collection sat.
const flowMarkerCore = "yaymlqFlowNode"

// flowCollection is one flow mapping or sequence as the source spelled it.
type flowCollection struct {
	node *yaml.Node
	// canon is the node rendered by yaml.v3 at record time. An edit that
	// changed anything inside the collection changes this too, so the record no
	// longer describes what is in the tree and is not applied — the caller's new
	// collection is the caller's, emitted for what it now is.
	canon  string
	marker string
	// text is the collection's source lines: the first from its opening bracket
	// onward, the last up to and including the bracket that closes it, the rest
	// verbatim.
	text []string
	// column is the 1-indexed source column the opening bracket sat in. The
	// restore reads the same quantity off the output and shifts the
	// continuation lines by the difference, so they move with the bracket when
	// the indentation around it changed.
	column int
	masked bool
}

// recordFlowCollections walks every document in a stream (before any mutation,
// while Node.Line still lines up with source) and records each flow
// collection's source spelling. lines is the whole raw input split once, shared
// with the other fixups.
func recordFlowCollections(docs []*yaml.Node, lines []string, source []byte) []flowCollection {
	var out []flowCollection
	for _, d := range docs {
		walkFlowCollections(d, lines, &out)
	}
	if len(out) == 0 {
		return nil
	}
	prefix := flowMarkerCore
	for bytes.Contains(source, []byte(prefix)) {
		prefix += "z"
	}
	for i := range out {
		out[i].marker = prefix + strconv.Itoa(i)
	}
	return out
}

// walkFlowCollections descends until it finds a flow collection worth
// recording and then stops: everything below one is inside its text already.
// A collection it declines to record is descended into, since a flow
// collection nested in one this cannot read may still be readable itself.
func walkFlowCollections(n *yaml.Node, lines []string, out *[]flowCollection) {
	if n.Style&yaml.FlowStyle != 0 && (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) {
		if considerFlowCollection(n, lines, out) {
			return
		}
	}
	for _, c := range n.Content {
		walkFlowCollections(c, lines, out)
	}
}

// considerFlowCollection records n when its source spelling can be read back
// out of lines, differs from what the encoder would write, and re-parses to the
// value the decoder produced. It reports whether a record was made.
func considerFlowCollection(n *yaml.Node, lines []string, out *[]flowCollection) bool {
	if n.Line < 1 || n.Line > len(lines) {
		return false
	}
	// An anchor below the collection is the one thing the mask cannot carry: it
	// would vanish with the children, and an alias to it elsewhere in the
	// document would then be emitted against an anchor the output no longer
	// defines — a file that does not parse, in exchange for spacing. An anchor
	// on the collection itself stays on the marker, so it is no obstacle.
	if anchorBelow(n) {
		return false
	}
	text, ok := flowExtent(lines, n.Line, n.Column)
	if !ok {
		return false
	}
	canon, err := yaml.Marshal(n)
	if err != nil {
		return false
	}
	// Nothing to restore when the source already spelled it the encoder's way,
	// which is the common case for a short collection — and one fewer node
	// through the marker round-trip is one fewer chance to get it wrong.
	if len(text) == 1 && text[0] == strings.TrimRight(string(canon), "\n") {
		return false
	}
	if !flowReproduces(text, n) {
		return false
	}
	*out = append(*out, flowCollection{
		node:   n,
		canon:  string(canon),
		text:   text,
		column: n.Column,
	})
	return true
}

// anchorBelow reports whether any node under n carries an anchor. n's own
// anchor doesn't count: it survives on the masked scalar.
func anchorBelow(n *yaml.Node) bool {
	for _, c := range n.Content {
		if c.Anchor != "" || anchorBelow(c) {
			return true
		}
	}
	return false
}

// flowExtent reads the flow collection opening at line:col out of lines and
// returns its source text. It tracks bracket depth through quoted scalars and
// comments, which is what a flow collection's extent actually depends on — a
// "]" inside quotes or after a "#" closes nothing.
//
// A quoted scalar broken across lines ends the scan without a record: the
// collection is then left to the encoder, which is strictly better than
// guessing where it ended.
func flowExtent(lines []string, line, col int) ([]string, bool) {
	col, ok := flowBracket(lines[line-1], col)
	if !ok {
		return nil, false
	}
	var text []string
	depth := 0
	for n := line; n <= len(lines); n++ {
		s := lines[n-1]
		from := 0
		if n == line {
			from = col - 1
		}
		end, closed, ok := scanFlowLine(s, from, &depth)
		if !ok {
			return nil, false
		}
		text = append(text, s[from:end])
		if closed {
			return text, true
		}
	}
	return nil, false
}

// flowBracket returns the 1-indexed column of the bracket that opens the
// collection a node reports itself at. It scans forward from col, which is
// where an anchor or a tag sits when the node carries one — yaml.v3 gives a
// node's position as the start of its properties, not of its value, so a
// collection written "&base { a: 1 }" reports the column of the "&".
//
// Whatever stands between is required to be those properties and nothing else,
// the same check blockHeader makes before trusting a "|" it found: anything
// further along is a bracket that belongs to something other than this node.
func flowBracket(s string, col int) (int, bool) {
	if col < 1 || col > len(s) {
		return 0, false
	}
	rest := s[col-1:]
	i := strings.IndexAny(rest, "{[")
	if i < 0 {
		return 0, false
	}
	for _, f := range strings.Fields(rest[:i]) {
		if f[0] != '&' && f[0] != '!' {
			return 0, false
		}
	}
	return col + i, true
}

// scanFlowLine advances depth over one line of a flow collection, starting at
// from. It returns the index just past the bracket that brought depth back to
// zero and closed=true when the collection ends on this line, or the end of the
// line and closed=false when it carries on. ok is false when the line ends
// inside a quoted scalar, which this does not follow across a break.
func scanFlowLine(s string, from int, depth *int) (end int, closed, ok bool) {
	for i := from; i < len(s); {
		switch s[i] {
		case '\'', '"':
			if !opensQuotedScalar(s, from, i) {
				i++
				continue
			}
			j, ok := endOfQuoted(s, i)
			if !ok {
				return 0, false, false
			}
			i = j
		case '#':
			// A "#" opens a comment only at the start of what is being scanned
			// or after white space; inside a plain scalar it is just a
			// character, which is why "a#b" is a key and not a comment.
			if i == from || s[i-1] == ' ' || s[i-1] == '\t' {
				return len(s), false, true
			}
			i++
		case '{', '[':
			*depth++
			i++
		case '}', ']':
			*depth--
			i++
			if *depth <= 0 {
				return i, true, true
			}
		default:
			i++
		}
	}
	return len(s), false, true
}

// opensQuotedScalar reports whether the quote at i starts a quoted scalar
// rather than being a character inside a plain one. A node begins where the
// collection or the previous entry left off, so a quote only opens a scalar
// directly after "{", "[", "," or ":" — "{00\"0}" is the plain scalar 00"0 and
// not a quote at all, and reading it as one ran the scan off the end of the line
// and cost the whole collection its record.
func opensQuotedScalar(s string, from, i int) bool {
	for j := i - 1; j >= from; j-- {
		switch s[j] {
		case ' ', '\t':
			continue
		case '{', '[', ',', ':':
			return true
		default:
			return false
		}
	}
	return true
}

// endOfQuoted returns the index just past the closing quote of the scalar
// starting at i, or ok=false when the line ends first — this does not follow a
// quoted scalar across a break. A single-quoted scalar escapes its quote by
// doubling it; a double-quoted one with a backslash.
func endOfQuoted(s string, i int) (int, bool) {
	if s[i] == '\'' {
		return endOfSingleQuoted(s, i)
	}
	return endOfDoubleQuoted(s, i)
}

func endOfSingleQuoted(s string, i int) (int, bool) {
	for j := i + 1; j < len(s); j++ {
		if s[j] != '\'' {
			continue
		}
		if j+1 < len(s) && s[j+1] == '\'' {
			j++
			continue
		}
		return j + 1, true
	}
	return 0, false
}

func endOfDoubleQuoted(s string, i int) (int, bool) {
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			return j + 1, true
		}
	}
	return 0, false
}

// flowImplicitNull reports whether any flow collection under n holds a null the
// source wrote by leaving it out — "{a}" rather than "{a: 1}", "[&x]" rather
// than "[&x 1]". yaml.v3 decodes those as null and its encoder writes each back
// as the empty string, so such a collection changes value on a round-trip
// whatever this file does.
//
// Restoring the source text fixes it where the extent can be read; where it
// cannot, the collection is left to the encoder and the pre-existing change
// stands. Nothing here can promise otherwise, so FuzzEditFlowCollections
// excludes it rather than asserting against yaml.v3.
func flowImplicitNull(n *yaml.Node, inFlow bool) bool {
	if inFlow && n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == "" {
		return true
	}
	below := inFlow || n.Style&yaml.FlowStyle != 0
	for _, c := range n.Content {
		if flowImplicitNull(c, below) {
			return true
		}
	}
	return false
}

// flowReproduces re-parses the recorded text and reports whether it yields the
// value the decoder got from the same source. Nothing is restored on the
// strength of a record that fails this: the extent is read out of raw text,
// where a construct scanFlowLine does not model would otherwise splice a
// truncated collection over a correct one.
//
// A collection holding an alias to an anchor defined outside it fails here, and
// is meant to: the text does not stand on its own, so there is no way to check
// what it means.
func flowReproduces(text []string, n *yaml.Node) bool {
	var want any
	if err := n.Decode(&want); err != nil {
		return false
	}
	var got any
	if err := yaml.Unmarshal([]byte(strings.Join(text, "\n")+"\n"), &got); err != nil {
		return false
	}
	return reflect.DeepEqual(got, want)
}

// maskFlowCollections swaps each recorded collection for a one-line plain
// scalar holding its marker, so the encoder writes the marker where the
// collection was. Run after the mutation: a collection the edit changed no
// longer renders the way it was recorded, and is left to the encoder.
//
// The node's anchor, tag and comments stay on the marker, so an alias to the
// collection and a comment on its line both come through; the tag is cleared
// only of the !!map/!!seq the decoder supplies for every collection, which
// would otherwise be emitted against a scalar.
func maskFlowCollections(flows []flowCollection) {
	for i := range flows {
		f := &flows[i]
		canon, err := yaml.Marshal(f.node)
		if err != nil || string(canon) != f.canon {
			continue
		}
		f.node.Kind = yaml.ScalarNode
		f.node.Style = 0
		f.node.Value = f.marker
		f.node.Content = nil
		if f.node.Tag == "!!map" || f.node.Tag == "!!seq" {
			f.node.Tag = ""
		}
		f.masked = true
	}
}

// restoreFlowCollections puts each masked collection back into out: the marker
// becomes the source's own text, with the continuation lines shifted by however
// far the opening bracket moved. A collection the edit deleted simply never
// appears, since the match is on the marker the encoder did or didn't emit
// rather than on position in the document.
//
// One pass over the whole output rather than one search per record, so a
// document full of flow collections costs O(output) and not O(output ×
// collections) — the same reason sourceLines is split once per stream (#71).
func restoreFlowCollections(out []byte, flows []flowCollection) []byte {
	byMarker := make(map[string]*flowCollection, len(flows))
	for i := range flows {
		if flows[i].masked {
			byMarker[flows[i].marker] = &flows[i]
		}
	}
	if len(byMarker) == 0 {
		return out
	}
	res := make([]byte, 0, len(out))
	lineStart := 0
	for i := 0; i < len(out); {
		if out[i] == '\n' {
			res = append(res, '\n')
			i++
			lineStart = len(res)
			continue
		}
		if !bytes.HasPrefix(out[i:], []byte(flowMarkerCore)) {
			res = append(res, out[i])
			i++
			continue
		}
		j := flowMarkerEnd(out, i)
		f := byMarker[string(out[i:j])]
		if f == nil {
			// A line of the document's own that happens to contain the core,
			// yielding a marker no record claims.
			res = append(res, out[i:j]...)
			i = j
			continue
		}
		shift := (len(res) - lineStart) - (f.column - 1)
		res = append(res, f.text[0]...)
		for _, line := range f.text[1:] {
			res = append(res, '\n')
			lineStart = len(res)
			res = append(res, shiftBlockLine(line, shift)...)
		}
		i = j
	}
	return res
}

// flowMarkerEnd returns the index just past the marker candidate starting at i.
func flowMarkerEnd(out []byte, i int) int {
	j := i + len(flowMarkerCore)
	for j < len(out) && (out[j] == 'z' || (out[j] >= '0' && out[j] <= '9')) {
		j++
	}
	return j
}
