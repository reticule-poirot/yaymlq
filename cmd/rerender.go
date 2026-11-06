package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
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
	cp := s.prepare(n, loc.start, s.t.src[loc.start:loc.end], true)
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
	cp := s.prepare(n, start, s.t.src[start:end], false)
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
	cp := s.prepare(doc, start, s.t.src[start:end], false)
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
	case end == len(s.t.src) && tail == 0 && !s.endsInBreak() && !readsBackExact(lines, cp):
		// The region replaced runs to the true end of the source, which
		// has no trailing line break for the rendering's last line to
		// rely on, and that line turns out to need one — a block
		// scalar's last line is only its value when a break ends it
		// (#353). Most renderings don't (an ordinary scalar or a short
		// collection is a complete value without one), which is exactly
		// what readsBackExact just ruled out for those.
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
	// yaml.v4 puts an empty root on the line after a last line with no break.
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
	// insignificant to yaml.v4: "[]" followed by a line holding a tab does not
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
// whose text is not inside span, which starts at offset start, lives in source
// that stays, and is dropped. The guards the encoder needs run on the copy, never
// on the tree.
func (s *splicer) prepare(n *yaml.Node, start int, span []byte, inFlow bool) *yaml.Node {
	cp := s.copyNode(n, start, span)
	normalizeFlowNulls(cp, inFlow && n.Kind == yaml.ScalarNode)
	guardEncodedScalars([]*yaml.Node{cp}, s.indent)
	quoteTrailingKeep(cp)
	return cp
}

// quoteTrailingKeep double-quotes the last scalar of a rendering when its
// value ends in more than one line break or is a lone one. The encoder would
// write it as a keep-chomped block, which takes every blank line that follows
// as content — and what follows a rendering is the source's, blank lines
// included.
func quoteTrailingKeep(n *yaml.Node) {
	for len(n.Content) > 0 {
		n = n.Content[len(n.Content)-1]
	}
	if n.Kind == yaml.ScalarNode && (n.Value == "\n" || strings.HasSuffix(n.Value, "\n\n")) {
		n.Style = yaml.DoubleQuotedStyle
	}
}

func (s *splicer) copyNode(n *yaml.Node, start int, span []byte) *yaml.Node {
	c := *n
	if sh, orig := s.snap[n]; orig && span != nil {
		c.HeadComment = s.headInSpan(c.HeadComment, sh.row, start)
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
		c.Content = append(c.Content, s.copyNode(ch, start, span))
	}
	return &c
}

// headInSpan is head, the head comment of the node on line row, without the
// lines that lie before a span starting at offset start. The parser joins the
// comments on both sides of a "---" into one head comment on the first key, so
// only the end of it may be the span's (#394), and the same text can be on both
// sides, so the lines are matched against the source above the span, in order.
// Only the span's first node can have such lines.
func (s *splicer) headInSpan(head string, row, start int) string {
	if head == "" || row < 1 {
		return head
	}
	for l := min(row-1, s.t.n()); l >= 1 && s.t.starts[l-1] >= start; l-- {
		if text := bytes.TrimSpace(s.t.text(l)); len(text) > 0 && text[0] != '#' {
			return head
		}
	}
	// The line holding start is above the span unless the span starts with it.
	last := s.t.lineOf(start)
	if last >= 1 && last <= s.t.n() && s.t.starts[last-1] == start {
		last--
	}
	above := s.commentsAbove(last)
	lines := strings.Split(head, "\n")
	for n := min(len(above), len(lines)); n > 0; n-- {
		if slices.Equal(above[len(above)-n:], nonBlankPrefix(lines, n)) {
			for i, seen := 0, 0; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) != "" {
					seen++
				}
				if seen == n {
					return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
				}
			}
		}
	}
	return head
}

// commentsAbove lists the comments on line last and the lines above it, top
// first, as far up as the document's own content: own-line comments, and one on a
// "---" marker, past blank lines and directives.
func (s *splicer) commentsAbove(last int) []string {
	var out []string
	for l := min(last, s.t.n()); l >= 1; l-- {
		text := string(bytes.TrimSpace(s.t.text(l)))
		switch {
		case text == "" || text[0] == '%':
		case text[0] == '#':
			out = append(out, text)
		case isDocStart(text):
			if i := strings.Index(text, "#"); i >= 0 {
				out = append(out, text[i:])
			}
		default:
			slices.Reverse(out)
			return out
		}
	}
	slices.Reverse(out)
	return out
}

// nonBlankPrefix is the first n lines of c that aren't blank, trimmed.
func nonBlankPrefix(c []string, n int) []string {
	out := make([]string, 0, n)
	for _, l := range c {
		if l = strings.TrimSpace(l); l != "" && len(out) < n {
			out = append(out, l)
		}
	}
	return out
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
	if err := encodeYAML(&b, n, s.indent); err != nil {
		return nil, decline("encode: %v", err)
	}
	// Every line break at the end goes: none is content, since a scalar that
	// would end in a keep-chomped block is quoted (quoteTrailingKeep), and
	// yaml.v4 writes a blank line after a folded one that ends a stream.
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n"), nil
}

// readsBack reports whether lines decode to cp. A rendering holding an alias
// to an anchor outside it cannot be read on its own; the document's
// verification is its check.
func readsBack(lines []string, cp *yaml.Node) bool {
	return decodesToNode(strings.Join(lines, "\n")+"\n", cp)
}

// readsBackExact is readsBack without the trailing line break readsBack
// always adds before checking. rerenderDoc uses it to tell whether the text
// it is about to write — which, at the true end of a source with no break
// of its own, carries none either unless this says it must — actually needs
// one: a block scalar's last line is only its value when a break ends it,
// so a rendering ending in one reads back one line short without it (#353).
// Most renderings don't need it (an ordinary scalar or a short collection
// is a complete value without a trailing break), so this is only consulted
// where that matters, not added unconditionally.
func readsBackExact(lines []string, cp *yaml.Node) bool {
	return decodesToNode(strings.Join(lines, "\n"), cp)
}

func decodesToNode(text string, cp *yaml.Node) bool {
	var g yaml.Node
	if err := yaml.Unmarshal([]byte(text), &g); err != nil {
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
	return indent(lines, col, true)
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
	cp := s.prepare(n, 0, nil, false)
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
	wrap := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{s.prepare(k, 0, nil, false)}}
	if isPair {
		wrap = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{s.prepare(k, 0, nil, false), s.prepare(v, 0, nil, false)}}
	}
	return s.encode(wrap)
}

// guardEncodedScalars makes every scalar the encoder is about to render itself
// come out holding the value it holds in the tree. It runs on the final tree,
// after the masks, so what it sees is exactly what the encoder will: a block
// the splice covers is a one-line plain marker by then and passes untouched,
// and what is left is every block the splice declined — a header on its own
// line, a block used as a key, a record that did not verify — plus any value
// the edit itself wrote (#274).
//
// The encoder renders a multi-line string in block style and has got some of
// them wrong on its own: a sequence item that opened with a line break did not
// parse at --indent 4, a line opening with a tab came out unparseable. yaml.v4
// gets at least those two right. Rather than list what is left, each candidate is
// encoded alone and read back; one that does not survive is switched to
// double-quoted style, which the encoder renders exactly for every string.
func guardEncodedScalars(docs []*yaml.Node, indent int) {
	g := scalarGuard{indent: indent}
	for _, d := range docs {
		g.keep = false
		g.walk(d)
	}
}

// normalizeFlowNulls gives the value "null" to every empty !!null scalar inside
// a flow collection, so the encoder writes "null" where it would otherwise write
// an empty single-quoted string — a changed value, which is the one thing a re-render must not do
// (#263): say the thing explicitly in the tree, rather than argue with the
// encoder about it.
//
// Scoped to flow because block context is already right: the encoder writes an
// empty null as nothing there, and "key:" with no value is one of the most
// common lines in a YAML file, so normalising everywhere would put "null" all
// over documents that round-trip exactly today.
//
// One block-context case is not right, and it is a key: an empty null on the
// left of a colon — "? " with nothing after it, or an anchor with nothing after
// it — has no empty spelling the encoder can write there, so it wrote an empty
// quoted string and the mapping came back holding a different key (#273). Keys
// are normalised wherever they sit; values only in flow.
//
// It runs on the copy a re-render hands the encoder (prepare), never on the
// edited tree.
func normalizeFlowNulls(n *yaml.Node, inFlow bool) {
	if inFlow && n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == "" {
		n.Value = "null"
		return
	}
	below := inFlow || n.Style&yaml.FlowStyle != 0
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Kind == yaml.ScalarNode && c.Tag == "!!null" && c.Value == "" {
			c.Value = "null"
			continue
		}
		normalizeFlowNulls(c, below)
	}
}

type scalarGuard struct {
	indent int
	// keep is whether the last node emitted was a block scalar under keep
	// chomping, so a blank line above the next one would land in its value.
	keep bool
}

func (g *scalarGuard) walk(n *yaml.Node) {
	if g.keep && strings.HasPrefix(n.HeadComment, "\n") {
		n.HeadComment = strings.TrimLeft(n.HeadComment, "\n")
	}
	g.keep = false
	switch n.Kind {
	case yaml.ScalarNode:
		if !encodable(n) {
			return
		}
		if !encoderReproduces(n, g.indent) {
			n.Style = n.Style&^(yaml.LiteralStyle|yaml.FoldedStyle|yaml.SingleQuotedStyle) | yaml.DoubleQuotedStyle
			return
		}
		g.keep = rendersAsBlock(n) && strings.HasSuffix(n.Value, "\n\n")
	default:
		for _, c := range n.Content {
			g.walk(c)
		}
	}
}

// encodable reports whether n is a scalar the encoder may render in block
// style: one that asks for it, or a multi-line string it picks block style
// for unasked.
func encodable(n *yaml.Node) bool {
	return n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(n.Value, "\n")
}

// encoderReproduces encodes a copy of n in the three positions a block scalar
// can take — a mapping value, a sequence item, an explicit key — at the
// document's indent, and reports whether each reads back as n's value.
func encoderReproduces(n *yaml.Node, indent int) bool {
	leaf := func() *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: n.Tag, Value: n.Value, Style: n.Style &^ yaml.FlowStyle}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	probe := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		str("v"), leaf(),
		str("s"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{leaf()}},
		str("k"), {Kind: yaml.MappingNode, Content: []*yaml.Node{leaf(), {Kind: yaml.ScalarNode, Tag: "!!null", Value: "~"}}},
	}}
	var buf bytes.Buffer
	if encodeYAML(&buf, probe, indent) != nil {
		return false
	}
	var back yaml.Node
	if yaml.Unmarshal(buf.Bytes(), &back) != nil || len(back.Content) != 1 {
		return false
	}
	m := back.Content[0]
	if m.Kind != yaml.MappingNode || len(m.Content) != 6 {
		return false
	}
	got := []*yaml.Node{m.Content[1]}
	if s := m.Content[3]; s.Kind == yaml.SequenceNode && len(s.Content) == 1 {
		got = append(got, s.Content[0])
	}
	if k := m.Content[5]; k.Kind == yaml.MappingNode && len(k.Content) == 2 {
		got = append(got, k.Content[0])
	}
	if len(got) != 3 {
		return false
	}
	for _, g := range got {
		if g.Kind != yaml.ScalarNode || g.Value != n.Value {
			return false
		}
	}
	return true
}

// rendersAsBlock reports whether the encoder writes n as a block scalar.
func rendersAsBlock(n *yaml.Node) bool {
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.FlowStyle) != 0 {
		return false
	}
	return n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(n.Value, "\n")
}
