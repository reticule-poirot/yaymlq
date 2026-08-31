package path_test

import (
	"reflect"
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
	})
}
