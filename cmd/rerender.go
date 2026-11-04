package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Re-rendering: what the splice does with an edit it cannot place.
//
// When a node's change declines, walk drops what it recorded for that node
// and re-renders the smallest node around it that can stand alone, the
// encoder writing it whole, and the bytes of every other node are still the
// source's. The ladder, smallest first:
//
//   - value: a new value the splice's own renderer declines (an anchored or
//     tagged collection, a scalar over several lines), written by the encoder
//     into the span the splice already found for it;
//   - flow: the outermost flow collection holding the change;
//   - block: a block collection under a key, from the end of the key's line
//     through the entry's last line, the key line kept as written;
//   - doc: the document, between its markers, which stay as written along
//     with the blank lines beside them.
//
// A sequence item's collection, an explicit key and a compact item are never
// placed on their own; the change escalates to the collection around them.
//
// Every re-render works on a copy, so the edited tree stays what the result is
// verified against. Comments on the copy whose text is not inside the span
// being replaced belong to source that stays (a key's line, the lines above a
// "---"), and are dropped from the copy so they are not written twice. Each
// rendering is read back on its own before it is used, and the whole output
// is verified per document (splice); a document that fails is re-rendered
// whole, and one that fails as a whole is an error, never written.

// forceRerender and rerenderMangle are test hooks. forceRerender makes walk
// re-render a node it could have spliced; rerenderMangle rewrites a
// rendering, so a test can make one fail to read back.
var (
	forceRerender  func(n *yaml.Node) bool
	rerenderMangle func(level, text string) string
)

// errUnwritable is an edit whose document did not read back as edited even
// when re-rendered whole. Nothing is written.
var errUnwritable = errors.New("the edited document does not read back as the edited tree; nothing was written")

func unwritable(doc int, cause error) error {
	if cause != nil {
		// The cause is text, not wrapped: it is the decline that ended the
		// ladder, and the error must not read as one.
		return fmt.Errorf("document %d: %w (%s)", doc, errUnwritable, cause.Error())
	}
	return fmt.Errorf("document %d: %w", doc, errUnwritable)
}

// rerender re-renders n at the smallest level it can stand alone at, or
// declines so the change escalates to n's parent.
func (s *splicer) rerender(n, parent, key *yaml.Node, flow bool) error {
	switch {
	case n.Kind == yaml.DocumentNode:
		return s.rerenderDoc(n)
	case flow:
		return decline("a node inside a flow collection")
	case n.Kind != yaml.MappingNode && n.Kind != yaml.SequenceNode:
		return decline("a scalar cannot stand alone")
	case ownChanged(s.snap[n], n):
		return decline("a collection whose own fields changed")
	case n.Style&yaml.FlowStyle != 0:
		return s.rerenderFlow(n)
	case key != nil && parent != nil && parent.Kind == yaml.MappingNode:
		return s.rerenderUnderKey(n, parent, key)
	}
	return decline("a collection that cannot stand alone")
}

// rerenderFlow re-renders the outermost flow collection n over its source
// text, which may run over several lines.
func (s *splicer) rerenderFlow(n *yaml.Node) error {
	old := s.old(n)
	loc, _, err := s.flowSpan(old)
	if err != nil {
		return err
	}
	cp := s.prepare(n, s.t.src[loc.start:loc.end], true)
	cp.Style |= yaml.FlowStyle
	lines, err := s.encode(cp)
	if err != nil {
		return err
	}
	if !readsBack(lines, cp) {
		return decline("a flow collection's rendering does not read back")
	}
	text := lines[0]
	if len(lines) > 1 {
		// Continuation lines need only be indented past the line the
		// collection opens on.
		text += s.eol + placeLines(lines[1:], indentOf(s.t.text(old.Line)), s.eol)
	}
	s.addLevel(loc.start, loc.end, text, "flow")
	return nil
}

// rerenderUnderKey re-renders the block collection n, key's value, from the
// end of the key's line through the entry's last line. The key line stays as
// written, its properties and comment included.
func (s *splicer) rerenderUnderKey(n, parent, key *yaml.Node) error {
	po, ok := s.snap[parent]
	if !ok || parent.Style&yaml.FlowStyle != 0 {
		return decline("a parent the splice cannot read")
	}
	ki := -1
	for i := 0; i+1 < len(po.content); i += 2 {
		if po.content[i] == key && po.content[i+1] == n {
			ki = i
		}
	}
	if ki < 0 {
		return decline("a value whose key moved")
	}
	if _, err := s.keyColon(key); err != nil {
		return err
	}
	oldN, oldP := s.old(n), s.old(parent)
	if len(oldN.Content) == 0 || oldN.Content[0].Line <= key.Line {
		return decline("a collection that starts on its key's line")
	}
	col := oldN.Content[0].Column - 1
	if n.Kind == yaml.SequenceNode {
		_, ind, err := s.entryStart(oldN.Content[0], false)
		if err != nil {
			return err
		}
		if ind < 0 {
			return decline("a compact first item")
		}
		col = ind
	}
	last, err := s.entryEnd(oldP, oldP.Content, ki, 2, key.Column-1)
	if err != nil {
		return err
	}
	start, end := s.t.ends[key.Line-1], s.t.ends[last-1]
	cp := s.prepare(n, s.t.src[start:end], false)
	// The anchor and tag are on the key's line, which stays.
	cp.Anchor, cp.Tag = "", "!!map"
	cp.Style &^= yaml.TaggedStyle
	if cp.Kind == yaml.SequenceNode {
		cp.Tag = "!!seq"
	}
	lines, err := s.encode(cp)
	if err != nil {
		return err
	}
	if !readsBack(lines, cp) {
		return decline("a block collection's rendering does not read back")
	}
	s.addLevel(start, end, s.eol+placeLines(lines, col, s.eol), "block")
	return nil
}

// rerenderDoc re-renders a document's root. The span runs from the line after
// the "---" (or the start of the stream, or the end of the previous
// document's "..."), or from the root itself when it shares the marker's
// line, through the last line that is not blank before the next marker. The
// markers, the blank lines beside them and the end of the stream are outside
// it.
func (s *splicer) rerenderDoc(doc *yaml.Node) error {
	if len(doc.Content) != 1 {
		return decline("a document without one root")
	}
	start, end, tail, onMarker, err := s.docRegion(s.old(doc).Content[0])
	if err != nil {
		return err
	}
	cp := s.prepare(doc, s.t.src[start:end], false)
	lines, err := s.encode(cp)
	if err != nil {
		return err
	}
	if !readsBack(lines, cp) {
		return decline("a document's rendering does not read back")
	}
	text := placeLines(lines, 0, s.eol) + strings.Repeat(s.eol, tail)
	switch {
	case onMarker && len(lines) == 1:
		text = " " + text
	case onMarker:
		text = s.eol + text
	case start == end && start == len(s.t.src) && start > 0 && !s.endsInBreak():
		text = s.eol + text
	case start == end:
		text += s.eol
	}
	if rerenderMangle != nil {
		text = rerenderMangle("doc", text)
	}
	s.edits = append(s.edits, edit{start: start, end: end, text: text, depth: 0, doc: s.doc, level: "doc"})
	s.docLevel[s.doc] = true
	return nil
}

// docRegion finds the span rerenderDoc replaces for a document whose root,
// before the edit, was root. tail is how many blank lines at its end the span
// takes, to be written back as empty lines.
func (s *splicer) docRegion(root *yaml.Node) (start, end, tail int, onMarker bool, err error) {
	n := s.t.n()
	isSep := func(l int) bool {
		b := string(s.t.text(l))
		return isDocStart(b) || isDocEnd(b)
	}
	r := root.Line
	empty := root.Kind == yaml.ScalarNode && root.Tag == "!!null" && root.Value == "" && root.Style == 0
	// yaml.v3 puts an empty root on the line after a last line with no break.
	if r < 1 || r > n+1 || (r == n+1 && !empty) {
		return 0, 0, 0, false, decline("a root outside the source")
	}
	var first int // the first line of the span
	switch {
	case !empty && isDocStart(string(s.t.text(r))):
		onMarker = true
		start = s.t.starts[r-1] + 3
		first = r
	default:
		p := r - 1
		for p >= 1 && !isSep(p) {
			p--
		}
		if p >= 1 {
			start = s.t.brk[p-1]
		}
		first = p + 1
	}
	from := first
	if !empty {
		from = lastLine(root) + 1
	}
	next := n + 1
	for l := max(from, first); l <= n; l++ {
		if isSep(l) && (l > first || !onMarker) {
			next = l
			break
		}
	}
	last := next - 1
	for last >= first && isBlankText(s.t.text(last)) && !s.keepBlank[last] {
		last--
	}
	end = start
	if last >= first {
		end = s.t.ends[last-1]
	}
	// White space on the blank lines after the root is not always
	// insignificant to yaml.v3: "[]" followed by a line holding a tab does not
	// parse, where "- 0" followed by it does. Those lines join the span and are
	// written back empty.
	for l := max(last, first-1) + 1; l < next; l++ {
		if len(s.t.text(l)) > 0 && last >= first {
			tail = next - 1 - last
			end = s.t.ends[next-2]
			break
		}
	}
	if end < start {
		return 0, 0, 0, false, decline("an empty document region")
	}
	return start, end, tail, onMarker, nil
}

func (s *splicer) endsInBreak() bool {
	n := s.t.n()
	return n > 1 && s.t.starts[n-1] == len(s.t.src)
}

// old is n as it was before the edit: its fields and children from the
// snapshot, so positions and extents are measured on what the source holds.
func (s *splicer) old(n *yaml.Node) *yaml.Node {
	if c, ok := s.olds[n]; ok {
		return c
	}
	sh, ok := s.snap[n]
	if !ok {
		return n
	}
	c := &yaml.Node{Kind: sh.kind, Tag: sh.tag, Value: sh.value, Anchor: sh.anchor, Style: sh.style,
		Alias: sh.alias, Line: sh.row, Column: sh.col, HeadComment: sh.head, LineComment: sh.line, FootComment: sh.foot}
	s.olds[n] = c
	for _, ch := range sh.content {
		c.Content = append(c.Content, s.old(ch))
	}
	return c
}

// prepare copies n for the encoder. A comment on a node the source holds
// whose text is not inside span lives in source that stays, and is dropped.
// The guards the encoder needs run on the copy, never on the tree.
func (s *splicer) prepare(n *yaml.Node, span []byte, inFlow bool) *yaml.Node {
	cp := s.copyNode(n, span)
	normalizeFlowNulls(cp, inFlow && n.Kind == yaml.ScalarNode)
	guardEncodedScalars([]*yaml.Node{cp}, s.indent)
	quoteTrailingKeep(cp)
	return cp
}

// quoteTrailingKeep double-quotes the last scalar of a rendering when its
// value ends in more than one line break. The encoder would write it as a
// keep-chomped block, which takes every blank line that follows as content —
// and what follows a rendering is the source's, blank lines included.
func quoteTrailingKeep(n *yaml.Node) {
	for len(n.Content) > 0 {
		n = n.Content[len(n.Content)-1]
	}
	if n.Kind == yaml.ScalarNode && strings.HasSuffix(n.Value, "\n\n") {
		n.Style = yaml.DoubleQuotedStyle
	}
}

func (s *splicer) copyNode(n *yaml.Node, span []byte) *yaml.Node {
	c := *n
	if _, orig := s.snap[n]; orig && span != nil {
		if !commentIn(c.HeadComment, span) {
			c.HeadComment = ""
		}
		if !commentIn(c.LineComment, span) {
			c.LineComment = ""
		}
		if !commentIn(c.FootComment, span) {
			c.FootComment = ""
		}
	}
	c.Content = nil
	for _, ch := range n.Content {
		c.Content = append(c.Content, s.copyNode(ch, span))
	}
	return &c
}

// commentIn reports whether every line of comment c appears in span.
func commentIn(c string, span []byte) bool {
	for _, l := range strings.Split(c, "\n") {
		if l = strings.TrimSpace(l); l != "" && !bytes.Contains(span, []byte(l)) {
			return false
		}
	}
	return true
}

// encode is the encoder's text for n at the edit's indent, split into lines.
func (s *splicer) encode(n *yaml.Node) ([]string, error) {
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(s.indent)
	if err := e.Encode(n); err != nil {
		return nil, decline("encode: %v", err)
	}
	if err := e.Close(); err != nil {
		return nil, decline("encode: %v", err)
	}
	// Every line break at the end goes: none is content, since a scalar that
	// would end in a keep-chomped block is quoted (quoteTrailingKeep), and
	// yaml.v3 writes a blank line after a folded one that ends a stream.
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n"), nil
}

// readsBack reports whether lines decode to cp. A rendering holding an alias
// to an anchor outside it cannot be read on its own; the document's
// verification is its check.
func readsBack(lines []string, cp *yaml.Node) bool {
	var g yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")+"\n"), &g); err != nil {
		return strings.Contains(err.Error(), "unknown anchor")
	}
	want := cp
	if cp.Kind != yaml.DocumentNode {
		want = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{cp}}
	}
	return nodeEqual(&g, want)
}

// placeLines indents every line that is not empty by col and joins them with
// eol. An empty line stays empty: inside a block scalar, white space on it
// would be content or a stray trailing run.
func placeLines(lines []string, col int, eol string) string {
	return strings.Join(indentNonEmpty(lines, col), eol)
}

func indentNonEmpty(lines []string, col int) []string {
	pad := strings.Repeat(" ", col)
	out := make([]string, len(lines))
	for i, l := range lines {
		if l != "" {
			l = pad + l
		}
		out[i] = l
	}
	return out
}

// valueLines is n's text as a value: head goes after the key's colon or the
// item's dash, body is the lines below, relative to the key's or the dash's
// column. The splice's renderer writes it when it can, which keeps the
// source's sequence style; the encoder writes what it declines. n's own
// comments are left out: they are the replaced value's, and stay in source.
func (s *splicer) valueLines(n *yaml.Node, item bool) (head string, body []string, err error) {
	if checkNew(n, true) == nil {
		if lines, inline, err := s.render(n); err == nil {
			switch {
			case inline:
				return lines[0], nil, nil
			case item:
				return lines[0], indentLines(lines[1:], 2), nil
			default:
				return "", indentLines(lines, s.childIndent(n)), nil
			}
		}
	}
	cp := s.prepare(n, nil, false)
	cp.HeadComment, cp.LineComment, cp.FootComment = "", "", ""
	wrap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "k"}, cp}}
	lead := "k:"
	if item {
		wrap = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{cp}}
		lead = "-"
	}
	lines, err := s.encode(wrap)
	if err != nil {
		return "", nil, err
	}
	if !strings.HasPrefix(lines[0], lead) {
		return "", nil, decline("an encoding that does not lead with %q", lead)
	}
	return strings.TrimSpace(lines[0][len(lead):]), lines[1:], nil
}

// entryLines is a new entry's text, relative to its key's or dash's column:
// the splice's renderer first, the encoder for what it declines.
func (s *splicer) entryLines(k, v *yaml.Node) ([]string, error) {
	isPair := v != nil
	if checkNew(k, false) == nil && (!isPair || checkNew(v, false) == nil) {
		var ls []string
		var err error
		if isPair {
			ls, err = s.renderPair(k, v)
		} else {
			ls, err = s.renderItem(k)
		}
		if err == nil {
			return ls, nil
		}
	}
	wrap := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{s.prepare(k, nil, false)}}
	if isPair {
		wrap = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{s.prepare(k, nil, false), s.prepare(v, nil, false)}}
	}
	return s.encode(wrap)
}
