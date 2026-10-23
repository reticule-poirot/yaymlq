package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Three documents, two of which have .meta.env: the manifest shape #170 is
// about, where "the same change everywhere" currently needs a three-program
// pipeline.
const editStream = "kind: A\nmeta:\n  env: dev\n" +
	"---\n" +
	"kind: B\n" +
	"---\n" +
	"kind: C\nmeta:\n  env: dev\n"

// TestEditAllDocsAppliesWhereThePathResolves guards #170 on the decision
// recorded there: --all-docs is lenient, matching what #175 settled for the
// read side. A document without the path is skipped rather than failing the
// batch, so one flag spelled the same way on both sides behaves the same way.
func TestEditAllDocsAppliesWhereThePathResolves(t *testing.T) {
	got, err := execute(t, editStream, "set", "--all-docs", "--must-exist", ".meta.env", "prod")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if n := strings.Count(got, "env: prod"); n != 2 {
		t.Fatalf("want both matching documents updated, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "kind: B") {
		t.Errorf("the skipped document should still be in the output:\n%s", got)
	}
}

// TestEditAllDocsPerVerb covers the three verbs whose path must already
// resolve, which is where leniency actually does something.
func TestEditAllDocsPerVerb(t *testing.T) {
	for name, tc := range map[string]struct {
		args    []string
		absent  string
		present string
	}{
		"delete": {[]string{"delete", "--all-docs", ".meta.env"}, "env:", "kind: B"},
		"rename": {[]string{"rename", "--all-docs", ".meta.env", "environment"}, "env: dev", "environment: dev"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, editStream, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.Contains(got, tc.absent) {
				t.Errorf("%q should be gone from every matching document:\n%s", tc.absent, got)
			}
			if !strings.Contains(got, tc.present) {
				t.Errorf("want %q in the output:\n%s", tc.present, got)
			}
		})
	}
}

// TestEditAllDocsFailsWhenNoDocumentMatches: lenient about which documents
// match, not about matching nothing — the same half of the rule #175 keeps.
func TestEditAllDocsFailsWhenNoDocumentMatches(t *testing.T) {
	out, err := execute(t, editStream, "delete", "--all-docs", ".meta.nope")
	if err == nil {
		t.Fatalf("want an error when no document has the path, got:\n%s", out)
	}
	if code := codeFor(err); code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
	// The first miss's message, so #163's recovery facts survive.
	if msg := annotateNotFound(err, defaultMaxSuggestions).Error(); !strings.Contains(msg, "no such key") {
		t.Errorf("want a \"no such key\" message, got %q", msg)
	}
}

// TestEditAllDocsWritesNothingWhenNoDocumentMatches: the failure happens
// before the write, so a run that matched nothing leaves the file alone.
func TestEditAllDocsWritesNothingWhenNoDocumentMatches(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(f, []byte(editStream), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "", "delete", "-i", "--all-docs", ".meta.nope", f); err == nil {
		t.Fatal("want an error")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != editStream {
		t.Errorf("file changed by a run that matched nothing:\nwant:\n%q\ngot:\n%q", editStream, data)
	}
}

// TestEditAllDocsStillFailsOnARefusalAboutTheEdit is the boundary the
// ErrNotResolved tag exists to draw. A wildcard path and an edit that would
// strand an alias are wrong about the edit, not about the document, so they
// are true everywhere — skipping document after document and then reporting
// "nothing matched" would be actively misleading.
func TestEditAllDocsStillFailsOnARefusalAboutTheEdit(t *testing.T) {
	t.Run("wildcard", func(t *testing.T) {
		_, err := execute(t, editStream, "delete", "--all-docs", ".meta.*")
		if err == nil {
			t.Fatal("want an error for a wildcard path")
		}
		if msg := err.Error(); !strings.Contains(msg, "wildcard") {
			t.Errorf("want the wildcard named, got %q", msg)
		}
	})
	t.Run("anchored", func(t *testing.T) {
		in := "base: &b\n  k: 1\nuse:\n  <<: *b\n---\nbase: &c\n  k: 2\n"
		_, err := execute(t, in, "delete", "--all-docs", ".base")
		if err == nil {
			t.Fatal("want an error for an edit that would strand an alias")
		}
		if msg := err.Error(); !strings.Contains(msg, "anchor") {
			t.Errorf("want the anchor named, got %q", msg)
		}
	})
}

// TestSetAllDocsLandsEverywhereWithoutMustExist: set creates what it can't
// find, so "every document" is unambiguous for it — which is exactly why
// --must-exist is what gives a stream-wide set a non-creating mode (#164).
func TestSetAllDocsLandsEverywhereWithoutMustExist(t *testing.T) {
	got, err := execute(t, editStream, "set", "--all-docs", ".meta.env", "prod")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if n := strings.Count(got, "env: prod"); n != 3 {
		t.Fatalf("want all three documents to have the key, got %d:\n%s", n, got)
	}
}

// TestEditAllDocsConflictsWithDoc: the same mutual exclusion the read side
// has, since asking for one document and every document is contradictory.
func TestEditAllDocsConflictsWithDoc(t *testing.T) {
	for _, verb := range []string{"set", "append", "delete", "rename"} {
		t.Run(verb, func(t *testing.T) {
			args := []string{verb, "--all-docs", "--doc", "1", ".meta.env"}
			if verb != "delete" {
				args = append(args, "x")
			}
			_, err := execute(t, editStream, args...)
			if err == nil {
				t.Fatal("want --doc with --all-docs rejected")
			}
			if code := codeFor(err); code != 3 {
				t.Errorf("want exit 3, got %d", code)
			}
			// Asserted so an "unknown flag: --all-docs" — also exit 3 —
			// can't satisfy this test while the flag doesn't exist.
			if msg := err.Error(); !strings.Contains(msg, "--all-docs") || !strings.Contains(msg, "--doc") {
				t.Errorf("want both flags named, got %q", msg)
			}
		})
	}
}

// TestApplyAllDocsAppliesEachOpToEveryDocument: an op that names no --doc of
// its own follows the invocation's scope, which is how apply's existing
// per-op --doc already works — --all-docs just makes that scope the whole
// stream.
func TestApplyAllDocsAppliesEachOpToEveryDocument(t *testing.T) {
	script := writeScript(t, t.TempDir(), "set --doc 0 .kind = Z\ndelete .meta.env\n")
	got, err := execute(t, editStream, "apply", "--all-docs", "-f", script)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "env: dev") {
		t.Errorf("the unqualified op should have reached every document:\n%s", got)
	}
	// An op naming its own document still means that one, not all of them.
	if n := strings.Count(got, "kind: Z"); n != 1 {
		t.Errorf("want the --doc 0 op to apply to one document, got %d:\n%s", n, got)
	}
}

// TestEditDocStillSelectsOneDocument is the guard: without --all-docs nothing
// changes, and a miss in the selected document is still an error.
func TestEditDocStillSelectsOneDocument(t *testing.T) {
	got, err := execute(t, editStream, "delete", "--doc", "0", ".meta.env")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if n := strings.Count(got, "env: dev"); n != 1 {
		t.Fatalf("want only document 0 edited, got %d remaining:\n%s", n, got)
	}
	if _, err := execute(t, editStream, "delete", "--doc", "1", ".meta.env"); err == nil {
		t.Fatal("want a miss in the selected document to stay an error")
	}
}
