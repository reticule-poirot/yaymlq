package cmd

import (
	"bytes"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 decodes a block scalar to its text and remembers only
// whether the style was literal or folded, so its encoder builds the header
// again from scratch: the chomping indicator, the explicit indentation
// indicator, and — in a folded block — where the author broke the lines are
// all gone by then. For the two keep-chomped forms (|+ and >+) that is not
// layout but value, since the trailing line breaks they keep are content; the
// encoder added one of its own and blanklines.go added another, so every edit
// to an unrelated key grew the block by a line and `set -i` stopped being
// idempotent (#250).
//
// recordBlockScalars / maskBlockScalars / restoreBlockScalars bridge that the
// way the other five fixups bridge theirs, with one difference: there is
// nothing in the node to put the fact back into — a header is not
// representable in yaml.Node. So the encoder is handed a one-line plain marker
// in the scalar's place and the marker is swapped back for the source's own
// block afterward. The encoder never sees a block scalar, so it never forms an
// opinion about one.
//
// Every record is checked before it is trusted: the recorded header and body
// are re-parsed and must yield the value the decoder produced, or the node is
// left to the encoder. That is what keeps a misjudged block extent from
// splicing broken YAML — the extent has to be read out of the source text,
// where a nested or compact construct can put the body somewhere this doesn't
// predict.

// blockMarkerCore is the fixed part of the stand-in marker, so that
// restoreBlockScalars can find a candidate in a line without knowing the
// per-stream prefix. It is deliberately plain-scalar-safe: the encoder emits
// it unquoted and unwrapped, on one line, however the document is shaped.
const blockMarkerCore = "yaymlqBlockScalar"

// blockScalar is one literal or folded scalar as the source spelled it.
type blockScalar struct {
	node *yaml.Node
	// value and style are what the decoder produced. An edit that replaced
	// either of them replaced the scalar, so the record no longer describes
	// what is in the tree and is not applied.
	value  string
	style  yaml.Style
	marker string
	// header is the indicator run alone — "|", ">-", "|2", "|2-" — never the
	// anchor, tag or comment that may share the line.
	header string
	// body is the block's source lines below the header, verbatim and in
	// order, including the blank lines a keep-chomped block counts as content.
	body []string
	// bound is the indentation the block's content is measured against — the
	// column of the key, or of the dash of the sequence it belongs to. The
	// same quantity restoreBlockScalars reads off the output line, so the two
	// differ only when the indentation around the block really changed.
	bound int
	// keep records chomping, which decides who owns the blank lines below the
	// block: under "+" they are the value's and body already holds them, so
	// the restore drops any the encoder emitted there.
	keep   bool
	masked bool
}

// recordBlockScalars walks every document in a stream (before any mutation,
// while Node.Line still lines up with source) and records each literal or
// folded scalar's source spelling. lines is the whole raw input split once,
// shared with the other fixups.
func recordBlockScalars(docs []*yaml.Node, lines []string, source []byte) []blockScalar {
	var out []blockScalar
	for _, d := range docs {
		walkBlockScalars(d, lines, &out)
	}
	if len(out) == 0 {
		return nil
	}
	prefix := blockMarkerPrefix(source)
	for i := range out {
		out[i].marker = prefix + strconv.Itoa(i)
	}
	return out
}

// blockMarkerPrefix extends blockMarkerCore until it appears nowhere in
// source, so no marker can collide with the document's own text.
func blockMarkerPrefix(source []byte) string {
	prefix := blockMarkerCore
	for bytes.Contains(source, []byte(prefix)) {
		prefix += "z"
	}
	return prefix
}

// walkBlockScalars descends into values and sequence elements, never into a
// mapping key: a block scalar used as a key is emitted in explicit-key form,
// which this restore has no line to splice into. Each candidate is considered
// by its parent, which is what knows the indentation the block's content is
// measured against.
func walkBlockScalars(n *yaml.Node, lines []string, out *[]blockScalar) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			considerBlockScalar(c, 0, lines, out)
			walkBlockScalars(c, lines, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			considerBlockScalar(n.Content[i+1], n.Content[i].Column-1, lines, out)
			walkBlockScalars(n.Content[i+1], lines, out)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			considerBlockScalar(c, n.Column-1, lines, out)
			walkBlockScalars(c, lines, out)
		}
	}
}

// considerBlockScalar records n when it is a block scalar whose source
// spelling can be read back out of lines and verified. parent is the
// indentation the block's content indentation is measured against — the
// column of the key, or of the sequence's dash.
func considerBlockScalar(n *yaml.Node, parent int, lines []string, out *[]blockScalar) {
	if n.Kind != yaml.ScalarNode || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		return
	}
	if n.Line < 1 || n.Line > len(lines) {
		return
	}
	line := strings.TrimSuffix(lines[n.Line-1], "\r")
	header, idx, indicator, ok := blockHeader(line, n.Column)
	if !ok {
		return
	}
	// A header on a line of its own is left to the encoder, which only ever
	// emits the scalar on its key's line: there is no line of the author's
	// shape to splice the block back into, and guessing one would re-indent
	// the body against a prefix that moved. Only the header's placement is
	// normalised that way — the value comes through either route.
	if idx > parent && strings.TrimSpace(line[:idx]) == "" {
		return
	}
	// lineBound has to read the same indentation out of the output line that
	// the tree gave for the source one, so the two are checked to agree here
	// rather than assumed to. A disagreement means the line is shaped in a way
	// lineBound doesn't model, and leaving the block to the encoder is then
	// strictly better than shifting its body by a wrong amount.
	if lineBound(line, idx) != parent {
		return
	}
	keep := strings.Contains(header, "+")
	body, ok := blockBody(lines, n.Line, parent, indicator, keep)
	if !ok {
		return
	}
	if !blockReproduces(header, body, parent, n.Value) {
		return
	}
	*out = append(*out, blockScalar{
		node:   n,
		value:  n.Value,
		style:  n.Style,
		header: header,
		body:   body,
		bound:  parent,
		keep:   keep,
	})
}

// blockHeader reads the indicator run that opens a block scalar: "|" or ">",
// then any mix of a chomping indicator and an explicit indentation indicator,
// in either order. It scans forward from the node's own start column, which is
// where an anchor or a tag sits when the node carries one, so the indicator
// may be a few characters in. The run has to end the line (a trailing comment
// aside), which is what rules out a "|" that belongs to something else.
func blockHeader(line string, col int) (header string, idx, indicator int, ok bool) {
	if col < 1 || col > len(line) {
		return "", 0, 0, false
	}
	rest := line[col-1:]
	i := strings.IndexAny(rest, "|>")
	if i < 0 || strings.ContainsRune(rest[:i], '#') {
		return "", 0, 0, false
	}
	j := i + 1
	for j < len(rest) {
		c := rest[j]
		if c >= '1' && c <= '9' {
			if indicator != 0 {
				return "", 0, 0, false
			}
			indicator = int(c - '0')
		} else if c != '+' && c != '-' {
			break
		}
		j++
	}
	// Whatever follows the run has to be the end of the line, the white space
	// before a comment (a tab counts: the header's trailing white space is
	// s-white, space or tab) or the comment itself. Anything else means the
	// "|" was part of something that isn't a header, and the block is left to
	// the encoder.
	if j < len(rest) && rest[j] != ' ' && rest[j] != '\t' && rest[j] != '#' {
		return "", 0, 0, false
	}
	return rest[i:j], col - 1 + i, indicator, true
}

// blockBody returns the block's source lines below its header. The extent is
// the block's content indentation: the explicit indicator when there is one,
// otherwise the indentation of the first non-blank line below the header,
// which has to be deeper than the construct the block belongs to. A trailing
// run of blank lines is claimed only under keep chomping, where it is value;
// under the other three it is spacing before the next key, and blanklines.go
// owns it as it owns every other blank line in the document.
func blockBody(lines []string, header, parent, indicator int, keep bool) (body []string, ok bool) {
	content := 0
	if indicator > 0 {
		content = parent + indicator
	}
	// Splitting a source that ends in a newline leaves a final empty element
	// that is not a line of the document — claiming it would invent a blank
	// line at the end of every file.
	limit := len(lines)
	if limit > 0 && lines[limit-1] == "" {
		limit--
	}
	var pending []string
	for i := header + 1; i <= limit; i++ {
		line := strings.TrimSuffix(lines[i-1], "\r")
		if blockBlank(line) {
			pending = append(pending, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if content == 0 {
			if indent <= parent {
				break
			}
			content = indent
		}
		if indent < content {
			break
		}
		body = append(body, pending...)
		pending = pending[:0]
		body = append(body, line)
	}
	if keep {
		body = append(body, pending...)
	}
	return body, len(body) > 0
}

// blockBlank reports whether line is an empty line of a block scalar rather
// than one of its content lines. Only spaces indent YAML, so a line holding a
// tab holds content however empty it looks — and in a block scalar that tab is
// a character of the value.
func blockBlank(line string) bool {
	return strings.TrimLeft(line, " ") == ""
}

// blockReproduces re-parses the recorded header and body and reports whether
// they yield value — the text the decoder got from the same source. Nothing is
// restored on the strength of a record that fails this: the block's extent is
// read out of the raw text, and a construct that puts the body somewhere
// blockBody doesn't predict would otherwise splice a truncated block over a
// correct one.
func blockReproduces(header string, body []string, parent int, value string) bool {
	probe := make([]string, 0, len(body)+2)
	probe = append(probe, "k: "+header)
	for _, line := range body {
		if blockBlank(line) {
			probe = append(probe, line)
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " ")) <= parent {
			return false
		}
		probe = append(probe, line[parent:])
	}
	var got map[string]string
	if err := yaml.Unmarshal([]byte(strings.Join(probe, "\n")+"\n"), &got); err != nil {
		return false
	}
	return len(got) == 1 && got["k"] == value
}

// lineBound returns the indentation the block scalar at idx on line is
// measured against: the column of the innermost construct that owns it. That
// is the key it is the value of, or — when the scalar follows a dash directly
// — the dash of the sequence it is an element of, since a sequence's own
// indentation is where its dash sits and not where its items start.
//
// It is the same quantity the node tree gives as parent, which
// considerBlockScalar checks it against before recording anything. Having it
// readable from a line is what lets restoreBlockScalars measure the *output*
// line the same way, so the two agree unless the indentation around the block
// really changed — which is the only case the body needs shifting for, and
// where measuring past the dash instead got it wrong by the width of a dash
// gutter the encoder had normalised.
func lineBound(line string, idx int) int {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	dash := -1
	for i+1 < idx && line[i] == '-' && line[i+1] == ' ' {
		dash = i
		i += 2
		for i < idx && line[i] == ' ' {
			i++
		}
	}
	if i == idx && dash >= 0 {
		return dash
	}
	return i
}

// maskBlockScalars swaps each recorded scalar's text for its marker and clears
// the block style, so the encoder emits a one-line plain scalar where the
// block was. Run after the mutation: a scalar the edit replaced no longer
// matches what was recorded, and the caller's new value is the caller's — it
// is emitted for what it now is, not forced back into a header describing text
// that is gone.
func maskBlockScalars(blocks []blockScalar) {
	for i := range blocks {
		b := &blocks[i]
		if b.node.Value != b.value || b.node.Style != b.style {
			continue
		}
		b.node.Value = b.marker
		b.node.Style &^= yaml.LiteralStyle | yaml.FoldedStyle
		b.masked = true
	}
}

// restoreBlockScalars puts each masked block back into out: the marker becomes
// the source's own header, and the source's own body lines follow it. A block
// the edit deleted simply never appears, since the match is on the marker the
// encoder did or didn't emit rather than on position in the document.
//
// It runs after the line-scanning fixups and before the line endings are
// reapplied, so a body line is never mistaken for a comment gutter or an
// anchor to rejoin, and a CRLF document gets its endings on the restored lines
// too.
func restoreBlockScalars(out []byte, blocks []blockScalar) []byte {
	byMarker := make(map[string]*blockScalar, len(blocks))
	for i := range blocks {
		if blocks[i].masked {
			byMarker[blocks[i].marker] = &blocks[i]
		}
	}
	if len(byMarker) == 0 {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	res := make([][]byte, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		idx, marker, found := blockMarkerAt(line)
		b := byMarker[marker]
		if !found || b == nil {
			res = append(res, line)
			continue
		}
		head := make([]byte, 0, idx+len(b.header)+len(line)-idx-len(marker))
		head = append(head, line[:idx]...)
		head = append(head, b.header...)
		head = append(head, line[idx+len(marker):]...)
		res = append(res, head)
		shift := lineBound(string(line), idx) - b.bound
		for _, body := range b.body {
			res = append(res, shiftBlockLine(body, shift))
		}
		if !b.keep {
			continue
		}
		// Under keep chomping there is no such thing as a blank line between
		// the block and the next node that isn't part of the value, and the
		// source's own are already in body. So whatever the encoder put there
		// — blanklines.go's marker, whose own arithmetic can land a line off
		// when a head comment sits between the two — is a second copy, and
		// dropping it here is what makes the restore independent of that
		// (#250). The final element is the newline the output ends with, never
		// a line of the document, so it is never consumed.
		for i+1 < len(lines)-1 && len(bytes.TrimSpace(lines[i+1])) == 0 {
			i++
		}
	}
	return bytes.Join(res, []byte("\n"))
}

// blockMarkerAt finds a stand-in marker in line. The constant core is enough
// to spot a candidate; a line of the document's own that happens to contain it
// yields a marker no record claims, and is left alone.
func blockMarkerAt(line []byte) (idx int, marker string, ok bool) {
	i := bytes.Index(line, []byte(blockMarkerCore))
	if i < 0 {
		return 0, "", false
	}
	j := i + len(blockMarkerCore)
	for j < len(line) && (line[j] == 'z' || (line[j] >= '0' && line[j] <= '9')) {
		j++
	}
	return i, string(line[i:j]), true
}

// shiftBlockLine re-indents one body line by shift columns. A blank or
// space-only line is left exactly as the source wrote it: inside a literal
// block those spaces are content, so adding or removing them would change the
// value rather than the layout.
func shiftBlockLine(line string, shift int) []byte {
	if shift == 0 || blockBlank(line) {
		return []byte(line)
	}
	if shift > 0 {
		return []byte(strings.Repeat(" ", shift) + line)
	}
	drop := -shift
	if n := len(line) - len(strings.TrimLeft(line, " ")); drop > n {
		drop = n
	}
	return []byte(line[drop:])
}
