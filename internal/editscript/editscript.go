// Package editscript parses yaymlq apply's batch-edit format: one operation
// per line, run in order against the same document before a single
// encode/write, instead of one parse/serialize cycle per edit.
package editscript

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Verb names one of the four editing operations a script line can perform —
// the same four verbs as yaymlq's own set/append/delete/rename commands.
type Verb string

// The four operations a script line can perform.
const (
	Set    Verb = "set"
	Append Verb = "append"
	Delete Verb = "delete"
	Rename Verb = "rename"
)

// Op is one parsed script line.
//
// Path is the raw path expression text — Parse doesn't call internal/path
// itself, the same way the caller already parses set/append/delete/rename's
// own <path> argument. Value is the raw text after "=": YAML to be parsed
// by ymledit.ParseValue for Set/Append, the literal new key for Rename, and
// unused (empty) for Delete. Line is the 1-indexed source line, so a
// caller's error can point at exactly which op failed.
type Op struct {
	Verb  Verb
	Path  string
	Value string
	Line  int
}

// Parse reads a batch-edit script:
//
//	set <path> = <value>       value is YAML, like set's own <value> argument
//	append <path> = <value>    same
//	delete <path>
//	rename <path> = <newkey>   newkey is literal, like rename's own argument
//
// Blank lines, and lines whose first non-space character is '#', are
// ignored. path and value/newkey split on the line's first "=" — a path
// expression has no legitimate use for that character, so this is
// unambiguous for anything -f/--edits is realistically pointed at.
func Parse(r io.Reader) ([]Op, error) {
	var ops []Op
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // allow a long value on one line
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		op, err := parseLine(line, lineNo)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading edit script: %w", err)
	}
	return ops, nil
}

func parseLine(line string, lineNo int) (Op, error) {
	verbText, rest, ok := strings.Cut(line, " ")
	if !ok {
		return Op{}, fmt.Errorf("line %d: missing path: %q", lineNo, line)
	}
	rest = strings.TrimSpace(rest)

	switch Verb(verbText) {
	case Set:
		p, v, ok := splitPathValue(rest)
		if !ok {
			return Op{}, fmt.Errorf("line %d: set needs \"<path> = <value>\", got %q", lineNo, line)
		}
		return Op{Verb: Set, Path: p, Value: v, Line: lineNo}, nil
	case Append:
		p, v, ok := splitPathValue(rest)
		if !ok {
			return Op{}, fmt.Errorf("line %d: append needs \"<path> = <value>\", got %q", lineNo, line)
		}
		return Op{Verb: Append, Path: p, Value: v, Line: lineNo}, nil
	case Rename:
		p, v, ok := splitPathValue(rest)
		if !ok {
			return Op{}, fmt.Errorf("line %d: rename needs \"<path> = <newkey>\", got %q", lineNo, line)
		}
		return Op{Verb: Rename, Path: p, Value: v, Line: lineNo}, nil
	case Delete:
		if rest == "" {
			return Op{}, fmt.Errorf("line %d: delete needs a path", lineNo)
		}
		return Op{Verb: Delete, Path: rest, Line: lineNo}, nil
	default:
		return Op{}, fmt.Errorf("line %d: unknown verb %q (want set/append/delete/rename)", lineNo, verbText)
	}
}

// splitPathValue splits rest on its first "=", trimming both sides. ok is
// false when there's no "=" at all, or the path half is empty.
func splitPathValue(rest string) (path, value string, ok bool) {
	i := strings.IndexByte(rest, '=')
	if i < 0 {
		return "", "", false
	}
	path = strings.TrimSpace(rest[:i])
	value = strings.TrimSpace(rest[i+1:])
	if path == "" {
		return "", "", false
	}
	return path, value, true
}
