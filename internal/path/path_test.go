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
		{"interior space", []path.Segment{{Key: "two words"}}, `"two words"`},
		// A path is consumed line by line and word by word — an apply
		// script splits on " = ", xargs on whitespace — so any whitespace
		// in a key is quoted, not just the kind Parse itself would trim.
		// TestFormatEscapesWhatItMustQuote covers the whitespace that is
		// also escaped (tab, newline, carriage return).
		{"apply's separator", []path.Segment{{Key: "a = b"}}, `"a = b"`},
		{"bare equals", []path.Segment{{Key: "a=b"}}, `"a=b"`},
		{"key that is only an equals", []path.Segment{{Key: "="}}, `"="`},
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
// Since #157 there is nothing to exclude: escapes inside a quoted segment
// mean every key can be rendered and read back, line breaks and both quote
// characters included.
func TestFormatRoundTripsThroughParse(t *testing.T) {
	trails := [][]path.Segment{
		{{Key: "a"}, {Key: "b"}},
		{{Key: "a.b"}, {Key: "c"}},
		{{Key: "jobs"}, {Key: "test"}, {Key: "steps"}, {Index: 1, IsIndex: true}, {Key: "with"}, {Key: "go-version"}},
		{{Key: "*"}},
		{{Key: "7"}, {Index: 0, IsIndex: true}},
		{{Key: " a "}},
		{{Key: "a = b"}, {Key: "c"}},
		{{Key: "a=b"}},
		{{Key: "two words"}},
		{{Key: ""}},
		{{Key: `say "hi"`}},
		{{Key: "it's"}},
		{{Key: `it's "both"`}},
		{{Key: "a\nb"}},
		{{Key: `back\slash`}},
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

// TestParseEscapesInsideQuotes: a quoted run ends at the first matching quote
// character, which left a key holding that character — or a newline, which
// can't be written on one line — unaddressable. A backslash inside a quoted
// run now escapes the next character.
func TestParseEscapesInsideQuotes(t *testing.T) {
	tests := []struct {
		name, expr string
		want       []path.Segment
	}{
		{"escaped double quote", `"say \"hi\""`, []path.Segment{{Key: `say "hi"`}}},
		{"escaped single quote", `'it\'s'`, []path.Segment{{Key: "it's"}}},
		{"both quote characters", `"it's \"fine\""`, []path.Segment{{Key: `it's "fine"`}}},
		{"newline", `"a\nb"`, []path.Segment{{Key: "a\nb"}}},
		{"tab", `"a\tb"`, []path.Segment{{Key: "a\tb"}}},
		{"carriage return", `"a\rb"`, []path.Segment{{Key: "a\rb"}}},
		{"literal backslash", `"a\\b"`, []path.Segment{{Key: `a\b`}}},
		{"escape then more segments", `"a\nb".c`, []path.Segment{{Key: "a\nb"}, {Key: "c"}}},
		{"the other quote needs no escape", `"it's"`, []path.Segment{{Key: "it's"}}},
	}
	for _, tc := range tests {
		got, err := path.Parse(tc.expr)
		if err != nil {
			t.Errorf("%s: Parse(%q): %v", tc.name, tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Parse(%q) = %#v, want %#v", tc.name, tc.expr, got, tc.want)
		}
	}
}

// TestParseUnknownEscapeIsASyntaxError: an unrecognised escape is refused
// rather than silently dropping the backslash. This is the breaking half of
// the change — `"a\b"` used to mean the literal three characters — and
// failing loudly is the point: the alternative is a path that quietly
// resolves somewhere else.
func TestParseUnknownEscapeIsASyntaxError(t *testing.T) {
	for _, expr := range []string{`"a\b"`, `"a\ "`, `"\x41"`, `"trailing\"`} {
		_, err := path.Parse(expr)
		var se *path.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("Parse(%q): want a *path.SyntaxError, got %v", expr, err)
		}
	}
}

// TestParseBackslashOutsideQuotesIsLiteral: escapes are a quoted-run feature
// only, so an unquoted key holding a backslash keeps working unchanged.
func TestParseBackslashOutsideQuotesIsLiteral(t *testing.T) {
	got, err := path.Parse(`a\b.c`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []path.Segment{{Key: `a\b`}, {Key: "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %#v, want %#v", got, want)
	}
}

// TestFormatEscapesWhatItMustQuote: the keys that had no representation at
// all now have one, on a single line.
func TestFormatEscapesWhatItMustQuote(t *testing.T) {
	tests := []struct {
		name string
		segs []path.Segment
		want string
	}{
		{"newline", []path.Segment{{Key: "a\nb"}}, `"a\nb"`},
		{"tab", []path.Segment{{Key: "a\tb"}}, `"a\tb"`},
		{"carriage return", []path.Segment{{Key: "a\rb"}}, `"a\rb"`},
		{"backslash", []path.Segment{{Key: `a\b`}}, `"a\\b"`},
		{"both quote characters", []path.Segment{{Key: `it's "fine"`}}, `"it's \"fine\""`},
		// Unchanged from before escapes existed: with only one quote
		// character present, the other one still does the quoting, which
		// reads better than escaping.
		{"double quote only", []path.Segment{{Key: `say "hi"`}}, `'say "hi"'`},
		{"single quote only", []path.Segment{{Key: "it's"}}, `"it's"`},
	}
	for _, tc := range tests {
		if got := path.Format(tc.segs); got != tc.want {
			t.Errorf("%s: Format = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFormatRoundTripsEveryKey is the property #157 was filed to get: there
// is no longer any key that Format can't render and Parse can't read back.
func TestFormatRoundTripsEveryKey(t *testing.T) {
	for _, key := range []string{
		"a\nb", "a\tb", "a\rb", `a\b`, `it's "fine"`, `"`, `'`, `\`, "\n",
		`a\nb`, `"''"`, "a = b", " ", "",
	} {
		want := []path.Segment{{Key: key}}
		expr := path.Format(want)
		if strings.ContainsAny(expr, "\n\r") {
			t.Errorf("Format(%q) = %q, which spans lines", key, expr)
		}
		got, err := path.Parse(expr)
		if err != nil {
			t.Errorf("Parse(Format(%q) = %q): %v", key, expr, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("key %q round-tripped through %q as %#v", key, expr, got)
		}
	}
}
