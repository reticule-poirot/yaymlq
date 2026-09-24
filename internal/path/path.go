// Package path parses yaymlq path expressions into an ordered list of segments.
//
// It is shared by the query engine (internal/query) and the editor
// (internal/ymledit) so both understand exactly the same syntax.
package path

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SyntaxError marks a Parse failure as being about the path expression's own
// syntax (an unterminated bracket or quote, a bad index, invalid UTF-8) —
// distinct from a failure to resolve that path against data, which Parse
// never produces. Callers use errors.As to tell the two apart, e.g. to map a
// bad expression to a usage-class exit code.
type SyntaxError struct{ err error }

func (e *SyntaxError) Error() string { return e.err.Error() }
func (e *SyntaxError) Unwrap() error { return e.err }

func syntaxErrorf(format string, args ...any) error {
	return &SyntaxError{fmt.Errorf(format, args...)}
}

// Segment is a single step in a parsed path: a map key, a slice index, or a
// wildcard that fans out over every child of a mapping or list.
type Segment struct {
	Key        string
	Index      int
	IsIndex    bool
	IsWildcard bool
}

// String renders a single segment.
func (s Segment) String() string {
	switch {
	case s.IsWildcard:
		return "*"
	case s.IsIndex:
		return fmt.Sprintf("[%d]", s.Index)
	default:
		return s.Key
	}
}

// Format renders a segment trail as a path expression like `a.b[0].c`.
//
// The output is meant to be fed back to Parse — that round trip is what
// `get --paths` exists for — so a key whose literal text would parse as
// something else is quoted (see quoteKey). Segment.String is the
// unquoted display form and stays as it is.
func Format(segs []Segment) string {
	var b []byte
	for _, s := range segs {
		if s.IsIndex {
			b = append(b, s.String()...)
			continue
		}
		if len(b) > 0 {
			b = append(b, '.')
		}
		if s.IsWildcard {
			b = append(b, s.String()...)
			continue
		}
		b = append(b, quoteKey(s.Key)...)
	}
	if len(b) == 0 {
		return "."
	}
	return string(b)
}

// quoteKey renders a map key as path-expression text, wrapping it in quotes
// when its bare form would parse as something other than that key: a `.` or
// `[` splits it across segments, a lone `*` becomes a wildcard, an integer
// becomes an index, surrounding spaces are trimmed off, and an empty key
// disappears entirely. Either quote character inside the key also forces
// quoting, since Parse treats one as the start of a quoted run.
//
// A key containing both quote characters cannot be expressed at all — the
// grammar has no escape syntax — so it comes back double-quoted and does
// not round-trip. That is a limitation of Parse, not of this function.
func quoteKey(key string) string {
	if !needsQuoting(key) {
		return key
	}
	if strings.ContainsRune(key, '"') {
		return "'" + key + "'"
	}
	return `"` + key + `"`
}

func needsQuoting(key string) bool {
	if key == "" || key == "*" || strings.TrimSpace(key) != key {
		return true
	}
	if strings.ContainsAny(key, `.["'`) {
		return true
	}
	_, err := strconv.Atoi(key)
	return err == nil
}

// Parse turns a path expression into an ordered list of segments.
//
// Supported syntax:
//
//	.a.b.c      map keys, leading dot optional
//	a.b.c       same as above
//	a[0].b      bracketed slice index
//	a.0.b       bare numeric segment is treated as a slice index
//	a.*.b       wildcard: every value of a mapping or list
//	a[].b       wildcard, jq-style
//	a[*].b      wildcard
//	"a.b".c     quoted segment containing a literal dot (never a wildcard/index)
//
// An empty path (or ".") returns no segments, which callers treat as "the whole
// document".
func Parse(expr string) ([]Segment, error) {
	if !utf8.ValidString(expr) {
		return nil, syntaxErrorf("path %q is not valid UTF-8", expr)
	}
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, ".")
	if expr == "" {
		return nil, nil
	}

	var (
		segs   []Segment
		buf    strings.Builder
		quoted bool
		i      int
	)

	flush := func() {
		if buf.Len() == 0 && !quoted {
			return
		}
		tok := buf.String()
		buf.Reset()
		wasQuoted := quoted
		quoted = false

		if wasQuoted {
			segs = append(segs, Segment{Key: tok})
			return
		}
		if tok == "*" {
			segs = append(segs, Segment{IsWildcard: true})
			return
		}
		if n, err := strconv.Atoi(tok); err == nil {
			segs = append(segs, Segment{Index: n, IsIndex: true})
			return
		}
		segs = append(segs, Segment{Key: tok})
	}

	for i < len(expr) {
		c := expr[i]
		switch c {
		case '.':
			flush()
			i++
		case '[':
			flush()
			end := strings.IndexByte(expr[i:], ']')
			if end < 0 {
				return nil, syntaxErrorf("unterminated '[' in path %q", expr)
			}
			inner := strings.TrimSpace(expr[i+1 : i+end])
			switch inner {
			case "", "*":
				segs = append(segs, Segment{IsWildcard: true})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					return nil, syntaxErrorf("invalid array index %q in path %q", inner, expr)
				}
				segs = append(segs, Segment{Index: n, IsIndex: true})
			}
			i += end + 1
			if i < len(expr) && expr[i] == '.' {
				i++
			}
		case '"', '\'':
			quote := c
			quoted = true
			i++
			start := i
			for i < len(expr) && expr[i] != quote {
				i++
			}
			if i >= len(expr) {
				return nil, syntaxErrorf("unterminated %c-quote in path %q", quote, expr)
			}
			buf.WriteString(expr[start:i])
			i++
		default:
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return segs, nil
}
