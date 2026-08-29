package query

import (
	"fmt"
	"strconv"
	"strings"
)

// segment is a single step in a parsed path: either a map key or a slice index.
type segment struct {
	key     string
	index   int
	isIndex bool
}

func (s segment) String() string {
	if s.isIndex {
		return fmt.Sprintf("[%d]", s.index)
	}
	return s.key
}

// parsePath turns a path expression into an ordered list of segments.
//
// Supported syntax:
//
//	.a.b.c      map keys, leading dot optional
//	a.b.c       same as above
//	a[0].b      bracketed slice index
//	a.0.b       bare numeric segment is treated as a slice index
//	"a.b".c     quoted segment containing a literal dot
//
// An empty path (or ".") selects the whole document.
func parsePath(expr string) ([]segment, error) {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, ".")
	if expr == "" {
		return nil, nil
	}

	var (
		segs []segment
		buf  strings.Builder
		i    int
	)

	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		tok := buf.String()
		buf.Reset()
		if n, err := strconv.Atoi(tok); err == nil {
			segs = append(segs, segment{index: n, isIndex: true})
			return nil
		}
		segs = append(segs, segment{key: tok})
		return nil
	}

	for i < len(expr) {
		c := expr[i]
		switch c {
		case '.':
			if err := flush(); err != nil {
				return nil, err
			}
			i++
		case '[':
			if err := flush(); err != nil {
				return nil, err
			}
			end := strings.IndexByte(expr[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unterminated '[' in path %q", expr)
			}
			inner := expr[i+1 : i+end]
			n, err := strconv.Atoi(strings.TrimSpace(inner))
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q in path %q", inner, expr)
			}
			segs = append(segs, segment{index: n, isIndex: true})
			i += end + 1
			if i < len(expr) && expr[i] == '.' {
				i++
			}
		case '"', '\'':
			quote := c
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
	if err := flush(); err != nil {
		return nil, err
	}
	return segs, nil
}
