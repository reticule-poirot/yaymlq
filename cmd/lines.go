package cmd

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// What a line is, for everything that reads the raw source.
//
// The splice locates a node by the source line it sits on, and yaml.Node.Line
// counts lines the way YAML does: on "\n", "\r\n", "\r", NEL, LS and PS. So
// this file owns that definition, once — sourceLines and its byte-offset form,
// lineTable — rather than letting each reader split the input its own way and
// disagree with the parser from the first break it doesn't know about (#254).
// sourceLineEnding is the ending new text is written with.

// sourceLines splits source into lines, once per stream (applyEdit), rather
// than once per document, which would redo the same full-input split for every
// document — O(documents × input size) instead of O(input size).
//
// Line i of the document is element i-1, matching yaml.Node.Line. A source
// ending in a break leaves a final empty element, which is not a line of the
// document; callers that measure the document's own lines drop it.
func sourceLines(source []byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(source); {
		if w := yamlBreakWidth(source[i:]); w > 0 {
			out = append(out, string(source[start:i]))
			i += w
			start = i
			continue
		}
		_, w := utf8.DecodeRune(source[i:])
		i += w
	}
	return append(out, string(source[start:]))
}

// yamlBreakWidth reports how many bytes the YAML line break at the start of b
// occupies, or 0 when b does not start with one. A CRLF pair is one break, not
// two.
//
// The list is what yaml.v3 counts, which is what has to be matched: NEL, LS
// and PS are line breaks in YAML 1.1 and were dropped from 1.2, but
// yaml.Node.Line advances on all three, and agreeing with the parser is the
// whole point. Measured, not assumed — see TestSourceLinesAgreesWithNodeLine.
func yamlBreakWidth(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	switch b[0] {
	case '\n':
		return 1
	case '\r':
		if len(b) > 1 && b[1] == '\n' {
			return 2
		}
		return 1
	}
	r, w := utf8.DecodeRune(b)
	switch r {
	case '\u0085', ' ', ' ':
		return w
	}
	return 0
}

// sourceLineEnding returns the line ending source uses throughout, or "" when
// there is no single one to restore — a source with no line endings at all, or
// one that mixes kinds, is left to the encoder's plain "\n". That is the rule
// CRLF restoration already followed; CR joins it so a classic-Mac document is
// written back as it arrived.
//
// NEL, LS and PS are deliberately not endings here even though they are breaks
// for numbering. Nobody writes a file separated by U+2028 on purpose — it
// arrives from a generator that didn't mean to — and almost nothing downstream
// handles one, so emitting it back would preserve a mistake. They don't
// disqualify an otherwise consistent source either: the encoder renders every
// break as "\n", so one stray LS in a CRLF file becomes CRLF like the rest.
func sourceLineEnding(source []byte) string {
	crlf := bytes.Count(source, []byte("\r\n"))
	cr := bytes.Count(source, []byte("\r")) - crlf
	lf := bytes.Count(source, []byte("\n")) - crlf
	switch {
	case lf > 0:
		return ""
	case crlf > 0 && cr == 0:
		return "\r\n"
	case cr > 0 && crlf == 0:
		return "\r"
	}
	return ""
}

// lineTable is sourceLines' byte-offset form, for the splice: it maps a
// yaml.Node's Line and Column to where that position sits in the source.
// Lines break where sourceLines breaks them, and Column counts runes, which is
// how yaml.v3 counts it — a byte count would land mid-character after the
// first non-ASCII rune on a line.
type lineTable struct {
	src []byte
	// starts[i], ends[i] and brk[i] are, for line i+1: where it starts, where
	// its break starts, and where the next line starts.
	starts, ends, brk []int
}

func newLineTable(src []byte) *lineTable {
	t := &lineTable{src: src}
	start := 0
	for i := 0; i < len(src); {
		if w := yamlBreakWidth(src[i:]); w > 0 {
			t.starts, t.ends, t.brk = append(t.starts, start), append(t.ends, i), append(t.brk, i+w)
			i += w
			start = i
			continue
		}
		_, w := utf8.DecodeRune(src[i:])
		i += w
	}
	t.starts, t.ends, t.brk = append(t.starts, start), append(t.ends, len(src)), append(t.brk, len(src))
	return t
}

// n is the number of lines, counting the empty one after a final break.
func (t *lineTable) n() int { return len(t.starts) }

// lineOf is the 1-based line holding offset off.
func (t *lineTable) lineOf(off int) int {
	return sort.Search(len(t.starts), func(i int) bool { return t.starts[i] > off })
}

// text is line's content, without its break.
func (t *lineTable) text(line int) []byte { return t.src[t.starts[line-1]:t.ends[line-1]] }

// offset returns the byte offset of a 1-based line and column, or a decline
// when the position is not in the source.
func (t *lineTable) offset(line, col int) (int, error) {
	if line < 1 || line > len(t.starts) || col < 1 {
		return 0, decline("line %d, column %d is not in the source", line, col)
	}
	off := t.starts[line-1]
	for c := 1; c < col; c++ {
		if off >= t.ends[line-1] {
			return 0, decline("column %d is past the end of line %d", col, line)
		}
		_, w := utf8.DecodeRune(t.src[off:])
		off += w
	}
	return off, nil
}

func isDocEnd(line string) bool { return isDocToken(line, "...") }

// isDocStart and isDocEnd report whether line is a document-start or
// document-end marker. The token has to stand alone: what may follow it is the
// white space before the node or comment that shares the line, which is what
// separates "---" the marker from "---" the first three characters of something
// else.
func isDocStart(line string) bool { return isDocToken(line, "---") }

func isDocToken(line, token string) bool {
	if line == token {
		return true
	}
	if len(line) <= len(token) || line[:len(token)] != token {
		return false
	}
	c := line[len(token)]
	return c == ' ' || c == '\t'
}
