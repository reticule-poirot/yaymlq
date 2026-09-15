package cmd

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestEditPreservesBlankLines(t *testing.T) {
	in := "version: \"3.9\"\n\nservices:\n  web:\n    image: nginx:1.27\n\n    ports:\n      - \"80:80\"\n\nvolumes:\n  data: {}\n"

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"set", []string{"set", ".services.web.image", "nginx:1.28"}},
		{"append", []string{"append", ".services.web.ports", "9090"}},
		{"delete", []string{"delete", ".volumes.data"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.Count(got, "\n\n") != 3 {
				t.Fatalf("want 3 blank lines preserved, got:\n%s", got)
			}
		})
	}
}

func TestEditPreservesBlankLinesAcrossMultipleDocuments(t *testing.T) {
	// blankLines(data) is now computed once for the whole stream and shared
	// across documents (the #71 fix) instead of once per document — each
	// document's own blank lines still need to land correctly, keyed by
	// their real (stream-wide, not per-document-local) source line numbers.
	// applyEdit re-encodes every document in the stream regardless of which
	// one --doc targets, so editing just the first still exercises both
	// documents' blank-line markers.
	in := "a: 1\n\nb: 2\n---\nc: 3\n\nd: 4\n"
	got, err := execute(t, in, "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Count(got, "\n\n") != 2 {
		t.Fatalf("want one blank line preserved per document, got:\n%s", got)
	}
	if !strings.Contains(got, "a: 9\n\nb: 2") || !strings.Contains(got, "c: 3\n\nd: 4") {
		t.Fatalf("blank line not attached to the right node in each document, got:\n%s", got)
	}
}

// TestPreserveBlankLinesScalesLinearlyWithDocumentCount guards #71:
// applyEdit used to call preserveBlankLines (a full-input scan) once per
// document instead of once total, making a multi-document edit
// O(documents × input size). A regression back to that would blow well
// past this generous bound even at a modest document count.
func TestPreserveBlankLinesScalesLinearlyWithDocumentCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 4000; i++ {
		b.WriteString("---\na: 1\n")
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

func TestEditBlankLinesHaveNoTrailingWhitespace(t *testing.T) {
	// The blank line between image and ports sits at indent 4; the encoder
	// pads it, tidyBlankLines must strip that back to an empty line.
	in := "a:\n  b: 1\n\n  c: 2\n"
	got, err := execute(t, in, "set", ".a.b", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, line := range strings.Split(got, "\n") {
		if line != "" && strings.TrimSpace(line) == "" {
			t.Fatalf("blank line has trailing whitespace %q in:\n%s", line, got)
		}
	}
}

func TestEditRunsOfBlankLinesCollapse(t *testing.T) {
	got, err := execute(t, "a: 1\n\n\n\nb: 2\n", "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "a: 9\n\nb: 2\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPreserveBlankLinesIsIdempotent(t *testing.T) {
	src := []byte("a: 1\n\nb: 2\n")
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		t.Fatal(err)
	}
	preserveBlankLines(&doc, src)
	preserveBlankLines(&doc, src)
	if h := doc.Content[0].Content[2].HeadComment; h != "\n" {
		t.Fatalf("HeadComment = %q, want one leading newline", h)
	}
}
