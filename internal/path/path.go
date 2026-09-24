// Package path parses yaymlq path expressions into an ordered list of segments.
//
// It is shared by the query engine (internal/query) and the editor
// (internal/ymledit) so both understand exactly the same syntax.
package path

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
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
// becomes an index, and an empty key disappears entirely. Either quote
// character inside the key also forces quoting, since Parse treats one as
// the start of a quoted run.
//
// Whitespace and "=" are quoted too, and those rules are about the
// consumers rather than about Parse, which cares about neither: a rendered
// path is read a line and a word at a time, and an apply script splits an
// op on its first unquoted "=", so an unquoted key holding either was
// silently cut in half downstream and the edit landed on a key that didn't
// exist (#158). cmd's FuzzPathThroughEditScript is what keeps this list
// honest, since it checks a rendered path against the script grammar
// directly rather than against a guess about it.
//
// Every key round-trips: a newline, a backslash, or both quote characters at
// once are escaped (see unescape), so there is no key Format cannot render
// on one line and Parse cannot read back.
func quoteKey(key string) string {
	if !needsQuoting(key) {
		return key
	}
	// With only one of the two quote characters in the key, the other one
	// does the quoting and nothing needs escaping — which reads better, and
	// is what this produced before escapes existed. A key holding both is
	// what escapes are for.
	q := byte('"')
	if strings.ContainsRune(key, '"') && !strings.ContainsRune(key, '\'') {
		q = '\''
	}
	var b strings.Builder
	b.WriteByte(q)
	for i := 0; i < len(key); i++ {
		switch c := key[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case q:
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(q)
	return b.String()
}

func needsQuoting(key string) bool {
	if key == "" || key == "*" {
		return true
	}
	if strings.ContainsRune(key, '=') || strings.ContainsFunc(key, unicode.IsSpace) {
		return true
	}
	// A backslash is only an escape inside quotes, so an unquoted one is
	// already literal — but quoting it (and escaping it) is what keeps
	// Format's output readable back as the same key regardless of which
	// side of a quote it lands on.
	if strings.ContainsRune(key, '\\') {
		return true
	}
	if strings.ContainsAny(key, `.["'`) {
		return true
	}
	_, err := strconv.Atoi(key)
	return err == nil
}

// unescape maps the character after a backslash inside a quoted segment to
// the byte it stands for. An unrecognised one is rejected rather than read
// as itself: silently dropping the backslash would turn a typo into a path
// that resolves somewhere else, and refusing leaves room to add escapes
// later without changing what an existing path means.
func unescape(c byte) (byte, bool) {
	switch c {
	case '\\', '"', '\'':
		return c, true
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	}
	return 0, false
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
//	"a\nb"      inside quotes, \\ escapes: \\ \" \' \n \t \r
//	            (outside quotes a backslash is an ordinary character)
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
			for i < len(expr) && expr[i] != quote {
				if expr[i] != '\\' {
					buf.WriteByte(expr[i])
					i++
					continue
				}
				if i+1 >= len(expr) {
					return nil, syntaxErrorf("path %q ends with a trailing backslash", expr)
				}
				lit, ok := unescape(expr[i+1])
				if !ok {
					return nil, syntaxErrorf(`unknown escape "\%c" in path %q (want \\ \" \' \n \t \r)`, expr[i+1], expr)
				}
				buf.WriteByte(lit)
				i += 2
			}
			if i >= len(expr) {
				return nil, syntaxErrorf("unterminated %c-quote in path %q", quote, expr)
			}
			i++
		default:
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return segs, nil
}
