package cmd

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 records the source line of every node but throws away blank
// lines when it decodes, so a round-trip through set / append / delete collapses
// the vertical spacing of a hand-formatted document. The encoder will, however,
// emit a blank line before a node whose HeadComment begins with a newline.
//
// preserveBlankLines bridges the two: it runs on the freshly decoded tree
// (before any mutation, while Line numbers still line up with source) and, for
// every node that had a blank line above it in source, prefixes that node's
// HeadComment with "\n". A run of blank lines collapses to one.
//
// A convenience for a single document. A caller working through a
// multi-document stream against the same source (applyEdit, in cmd/edit.go)
// should call sourceLines once and markBlankLines per document instead — the
// split is a full scan of source, so calling this function once per document
// would redo it every time.
func preserveBlankLines(doc *yaml.Node, source []byte) {
	blank := blankLines(sourceLines(source))
	if len(blank) == 0 {
		return
	}
	markBlankLines(doc, blank, nil)
}

// blankLines returns the set of 1-indexed source line numbers that are empty
// or whitespace-only. lines comes from sourceLines, so the numbering is the
// one yaml.Node.Line uses — counting only "\n" here put every mark after the
// first other kind of break on the wrong node (#254).
func blankLines(lines []string) map[int]bool {
	out := make(map[int]bool)
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			out[i+1] = true
		}
	}
	return out
}

func markBlankLines(n *yaml.Node, blank map[int]bool, ld *loneDashes) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			markBlankLines(c, blank, ld)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			markBlankAbove(n.Content[i], blank, ld)
			markBlankLines(n.Content[i+1], blank, ld)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			markBlankAbove(c, blank, ld)
			markBlankLines(c, blank, ld)
		}
	}
}

// markBlankAbove prefixes n.HeadComment with a newline when the source line
// directly above n (or above n's head comment block, if it has one) was blank.
func markBlankAbove(n *yaml.Node, blank map[int]bool, ld *loneDashes) {
	if blankAbove(n, blank, ld) >= 1 && !strings.HasPrefix(n.HeadComment, "\n") {
		n.HeadComment = "\n" + n.HeadComment
	}
}

// blankAbove returns the source line directly above n — above n's head comment
// block, when it has one — if that line was blank, and 0 otherwise. Shared with
// dropHeadCommentBlanks so the two agree on which line is "above" a node; they
// disagreed about nothing, but only because the arithmetic was written twice.
func blankAbove(n *yaml.Node, blank map[int]bool, ld *loneDashes) int {
	above := ld.top(n) - 1
	if n.HeadComment != "" {
		above -= strings.Count(n.HeadComment, "\n") + 1
	}
	if above >= 1 && blank[above] {
		return above
	}
	return 0
}

// dropHeadCommentBlanks removes the blank line that separates a document's own
// head comment from its first node, so blanklines.go doesn't mark it as spacing:
// yaml.v3 attaches a comment block that a blank line divides from the content to
// the *document* node, and its encoder emits that blank line itself. Marking it
// too wrote a second copy, so a file opening with a comment header — most files
// that open with a comment at all — gained a blank line on every edit.
//
// It is the same move dropBlockContentBlanks makes one level down: a blank line
// that belongs to something else is not spacing for this fixup to restore. The
// head comment is the signal, not the geometry: yaml.v3 gives the comment to the
// document only when a blank line divided them, and to the first key when
// nothing did.
//
// The whole run is dropped, not one line, because the encoder emits exactly one
// blank there — which is the "a run of blank lines collapses to one" rule this
// fixup follows everywhere else.
func dropHeadCommentBlanks(blank map[int]bool, docs []*yaml.Node, ld *loneDashes) {
	for _, d := range docs {
		if d.HeadComment == "" || len(d.Content) == 0 {
			continue
		}
		// The node markBlankLines would mark: the first key of a root mapping,
		// the first element of a root sequence. A root scalar has no child and
		// is never marked, so dropping nothing for it is right.
		first := d.Content[0]
		if len(first.Content) > 0 {
			first = first.Content[0]
		}
		for above := blankAbove(first, blank, ld); above >= 1 && blank[above]; above-- {
			delete(blank, above)
		}
	}
}

// tidyBlankLines rewrites lines that are only spaces or tabs as empty lines.
// yaml.v3 indents the blank lines it emits for nested nodes; it never emits a
// semantically significant whitespace-only line (a block scalar that would
// contain one is written as a quoted string instead), so this only touches
// spacing.
func tidyBlankLines(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		if len(line) > 0 && len(bytes.TrimLeft(line, " \t")) == 0 {
			lines[i] = nil
		}
	}
	return bytes.Join(lines, []byte("\n"))
}
