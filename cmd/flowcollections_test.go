package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestEditKeepsAnUntouchedFlowCollectionByteIdentical is #252's flow row, which
// turned out to be wider than the line breaks it was filed about: yaml.v3
// renders a flow collection from scratch, so the author's line breaks, the
// spacing inside the brackets and a trailing comma all come back as the
// encoder's own one-line spelling.
func TestEditKeepsAnUntouchedFlowCollectionByteIdentical(t *testing.T) {
	cases := []struct{ name, in string }{
		{"canonical spelling", "m: {a: 1, b: 2}\nafter: 1\n"},
		{"padded braces", "m: { a: 1, b: 2 }\nafter: 1\n"},
		{"no space after comma", "s: [1,2]\nafter: 1\n"},
		{"padded brackets", "s: [ 1, 2 ]\nafter: 1\n"},
		{"trailing comma", "s: [1, 2,]\nafter: 1\n"},
		{"broken over lines", "m: {\n  a: 1,\n  b: 2\n}\nafter: 1\n"},
		{"one item per line", "s: [\n  1,\n  2,\n]\nafter: 1\n"},
		{"nested flow", "m: {a: [1,2], b: { c: 3 }}\nafter: 1\n"},
		{"empty mapping", "m: {}\nafter: 1\n"},
		{"empty sequence", "s: []\nafter: 1\n"},
		{"trailing comment", "m: { a: 1 } # note\nafter: 1\n"},
		{"inside a sequence", "s:\n  - [ 1, 2 ]\n  - [ 3 ]\nafter: 1\n"},
		{"nested deeper", "a:\n  b:\n    c: [ 1, 2 ]\nafter: 1\n"},
		{"quoted bracket", "s: [ \"]\", '[' ]\nafter: 1\n"},
		{"comment inside", "s: [\n  1, # one\n  2,\n]\nafter: 1\n"},
		{"anchored collection", "m: &base { a: 1 }\nn: *base\nafter: 1\n"},
		{"tagged collection", "m: !Pair [ 1, 2 ]\nafter: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("an edit to .after rewrote the flow collection\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditKeepsAFlowCollectionAtTheRoot covers the position with no key above
// it. The edit has to land in a second document, since an edit to the only
// document a root collection is would be an edit to the collection itself.
func TestEditKeepsAFlowCollectionAtTheRoot(t *testing.T) {
	in := "{ a: 1 }\n---\nafter: 1\n"
	want := "{ a: 1 }\n---\nafter: 2\n"
	got, err := execute(t, in, "set", "--doc", "1", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditingAFlowCollectionItselfRewritesIt: the source's spelling is kept
// for a collection the edit left alone, and a collection the edit replaced is
// the caller's, emitted for what it now is.
func TestEditingAFlowCollectionItselfRewritesIt(t *testing.T) {
	cases := []struct {
		name, in string
		args     []string
		want     string
	}{
		{"replacing it", "m: { a: 1 }\n", []string{"set", ".m", "{b: 2}"}, "m: {b: 2}\n"},
		{"editing into it", "m: { a: 1 }\n", []string{"set", ".m.a", "9"}, "m: {a: 9}\n"},
		{"appending to it", "s: [ 1 ]\n", []string{"append", ".s", "2"}, "s: [ 1, 2 ]\n"},
		{"deleting from it", "m: { a: 1, b: 2 }\n", []string{"delete", ".m.a"}, "m: {b: 2}\n"},
		{"editing a root collection", "{ after: 1, b: 2 }\n", []string{"set", ".after", "2"}, "{after: 2, b: 2}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestDeletingAFlowCollectionTakesItsSpellingWithIt checks the restore matches
// on the marker the encoder did or didn't emit rather than on position, so a
// collection the edit removed simply never comes back.
func TestDeletingAFlowCollectionTakesItsSpellingWithIt(t *testing.T) {
	got, err := execute(t, "m: {\n  a: 1\n}\nafter: 1\n", "delete", ".m")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "after: 1\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestRenamingAFlowCollectionsKeyKeepsItsSpelling moves the collection's line
// without touching the collection, which is what makes the restore depend on
// the marker rather than on the line it was recorded from.
func TestRenamingAFlowCollectionsKeyKeepsItsSpelling(t *testing.T) {
	got, err := execute(t, "m: { a: 1 }\nafter: 1\n", "rename", ".m", "n")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "n: { a: 1 }\nafter: 1\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditKeepsAFlowCollectionUnderCRLF checks the restore lands before the
// line endings are reapplied, which the lines it splices in depend on.
func TestEditKeepsAFlowCollectionUnderCRLF(t *testing.T) {
	in := "m: {\r\n  a: 1\r\n}\r\nafter: 1\r\n"
	want := "m: {\r\n  a: 1\r\n}\r\nafter: 2\r\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditInPlaceIsIdempotentWithAFlowCollection is the property the rewrite
// costs: the second run of the same edit has a different file to work on.
func TestEditInPlaceIsIdempotentWithAFlowCollection(t *testing.T) {
	in := "m: {\n  a: 1,\n  b: 2\n}\nafter: 1\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for i := range 2 {
		if _, err := execute(t, "", "set", "-i", ".after", "2", path); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: a path this test just created
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	if string(got) != want {
		t.Errorf("two runs of the same edit\n--- want\n%q\n--- got\n%q", want, string(got))
	}
}

// TestEditKeepsAFlowCollectionBesideABlockScalar holds the two restores that
// both splice source text into the encoder's output against each other.
func TestEditKeepsAFlowCollectionBesideABlockScalar(t *testing.T) {
	in := "f: |\n  one\n  two\nm: { a: 1 }\nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestFlowBracketRejectsWhatIsNotACollection covers the scan from a node's
// reported column to the bracket that opens it: only the properties yaml.v3
// counts as part of the node may stand between.
func TestFlowBracketRejectsWhatIsNotACollection(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		col        int
	}{
		{"no bracket at all", "f: plain", 4},
		{"column past the line", "f: {}", 99},
		{"column before the line", "f: {}", 0},
		{"text before the bracket", "f: x {a: 1}", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := flowBracket(tc.line, tc.col); ok {
				t.Errorf("flowBracket(%q, %d) accepted a line that opens no collection", tc.line, tc.col)
			}
		})
	}
	for _, tc := range []struct {
		name, line string
		col, want  int
	}{
		{"bracket at the column", "f: {a: 1}", 4, 4},
		{"past an anchor", "f: &b {a: 1}", 4, 7},
		{"past a tag", "f: !T [1]", 4, 7},
		{"past both", "f: &b !T [1]", 4, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := flowBracket(tc.line, tc.col)
			if !ok || got != tc.want {
				t.Errorf("flowBracket(%q, %d) = %d, %v; want %d, true", tc.line, tc.col, got, ok, tc.want)
			}
		})
	}
}

// TestFlowExtentDeclinesACollectionThatNeverCloses is the one decline no
// document can reach through the CLI — an unclosed collection does not parse,
// so the decoder never hands one over — and the one whose absence would run the
// scan past the end of the source.
func TestFlowExtentDeclinesACollectionThatNeverCloses(t *testing.T) {
	if _, ok := flowExtent([]string{"f: [1,", "  2,"}, 1, 4); ok {
		t.Error("flowExtent accepted a collection with no closing bracket")
	}
}

// TestEditKeepsAFlowCollectionWithABracketTag covers a tag spelled with a
// bracket. yaml.v3 reads a tag up to the next blank, so the "[" in "![" is
// part of the tag; scanned as structure, it made an inner collection's extent
// swallow its parent's closing bracket, and the edit wrote one too many (#282).
func TestEditKeepsAFlowCollectionWithABracketTag(t *testing.T) {
	for _, in := range []string{
		"f: [[![\n]]\nafter: 1\n",
		"f: [[![ ]]\nafter: 1\n",
		"f: [[!a[ b]]\nafter: 1\n",
		"f: {a: [![ ]}\nafter: 1\n",
		"f: [[![ ], 1]\nafter: 1\n",
		"f:\n  - [ [![ ]]\nafter: 1\n",
	} {
		t.Run(in, func(t *testing.T) {
			got, err := execute(t, in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if want := strings.Replace(in, "after: 1", "after: 2", 1); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestFlowExtentSkipsATag(t *testing.T) {
	text, ok := flowExtent([]string{"f: [[![", "]]"}, 1, 5)
	if !ok || len(text) != 2 || text[1] != "]" {
		t.Errorf("flowExtent of the inner collection = %q, %v; want it to end at the first \"]\"", text, ok)
	}
}

// TestOpensQuotedScalarTellsAQuoteFromACharacter pins the rule that cost a
// whole collection its record when it was missing: a quote opens a scalar only
// where a node begins, so the one in the plain scalar 00"0 is just a character.
func TestOpensQuotedScalarTellsAQuoteFromACharacter(t *testing.T) {
	for _, tc := range []struct {
		line string
		at   int
		want bool
	}{
		{`{"a": 1}`, 1, true},
		{`{a: "b"}`, 4, true},
		{`[1, "b"]`, 4, true},
		{`{00"0}`, 3, false},
		{`[a'b]`, 2, false},
		{`"a"`, 0, true},
	} {
		if got := opensQuotedScalar(tc.line, 0, tc.at); got != tc.want {
			t.Errorf("opensQuotedScalar(%q, 0, %d) = %v, want %v", tc.line, tc.at, got, tc.want)
		}
	}
}

// TestEndOfQuotedHandlesEscapesAndUnterminatedScalars covers both quote forms'
// escapes and the line that ends inside one.
func TestEndOfQuotedHandlesEscapesAndUnterminatedScalars(t *testing.T) {
	for _, tc := range []struct {
		line string
		at   int
		want int
		ok   bool
	}{
		{`'a'`, 0, 3, true},
		{`'a''b'`, 0, 6, true},
		{`'a`, 0, 0, false},
		{`"a"`, 0, 3, true},
		{`"a\"b"`, 0, 6, true},
		{`"a`, 0, 0, false},
		{`"a\`, 0, 0, false},
	} {
		got, ok := endOfQuoted(tc.line, tc.at)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("endOfQuoted(%q, %d) = %d, %v; want %d, %v", tc.line, tc.at, got, ok, tc.want, tc.ok)
		}
	}
}

// TestNormalizeFlowNullsScopesItselfToFlow pins the scope the fix rests on: an
// empty null inside a flow collection is given the value "null", and one in
// block context is left exactly as it is.
func TestNormalizeFlowNullsScopesItselfToFlow(t *testing.T) {
	for _, tc := range []struct {
		src, path, want string
	}{
		{"f: {a}\n", "a", "null"},
		{"f: [&x]\n", "0", "null"},
		{"f: {a: 1}\n", "a", "1"},
		{"f:\n  a:\n", "a", ""}, // block: untouched
		{"f:\n  - \n", "0", ""}, // block: untouched
		{"f: {a: null}\n", "a", "null"},
	} {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(tc.src), &n); err != nil {
			t.Fatalf("unmarshal %q: %v", tc.src, err)
		}
		normalizeFlowNulls(&n, false)
		got := flowTestValue(t, &n, tc.path)
		if got != tc.want {
			t.Errorf("normalizeFlowNulls(%q) left %q = %q, want %q", tc.src, tc.path, got, tc.want)
		}
	}
}

// flowTestValue reads the Value of .f's child named by key (a mapping key, or a
// sequence index) out of a decoded document.
func flowTestValue(t *testing.T, doc *yaml.Node, key string) string {
	t.Helper()
	f := doc.Content[0].Content[1]
	if f.Kind == yaml.SequenceNode {
		return f.Content[0].Value
	}
	for i := 0; i+1 < len(f.Content); i += 2 {
		if f.Content[i].Value == key {
			return f.Content[i+1].Value
		}
	}
	t.Fatalf("no key %q under .f", key)
	return ""
}

// An empty null used as a mapping key has no plain spelling in block context:
// the encoder has to write something left of the colon, and what it wrote was
// an empty quoted string — a different key (#273). The value beside it, and an empty null value
// anywhere in block context, stay exactly as they are.
func TestEditKeepsAnEmptyNullKeyNull(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"explicit key", "a:\n  ? \n  : one\nafter: 1\n", "a:\n  ? \n  : one\nafter: 2\n"},
		{"explicit key, null value", "f:\n ?\n\n\nafter: 1\n", "f:\n ?\n\n\nafter: 2\n"},
		{"anchored key", "a:\n  &k : one\nafter: 1\n", "a:\n  &k : one\nafter: 2\n"},
		{"in a sequence item", "s:\n  - ? \n    : one\nafter: 1\n", "s:\n  - ? \n    : one\nafter: 2\n"},
		{"null value stays empty", "a:\n  k:\nafter: 1\n", "a:\n  k:\nafter: 2\n"},
		{"spelled null key untouched", "a:\n  ~: one\nafter: 1\n", "a:\n  ~: one\nafter: 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.src, "set", ".after", "2")
			if err != nil {
				t.Fatalf("set: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}
