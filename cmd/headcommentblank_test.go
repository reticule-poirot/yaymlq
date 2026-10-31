package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditKeepsOneBlankAfterADocumentHeadComment covers #267: a file that opens
// with a comment header, a blank line, then its first key — which is most files
// that open with a comment at all — gained a second blank line on every edit.
// testdata/fidelity/head-comment.yaml is the same defect checked whole.
//
// yaml.v3 attaches that header to the *document* node and its encoder emits the
// blank line itself, so blanklines.go's mark was a second copy of a separator
// that was already there. The same mistake dropBlockContentBlanks exists to
// prevent one level down: a blank line that belongs to something else is not
// spacing for this fixup to restore.
func TestEditKeepsOneBlankAfterADocumentHeadComment(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"one comment", "# one\n\nk: 1\n", "# one\n\nk: 2\n"},
		{"several comments", "# one\n# two\n# three\n\nk: 1\n", "# one\n# two\n# three\n\nk: 2\n"},
		{"comment on the key too", "# doc\n\n# key\nk: 1\n", "# doc\n\n# key\nk: 2\n"},
		{"a run collapses to one", "# one\n\n\n\nk: 1\n", "# one\n\nk: 2\n"},
		{"no blank, no change", "# one\nk: 1\n", "# one\nk: 2\n"},
		{"sequence root", "# one\n\n- a\n- b\n", "# one\n\n- a\n- c\n"},
		{"later blanks still kept", "# one\n\nk: 1\n\nj: 2\n", "# one\n\nk: 2\n\nj: 2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"set", ".k", "2"}
			if strings.Contains(tc.in, "- a") {
				args = []string{"set", "[1]", "c"}
			}
			got, err := execute(t, tc.in, args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestEditInPlaceIsIdempotentWithAHeadComment is the shape of the defect that
// matters most: the file after one edit is not the file the next edit starts
// from, so a tool run twice produces two different diffs.
func TestEditInPlaceIsIdempotentWithAHeadComment(t *testing.T) {
	in := "# header\n\nk: 1\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for i := range 3 {
		if _, err := execute(t, "", "set", "-i", ".k", "2", path); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: a path this test just created
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "# header\n\nk: 2\n"; string(got) != want {
		t.Errorf("three runs of the same edit\n--- want\n%q\n--- got\n%q", want, string(got))
	}
}
