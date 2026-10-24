package query_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

// A top-level `image`, one nested two levels down, and one inside a list —
// the three depths `**` has to reach, plus an `image` nested *inside* an
// `image` mapping, which is the "report both" case.
const descendDoc = `
image: top
spec:
  image: mid
  containers:
    - name: web
      image: deep
    - name: db
nest:
  image:
    image: inner
`

func descendMustDoc(t *testing.T) any {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal([]byte(descendDoc), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return doc
}

// TestRunDescend is #174: `**` matches at any depth, this level included, so
// `**.image` is a strict superset of `.image`. Order is document order with
// mappings sorted by key — the rule single-level wildcards already follow —
// and the current node comes before its children, so a match at depth 0
// precedes the ones beneath it.
func TestRunDescend(t *testing.T) {
	doc := descendMustDoc(t)
	got, err := query.Run(doc, "**.image")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []any{
		"top",                            // depth 0 — included
		map[string]any{"image": "inner"}, // nest.image, a mapping
		"inner",                          // nest.image.image — reported too
		"mid",                            // spec.image
		"deep",                           // spec.containers[0].image
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Run(**.image) =\n  %#v\nwant\n  %#v", got, want)
	}
}

// TestRunDescendIsASupersetOfTheConcretePath states the depth-0 rule as the
// property that makes it easy to explain: anything `.x` finds, `**.x` finds.
func TestRunDescendIsASupersetOfTheConcretePath(t *testing.T) {
	doc := descendMustDoc(t)
	one, err := query.Run(doc, ".image")
	if err != nil {
		t.Fatalf("Run(.image): %v", err)
	}
	all, err := query.Run(doc, "**.image")
	if err != nil {
		t.Fatalf("Run(**.image): %v", err)
	}
	for _, v := range one {
		if !containsValue(all, v) {
			t.Errorf("**.image is missing %#v, which .image found", v)
		}
	}
	if len(all) <= len(one) {
		t.Errorf("**.image found %d, .image found %d; want strictly more", len(all), len(one))
	}
}

func containsValue(haystack []any, needle any) bool {
	for _, h := range haystack {
		if reflect.DeepEqual(h, needle) {
			return true
		}
	}
	return false
}

// TestRunDescendInterior: `spec.**.image` is anchored, so the top-level image
// is out of scope and only what lies under spec matches.
func TestRunDescendInterior(t *testing.T) {
	doc := descendMustDoc(t)
	got, err := query.Run(doc, "spec.**.image")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []any{"mid", "deep"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestRunDescendTrailing: `**` last means "this node and every node beneath
// it", which is the natural reading of a segment that matches at every depth.
func TestRunDescendTrailing(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("a:\n  b: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.Run(doc, "a.**")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []any{map[string]any{"b": 1}, 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestRunDescendOverALists: descent walks list elements too, not just
// mappings, or `**.image` would miss every container.
func TestRunDescendOverALists(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("- image: a\n- nested:\n    image: b\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.Run(doc, "**.image")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []any{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestRunDescendNoMatchIsNotAnError: descent fans out, so it follows the
// wildcard contract — zero matches is an empty result, not ErrNotFound. cmd
// is what tells the caller about it (#173).
func TestRunDescendNoMatchIsNotAnError(t *testing.T) {
	doc := descendMustDoc(t)
	got, err := query.Run(doc, "**.nope")
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no results, got %#v", got)
	}
	if errors.Is(err, query.ErrNotFound) {
		t.Error("descent must not report ErrNotFound for zero matches")
	}
}

// TestRunDescendOverAScalarIsEmpty: nothing to descend into is zero matches,
// the same as a wildcard over a scalar once it is in lenient position.
func TestRunDescendOverAScalarIsEmpty(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("a: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.Run(doc, "a.**.b")
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no results, got %#v", got)
	}
}

// TestRunMatchesDescendPathsAreConcrete is what makes descent usable with
// apply: every resolved path has the `**` replaced by the keys and indices it
// actually went through, so it parses back and addresses one node.
func TestRunMatchesDescendPathsAreConcrete(t *testing.T) {
	doc := descendMustDoc(t)
	matches, err := query.RunMatches(doc, "**.image")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no matches")
	}
	for _, m := range matches {
		for _, seg := range m.Path {
			if seg.IsDescend || seg.IsWildcard {
				t.Fatalf("resolved path %q still contains a fan-out segment", path.Format(m.Path))
			}
		}
		// And it resolves back to exactly the value it came from.
		back, err := query.Run(doc, path.Format(m.Path))
		if err != nil {
			t.Errorf("Run(%q): %v", path.Format(m.Path), err)
			continue
		}
		if len(back) != 1 || !reflect.DeepEqual(back[0], m.Value) {
			t.Errorf("%q resolved back to %#v, want %#v", path.Format(m.Path), back, m.Value)
		}
	}
}

// TestRunMatchesDescendReachesTheExpectedPaths pins the trails, since the
// values alone can't show that depth 0 and the nested case are both covered.
func TestRunMatchesDescendReachesTheExpectedPaths(t *testing.T) {
	doc := descendMustDoc(t)
	matches, err := query.RunMatches(doc, "**.image")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, path.Format(m.Path))
	}
	want := []string{
		"image",
		"nest.image",
		"nest.image.image",
		"spec.image",
		"spec.containers[0].image",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths =\n  %q\nwant\n  %q", got, want)
	}
}

// TestRunDescendAddressesALiteralDoubleStarKey: `**` is now a token, so the
// only way to reach a key actually named `**` is the quoted form — which is
// what path.Format emits for it.
func TestRunDescendAddressesALiteralDoubleStarKey(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("\"**\":\n  image: odd\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.Run(doc, `"**".image`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []any{"odd"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestRunDescendOverNonStringKeys: yaml.v3 decodes a mapping into
// map[any]any as soon as it has one non-string key, and descent has to walk
// that shape too — otherwise `**` silently stops at the first mapping keyed
// by an int or a bool, which is a plausible shape in a config file.
func TestRunDescendOverNonStringKeys(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("outer:\n  1: {image: one}\n  true: {image: two}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.Run(doc, "**.image")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Sorted by each key's string form, the order a wildcard already uses.
	if want := []any{"one", "two"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	// And the resolved paths address those keys in their string form.
	matches, err := query.RunMatches(doc, "**.image")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	trails := make([]string, 0, len(matches))
	for _, m := range matches {
		trails = append(trails, path.Format(m.Path))
	}
	if want := []string{`outer."1".image`, "outer.true.image"}; !reflect.DeepEqual(trails, want) {
		t.Errorf("paths = %q, want %q", trails, want)
	}
}
