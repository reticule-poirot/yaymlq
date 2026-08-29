package query

import (
	"fmt"
	"strconv"
	"strings"
)

// segment is a single step in a parsed path: a map key, a slice index, or a
// wildcard that fans out over every child of a mapping or list.
type segment struct {
	key        string
	index      int
	isIndex    bool
	isWildcard bool
}

func (s segment) String() string {
	switch {
	case s.isWildcard:
		return "*"
	case s.isIndex:
		return fmt.Sprintf("[%d]", s.index)
	default:
		return s.key
	}
}

// parsePath turns a path expression into an ordered list of segments.
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
// An empty path (or ".") selects the whole document.
func parsePath(expr string) ([]segment, error) {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, ".")
	if expr == "" {
		return nil, nil
	}

	var (
		segs   []segment
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
			segs = append(segs, segment{key: tok})
			return
		}
		if tok == "*" {
			segs = append(segs, segment{isWildcard: true})
			return
		}
		if n, err := strconv.Atoi(tok); err == nil {
			segs = append(segs, segment{index: n, isIndex: true})
			return
		}
		segs = append(segs, segment{key: tok})
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
				segs = append(segs, segment{isWildcard: true})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("invalid array index %q in path %q", inner, expr)
				}
				segs = append(segs, segment{index: n, isIndex: true})
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
