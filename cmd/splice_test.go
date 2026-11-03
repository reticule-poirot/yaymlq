package cmd

import (
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
	data := []byte(src)
	docs, err := decodeNodes(data, sourceLines(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	snap := snapshotTree(docs)
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
		err = ymledit.Set(docs[doc], segs, v, nil)
	case "delete":
		err = ymledit.Delete(docs[doc], segs, nil)
	case "rename":
		err = ymledit.Rename(docs[doc], segs, arg, nil)
	case "append":
		v, _ := ymledit.ParseValue(arg, false)
		err = ymledit.Append(docs[doc], segs, v, nil)
	default:
		t.Fatalf("unknown verb %q", verb)
	}
	if err != nil {
		return nil, nil, err
	}
	return splice(data, docs, snap)
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

func TestSpliceDeclines(t *testing.T) {
	cases := []struct {
		name              string
		src               string
		verb, expr, value string
	}{
		{"insertion", "a: 1\n", "set", ".b", "2"},
		{"append", "l:\n- a\n", "append", ".l", "b"},
		{"block scalar value", "a: |\n  x\n", "set", ".a", "y"},
		{"multi-line plain value", "a: one\n  two\n", "set", ".a", "y"},
		{"flow collection", "a: {b: 1}\n", "set", ".a.b", "2"},
		{"delete in flow", "a: [1, 2]\n", "delete", ".a[0]", ""},
		{"value brings its own comment", "a: 1\n", "set", ".a", "2 # c"},
		{"scalar to collection", "a: 1\n", "set", ".a", "{b: 1}"},
		{"anchored key", "&k a: 1\n", "rename", ".a", "b"},
		{"explicit key", "? a\n: 1\nb: 2\n", "delete", ".a", ""},
		{"BOM", "\ufeffa: 1\n", "set", ".a", "2"},
		{"last entry of the root", "a: 1\n", "delete", ".a", ""},
		{"empty node past the last line", "  -\n  - ?", "delete", "[1]", ""},
		{"multi-line rendering", "a: 1\n", "set", ".a", "|\n  x\n  y\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := spliceRun(t, tc.src, 0, tc.verb, tc.expr, tc.value)
			if !errors.Is(err, errDecline) {
				t.Fatalf("got %q, %v; want a decline", out, err)
			}
		})
	}
}

// TestSpliceDeclinesAnExplicitIndent: --indent asks for the document to be
// re-rendered, which only the encoder does.
func TestSpliceDeclinesAnExplicitIndent(t *testing.T) {
	got, err := execute(t, "a:\n  b: 1\n\n\nc: 2\n", "set", "--indent", "4", ".c", "3")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a:\n    b: 1\n\nc: 3\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
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
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: a fixture in this repo
		if err != nil {
			t.Fatal(err)
		}
		data := src
		docs, err := decodeNodes(data, sourceLines(data))
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		for di, d := range docs {
			for _, tg := range corpusTargets(d) {
				for _, op := range tg.ops() {
					total++
					out, edits, err := spliceRun(t, string(src), di, op.verb, tg.expr, op.arg)
					if errors.Is(err, errDecline) {
						continue
					}
					if err != nil {
						continue // ymledit refused the edit itself
					}
					spliced++
					checkSpliceLocal(t, e.Name(), op.verb+" "+tg.expr, src, out, edits, tg)
				}
			}
		}
	}
	t.Logf("spliced %d of %d corpus edits", spliced, total)
	if total == 0 || spliced*100 < total*70 {
		t.Fatalf("spliced %d of %d corpus edits, below the 70%% floor", spliced, total)
	}
}

type corpusTarget struct {
	expr        string
	key         bool // the entry is a mapping pair, so rename applies
	scalar      bool
	first, last int // source lines the entry spans
}

type corpusOp struct{ verb, arg string }

func (tg corpusTarget) ops() []corpusOp {
	ops := []corpusOp{{"delete", ""}}
	if tg.key {
		ops = append(ops, corpusOp{"rename", "renamed"})
	}
	if tg.scalar {
		ops = append(ops, corpusOp{"set", "replaced"})
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
				walk(c, segs)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" || k.Value == "<<" {
					continue
				}
				s := append(append([]path.Segment(nil), segs...), path.Segment{Key: k.Value})
				out = append(out, corpusTarget{path.Format(s), true, v.Kind == yaml.ScalarNode, k.Line, lastLine(v)})
				walk(v, s)
			}
		case yaml.SequenceNode:
			for i, v := range n.Content {
				s := append(append([]path.Segment(nil), segs...), path.Segment{Index: i, IsIndex: true})
				out = append(out, corpusTarget{path.Format(s), false, v.Kind == yaml.ScalarNode, v.Line, lastLine(v)})
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
	case "set", "rename":
		if len(edits) != 1 {
			t.Errorf("%s: %s made %d edits, want 1", fixture, op, len(edits))
			return
		}
		e := edits[0]
		if lineOf(e.start) != lineOf(e.end) {
			t.Errorf("%s: %s spans lines %d-%d", fixture, op, lineOf(e.start), lineOf(e.end))
		}
		if verb == "rename" && lineOf(e.start) != tg.first {
			t.Errorf("%s: %s edited line %d, not the key's line %d", fixture, op, lineOf(e.start), tg.first)
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

// FuzzSplice: whatever splices must decode to the edited tree — which
// spliceEdit checks itself — and must leave every byte outside its spans as
// the source had it.
func FuzzSplice(f *testing.F) {
	seeds := []struct{ src, verb, expr, arg string }{
		{"a: 1\nb: 2\n", "set", ".a", "3"},
		{"a: 1 # c\n\n\nb: 2\n", "delete", ".a", ""},
		{"a:\n  - x\n  - y\n", "delete", ".a[0]", ""},
		{"a:\n  b: 1\n", "delete", ".a.b", ""},
		{"a: 'q'\n", "rename", ".a", "b c"},
		{"- a: 1\n  b: 2\n", "delete", "[0].a", ""},
		{"a: \"x\\\"y\" # z\n", "set", ".a", "w"},
	}
	for _, s := range seeds {
		f.Add(s.src, s.verb, s.expr, s.arg)
	}
	f.Fuzz(func(t *testing.T, src, verb, expr, arg string) {
		switch verb {
		case "set", "delete", "rename":
		default:
			return
		}
		if len(src) > 4096 {
			return
		}
		data := []byte(src)
		docs, err := decodeNodes(data, sourceLines(data))
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
		}
		if err != nil {
			return
		}
		out, edits, err := splice(data, docs, snap)
		if err != nil {
			if !errors.Is(err, errDecline) {
				t.Fatalf("non-decline error: %v", err)
			}
			return
		}
		if !sameAsTree(out, docs) {
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
