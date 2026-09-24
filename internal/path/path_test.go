package path_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

func TestParse(t *testing.T) {
	tests := []struct {
		expr string
		want []path.Segment
	}{
		{"", nil},
		{".", nil},
		{"a.b", []path.Segment{{Key: "a"}, {Key: "b"}}},
		{".a.b", []path.Segment{{Key: "a"}, {Key: "b"}}},
		{"a[0].b", []path.Segment{{Key: "a"}, {Index: 0, IsIndex: true}, {Key: "b"}}},
		{"a.2", []path.Segment{{Key: "a"}, {Index: 2, IsIndex: true}}},
		{"a[-1]", []path.Segment{{Key: "a"}, {Index: -1, IsIndex: true}}},
		{"a.*.b", []path.Segment{{Key: "a"}, {IsWildcard: true}, {Key: "b"}}},
		{"a[].b", []path.Segment{{Key: "a"}, {IsWildcard: true}, {Key: "b"}}},
		{"a[*]", []path.Segment{{Key: "a"}, {IsWildcard: true}}},
		{`"a.b".c`, []path.Segment{{Key: "a.b"}, {Key: "c"}}},
		{`"*"`, []path.Segment{{Key: "*"}}},
		{`"7"`, []path.Segment{{Key: "7"}}},
	}
	for _, tc := range tests {
		got, err := path.Parse(tc.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, expr := range []string{"a[", "a[x]", `"unterminated`, "a[1", "\xd9", "a.\xff.b"} {
		if _, err := path.Parse(expr); err == nil {
			t.Errorf("Parse(%q): expected error", expr)
		}
	}
}

func TestParseErrorsAreSyntaxErrors(t *testing.T) {
	for _, expr := range []string{"a[", "a[x]", `"unterminated`, "a[1", "\xd9", "a.\xff.b"} {
		_, err := path.Parse(expr)
		var se *path.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("Parse(%q): error %v is not a *path.SyntaxError", expr, err)
		}
	}
}

func TestFormat(t *testing.T) {
	segs, _ := path.Parse("a.b[2].c")
	if got := path.Format(segs); got != "a.b[2].c" {
		t.Errorf("Format = %q, want %q", got, "a.b[2].c")
	}
	if got := path.Format(nil); got != "." {
		t.Errorf("Format(nil) = %q, want %q", got, ".")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", ".", "a.b.c", "a[0].b", "a.*.b", "a[].b", `"a.b".c`, "[-1]", "a[", `'x`, `""`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		if len(segs) > len(expr)+1 {
			t.Fatalf("Parse(%q) produced %d segments, more than the input length", expr, len(segs))
		}
		for _, s := range segs {
			if s.IsWildcard && (s.IsIndex || s.Key != "") {
				t.Fatalf("Parse(%q) produced a malformed wildcard segment: %#v", expr, s)
			}
			// A key holding both quote characters is inexpressible: the
			// grammar has no escape syntax, so Format cannot round-trip it
			// and the property below does not apply.
			if strings.ContainsRune(s.Key, '"') && strings.ContainsRune(s.Key, '\'') {
				return
			}
		}
		// Format's output is a path expression, so re-parsing it must give
		// the same trail back — the property `get --paths` depends on to
		// feed its output into `apply`.
		again, err := path.Parse(path.Format(segs))
		if err != nil {
			t.Fatalf("Parse(Format(Parse(%q)) = %q): %v", expr, path.Format(segs), err)
		}
		if !reflect.DeepEqual(again, segs) {
			t.Fatalf("Parse(%q) = %#v, but round trip through Format(%q) = %#v", expr, segs, path.Format(segs), again)
		}
	})
}

// TestFormatQuotesAmbiguousKeys pins the cases where a key's literal text
// would re-parse as something other than that key: a dot or bracket splits
// it into several segments, a bare `*` becomes a wildcard, a bare number
// becomes an index, and surrounding whitespace is trimmed away.
func TestFormatQuotesAmbiguousKeys(t *testing.T) {
	tests := []struct {
		name string
		segs []path.Segment
		want string
	}{
		{"dot in key", []path.Segment{{Key: "a.b"}, {Key: "c"}}, `"a.b".c`},
		{"bracket in key", []path.Segment{{Key: "a[0]"}}, `"a[0]"`},
		{"star as key", []path.Segment{{Key: "*"}}, `"*"`},
		{"number as key", []path.Segment{{Key: "7"}}, `"7"`},
		{"negative number as key", []path.Segment{{Key: "-1"}}, `"-1"`},
		{"empty key", []path.Segment{{Key: "a"}, {Key: ""}}, `a.""`},
		{"padded key", []path.Segment{{Key: " a "}}, `" a "`},
		{"double quote in key", []path.Segment{{Key: `say "hi"`}}, `'say "hi"'`},
		{"single quote in key", []path.Segment{{Key: "it's"}}, `"it's"`},
		{"plain key needs nothing", []path.Segment{{Key: "plain"}, {Index: 2, IsIndex: true}}, "plain[2]"},
		{"wildcard segment is not a key", []path.Segment{{Key: "a"}, {IsWildcard: true}}, "a.*"},
	}
	for _, tc := range tests {
		if got := path.Format(tc.segs); got != tc.want {
			t.Errorf("%s: Format = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFormatRoundTripsThroughParse is the property --paths depends on:
// feeding Format's output back to Parse has to give the same trail back,
// or a path list can't be fed to `apply`.
//
// A key containing both a single and a double quote is excluded: the path
// grammar has no escape syntax, so such a key is currently inexpressible —
// a pre-existing Parse limitation, not one Format introduces.
func TestFormatRoundTripsThroughParse(t *testing.T) {
	trails := [][]path.Segment{
		{{Key: "a"}, {Key: "b"}},
		{{Key: "a.b"}, {Key: "c"}},
		{{Key: "jobs"}, {Key: "test"}, {Key: "steps"}, {Index: 1, IsIndex: true}, {Key: "with"}, {Key: "go-version"}},
		{{Key: "*"}},
		{{Key: "7"}, {Index: 0, IsIndex: true}},
		{{Key: " a "}},
		{{Key: ""}},
		{{Key: `say "hi"`}},
		{{Key: "it's"}},
		{{Key: "a"}, {Index: -1, IsIndex: true}},
	}
	for _, want := range trails {
		expr := path.Format(want)
		got, err := path.Parse(expr)
		if err != nil {
			t.Errorf("Parse(Format(%#v) = %q): %v", want, expr, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip of %#v through %q = %#v", want, expr, got)
		}
	}
}
