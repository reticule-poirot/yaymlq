package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// TestPlainMergeOutsideKeyPositionReadsAsText: a plain << is a merge only as a
// mapping key. yaml.v4 tags it !!merge wherever it stands and its decoder then
// drops the entry, so the value would silently vanish from a read.
func TestPlainMergeOutsideKeyPositionReadsAsText(t *testing.T) {
	cases := []struct {
		name, src string
		want      any
	}{
		{"mapping value", "k: <<\nj: 1\n", map[string]any{"k": "<<", "j": 1.0}},
		{"sequence item", "- <<\n- x\n", []any{"<<", "x"}},
		{"flow sequence", "[<<]\n", []any{"<<"}},
		{"flow mapping value", "{a: <<}\n", map[string]any{"a": "<<"}},
		{"nested", "a:\n  - b: <<\n", map[string]any{"a": []any{map[string]any{"b": "<<"}}}},
		{"root", "<<\n", "<<"},
		{"anchored value through its alias", "a: &x <<\nb: *x\n", map[string]any{"a": "<<", "b": "<<"}},
		{"alias to an anchored merge key", "&x <<: {a: 1}\nb: *x\n", map[string]any{"a": 1.0, "b": "<<"}},
		{"explicitly tagged", "k: !!merge <<\n", map[string]any{"k": "<<"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execute(t, tc.src, "-o", "json", ".")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			var got any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output %q: %v", out, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestMergeKeyStillMerges is the other half of the retag above: it must leave
// the << that means something alone, at any depth.
func TestMergeKeyStillMerges(t *testing.T) {
	src := "base: &b {a: 1}\nout:\n  <<: *b\n  c: 2\n"
	got, err := execute(t, src, "-o", "raw", ".out.a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "1\n" {
		t.Errorf("got %q, want the merged value", got)
	}
}

func TestPlainMergeInDefaultReadsAsText(t *testing.T) {
	got, err := execute(t, "a: 1\n", "-o", "raw", "--default", "<<", ".missing")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "<<\n" {
		t.Errorf("got %q, want %q", got, "<<\n")
	}
}

// TestEditKeepsPlainMergeValue: the retag is for a read's decoded value. An
// edit's tree goes back to the file, where the source's plain << must stay.
func TestEditKeepsPlainMergeValue(t *testing.T) {
	got, err := execute(t, "k: <<\nj: 1\n", "set", ".j", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "k: <<\nj: 2\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRootBlockScalarSwallowingAMarkerIsRefused: yaml.v4 reads a root block
// scalar whose indentation it auto-detects as zero to the end of the stream,
// marker lines included, so every later document becomes text in this one.
func TestRootBlockScalarSwallowingAMarkerIsRefused(t *testing.T) {
	cases := []struct{ name, src string }{
		{"literal", "--- |\n---\nb: 1\n"},
		{"folded", "--- >\n---\nb: 1\n"},
		{"strip", "--- |-\n---\nb: 1\n"},
		{"keep", "--- |+\n---\nb: 1\n"},
		{"document end marker", "--- |\n...\nb: 1\n"},
		{"marker with content", "--- |\n--- x\n"},
		{"marker at end of input", "--- |\n---"},
		{"blank line first", "--- |\n\n---\nb: 1\n"},
		{"header comment", "--- | # c\n---\nb: 1\n"},
		{"anchored", "--- &a |\n---\nb: 1\n"},
		{"tagged", "--- !!str |\n---\nb: 1\n"},
		{"header below the tag", "--- !!str\n|\n---\nb: 1\n"},
		{"header below the marker", "---\n|\n---\nb: 1\n"},
		{"no marker before it", "|\n---\nb: 1\n"},
		{"mid-stream", "a: 1\n--- |\n---\nb: 1\n"},
		{"crlf", "--- |\r\n---\r\nb: 1\r\n"},
		{"unindented body", "--- |\nbody\n---\nb: 1\n"},
		{"comment-like body", "--- |\n# c\n---\nb: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, args := range [][]string{
				{"--all-docs", "."},
				{"set", "--all-docs", ".z", "1"},
			} {
				_, err := execute(t, tc.src, args...)
				if err == nil {
					t.Fatalf("%v: want a refusal, got nil", args)
				}
				if codeFor(err) != 2 {
					t.Errorf("%v: codeFor = %d, want 2 (parse); err = %v", args, codeFor(err), err)
				}
				if !strings.Contains(err.Error(), "block scalar") {
					t.Errorf("%v: error should name the construct, got %q", args, err)
				}
			}
			out, err := execute(t, tc.src, "validate")
			if err == nil || !strings.Contains(out, "block scalar") {
				t.Errorf("validate: err = %v, output %q; want a failure naming the construct", err, out)
			}
		})
	}
}

// TestRootBlockScalarThatSwallowsNothingIsRead holds the refusal above to what
// is actually lost: each of these has a marker nearby, or a marker-like line,
// and yaml.v4 reads each one correctly.
func TestRootBlockScalarThatSwallowsNothingIsRead(t *testing.T) {
	cases := []struct {
		name, src string
		want      []any
	}{
		{"empty, alone", "--- |\n", []any{""}},
		{"indented body", "--- |\n  body\n---\nb: 1\n", []any{"body\n", map[string]any{"b": 1.0}}},
		{"indented body spelling a marker", "--- |\n  ---\n---\nb: 1\n", []any{"---\n", map[string]any{"b": 1.0}}},
		{"explicit indent", "--- |2\n---\nb: 1\n", []any{"", map[string]any{"b": 1.0}}},
		{"unindented body, no marker", "--- |\nbody\n", []any{"body\n"}},
		{"line only starting like a marker", "--- |\n---x\n", []any{"---x\n"}},
		{"under a key", "k: |\n---\nb: 1\n", []any{map[string]any{"k": ""}, map[string]any{"b": 1.0}}},
		{"under a list item", "- |\n---\nb: 1\n", []any{[]any{""}, map[string]any{"b": 1.0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execute(t, tc.src, "--all-docs", "-o", "json", ".")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			dec := json.NewDecoder(strings.NewReader(out))
			var got []any
			for dec.More() {
				var v any
				if err := dec.Decode(&v); err != nil {
					t.Fatalf("output %q: %v", out, err)
				}
				got = append(got, v)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestOutputDoesNotWrapLongScalars: the encoder's default line width would fold
// a long value across lines, in a read's output and in a re-rendered edit.
func TestOutputDoesNotWrapLongScalars(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 40))
	t.Run("read", func(t *testing.T) {
		got, err := execute(t, "a: "+long+"\n", "-o", "yaml", ".")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "a: " + long + "\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("flow rendering", func(t *testing.T) {
		got, err := execute(t, "a: {b: "+long+", c: 1}\n", "delete", ".a.c")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "a: {b: " + long + "}\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("forced re-render", func(t *testing.T) {
		src := "x:\n  # keep\n  a: " + long + "\n  b: 1\n"
		forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.MappingNode && n.Line == 3 }
		defer func() { forceRerender = nil }()
		got, err := execute(t, src, "set", ".x.b", "2")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if !strings.Contains(got, "a: "+long+"\n") {
			t.Errorf("long scalar wrapped:\n%s", got)
		}
	})
}

// TestBlockBodyClaimsATrailingLineDeeperThanItsContent: a spaces-only line past
// the content indentation is a text line, so it is value under every chomping.
// Left out of the extent it stays behind when the block is rewritten, and the
// document fails verification and re-renders whole.
func TestBlockBodyClaimsATrailingLineDeeperThanItsContent(t *testing.T) {
	src := "l:\n- |\n  x\n   \nz: 1\n"
	forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.SequenceNode }
	defer func() { forceRerender = nil }()
	got, err := execute(t, src, "set", ".z", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "l:\n- |\n  x\n   \nz: 2\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestJSONErrorLineFromEveryParserStage: yaml.v4 writes a position two ways, the
// constructor's "at L2.C5" as well as the parser's "line 2", and -o json's line
// field has to read both.
func TestJSONErrorLineFromEveryParserStage(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
	}{
		{"parser", "a: 1\nb: [\n  c\n", 4},
		{"constructor", "a: 1\n<<: 1\n", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			je := decodeJSONErr(t, tc.src, "-o", "json", ".a")
			if je.Kind != "parse" {
				t.Fatalf("kind = %q, want parse", je.Kind)
			}
			if je.Line != tc.line {
				t.Errorf("line = %d, want %d; error %q", je.Line, tc.line, je.Error)
			}
		})
	}
}

// TestFlowLineLeavesItsNodeTagged: yaml.v4's encoder strips tags from the node
// it is handed (upstream #471). The tree it was given is the one verification
// compares against, so a rendering must not reach it.
func TestFlowLineLeavesItsNodeTagged(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("[x, 1, {a: b}]\n"), &doc); err != nil {
		t.Fatal(err)
	}
	n := doc.Content[0]
	if _, err := flowLine(n); err != nil {
		t.Fatalf("flowLine: %v", err)
	}
	var walk func(*yaml.Node)
	walk = func(c *yaml.Node) {
		if c.Tag == "" {
			t.Errorf("node %q lost its tag", c.Value)
		}
		for _, k := range c.Content {
			walk(k)
		}
	}
	walk(n)
}

// TestParseRefusalsOfTheNewParser pins what the parser now refuses, all of it
// documented as a breaking change in #387: exit 2, and nothing written.
func TestParseRefusalsOfTheNewParser(t *testing.T) {
	cases := []struct{ name, src string }{
		{"alias to an anchor in an earlier document", "a: &x 1\n---\nb: *x\n"},
		{"flow indicator in a tag", "[![ ]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, tc.src, "--all-docs", ".")
			if err == nil {
				t.Fatal("want a refusal, got nil")
			}
			if codeFor(err) != 2 {
				t.Errorf("codeFor = %d, want 2 (parse); err = %v", codeFor(err), err)
			}
		})
	}
}

// TestPlainNegativeZeroIsAFloat pins the other documented change: plain -0
// keeps its sign instead of decoding as the integer 0.
func TestPlainNegativeZeroIsAFloat(t *testing.T) {
	got, err := execute(t, "a: -0\n", "-o", "raw", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "-0\n" {
		t.Errorf("got %q, want %q", got, "-0\n")
	}
}
