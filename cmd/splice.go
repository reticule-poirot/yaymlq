package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source splicing: an edit rewrites only the bytes it changed.
//
// The editing verbs used to re-encode the whole stream with yaml.v3 and then
// repair what the encoder lost, one fidelity pass per loss. The splice works
// the other way round: it finds what the edit changed (changeset.go), locates
// those nodes' bytes in the source, and replaces just them, so every byte the
// edit did not touch is copied — blank lines, comments, quoting, line breaks
// and block headers included, without a pass for any of them.
//
// It handles a narrow set of edits and declines everything else with
// errDecline, which sends applyEdit down the encoder pipeline as before:
//
//   - a scalar value replaced by a scalar, outside flow collections;
//   - a key renamed, when the key carries no anchor;
//   - a block mapping entry or sequence item deleted, writing `{}` or `[]` on
//     the key's line when it was the collection's only one.
//
// Every span is read back on its own before it is trusted, and the whole
// result is decoded and compared with the edited tree (spliceverify.go); a
// result that disagrees declines rather than ships.

var errDecline = errors.New("splice declined")

func decline(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errDecline}, a...)...)
}

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

type splicer struct {
	t     *lineTable
	snap  map[*yaml.Node]shallow
	edits []edit
}

// spliceEdit returns src with the changes the mutation made to docs since
// snap spliced in, or an error wrapping errDecline.
func spliceEdit(src []byte, docs []*yaml.Node, snap map[*yaml.Node]shallow) ([]byte, error) {
	out, _, err := splice(src, docs, snap)
	return out, err
}

// splice is spliceEdit, also returning the edits it made, for the tests that
// hold a splice to touching nothing outside them.
func splice(src []byte, docs []*yaml.Node, snap map[*yaml.Node]shallow) ([]byte, []edit, error) {
	// Node columns are counted in the decoded text; a BOM or a UTF-16
	// encoding puts the source's bytes somewhere else.
	for _, bom := range []string{"\xef\xbb\xbf", "\xff\xfe", "\xfe\xff"} {
		if bytes.HasPrefix(src, []byte(bom)) {
			return nil, nil, decline("byte order mark")
		}
	}
	s := &splicer{t: newLineTable(src), snap: snap}
	for _, d := range docs {
		if err := s.walk(d, nil, nil, false); err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(s.edits, func(i, j int) bool { return s.edits[i].start < s.edits[j].start })
	var out bytes.Buffer
	pos := 0
	for _, e := range s.edits {
		if e.start < pos || e.end > len(src) || e.start > e.end {
			return nil, nil, decline("overlapping spans")
		}
		out.Write(src[pos:e.start])
		out.WriteString(e.text)
		pos = e.end
	}
	out.Write(src[pos:])
	res := out.Bytes()
	if !sameAsTree(res, docs) {
		return nil, nil, decline("the spliced output does not decode to the edited tree")
	}
	return res, s.edits, nil
}

// walk visits n, whose parent is parent and, when n is a mapping value, whose
// key is key. flow is whether n sits inside a flow collection.
func (s *splicer) walk(n, parent, key *yaml.Node, flow bool) error {
	old, known := s.snap[n]
	if !known {
		return decline("a node the edit brought in")
	}
	inFlow := flow || n.Style&yaml.FlowStyle != 0
	if ownChanged(old, n) {
		if err := s.renamed(n, parent, old, flow); err != nil {
			return err
		}
	}
	if contentChanged(old, n) {
		if err := s.container(n, key, old, inFlow); err != nil {
			return err
		}
	}
	for i, c := range n.Content {
		if _, ok := s.snap[c]; !ok {
			continue // brought in by the edit, and container has handled it
		}
		var k *yaml.Node
		if n.Kind == yaml.MappingNode && i%2 == 1 {
			k = n.Content[i-1]
		}
		if err := s.walk(c, n, k, inFlow); err != nil {
			return err
		}
	}
	return nil
}

// renamed handles a node whose own fields changed, which a verb does only to a
// key it renames.
func (s *splicer) renamed(n, parent *yaml.Node, old shallow, flow bool) error {
	if flow {
		return decline("a change inside a flow collection")
	}
	if n.Kind != yaml.ScalarNode || old.kind != yaml.ScalarNode || parent == nil || parent.Kind != yaml.MappingNode {
		return decline("an in-place change to a node that is not a key")
	}
	isKey := false
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i] == n {
			isKey = true
		}
	}
	if !isKey {
		return decline("an in-place change to a value")
	}
	if old.anchor != "" {
		return decline("an anchored key")
	}
	was := *n
	was.Kind, was.Tag, was.Value, was.Anchor, was.Style = old.kind, old.tag, old.value, old.anchor, old.style
	start, end, err := s.scalarSpan(&was)
	if err != nil {
		return err
	}
	text, err := renderScalar(n, true)
	if err != nil {
		return err
	}
	s.edits = append(s.edits, edit{start, end, text})
	return nil
}

// container handles a collection whose child list changed: one value
// replaced, or one entry removed. key is the collection's own key when it is
// a mapping value.
func (s *splicer) container(n, key *yaml.Node, old shallow, flow bool) error {
	if flow {
		return decline("a change inside a flow collection")
	}
	step := 1
	switch n.Kind {
	case yaml.MappingNode:
		step = 2
	case yaml.SequenceNode:
	default:
		return decline("a content change to a %v", n.Kind)
	}
	if len(old.content) == len(n.Content) {
		changed := -1
		for i := range n.Content {
			if old.content[i] == n.Content[i] {
				continue
			}
			if (step == 2 && i%2 == 0) || changed >= 0 {
				return decline("more than one value replaced")
			}
			changed = i
		}
		return s.replaceValue(old.content[changed], n.Content[changed])
	}
	if len(old.content) == len(n.Content)+step {
		for i := 0; i < len(old.content); i += step {
			rest := append(append([]*yaml.Node(nil), old.content[:i]...), old.content[i+step:]...)
			if samePtrs(rest, n.Content) {
				return s.deleteEntry(n, key, old.content, i, step)
			}
		}
	}
	return decline("a collection change other than a replace or a delete")
}

// replaceValue splices newN's one-line rendering over oldN's source span.
func (s *splicer) replaceValue(oldN, newN *yaml.Node) error {
	if oldN.Kind != yaml.ScalarNode || newN.Kind != yaml.ScalarNode {
		return decline("a replace that is not scalar for scalar")
	}
	// ymledit carries the old value's comments onto the new one; anything
	// else is a comment the value brought, which the span cannot hold.
	if newN.LineComment != oldN.LineComment || newN.HeadComment != oldN.HeadComment || newN.FootComment != oldN.FootComment {
		return decline("the value brings its own comment")
	}
	start, end, err := s.scalarSpan(oldN)
	if err != nil {
		return err
	}
	text, err := renderScalar(newN, false)
	if err != nil {
		return err
	}
	s.edits = append(s.edits, edit{start, end, text})
	return nil
}

// renderScalar is the encoder's spelling of n alone, on one line and without
// its comments.
func renderScalar(n *yaml.Node, key bool) (string, error) {
	c := *n
	c.HeadComment, c.LineComment, c.FootComment = "", "", ""
	c.Line, c.Column = 0, 0
	if c.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		c.Style = 0
	}
	b, err := yaml.Marshal(&c)
	if err != nil {
		return "", decline("render: %v", err)
	}
	text := strings.TrimSuffix(string(b), "\n")
	if strings.ContainsAny(text, "\n\r") {
		return "", decline("a rendering over more than one line")
	}
	// The encoder writes a key longer than this in explicit "? " form, which
	// one span in the key's place cannot hold.
	if key && len(text) > 1000 {
		return "", decline("a key too long for implicit form")
	}
	return text, nil
}

// scalarSpan finds the source bytes of a one-line scalar, its anchor and tag
// included, and reads them back to check they are n.
func (s *splicer) scalarSpan(n *yaml.Node) (int, int, error) {
	start, err := s.t.offset(n.Line, n.Column)
	if err != nil {
		return 0, 0, err
	}
	src := s.t.src
	lineEnd := s.t.ends[n.Line-1]
	p := start
	for p < lineEnd && (src[p] == '&' || src[p] == '!') {
		if src[p] == '!' && p+1 < lineEnd && src[p+1] == '<' {
			for p < lineEnd && src[p] != '>' {
				p++
			}
		}
		for p < lineEnd && src[p] != ' ' && src[p] != '\t' {
			p++
		}
		for p < lineEnd && (src[p] == ' ' || src[p] == '\t') {
			p++
		}
	}
	end := -1
	switch {
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return 0, 0, decline("a block scalar")
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if p >= lineEnd || src[p] != '"' {
			return 0, 0, decline("no opening quote")
		}
		for q := p + 1; q < lineEnd; q++ {
			if src[q] == '\\' {
				q++
				continue
			}
			if src[q] == '"' {
				end = q + 1
				break
			}
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		if p >= lineEnd || src[p] != '\'' {
			return 0, 0, decline("no opening quote")
		}
		for q := p + 1; q < lineEnd; q++ {
			if src[q] == '\'' {
				if q+1 < lineEnd && src[q+1] == '\'' {
					q++
					continue
				}
				end = q + 1
				break
			}
		}
	default:
		q := p
		for q < lineEnd {
			ch := src[q]
			if ch == '#' && q > p && (src[q-1] == ' ' || src[q-1] == '\t') {
				break
			}
			if ch == ':' && (q+1 >= lineEnd || src[q+1] == ' ' || src[q+1] == '\t') {
				break
			}
			q++
		}
		for q > p && (src[q-1] == ' ' || src[q-1] == '\t') {
			q--
		}
		end = q
	}
	if end < 0 {
		return 0, 0, decline("a quoted scalar that runs past its line")
	}
	span := src[start:end]
	var got yaml.Node
	if err := yaml.Unmarshal(span, &got); err != nil || got.Kind != yaml.DocumentNode || len(got.Content) != 1 {
		return 0, 0, decline("the span %q does not read back", span)
	}
	g := got.Content[0]
	wantTag := n.Tag
	if wantTag == "" || wantTag == "!!merge" {
		wantTag = g.Tag
	}
	if g.Kind != yaml.ScalarNode || g.Value != n.Value || g.Tag != wantTag || g.Anchor != n.Anchor {
		return 0, 0, decline("the span %q is not the node", span)
	}
	if _, ok := decodeOne(span); !ok {
		return 0, 0, decline("the span %q is not one document", span)
	}
	return start, end, nil
}

// deleteEntry removes block mapping pair i (the content index of its key) or
// block sequence item i from old, the collection's children before the edit,
// together with the comment lines directly above it.
func (s *splicer) deleteEntry(parent, key *yaml.Node, old []*yaml.Node, i, step int) error {
	isMap := step == 2
	start, indent, err := s.entryStart(old[i], isMap)
	if err != nil {
		return err
	}
	if i+step < len(old) {
		next, nextIndent, err := s.entryStart(old[i+step], isMap)
		if err != nil {
			return err
		}
		if indent < 0 {
			// A compact first entry ("- a: 1"): pull the next one up onto
			// this line.
			nextOff, err := s.t.offset(old[i+step].Line, old[i+step].Column)
			if err != nil {
				return err
			}
			if !isMap || old[i+step].Column != old[i].Column || next.line != old[i+step].Line {
				return decline("a compact entry with a sibling unlike it")
			}
			s.edits = append(s.edits, edit{start.off, nextOff, ""})
			return nil
		}
		if nextIndent != indent {
			return decline("siblings at different indents")
		}
		s.edits = append(s.edits, edit{start.off, next.off, ""})
		return nil
	}
	if !start.lineStart {
		return decline("a last entry that does not start its line")
	}
	empty := len(old) == step
	var empt edit
	if empty {
		if empt, err = s.emptyMarker(parent, key, start.line); err != nil {
			return err
		}
	}
	// The last entry runs through every line indented past it, or, for an
	// indentless sequence value, through its dash lines.
	last := lastLine(old[i])
	if isMap {
		last = max(last, lastLine(old[i+1]))
	}
	// yaml.v3 can place an empty node on the line after the last one.
	if last > s.t.n() {
		return decline("an entry that ends past the source")
	}
	indentless := isMap && old[i+1].Kind == yaml.SequenceNode && old[i+1].Style&yaml.FlowStyle == 0 && old[i+1].Column == old[i].Column
	for l := last + 1; l <= s.t.n(); l++ {
		b := s.t.text(l)
		if isBlankText(b) {
			continue
		}
		ind := indentOf(b)
		if ind > indent || (indentless && ind == indent && b[ind] == '-') {
			last = l
			continue
		}
		break
	}
	// And from the blank lines above it, which separated it from what is left.
	first := start.line
	for first > 1 && isBlankText(s.t.text(first-1)) {
		first--
	}
	s.edits = append(s.edits, edit{s.t.starts[first-1], s.t.brk[last-1], ""})
	if empty {
		s.edits = append(s.edits, empt)
	}
	return nil
}

// emptyMarker is the edit that writes "{}" or "[]" after key's colon, for a
// collection whose only entry is being deleted: with the entry's lines gone,
// the key would otherwise be left holding null.
func (s *splicer) emptyMarker(coll, key *yaml.Node, entryLine int) (edit, error) {
	if key == nil {
		return edit{}, decline("emptying a collection that is not a mapping value")
	}
	if coll.Anchor != "" || (coll.Tag != "!!map" && coll.Tag != "!!seq") {
		return edit{}, decline("emptying a collection with properties")
	}
	if key.Line >= entryLine {
		return edit{}, decline("emptying a collection that starts on its key's line")
	}
	k := *key
	if snap, ok := s.snap[key]; ok {
		k.Value, k.Tag, k.Style, k.Anchor = snap.value, snap.tag, snap.style, snap.anchor
	}
	_, end, err := s.scalarSpan(&k)
	if err != nil {
		return edit{}, err
	}
	src, lineEnd := s.t.src, s.t.ends[key.Line-1]
	p := end
	for p < lineEnd && (src[p] == ' ' || src[p] == '\t') {
		p++
	}
	if p >= lineEnd || src[p] != ':' {
		return edit{}, decline("no colon after the key")
	}
	p++
	rest := bytes.TrimLeft(src[p:lineEnd], " \t")
	if len(rest) > 0 && rest[0] != '#' {
		return edit{}, decline("something after the key's colon")
	}
	marker := " {}"
	if coll.Kind == yaml.SequenceNode {
		marker = " []"
	}
	return edit{p, p, marker}, nil
}

type entryPos struct {
	off       int
	line      int
	lineStart bool
}

// entryStart finds where an entry begins — its key, or an item's dash — moved
// up over the comment lines directly above it at the same indent. The indent
// is -1 for an entry in compact form, which starts mid-line.
func (s *splicer) entryStart(n *yaml.Node, isMap bool) (entryPos, int, error) {
	off, err := s.t.offset(n.Line, n.Column)
	if err != nil {
		return entryPos{}, 0, err
	}
	src := s.t.src
	ls := s.t.starts[n.Line-1]
	if isMap {
		if bytes.Contains(src[ls:off], []byte("?")) {
			return entryPos{}, 0, decline("an explicit key")
		}
	} else {
		q := off - 1
		for q >= ls && src[q] == ' ' {
			q--
		}
		if q < ls || src[q] != '-' {
			return entryPos{}, 0, decline("an item whose dash is not on its line")
		}
		off = q
	}
	if len(bytes.Trim(src[ls:off], " ")) != 0 {
		return entryPos{off: off, line: n.Line}, -1, nil
	}
	indent := off - ls
	line := n.Line
	for line > 1 {
		b := s.t.text(line - 1)
		if !isBlankText(b) && indentOf(b) == indent && isCommentLine(b) {
			line--
			continue
		}
		break
	}
	return entryPos{off: s.t.starts[line-1], line: line, lineStart: true}, indent, nil
}

// lastLine is the last source line a node or anything under it starts on.
func lastLine(n *yaml.Node) int {
	l := n.Line
	for _, c := range n.Content {
		l = max(l, lastLine(c))
	}
	return l
}

// indentOf counts b's leading spaces.
func indentOf(b []byte) int {
	n := 0
	for n < len(b) && b[n] == ' ' {
		n++
	}
	return n
}

func isCommentLine(b []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(b, " \t"), []byte("#"))
}

func isBlankText(b []byte) bool { return len(bytes.Trim(b, " \t")) == 0 }
