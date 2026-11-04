package cmd

import (
	"strings"
	"testing"
)

// TestEditPreservesCommentGuttersOnUntouchedLines guards #102: yaml.v3
// discards inline comments' original gutter width at parse time and always
// re-emits exactly one space, which once collapsed every hand-aligned comment
// in the document, not just the one on the line being edited.
func TestEditPreservesCommentGuttersOnUntouchedLines(t *testing.T) {
	in := "name: web  # two spaces, untouched\n" +
		"replicas: 3    # four spaces, untouched\n" +
		"image: v1  # two spaces, this one changes\n"

	got, err := execute(t, in, "set", ".image", "v2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "name: web  # two spaces, untouched\n" +
		"replicas: 3    # four spaces, untouched\n" +
		"image: v2  # two spaces, this one changes\n"
	if got != want {
		t.Fatalf("gutters not preserved:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditStandaloneCommentUnaffected checks a full-line comment (one not
// attached to any value on the same line) is left alone.
func TestEditStandaloneCommentUnaffected(t *testing.T) {
	in := "# a standalone comment, indented oddly on purpose\n" +
		"replicas: 3    # inline, four spaces\n"

	got, err := execute(t, in, "set", ".replicas", "5")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(got, "# a standalone comment, indented oddly on purpose\n") {
		t.Fatalf("standalone comment line changed, got:\n%q", got)
	}
	if !strings.Contains(got, "replicas: 5    # inline, four spaces") {
		t.Fatalf("inline comment gutter not preserved, got:\n%q", got)
	}
}

// TestEditCommentGuttersAcrossMultipleDocuments mirrors
// TestEditPreservesBlankLinesAcrossMultipleDocuments: both documents' own
// gutters survive an edit in one of them.
func TestEditCommentGuttersAcrossMultipleDocuments(t *testing.T) {
	in := "a: 1  # doc one, two spaces\n---\nb: 2    # doc two, four spaces\n"
	got, err := execute(t, in, "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "a: 9  # doc one, two spaces\n---\nb: 2    # doc two, four spaces\n"
	if got != want {
		t.Fatalf("gutters not preserved across documents:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestEditCommentGuttersSurviveDeleteOfDifferentNode checks that deleting
// one commented node doesn't disturb the gutters of the others.
func TestEditCommentGuttersSurviveDeleteOfDifferentNode(t *testing.T) {
	in := "a: 1    # first\nb: 2    # second\nc: 3    # third\n"
	got, err := execute(t, in, "delete", ".b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "a: 1    # first\nc: 3    # third\n"
	if got != want {
		t.Fatalf("gutters not preserved after delete:\nwant:\n%q\ngot:\n%q", want, got)
	}
}
