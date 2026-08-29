// Package path parses yaymlq path expressions into an ordered list of segments.
//
// It is shared by the query engine (internal/query) and the editor
// (internal/ymledit) so both understand exactly the same syntax.
package path

import (
	"fmt"
	"strconv"
	"strings"
)

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

// Format renders a segment trail as a readable path like `a.b[0].c`.
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
		b = append(b, s.String()...)
	}
	if len(b) == 0 {
		return "."
	}
	return string(b)
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
				return nil, fmt.Errorf("unterminated '[' in path %q", expr)
			}
			inner := strings.TrimSpace(expr[i+1 : i+end])
			switch inner {
			case "", "*":
				segs = append(segs, Segment{IsWildcard: true})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("invalid array index %q in path %q", inner, expr)
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
				return nil, fmt.Errorf("unterminated %c-quote in path %q", quote, expr)
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
