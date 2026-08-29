package query

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// FuzzParsePath checks that the path parser never panics and stays within sane
// bounds. (The fuzzing harness fails the test automatically on any panic.)
func FuzzParsePath(f *testing.F) {
	for _, s := range []string{"", ".", "a.b.c", "a[0].b", "a.*.b", "a[].b", `"a.b".c`, "[-1]", "a[", `'x`, `""`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		segs, err := parsePath(expr)
		if err != nil {
			return
		}
		if len(segs) > len(expr)+1 {
			t.Fatalf("parsePath(%q) produced %d segments, more than the input length", expr, len(segs))
		}
		for _, s := range segs {
			if s.isWildcard && (s.isIndex || s.key != "") {
				t.Fatalf("parsePath(%q) produced a malformed wildcard segment: %#v", expr, s)
			}
		}
	})
}

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
	f.Fuzz(func(t *testing.T, expr string) {
		_, _ = Run(doc, expr)
	})
}
