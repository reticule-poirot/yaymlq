package cmd

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 records a scalar's trailing comment text (Node.LineComment)
// but discards how many spaces preceded its "#" in source, and its encoder
// always re-emits exactly one — so a round-trip through set/append/delete/
// rename/apply silently collapses every hand-aligned inline comment gutter in
// the document to a single space, not just the one on the line being edited.
// A standalone full-line comment (one starting at column 0, not attached to
// a value) is unaffected — this only happens to Node.LineComment.
//
// recordCommentGutters/widenCommentGutters bridge the two, the same way
// preserveBlankLines/tidyBlankLines bridge yaml.v3 dropping blank lines:
// record each comment's original gutter width before mutation (while
// Node.Line still lines up with source), then widen the encoder's fixed
// single space back to that count in the final output. Matched by comment
// text, in the order each occurrence appears — the same text appearing more
// than once in a document is matched oldest-first, so an edit that deletes
// one of several identically-worded comments (rare) can misalign gutters
// for the survivors; the result is still valid YAML with a wrong-but-benign
// gutter width, never corrupted content, so this is an accepted
// simplification rather than something worth chasing further.

// recordCommentGutters walks doc (before any mutation, while Node.Line still
// lines up with source) and appends each LineComment's original gutter width
// onto gutters, keyed by the comment's own text. A caller working through a
// multi-document stream against the same source calls this once per
// document, sharing one gutters map, then widenCommentGutters once over the
// whole encoded output afterward — same reasoning as blankLines/
// markBlankLines being split the way they are.
func recordCommentGutters(doc *yaml.Node, source []byte, gutters map[string][]int) {
	lines := strings.Split(string(source), "\n")
	walkCommentGutters(doc, lines, gutters)
}

func walkCommentGutters(n *yaml.Node, lines []string, gutters map[string][]int) {
	if n.LineComment != "" && n.Line >= 1 && n.Line <= len(lines) {
		if w, ok := gutterWidth(lines[n.Line-1], n.LineComment); ok {
			gutters[n.LineComment] = append(gutters[n.LineComment], w)
		}
	}
	for _, c := range n.Content {
		walkCommentGutters(c, lines, gutters)
	}
}

// gutterWidth finds comment (e.g. "# text", as yaml.v3's Node.LineComment
// stores it) at the end of line and returns the number of spaces
// immediately before its "#".
func gutterWidth(line, comment string) (int, bool) {
	idx := strings.LastIndex(line, comment)
	if idx < 0 {
		return 0, false
	}
	width := 0
	for i := idx - 1; i >= 0 && line[i] == ' '; i-- {
		width++
	}
	return width, true
}

// widenCommentGutters restores each inline comment's original gutter width
// in out, consuming gutters (built by recordCommentGutters) in place. A
// comment deleted by the edit is simply never consumed; one whose original
// width was already 1 is left alone (yaml.v3's own default).
func widenCommentGutters(out []byte, gutters map[string][]int) []byte {
	if len(gutters) == 0 {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	for i, line := range lines {
		idx, text, ok := trailingCommentIndex(line)
		if !ok {
			continue
		}
		queue := gutters[text]
		if len(queue) == 0 {
			continue
		}
		width := queue[0]
		gutters[text] = queue[1:]
		if width <= 1 {
			continue
		}
		widened := make([]byte, 0, len(line)+width-1)
		widened = append(widened, line[:idx]...)
		widened = append(widened, bytes.Repeat([]byte(" "), width)...)
		widened = append(widened, line[idx+1:]...) // skip the encoder's own single space
		lines[i] = widened
	}
	return bytes.Join(lines, []byte("\n"))
}

// trailingCommentIndex finds the boundary yaml.v3's encoder always writes
// before an inline comment (a single space then "#") and returns its index
// (pointing at the space) and the comment text from "#" onward. A line
// whose "#" starts at column 0 (a standalone comment) has no such boundary
// and returns ok=false, matching that those are never touched.
func trailingCommentIndex(line []byte) (idx int, text string, ok bool) {
	idx = bytes.Index(line, []byte(" #"))
	if idx < 0 {
		return 0, "", false
	}
	return idx, string(line[idx+1:]), true
}
