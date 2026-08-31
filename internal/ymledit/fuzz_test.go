package ymledit_test

import (
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"gopkg.in/yaml.v3"
)

const fuzzSeedDoc = `
name: demo
meta:
  labels: {app: api, tier: backend}
  ports: [80, 443, 8080]
nested:
  a: {b: {c: deep}}
list:
  - {id: 1, enabled: true}
  - {id: 2, enabled: false}
`

// freshSeed returns a newly decoded copy of fuzzSeedDoc; Set and Delete mutate
// the tree in place, so every fuzz iteration needs its own.
func freshSeed(t *testing.T) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fuzzSeedDoc), &doc); err != nil {
		t.Fatalf("seed doc: %v", err)
	}
	return &doc
}

// FuzzSet checks that setting an arbitrary path/value into a real document never
// panics, and that a successful edit leaves a tree that still re-encodes.
func FuzzSet(f *testing.F) {
	seeds := []struct{ expr, val string }{
		{".name", "changed"},
		{".meta.labels.app", "web"},
		{".meta.ports[0]", "9090"},
		{".meta.ports[-1]", "0"},
		{".nested.a.b.c", "x"},
		{".brand.new.key", "42"},
		{".list[1].enabled", "true"},
		{"", "x"},
		{".meta.*", "x"},
		{"a[", "x"},
	}
	for _, s := range seeds {
		f.Add(s.expr, s.val)
	}

	f.Fuzz(func(t *testing.T, expr, val string) {
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		value, err := ymledit.ParseValue(val, false)
		if err != nil {
			return
		}
		doc := freshSeed(t)
		if err := ymledit.Set(doc, segs, value); err != nil {
			return
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("Set(%q, %q) produced a tree that will not encode: %v", expr, val, err)
		}
	})
}

// FuzzAppend checks the same for list appends.
func FuzzAppend(f *testing.F) {
	seeds := []struct{ expr, val string }{
		{".meta.ports", "9090"},
		{".meta.ports", "{a: 1}"},
		{".list", "{id: 3}"},
		{".list[0]", "x"},
		{".name", "x"},
		{".meta", "x"},
		{".missing", "x"},
		{"", "x"},
		{".meta.*", "x"},
	}
	for _, s := range seeds {
		f.Add(s.expr, s.val)
	}

	f.Fuzz(func(t *testing.T, expr, val string) {
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		value, err := ymledit.ParseValue(val, false)
		if err != nil {
			return
		}
		doc := freshSeed(t)
		if err := ymledit.Append(doc, segs, value); err != nil {
			return
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("Append(%q, %q) produced a tree that will not encode: %v", expr, val, err)
		}
	})
}

// FuzzDelete checks the same for removals.
func FuzzDelete(f *testing.F) {
	for _, expr := range []string{
		".name", ".meta.labels.app", ".meta.ports[0]", ".meta.ports[-1]",
		".nested.a.b", ".list[0]", ".missing", "", ".meta.*", "a[",
	} {
		f.Add(expr)
	}

	f.Fuzz(func(t *testing.T, expr string) {
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		doc := freshSeed(t)
		if err := ymledit.Delete(doc, segs); err != nil {
			return
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("Delete(%q) produced a tree that will not encode: %v", expr, err)
		}
	})
}
