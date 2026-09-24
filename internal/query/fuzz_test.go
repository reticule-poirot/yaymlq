package query_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

// FuzzRun checks that resolving an arbitrary path against a real document never
// panics.
func FuzzRun(f *testing.F) {
	const seedDoc = `
name: yaymlq
nested: {a: {b: hello}}
services:
  - {name: web, ports: [80, 443]}
  - {name: db, ports: [5432]}
`
	var doc any
	if err := yaml.Unmarshal([]byte(seedDoc), &doc); err != nil {
		f.Fatalf("seed doc: %v", err)
	}
	for _, s := range []string{"", "nested.a.b", "services.*.ports[]", "services[99]", "x.y.z", "services.*.*.*"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, expr string) {
		_, _ = query.Run(doc, expr)
	})
}

// FuzzRunMatchesPathsResolveBack asserts the contract `get --paths` sells: a
// listed path, fed back in as a query, resolves to exactly the value it was
// listed for. Unlike FuzzRun this fuzzes the document too, since the paths
// that are hard to render are the ones with awkward keys in them.
func FuzzRunMatchesPathsResolveBack(f *testing.F) {
	seeds := []struct{ src, expr string }{
		{"a: {b: 1, c: 2}", ".a.*"},
		{"a: [{b: 1}, {b: 2}]", ".a[*].b"},
		{`{"odd.key": 1, "7": 2, "*": 3, "": 4}`, ".*"},
		{"a: {b: {c: {d: 1, e: 2}}}", ".a.b.c.*"},
		{"a: 1", "."},
		{"- 1\n- 2\n", "[*]"},
	}
	for _, s := range seeds {
		f.Add(s.src, s.expr)
	}
	f.Fuzz(func(t *testing.T, src, expr string) {
		var doc any
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			return
		}
		matches, err := query.RunMatches(doc, expr)
		if err != nil {
			return
		}

		seen := map[string]int{}
		for _, m := range matches {
			seen[path.Format(m.Path)]++
		}
		for _, m := range matches {
			p := path.Format(m.Path)
			// Two distinct keys can render to the same path — a mapping
			// with both the integer 1 and the string "1" as keys, say —
			// and then no query can tell them apart. Nothing to assert.
			if seen[p] > 1 {
				continue
			}
			if inexpressible(m.Path) {
				continue
			}
			// A NaN never equals itself, so a value holding one can't be
			// compared against a re-resolved copy at all.
			if hasNaN(m.Value) {
				continue
			}
			got, err := query.Run(doc, p)
			if err != nil {
				t.Fatalf("doc %q: path %q of match on %q did not resolve: %v", src, p, expr, err)
			}
			if len(got) != 1 || !reflect.DeepEqual(got[0], m.Value) {
				t.Fatalf("doc %q: path %q resolved to %#v, want the matched value %#v", src, p, got, m.Value)
			}
		}
	})
}

// hasNaN reports whether v is, or contains, a NaN.
func hasNaN(v any) bool {
	switch x := v.(type) {
	case float64:
		return math.IsNaN(x)
	case []any:
		for _, e := range x {
			if hasNaN(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasNaN(e) {
				return true
			}
		}
	case map[any]any:
		for _, e := range x {
			if hasNaN(e) {
				return true
			}
		}
	}
	return false
}

// inexpressible reports whether a trail contains a key the path grammar
// cannot represent: one holding both quote characters, which Format cannot
// quote and Parse has no escape syntax for.
func inexpressible(segs []path.Segment) bool {
	for _, s := range segs {
		if strings.ContainsRune(s.Key, '"') && strings.ContainsRune(s.Key, '\'') {
			return true
		}
	}
	return false
}
