package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Source splicing: an edit rewrites only the bytes it changed.
//
// The editing verbs used to re-encode the whole stream and then
// repair what the encoder lost, one fidelity pass per loss. The splice works
// the other way round: it finds what the edit changed (changeset.go), locates
// those nodes' bytes in the source, and replaces just them, so every byte the
// edit did not touch is copied — blank lines, comments, quoting, line breaks
// and block headers included, without a pass for any of them.
//
// It places these edits itself:
//
//   - a scalar value replaced by a scalar, outside flow collections;
//   - a key renamed, when the key carries no anchor;
//   - a block mapping entry or sequence item deleted, writing `{}` or `[]` on
//     the key's line when it was the collection's only one.
//   - entries added at the end of a block collection, or of a flow collection
//     on one line;
//   - a null turned into a collection, a value replaced by a collection, and a
//     block collection replaced by anything.
//
// Inserted text is rendered by splicerender.go, indented by --indent (or the
// source's detected width) relative to the collection it joins, and by the
// encoder where that declines. Anything else returns errDecline, and walk
// re-renders the smallest node around the change that can stand alone
// (rerender.go).
//
// Every span is read back on its own before it is trusted, and the whole
// result is decoded and compared with the edited tree, document by document
// (spliceverify.go). A document that disagrees is re-rendered whole; one that
// still disagrees fails the edit, and nothing is written.

var errDecline = errors.New("splice declined")

func decline(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errDecline}, a...)...)
}

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
	// depth orders two insertions at one offset: the deeper collection's
	// text goes first, since its entries end before its parent's next one.
	depth int
	// doc is the index of the document the edit is in, level the rung of the
	// re-render ladder that wrote it (rerender.go), "" for a splice.
	doc   int
	level string
}

type splicer struct {
	t     *lineTable
	lines []string
	snap  map[*yaml.Node]shallow
	edits []edit
	// keepBlank holds the source lines inside a block scalar's span that is
	// still in the tree: a blank one is the value's content, not spacing a
	// delete may take with it.
	keepBlank map[int]bool
	// indent is how far inserted text nests a block value, eol the line
	// ending it is written with, and indentless whether a block sequence
	// under a key sits at the key's column (detectIndentless).
	indent     int
	eol        string
	indentless bool
	// depth is the nesting of the node walk is visiting, stamped on its edits.
	depth   int
	lastDoc *yaml.Node
	// doc is the index of the document being walked; docLevel marks the
	// documents already re-rendered whole.
	doc      int
	docLevel map[int]bool
	olds     map[*yaml.Node]*yaml.Node
}

// add records an edit at the current depth, dropping one that does nothing.
func (s *splicer) add(start, end int, text string) {
	if start == end && text == "" {
		return
	}
	s.edits = append(s.edits, edit{start: start, end: end, text: text, depth: s.depth, doc: s.doc})
}

// addLevel records a re-rendered span.
func (s *splicer) addLevel(start, end int, text, level string) {
	if rerenderMangle != nil {
		text = rerenderMangle(level, text)
	}
	s.edits = append(s.edits, edit{start: start, end: end, text: text, depth: s.depth, doc: s.doc, level: level})
}

// scalarLoc is where a scalar sits in the source. A replace rewrites
// [start, end); a block scalar's body also has to go, which is [cut, cutEnd),
// kept separate so a comment on the header line between the two survives.
// cut is -1 for every other scalar.
type scalarLoc struct {
	start, end, cut, cutEnd int
}

// spliceEdit returns src with the changes the mutation made to docs since
// snap spliced in. indent is how far inserted text nests a block value. lines
// is src split by sourceLines, which applyEdit already did once for indent
// detection — passed in rather than split again here, the whole input split
// only once per edit instead of twice. An error wrapping errDecline is a
// source the splice cannot read at all (a byte order mark); errUnwritable is
// an edit whose document does not read back even re-rendered whole.
func spliceEdit(src []byte, lines []string, docs []*yaml.Node, snap map[*yaml.Node]shallow, indent int) ([]byte, error) {
	out, _, err := spliceLines(src, lines, docs, snap, indent)
	return out, err
}

// splice is spliceEdit, also returning the edits it made, for the tests that
// hold a splice to touching nothing outside them. It splits src itself,
// since a test calling it directly has no indent-detection split to reuse.
func splice(src []byte, docs []*yaml.Node, snap map[*yaml.Node]shallow, indent int) ([]byte, []edit, error) {
	return spliceLines(src, sourceLines(src), docs, snap, indent)
}

// spliceLines is splice, taking src's lines (sourceLines(src)) as a
// parameter instead of splitting src itself.
func spliceLines(src []byte, lines []string, docs []*yaml.Node, snap map[*yaml.Node]shallow, indent int) ([]byte, []edit, error) {
	// Node columns are counted in the decoded text; a BOM or a UTF-16
	// encoding puts the source's bytes somewhere else. trySplice already
	// sets the UTF-8 BOM aside before calling in from applyEdit's real
	// path, so this never fires there — it stays as the guard for any
	// caller (today, only tests) that passes src through unstripped.
	for _, bom := range []string{"\xef\xbb\xbf", "\xff\xfe", "\xfe\xff"} {
		if bytes.HasPrefix(src, []byte(bom)) {
			return nil, nil, decline("byte order mark")
		}
	}
	if len(docs) == 0 {
		return nil, nil, decline("no documents")
	}
	s := &splicer{t: newLineTable(src), lines: lines, snap: snap, keepBlank: map[int]bool{},
		indent: indent, eol: sourceLineEnding(src), indentless: detectIndentless(docs, snap), lastDoc: docs[len(docs)-1],
		docLevel: map[int]bool{}, olds: map[*yaml.Node]*yaml.Node{}}
	if s.eol == "" {
		s.eol = "\n"
	}
	for _, b := range recordBlockScalars(docs, s.lines) {
		for l := b.first; l <= b.last; l++ {
			s.keepBlank[l] = true
		}
	}
	for i, d := range docs {
		s.doc = i
		if err := s.walk(d, nil, nil, false, 0); err != nil {
			return nil, nil, unwritable(i, err)
		}
	}
	for round := 0; ; round++ {
		res, overlap := s.assemble()
		var bad []bool
		ok := !overlap
		if ok {
			bad, ok = verifyDocs(res, docs)
		}
		if !ok {
			// Nothing names the document at fault: every one with an edit
			// is suspect.
			bad = make([]bool, len(docs))
			for _, e := range s.edits {
				bad[e.doc] = true
			}
		}
		redo := false
		for i, b := range bad {
			if !b {
				continue
			}
			if s.docLevel[i] || round > 0 {
				return nil, nil, unwritable(i, nil)
			}
			redo = true
			kept := s.edits[:0]
			for _, e := range s.edits {
				if e.doc != i {
					kept = append(kept, e)
				}
			}
			s.edits = kept
			s.doc, s.depth = i, 0
			if err := s.rerenderDoc(docs[i]); err != nil {
				return nil, nil, unwritable(i, err)
			}
		}
		if !redo {
			if !ok {
				// No document was bad and yet the stream did not verify:
				// the edits are not to blame.
				return nil, nil, unwritable(0, nil)
			}
			return res, s.edits, nil
		}
	}
}

// assemble writes the source with the edits in place; overlap is two edits
// claiming the same bytes.
func (s *splicer) assemble() (res []byte, overlap bool) {
	sort.SliceStable(s.edits, func(i, j int) bool {
		a, b := s.edits[i], s.edits[j]
		if a.start != b.start {
			return a.start < b.start
		}
		return a.depth > b.depth
	})
	src := s.t.src
	var out bytes.Buffer
	pos := 0
	for _, e := range s.edits {
		if e.start < pos || e.end > len(src) || e.start > e.end {
			return nil, true
		}
		out.Write(src[pos:e.start])
		out.WriteString(e.text)
		pos = e.end
	}
	out.Write(src[pos:])
	return out.Bytes(), false
}

// walk visits n, whose parent is parent and, when n is a mapping value, whose
// key is key. flow is whether n sits inside a flow collection, depth how deep
// n is. A change the splice declines drops what n recorded and re-renders n
// (rerender.go); a node that cannot be re-rendered on its own passes the
// decline up to its parent.
func (s *splicer) walk(n, parent, key *yaml.Node, flow bool, depth int) error {
	mark := len(s.edits)
	err := s.visit(n, parent, key, flow, depth)
	forced := err == nil && forceRerender != nil && forceRerender(n)
	if err == nil && !forced {
		return nil
	}
	if err != nil && !errors.Is(err, errDecline) {
		return err
	}
	var spliced []edit
	if forced {
		spliced = slices.Clone(s.edits[mark:])
	}
	s.edits = s.edits[:mark]
	s.depth = depth
	if rerr := s.rerender(n, parent, key, flow); rerr != nil {
		if forced {
			// The hook asked for a re-render this node cannot have; the
			// splice stands. Its edits are kept rather than walked again,
			// which would double the work at every level of the subtree.
			s.edits = append(s.edits[:mark], spliced...)
			return nil
		}
		return rerr
	}
	return nil
}

func (s *splicer) visit(n, parent, key *yaml.Node, flow bool, depth int) error {
	old, known := s.snap[n]
	if !known {
		return decline("a node the edit brought in")
	}
	s.depth = depth
	inFlow := flow || n.Style&yaml.FlowStyle != 0
	if ownChanged(old, n) {
		// ymledit turns a null into a mapping in place, so the node keeps its
		// address and everything under it is new.
		if old.kind == yaml.ScalarNode && old.tag == "!!null" && n.Kind != yaml.ScalarNode {
			return s.nullToCollection(n, parent, key, old, flow)
		}
		if err := s.renamed(n, parent, old, flow); err != nil {
			return err
		}
	}
	if contentChanged(old, n) {
		if err := s.container(n, key, old, flow); err != nil {
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
		if err := s.walk(c, n, k, inFlow, depth+1); err != nil {
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
	loc, err := s.scalarSpan(&was, n.Column-1)
	if err != nil {
		return err
	}
	if loc.cut >= 0 {
		return decline("a block scalar key")
	}
	text, err := renderScalar(n, true)
	if err != nil {
		return err
	}
	s.add(loc.start, loc.end, text)
	return nil
}

// container handles a collection whose child list changed: one value
// replaced, one entry removed, or entries added at the end. key is the
// collection's own key when it is a mapping value; flow is whether the
// collection sits inside a flow collection.
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
	added := len(n.Content) > len(old.content) && samePtrs(old.content, n.Content[:len(old.content)])
	if n.Style&yaml.FlowStyle != 0 {
		if added {
			return s.flowInsert(n, n.Content[len(old.content):], step)
		}
		return decline("a change inside a flow collection")
	}
	if added {
		return s.insertEntries(n, old.content, n.Content[len(old.content):], step)
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
		return s.replaceValue(n, old.content, changed, step)
	}
	if len(old.content) == len(n.Content)+step {
		// Find where old and new first diverge, one entry at a time (step
		// is 1 or 2, so each comparison is O(1)) instead of rebuilding a
		// full copy of "the rest of old.content" for every candidate
		// deletion index to compare against n.Content in full — O(n)
		// instead of O(n²), with no allocation, for a delete near the end
		// of a large mapping or sequence (#331). Every entry before the
		// divergence is untouched, so it is the deleted entry's position
		// whenever the remainder past it still lines up with n.Content.
		i := 0
		for i < len(n.Content) && samePtrs(old.content[i:i+step], n.Content[i:i+step]) {
			i += step
		}
		if samePtrs(old.content[i+step:], n.Content[i:]) {
			return s.deleteEntry(n, key, old.content, i, step)
		}
	}
	return decline("a collection change other than a replace, a delete or an insertion")
}

// replaceValue splices the rendering of coll's new value i over old[i], the
// value it replaced.
func (s *splicer) replaceValue(coll *yaml.Node, old []*yaml.Node, i, step int) error {
	oldN, newN := old[i], coll.Content[i]
	// The indentation a value's continuation lines and block body are
	// measured against: its key's column, or its sequence's dash.
	parent := coll.Column - 1
	var key *yaml.Node
	if step == 2 {
		key = old[i-1]
		parent = key.Column - 1
	} else if _, ind, err := s.entryStart(oldN, false); err == nil && ind >= 0 {
		// A sequence reports the column of its anchor or tag when it has
		// one, not its dash's.
		parent = ind
	}
	// ymledit carries the old value's comments onto the new one; anything
	// else is a comment the value brought, which the span cannot hold.
	if newN.LineComment != oldN.LineComment || newN.HeadComment != oldN.HeadComment || newN.FootComment != oldN.FootComment {
		return decline("the value brings its own comment")
	}
	head, body, err := s.valueLines(newN, step == 1)
	if err != nil {
		return err
	}
	text := placeLines(body, parent, s.eol)
	switch {
	case oldN.Kind == yaml.ScalarNode || oldN.Kind == yaml.AliasNode || oldN.Style&yaml.FlowStyle != 0:
		var loc scalarLoc
		switch oldN.Kind {
		case yaml.ScalarNode:
			loc, err = s.scalarSpan(oldN, parent)
		case yaml.AliasNode:
			loc, err = s.aliasSpan(oldN)
		default:
			loc, _, err = s.flowSpan(oldN)
		}
		if err != nil {
			return err
		}
		if head == "" {
			// The value starts below its key: the colon ends the line.
			if key == nil {
				return decline("a value with nothing on its dash's line")
			}
			colon, err := s.keyColon(key)
			if err != nil {
				return err
			}
			s.add(colon, loc.end, "")
		} else {
			s.add(loc.start, loc.end, head)
		}
		switch {
		case loc.cut >= 0 && len(body) > 0:
			s.add(loc.cut, loc.cutEnd, s.eol+text)
		case loc.cut >= 0:
			s.add(loc.cut, loc.cutEnd, "")
		case len(body) > 0:
			at := s.t.ends[s.t.lineOf(loc.end)-1]
			s.add(at, at, s.eol+text)
		}
		return nil
	case oldN.Kind == yaml.MappingNode || oldN.Kind == yaml.SequenceNode:
		if oldN.Anchor != "" || (oldN.Tag != "!!map" && oldN.Tag != "!!seq") {
			return decline("a replaced collection with properties")
		}
		if key == nil {
			return s.replaceItem(coll, old, i, head, body)
		}
		colon, err := s.requireBareColon(key)
		if err != nil {
			return err
		}
		keyEnd := s.t.ends[key.Line-1]
		last, err := s.entryEnd(coll, old, i-1, step, key.Column-1)
		if err != nil {
			return err
		}
		if last <= key.Line {
			return decline("a block collection on its key's line")
		}
		if head != "" {
			s.add(colon, colon, " "+head)
		}
		if len(body) > 0 {
			text = s.eol + text
		}
		s.add(keyEnd, s.t.ends[last-1], text)
		return nil
	}
	return decline("replacing a %v", oldN.Kind)
}

// replaceItem replaces block sequence item i of old, a block collection, with
// head on its dash's line and body below it: everything after the dash
// through the item's last line goes.
func (s *splicer) replaceItem(coll *yaml.Node, old []*yaml.Node, i int, head string, body []string) error {
	_, indent, err := s.entryStart(old[i], false)
	if err != nil {
		return err
	}
	if indent < 0 || head == "" {
		return decline("a compact item replaced, or a value that does not start on the dash's line")
	}
	line, dash := old[i].Line, s.t.starts[old[i].Line-1]+indent
	if b := s.t.text(line); indentOf(b) != indent || b[indent] != '-' {
		if line, dash, err = s.dashAbove(old[i]); err != nil {
			return err
		}
	}
	if line != old[i].Line && !isBlankText(s.t.src[dash+1:s.t.ends[line-1]]) {
		return decline("a comment on a lone dash")
	}
	last, err := s.entryEnd(coll, old, i, 1, indent)
	if err != nil {
		return err
	}
	text := " " + head
	if len(body) > 0 {
		text += s.eol + placeLines(body, indent, s.eol)
	}
	s.add(dash+1, s.t.ends[last-1], text)
	return nil
}

// insertEntries writes added, new entries at the end of a block collection,
// below the last of old, its entries before the edit.
func (s *splicer) insertEntries(coll *yaml.Node, old, added []*yaml.Node, step int) error {
	if len(old) == 0 {
		return decline("an insertion into an empty block collection")
	}
	isMap := step == 2
	lastI := len(old) - step
	_, col, err := s.entryStart(old[lastI], isMap)
	if err != nil {
		return err
	}
	if isMap {
		col = old[lastI].Column - 1
	} else if col < 0 {
		return decline("an insertion after a compact item")
	}
	last, err := s.entryEnd(coll, old, lastI, step, col)
	if err != nil {
		return err
	}
	var lines []string
	for j := 0; j < len(added); j += step {
		var v *yaml.Node
		if isMap {
			v = added[j+1]
		}
		ls, err := s.entryLines(added[j], v)
		if err != nil {
			return err
		}
		lines = append(lines, ls...)
	}
	// Inserted after the last entry's own line break, not before it
	// (ends[last-1], the position its content ends and the break starts):
	// splicing the separator there would cut the break away from the line
	// it terminates and reattach it after the new entry instead — the same
	// bytes on a uniformly-ended source, but on one that mixes endings it
	// silently rewrote an untouched line's own terminator to s.eol (#327).
	// When the last entry has no break at all (no trailing newline), brk
	// and ends are the same position, and the leading separator below
	// supplies the one line break the file needs.
	hadBreak := s.t.brk[last-1] != s.t.ends[last-1]
	at := s.t.brk[last-1]
	text := placeLines(lines, col, s.eol)
	if hadBreak {
		text += s.eol
	} else {
		text = s.eol + text
	}
	s.add(at, at, text)
	return nil
}

// flowInsert writes added, new entries at the end of a flow collection that
// opens and closes on one line.
func (s *splicer) flowInsert(coll *yaml.Node, added []*yaml.Node, step int) error {
	loc, open, err := s.flowSpan(coll)
	if err != nil {
		return err
	}
	src := s.t.src
	if bytes.ContainsAny(src[open:loc.end], "\r\n") {
		return decline("an insertion into a flow collection over several lines")
	}
	p := loc.end - 1
	for p > open+1 && (src[p-1] == ' ' || src[p-1] == '\t') {
		p--
	}
	var parts []string
	for j := 0; j < len(added); j += step {
		var k, v *yaml.Node = added[j], nil
		if step == 2 {
			v = added[j+1]
		}
		if err := checkNew(k, false); err != nil {
			return err
		}
		if v != nil {
			if err := checkNew(v, false); err != nil {
				return err
			}
		}
		t, err := flowEntry(k, v)
		if err != nil {
			return err
		}
		parts = append(parts, t)
	}
	text := strings.Join(parts, ", ")
	switch {
	case p > open+1 && src[p-1] == ',': // a trailing comma
		text = " " + text
	case p > open+1:
		text = ", " + text
	}
	s.add(p, p, text)
	return nil
}

// flowSpan finds a flow collection, which may run over several lines: from
// its anchor or tag, or its opening bracket when it has neither, through its
// closing bracket. open is where the opening bracket is.
func (s *splicer) flowSpan(n *yaml.Node) (loc scalarLoc, open int, err error) {
	start, err := s.t.offset(n.Line, n.Column)
	if err != nil {
		return scalarLoc{}, 0, err
	}
	src, lineEnd := s.t.src, s.t.ends[n.Line-1]
	open = skipProperties(src, start, lineEnd)
	if open >= lineEnd || (src[open] != '[' && src[open] != '{') {
		return scalarLoc{}, 0, decline("a flow collection that does not start with its bracket")
	}
	ls := s.t.starts[n.Line-1]
	text, ok := flowExtent(s.lines, n.Line, open-ls+1)
	if !ok {
		return scalarLoc{}, 0, decline("a flow collection whose end cannot be found")
	}
	end := open + len(text[0])
	if k := len(text); k > 1 {
		end = s.t.starts[n.Line+k-2] + len(text[k-1])
	}
	return scalarLoc{start: start, end: end, cut: -1}, open, nil
}

// nullToCollection handles a null that the edit turned into a collection in
// place: the null's spelling goes, and the collection's text takes its place.
func (s *splicer) nullToCollection(n, parent, key *yaml.Node, old shallow, flow bool) error {
	if flow {
		return decline("a change inside a flow collection")
	}
	if old.anchor != "" {
		return decline("an anchored null")
	}
	render := func() ([]string, error) {
		if err := checkNew(n, true); err != nil {
			return nil, err
		}
		lines, _, err := s.render(n)
		return lines, err
	}
	switch {
	case parent != nil && parent.Kind == yaml.DocumentNode && old.value == "" && old.style == 0:
		lines, err := render()
		if err != nil {
			return err
		}
		return s.fillDocument(parent, lines)
	case parent != nil && parent.Kind == yaml.DocumentNode:
		was := yaml.Node{Kind: yaml.ScalarNode, Tag: old.tag, Value: old.value, Style: old.style, Line: old.row, Column: old.col}
		loc, err := s.scalarSpan(&was, -1)
		if err != nil {
			return err
		}
		if loc.cut >= 0 {
			return decline("a block scalar null")
		}
		lines, err := render()
		if err != nil {
			return err
		}
		s.add(loc.start, loc.end, strings.Join(lines, s.eol))
		return nil
	case key != nil:
		colon, err := s.keyColon(key)
		if err != nil {
			return err
		}
		src, lineEnd := s.t.src, s.t.ends[key.Line-1]
		p := colon
		for p < lineEnd && (src[p] == ' ' || src[p] == '\t') {
			p++
		}
		q := p
		for q < lineEnd && (src[q] != '#' || (q > p && src[q-1] != ' ' && src[q-1] != '\t')) {
			q++
		}
		for q > p && (src[q-1] == ' ' || src[q-1] == '\t') {
			q--
		}
		if q > p {
			var got yaml.Node
			if err := yaml.Unmarshal(src[p:q], &got); err != nil || len(got.Content) != 1 ||
				got.Content[0].Tag != "!!null" || got.Content[0].Anchor != "" {
				return decline("the null after the key does not read back")
			}
		} else {
			q = colon
		}
		head, body, err := s.valueLines(n, false)
		if err != nil {
			return err
		}
		if head != "" {
			head = " " + head
		}
		s.add(colon, q, head)
		if len(body) > 0 {
			s.add(lineEnd, lineEnd, s.eol+placeLines(body, key.Column-1, s.eol))
		}
		return nil
	}
	return decline("a null sequence item turned into a collection")
}

// fillDocument writes the root an edit gave an empty document. Its null has
// no spelling to replace, so the text can go only at the end of the source,
// which is why only the last document is filled here; any other is
// re-rendered whole (rerenderDoc).
func (s *splicer) fillDocument(doc *yaml.Node, lines []string) error {
	if doc != s.lastDoc {
		return decline("filling an empty document that is not the last")
	}
	text := strings.Join(lines, s.eol)
	end := len(s.t.src)
	if n := s.t.n(); n > 1 && s.t.starts[n-1] == end {
		text += s.eol
	} else {
		text = s.eol + text
	}
	s.add(end, end, text)
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
	b, err := marshalNode(&c)
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

// skipProperties advances p past any "&anchor"/"!tag" properties at the head
// of a node's span — scalarSpan's and flowSpan's shared prefix, since both a
// scalar and a flow collection can carry either, in either order, each
// followed by its own run of blank space. A verbatim tag ("!<...>") is
// walked to its '>' first, though the generic non-blank skip that follows
// would land in the same place: a tag URI has no literal space to stop on
// early.
func skipProperties(src []byte, p, lineEnd int) int {
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
	return p
}

// scalarSpan finds the source bytes of a scalar, its anchor and tag
// included, and reads them back to check they are n. parent is the
// indentation the scalar's continuation lines and block body must be indented
// past: its key's column, or its sequence's dash.
func (s *splicer) scalarSpan(n *yaml.Node, parent int) (scalarLoc, error) {
	start, err := s.t.offset(n.Line, n.Column)
	if err != nil {
		return scalarLoc{}, err
	}
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return s.blockSpan(n, start, parent)
	}
	src := s.t.src
	lineEnd := s.t.ends[n.Line-1]
	p := skipProperties(src, start, lineEnd)
	end := -1
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if p >= lineEnd || src[p] != '"' {
			return scalarLoc{}, decline("no opening quote")
		}
		for q := p + 1; q < len(src); q++ {
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
			return scalarLoc{}, decline("no opening quote")
		}
		for q := p + 1; q < len(src); q++ {
			if src[q] == '\'' {
				if q+1 < len(src) && src[q+1] == '\'' {
					q++
					continue
				}
				end = q + 1
				break
			}
		}
	default:
		end = s.plainEnd(p, lineEnd, n.Line, parent)
	}
	if end < 0 {
		return scalarLoc{}, decline("an unterminated quoted scalar")
	}
	span := src[start:end]
	var got yaml.Node
	if err := yaml.Unmarshal(span, &got); err != nil || got.Kind != yaml.DocumentNode || len(got.Content) != 1 {
		return scalarLoc{}, decline("the span %q does not read back", span)
	}
	g := got.Content[0]
	wantTag := n.Tag
	if wantTag == "" || wantTag == "!!merge" {
		wantTag = g.Tag
	}
	if g.Kind != yaml.ScalarNode || g.Value != n.Value || g.Tag != wantTag || g.Anchor != n.Anchor {
		return scalarLoc{}, decline("the span %q is not the node", span)
	}
	if _, ok := decodeOne(span); !ok {
		return scalarLoc{}, decline("the span %q is not one document", span)
	}
	return scalarLoc{start: start, end: end, cut: -1}, nil
}

// aliasSpan locates an alias's "*name" text in the source. Unlike a scalar,
// an alias can't carry its own anchor or tag and its name is never quoted,
// so its span is just "*" followed by its name, with nothing to cut: like a
// plain unquoted scalar, it is always exactly one line.
func (s *splicer) aliasSpan(n *yaml.Node) (scalarLoc, error) {
	start, err := s.t.offset(n.Line, n.Column)
	if err != nil {
		return scalarLoc{}, err
	}
	return scalarLoc{start: start, end: start + 1 + len(n.Value), cut: -1}, nil
}

// plainEnd is where a plain scalar starting at p on line ends. It runs to a
// " #" comment or a ": " indicator, and past the end of the line onto every
// continuation line below: one indented past parent that is not a comment or a
// document marker. Blank lines between continuations belong to the scalar
// (they fold to line breaks); blank lines after the last one do not.
func (s *splicer) plainEnd(p, lineEnd, line, parent int) int {
	src := s.t.src
	scan := func(p, lineEnd int) (end int, stopped bool) {
		q := p
		for q < lineEnd {
			ch := src[q]
			if ch == '#' && q > p && (src[q-1] == ' ' || src[q-1] == '\t') {
				stopped = true
				break
			}
			if ch == ':' && (q+1 >= lineEnd || src[q+1] == ' ' || src[q+1] == '\t') {
				stopped = true
				break
			}
			q++
		}
		for q > p && (src[q-1] == ' ' || src[q-1] == '\t') {
			q--
		}
		return q, stopped
	}
	end, stopped := scan(p, lineEnd)
	for l := line + 1; !stopped && l <= s.t.n(); l++ {
		b := s.t.text(l)
		if isBlankText(b) {
			continue
		}
		ind := indentOf(b)
		if ind <= parent || isCommentLine(b) || isDocStart(string(b)) || isDocEnd(string(b)) {
			break
		}
		end, stopped = scan(s.t.starts[l-1]+ind, s.t.ends[l-1])
	}
	return end
}

// blockSpan locates a literal or folded scalar: its properties and header,
// replaced by a new value, and its body below, removed. A keep-chomped block's
// trailing blank lines are its content and go with the body. The header and
// body are read back on their own before they are trusted (blockReproduces).
func (s *splicer) blockSpan(n *yaml.Node, start, parent int) (scalarLoc, error) {
	ls := s.t.starts[n.Line-1]
	line := s.lines[n.Line-1]
	header, idx, indicator, ok := blockHeader(line, start-ls+1)
	if !ok {
		return scalarLoc{}, decline("a block scalar header it cannot read")
	}
	body, last, content, ok := blockBody(s.lines, n.Line, parent, indicator, strings.Contains(header, "+"))
	if !ok || !blockReproduces(header, body, parent, content, n.Value) {
		return scalarLoc{}, decline("a block scalar that does not read back")
	}
	return scalarLoc{start: start, end: ls + idx + len(header), cut: s.t.ends[n.Line-1], cutEnd: s.t.ends[last-1]}, nil
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
			s.add(start.off, nextOff, "")
			return nil
		}
		if nextIndent != indent {
			return decline("siblings at different indents")
		}
		s.add(start.off, next.off, "")
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
	last, err := s.entryEnd(parent, old, i, step, indent)
	if err != nil {
		return err
	}
	// And from the blank lines above it, which separated it from what is left.
	first := start.line
	for first > 1 && isBlankText(s.t.text(first-1)) && !s.keepBlank[first-1] {
		first--
	}
	s.add(s.t.starts[first-1], s.t.brk[last-1], "")
	if empty {
		s.add(empt.start, empt.end, empt.text)
	}
	return nil
}

// entryEnd is the last source line of entry i of old, a block collection's
// children (the key's index for a mapping), whose key or dash is at indent:
// every line indented past it, or, for an indentless sequence value, through
// its dash lines, and the trailing blank lines of a keep-chomped block it
// owns.
func (s *splicer) entryEnd(parent *yaml.Node, old []*yaml.Node, i, step, indent int) (int, error) {
	isMap := step == 2
	// The last entry runs through every line indented past it, or, for an
	// indentless sequence value, through its dash lines.
	last := lastLine(old[i])
	if isMap {
		last = max(last, lastLine(old[i+1]))
	}
	// yaml.v4 can place an empty node on the line after the last one.
	if last > s.t.n() {
		return 0, decline("an entry that ends past the source")
	}
	// A keep-chomped block in the entry owns the blank lines below it.
	var own []blockScalar
	if isMap {
		considerBlockScalar(old[i+1], old[i].Column-1, s.lines, &own)
		walkBlockScalars(old[i+1], s.lines, &own)
	} else {
		considerBlockScalar(old[i], parent.Column-1, s.lines, &own)
		walkBlockScalars(old[i], s.lines, &own)
	}
	for _, b := range own {
		last = max(last, b.last)
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
	return last, nil
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
	p, err := s.requireBareColon(key)
	if err != nil {
		return edit{}, err
	}
	marker := " {}"
	if coll.Kind == yaml.SequenceNode {
		marker = " []"
	}
	return edit{start: p, end: p, text: marker}, nil
}

// keyColon is the offset just past key's colon. The key is read as the
// source spells it, before any rename.
func (s *splicer) keyColon(key *yaml.Node) (int, error) {
	k := *key
	if snap, ok := s.snap[key]; ok {
		k.Value, k.Tag, k.Style, k.Anchor = snap.value, snap.tag, snap.style, snap.anchor
	}
	loc, err := s.scalarSpan(&k, key.Column-1)
	if err != nil {
		return 0, err
	}
	src, lineEnd := s.t.src, s.t.ends[key.Line-1]
	p := loc.end
	for p < lineEnd && (src[p] == ' ' || src[p] == '\t') {
		p++
	}
	if p >= lineEnd || src[p] != ':' {
		return 0, decline("no colon after the key")
	}
	return p + 1, nil
}

// requireBareColon is keyColon, declined unless nothing but a comment
// follows the colon on key's line — the shared precondition for an edit
// that writes its own text right after it (an empty-collection marker, a
// collection moved onto the key's line).
func (s *splicer) requireBareColon(key *yaml.Node) (int, error) {
	colon, err := s.keyColon(key)
	if err != nil {
		return 0, err
	}
	lineEnd := s.t.ends[key.Line-1]
	if rest := bytes.TrimLeft(s.t.src[colon:lineEnd], " \t"); len(rest) > 0 && rest[0] != '#' {
		return 0, decline("something after the key's colon")
	}
	return colon, nil
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
	line := n.Line
	ls := s.t.starts[line-1]
	if isMap {
		if bytes.Contains(src[ls:off], []byte("?")) {
			return entryPos{}, 0, decline("an explicit key")
		}
	} else {
		q := off - 1
		for q >= ls && src[q] == ' ' {
			q--
		}
		if q < ls {
			if line, q, err = s.dashAbove(n); err != nil {
				return entryPos{}, 0, err
			}
			ls = s.t.starts[line-1]
		} else if src[q] != '-' {
			return entryPos{}, 0, decline("an item whose dash is not on its line")
		}
		off = q
	}
	if len(bytes.Trim(src[ls:off], " ")) != 0 {
		return entryPos{off: off, line: line}, -1, nil
	}
	indent := off - ls
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

// dashAbove finds the dash of a sequence item whose content starts on a later
// line: a dash alone on its line, or with only a comment after it, above the
// item past any blank or comment lines. It returns the dash's line and offset.
func (s *splicer) dashAbove(n *yaml.Node) (int, int, error) {
	for l := n.Line - 1; l >= 1; l-- {
		b := s.t.text(l)
		if isBlankText(b) || isCommentLine(b) {
			continue
		}
		if i := bytes.Index(b, []byte(" #")); i >= 0 {
			b = b[:i]
		}
		b = bytes.TrimRight(b, " \t")
		ind := indentOf(b)
		if string(b[ind:]) != "-" || ind >= n.Column-1 {
			break
		}
		return l, s.t.starts[l-1] + ind, nil
	}
	return 0, 0, decline("an item whose dash is not on its line")
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
// where an anchor or a tag sits when the node carries one — yaml.v4 gives a
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
		case '!':
			// A tag runs to the next blank. The parser once took flow indicators
			// in it as characters: "![" was a whole tag, and counting its bracket
			// ended an inner collection on its parent's closer (#282). yaml.v4
			// refuses such a tag at the parse.
			// Where a node cannot begin, the "!" is a plain scalar's.
			if !opensQuotedScalar(s, from, i) {
				i++
				continue
			}
			for i < len(s) && s[i] != ' ' && s[i] != '\t' {
				i++
			}
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
