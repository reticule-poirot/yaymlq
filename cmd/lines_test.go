package cmd

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlBreaks are the five characters YAML ends a line on, and the one it
// doesn't. Every fixup that reads the raw source counts lines, and
// yaml.Node.Line counts all five — so a document using any of the four
// non-LF breaks was read against the wrong lines from the first one onward
// (#254).
var yamlBreaks = []struct{ name, br string }{
	{"LF", "\n"},
	{"CRLF", "\r\n"},
	{"CR", "\r"},
	{"NEL", "\u0085"},
	{"LS", " "},
	{"PS", " "},
}

// TestSourceLinesAgreesWithNodeLine is the contract the fixups rest on, stated
// as a property rather than a list of cases: for every node in the document,
// the line sourceLines calls Node.Line has to be the line that node was
// written on.
func TestSourceLinesAgreesWithNodeLine(t *testing.T) {
	for _, b := range yamlBreaks {
		t.Run(b.name, func(t *testing.T) {
			src := strings.Join([]string{"alpha: 1", "beta: 2", "", "gamma:", "  delta: 4"}, b.br) + b.br
			lines := sourceLines([]byte(src))

			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
				t.Fatal(err)
			}
			var check func(n *yaml.Node)
			check = func(n *yaml.Node) {
				if n.Kind == yaml.ScalarNode && n.Value != "" {
					if n.Line < 1 || n.Line > len(lines) {
						t.Fatalf("node %q is on line %d, outside the %d lines sourceLines found", n.Value, n.Line, len(lines))
					}
					if got := lines[n.Line-1]; !strings.Contains(got, n.Value) {
						t.Errorf("node %q says line %d, which sourceLines reads as %q", n.Value, n.Line, got)
					}
				}
				for _, c := range n.Content {
					check(c)
				}
			}
			check(&doc)
		})
	}
}

// TestSourceLinesShape pins the two edges every caller depends on: a break
// ends a line and is not part of it, and a source ending in one leaves a final
// empty element that is not a line of the document.
func TestSourceLinesShape(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"LF", "a\nb\n", []string{"a", "b", ""}},
		{"no trailing break", "a\nb", []string{"a", "b"}},
		{"CRLF is one break", "a\r\nb\r\n", []string{"a", "b", ""}},
		{"bare CR", "a\rb\r", []string{"a", "b", ""}},
		{"CR then LF text", "a\r\r\nb", []string{"a", "", "b"}},
		{"NEL", "a\u0085b", []string{"a", "b"}},
		{"LS", "a b", []string{"a", "b"}},
		{"PS", "a b", []string{"a", "b"}},
		{"mixed", "a\r\nb\nc\rd", []string{"a", "b", "c", "d"}},
		{"empty", "", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sourceLines([]byte(tc.src))
			if len(got) != len(tc.want) {
				t.Fatalf("sourceLines(%q) = %q, want %q", tc.src, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("sourceLines(%q) = %q, want %q", tc.src, got, tc.want)
				}
			}
		})
	}
}

// TestEditKeepsAValueAcrossEveryLineBreak is #254's own symptom: a block
// scalar's trailing blank line is content under keep chomping, and the fixups
// could only find it if they agreed with the parser about which line it was
// on.
func TestEditKeepsAValueAcrossEveryLineBreak(t *testing.T) {
	for _, b := range yamlBreaks {
		t.Run(b.name, func(t *testing.T) {
			src := "f: |+" + b.br + "  one" + b.br + b.br + "after: 1" + b.br
			before, err := execute(t, src, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			edited, err := execute(t, src, "set", ".after", "2")
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			after, err := execute(t, edited, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read after: %v\n%q", err, edited)
			}
			if before != after {
				t.Errorf("editing .after changed .f\n--- out\n%q\n before %s  after %s", edited, before, after)
			}
		})
	}
}

// TestEditKeepsAValueInIssue254sOwnDocuments is the issue's two reproductions,
// pasted from the runs that filed it. Both are shapes nobody would write by
// hand, and both are exactly the kind that a line miscount turns into a
// changed value rather than a changed layout.
func TestEditKeepsAValueInIssue254sOwnDocuments(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"bare CR", "f: >+\n\rafter: 1\n", `"\n"`},
		{"NEL", "f: |\u0085\n after: 1\n", `"\nafter: 1\n"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := execute(t, tc.in, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			if strings.TrimSpace(before) != tc.want {
				t.Fatalf("the fixture no longer reads as %s but as %s", tc.want, before)
			}
			edited, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			after, err := execute(t, edited, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read after: %v\n%q", err, edited)
			}
			if before != after {
				t.Errorf("editing .after changed .f\n--- out\n%q\n before %s  after %s", edited, before, after)
			}
		})
	}
}

// TestEditKeepsAValueYamlV3KeepsVerbatim is the regression correcting the line
// numbering introduced, caught before it shipped and worth a test of its own
// because it is the one place yaml.v3 contradicts itself: it counts LS and PS
// as lines, so Node.Line advances over them, but it does *not* normalise them
// to "\n" inside a block scalar's content, where CR and NEL both are.
//
// A block scalar's recorded body can therefore never re-parse to the value the
// decoder produced, so the restore declines it — correctly. But with the lines
// now counted right, blanklines.go could see the blank line *inside* the block
// and mark it as spacing, and the encoder then emitted a second copy into a
// value nothing was going to put back. Hence dropBlockContentBlanks running
// for unverified records too.
func TestEditKeepsAValueYamlV3KeepsVerbatim(t *testing.T) {
	for _, br := range []string{" ", " "} {
		t.Run(br, func(t *testing.T) {
			in := "f: |+" + br + "  one" + br + br + "after: 1" + br
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
				t.Fatalf("read after: %v", err)
			}
			if before != after {
				t.Errorf("editing .after changed .f\n--- out\n%q\n before %s  after %s", edited, before, after)
			}
		})
	}
}

// TestEditKeepsABlankLineAcrossEveryLineBreak is the same disagreement seen
// through blanklines.go: it marks the node whose source line above was blank,
// so a break it cannot count moves every mark after it.
func TestEditKeepsABlankLineAcrossEveryLineBreak(t *testing.T) {
	for _, b := range yamlBreaks {
		t.Run(b.name, func(t *testing.T) {
			src := "a: 1" + b.br + b.br + "b: 2" + b.br
			got, err := execute(t, src, "set", ".a", "9")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			// Compared on the LF-normalised text: whether the endings come
			// back is a separate promise, tested below.
			norm := strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", " ", "\n", " ", "\n").Replace(got)
			if norm != "a: 9\n\nb: 2\n" {
				t.Errorf("the blank line between a and b was not preserved: %q", got)
			}
		})
	}
}

// TestEditKeepsACommentGutterAcrossEveryLineBreak is the third fixup reading
// the same lines — it recovers a gutter's width from the source line the
// comment was written on.
func TestEditKeepsACommentGutterAcrossEveryLineBreak(t *testing.T) {
	for _, b := range yamlBreaks {
		t.Run(b.name, func(t *testing.T) {
			src := "a: 1" + b.br + "b: 2    # four spaces" + b.br
			got, err := execute(t, src, "set", ".a", "9")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(got, "2    # four spaces") {
				t.Errorf("the gutter width was not restored: %q", got)
			}
		})
	}
}

func TestSourceLineEnding(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"all CRLF", "a: 1\r\nb: 2\r\n", "\r\n"},
		{"all CR", "a: 1\rb: 2\r", "\r"},
		{"all LF", "a: 1\nb: 2\n", ""},
		{"CRLF and LF", "a: 1\r\nb: 2\n", ""},
		{"CR and LF", "a: 1\rb: 2\n", ""},
		{"CR and CRLF", "a: 1\rb: 2\r\n", ""},
		{"empty", "", ""},
		{"no break at all", "a: 1", ""},
		// NEL/LS/PS are counted as breaks for line numbering but never
		// written back, so they don't decide the ending either way.
		{"CRLF with an LS", "a: 1\r\nb: 2 c: 3\r\n", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceLineEnding([]byte(tc.source)); got != tc.want {
				t.Errorf("sourceLineEnding(%q) = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}

func TestRestoreLineEnding(t *testing.T) {
	if got := string(restoreLineEnding([]byte("a: 1\n\nb: 2\n"), "\r\n")); got != "a: 1\r\n\r\nb: 2\r\n" {
		t.Errorf("CRLF: got %q", got)
	}
	if got := string(restoreLineEnding([]byte("a: 1\n\nb: 2\n"), "\r")); got != "a: 1\r\rb: 2\r" {
		t.Errorf("CR: got %q", got)
	}
	if got := string(restoreLineEnding([]byte("a: 1\n"), "")); got != "a: 1\n" {
		t.Errorf("none: got %q", got)
	}
}

// TestEditPreservesBareCR is the half the issue left to be decided: a document
// written with classic-Mac endings is written back with them, under the same
// rule CRLF already followed — restore an ending only when the source uses it
// throughout.
func TestEditPreservesBareCR(t *testing.T) {
	in := "a: 1\rb: 2\r"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"set", []string{"set", ".a", "9"}},
		{"delete", []string{"delete", ".b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("want bare CR throughout, got %q", got)
			}
			if !strings.Contains(got, "\r") {
				t.Errorf("want CR preserved, got %q", got)
			}
		})
	}
}

// TestEditMixedBreaksLeftAsLF records the rule rather than leaving it to be
// discovered: there is no single ending to put back, so the encoder's own is
// what the document gets.
func TestEditMixedBreaksLeftAsLF(t *testing.T) {
	for _, src := range []string{"a: 1\rb: 2\n", "a: 1\r\nb: 2\n", "a: 1 b: 2\n"} {
		t.Run(src, func(t *testing.T) {
			got, err := execute(t, src, "set", ".a", "9")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.ContainsAny(got, "\r\u0085  ") {
				t.Errorf("a source mixing break kinds should come back as LF, got %q", got)
			}
		})
	}
}
