package cmd

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// gopkg.in/yaml.v3 does not record the white space at the end of a line — there
// is nothing in yaml.Node to record it in — and its encoder writes none, so a
// round-trip through set/append/delete/rename/apply strips every trailing space
// and tab in the document (#252). The characters mean nothing to YAML, which is
// the whole reason an edit has no business removing them: the lines they sit on
// are lines the caller did not touch, and they appear in the diff all the same.
//
// recordTrailingSpace / restoreTrailingSpace bridge it the way
// commentgutters.go bridges the gutter width, and with the same matching rule:
// keyed by the line's own text, consumed in the order the lines appear. A line
// the edit rewrote no longer matches what was recorded, so it keeps the
// caller's spelling rather than the source's.
//
// What makes this one different is where the check lives. Everywhere else in
// this layer trailing white space is insignificant, but inside a block scalar
// it is a character of the value — so a body line that happens to read exactly
// like a line elsewhere that did carry trailing white space would have its
// value changed by the restore, and matching on text cannot tell the two apart.
// Rather than carve out the block spans and hope the carve-out is complete,
// restoreTrailingSpace checks the finished output against the output it started
// from and keeps the result only if every document still decodes to the same
// value. A collision therefore costs the file its trailing white space, not its
// contents.
//
// A line of nothing but white space is not this fixup's: that is vertical
// spacing, which blanklines.go owns and tidyBlankLines empties.

// recordTrailingSpace maps each source line that ends in white space to the run
// it ends with, keyed by the rest of the line. lines comes from sourceLines, so
// the runs never include a line ending. Returns nil when no line in the stream
// carries any, which is the common case and costs nothing further.
func recordTrailingSpace(lines []string) map[string][]string {
	var runs map[string][]string
	for _, line := range lines {
		text := strings.TrimRight(line, " \t")
		if len(text) == len(line) || text == "" {
			continue // nothing trailing, or a white-space-only line
		}
		if runs == nil {
			runs = make(map[string][]string)
		}
		runs[text] = append(runs[text], line[len(text):])
	}
	return runs
}

// restoreTrailingSpace puts each recorded run back on the end of the output
// line that matches the text it was recorded against, consuming runs in place.
// It returns out unchanged when the result would mean something else — see this
// file's header for why the guard is on the whole output.
//
// It runs after every restore that splices source text back in, so a block
// scalar's body and a flow collection's own lines already carry the white space
// they were written with and do not match a record; and before the line endings
// are reapplied, since a run appended after a "\r\n" would not be at the end of
// the line at all.
func restoreTrailingSpace(out []byte, runs map[string][]string) []byte {
	if len(runs) == 0 {
		return out
	}
	lines := bytes.Split(out, []byte("\n"))
	changed := false
	for i, line := range lines {
		queue := runs[string(line)]
		if len(queue) == 0 {
			continue
		}
		runs[string(line)] = queue[1:]
		lines[i] = append(append([]byte{}, line...), queue[0]...)
		changed = true
	}
	if !changed {
		return out
	}
	restored := bytes.Join(lines, []byte("\n"))
	if !sameValues(out, restored) {
		return out
	}
	return restored
}

// sameValues reports whether two encodings of a stream decode to the same
// sequence of values. Comments and layout are not compared, which is the point:
// this is asked whether a change to the text changed what the text means.
//
// A stream that will not decode answers no. Neither side is user input at this
// point — both are this process's own output — so a decode failure is a reason
// to keep what was already there, not to report an error against the document.
func sameValues(a, b []byte) bool {
	av, ok := decodeValues(a)
	if !ok {
		return false
	}
	bv, ok := decodeValues(b)
	if !ok {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func decodeValues(data []byte) ([]any, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	out := []any{}
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return out, true
		}
		if err != nil {
			return nil, false
		}
		out = append(out, v)
	}
}
