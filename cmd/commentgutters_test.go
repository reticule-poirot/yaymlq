package cmd

import (
	"strings"
	"testing"
	"time"
)

// TestEditPreservesCommentGuttersOnUntouchedLines guards #102: yaml.v3
// discards inline comments' original gutter width at parse time and always
// re-emits exactly one space, so without recordCommentGutters/
// widenCommentGutters every edit command would collapse every hand-aligned
// comment in the document, not just the one on the line being edited.
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
// attached to any value on the same line) is left alone — widenCommentGutters
// only ever touches an inline (Node.LineComment) gutter.
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
// TestEditPreservesBlankLinesAcrossMultipleDocuments: recordCommentGutters
// is called once per document sharing one gutters map (matching how
// blankLines/markBlankLines are split for the same O(documents × input
// size) reason), so both documents' own gutters need to land correctly.
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
// one commented node doesn't disturb the recorded gutters of others —
// widenCommentGutters matches by comment text in order of appearance, and a
// deleted comment is simply never consumed.
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

func TestGutterWidth(t *testing.T) {
	cases := []struct {
		line, comment string
		want          int
		wantOK        bool
	}{
		{"image: v1  # two spaces", "# two spaces", 2, true},
		{"image: v1 # one space", "# one space", 1, true},
		{"image: v1# no space", "# no space", 0, true},
		{"image: v1", "# missing", 0, false},
	}
	for _, tc := range cases {
		got, ok := gutterWidth(tc.line, tc.comment)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("gutterWidth(%q, %q) = (%d, %v), want (%d, %v)",
				tc.line, tc.comment, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestTrailingCommentIndex(t *testing.T) {
	idx, text, ok := trailingCommentIndex([]byte("image: v1 # a comment"))
	if !ok || idx != 9 || text != "# a comment" {
		t.Fatalf("got (%d, %q, %v), want (9, %q, true)", idx, text, ok, "# a comment")
	}
	if _, _, ok := trailingCommentIndex([]byte("# standalone, no preceding space")); ok {
		t.Fatal("standalone comment should not match")
	}
	if _, _, ok := trailingCommentIndex([]byte("no comment here")); ok {
		t.Fatal("line with no comment should not match")
	}
}

// TestCommentGuttersScaleLinearlyWithDocumentCount guards against
// recordCommentGutters re-splitting the whole input once per document
// instead of once total — the exact O(documents × input size) bug #71
// already fixed for blank lines, briefly reintroduced here in this file's
// first version. The existing blank-lines equivalent of this test caught
// it, but only on CI's slower runner (margin enough to pass on a faster
// local machine); this dedicated test guards the comment-gutter path
// specifically, independent of that test's own margin.
func TestCommentGuttersScaleLinearlyWithDocumentCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 4000; i++ {
		b.WriteString("---\na: 1  # comment\n")
	}
	in := b.String()

	start := time.Now()
	if _, err := execute(t, in, "set", ".a", "2"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("editing a %d-document stream took %v, want well under 2s — looks like the quadratic bug is back", 4000, elapsed)
	}
}
