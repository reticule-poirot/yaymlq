package cmd

import (
	"bytes"
	"strings"
)

// blanklines.go restores a blank line by marking the node below it, and some
// blank lines have no node below: the one at the end of the stream, and the
// ones beside a document marker — above a "---" or a "...", or below one. The
// encoder writes none of them, so an edit anywhere dropped every such line
// (#270, and the blank beside a marker #259 left open). This is that fixup's
// second mechanism, and it works on the output rather than the tree, because
// what these lines sit beside is not a node: the stream's own boundaries.
//
// So the record is taken per boundary — every source "---" and "..." line at
// column 0, and the end of the stream — and says whether a blank line sat
// directly above it and, for a bare marker, directly below it. A run counts as
// one, the rule blanklines.go follows everywhere else. The restore tops up
// rather than inserts: where the output already has a blank line at that spot
// it adds nothing, which is what keeps a blank blanklines.go already marked,
// one the encoder writes itself (an empty document) and one that is a keep
// block's content from being written twice.
//
// A blank line inside a block scalar's source span is the value's, not the
// boundary's, whether or not the block is restored, so it is never recorded;
// an edit that replaces the block takes those lines with it.
//
// Each restore is checked twice before it is trusted. Per blank line: the
// nearest non-blank line beyond it has to be the same in the output as in the
// source — the same comment, or not a comment — since a comment yaml.v3 gave
// to the other side of a marker means the boundary is not where this reads it
// (#290).
// And whole: the output's boundaries have to be the source's, kind for
// kind and in order, or nothing is added; and the result has to decode to the
// same values, because a blank line after a keep block the edit brought in
// would be content, which no line-level check can see.

// boundaryBlank is one blank line beside a boundary, and the line beyond it
// that places it.
type boundaryBlank struct {
	// beyond is the trimmed text of the nearest non-blank line on the far side
	// of the blank: a comment's own text, "" for any other line, and edgeOfStream
	// when the blank runs to the start or the end of the stream.
	beyond string
}

// edgeOfStream is the beyond of a blank line with nothing past it. It can be no
// line's trimmed text, since a trimmed line never holds a break.
const edgeOfStream = "\n"

// boundary is one source "---" or "..." line and the blank lines beside it.
type boundary struct {
	end          bool
	above, below *boundaryBlank
}

// boundaryBlanks is the record: the stream's markers in order, then its end.
type boundaryBlanks struct {
	marks []boundary
	eof   *boundaryBlank
}

// recordBoundaryBlanks reads the blank lines beside each boundary out of the
// raw source, before any mutation. blocks gives the source spans whose blank
// lines belong to a value. Returns nil when there is nothing to restore.
func recordBoundaryBlanks(lines []string, blocks []blockScalar) *boundaryBlanks {
	// A source ending in a break leaves a final empty element that is not a line
	// of the document. One that does not end in a break has no blank line at
	// its end to restore: a trailing white space fragment is not a line, and
	// writing it as one would add the break the source left out.
	n := len(lines)
	endsInBreak := n > 0 && lines[n-1] == ""
	if endsInBreak {
		n--
	}
	inBlock := make(map[int]bool)
	for _, b := range blocks {
		for i := b.first; i <= b.last; i++ {
			inBlock[i-1] = true
		}
	}
	blank := func(i int) bool {
		return i >= 0 && i < n && !inBlock[i] && isBlankLine(lines[i])
	}
	beyond := func(i, step int) *boundaryBlank {
		for ; i >= 0 && i < n; i += step {
			if !isBlankLine(lines[i]) {
				return &boundaryBlank{beyond: commentText(lines[i])}
			}
		}
		return &boundaryBlank{beyond: edgeOfStream}
	}

	bb := &boundaryBlanks{}
	found := false
	for i := 0; i < n; i++ {
		start, end := isDocStart(lines[i]), isDocEnd(lines[i])
		if !start && !end {
			continue
		}
		m := boundary{end: end}
		if blank(i - 1) {
			m.above = beyond(i-1, -1)
			found = true
		}
		// Only a bare marker can have a blank line below it that is the
		// marker's: anything sharing its line is a node, or a comment the
		// encoder places on its own.
		if (lines[i] == "---" || lines[i] == "...") && blank(i+1) {
			m.below = beyond(i+1, 1)
			found = true
		}
		bb.marks = append(bb.marks, m)
	}
	if endsInBreak && blank(n-1) {
		bb.eof = beyond(n-1, -1)
		found = true
	}
	if !found {
		return nil
	}
	return bb
}

// restoreBoundaryBlanks puts back the blank lines bb recorded. It runs after
// restoreDocMarkers, which writes the boundaries it anchors on, and before the
// line endings are reapplied, so the lines it adds get them too.
func restoreBoundaryBlanks(out []byte, bb *boundaryBlanks) []byte {
	if bb == nil {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	n := len(lines)
	if n > 0 && len(lines[n-1]) == 0 {
		n--
	}
	var marks []int
	for i := 0; i < n; i++ {
		s := string(lines[i])
		start, end := isDocStart(s), isDocEnd(s)
		if !start && !end {
			continue
		}
		if len(marks) == len(bb.marks) || bb.marks[len(marks)].end != end {
			return out
		}
		marks = append(marks, i)
	}
	if len(marks) != len(bb.marks) {
		return out
	}

	blank := func(i int) bool { return i >= 0 && i < n && isBlankLine(string(lines[i])) }
	// beyond reads the output the way the record read the source, from the
	// line at i outward in the direction of step.
	beyond := func(i, step int) string {
		for ; i >= 0 && i < n; i += step {
			if !blank(i) {
				return commentText(string(lines[i]))
			}
		}
		return edgeOfStream
	}
	// insert holds the positions a blank line goes in front of. A blank between
	// a "..." and the "---" after it is recorded twice, once below each, and is
	// one position here.
	insert := make(map[int]bool)
	add := func(want *boundaryBlank, at, step int) {
		// at is where the blank goes; the line it would sit beside, on the far
		// side from the boundary, is at itself when step is 1 and at-1 when -1.
		near := at
		if step < 0 {
			near = at - 1
		}
		if want == nil || blank(near) || beyond(near, step) != want.beyond {
			return
		}
		insert[at] = true
	}
	for k, i := range marks {
		add(bb.marks[k].above, i, -1)
		add(bb.marks[k].below, i+1, 1)
	}
	add(bb.eof, n, -1)
	if len(insert) == 0 {
		return out
	}

	res := make([][]byte, 0, len(lines)+len(insert))
	for i, line := range lines {
		if insert[i] && i <= n {
			res = append(res, nil)
		}
		res = append(res, line)
	}
	if insert[len(lines)] {
		res = append(res, nil)
	}
	restored := bytes.Join(res, []byte("\n"))
	if !sameValues(out, restored) {
		return out
	}
	return restored
}

// isBlankLine reports whether line holds nothing but spaces and tabs — the
// white space YAML separates with, and not every Unicode space strings.TrimSpace
// knows, since a line holding a no-break space is content.
func isBlankLine(line string) bool {
	return strings.Trim(line, " \t") == ""
}

// commentText returns line's comment when the line is nothing but one, and ""
// for any other line.
func commentText(line string) string {
	text := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(text, "#") {
		return ""
	}
	return strings.TrimRight(text, " \t")
}
