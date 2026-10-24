package path_test

import (
	"reflect"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// fanningExprs are the spellings that match more than one node, and
// plainExprs the ones that match at most one. Between them they are meant to
// name every segment kind Parse can produce — see
// TestEverySegmentKindIsClassifiedAsFanningOrNot, which fails if a new one
// appears in neither.
//
// The quoted entries in plainExprs are the point of the split: `"*"` and
// `"**"` are legal YAML keys, and a path that fans out means something quite
// different from a path naming a key spelled that way (#157/#158).
var (
	fanningExprs = []string{".*", "a.*", "a.*.b", "a[]", "a[*]", "**", "**.b", "a.**", "a.**.b"}
	plainExprs   = []string{"", ".", ".a", "a.b.c", "a[0]", "a[-1]", "a.0.b", `"a.b".c`, `"*"`, `"**"`, `"*".b`}
)

// TestFansMatchesTheSpellingsThatFanOut is #207. The editing verbs refuse a
// fan-out by testing one predicate, Segment.Fans(), rather than each kind
// separately — CLAUDE.md states that as the reason "a new fan-out kind can't
// be accepted by default". Nothing held the predicate to the parser, though:
// Fans() had no caller and no test in this package, and every test naming a
// fan-out named `*` or `**` specifically.
//
// Asserting Fans() against its own body would prove nothing (it would restate
// `IsWildcard || IsDescend` and pass by construction). What is worth pinning
// is the mapping from the spellings Parse accepts to the answer, so a new
// kind is a row here and a decision rather than a silent default.
func TestFansMatchesTheSpellingsThatFanOut(t *testing.T) {
	for _, expr := range fanningExprs {
		segs, err := path.Parse(expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
			continue
		}
		if countFanning(segs) == 0 {
			t.Errorf("Parse(%q) = %#v: no segment reports Fans(), so an editing verb would accept it", expr, segs)
		}
	}

	for _, expr := range plainExprs {
		segs, err := path.Parse(expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
			continue
		}
		if n := countFanning(segs); n != 0 {
			t.Errorf("Parse(%q) = %#v: %d segment(s) report Fans(), so an editing verb would refuse a path that names one node", expr, segs, n)
		}
	}
}

// TestEverySegmentKindIsClassifiedAsFanningOrNot is the forward-looking half.
// A third fan-out kind — a slice expression, a filter, a union — wired
// through Parse but not into Fans() would be accepted by every editing verb,
// and no existing test would notice, because they all name `*` or `**`.
//
// This fails instead: it requires every boolean a Segment can carry to be set
// by some spelling in the two tables above, so a new kind cannot be added
// without a row, and a row forces the choice of which table it belongs in.
func TestEverySegmentKindIsClassifiedAsFanningOrNot(t *testing.T) {
	exercised := map[string]bool{}
	for _, expr := range append(append([]string{}, fanningExprs...), plainExprs...) {
		segs, err := path.Parse(expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
			continue
		}
		for _, seg := range segs {
			v := reflect.ValueOf(seg)
			for i := range v.NumField() {
				if v.Field(i).Kind() == reflect.Bool && v.Field(i).Bool() {
					exercised[v.Type().Field(i).Name] = true
				}
			}
		}
	}

	st := reflect.TypeOf(path.Segment{})
	for i := range st.NumField() {
		f := st.Field(i)
		if f.Type.Kind() != reflect.Bool {
			continue
		}
		if !exercised[f.Name] {
			t.Errorf("no spelling in fanningExprs or plainExprs produces a segment with %s set: "+
				"a new segment kind needs a row in one of them, and which one is the decision about Fans()", f.Name)
		}
	}
}

func countFanning(segs []path.Segment) int {
	n := 0
	for _, seg := range segs {
		if seg.Fans() {
			n++
		}
	}
	return n
}
