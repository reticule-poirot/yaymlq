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
		if err := ymledit.Set(doc, segs, value, nil); err != nil {
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
		if err := ymledit.Append(doc, segs, value, nil); err != nil {
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
		if err := ymledit.Delete(doc, segs, nil); err != nil {
			return
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("Delete(%q) produced a tree that will not encode: %v", expr, err)
		}
	})
}

// FuzzEditIndexMatchesUncached is a differential test: it runs the same
// short sequence of Set/Delete/Append/Rename ops against two fresh copies
// of the seed document — one sharing a single *EditIndex across every op
// (apply's batch path), one passing nil each time (today's per-call
// behavior, already exercised by FuzzSet/FuzzDelete/FuzzAppend/FuzzRename
// above) — and requires them to agree at every step: the same error (or
// both nil), and the same resulting document once every op has run. This
// is the strongest correctness check for EditIndex: it doesn't need to
// know what "correct" looks like in the abstract, only that caching state
// across a batch of ops must never change the outcome from not caching it.
func FuzzEditIndexMatchesUncached(f *testing.F) {
	f.Add([]byte{0, 0, 1, 1, 2, 3, 3, 0})
	f.Add([]byte{2, 5, 2, 5, 0, 5}) // delete then re-set the same key
	f.Add([]byte{0, 6, 0, 6, 0, 6}) // set the same new key repeatedly
	f.Add([]byte{1, 2, 0, 2})       // append into a list, then set through it

	// A small fixed vocabulary of paths/values/keys, reused by index (mod
	// length) — keeps the fuzzer's job "pick a short sequence of (verb,
	// operand) pairs" instead of also having to discover valid YAML syntax
	// from scratch.
	paths := []string{
		".name", ".meta.labels.app", ".meta.ports[0]", ".nested.a.b.c",
		".list[0]", ".brand.new.key", ".meta.labels.newkey", ".missing.deep.path",
	}
	values := []string{"1", "changed", "{a: 1}", "[1, 2]", "true", ""}
	newKeys := []string{"renamed", "app", "newname"}

	doOp := func(doc *yaml.Node, ei *ymledit.EditIndex, verb byte, operand int) error {
		p := paths[operand%len(paths)]
		switch verb % 4 {
		case 0:
			segs, err := path.Parse(p)
			if err != nil {
				return err
			}
			v, err := ymledit.ParseValue(values[operand%len(values)], false)
			if err != nil {
				return err
			}
			return ymledit.Set(doc, segs, v, ei)
		case 1:
			segs, err := path.Parse(p)
			if err != nil {
				return err
			}
			v, err := ymledit.ParseValue(values[operand%len(values)], false)
			if err != nil {
				return err
			}
			return ymledit.Append(doc, segs, v, ei)
		case 2:
			segs, err := path.Parse(p)
			if err != nil {
				return err
			}
			return ymledit.Delete(doc, segs, ei)
		default:
			segs, err := path.Parse(p)
			if err != nil {
				return err
			}
			return ymledit.Rename(doc, segs, newKeys[operand%len(newKeys)], ei)
		}
	}

	f.Fuzz(func(t *testing.T, ops []byte) {
		const maxOps = 40 // this is about cache correctness across a batch,
		if len(ops) > maxOps {
			ops = ops[:maxOps] // not about how long a sequence the fuzzer can build
		}

		cached := freshSeed(t)
		uncached := freshSeed(t)
		ei := &ymledit.EditIndex{}

		for i := 0; i+1 < len(ops); i += 2 {
			verb, operand := ops[i], int(ops[i+1])
			errCached := doOp(cached, ei, verb, operand)
			errUncached := doOp(uncached, nil, verb, operand)
			if (errCached == nil) != (errUncached == nil) {
				t.Fatalf("op %d (verb=%d operand=%d): cached err=%v, uncached err=%v — diverged",
					i/2, verb%4, operand, errCached, errUncached)
			}
		}

		wantBytes, err := yaml.Marshal(uncached)
		if err != nil {
			t.Fatalf("uncached result won't encode: %v", err)
		}
		gotBytes, err := yaml.Marshal(cached)
		if err != nil {
			t.Fatalf("cached result won't encode: %v", err)
		}
		if string(gotBytes) != string(wantBytes) {
			t.Fatalf("cached and uncached results diverged:\ncached:\n%s\nuncached:\n%s", gotBytes, wantBytes)
		}
	})
}

// FuzzRename checks the same for renames.
func FuzzRename(f *testing.F) {
	seeds := []struct{ expr, newKey string }{
		{".name", "renamed"},
		{".meta.labels.app", "application"},
		{".meta.labels.app", "tier"}, // collides with an existing sibling
		{".meta.labels.app", "app"},  // renaming to its own name
		{".nested.a.b", "c"},
		{".list[0]", "x"},
		{".missing", "x"},
		{"", "x"},
		{".meta.*", "x"},
	}
	for _, s := range seeds {
		f.Add(s.expr, s.newKey)
	}

	f.Fuzz(func(t *testing.T, expr, newKey string) {
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		doc := freshSeed(t)
		if err := ymledit.Rename(doc, segs, newKey, nil); err != nil {
			return
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("Rename(%q, %q) produced a tree that will not encode: %v", expr, newKey, err)
		}
	})
}
