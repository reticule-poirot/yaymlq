package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditRefusesWhatAReadRefuses: an editing verb decodes into nodes, where
// yaml.v4 checks no keys at all, so a document `get` refused as a duplicate —
// even `a` written twice — was edited and written back, the edit landing on
// whichever of the two its key lookup reached first. Every verb now refuses it
// with the read's parse error, and leaves the file as it was.
func TestEditRefusesWhatAReadRefuses(t *testing.T) {
	docs := map[string]string{
		"same text":      "a: x\na: y\n",
		"quoted twin":    "a: x\n\"a\": y\n",
		"octal":          "a: x\n7: p\n0o7: q\n",
		"bool case":      "a: x\ntrue: p\nTrue: q\n",
		"alias key":      "&k a: x\n*k : y\n",
		"nested":         "top:\n  - a: x\n    a: y\na: z\n",
		"later document": "a: x\n---\nb: 1\nb: 2\n",
	}
	verbs := [][]string{
		{"set", ".a", "z"},
		{"delete", ".a"},
		{"rename", ".a", "b2"},
		{"append", ".l", "z"},
	}
	for name, doc := range docs {
		for _, verb := range verbs {
			t.Run(name+"/"+verb[0], func(t *testing.T) {
				f := filepath.Join(t.TempDir(), "d.yaml")
				if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
					t.Fatal(err)
				}
				out, err := execute(t, "", append(append([]string{}, verb...), f, "-i")...)
				if code := exitCode(err, io.Discard); code != 2 {
					t.Fatalf("exit %d, want 2 (parse); err %v, output %s", code, err, out)
				}
				if !strings.Contains(err.Error(), "already defined") {
					t.Errorf("error %q is not the duplicate-key error", err)
				}
				if got, _ := os.ReadFile(f); string(got) != doc {
					t.Errorf("file changed:\n%s", got)
				}
			})
		}
		t.Run(name+"/apply", func(t *testing.T) {
			dir := t.TempDir()
			f := filepath.Join(dir, "d.yaml")
			if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			script := writeScript(t, dir, "set .a = z\n")
			_, err := execute(t, "", "apply", "-f", script, "-i", f)
			if code := exitCode(err, io.Discard); code != 2 {
				t.Fatalf("exit %d, want 2 (parse); err %v", code, err)
			}
			if got, _ := os.ReadFile(f); string(got) != doc {
				t.Errorf("file changed:\n%s", got)
			}
		})
	}
}

// TestEditKeepsKeysThatOnlyLookAlike: the edit-side check refuses no more than
// the read side does.
func TestEditKeepsKeysThatOnlyLookAlike(t *testing.T) {
	for _, tc := range []struct{ doc, want string }{
		{"1: x\n1.0: y\na: 1\n", "1: x\n1.0: y\na: 2\n"},
		{".nan: x\n.NaN: y\na: 1\n", ".nan: x\n.NaN: y\na: 2\n"},
		{"base: &b\n  k: 1\nm:\n  <<: *b\n  k: 2\na: 1\n", "base: &b\n  k: 1\nm:\n  <<: *b\n  k: 2\na: 2\n"},
	} {
		got, err := execute(t, tc.doc, "set", ".a", "2")
		if err != nil {
			t.Errorf("%q: %v", tc.doc, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.doc, got, tc.want)
		}
	}
}
