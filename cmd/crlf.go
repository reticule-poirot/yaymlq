package cmd

import "bytes"

// gopkg.in/yaml.v3's encoder always emits bare "\n" line endings, so a
// round-trip through set / append / delete / rename silently converts a
// CRLF-formatted source to LF. hasCRLF / restoreCRLF bridge that, the same
// way preserveBlankLines / tidyBlankLines bridge the encoder's blank-line
// behavior: detect the source's line ending on the way in, reapply it to the
// encoder's output on the way out.

// hasCRLF reports whether source consistently uses CRLF line endings: every
// "\n" is part of a "\r\n" pair. A source with no line endings at all, or a
// mix of bare "\n" and "\r\n", is not treated as CRLF — there's no single
// ending to restore, so the encoder's plain "\n" output is left alone.
func hasCRLF(source []byte) bool {
	crlf := bytes.Count(source, []byte("\r\n"))
	if crlf == 0 {
		return false
	}
	lf := bytes.Count(source, []byte("\n")) - crlf
	return lf == 0
}

// restoreCRLF rewrites every "\n" in b (the encoder's output, which only ever
// contains bare "\n") to "\r\n".
func restoreCRLF(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}
