package path_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// TestParseDescend is #174's grammar: `**` is a segment that spans levels
// rather than one level, allowed leading and interior.
func TestParseDescend(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want []path.Segment
	}{
		{"**", []path.Segment{{IsDescend: true}}},
		{"**.image", []path.Segment{{IsDescend: true}, {Key: "image"}}},
		{".**.image", []path.Segment{{IsDescend: true}, {Key: "image"}}},
		{"spec.**.image", []path.Segment{{Key: "spec"}, {IsDescend: true}, {Key: "image"}}},
		{"spec.**", []path.Segment{{Key: "spec"}, {IsDescend: true}}},
		{"**.containers[0].image", []path.Segment{
			{IsDescend: true}, {Key: "containers"}, {Index: 0, IsIndex: true}, {Key: "image"},
		}},
		// One star is still one level, and three is just a key.
		{"*", []path.Segment{{IsWildcard: true}}},
		{"***", []path.Segment{{Key: "***"}}},
	} {
		got, err := path.Parse(tc.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.expr, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", tc.expr, got, tc.want)
		}
	}
}

func TestDescendSegmentString(t *testing.T) {
	if got := (path.Segment{IsDescend: true}).String(); got != "**" {
		t.Errorf("String() = %q, want %q", got, "**")
	}
}

// TestFormatDescendRoundTrips: Parse(Format(segs)) == segs is the property
// `get --paths` depends on, and `**` has to hold it like every other segment.
func TestFormatDescendRoundTrips(t *testing.T) {
	for _, segs := range [][]path.Segment{
		{{IsDescend: true}},
		{{IsDescend: true}, {Key: "image"}},
		{{Key: "spec"}, {IsDescend: true}, {Key: "image"}},
		{{IsDescend: true}, {Index: 2, IsIndex: true}},
	} {
		text := path.Format(segs)
		back, err := path.Parse(text)
		if err != nil {
			t.Errorf("Parse(Format(%+v) = %q): %v", segs, text, err)
			continue
		}
		if !reflect.DeepEqual(back, segs) {
			t.Errorf("round trip of %q: got %+v, want %+v", text, back, segs)
		}
	}
}

// TestLiteralDoubleStarKeyIsQuoted is the breaking half of #174, and the
// reason the change isn't purely additive. A key literally named `**` is
// legal YAML and was addressable bare before this; now that `**` is a token,
// Format has to quote it or a rendered path means something else entirely to
// whatever reads it back — the same seam #157 and #158 were about, one level
// up: not truncated, re-interpreted.
func TestLiteralDoubleStarKeyIsQuoted(t *testing.T) {
	segs := []path.Segment{{Key: "**"}, {Key: "image"}}
	text := path.Format(segs)
	if text == "**.image" {
		t.Fatalf("Format rendered a literal ** key as the descent token: %q", text)
	}
	back, err := path.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	if !reflect.DeepEqual(back, segs) {
		t.Errorf("round trip of %q: got %+v, want %+v", text, back, segs)
	}
}

// TestBracketedDescendIsASyntaxError: `[**]` has no meaning, and guessing one
// would be a second spelling to keep in step with the first.
func TestBracketedDescendIsASyntaxError(t *testing.T) {
	_, err := path.Parse("a[**]")
	if err == nil {
		t.Fatal("want a syntax error for a[**]")
	}
	var se *path.SyntaxError
	if !errors.As(err, &se) {
		t.Errorf("error %v is not a *path.SyntaxError", err)
	}
}
