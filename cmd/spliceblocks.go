package cmd

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// blockBody returns the block's source lines below its header. The extent is
// the block's content indentation: the explicit indicator when there is one,
// otherwise the indentation of the first non-blank line below the header,
// which has to be deeper than the construct the block belongs to. A trailing
// run of blank lines is claimed only under keep chomping, where it is value;
// under the other three it is spacing before the next key, and stays where it
// is.
func blockBody(lines []string, header, parent, indicator int, keep bool) (body []string, last, content int, ok bool) {
	if indicator > 0 {
		content = parent + indicator
	}
	// Splitting a source that ends in a newline leaves a final empty element
	// that is not a line of the document — claiming it would invent a blank
	// line at the end of every file.
	limit := len(lines)
	if limit > 0 && lines[limit-1] == "" {
		limit--
	}
	var pending []string
	pendingLast := 0
	for i := header + 1; i <= limit; i++ {
		line := lines[i-1]
		if blockBlank(line) {
			pending = append(pending, line)
			pendingLast = i
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if content == 0 {
			if indent <= parent {
				break
			}
			content = indent
		}
		if indent < content {
			break
		}
		body = append(body, pending...)
		pending = pending[:0]
		body = append(body, line)
		last = i
	}
	if keep && len(pending) > 0 {
		body = append(body, pending...)
		last = pendingLast
	}
	return body, last, content, len(body) > 0
}

// blockHeader reads the indicator run that opens a block scalar: "|" or ">",
// then any mix of a chomping indicator and an explicit indentation indicator,
// in either order. It scans forward from the node's own start column, which is
// where an anchor or a tag sits when the node carries one, so the indicator
// may be a few characters in. The run has to end the line (a trailing comment
// aside), which is what rules out a "|" that belongs to something else.
func blockHeader(line string, col int) (header string, idx, indicator int, ok bool) {
	if col < 1 || col > len(line) {
		return "", 0, 0, false
	}
	rest := line[col-1:]
	i := strings.IndexAny(rest, "|>")
	if i < 0 || strings.ContainsRune(rest[:i], '#') {
		return "", 0, 0, false
	}
	j := i + 1
	for j < len(rest) {
		c := rest[j]
		if c >= '1' && c <= '9' {
			if indicator != 0 {
				return "", 0, 0, false
			}
			indicator = int(c - '0')
		} else if c != '+' && c != '-' {
			break
		}
		j++
	}
	// Whatever follows the run has to be the end of the line, the white space
	// before a comment (a tab counts: the header's trailing white space is
	// s-white, space or tab) or the comment itself. Anything else means the
	// "|" was part of something that isn't a header.
	if j < len(rest) && rest[j] != ' ' && rest[j] != '\t' && rest[j] != '#' {
		return "", 0, 0, false
	}
	return rest[i:j], col - 1 + i, indicator, true
}

// blockReproduces re-parses the recorded header and body and reports whether
// they yield value — the text the decoder got from the same source. Nothing is
// spliced on the strength of a block that fails this: the block's extent is
// read out of the raw text, and a construct that puts the body somewhere
// blockBody doesn't predict would otherwise splice a truncated block over a
// correct one.
func blockReproduces(header string, body []string, parent, content int, value string) bool {
	probe := make([]string, 0, len(body)+2)
	probe = append(probe, "k: "+header)
	for _, line := range body {
		if !blockBlank(line) && len(line)-len(strings.TrimLeft(line, " ")) <= parent {
			return false
		}
		// The same re-indent the splice makes, so a block that verifies here
		// verifies wherever the splice puts it.
		probe = append(probe, shiftBlockLine(line, -parent, content))
	}
	got, ok := decodeOne([]byte(strings.Join(probe, "\n") + "\n"))
	m, isMap := got.(map[string]any)
	return ok && isMap && len(m) == 1 && m["k"] == value
}

// blockScalar is the source span of one literal or folded scalar's body.
type blockScalar struct {
	// first and last are the 1-indexed source lines the body occupies. A blank
	// line in that span belongs to the value, which is why the splice reads it
	// as content (keepBlank) rather than as spacing.
	first, last int
}

// considerBlockScalar records n's body span when it is a block scalar whose
// body can be read out of lines. parent is the
// indentation the block's content indentation is measured against — the
// column of the key, or of the sequence's dash.
func considerBlockScalar(n *yaml.Node, parent int, lines []string, out *[]blockScalar) {
	if n.Kind != yaml.ScalarNode || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		return
	}
	if n.Line < 1 || n.Line > len(lines) {
		return
	}
	line := lines[n.Line-1]
	header, idx, indicator, ok := blockHeader(line, n.Column)
	if !ok {
		return
	}
	// A header on a line of its own is not recorded: its body is measured
	// against a prefix this does not model.
	if idx > parent && strings.TrimSpace(line[:idx]) == "" {
		return
	}
	// A line whose indentation lineBound reads differently from the tree is
	// shaped in a way it doesn't model, so its body is not measured.
	if lineBound(line, idx) != parent {
		return
	}
	keep := strings.Contains(header, "+")
	_, last, _, ok := blockBody(lines, n.Line, parent, indicator, keep)
	if !ok {
		return
	}
	*out = append(*out, blockScalar{first: n.Line + 1, last: last})
}

// recordBlockScalars walks every document in a stream (before any mutation,
// while Node.Line still lines up with source) and records each literal or
// folded scalar's source spelling. lines is the whole raw input split once,
// shared with the other fixups.
func recordBlockScalars(docs []*yaml.Node, lines []string) []blockScalar {
	var out []blockScalar
	for _, d := range docs {
		walkBlockScalars(d, lines, &out)
	}
	return out
}

// walkBlockScalars descends into values and sequence elements, never into a
// mapping key: a block scalar used as a key is emitted in explicit-key form,
// which this restore has no line to splice into. Each candidate is considered
// by its parent, which is what knows the indentation the block's content is
// measured against.
func walkBlockScalars(n *yaml.Node, lines []string, out *[]blockScalar) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			considerBlockScalar(c, 0, lines, out)
			walkBlockScalars(c, lines, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			considerBlockScalar(n.Content[i+1], n.Content[i].Column-1, lines, out)
			walkBlockScalars(n.Content[i+1], lines, out)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			considerBlockScalar(c, n.Column-1, lines, out)
			walkBlockScalars(c, lines, out)
		}
	}
}

// blockBlank reports whether line is an empty line of a block scalar rather
// than one of its content lines. Only spaces indent YAML, so a line holding a
// tab holds content however empty it looks — and in a block scalar that tab is
// a character of the value.
func blockBlank(line string) bool {
	return strings.TrimLeft(line, " ") == ""
}

// lineBound returns the indentation the block scalar at idx on line is
// measured against: the column of the innermost construct that owns it. That
// is the key it is the value of, or — when the scalar follows a dash directly
// — the dash of the sequence it is an element of, since a sequence's own
// indentation is where its dash sits and not where its items start.
//
// It is the same quantity the node tree gives as parent, which
// considerBlockScalar checks it against before recording anything.
func lineBound(line string, idx int) int {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	dash := -1
	// The three block indicators that can be compacted onto one line, each
	// "X " followed by any further spaces. "? " and ": " matter because a key
	// too long to inline forces the encoder into explicit-key form, and the
	// sequence or scalar then sits past a ": " that was never in the source.
	for i+1 < idx && line[i+1] == ' ' {
		switch line[i] {
		case '-':
			dash = i
		case '?', ':':
			// A mapping owns what follows, so a sequence seen before this
			// one no longer does.
			dash = -1
		default:
			return i
		}
		i += 2
		for i < idx && line[i] == ' ' {
			i++
		}
	}
	if dash >= 0 && i == idx {
		return dash
	}
	return i
}

// shiftBlockLine re-indents one body line by shift columns, content being the
// column the body's content lines sit at before the shift. A space-only line
// is two things at once: its spaces up to content are indentation and the rest
// are a character of the value. So one indented past content moves like any
// other line, keeping what lies past the indentation (#280), and one indented
// no further is empty either way and is left as the source wrote it — clipped
// only where a dedent would otherwise leave it past the new content column. A
// content of 0 says there is no such column, every space-only line is empty,
// and all of them are left alone; that is also the answer for a flow scalar,
// whose space-only lines never hold value.
func shiftBlockLine(line string, shift, content int) string {
	if shift == 0 {
		return line
	}
	if blockBlank(line) && (content == 0 || len(line) <= content) {
		if shift < 0 && content > 0 && len(line) > content+shift {
			return line[:content+shift]
		}
		return line
	}
	if shift > 0 {
		return strings.Repeat(" ", shift) + line
	}
	drop := -shift
	if n := len(line) - len(strings.TrimLeft(line, " ")); drop > n {
		drop = n
	}
	return line[drop:]
}
