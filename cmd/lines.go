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
