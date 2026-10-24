package ymledit_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"gopkg.in/yaml.v3"
)

// The last four keys are not decoration (#205). `common` carries an anchor
// that `service`'s merge key and `alias` both reference, which is the only
// thing that makes anchorAliased seed EditIndex's reference counts — and so
// the only thing that makes the counts, their incremental updates, and
// ErrAnchored reachable at all. `blank` is null-valued, which is the single
// branch set's mapping reset is reached from. Two references rather than one
// so that dropping one still leaves the anchor live, which is where a wrong
// count and a right one give different answers.
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
common: &common
  restart: always
service:
  <<: *common
  image: nginx
alias: *common
blank:
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

// TestFuzzSeedDocReachesTheEditIndexCache guards #205. EditIndex's cached
// paths are switched on by two shapes in the document, not by the ops run
// against it: anchorAliased returns at its first line unless a node carries
// an anchor, so without an alias reference ei.seededRefs stays false forever
// and noteSubtreeRemoved/noteSubtreeInserted are no-ops; and
// noteMappingReset is reached only from set's auto-vivification of a null.
//
// FuzzEditIndexMatchesUncached calls itself the strongest correctness check
// for EditIndex, and with neither shape in the seed it compared two code
// paths that could not diverge, however long it ran. This fails if either
// shape is trimmed out of fuzzSeedDoc again.
func TestFuzzSeedDocReachesTheEditIndexCache(t *testing.T) {
	// An alias reference, proven through the behavior it enables rather
	// than by reading the YAML: ErrAnchored can only be reported if
	// anchorAliased found a live reference, which is also what seeds the
	// cache's reference counts.
	segs, err := path.Parse(".common")
	if err != nil {
		t.Fatal(err)
	}
	ei := &ymledit.EditIndex{}
	err = ymledit.Delete(freshSeed(t), segs, ei)
	if !errors.Is(err, ymledit.ErrAnchored) {
		t.Errorf("deleting the seed's anchored key: got %v, want ErrAnchored — the seed needs an anchor with a live alias reference", err)
	}

	// A null-valued key, asserted on the tree: a set through it takes the
	// mapping-reset branch, and nothing else in the document does.
	root := freshSeed(t).Content[0]
	var found bool
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i+1].Kind == yaml.ScalarNode && root.Content[i+1].Tag == "!!null" {
			found = true
			break
		}
	}
	if !found {
		t.Error("the seed has no null-valued key, so set's mapping reset is unreachable from the fuzzer")
	}
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
	// The anchor cases (#205). Each one needs the reference counts to be
	// right rather than merely present: the first drops both references and
	// then the anchor, which must stop being refused at exactly the third
	// op; the second is refused first, which is what seeds the counts, and
	// then edits through the merged mapping.
	f.Add([]byte{2, 9, 2, 10, 2, 8})
	f.Add([]byte{2, 8, 0, 11, 2, 9})
	f.Add([]byte{0, 12, 2, 12, 0, 12}) // set through the null, delete it, set it again

	// A small fixed vocabulary of paths/values/keys, reused by index (mod
	// length) — keeps the fuzzer's job "pick a short sequence of (verb,
	// operand) pairs" instead of also having to discover valid YAML syntax
	// from scratch.
	paths := []string{
		".name", ".meta.labels.app", ".meta.ports[0]", ".nested.a.b.c",
		".list[0]", ".brand.new.key", ".meta.labels.newkey", ".missing.deep.path",
		// 8..12: the anchor, its two references, a key under the merged
		// mapping, and a key under the null. The seeds above address these
		// by index, so inserting rather than appending here renumbers them.
		".common", ".alias", ".service", ".service.image", ".blank.x",
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

// FuzzSetMustExistNeverCreates asserts the property the guard exists for: a
// refused edit changes nothing. The unit tests cover the two shapes that can
// mutate before failing (a missing key, and walking into a null, which Set
// resets to an empty mapping first); this checks the whole path space, since
// "fails" and "fails having already half-applied" are indistinguishable from
// the caller's exit code and only differ in the document left behind.
func FuzzSetMustExistNeverCreates(f *testing.F) {
	seeds := []struct{ expr, val string }{
		{".name", "changed"},
		{".meta.labels.nope", "x"},
		{".nope.deeper", "x"},
		{".meta.ports[9]", "x"},
		{".meta.labels.app", "web"},
		{"", "x"},
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
		before, err := yaml.Marshal(doc)
		if err != nil {
			return
		}
		if err := ymledit.SetMustExist(doc, segs, value, nil); err == nil {
			return
		}
		after, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatalf("SetMustExist(%q, %q) failed and left a tree that will not encode: %v", expr, val, err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("SetMustExist(%q, %q) failed but changed the document:\nbefore:\n%s\nafter:\n%s", expr, val, before, after)
		}
	})
}
