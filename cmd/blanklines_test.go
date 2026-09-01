package cmd

import (
	"strings"
	"testing"

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
