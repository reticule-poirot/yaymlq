package cmd

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 decodes a stream to one node per document and keeps nothing
// about how the source delimited them: the "---" that introduced a document and
// the "..." that closed it are gone by the time the encoder runs, and the
// encoder writes its own separator — one "---" between documents, none before
// the first and never a "...". So an edit to an unrelated key drops the "---" at
// the top of a manifest and every document-end marker in the stream (#252).
//
// That is a sharper loss than the spacing the other fixups restore. Both
// markers are document structure: anything reading the file textually — a
// `csplit` on "^---$", a reviewer counting documents, a renderer that treats a
// leading "---" as front-matter — sees a different stream afterward, and the
// edit was to a key somewhere else entirely.
//
// recordDocMarkers / restoreDocMarkers bridge it the way the rest of the layer
// bridges its own fact: read what the source said before any mutation, put it
// back around the encode. Only the bare marker token is restored. What else
// shared the marker's line — a comment, or a node written compactly after it —
// stays where the encoder put it, with one exception: when the encoder wrote
// nothing but an anchor, a tag or a block-scalar header on that first line, the
// marker folds back onto it, which is the usual spelling of a tagged document
// and the shape "--- !Config" needs to come back whole.

// docMarker is what the source said about one document's own boundaries.
type docMarker struct {
	// start is whether the source introduced the document with a "---" line.
	// Every document but the first has one — a document that follows another
	// cannot begin without it — so for those this only ever confirms what the
	// encoder's own separator already provides.
	start bool
	// inline is whether the document's content began on that same "---" line,
	// which is how a root tag or anchor is usually written. It is a necessary
	// condition for folding the marker back onto the encoder's first line, not
	// a sufficient one — see docTokenLine for the other half.
	inline bool
	// end is whether the source closed the document with a "..." line.
	end bool
	// below is how many comment lines the source wrote above the "---". The
	// encoder emits a document's head comment before anything else, so the
	// marker has to go after that many of those lines rather than at the top of
	// the document — a description or licence header above `---` is the ordinary
	// way a .goreleaser.yml or an Ansible playbook starts, and restoring the
	// marker above it moved two lines the edit never touched.
	below int
}

// recordDocMarkers reads each document's own delimiters out of the raw source.
// Called before any mutation, while Node.Line still lines up with source; lines
// is the whole input split once, shared with the other fixups.
//
// A document node's Line is the line of its "---" when it has one and the line
// of its content when it does not, so the two are told apart by whether that
// line actually holds a marker rather than by arithmetic on the content's line.
// Returns nil when the stream carried no markers at all, which is the common
// case and the one that costs nothing.
func recordDocMarkers(docs []*yaml.Node, lines []string) []docMarker {
	marks := make([]docMarker, len(docs))
	found := false
	for i, d := range docs {
		if d.Line >= 1 && d.Line <= len(lines) && isDocStart(lines[d.Line-1]) {
			marks[i].start = true
			marks[i].inline = len(d.Content) > 0 && d.Content[0].Line == d.Line
			floor := 1
			if i > 0 && docs[i-1].Line >= 1 {
				floor = docs[i-1].Line + 1
			}
			marks[i].below = commentsAbove(lines, d.Line, floor)
			found = true
		}
		// The "..." closing this document lies below its first line and above
		// the next document's, which is where the parser would have had to see
		// it. A marker at column 0 is always a marker — a block scalar's content
		// has to be indented past its parent, so no line of a value reaches
		// column 0 — so no further disambiguation is needed inside that span.
		limit := len(lines)
		if i+1 < len(docs) && docs[i+1].Line >= 1 {
			limit = docs[i+1].Line - 1
		}
		// A decoded node's Line is always at least 1; the floor is there so a
		// Line that somehow is not can only shorten the scan, never index
		// backwards out of it.
		for n := max(d.Line+1, 1); n <= limit && n <= len(lines); n++ {
			if isDocEnd(lines[n-1]) {
				marks[i].end = true
				found = true
				break
			}
		}
	}
	if !found {
		return nil
	}
	return marks
}

// commentsAbove counts the comment lines in the run of comment and blank lines
// directly above line, stopping at floor so a comment that trailed the previous
// document is not counted as introducing this one. Blank lines in the run are
// crossed but not counted: the encoder does not emit them, so they are not lines
// the marker has to be placed after.
func commentsAbove(lines []string, line, floor int) int {
	count := 0
	for n := line - 1; n >= floor && n <= len(lines); n-- {
		switch text := strings.TrimLeft(lines[n-1], " \t"); {
		case text == "":
			continue
		case strings.HasPrefix(text, "#"):
			count++
		default:
			return count
		}
	}
	return count
}

// isDocStart and isDocEnd report whether line is a document-start or
// document-end marker. The token has to stand alone: what may follow it is the
// white space before the node or comment that shares the line, which is what
// separates "---" the marker from "---" the first three characters of something
// else.
func isDocStart(line string) bool { return isDocToken(line, "---") }

func isDocEnd(line string) bool { return isDocToken(line, "...") }

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

// docTokenLine reports whether line holds nothing but the properties that may
// precede a document's root node: an anchor, a tag, and a block scalar's own
// header. Such a line introduces the node rather than being part of it, so
// prefixing "--- " to it is always the document the source wrote.
//
// Anything else is left on its own line below the marker, because a node the
// encoder puts there may be one the marker's line cannot hold: "--- k: 1" is
// not a document yaml.v3 will read back, so folding a block mapping's first
// entry up would write out a file this tool can no longer parse. That was the
// bug the first version of this had — it folded on the source's layout alone,
// and a document introduced by the non-specific tag "!", which the encoder
// resolves away and does not re-emit, had its first mapping entry pulled onto
// the marker line.
func docTokenLine(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	for _, f := range bytes.Fields(line) {
		switch f[0] {
		case '&', '!':
		case '|', '>':
			// A header is the indicator plus chomping and indentation
			// indicators, nothing else; a bare "|" or ">" cannot be content,
			// since both are reserved and a scalar spelled that way is quoted.
			for _, c := range f[1:] {
				if c != '+' && c != '-' && (c < '1' || c > '9') {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// commentLine reports whether line holds nothing but a comment, which is what
// the encoder writes a document's head comment as.
func commentLine(line []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(line, " \t"), []byte("#"))
}

// restoreDocMarkers rewrites the encoder's document delimiters as the source
// spelled them: a "---" above the first document when the source had one, a
// "..." below every document the source closed, and a marker the source wrote
// content on folded back onto that content's line.
//
// It replaces the encoder's own separators rather than inserting around them,
// so every document's start comes from one place. It runs after the restores
// that match on text (a block scalar's body must not be read as a document) and
// before the line endings are reapplied, so the lines it adds get them too.
//
// The split is checked against the records before anything is changed: the
// encoder writes exactly one bare "---" between documents, so finding any other
// number of them means the output is not shaped the way this reads it, and
// leaving the delimiters alone is then better than moving a document boundary.
func restoreDocMarkers(out []byte, marks []docMarker) []byte {
	if len(marks) == 0 {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	// A stream ending in a break leaves a final empty element that is not a
	// line of the output; it is put back untouched so the result still ends the
	// way the encoder ended it.
	body := len(lines)
	if body > 0 && len(lines[body-1]) == 0 {
		body--
	}
	sep := []byte("---")
	starts := []int{0}
	for i := range lines[:body] {
		if bytes.Equal(lines[i], sep) {
			starts = append(starts, i+1)
		}
	}
	if len(starts) != len(marks) {
		return out
	}

	res := make([][]byte, 0, len(lines)+2*len(marks))
	for i, m := range marks {
		doc := lines[starts[i]:body]
		if i+1 < len(starts) {
			// Up to, but not including, the separator the encoder wrote.
			doc = lines[starts[i] : starts[i+1]-1]
		}
		// The head comment the encoder wrote first goes out first, as many of its
		// lines as the source wrote above the marker, so the marker lands back
		// between them rather than above the lot.
		lead := 0
		for lead < m.below && lead < len(doc) && commentLine(doc[lead]) {
			lead++
		}
		res = append(res, doc[:lead]...)
		doc = doc[lead:]
		switch {
		case m.start && m.inline && len(doc) > 0 && docTokenLine(doc[0]):
			res = append(res, append(append([]byte{}, "--- "...), doc[0]...))
			doc = doc[1:]
		case m.start || i > 0:
			// A document after the first needs a separator whatever the record
			// says, or the stream it belongs to becomes one document.
			res = append(res, sep)
		}
		res = append(res, doc...)
		if m.end {
			res = append(res, []byte("..."))
		}
	}
	res = append(res, lines[body:]...)
	return bytes.Join(res, []byte("\n"))
}
