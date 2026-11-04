package cmd

import (
	"strings"
	"testing"
	"time"
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
	// Lines are split once for the whole stream (#71), so each document's
	// blank lines are found by stream-wide, not per-document, line numbers.
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
// applyEdit used to make a full-input scan once per document instead of
// once total, making a multi-document edit
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
	// The blank line inside an indented mapping must stay empty, not padded
	// to the indentation around it.
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
