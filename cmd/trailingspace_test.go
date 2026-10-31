package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditKeepsTrailingWhitespace is #252's trailing-whitespace row. The
// characters mean nothing to YAML, which is exactly why an edit has no business
// removing them: the lines they sit on are lines the caller did not touch, and
// they show up in the diff all the same.
func TestEditKeepsTrailingWhitespace(t *testing.T) {
	cases := []struct{ name, in string }{
		{"spaces after a value", "a: 1   \nafter: 1\n"},
		{"a tab after a value", "a: 2\t\nafter: 1\n"},
		{"after a bare key", "c:   \n  d: 3\nafter: 1\n"},
		{"on a nested line", "c:\n  d: 3  \nafter: 1\n"},
		{"after a comment", "a: 1 # note  \nafter: 1\n"},
		{"after a sequence item", "s:\n  - 1  \n  - 2\nafter: 1\n"},
		{"on several lines", "a: 1 \nb: 2  \nc: 3   \nafter: 1\n"},
		{"mixed spaces and tabs", "a: 1 \t \nafter: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("an edit to .after stripped trailing whitespace\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditDropsTrailingWhitespaceFromTheLineItChanged is the rule the rest of
// this layer already follows: the line the edit rewrote is the caller's, and
// comes out as the caller's value, not as the source's spelling of a value that
// is gone.
func TestEditDropsTrailingWhitespaceFromTheLineItChanged(t *testing.T) {
	got, err := execute(t, "a: 1\nafter: 1   \n", "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "a: 1\nafter: 2\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditStillBlanksAWhitespaceOnlyLine keeps the boundary with blanklines.go
// explicit: a line of nothing but spaces is vertical spacing, which that fixup
// owns and collapses, not trailing whitespace on a line of content.
func TestEditStillBlanksAWhitespaceOnlyLine(t *testing.T) {
	got, err := execute(t, "a: 1\n   \nafter: 1\n", "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "a: 1\n\nafter: 2\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditNeverTrailsWhitespaceIntoABlockScalar is the failure this fixup is
// gated against, and the reason the gate is a check on the whole output rather
// than on each line: inside a block scalar a trailing space is a character of
// the value, so a line of a block's body that happens to read exactly like a
// line elsewhere that *did* carry trailing whitespace would have its value
// changed by the restore. The restore is dropped whole when that happens.
func TestEditNeverTrailsWhitespaceIntoABlockScalar(t *testing.T) {
	in := "f: |\n  a: 1\nm:\n  a: 1   \nafter: 1\n"
	before, err := execute(t, in, "-o", "json", ".f")
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	edited, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	after, err := execute(t, edited, "-o", "json", ".f")
	if err != nil {
		t.Fatalf("read after: %q\n%v", edited, err)
	}
	if before != after {
		t.Fatalf("editing .after changed .f\n--- out\n%q\n before %s  after %s", edited, before, after)
	}
}

// TestEditKeepsTrailingWhitespaceBesideABlockScalar is the ordinary case of the
// same arrangement: no collision, so the block keeps its own trailing spaces
// (blockscalars.go's doing) and the line outside keeps its own.
func TestEditKeepsTrailingWhitespaceBesideABlockScalar(t *testing.T) {
	in := "f: |\n  body   \n  more\nx: 1  \nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditKeepsTrailingWhitespaceUnderCRLF checks the restore runs before the
// line endings are reapplied — a run appended after one would land past the
// "\r" and not at the end of the line at all.
func TestEditKeepsTrailingWhitespaceUnderCRLF(t *testing.T) {
	in := "a: 1   \r\nafter: 1\r\n"
	want := "a: 1   \r\nafter: 2\r\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditInPlaceIsIdempotentWithTrailingWhitespace is the property the strip
// costs: the second run of the same edit has a different file to work on.
func TestEditInPlaceIsIdempotentWithTrailingWhitespace(t *testing.T) {
	in := "a: 1   \nb: 2\t\nafter: 1\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for i := range 2 {
		if _, err := execute(t, "", "set", "-i", ".after", "2", path); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: a path this test just created
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	if string(got) != want {
		t.Errorf("two runs of the same edit\n--- want\n%q\n--- got\n%q", want, string(got))
	}
}

// TestSameValuesComparesMeaningNotText pins the guard the whole fixup rests on.
// It is asked whether a change to the text changed what the text means, so
// layout differences are the same and a value difference is not — and a stream
// it cannot decode answers no, which keeps the restore from being applied on the
// strength of a comparison that did not happen.
func TestSameValuesComparesMeaningNotText(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		want       bool
	}{
		{"identical", "a: 1\n", "a: 1\n", true},
		{"layout only", "a: 1\n", "a:    1\n", true},
		{"comment only", "a: 1\n", "a: 1 # note\n", true},
		{"across documents", "a: 1\n---\nb: 2\n", "a: 1\n---\nb:  2\n", true},
		{"value changed", "a: 1\n", "a: 2\n", false},
		{"block scalar changed", "f: |\n  x\n", "f: |\n  x \n", false},
		{"document count changed", "a: 1\n", "a: 1\n---\nb: 2\n", false},
		{"left will not decode", "a: [\n", "a: 1\n", false},
		{"right will not decode", "a: 1\n", "a: [\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameValues([]byte(tc.a), []byte(tc.b)); got != tc.want {
				t.Errorf("sameValues(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
