package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditValueSpelledLikeAMarker guards #286: the splicing fixups swap every
// marker-shaped token in the encoder's output for a recorded source spelling,
// so a marker has to collide with nothing the encoder sees — the edit's own
// values and keys included, not just the source it was read from.
func TestEditValueSpelledLikeAMarker(t *testing.T) {
	edits := filepath.Join(t.TempDir(), "edits")
	if err := os.WriteFile(edits, []byte("set .f = yaymlqBlockScalar0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, in   string
		args       []string
		path, want string
	}{
		{"set over a block", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "yaymlqBlockScalar0"}, ".f", `"yaymlqBlockScalar0"`},
		{"set over a flow collection", "a: [1,  2]\nf: 0\n",
			[]string{"set", ".f", "yaymlqFlowNode0"}, ".f", `"yaymlqFlowNode0"`},
		{"set over a quoted scalar", "q: \"\\x41\"\nf: 0\n",
			[]string{"set", ".f", "yaymlqQuotedScalar0"}, ".f", `"yaymlqQuotedScalar0"`},
		{"rename to a marker", "b: |\n  keep\nf: 0\n",
			[]string{"rename", ".f", "yaymlqBlockScalar0"}, ".yaymlqBlockScalar0", "0"},
		{"append a marker", "b: |\n  keep\nl: [x]\n",
			[]string{"append", ".l", "yaymlqBlockScalar0"}, ".l[1]", `"yaymlqBlockScalar0"`},
		{"apply a marker", "b: |\n  keep\nf: 0\n",
			[]string{"apply", "-f", edits}, ".f", `"yaymlqBlockScalar0"`},
		{"marker core with a suffix", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "yaymlqBlockScalarz0"}, ".f", `"yaymlqBlockScalarz0"`},
		{"marker with a trailing letter", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "yaymlqBlockScalar0x"}, ".f", `"yaymlqBlockScalar0x"`},
		{"marker inside a longer word", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "xyaymlqBlockScalar0"}, ".f", `"xyaymlqBlockScalar0"`},
		{"set over the masked node itself", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".b", "yaymlqBlockScalar0"}, ".b", `"yaymlqBlockScalar0"`},
		{"marker as a tag", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "!yaymlqBlockScalar0 v"}, ".f", `"v"`},
		{"marker as an anchor", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "&yaymlqBlockScalar0 v"}, ".f", `"v"`},
		{"source spelling kept too", "b: |\n  keep\nf: 0\n",
			[]string{"set", ".f", "yaymlqBlockScalar0"}, ".b", `"keep\n"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edited, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			got, err := execute(t, edited, "-o", "json", tc.path)
			if err != nil {
				t.Fatalf("read back %s: %v\n%q", tc.path, err, edited)
			}
			if got != tc.want+"\n" && got != tc.want {
				t.Fatalf("%s: want %s got %s\n%q", tc.path, tc.want, got, edited)
			}
		})
	}
}

// TestEditCommentSpelledLikeAMarker is #286 through a comment: set's value is
// YAML, so a comment in it reaches the output, and one spelled like a marker
// was taken for one — a lone dash's, which removed the comment, or a block's,
// which spliced another node's text in after it.
func TestEditCommentSpelledLikeAMarker(t *testing.T) {
	cases := []struct{ name, in, value, want string }{
		{"lone dash", "l:\n-\n  a: 1\nf: 0\n", "x # yaymlqLoneDash", "f: x # yaymlqLoneDash\n"},
		{"block", "b: |\n  keep\nf: 0\n", "v # yaymlqBlockScalar0", "f: v # yaymlqBlockScalar0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", ".f", tc.value)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.HasSuffix(got, tc.want) {
				t.Fatalf("want output ending %q, got\n%q", tc.want, got)
			}
		})
	}
}
