package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"gopkg.in/yaml.v3"
)

func TestLineTableOffsets(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		line, col int
		want      int
	}{
		{"first line", "a: 1\nb: 2\n", 1, 4, 3},
		{"second line, LF", "a: 1\nb: 2\n", 2, 1, 5},
		{"CRLF counts as one break", "a: 1\r\nb: 2\r\n", 2, 4, 9},
		{"bare CR", "a: 1\rb: 2\r", 2, 1, 5},
		{"NEL", "a: 1\u0085b: 2\n", 2, 1, 6},
		{"LS", "a: 1\u2028b: 2\n", 2, 1, 7},
		{"PS", "a: 1\u2029b: 2\n", 2, 1, 7},
		{"column counts runes", "é: ü\n", 1, 4, 4},
		{"last line without a break", "a: 1\nb", 2, 1, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newLineTable([]byte(tc.src)).offset(tc.line, tc.col)
			if err != nil {
				t.Fatalf("offset(%d, %d): %v", tc.line, tc.col, err)
			}
			if got != tc.want {
				t.Fatalf("offset(%d, %d) = %d, want %d", tc.line, tc.col, got, tc.want)
			}
		})
	}
}

func TestLineTableOffsetOutOfRangeDeclines(t *testing.T) {
	lt := newLineTable([]byte("a: 1\n"))
	for _, pos := range [][2]int{{0, 1}, {3, 1}, {1, 9}} {
		if _, err := lt.offset(pos[0], pos[1]); !errors.Is(err, errDecline) {
			t.Errorf("offset(%d, %d) = %v, want a decline", pos[0], pos[1], err)
		}
	}
}

// spliceRun decodes src, applies one edit to document doc with ymledit the way
// the verbs do, and splices the result.
func spliceRun(t testing.TB, src string, doc int, verb, expr, arg string) ([]byte, []edit, error) {
	t.Helper()
	return spliceOps(t, src, doc, [][3]string{{verb, expr, arg}})
}

// spliceOps is spliceRun for several edits to one document, the way apply
// runs a script.
func spliceOps(t testing.TB, src string, doc int, ops [][3]string) ([]byte, []edit, error) {
	t.Helper()
	data := []byte(src)
	lines := sourceLines(data)
	docs, err := decodeNodes(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	snap := snapshotTree(docs)
	for _, op := range ops {
		if err := applyOp(t, docs[doc], op[0], op[1], op[2]); err != nil {
			return nil, nil, err
		}
	}
	indent := detectIndent(lines)
	if indent < 1 {
		indent = 2
	}
	return splice(data, docs, snap, indent)
}

func applyOp(t testing.TB, doc *yaml.Node, verb, expr, arg string) error {
	t.Helper()
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	switch verb {
	case "set":
		v, perr := ymledit.ParseValue(arg, false)
		if perr != nil {
			t.Fatalf("value %q: %v", arg, perr)
		}
		return ymledit.Set(doc, segs, v, nil)
	case "delete":
		return ymledit.Delete(doc, segs, nil)
	case "rename":
		return ymledit.Rename(doc, segs, arg, nil)
	case "append":
		v, perr := ymledit.ParseValue(arg, false)
		if perr != nil {
			t.Fatalf("value %q: %v", arg, perr)
		}
		return ymledit.Append(doc, segs, v, nil)
	}
	t.Fatalf("unknown verb %q", verb)
	return nil
}

func TestSpliceKeepsEveryOtherByte(t *testing.T) {
	cases := []struct {
		name string
		src  string
		args []string
		want string
	}{
		{
			name: "set keeps blank runs and comments elsewhere",
			src:  "# head\na: 1   # gutter\n\n\n\nb: 2\n",
			args: []string{"set", ".b", "3"},
			want: "# head\na: 1   # gutter\n\n\n\nb: 3\n",
		},
		{
			name: "set keeps the value's own inline comment",
			src:  "a: 1 # one\nb: 2\n",
			args: []string{"set", ".a", "9"},
			want: "a: 9 # one\nb: 2\n",
		},
		{
			name: "set replaces a quoted value with the encoder's spelling",
			src:  "a: 'x'\nb: \"y\"\n",
			args: []string{"set", ".b", "z"},
			want: "a: 'x'\nb: z\n",
		},
		{
			name: "set keeps a tag and anchor elsewhere",
			src:  "a: &x !t 1\nb: 2\n",
			args: []string{"set", ".b", "3"},
			want: "a: &x !t 1\nb: 3\n",
		},
		{
			name: "set an item in a block sequence",
			src:  "l:\n- a\n- b   # c\n",
			args: []string{"set", ".l[1]", "z"},
			want: "l:\n- a\n- z   # c\n",
		},
		{
			name: "set keeps mixed line breaks",
			src:  "a: 1\r\nb: 2\nc: 3\r\n",
			args: []string{"set", ".b", "9"},
			want: "a: 1\r\nb: 9\nc: 3\r\n",
		},
		{
			name: "set keeps an own-line block header elsewhere",
			src:  "a: |\n  text\nb: 1\n",
			args: []string{"set", ".b", "2"},
			want: "a: |\n  text\nb: 2\n",
		},
		{
			// #329: replacing an alias's value used to decline (no case
			// matched an AliasNode) and fall back to re-rendering the
			// whole document, collapsing the blank lines below.
			name: "set replaces a scalar value that is an alias",
			src:  "a: &x 1\nb: *x\n\n\nc: 3\n",
			args: []string{"set", ".b", "9"},
			want: "a: &x 1\nb: 9\n\n\nc: 3\n",
		},
		{
			name: "set replaces a block-mapping value that is an alias",
			src:  "x: &d\n  restart: always\n\nweb: *d\n\napi: 1\n",
			args: []string{"set", ".web", "{restart: never}"},
			want: "x: &d\n  restart: always\n\nweb: {restart: never}\n\napi: 1\n",
		},
		{
			name: "rename keeps the value and its comment",
			src:  "a: 1  # one\nb:\n  c: 2\n",
			args: []string{"rename", ".b", "z"},
			want: "a: 1  # one\nz:\n  c: 2\n",
		},
		{
			name: "rename quotes a key that needs it",
			src:  "a: 1\n",
			args: []string{"rename", ".a", "x: y"},
			want: "'x: y': 1\n",
		},
		{
			name: "delete a middle entry with its head comment",
			src:  "a: 1\n# about b\nb:\n  x: 1\n\nc: 3\n",
			args: []string{"delete", ".b"},
			want: "a: 1\nc: 3\n",
		},
		{
			name: "delete the last entry drops the blank lines above it",
			src:  "a: 1\n\nb: 2\n",
			args: []string{"delete", ".b"},
			want: "a: 1\n",
		},
		{
			name: "delete a sequence item",
			src:  "l:\n  - a\n  - b\n  - c\n",
			args: []string{"delete", ".l[1]"},
			want: "l:\n  - a\n  - c\n",
		},
		{
			name: "delete the only entry of a nested mapping writes {}",
			src:  "a:\n  b: 1\nc: 2\n",
			args: []string{"delete", ".a.b"},
			want: "a: {}\nc: 2\n",
		},
		{
			name: "delete the only item of a nested sequence writes []",
			src:  "a: # keep\n  - x\nc: 2\n",
			args: []string{"delete", ".a[0]"},
			want: "a: [] # keep\nc: 2\n",
		},
		{
			name: "delete in the second document leaves the first alone",
			src:  "a:   1\n\n\n---\nb: 2\nc: 3\n",
			args: []string{"delete", "--doc", "1", ".c"},
			want: "a:   1\n\n\n---\nb: 2\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.src, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v (%s)", tc.args, err, got)
			}
			if got != tc.want {
				t.Fatalf("%v:\n got %q\nwant %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestSpliceWidenedSpans covers the spans stage 1 locates: block scalars,
// scalars over several lines, and items whose dash is not on their line. It
// drives the splice directly, so a pass cannot come from the encoder fallback.
func TestSpliceWidenedSpans(t *testing.T) {
	cases := []struct {
		name              string
		src               string
		verb, expr, value string
		want              string
	}{
		{
			name: "block value, header comment and following blanks kept",
			src:  "a: | # note\n  one\n  two\n\n\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a: x # note\n\n\nb: 1\n",
		},
		{
			name: "keep block's trailing blanks go with it",
			src:  "a: |+\n  one\n\n\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a: x\nb: 1\n",
		},
		{
			name: "indentation indicator in a sequence item",
			src:  "l:\n  - |2-\n      indented\n  - z\n",
			verb: "set", expr: ".l[0]", value: "x",
			want: "l:\n  - x\n  - z\n",
		},
		{
			name: "own-line header",
			src:  "a:\n  |\n  one\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a:\n  x\nb: 1\n",
		},
		{
			name: "multi-line plain value before a comment",
			src:  "a: one\n  two\n  three\n# after\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a: x\n# after\nb: 1\n",
		},
		{
			name: "multi-line double-quoted value",
			src:  "a: \"one\n  two\" # c\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a: x # c\nb: 1\n",
		},
		{
			name: "multi-line single-quoted value",
			src:  "a: 'one\n\n  two'\nb: 1\n",
			verb: "set", expr: ".a", value: "x",
			want: "a: x\nb: 1\n",
		},
		{
			name: "delete a lone-dash item mid-sequence",
			src:  "l:\n  -\n    a: 1\n  - b\n",
			verb: "delete", expr: ".l[0]",
			want: "l:\n  - b\n",
		},
		{
			name: "delete a lone-dash item at the end",
			src:  "l:\n  - b\n  -\n    a: 1\nc: 2\n",
			verb: "delete", expr: ".l[1]",
			want: "l:\n  - b\nc: 2\n",
		},
		{
			name: "delete an item whose dash carries a comment",
			src:  "l:\n- b\n- # about\n  a: 1\n- c\n",
			verb: "delete", expr: ".l[1]",
			want: "l:\n- b\n- c\n",
		},
		{
			name: "delete the last entry under a keep block keeps its blanks",
			src:  "a: |+\n  one\n\n\nb: 1\n",
			verb: "delete", expr: ".b",
			want: "a: |+\n  one\n\n\n",
		},
		{
			name: "delete an entry whose value is a keep block",
			src:  "x: 1\na: |+\n  one\n\n\nb: 1\n",
			verb: "delete", expr: ".a",
			want: "x: 1\nb: 1\n",
		},
		{
			name: "delete the last entry when its value is a keep block",
			src:  "x: 1\na: |+\n  one\n\n",
			verb: "delete", expr: ".a",
			want: "x: 1\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := spliceRun(t, tc.src, 0, tc.verb, tc.expr, tc.value)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.verb, tc.expr, err)
			}
			if string(out) != tc.want {
				t.Fatalf("%s %s:\n got %q\nwant %q", tc.verb, tc.expr, out, tc.want)
			}
		})
	}
}

// TestSpliceFallsBack covers the edits the splice cannot place as they are:
// each lands at the smallest level of the re-render ladder that holds it
// (rerender.go), "" being the splice itself, its value written by the
// encoder where the splice's renderer declines.
func TestSpliceFallsBack(t *testing.T) {
	cases := []struct {
		name              string
		src               string
		verb, expr, value string
		want, level       string
	}{
		{"append to a flow sequence over several lines", "l: [a,\n  b]\n", "append", ".l", "c", "l: [a, b, c]\n", "flow"},
		{"append after a compact nested item", "l:\n- - a\n", "append", ".l[0]", "b", "l:\n- - a\n  - b\n", "block"},
		{"null sequence item becomes a mapping", "l:\n- ~\n", "set", ".l[0].a", "1", "l:\n- a: 1\n", "block"},
		{"new anchored collection", "a: 1\n", "set", ".b", "&x {c: 1}", "a: 1\nb: &x {c: 1}\n", ""},
		{"new tagged collection", "a: 1\n", "set", ".b", "!t {c: 1}", "a: 1\nb: !t {c: 1}\n", ""},
		{"new key brings a comment", "a: 1\n", "set", ".b", "x # c", "a: 1\nb: x # c\n", ""},
		{"new collection brings a comment", "a: x\n", "set", ".a", "p: 1 # c", "a:\n  p: 1 # c\n", ""},
		{"multi-line rendering", "a: 1\n", "set", ".a", "|\n  x\n  y\n", "a: |\n  x\n  y\n", ""},
		{"flow collection", "a: {b: 1}\n", "set", ".a.b", "2", "a: {b: 2}\n", "flow"},
		{"delete in flow", "a: [1, 2]\n", "delete", ".a[0]", "", "a: [2]\n", "flow"},
		{"value brings its own comment", "a: 1\n", "set", ".a", "2 # c", "a: 2 # c\n", "doc"},
		{"anchored key", "&k a: 1\n", "rename", ".a", "b", "&k b: 1\n", "doc"},
		{"explicit key", "? a\n: 1\nb: 2\n", "delete", ".a", "", "b: 2\n", "doc"},
		{"last entry of the root", "a: 1\n", "delete", ".a", "", "{}\n", "doc"},
		{"empty node past the last line", "  -\n  - ?", "delete", "[1]", "", "-", "doc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, edits, err := spliceRun(t, tc.src, 0, tc.verb, tc.expr, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Fatalf("got %q, want %q", out, tc.want)
			}
			for _, e := range edits {
				if e.level != tc.level {
					t.Fatalf("an edit at level %q, want %q", e.level, tc.level)
				}
			}
		})
	}
}

// TestSpliceDeclinesAByteOrderMark: the splice's offsets are counted in the
// text after a BOM, which trySplice sets aside before calling it.
func TestSpliceDeclinesAByteOrderMark(t *testing.T) {
	if out, _, err := spliceRun(t, "\ufeffa: 1\n", 0, "set", ".a", "2"); !errors.Is(err, errDecline) {
		t.Fatalf("got %q, %v; want a decline", out, err)
	}
}

// TestSpliceIndentAppliesToInsertedText: the splice never re-indents what the
// source holds, so --indent sets only the indentation of new text.
func TestSpliceIndentAppliesToInsertedText(t *testing.T) {
	got, err := execute(t, "a:\n  b: 1\n\n\nc: 2\n", "set", "--indent", "4", ".d.e", "1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a:\n  b: 1\n\n\nc: 2\nd:\n    e: 1\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSpliceInsertions covers the edits stage 2 splices: new keys, chains of
// them, a null or scalar turned into a collection, and appended items. It drives
// the splice directly, so a pass cannot come from the encoder fallback.
func TestSpliceInsertions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		doc  int
		ops  [][3]string
		want string
	}{
		{"new key after a block value, before blanks and a comment",
			"a:\n  x: |\n    text\n\n# tail\nb: 1\n", 0, [][3]string{{"set", ".a.y", "2"}},
			"a:\n  x: |\n    text\n  y: 2\n\n# tail\nb: 1\n"},
		{"chain of missing keys", "a: 1\n", 0, [][3]string{{"set", ".b.c.d", "x"}},
			"a: 1\nb:\n  c:\n    d: x\n"},
		{"block sequence value, indentless source", "l:\n- a\nm: 1\n", 0, [][3]string{{"set", ".n", "- x\n- y"}},
			"l:\n- a\nm: 1\nn:\n- x\n- y\n"},
		{"block sequence value, indented source", "l:\n  - a\nm: 1\n", 0, [][3]string{{"set", ".n", "- x\n- y"}},
			"l:\n  - a\nm: 1\nn:\n  - x\n  - y\n"},
		{"empty null becomes a mapping", "a:\nb: 1\n", 0, [][3]string{{"set", ".a.c", "1"}},
			"a:\n  c: 1\nb: 1\n"},
		{"spelled null with a comment becomes a mapping", "a: ~ # c\nb: 1\n", 0, [][3]string{{"set", ".a.c", "1"}},
			"a: # c\n  c: 1\nb: 1\n"},
		{"null root becomes a mapping", "~\n", 0, [][3]string{{"set", ".a", "1"}},
			"a: 1\n"},
		{"scalar to a flow sequence", "a: x\nb: 1\n", 0, [][3]string{{"set", ".a", "[1, 2]"}},
			"a: [1, 2]\nb: 1\n"},
		{"scalar to a block mapping", "a: x # c\nb: 1\n", 0, [][3]string{{"set", ".a", "p: 1\nq: 2"}},
			"a: # c\n  p: 1\n  q: 2\nb: 1\n"},
		{"block mapping to a scalar", "a:\n  b: 1\nc: 2\n", 0, [][3]string{{"set", ".a", "5"}},
			"a: 5\nc: 2\n"},
		{"append to an indented sequence", "l:\n  - a\n", 0, [][3]string{{"append", ".l", "b"}},
			"l:\n  - a\n  - b\n"},
		{"append to an indentless sequence", "l:\n- a\nm: 1\n", 0, [][3]string{{"append", ".l", "b"}},
			"l:\n- a\n- b\nm: 1\n"},
		{"append a mapping to a sequence of mappings", "l:\n  - n: a\n    p: 1\n", 0, [][3]string{{"append", ".l", "n: b\np: 2"}},
			"l:\n  - n: a\n    p: 1\n  - n: b\n    p: 2\n"},
		{"append to a flow sequence", "l: [a, b] # c\n", 0, [][3]string{{"append", ".l", "c d"}},
			"l: [a, b, c d] # c\n"},
		{"append to an empty flow sequence", "x: []\n", 0, [][3]string{{"append", ".x", "a,b"}},
			"x: ['a,b']\n"},
		{"set into an empty document", "---\n", 0, [][3]string{{"set", ".a", "1"}},
			"---\na: 1\n"},
		{"no final newline", "a: 1", 0, [][3]string{{"set", ".b", "2"}},
			"a: 1\nb: 2"},
		{"CRLF source", "a: 1\r\nb: 2\r\n", 0, [][3]string{{"set", ".c.d", "3"}},
			"a: 1\r\nb: 2\r\nc:\r\n  d: 3\r\n"},
		{"parent key and child append at one offset", "a:\n  l:\n    - x\n", 0,
			[][3]string{{"append", ".a.l", "y"}, {"set", ".a.k", "v"}},
			"a:\n  l:\n    - x\n    - y\n  k: v\n"},
		{"second document", "a: 1\n---\nb: 2\n\n---\nc: 3\n", 1, [][3]string{{"set", ".n", "x"}},
			"a: 1\n---\nb: 2\nn: x\n\n---\nc: 3\n"},
		{"append after a trailing comma", "l: [1, 2,]\n", 0, [][3]string{{"append", ".l", "3"}},
			"l: [1, 2, 3]\n"},
		{"append to an anchored flow sequence", "l: &a [1]\n", 0, [][3]string{{"append", ".l", "2"}},
			"l: &a [1, 2]\n"},
		{"new key in a flow mapping", "m: {a: 1}\n", 0, [][3]string{{"set", ".m.b", "x y"}},
			"m: {a: 1, b: x y}\n"},
		{"flow collection to a block mapping", "a: [1] # c\nb: 1\n", 0, [][3]string{{"set", ".a", "p: 1"}},
			"a: # c\n  p: 1\nb: 1\n"},
		{"block scalar to a block mapping", "a: | # c\n  x\nb: 1\n", 0, [][3]string{{"set", ".a", "p: 1"}},
			"a: # c\n  p: 1\nb: 1\n"},
		{"multi-line plain scalar to a block mapping", "a: x\n  y\nb: 1\n", 0, [][3]string{{"set", ".a", "p: 1"}},
			"a:\n  p: 1\nb: 1\n"},
		{"block mapping to a block sequence", "a:\n  b: 1\n\nc: 2\n", 0, [][3]string{{"set", ".a", "- 1"}},
			"a:\n  - 1\n\nc: 2\n"},
		{"nested new collections", "a: 1\n", 0, [][3]string{{"set", ".b", "- k: v\n  l:\n  - 1\n- [x]"}},
			"a: 1\nb:\n  - k: v\n    l:\n      - 1\n  - [x]\n"},
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

func TestSnapshotDiff(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\nb: [x]\n"), &doc); err != nil {
		t.Fatal(err)
	}
	snap := snapshotTree([]*yaml.Node{&doc})
	m := doc.Content[0]
	if ownChanged(snap[m], m) || contentChanged(snap[m], m) {
		t.Fatal("untouched mapping reads as changed")
	}
	m.Content[0].Value = "z"
	if !ownChanged(snap[m.Content[0]], m.Content[0]) {
		t.Error("renamed key does not read as changed")
	}
	if contentChanged(snap[m], m) {
		t.Error("a rename changed the parent's content")
	}
	m.Content[1] = &yaml.Node{Kind: yaml.ScalarNode, Value: "2"}
	if !contentChanged(snap[m], m) {
		t.Error("a replaced value does not change the parent's content")
	}
	seq := m.Content[3]
	seq.Content = seq.Content[:0]
	if !contentChanged(snap[seq], seq) {
		t.Error("a deleted item does not change the sequence's content")
	}
}

// TestSpliceCorpus edits every entry of every fidelity fixture three ways —
// set a scalar, rename a key, delete the entry — and holds each to the splice
// contract: decline, or change only the target's own lines. The splice rate is
// the coverage stage 0 gives; a floor keeps it from quietly sinking.
func TestSpliceCorpus(t *testing.T) {
	dir := filepath.Join("..", "testdata", "fidelity")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total, spliced := 0, 0
	levels := map[string]int{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: a fixture in this repo
		if err != nil {
			t.Fatal(err)
		}
		data := src
		docs, err := decodeNodes(data)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		for di, d := range docs {
			for _, tg := range corpusTargets(d) {
				for _, op := range tg.ops() {
					total++
					expr := tg.expr + op.suffix
					if expr == "" {
						expr = "."
					}
					out, edits, err := spliceRun(t, string(src), di, op.verb, expr, op.arg)
					if errors.Is(err, errDecline) || errors.Is(err, errUnwritable) {
						t.Errorf("%s: %s %s: %v", e.Name(), op.verb, expr, err)
						continue
					}
					if err != nil {
						continue // ymledit refused the edit itself
					}
					level := ""
					for _, ed := range edits {
						if ed.level != "" {
							level = ed.level
						}
					}
					levels[level]++
					if level != "" {
						checkOutsideSpans(t, e.Name(), op.verb+" "+expr, src, out, edits)
						continue
					}
					spliced++
					checkSpliceLocal(t, e.Name(), op.verb+" "+expr, src, out, edits, tg)
				}
			}
		}
	}
	t.Logf("spliced %d of %d corpus edits; re-rendered: %v", spliced, total, levels)
	if total == 0 || spliced*100 < total*80 {
		t.Fatalf("spliced %d of %d corpus edits, below the 80%% floor", spliced, total)
	}
}

type corpusTarget struct {
	expr        string // empty for a document's root
	key         bool   // the entry is a mapping pair, so rename applies
	kind        yaml.Kind
	first, last int // source lines the entry spans
}

// corpusOp is an edit to a target: suffix extends the target's path, for an
// edit that inserts below it.
type corpusOp struct{ verb, suffix, arg string }

func (tg corpusTarget) ops() []corpusOp {
	var ops []corpusOp
	if tg.expr != "" {
		ops = append(ops, corpusOp{"delete", "", ""}, corpusOp{"set", "", "replaced"})
	}
	if tg.key {
		ops = append(ops, corpusOp{"rename", "", "renamed"})
	}
	switch tg.kind {
	case yaml.ScalarNode:
		if tg.expr != "" {
			ops = append(ops, corpusOp{"set", "", "p: 1\nq: [2]"})
		}
	case yaml.MappingNode:
		ops = append(ops, corpusOp{"set", ".spliceNew", "x"}, corpusOp{"set", ".spliceNew.deeper", "- 1\n- 2"})
	case yaml.SequenceNode:
		ops = append(ops, corpusOp{"append", "", "x"}, corpusOp{"append", "", "k: v"})
	}
	return ops
}

func corpusTargets(doc *yaml.Node) []corpusTarget {
	var out []corpusTarget
	var walk func(n *yaml.Node, segs []path.Segment)
	walk = func(n *yaml.Node, segs []path.Segment) {
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				out = append(out, corpusTarget{"", false, c.Kind, c.Line, lastLine(c)})
				walk(c, segs)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" || k.Value == "<<" {
					continue
				}
				s := append(append([]path.Segment(nil), segs...), path.Segment{Key: k.Value})
				out = append(out, corpusTarget{path.Format(s), true, v.Kind, k.Line, lastLine(v)})
				walk(v, s)
			}
		case yaml.SequenceNode:
			for i, v := range n.Content {
				s := append(append([]path.Segment(nil), segs...), path.Segment{Index: i, IsIndex: true})
				out = append(out, corpusTarget{path.Format(s), false, v.Kind, v.Line, lastLine(v)})
				walk(v, s)
			}
		}
	}
	walk(doc, nil)
	return out
}

// checkSpliceLocal holds a splice to its locality promise: a set or a rename
// is one span on the target's own line, and a delete touches nothing below the
// entry's last line beyond what that line's own subtree holds.
func checkSpliceLocal(t *testing.T, fixture, op string, src, out []byte, edits []edit, tg corpusTarget) {
	t.Helper()
	lt := newLineTable(src)
	lineOf := func(off int) int {
		for i := lt.n() - 1; i >= 0; i-- {
			if lt.starts[i] <= off {
				return i + 1
			}
		}
		return 1
	}
	verb := strings.Fields(op)[0]
	switch verb {
	case "rename":
		if len(edits) != 1 {
			t.Errorf("%s: %s made %d edits, want 1", fixture, op, len(edits))
			return
		}
		e := edits[0]
		if lineOf(e.start) != lineOf(e.end) || lineOf(e.start) != tg.first {
			t.Errorf("%s: %s edited lines %d-%d, not the key's line %d", fixture, op, lineOf(e.start), lineOf(e.end), tg.first)
		}
	case "append":
		if len(edits) != 1 || lineOf(edits[0].start) < tg.first {
			t.Errorf("%s: %s made %d edits, want 1 at or below line %d", fixture, op, len(edits), tg.first)
			return
		}
	case "set":
		// A block scalar's replace is two edits, the header and the body, and
		// so is a value that moves between the key's line and the lines below.
		if len(edits) == 0 || len(edits) > 2 {
			t.Errorf("%s: %s made %d edits, want 1 or 2", fixture, op, len(edits))
			return
		}
		if l := lineOf(edits[0].start); l < tg.first && !loneDashAbove(lt, l, tg.first) {
			t.Errorf("%s: %s starts on line %d, above the entry's line %d", fixture, op, l, tg.first)
		}
	case "delete":
		if len(edits) == 0 || len(edits) > 2 {
			t.Errorf("%s: %s made %d edits", fixture, op, len(edits))
			return
		}
		if lineOf(edits[0].start) > tg.first {
			t.Errorf("%s: %s starts on line %d, below the entry's line %d", fixture, op, lineOf(edits[0].start), tg.first)
		}
	}
	for _, e := range edits {
		if e.start < 0 || e.end > len(src) || e.start > e.end {
			t.Errorf("%s: %s edit [%d,%d) out of bounds", fixture, op, e.start, e.end)
		}
	}
	if !strings.HasPrefix(string(out), string(src[:edits[0].start])) {
		t.Errorf("%s: %s changed bytes before its first span", fixture, op)
	}
	if last := edits[len(edits)-1]; !strings.HasSuffix(string(out), string(src[last.end:])) {
		t.Errorf("%s: %s changed bytes after its last span", fixture, op)
	}
}

// loneDashAbove reports whether line is a sequence item's lone dash for an
// item that starts on line first: a replaced item's text starts after it.
func loneDashAbove(lt *lineTable, line, first int) bool {
	b := bytes.TrimSpace(lt.text(line))
	if i := bytes.Index(b, []byte(" #")); i >= 0 {
		b = bytes.TrimSpace(b[:i])
	}
	if string(b) != "-" {
		return false
	}
	for l := line + 1; l < first; l++ {
		if t := lt.text(l); !isBlankText(t) && !isCommentLine(t) {
			return false
		}
	}
	return true
}

// encoderRoundTrips reports whether yaml.v3's own encoding of docs reads back
// as docs: when it does not, the edit made a tree no rendering can hold.
func encoderRoundTrips(docs []*yaml.Node) bool {
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	for _, d := range docs {
		if e.Encode(d) != nil {
			return false
		}
	}
	if e.Close() != nil {
		return false
	}
	bad, ok := verifyDocs(b.Bytes(), docs)
	for _, x := range bad {
		ok = ok && !x
	}
	return ok
}

// checkOutsideSpans holds a re-render to the bytes it was placed over: the
// source before the first span and after the last is in the output as it was.
func checkOutsideSpans(t *testing.T, fixture, op string, src, out []byte, edits []edit) {
	t.Helper()
	if len(edits) == 0 {
		t.Errorf("%s: %s made no edits", fixture, op)
		return
	}
	if !strings.HasPrefix(string(out), string(src[:edits[0].start])) {
		t.Errorf("%s: %s changed bytes before its first span", fixture, op)
	}
	if last := edits[len(edits)-1]; !strings.HasSuffix(string(out), string(src[last.end:])) {
		t.Errorf("%s: %s changed bytes after its last span", fixture, op)
	}
}

// FuzzSplice: whatever splices must decode to the edited tree — which
// spliceEdit checks itself — and must leave every byte outside its spans as
// the source had it.
func FuzzSplice(f *testing.F) {
	seeds := []struct{ src, verb, expr, arg string }{
		// From the targets of the passes the splice replaced: a block body line
		// repeated outside it with trailing white space, lone and compact dashes,
		// and spellings the encoder's stand-in markers used to collide with.
		{"f: |\n  a: 1\nm:\n  a: 1   \nafter: 1\n", "set", ".after", "2"},
		{"a: 1\n   \nafter: 1\n", "set", ".after", "2"},
		{"s:\n  -\n  - b: 2\n", "delete", ".s[1]", ""},
		{"s:\n  - - x\n  - - y\n", "set", ".s[0][0]", "z"},
		{"s:\n  - a: 1\n  - b: 2    # note\n", "append", ".s", "c"},
		{"f:\n  -\n    &a k: 1\n    j: *a\n  -   \nafter: 1\n", "set", ".after", "2"},
		{"f:\n  -\n    a: |\n      t\n  - z\nafter: 1\n", "delete", ".f[1]", ""},
		{"b: |\n  keep\nq: \"zzyaymlq\"\nl: [zzyaymlq]\n", "set", ".b", "zzyaymlq"},
		{"a: 1\nb: 2\n", "set", ".a", "3"},
		{"a: 1 # c\n\n\nb: 2\n", "delete", ".a", ""},
		{"a:\n  - x\n  - y\n", "delete", ".a[0]", ""},
		{"a:\n  b: 1\n", "delete", ".a.b", ""},
		{"a: 'q'\n", "rename", ".a", "b c"},
		{"- a: 1\n  b: 2\n", "delete", "[0].a", ""},
		{"a: \"x\\\"y\" # z\n", "set", ".a", "w"},
		{"a: |+ # c\n  one\n\n\nb: 1\n", "set", ".a", "x"},
		{"a: |+\n  one\n\nb: 1\n", "delete", ".b", ""},
		{"l:\n  - >2-\n     x\n  - y\n", "set", ".l[0]", "z"},
		{"a: one\n  two\n\n  three\nb: 1\n", "set", ".a", "x"},
		{"a: 'one\n  two'\n", "set", ".a", "x"},
		{"l:\n-\n  a: 1\n- # c\n  b: 2\n", "delete", ".l[1]", ""},
		{"a:\n  b: |+\n    x\n\nc: 1\n", "set", ".a.n", "y"},
		{"l:\n- a\nm: [x]\n", "append", ".l", "k: v"},
		{"m: [x, 'y,z']\n", "append", ".m", "{a: 1}"},
		{"a: ~\n", "set", ".a.b.c", "- 1\n- 2"},
		{"a:\n  b: 1\n", "set", ".a", "[1]"},
	}
	for _, s := range seeds {
		f.Add(s.src, s.verb, s.expr, s.arg)
	}
	f.Fuzz(func(t *testing.T, src, verb, expr, arg string) {
		switch verb {
		case "set", "delete", "rename", "append":
		default:
			return
		}
		if len(src) > 4096 {
			return
		}
		data := []byte(src)
		docs, err := decodeNodes(data)
		if err != nil || len(docs) == 0 {
			return
		}
		segs, err := path.Parse(expr)
		if err != nil {
			return
		}
		for _, s := range segs {
			if s.Fans() {
				return
			}
		}
		snap := snapshotTree(docs)
		switch verb {
		case "set":
			v, perr := ymledit.ParseValue(arg, false)
			if perr != nil {
				return
			}
			err = ymledit.Set(docs[0], segs, v, nil)
		case "delete":
			err = ymledit.Delete(docs[0], segs, nil)
		case "rename":
			err = ymledit.Rename(docs[0], segs, arg, nil)
		case "append":
			v, perr := ymledit.ParseValue(arg, false)
			if perr != nil {
				return
			}
			err = ymledit.Append(docs[0], segs, v, nil)
		}
		if err != nil {
			return
		}
		out, edits, err := splice(data, docs, snap, 2)
		if err != nil {
			// Every edit lands somewhere on the re-render ladder; only a
			// source the splice cannot count offsets in is declined.
			if errors.Is(err, errUnwritable) && !encoderRoundTrips(docs) {
				return // no spelling holds the edited tree; refusing it is right
			}
			bom := strings.HasPrefix(src, "\ufeff") || strings.HasPrefix(src, "\xff\xfe") || strings.HasPrefix(src, "\xfe\xff")
			if !errors.Is(err, errDecline) || !bom {
				t.Fatalf("the splice failed: %v", err)
			}
			return
		}
		bad, ok := verifyDocs(out, docs)
		for _, b := range bad {
			ok = ok && !b
		}
		if !ok {
			t.Fatalf("output %q does not decode to the edited tree", out)
		}
		pos, rebuilt := 0, ""
		for _, e := range edits {
			if e.start < pos || e.end > len(data) {
				t.Fatalf("edits overlap or run out of bounds: %+v", edits)
			}
			rebuilt += src[pos:e.start] + e.text
			pos = e.end
		}
		rebuilt += src[pos:]
		if rebuilt != string(out) {
			t.Fatalf("output is not the source with its spans replaced:\n%q\n%q", out, rebuilt)
		}
	})
}
