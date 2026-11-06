package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestForcedRerenderDeclineDoesNotRewalkTheSubtree: a sequence item cannot be
// re-rendered alone, and walking its subtree again after each decline doubled
// the work at every level, which hung the fuzzers on a deep sequence.
func TestForcedRerenderDeclineDoesNotRewalkTheSubtree(t *testing.T) {
	const depth = 64
	calls := 0
	forceRerender = func(*yaml.Node) bool {
		if calls++; calls > 10*depth {
			t.Fatalf("the hook ran %d times over %d levels", calls, depth)
		}
		return true
	}
	defer func() { forceRerender = nil }()
	in := strings.Repeat("- ", depth) + "x: 1\n"
	got, err := execute(t, in, "set", strings.Repeat(".[0]", depth)+".x", "2")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, got)
	}
	if want := strings.Repeat("- ", depth) + "x: 2\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNodeEqualTreatsNullSpellingsAlike(t *testing.T) {
	var a, b yaml.Node
	if err := yaml.Unmarshal([]byte("{a: ~, b: null, c: }"), &a); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte("{a: null, b: ~, c: null}"), &b); err != nil {
		t.Fatal(err)
	}
	if !nodeEqual(&a, &b) {
		t.Fatal("null spellings compared unequal")
	}
	if err := yaml.Unmarshal([]byte("{a: &x ~, b: null, c: null}"), &b); err != nil {
		t.Fatal(err)
	}
	if nodeEqual(&a, &b) {
		t.Fatal("an anchored null compared equal to a bare one")
	}
}

func TestVerifyDocsNamesTheDocumentThatDiffers(t *testing.T) {
	src := []byte("a: 1\n---\nb: 2\n---\nc: 3\n")
	docs, err := decodeNodes(src)
	if err != nil {
		t.Fatal(err)
	}
	bad, ok := verifyDocs([]byte("a: 1\n---\nb: 9\n---\nc: 3\n"), docs)
	if !ok || bad[0] || !bad[1] || bad[2] {
		t.Fatalf("got %v, %v; want only document 1 bad", bad, ok)
	}
	if _, ok := verifyDocs([]byte("a: 1\n---\nb: 2\n"), docs); ok {
		t.Fatal("a missing document verified")
	}
	if _, ok := verifyDocs([]byte("a: [\n"), docs); ok {
		t.Fatal("an unparseable stream verified")
	}
	// An alias may name an anchor in an earlier document, so the stream is
	// decoded whole, never one document at a time.
	src = []byte("a: &x 1\n---\nb: *x\n")
	if docs, err = decodeNodes(src); err != nil {
		t.Fatal(err)
	}
	if bad, ok := verifyDocs(src, docs); !ok || bad[0] || bad[1] {
		t.Fatalf("a cross-document alias: got %v, %v", bad, ok)
	}
}

// TestRerender covers the edits the splice cannot place: each one re-renders
// the smallest node around it that can stand alone, and nothing outside it.
func TestRerender(t *testing.T) {
	cases := []struct {
		name string
		src  string
		doc  int
		ops  [][3]string
		want string
	}{
		// The value itself, rendered by the encoder where the splice's own
		// renderer declines.
		{"value: new anchored collection", "a: 1\n", 0, [][3]string{{"set", ".b", "&x {c: 1}"}},
			"a: 1\nb: &x {c: 1}\n"},
		{"value: multi-line scalar", "a: 1\nb: 2\n", 0, [][3]string{{"set", ".a", "|\n  x\n  y\n"}},
			"a: |\n  x\n  y\nb: 2\n"},
		{"value: multi-line scalar into an anchored sequence", "s:\n  l: &p\n    - 80\n    - 443\nz: 1\n", 0,
			[][3]string{{"set", ".s.l[1]", "|\n  x\n  y\n"}},
			"s:\n  l: &p\n    - 80\n    - |\n      x\n      y\nz: 1\n"},
		// The outermost flow collection.
		{"flow: replace inside", "a: [1, 2] # c\nb:   1\n", 0, [][3]string{{"set", ".a[0]", "9"}},
			"a: [9, 2] # c\nb:   1\n"},
		{"flow: delete from a mapping", "m: {a: 1, b: 2}\n", 0, [][3]string{{"delete", ".m.a", ""}},
			"m: {b: 2}\n"},
		{"flow: nested, outermost re-rendered", "m: {a: [1, 2]}\nz:  1\n", 0, [][3]string{{"set", ".m.a[1]", "3"}},
			"m: {a: [1, 3]}\nz:  1\n"},
		{"flow: over several lines with a comment", "a: [1, # one\n  2]\nb:  1\n", 0, [][3]string{{"set", ".a[1]", "3"}},
			"a: [1, # one\n  3]\nb:  1\n"},
		{"flow: root", "[1, 2]\n", 0, [][3]string{{"set", "[0]", "5"}},
			"[5, 2]\n"},
		{"flow: cross-document alias", "a: &x 1\n---\nl: [*x, 2]\n", 1, [][3]string{{"set", ".l[1]", "3"}},
			"a: &x 1\n---\nl: [*x, 3]\n"},
		// The block collection under a key.
		{"block: sequence item replaced", "l:\n  - x: 1\n  - y\n\nm:   1\n", 0, [][3]string{{"set", ".l[0]", "5"}},
			"l:\n  - 5\n  - y\n\nm:   1\n"},
		{"block: anchored value keeps its key line", "k: &p # c\n  - x: 1\n  - y: 2\nm: 1\n", 0, [][3]string{{"set", ".k[1]", "5"}},
			"k: &p # c\n  - x: 1\n  - 5\nm: 1\n"},
		{"block: tagged value keeps its key line", "k: !t # c\n  - x: 1\n  - y: 2\nm: 1\n", 0, [][3]string{{"set", ".k[1]", "5"}},
			"k: !t # c\n  - x: 1\n  - 5\nm: 1\n"},
		{"block: head comment inside the body", "k:\n  # h\n  - x: 1\n  - y: 2\nz: 1\n", 0, [][3]string{{"set", ".k[1]", "5"}},
			"k:\n  # h\n  - x: 1\n  - 5\nz: 1\n"},
		{"block: block scalar shifts, blank lines unpadded", "k:\n  - |\n    a\n\n    b\n  - x: 1\n", 0, [][3]string{{"set", ".k[1]", "5"}},
			"k:\n  - |\n    a\n\n    b\n  - 5\n"},
		{"block: CRLF", "l:\r\n  - x: 1\r\n  - y\r\n", 0, [][3]string{{"set", ".l[0]", "5"}},
			"l:\r\n  - 5\r\n  - y\r\n"},
		// The document.
		{"doc: last root key deleted", "# head\na: 1\n", 0, [][3]string{{"delete", ".a", ""}},
			"{}\n"},
		{"doc: markers, blank lines and other documents untouched", "a:  1\n---\n# c\nb: 1\n\n...\n---\nc:   3\n", 1,
			[][3]string{{"delete", ".b", ""}},
			"a:  1\n---\n{}\n\n...\n---\nc:   3\n"},
		{"doc: root on the marker line", "x:  1\n--- !t\na: 1\n", 1, [][3]string{{"delete", ".a", ""}},
			"x:  1\n--- !t {}\n"},
		{"doc: white space on a blank line after the root", "- 0\n \t\n---\nb:  1\n", 0, [][3]string{{"delete", "[0]", ""}},
			"[]\n\n---\nb:  1\n"},
		{"doc: empty document that is not the last", "---\n---\nb:  1\n", 0, [][3]string{{"set", ".a", "1"}},
			"---\na: 1\n---\nb:  1\n"},
		// #353: the document's re-rendered span runs all the way to a
		// source with no trailing line break of its own, and the new
		// content's last line is a block scalar — which only holds its
		// last line as value when a break ends it, so the written result
		// needs one even though nothing in the source had one to carry
		// forward.
		{"doc: new block scalar is the document's last line, source has no trailing newline",
			"0:", 0, [][3]string{{"set", "A", ">\n 0\r"}},
			"0:\nA: >\n  0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := spliceOps(t, tc.src, tc.doc, tc.ops)
			if err != nil {
				t.Fatalf("%v: %v", tc.ops, err)
			}
			if string(out) != tc.want {
				t.Fatalf("%v:\n got %q\nwant %q", tc.ops, out, tc.want)
			}
		})
	}
}

// TestRerenderIndentAppliesToTheRerenderedNode: --indent sets the width of
// what is re-rendered, and nothing else moves.
func TestRerenderIndentAppliesToTheRerenderedNode(t *testing.T) {
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.MappingNode && n.Line == 2 }
	defer func() { forceRerender = nil }()
	got, err := execute(t, "k:\n  a:\n    b: 1\n  c: 2\nz:\n  w: 1\n", "set", "--indent", "4", ".k.c", "3")
	if err != nil {
		t.Fatal(err)
	}
	if want := "k:\n  a:\n      b: 1\n  c: 3\nz:\n  w: 1\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRerenderFallsBackToTheDocument: a re-render that does not read back as
// the edited tree is redone at the document level.
func TestRerenderFallsBackToTheDocument(t *testing.T) {
	rerenderMangle = func(level, text string) string {
		if level == "flow" {
			return "[oops]"
		}
		return text
	}
	defer func() { rerenderMangle = nil }()
	out, _, err := spliceOps(t, "a: [1, 2]\nb: 1\n", 0, [][3]string{{"set", ".a[0]", "9"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: [9, 2]\nb: 1\n"; string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestRerenderFailureWritesNothing: when even the document does not read back,
// the edit fails, and -i leaves the file as it was.
func TestRerenderFailureWritesNothing(t *testing.T) {
	rerenderMangle = func(string, string) string { return "{oops: 1}" }
	defer func() { rerenderMangle = nil }()
	src := "a: 1\n"
	file := filepath.Join(t.TempDir(), "f.yaml")
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := execute(t, "", "delete", "-i", ".a", file)
	if err == nil || errors.Is(err, errDecline) {
		t.Fatalf("got %v, want a hard error", err)
	}
	if code := codeFor(err); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	got, _ := os.ReadFile(file) //nolint:gosec // G304: the test's own temp file
	if string(got) != src {
		t.Fatalf("file changed to %q", got)
	}
}

// TestRerenderForced: the test hook re-renders a node the splice could have
// placed, and only that node.
func TestRerenderForced(t *testing.T) {
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.MappingNode && n.Line == 2 }
	defer func() { forceRerender = nil }()
	out, _, err := spliceOps(t, "a:\n  b:    1\n\n\n  c: 2\nd:    3\n", 0, [][3]string{{"set", ".a.b", "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a:\n  b: 5\n  c: 2\nd:    3\n"; string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestEditKeepsAByteOrderMark(t *testing.T) {
	got, err := execute(t, "\ufeffa: 1 # c\n", "set", ".a", "2")
	if err != nil {
		t.Fatal(err)
	}
	if want := "\ufeffa: 2 # c\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEditTranscodesUTF16(t *testing.T) {
	got, err := execute(t, "\xff\xfea\x00:\x00 \x001\x00\n\x00", "set", ".a", "2")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 2\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestEditTranscodesUTF16BigEndian is TestEditTranscodesUTF16's byte order
// reversed: the BOM (\xfe\xff) and each UTF-16 code unit's two bytes are
// both big-endian, where the other test's are both little-endian.
func TestEditTranscodesUTF16BigEndian(t *testing.T) {
	got, err := execute(t, "\xfe\xff\x00a\x00:\x00 \x001\x00\n", "set", ".a", "2")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 2\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRerenderQuotesATrailingKeepBlock: a keep-chomped block written last in a
// re-render would take the source's blank lines after it as content, so it is
// double-quoted instead.
func TestRerenderQuotesATrailingKeepBlock(t *testing.T) {
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.MappingNode && n.Line == 2 }
	defer func() { forceRerender = nil }()
	out, _, err := spliceOps(t, "c:\n  a: 0\n  s: |+\n    run\n\n\nz: 1\n", 0, [][3]string{{"set", ".c.a", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "c:\n  a: 1\n  s: \"run\\n\\n\\n\"\nz: 1\n"; string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestRerenderQuotesALoneLineBreakWrittenLast: "\n" is a keep-chomped block
// too, with no body to tell it from an empty one once encode has trimmed the
// rendering's trailing breaks, so the edit was refused as unreadable.
func TestRerenderQuotesALoneLineBreakWrittenLast(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		args     []string
		want     string
	}{
		{"literal", "a: |\nc: |+\n\n", []string{"set", ".a", "zz"}, "a: zz\nc: \"\\n\"\n"},
		{"folded", "a: |\nc: >+\n\n", []string{"set", ".a", "zz"}, "a: zz\nc: \"\\n\"\n"},
		{"explicit indent", "a: |\nc: |2+\n\n", []string{"set", ".a", "zz"}, "a: zz\nc: \"\\n\"\n"},
		{"mid-stream", "a: |\nc: |+\n\n---\nd: 1\n", []string{"set", ".a", "zz", "--doc", "0"}, "a: zz\nc: \"\\n\"\n---\nd: 1\n"},
		{"nested", "x:\n  a: |\n  c: |+\n\n", []string{"set", ".x.a", "zz"}, "x:\n  a: zz\n  c: \"\\n\"\n"},
		{"sequence item", "- |\n- |+\n\n", []string{"set", ".[0]", "1"}, "- 1\n- \"\\n\"\n"},
		{"delete", "? a\n: 1\nc: |+\n\n", []string{"delete", ".a"}, "c: \"\\n\"\n"},
		{"rename", "&n a: 1\nc: |+\n\n", []string{"rename", ".a", "b"}, "&n b: 1\nc: \"\\n\"\n"},
		{"set value", "a: 1\n", []string{"set", ".a", "|+\n\n"}, "a: \"\\n\"\n"},
		{"append value", "a:\n  - 1\n", []string{"append", ".a", "|+\n\n"}, "a:\n  - 1\n  - \"\\n\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRerenderKeepsMergeKeySpellings: a re-render writes `<<` as the source
// did, tag or no tag.
func TestRerenderKeepsMergeKeySpellings(t *testing.T) {
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.DocumentNode }
	defer func() { forceRerender = nil }()
	in := "base: &b\n  x: 1\nm:\n  <<: *b\n  y: 2\nn:\n  !!merge <<: *b\n  y: 3\n"
	got, err := execute(t, in, "set", ".m.y", "9")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, got)
	}
	if want := strings.Replace(in, "y: 2", "y: 9", 1); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestLeadingLineBreakSurvivesRendering: the value opens with a line break, so
// a block rendering has to keep it. A sequence item at --indent 4 still cannot
// be written as a block, which is why that width double-quotes.
func TestLeadingLineBreakSurvivesRendering(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		got, err := execute(t, "f: \"\\none\"\n", ".")
		if err != nil {
			t.Fatalf("execute: %v\n%s", err, got)
		}
		if want := "f: |2-\n\n  one\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("unforced re-render", func(t *testing.T) {
		got, err := execute(t, "a: |\nc: |\n\n  one\n", "set", ".a", "zz")
		if err != nil {
			t.Fatalf("execute: %v\n%s", err, got)
		}
		if want := "a: zz\nc: |2\n\n  one\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"forced at indent 2", nil, "f: |2\n\n  one\nz: 2\n"},
		{"forced at indent 4", []string{"--indent", "4"}, "f: \"\\none\\n\"\nz: 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.DocumentNode }
			defer func() { forceRerender = nil }()
			args := append([]string{"set", ".z", "2"}, tc.args...)
			got, err := execute(t, "f: |\n\n  one\nz: 1\n", args...)
			if err != nil {
				t.Fatalf("execute: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRerenderFillsALastEmptyDocumentWithoutAFinalBreak: the text goes after
// the marker on a line of its own, and the source's missing final break stays
// missing.
func TestRerenderFillsALastEmptyDocumentWithoutAFinalBreak(t *testing.T) {
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.DocumentNode }
	defer func() { forceRerender = nil }()
	out, _, err := spliceOps(t, "a: 1\n---", 1, [][3]string{{"set", ".b", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 1\n---\nb: 1"; string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
