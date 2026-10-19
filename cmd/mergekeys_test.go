package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestEditPreservesMergeKeys guards #171: yaml.v3's *decoder* gives the `<<`
// key scalar an explicit `!!merge` tag, and its encoder faithfully re-emits
// the tag it was handed, so every edit of a document using merge keys
// rewrote every `<<:` in it into `!!merge <<:` — including in mappings the
// edit never went near. Every other scalar comes back tagged `!!str`/`!!int`,
// which the encoder resolves implicitly and omits; `<<` was the one that
// round-tripped visibly.
func TestEditPreservesMergeKeys(t *testing.T) {
	in := "base: &b\n" +
		"  k: 1\n" +
		"use:\n" +
		"  <<: *b\n" +
		"  other: 2\n"

	got, err := execute(t, in, "set", ".use.other", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "base: &b\n" +
		"  k: 1\n" +
		"use:\n" +
		"  <<: *b\n" +
		"  other: 3\n"
	if got != want {
		t.Fatalf("merge key not preserved:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditLeavesExplicitMergeTagAlone is the mirror image: a source that
// spells the tag out asked for it, so normalising it away would be the same
// unrequested rewrite in the other direction. The decoder reports an explicit
// and an implicit `!!merge` identically, so the source line is the only thing
// that can tell them apart.
func TestEditLeavesExplicitMergeTagAlone(t *testing.T) {
	in := "base: &b\n" +
		"  k: 1\n" +
		"use:\n" +
		"  !!merge <<: *b\n" +
		"  other: 2\n"

	got, err := execute(t, in, "set", ".use.other", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "!!merge <<: *b") {
		t.Fatalf("explicit !!merge tag was stripped, got:\n%q", got)
	}
	if strings.Contains(got, "\n  <<: *b") {
		t.Fatalf("explicit !!merge tag rewritten to the implicit form, got:\n%q", got)
	}
}

// TestEditPreservesMergeKeysInBothFormsAtOnce pins the two rules against each
// other in one document, since the fix has to decide per node rather than per
// document.
func TestEditPreservesMergeKeysInBothFormsAtOnce(t *testing.T) {
	in := "base: &b\n" +
		"  k: 1\n" +
		"implicit:\n" +
		"  <<: *b\n" +
		"explicit:\n" +
		"  !!merge <<: *b\n" +
		"edited: 2\n"

	got, err := execute(t, in, "set", ".edited", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "base: &b\n" +
		"  k: 1\n" +
		"implicit:\n" +
		"  <<: *b\n" +
		"explicit:\n" +
		"  !!merge <<: *b\n" +
		"edited: 3\n"
	if got != want {
		t.Fatalf("merge key forms not each preserved:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditPreservesMergeKeysAcrossMultipleDocuments checks the normalisation
// runs per decoded document, the same way preserveBlankLines and
// recordCommentGutters do — a stream decodes into several nodes and only one
// of them is edited.
func TestEditPreservesMergeKeysAcrossMultipleDocuments(t *testing.T) {
	in := "base: &b\n" +
		"  k: 1\n" +
		"use:\n" +
		"  <<: *b\n" +
		"---\n" +
		"base: &c\n" +
		"  k: 2\n" +
		"use:\n" +
		"  <<: *c\n" +
		"  other: 2\n"

	got, err := execute(t, in, "set", "--doc", "1", ".use.other", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "!!merge") {
		t.Fatalf("merge key tagged in a multi-document stream, got:\n%q", got)
	}
	if n := strings.Count(got, "<<: *"); n != 2 {
		t.Fatalf("want 2 merge keys preserved, got %d:\n%q", n, got)
	}
}

// TestEditFixtureWithAnchorsIsByteIdenticalAwayFromTheEdit is the round-trip
// the fixtures were missing entirely: before #171 no test anywhere in the
// suite contained a merge key, which is why this survived. Everything except
// the edited line must come back unchanged, comments and blank lines
// included.
func TestEditFixtureWithAnchorsIsByteIdenticalAwayFromTheEdit(t *testing.T) {
	src, err := os.ReadFile("../testdata/anchors.yml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got, err := execute(t, string(src), "set", ".services.web.image", "nginx:1.28")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	want := strings.Replace(string(src), "image: nginx:1.27", "image: nginx:1.28", 1)
	if got != want {
		t.Fatalf("fixture round trip changed more than the edited line:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestDeleteStillRefusesToStrandAMergeKeysAnchor checks the fix doesn't
// disturb anchorAliased, which decides whether an edit would leave a dangling
// alias. It detects a merge key through its *value* (an AliasNode), not
// through the `!!merge` tag on the key, so clearing that tag must not change
// the answer.
func TestDeleteStillRefusesToStrandAMergeKeysAnchor(t *testing.T) {
	in := "base: &b\n" +
		"  k: 1\n" +
		"use:\n" +
		"  <<: *b\n"

	if _, err := execute(t, in, "delete", ".base"); err == nil {
		t.Fatal("deleting an anchor a merge key references should be refused, got no error")
	}
}
