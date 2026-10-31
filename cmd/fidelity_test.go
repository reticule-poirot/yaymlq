package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// A fixture with a .want sibling records a construct yaml.v3 still re-renders.
// Closing one means deleting its .want; the test says so when a .want has become
// unnecessary, so a gap cannot quietly outlive its fix.
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
			got, err := execute(t, string(src), "rename", ".nop", "nop")
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
				t.Errorf("a no-op edit did not reproduce the source\n--- want\n%q\n--- got\n%q", want, got)
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
