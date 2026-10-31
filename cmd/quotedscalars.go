package cmd

import (
	"bytes"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 decodes a quoted scalar to its text and remembers only which
// quote it wore, so its encoder escapes that text again from scratch and the
// author's own escaping is gone. A literal tab comes back as "\t", while
// "\u00e9", "\x41" and "\u0041\u0042" come back as the characters they name.
// None of them changes a value, and all of them land on lines the edit never
// touched — #252's literal-tab row, which turned out to be one case of the
// whole escape set.
//
// recordQuotedScalars / maskQuotedScalars / restoreQuotedScalars bridge it the
// way blockscalars.go and flowcollections.go bridge theirs, and for the same
// reason: how a scalar was escaped is not representable in yaml.Node. The
// encoder is handed a one-line plain marker in the scalar's place and the marker
// is swapped for the source's own text afterward.
//
// Only a scalar whose source spelling differs from the encoder's is recorded, so
// the overwhelming majority of a document's scalars never go near the marker.
// That is the gate that keeps the reach of this proportional to the problem: a
// single-quoted scalar has one escape and yaml.v3 writes it the same way, so one
// is recorded only in the rare case it was spelled some other way.

// quotedMarkerCore is the fixed part of the stand-in marker. Like the other
// two it is plain-scalar-safe: the encoder emits it unquoted, on one line,
// wherever the scalar sat — including in a mapping key, which is a position the
// other two have nothing to restore into.
const quotedMarkerCore = "yaymlqQuotedScalar"

// quotedScalar is one single- or double-quoted scalar as the source spelled it.
type quotedScalar struct {
	node *yaml.Node
	// value and style are what the decoder produced. An edit that replaced
	// either replaced the scalar, so the record no longer describes what is in
	// the tree and is not applied.
	value  string
	style  yaml.Style
	marker string
	// text is the scalar's source spelling, quotes included, from one line.
	text   string
	masked bool
}

// recordQuotedScalars walks every document in a stream (before any mutation,
// while Node.Line still lines up with source) and records each quoted scalar
// whose spelling the encoder would not reproduce. lines is the whole raw input
// split once, shared with the other fixups.
func recordQuotedScalars(docs []*yaml.Node, lines []string, source []byte) []quotedScalar {
	var out []quotedScalar
	for _, d := range docs {
		walkQuotedScalars(d, lines, &out)
	}
	if len(out) == 0 {
		return nil
	}
	prefix := quotedMarkerCore
	for bytes.Contains(source, []byte(prefix)) {
		prefix += "z"
	}
	for i := range out {
		out[i].marker = prefix + strconv.Itoa(i)
	}
	return out
}

func walkQuotedScalars(n *yaml.Node, lines []string, out *[]quotedScalar) {
	if n.Kind == yaml.ScalarNode {
		considerQuotedScalar(n, lines, out)
		return
	}
	for _, c := range n.Content {
		walkQuotedScalars(c, lines, out)
	}
}

// considerQuotedScalar records n when it is a quoted scalar whose source
// spelling can be read off its line, differs from what the encoder would write,
// and re-parses to the value the decoder produced.
func considerQuotedScalar(n *yaml.Node, lines []string, out *[]quotedScalar) {
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) == 0 {
		return
	}
	if n.Line < 1 || n.Line > len(lines) {
		return
	}
	line := lines[n.Line-1]
	start, ok := quoteStart(line, n.Column)
	if !ok {
		return
	}
	end, ok := endOfQuoted(line, start)
	if !ok {
		// The scalar runs past the end of its line, so there is no single
		// spelling to read; it is left to the encoder.
		return
	}
	text := line[start:end]
	canon, err := yaml.Marshal(n)
	if err != nil {
		return
	}
	if text == strings.TrimRight(string(canon), "\n") {
		return // the source already spelled it the encoder's way
	}
	if !quotedReproduces(text, n.Value) {
		return
	}
	*out = append(*out, quotedScalar{node: n, value: n.Value, style: n.Style, text: text})
}

// quoteStart returns the index of the quote that opens the scalar a node reports
// itself at. It scans forward from the node's column, which is where an anchor
// or a tag sits when the node carries one, and requires that only those stand in
// between — the same check flowBracket makes before trusting a bracket it found.
func quoteStart(line string, col int) (int, bool) {
	if col < 1 || col > len(line) {
		return 0, false
	}
	rest := line[col-1:]
	i := strings.IndexAny(rest, `'"`)
	if i < 0 {
		return 0, false
	}
	for _, f := range strings.Fields(rest[:i]) {
		if f[0] != '&' && f[0] != '!' {
			return 0, false
		}
	}
	return col - 1 + i, true
}

// quotedReproduces re-parses the recorded spelling and reports whether it yields
// value — the text the decoder got from the same source. Nothing is restored on
// the strength of a record that fails this: the spelling is read out of raw
// text, where a construct quoteStart does not model would otherwise splice a
// different string over the right one.
func quotedReproduces(text, value string) bool {
	got, ok := decodeOne([]byte(text + "\n"))
	return ok && got == value
}

// maskQuotedScalars swaps each recorded scalar's text for its marker and clears
// the quoting, so the encoder emits a one-line plain scalar where it was. Run
// after the mutation: a scalar the edit replaced no longer matches what was
// recorded, and the caller's new value is the caller's.
func maskQuotedScalars(quoted []quotedScalar) {
	for i := range quoted {
		q := &quoted[i]
		if q.node.Value != q.value || q.node.Style != q.style {
			continue
		}
		q.node.Value = q.marker
		q.node.Style &^= yaml.SingleQuotedStyle | yaml.DoubleQuotedStyle
		q.masked = true
	}
}

// restoreQuotedScalars puts each masked scalar's source spelling back into out.
// A scalar the edit deleted simply never appears, since the match is on the
// marker the encoder did or didn't emit rather than on position.
//
// One pass over the whole output, for the same reason restoreFlowCollections
// takes one: a document full of escaped scalars should cost O(output), not
// O(output × scalars).
func restoreQuotedScalars(out []byte, quoted []quotedScalar) []byte {
	byMarker := make(map[string]*quotedScalar, len(quoted))
	for i := range quoted {
		if quoted[i].masked {
			byMarker[quoted[i].marker] = &quoted[i]
		}
	}
	if len(byMarker) == 0 {
		return out
	}
	res := make([]byte, 0, len(out))
	for i := 0; i < len(out); {
		if !bytes.HasPrefix(out[i:], []byte(quotedMarkerCore)) {
			res = append(res, out[i])
			i++
			continue
		}
		j := markerEnd(out, i, len(quotedMarkerCore))
		if q := byMarker[string(out[i:j])]; q != nil {
			res = append(res, q.text...)
		} else {
			// A line of the document's own that happens to contain the core,
			// yielding a marker no record claims.
			res = append(res, out[i:j]...)
		}
		i = j
	}
	return res
}
