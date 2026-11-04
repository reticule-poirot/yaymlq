package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"gopkg.in/yaml.v3"
)

// TestFidelityCorpus runs a no-op edit over every fixture in
// testdata/fidelity and holds the output against the source.
//
// Renaming a key to itself changes nothing, so the whole read → mutate → encode
// pipeline runs with every fixup in it and nothing is asked to change. Any
// difference in the output is source fidelity lost, which makes this the one
// test in the package that does not have to know *which* fixup is responsible —
// and the one that catches a fixup placing something correctly in the shape its
// own tests use and wrongly in a shape they don't. Both #265 and the
// head-comment blank were exactly that: the restore was right whenever the
// construct was first in the document and wrong when a comment preceded it.
//
// The edit runs twice. A no-op is something the splice (splice.go) handles,
// and it must reproduce every fixture byte for byte with no exceptions. The
// encoder path runs it again with the splice off, because that path is the
// fallback for every edit the splice declines and keeps its fixups until #298.
//
// A fixture with a .want sibling records a construct the encoder path still
// re-renders. Closing one means deleting its .want; the test says so when a
// .want has become unnecessary, so a gap cannot quietly outlive its fix.
func TestFidelityCorpus(t *testing.T) {
	dir := filepath.Join("..", "testdata", "fidelity")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		seen++
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a fixture in this repo
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			spliced, err := execute(t, string(src), "rename", ".nop", "nop")
			if err != nil {
				t.Fatalf("no-op edit failed: %v", err)
			}
			if spliced != string(src) {
				t.Errorf("a spliced no-op edit did not reproduce the source\n--- want\n%q\n--- got\n%q", src, spliced)
			}
			got, err := executeEncoder(t, string(src), "rename", ".nop", "nop")
			if err != nil {
				t.Fatalf("no-op edit failed: %v", err)
			}

			want := string(src)
			gap := filepath.Join(dir, strings.TrimSuffix(name, ".yaml")+".want")
			recorded, err := os.ReadFile(gap) //nolint:gosec // G304: a fixture in this repo
			switch {
			case err == nil:
				if got == string(src) {
					t.Fatalf("this fixture round-trips now — delete %s", gap)
				}
				want = string(recorded)
			case !os.IsNotExist(err):
				t.Fatalf("read %s: %v", gap, err)
			}

			if got != want {
				t.Errorf("an encoder no-op edit did not reproduce the source\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
	// A corpus that silently emptied would pass every assertion above.
	if seen < 10 {
		t.Fatalf("found only %d fixtures in %s; the corpus is meant to cover the construct space", seen, dir)
	}
}

// TestFidelityCorpusFixturesCarryTheNopKey keeps the corpus drivable: the no-op
// edit is a rename of `.nop` to itself, so a fixture without that key would be
// checked by an edit that failed rather than by one that changed nothing.
func TestFidelityCorpusFixturesCarryTheNopKey(t *testing.T) {
	dir := filepath.Join("..", "testdata", "fidelity")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: a fixture in this repo
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if out, err := execute(t, string(src), "-o", "json", ".nop"); err != nil {
			t.Errorf("%s has no top-level `nop` key to drive the no-op edit: %v (%s)", e.Name(), err, out)
		}
	}
}

// TestFidelityCorpusForcedRerender re-renders each collection and document of
// every fixture in turn (forceRerender), with nothing edited, and holds the
// result to what a re-render promises: the same tree, every byte outside its
// span as the source had it, no comment lost or duplicated, a fixed point —
// re-rendering the output again changes nothing — and an output a spliced
// no-op reproduces.
func TestFidelityCorpusForcedRerender(t *testing.T) {
	dir := filepath.Join("..", "testdata", "fidelity")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	levels := map[string]int{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: a fixture in this repo
		if err != nil {
			t.Fatal(err)
		}
		docs, err := decodeNodes(src, sourceLines(src))
		if err != nil {
			t.Fatal(err)
		}
		for di, d := range docs {
			for _, tg := range append([]corpusTarget{{expr: "."}}, corpusTargets(d)...) {
				if tg.expr != "." && tg.kind != yaml.MappingNode && tg.kind != yaml.SequenceNode {
					continue
				}
				where := e.Name() + ": " + tg.expr
				out, edits, err := forcedRerender(t, src, di, tg.expr)
				if err != nil {
					t.Errorf("%s: %v", where, err)
					continue
				}
				for _, ed := range edits {
					levels[ed.level]++
				}
				if len(edits) > 0 {
					checkOutsideSpans(t, e.Name(), tg.expr, src, out, edits)
				}
				if a, b := commentTexts(t, src), commentTexts(t, out); a != b {
					t.Errorf("%s: comments changed\n--- source\n%s\n--- output\n%s", where, a, b)
				}
				again, _, err := forcedRerender(t, out, di, tg.expr)
				if err != nil || string(again) != string(out) {
					t.Errorf("%s: not a fixed point (%v)\n--- once\n%q\n--- twice\n%q", where, err, out, again)
				}
				if nop, err := execute(t, string(out), "rename", ".nop", "nop"); err != nil || nop != string(out) {
					t.Errorf("%s: a spliced no-op of the output changed it (%v)", where, err)
				}
			}
		}
	}
	t.Logf("re-renders by level: %v", levels)
}

// forcedRerender runs the splice over src with nothing edited and the node at
// expr in document doc forced to re-render. "." is the document itself.
func forcedRerender(t *testing.T, src []byte, doc int, expr string) ([]byte, []edit, error) {
	t.Helper()
	docs, err := decodeNodes(src, sourceLines(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	target := docs[doc]
	if expr != "." {
		segs, err := path.Parse(expr)
		if err != nil {
			t.Fatalf("parse %q: %v", expr, err)
		}
		target = target.Content[0]
		for _, sg := range segs {
			target = childAt(target, sg)
			if target == nil {
				t.Fatalf("%s: no node", expr)
			}
		}
	}
	forceRerender = func(n *yaml.Node) bool { return n == target }
	defer func() { forceRerender = nil }()
	indent := detectIndent(sourceLines(src))
	if indent < 1 {
		indent = 2
	}
	return splice(src, docs, snapshotTree(docs), indent)
}

func childAt(n *yaml.Node, sg path.Segment) *yaml.Node {
	switch {
	case sg.IsIndex && n.Kind == yaml.SequenceNode && sg.Index < len(n.Content):
		return n.Content[sg.Index]
	case !sg.IsIndex && n.Kind == yaml.MappingNode:
		var v *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == sg.Key {
				v = n.Content[i+1]
			}
		}
		return v
	}
	return nil
}

// commentTexts is every comment line in src, sorted: what a re-render must
// keep, wherever it puts it.
func commentTexts(t *testing.T, src []byte) string {
	t.Helper()
	docs, err := decodeNodes(src, sourceLines(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			for _, l := range strings.Split(c, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					out = append(out, l)
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, d := range docs {
		walk(d)
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}
