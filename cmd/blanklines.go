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
func preserveBlankLines(doc *yaml.Node, source []byte) {
	blank := blankLines(source)
	if len(blank) == 0 {
		return
	}
	markBlankLines(doc, blank)
}

// blankLines returns the set of 1-indexed source line numbers that are empty or
// whitespace-only — the same numbering yaml.Node.Line uses.
func blankLines(source []byte) map[int]bool {
	out := make(map[int]bool)
	for i, line := range strings.Split(string(source), "\n") {
		if strings.TrimSpace(line) == "" {
			out[i+1] = true
		}
	}
	return out
}

func markBlankLines(n *yaml.Node, blank map[int]bool) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			markBlankLines(c, blank)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			markBlankAbove(n.Content[i], blank)
			markBlankLines(n.Content[i+1], blank)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			markBlankAbove(c, blank)
			markBlankLines(c, blank)
		}
	}
}

// markBlankAbove prefixes n.HeadComment with a newline when the source line
// directly above n (or above n's head comment block, if it has one) was blank.
func markBlankAbove(n *yaml.Node, blank map[int]bool) {
	above := n.Line - 1
	if n.HeadComment != "" {
		above -= strings.Count(n.HeadComment, "\n") + 1
	}
	if above >= 1 && blank[above] && !strings.HasPrefix(n.HeadComment, "\n") {
		n.HeadComment = "\n" + n.HeadComment
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
