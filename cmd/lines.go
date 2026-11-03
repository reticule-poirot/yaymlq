package cmd

import (
	"bytes"
	"unicode/utf8"
)

// What a line is, for every fixup that reads the raw source.
//
// gopkg.in/yaml.v3's encoder always emits bare "\n" line endings, so a
// round-trip through set / append / delete / rename silently converts a
// CRLF-formatted source to LF. sourceLineEnding / restoreLineEnding bridge
// that, the same way preserveBlankLines / tidyBlankLines bridge the encoder's
// blank-line behavior: detect the source's line ending on the way in, reapply
// it to the encoder's output on the way out.
//
// sourceLines is the other half, and the one the fidelity layer depends on
// more deeply: every fixup locates a fact by the source line a node sits on,
// and yaml.Node.Line counts lines the way YAML does. So this file owns that
// definition, once, rather than letting each fixup split the input its own way
// and disagree with the parser from the first break it doesn't know about
// (#254).

// sourceLines splits source into lines, for the fixups that read the raw text
// to share across every document in a stream — computed by applyEdit rather
// than inside each per-document call, which would otherwise redo this same
// full-input split for every document — O(documents × input size) instead of
// O(input size).
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

// restoreLineEnding rewrites every "\n" in b (the encoder's output, which only
// ever contains bare "\n") to ending. An empty ending leaves b alone.
func restoreLineEnding(b []byte, ending string) []byte {
	if ending == "" || ending == "\n" {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\n"), []byte(ending))
}

// What a line of the encoder's output is: the scans that repair it split on
// "\n", correctly, since that is all yaml.v3 emits. But a block scalar the
// encoder wrote holds lines that read as structure to a line scan — a
// commented key, a stranded dash, a tab-only "blank" — and are content. A
// source block is masked to a one-line token before the encode and spliced
// back after these scans, so the only bodies they meet are the ones the
// encoder rendered itself: a multi-line scalar, a block blockscalars.go
// declined, or a block an edit brought in (#277). encodedBlockBodies is the
// one definition of which lines those are, so every scan skips the same ones.

// encodedBlockBodies reports, per line of the encoder's output, whether the
// line is content of a block scalar. A body is the lines below a header
// indented past the column of the construct that owns it — YAML's own rule,
// and the only one that holds: yaml.v3's indentation indicator does not follow
// the spec's parent-plus-digit reading, and a sibling key of an item can sit
// right of the header line's own indentation. A line of only spaces neither
// belongs to the body nor ends it; the encoder never writes one as content
// (a value holding one is quoted instead), and leaving it out keeps it
// spacing to tidyBlankLines.
func encodedBlockBodies(lines [][]byte) []bool {
	body := make([]bool, len(lines))
	owner := -1
	for i, line := range lines {
		if owner >= 0 {
			rest := bytes.TrimLeft(line, " ")
			if len(rest) == 0 {
				continue
			}
			if len(line)-len(rest) > owner {
				body[i] = true
				continue
			}
			owner = -1
		}
		if o, ok := blockHeaderOwner(line); ok {
			owner = o
		}
	}
	return body
}

// blockHeaderOwner reports whether line, from the encoder's output, opens a
// block scalar, and the column of the construct that owns it: the key's, or
// the last indicator's for an item, an explicit key or its value, or 0 for a
// document's root. The key has to be read, not just a "|" found at the end of
// the line, so that "k: a |" is not taken for a header. A miss in the other
// direction is the safe one — a body taken for structure is what #277 is,
// structure taken for a body only goes unrepaired.
func blockHeaderOwner(line []byte) (owner int, ok bool) {
	pos := len(line) - len(bytes.TrimLeft(line, " "))
	owner = pos
	if pos == 0 && bytes.HasPrefix(line, []byte("--- ")) {
		pos = skipSpaces(line, 4)
	}
	for pos+1 < len(line) && (line[pos] == '-' || line[pos] == '?' || line[pos] == ':') && line[pos+1] == ' ' {
		owner = pos
		pos = skipSpaces(line, pos+2)
	}
	if headerAt(line, pos) {
		return owner, true
	}
	end, ok := keyEnd(line, pos)
	if !ok {
		return 0, false
	}
	if headerAt(line, skipSpaces(line, end+1)) {
		return pos, true
	}
	return 0, false
}

// headerAt reports whether line, from pos, is any anchors and tags and then a
// block header ending the line.
func headerAt(line []byte, pos int) bool {
	for pos < len(line) && (line[pos] == '&' || line[pos] == '!') {
		sp := bytes.IndexByte(line[pos:], ' ')
		if sp < 0 {
			return false
		}
		pos = skipSpaces(line, pos+sp)
	}
	if pos >= len(line) || (line[pos] != '|' && line[pos] != '>') {
		return false
	}
	_, idx, _, ok := blockHeader(string(line), pos+1)
	return ok && idx == pos
}

// keyEnd returns the index of the ":" that ends the mapping key starting at
// pos, followed by a space. A quoted key ends at its closing quote; a plain one
// at its first ": ", which the encoder never writes inside a plain key.
func keyEnd(line []byte, pos int) (int, bool) {
	if pos >= len(line) {
		return 0, false
	}
	var i int
	switch line[pos] {
	case '"':
		for i = pos + 1; i < len(line) && line[i] != '"'; i++ {
			if line[i] == '\\' {
				i++
			}
		}
		i++
	case '\'':
		for i = pos + 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				break
			}
		}
		i++
	default:
		j := bytes.Index(line[pos:], []byte(": "))
		if j < 0 {
			return 0, false
		}
		return pos + j, true
	}
	if i+1 < len(line) && line[i] == ':' && line[i+1] == ' ' {
		return i, true
	}
	return 0, false
}

func skipSpaces(line []byte, pos int) int {
	for pos < len(line) && line[pos] == ' ' {
		pos++
	}
	return pos
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
