package ymledit_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"gopkg.in/yaml.v3"
)

func apply(t *testing.T, src, expr, value string, asString bool) string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	vn, err := ymledit.ParseValue(value, asString)
	if err != nil {
		t.Fatalf("parse value: %v", err)
	}
	if err := ymledit.Set(&doc, segs, vn, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	return buf.String()
}

func TestSetReplaceScalar(t *testing.T) {
	got := apply(t, "a:\n  b: 1\n", ".a.b", "2", false)
	if strings.TrimSpace(got) != "a:\n  b: 2" {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetPreservesComments(t *testing.T) {
	src := "svc:\n  # which image\n  image: old:1 # pinned\n  port: 80\n"
	got := apply(t, src, ".svc.image", "new:2", false)
	if !strings.Contains(got, "# which image") || !strings.Contains(got, "# pinned") {
		t.Fatalf("comments lost:\n%s", got)
	}
	if !strings.Contains(got, "image: new:2") {
		t.Fatalf("value not updated:\n%s", got)
	}
}

func TestSetCreatesIntermediateKeys(t *testing.T) {
	got := apply(t, "a:\n  b: 1\n", ".a.c.d", "x", false)
	if !strings.Contains(got, "c:\n    d: x") {
		t.Fatalf("nested key not created:\n%s", got)
	}
	if !strings.Contains(got, "b: 1") {
		t.Fatalf("existing key dropped:\n%s", got)
	}
}

func TestSetTypedVsString(t *testing.T) {
	if got := apply(t, "x: 0\n", ".x", "8080", false); strings.TrimSpace(got) != "x: 8080" {
		t.Fatalf("want int, got: %s", got)
	}
	if got := apply(t, "x: 0\n", ".x", "8080", true); strings.TrimSpace(got) != `x: "8080"` {
		t.Fatalf("want quoted string, got: %s", got)
	}
}

func TestSetListIndex(t *testing.T) {
	got := apply(t, "items:\n  - one\n  - two\n", ".items[1]", "TWO", false)
	if !strings.Contains(got, "- TWO") || !strings.Contains(got, "- one") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetErrors(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("a: {b: 1}\nlist: [1,2]\n"), &doc)

	cases := map[string]string{
		"wildcard":        ".a.*",
		"index into map":  ".a[0]",
		"index out range": ".list[9]",
		"whole doc":       "",
	}
	for name, expr := range cases {
		segs, _ := path.Parse(expr)
		vn, _ := ymledit.ParseValue("x", false)
		if err := ymledit.Set(&doc, segs, vn, nil); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		in       string
		asString bool
		wantTag  string
		wantVal  string
	}{
		{"8080", false, "!!int", "8080"},
		{"8080", true, "!!str", "8080"},
		{"true", false, "!!bool", "true"},
		{"nginx:1.27", false, "!!str", "nginx:1.27"},
		{"", false, "!!null", "null"},
	}
	for _, tc := range tests {
		n, err := ymledit.ParseValue(tc.in, tc.asString)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", tc.in, err)
		}
		if n.Tag != tc.wantTag || n.Value != tc.wantVal {
			t.Errorf("ParseValue(%q, %v) = tag %s val %q, want tag %s val %q",
				tc.in, tc.asString, n.Tag, n.Value, tc.wantTag, tc.wantVal)
		}
	}
}

func TestSetStructuredValue(t *testing.T) {
	got := apply(t, "a:\n  b: 1\nc: keep\n", ".a", "{x: 1, y: [2, 3]}", false)
	if !strings.Contains(got, "x: 1") || !strings.Contains(got, "y: [2, 3]") {
		t.Fatalf("collection value not applied:\n%s", got)
	}
	if !strings.Contains(got, "c: keep") {
		t.Fatalf("sibling key lost:\n%s", got)
	}
	if strings.Contains(got, "b: 1") {
		t.Fatalf("old subtree not replaced:\n%s", got)
	}
}

func TestParseValueRejectsLeadingHash(t *testing.T) {
	// In YAML a '#' at the start of the content always opens a comment, no
	// matter what follows — parsing it would silently discard the value as
	// null instead of erroring, so it must be rejected up front.
	for _, in := range []string{"#ffffff", "  #ffffff", "# just a comment"} {
		if _, err := ymledit.ParseValue(in, false); err == nil {
			t.Fatalf("ParseValue(%q): want an error, got nil", in)
		}
	}
}

func TestParseValueLeadingHashOnlyRejectedUnquotedAndUnescaped(t *testing.T) {
	// asString (-s/--string) and quoting the value both say the '#' is meant
	// literally, and must still work.
	n, err := ymledit.ParseValue("#ffffff", true)
	if err != nil {
		t.Fatalf("ParseValue(asString=true): %v", err)
	}
	if n.Tag != "!!str" || n.Value != "#ffffff" {
		t.Fatalf("got tag %s val %q", n.Tag, n.Value)
	}

	n, err = ymledit.ParseValue(`"#ffffff"`, false)
	if err != nil {
		t.Fatalf(`ParseValue(%q): %v`, `"#ffffff"`, err)
	}
	if n.Tag != "!!str" || n.Value != "#ffffff" {
		t.Fatalf("got tag %s val %q", n.Tag, n.Value)
	}
}

func TestParseValueEmptyStringIsStillNull(t *testing.T) {
	// The leading-'#' rejection must not catch the pre-existing "" -> null
	// behavior — an empty (or whitespace-only) value has no '#' to reject.
	for _, in := range []string{"", "   "} {
		n, err := ymledit.ParseValue(in, false)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", in, err)
		}
		if n.Tag != "!!null" {
			t.Fatalf("ParseValue(%q) tag = %s, want !!null", in, n.Tag)
		}
	}
}

func TestParseValueKinds(t *testing.T) {
	tests := []struct {
		in       string
		asString bool
		want     yaml.Kind
	}{
		{"8080", false, yaml.ScalarNode},
		{"{a: 1}", false, yaml.MappingNode},
		{"[1, 2]", false, yaml.SequenceNode},
		{"k:\n  v: 1", false, yaml.MappingNode},
		{"{a: 1}", true, yaml.ScalarNode}, // -s wins
		{"", false, yaml.ScalarNode},      // -> null
	}
	for _, tc := range tests {
		n, err := ymledit.ParseValue(tc.in, tc.asString)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", tc.in, err)
		}
		if n.Kind != tc.want {
			t.Errorf("ParseValue(%q, %v) kind = %v, want %v", tc.in, tc.asString, n.Kind, tc.want)
		}
	}
}

func TestSetErrorMessagesNameTheKind(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("scalar: hi\nlist: [1, 2]\nmap: {x: 1}\n"), &doc)

	cases := []struct {
		expr string
		want string
	}{
		{".scalar.child", "expected a mapping, got scalar"},
		{".map[0]", "expected a list, got mapping"},
		{".list.key", "expected a mapping, got list"},
		{".list[9]", "index 9 out of range"},
	}
	for _, tc := range cases {
		segs, _ := path.Parse(tc.expr)
		vn, _ := ymledit.ParseValue("x", false)
		err := ymledit.Set(&doc, segs, vn, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Set(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
		}
	}
}

func TestSetDuplicateKeyTargetsLastOccurrence(t *testing.T) {
	// query.Run (and so `get`) takes the last of a duplicate key; Set must
	// target the same one, or the edit is invisible to every reader.
	got := apply(t, "a: 1\na: 2\n", ".a", "9", false)
	if !strings.Contains(got, "a: 1") || !strings.Contains(got, "a: 9") {
		t.Fatalf("want the *second* a: rewritten, got:\n%s", got)
	}
}

func TestSetRefusesToOverwriteAliasedAnchor(t *testing.T) {
	src := "defaults: &d\n  retries: 3\nstaging: *d\n"
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte(src), &doc)
	segs, _ := path.Parse(".defaults")
	vn, _ := ymledit.ParseValue("{retries: 5}", false)
	err := ymledit.Set(&doc, segs, vn, nil)
	if !errors.Is(err, ymledit.ErrAnchored) {
		t.Fatalf("want ErrAnchored, got %v", err)
	}
	// The document must be untouched — a refused edit is not a partial one.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(&doc)
	_ = enc.Close()
	if !strings.Contains(buf.String(), "&d") || !strings.Contains(buf.String(), "*d") {
		t.Fatalf("anchor/alias lost despite the refused edit:\n%s", buf.String())
	}
}

func TestSetRefusesAutoVivifyOverAliasedAnchor(t *testing.T) {
	src := "a: &anch\nb: *anch\n"
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte(src), &doc)
	segs, _ := path.Parse(".a.x")
	vn, _ := ymledit.ParseValue("1", false)
	err := ymledit.Set(&doc, segs, vn, nil)
	if !errors.Is(err, ymledit.ErrAnchored) {
		t.Fatalf("want ErrAnchored, got %v", err)
	}
}

func TestSetAllowsOverwritingAnUnaliasedAnchor(t *testing.T) {
	// An anchor with no alias referencing it can be safely overwritten —
	// only actual aliasing should block the edit.
	got := apply(t, "a: &x 1\nb: 2\n", ".a", "9", false)
	if !strings.Contains(got, "a: 9") {
		t.Fatalf("got:\n%s", got)
	}
}

// TestSetLongPathStaysLinear guards #69: Set used to format the path prefix
// (path.Format(segs[:i+1])) eagerly at the top of every loop iteration, even
// though it's only used in error messages — making Set O(segments²). Set is
// the worst case among the four editors since it auto-creates missing
// mapping keys, so it walks the full segment count regardless of document
// size (Delete/Append/Rename need a document that deep, which yaml.v3 itself
// caps at a 10000 max depth).
//
// The path ends in an index segment against what auto-vivification made a
// mapping, so this walks (and auto-vivifies) every one of the N key segments
// before erroring on the last one — exercising the full path length without
// ever reaching the encoder (a separate, expected cost for genuinely
// rendering an N-deep document, not part of what this test is guarding).
func TestSetLongPathStaysLinear(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("k: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	segs, err := path.Parse(strings.Repeat(".a", 20000) + "[0]")
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	vn, _ := ymledit.ParseValue("1", false)

	start := time.Now()
	err = ymledit.Set(&doc, segs, vn, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want an error: the final segment is an index into an auto-vivified mapping")
	}
	// Generous bound: the fixed cost is a few ms; a regression to O(N²)
	// would blow well past this even at this modest N.
	if elapsed > time.Second {
		t.Fatalf("Set with a 20000-segment path took %v, want well under 1s — looks like the quadratic bug is back", elapsed)
	}
}

func TestSetWildcardIsUnsupported(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("a: {b: 1}\n"), &doc)
	segs, _ := path.Parse(".a.*")
	vn, _ := ymledit.ParseValue("x", false)
	err := ymledit.Set(&doc, segs, vn, nil)
	if !errors.Is(err, ymledit.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func remove(t *testing.T, src, expr string) string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	if err := ymledit.Delete(&doc, segs, nil); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	return buf.String()
}

func TestDeleteMapKey(t *testing.T) {
	got := remove(t, "a:\n  b: 1\n  c: 2\n", ".a.b")
	if strings.Contains(got, "b: 1") {
		t.Fatalf("key not removed:\n%s", got)
	}
	if !strings.Contains(got, "c: 2") {
		t.Fatalf("sibling key dropped:\n%s", got)
	}
}

func TestDeleteListIndex(t *testing.T) {
	got := remove(t, "items:\n  - one\n  - two\n  - three\n", ".items[1]")
	if strings.Contains(got, "two") {
		t.Fatalf("element not removed:\n%s", got)
	}
	if !strings.Contains(got, "- one") || !strings.Contains(got, "- three") {
		t.Fatalf("wrong elements removed:\n%s", got)
	}
}

func TestDeleteNegativeIndex(t *testing.T) {
	got := remove(t, "items:\n  - one\n  - two\n", ".items[-1]")
	if strings.Contains(got, "two") || !strings.Contains(got, "- one") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDeleteKeepsSiblingComments(t *testing.T) {
	src := "svc:\n  # keep this\n  image: old:1 # inline\n  port: 80 # remove with me\n"
	got := remove(t, src, ".svc.port")
	if !strings.Contains(got, "# keep this") || !strings.Contains(got, "# inline") {
		t.Fatalf("sibling comments lost:\n%s", got)
	}
	if strings.Contains(got, "remove with me") || strings.Contains(got, "port:") {
		t.Fatalf("deleted node or its comment survived:\n%s", got)
	}
}

func TestDeleteErrors(t *testing.T) {
	src := "a: {b: 1}\nlist: [1, 2]\nscalar: hi\n"
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"missing key", ".a.nope", "no such key"},
		{"index out of range", ".list[9]", "index 9 out of range"},
		{"index into map", ".a[0]", "expected a list, got mapping"},
		{"key into list", ".list.x", "expected a mapping, got list"},
		{"descend into scalar", ".scalar.child", "expected a mapping, got scalar"},
		{"whole document", "", "whole document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc yaml.Node
			_ = yaml.Unmarshal([]byte(src), &doc)
			segs, _ := path.Parse(tc.expr)
			err := ymledit.Delete(&doc, segs, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Delete(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
			}
		})
	}
}

func TestDeleteDuplicateKeyRemovesLastOccurrence(t *testing.T) {
	got := remove(t, "a: 1\na: 2\nb: 3\n", ".a")
	if !strings.Contains(got, "a: 1") {
		t.Fatalf("want the shadowed a: 1 left standing, got:\n%s", got)
	}
	if strings.Contains(got, "a: 2") {
		t.Fatalf("want the effective a: 2 removed, got:\n%s", got)
	}
}

func TestDeleteRefusesToRemoveAliasedAnchor(t *testing.T) {
	src := "defaults: &d\n  retries: 3\nstaging: *d\n"
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte(src), &doc)
	segs, _ := path.Parse(".defaults")
	err := ymledit.Delete(&doc, segs, nil)
	if !errors.Is(err, ymledit.ErrAnchored) {
		t.Fatalf("want ErrAnchored, got %v", err)
	}
}

func TestDeleteAllowsRemovingAnUnaliasedAnchor(t *testing.T) {
	got := remove(t, "a: &x 1\nb: 2\n", ".a")
	if strings.Contains(got, "a:") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDeleteWildcardIsUnsupported(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("a: {b: 1}\n"), &doc)
	segs, _ := path.Parse(".a.*")
	if err := ymledit.Delete(&doc, segs, nil); !errors.Is(err, ymledit.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func appendTo(t *testing.T, src, expr, value string) string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	vn, err := ymledit.ParseValue(value, false)
	if err != nil {
		t.Fatalf("parse value: %v", err)
	}
	if err := ymledit.Append(&doc, segs, vn, nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	return buf.String()
}

func TestAppendScalar(t *testing.T) {
	got := appendTo(t, "tags:\n  - a\n  - b\nname: x\n", ".tags", "c")
	if !strings.Contains(got, "- c") || !strings.Contains(got, "name: x") {
		t.Fatalf("got:\n%s", got)
	}
	if strings.Index(got, "- c") < strings.Index(got, "- b") {
		t.Fatalf("appended element is not last:\n%s", got)
	}
}

func TestAppendCollection(t *testing.T) {
	got := appendTo(t, "items:\n  - {id: 1}\n", ".items", "{id: 2, on: true}")
	if !strings.Contains(got, "id: 2") || !strings.Contains(got, "on: true") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAppendNestedViaIndex(t *testing.T) {
	got := appendTo(t, "m:\n  - [1, 2]\n  - [3, 4]\n", ".m[0]", "9")
	if !strings.Contains(got, "[1, 2, 9]") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAppendKeepsComments(t *testing.T) {
	got := appendTo(t, "tags:\n  - a # first\n  - b # second\n", ".tags", "c")
	if !strings.Contains(got, "# first") || !strings.Contains(got, "# second") {
		t.Fatalf("existing comments lost:\n%s", got)
	}
}

func TestAppendErrors(t *testing.T) {
	src := "list: [1, 2]\nmap: {a: 1}\nscalar: hi\n"
	cases := []struct {
		name, expr, want string
	}{
		{"into mapping", ".map", "expected a list to append to, got mapping"},
		{"into scalar", ".scalar", "expected a list to append to, got scalar"},
		{"missing key", ".nope", "no such key"},
		{"index out of range", ".list[9]", "index 9 out of range"},
		{"wildcard", ".list.*", "wildcards cannot be used with append"},
		{"whole document", "", "whole document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc yaml.Node
			_ = yaml.Unmarshal([]byte(src), &doc)
			segs, _ := path.Parse(tc.expr)
			err := ymledit.Append(&doc, segs, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Append(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
			}
		})
	}
}

func rename(t *testing.T, src, expr, newKey string) string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	if err := ymledit.Rename(&doc, segs, newKey, nil); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	return buf.String()
}

func TestRenameMapKey(t *testing.T) {
	got := rename(t, "a:\n  b: 1\n  c: 2\n", ".a.b", "renamed")
	if strings.Contains(got, "b:") {
		t.Fatalf("old key still present:\n%s", got)
	}
	if !strings.Contains(got, "renamed: 1") || !strings.Contains(got, "c: 2") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenameKeepsPositionAndComments(t *testing.T) {
	src := "a:\n  # keep me\n  b: 1 # inline\n  c: 2\n"
	got := rename(t, src, ".a.b", "renamed")
	if !strings.Contains(got, "# keep me") || !strings.Contains(got, "# inline") {
		t.Fatalf("comments lost:\n%s", got)
	}
	// b's position (before c) should be unchanged.
	if strings.Index(got, "renamed:") > strings.Index(got, "c: 2") {
		t.Fatalf("key order changed:\n%s", got)
	}
}

func TestRenameToOwnNameIsNoop(t *testing.T) {
	got := rename(t, "a:\n  b: 1\n", ".a.b", "b")
	if !strings.Contains(got, "b: 1") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenameNonStringKeyResetsTag(t *testing.T) {
	got := rename(t, "true: allow\nfalse: deny\n", `."true"`, "allowRule")
	if strings.Contains(got, "!!") {
		t.Fatalf("stale non-string tag survived the rename:\n%s", got)
	}
	// The result must round-trip: a leftover !!bool tag on a string value
	// fails to decode at all.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("renamed document does not decode: %v\n%s", err, got)
	}
	if !strings.Contains(got, "allowRule: allow") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenameDuplicateKeyTargetsLastOccurrence(t *testing.T) {
	got := rename(t, "a: 1\na: 2\n", ".a", "renamed")
	if !strings.Contains(got, "a: 1") {
		t.Fatalf("want the shadowed a: 1 left standing, got:\n%s", got)
	}
	if !strings.Contains(got, "renamed: 2") {
		t.Fatalf("want the effective a: 2 renamed, got:\n%s", got)
	}
}

func TestRenameErrors(t *testing.T) {
	src := "a:\n  b: 1\n  c: 2\nlist: [1, 2]\nscalar: hi\n"
	cases := []struct{ name, expr, newKey, want string }{
		{"collision with sibling", ".a.b", "c", `"c" already exists`},
		{"non-UTF-8 new key", ".a.b", "\xff", "not valid UTF-8"},
		{"missing key", ".a.nope", "x", "no such key"},
		{"list index", ".list[0]", "x", "a list index cannot be renamed"},
		{"wildcard", ".a.*", "x", "wildcards cannot be used with rename"},
		{"descend into scalar", ".scalar.child", "x", "expected a mapping, got scalar"},
		{"whole document", "", "x", "whole document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc yaml.Node
			_ = yaml.Unmarshal([]byte(src), &doc)
			segs, _ := path.Parse(tc.expr)
			err := ymledit.Rename(&doc, segs, tc.newKey, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Rename(%q, %q) err = %v, want to contain %q", tc.expr, tc.newKey, err, tc.want)
			}
		})
	}
}

// missErr runs op against src and returns the *ymledit.KeyError it must
// produce.
func missErr(t *testing.T, src, expr string, op func(*yaml.Node, []path.Segment) error) *ymledit.KeyError {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(expr)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	err = op(&doc, segs)
	var ke *ymledit.KeyError
	if !errors.As(err, &ke) {
		t.Fatalf("error %v is not a *ymledit.KeyError", err)
	}
	return ke
}

// TestKeyErrorCarriesAvailableKeys: the mapping being searched is the local
// variable the lookup just failed against, so naming what *was* there costs
// nothing — and saves the caller a second command to find out, the same
// argument internal/query's NotFoundError.Available makes for reads.
func TestKeyErrorCarriesAvailableKeys(t *testing.T) {
	const src = "a:\n  beta: 1\n  alpha: 2\n  gamma: 3\nlist: [1, 2]\n"
	ops := map[string]func(*yaml.Node, []path.Segment) error{
		"delete": func(d *yaml.Node, s []path.Segment) error { return ymledit.Delete(d, s, nil) },
		"rename": func(d *yaml.Node, s []path.Segment) error { return ymledit.Rename(d, s, "z", nil) },
		"append": func(d *yaml.Node, s []path.Segment) error {
			return ymledit.Append(d, s, &yaml.Node{Kind: yaml.ScalarNode, Value: "1"}, nil)
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			ke := missErr(t, src, ".a.nope", op)
			// Sorted, matching what `keys` prints and what a read miss
			// offers, so the two halves of the CLI don't disagree.
			want := []string{"alpha", "beta", "gamma"}
			if !reflect.DeepEqual(ke.Available, want) {
				t.Errorf("Available = %q, want %q", ke.Available, want)
			}
			if got := path.Format(ke.Path); got != "a.nope" {
				t.Errorf("Path formats to %q, want %q", got, "a.nope")
			}
			if !strings.Contains(ke.Error(), "no such key") {
				t.Errorf("message changed: %q", ke.Error())
			}
		})
	}
}

// TestKeyErrorOnTheTopLevelDocument covers the mapping with no trail above it.
func TestKeyErrorOnTheTopLevelDocument(t *testing.T) {
	ke := missErr(t, "b: 1\na: 2\n", ".nope", func(d *yaml.Node, s []path.Segment) error {
		return ymledit.Delete(d, s, nil)
	})
	if !reflect.DeepEqual(ke.Available, []string{"a", "b"}) {
		t.Errorf("Available = %q, want [a b]", ke.Available)
	}
}

// TestKeyErrorOnAnEmptyMapping: no keys, but still a key miss — the caller
// needs to tell that apart from an error with nothing to offer, so the slice
// is non-nil, the same contract query.NotFoundError.Available has.
func TestKeyErrorOnAnEmptyMapping(t *testing.T) {
	ke := missErr(t, "a: {}\n", ".a.nope", func(d *yaml.Node, s []path.Segment) error {
		return ymledit.Delete(d, s, nil)
	})
	if ke.Available == nil {
		t.Error("Available is nil for an empty mapping; it must be non-nil and empty")
	}
	if len(ke.Available) != 0 {
		t.Errorf("Available = %q, want empty", ke.Available)
	}
}

// TestKeyErrorDeduplicates: a malformed document can repeat a key, and
// offering it twice reads like a bug in the tool rather than in the input.
func TestKeyErrorDeduplicates(t *testing.T) {
	var doc yaml.Node
	// Built by hand: yaml.v3 refuses to decode a duplicate key.
	dup := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "a"}, {Kind: yaml.ScalarNode, Value: "1"},
		{Kind: yaml.ScalarNode, Value: "a"}, {Kind: yaml.ScalarNode, Value: "2"},
		{Kind: yaml.ScalarNode, Value: "b"}, {Kind: yaml.ScalarNode, Value: "3"},
	}}
	doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{dup}}
	segs, err := path.Parse(".nope")
	if err != nil {
		t.Fatal(err)
	}
	var ke *ymledit.KeyError
	if !errors.As(ymledit.Delete(&doc, segs, nil), &ke) {
		t.Fatal("want a *ymledit.KeyError")
	}
	if !reflect.DeepEqual(ke.Available, []string{"a", "b"}) {
		t.Errorf("Available = %q, want [a b] with the duplicate collapsed", ke.Available)
	}
}

// TestNonKeyMissesAreNotKeyErrors: an out-of-range index or a scalar in the
// way has no keys to offer and must not be dressed up as if it did.
func TestNonKeyMissesAreNotKeyErrors(t *testing.T) {
	const src = "a: {b: 1}\nlist: [1, 2]\nscalar: hi\n"
	for _, expr := range []string{".list[9]", ".a[0]", ".list.x", ".scalar.child"} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatal(err)
		}
		segs, err := path.Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		var ke *ymledit.KeyError
		if errors.As(ymledit.Delete(&doc, segs, nil), &ke) {
			t.Errorf("Delete(%q) produced a *ymledit.KeyError: %v", expr, ke)
		}
	}
}

// setMustExist is the must-exist form of apply's op signature, for missErr.
func setMustExist(d *yaml.Node, s []path.Segment) error {
	return ymledit.SetMustExist(d, s, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "9"}, nil)
}

// TestSetMustExistReplacesAnExistingValue: the guard changes nothing about a
// path that resolves, which is most of what it has to get right.
func TestSetMustExistReplacesAnExistingValue(t *testing.T) {
	const src = "a:\n  b:\n    c: 1\nlist: [1, 2]\n"
	for name, expr := range map[string]string{
		"leaf":        ".a.b.c",
		"whole map":   ".a.b",
		"list elem":   ".list[0]",
		"negative ix": ".list[-1]",
	} {
		t.Run(name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			segs, err := path.Parse(expr)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := setMustExist(&doc, segs); err != nil {
				t.Fatalf("SetMustExist(%s) on an existing path: %v", expr, err)
			}
		})
	}
}

// TestSetMustExistRefusesToCreate is #164: Set creates whatever it can't
// find, so a typo is indistinguishable from an intentional new key. The
// must-exist form reports the same *KeyError the other verbs already give,
// which is what makes #163's available keys and "did you mean" carry over
// for free.
func TestSetMustExistRefusesToCreate(t *testing.T) {
	const src = "a:\n  beta: 1\n  alpha: 2\nnull_parent:\nlist: [1]\n"
	for name, tc := range map[string]struct {
		expr      string
		wantPath  string
		available []string
	}{
		// A missing leaf on a mapping that exists: the issue's own example.
		"missing leaf": {".a.nope", "a.nope", []string{"alpha", "beta"}},
		// A missing intermediate, which Set would have invented a mapping for.
		"missing intermediate": {".a.nope.deeper", "a.nope", []string{"alpha", "beta"}},
		// Top level, where the mapping is the document root.
		"missing at root": {".nope", "nope", []string{"a", "list", "null_parent"}},
		// A null value is not a mapping with the key in it, so walking into
		// one is still creation — Set converts it, this must not.
		"through a null": {".null_parent.x", "null_parent.x", nil},
	} {
		t.Run(name, func(t *testing.T) {
			ke := missErr(t, src, tc.expr, setMustExist)
			if got := path.Format(ke.Path); got != tc.wantPath {
				t.Errorf("KeyError.Path = %q, want %q", got, tc.wantPath)
			}
			if tc.available != nil && !reflect.DeepEqual(ke.Available, tc.available) {
				t.Errorf("Available = %q, want %q", ke.Available, tc.available)
			}
		})
	}
}

// TestSetMustExistLeavesTheDocumentUnchangedOnAMiss: a refused edit must not
// half-apply. The null case is the one that can: Set replaces a null node
// with an empty mapping *before* looking the key up, so a naive guard would
// fail after having already changed the tree.
func TestSetMustExistLeavesTheDocumentUnchangedOnAMiss(t *testing.T) {
	const src = "a:\n  beta: 1\nnull_parent:\n"
	for name, expr := range map[string]string{
		"missing key":    ".a.nope",
		"through a null": ".null_parent.x",
	} {
		t.Run(name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			segs, err := path.Parse(expr)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := setMustExist(&doc, segs); err == nil {
				t.Fatal("want an error")
			}
			var buf bytes.Buffer
			enc := yaml.NewEncoder(&buf)
			enc.SetIndent(2)
			if err := enc.Encode(&doc); err != nil {
				t.Fatalf("encode: %v", err)
			}
			_ = enc.Close()
			if buf.String() != src {
				t.Errorf("document changed by a refused edit:\nwant:\n%q\ngot:\n%q", src, buf.String())
			}
		})
	}
}

// TestSetStillCreatesWithoutTheGuard pins the default, which #164 is explicit
// about keeping: creating is right and matches `jq '.a.b = x'`.
func TestSetStillCreatesWithoutTheGuard(t *testing.T) {
	got := apply(t, "a:\n  beta: 1\n", ".a.nope.deeper", "1", false)
	if !strings.Contains(got, "nope") || !strings.Contains(got, "deeper") {
		t.Errorf("Set should still create a missing path, got:\n%s", got)
	}
}

// resolveFailures is every way a path can fail to resolve in a document, per
// verb. --all-docs on the editing commands needs to tell these apart from a
// refusal that is about the edit rather than the document (#170), and they
// are plain errors with no sentinel, so ErrNotResolved tags them.
func resolveFailures() map[string]func(*yaml.Node, []path.Segment) error {
	return map[string]func(*yaml.Node, []path.Segment) error{
		"delete": func(d *yaml.Node, s []path.Segment) error { return ymledit.Delete(d, s, nil) },
		"rename": func(d *yaml.Node, s []path.Segment) error { return ymledit.Rename(d, s, "z", nil) },
		"append": func(d *yaml.Node, s []path.Segment) error {
			return ymledit.Append(d, s, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"}, nil)
		},
		"setMustExist": setMustExist,
	}
}

// TestNotResolvedIsTagged: a document that simply doesn't have the path must
// be distinguishable from one where the edit is wrong, which is what lets
// --all-docs skip the first and fail on the second.
func TestNotResolvedIsTagged(t *testing.T) {
	const src = "a:\n  beta: 1\nscalar: text\nlist: [1]\n"
	exprs := map[string]string{
		"missing key":        ".a.nope",
		"through a scalar":   ".scalar.nope",
		"index on a map":     ".a[0]",
		"index out of range": ".list[9]",
	}
	for verb, op := range resolveFailures() {
		for name, expr := range exprs {
			// rename refuses an index path outright — "a list index cannot
			// be renamed" is ErrUnsupported, true of every document, so it
			// is deliberately not in the skippable class. Asserted as such
			// in TestRefusalsAboutTheEditAreNotNotResolved.
			if verb == "rename" && strings.Contains(name, "index") {
				continue
			}
			t.Run(verb+"/"+name, func(t *testing.T) {
				var doc yaml.Node
				if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				segs, err := path.Parse(expr)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				err = op(&doc, segs)
				if err == nil {
					t.Fatalf("%s %s: want an error", verb, expr)
				}
				if !errors.Is(err, ymledit.ErrNotResolved) {
					t.Errorf("%v is not tagged ErrNotResolved", err)
				}
			})
		}
	}
}

// TestNotResolvedTaggingLeavesMessagesAlone: tagging is for callers, not
// readers. Same contract cmd's classifiedError has for exit codes — an error
// keeps exactly the text it had.
func TestNotResolvedTaggingLeavesMessagesAlone(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("scalar: text\n"), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(".scalar.nope")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = ymledit.Delete(&doc, segs, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	const want = "scalar.nope: expected a mapping, got scalar"
	if err.Error() != want {
		t.Errorf("message changed by tagging:\nwant %q\ngot  %q", want, err.Error())
	}
}

// TestAnEmptyDocumentIsNotResolved: a stream can hold an empty document
// (`---` with nothing under it), and --all-docs has to skip it rather than
// fail the batch.
func TestAnEmptyDocumentIsNotResolved(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("\n"), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	segs, err := path.Parse(".a")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = ymledit.Delete(&doc, segs, nil)
	if err == nil {
		t.Fatal("want an error for an empty document")
	}
	if !errors.Is(err, ymledit.ErrNotResolved) {
		t.Errorf("%v is not tagged ErrNotResolved", err)
	}
}

// TestRefusalsAboutTheEditAreNotNotResolved is the guard that makes the tag
// worth having: a wildcard path and an edit that would strand an alias are
// wrong about the *edit*, true of every document in a stream, so --all-docs
// must report them rather than skipping document after document.
func TestRefusalsAboutTheEditAreNotNotResolved(t *testing.T) {
	t.Run("wildcard", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("a:\n  b: 1\n"), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		segs, err := path.Parse(".a.*")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		err = ymledit.Delete(&doc, segs, nil)
		if !errors.Is(err, ymledit.ErrUnsupported) {
			t.Fatalf("want ErrUnsupported, got %v", err)
		}
		if errors.Is(err, ymledit.ErrNotResolved) {
			t.Error("a wildcard path must not be tagged ErrNotResolved")
		}
	})
	// rename's own rule, which is why the index shapes are excluded from
	// TestNotResolvedIsTagged's rename rows: renaming a list element is not
	// something any document could support, so --all-docs must report it
	// rather than skipping every document in turn.
	t.Run("rename an index", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("list: [1]\n"), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		segs, err := path.Parse(".list[0]")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		err = ymledit.Rename(&doc, segs, "z", nil)
		if !errors.Is(err, ymledit.ErrUnsupported) {
			t.Fatalf("want ErrUnsupported, got %v", err)
		}
		if errors.Is(err, ymledit.ErrNotResolved) {
			t.Error("renaming an index must not be tagged ErrNotResolved")
		}
	})
	t.Run("anchored", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("base: &b\n  k: 1\nuse:\n  <<: *b\n"), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		segs, err := path.Parse(".base")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		err = ymledit.Delete(&doc, segs, nil)
		if !errors.Is(err, ymledit.ErrAnchored) {
			t.Fatalf("want ErrAnchored, got %v", err)
		}
		if errors.Is(err, ymledit.ErrNotResolved) {
			t.Error("an anchored-node refusal must not be tagged ErrNotResolved")
		}
	})
}

// TestEmptyDocumentNodeIsReportedNotPanicked pins #204. Every verb guards
// on len(doc.Content) == 0 for a DocumentNode, and what stands behind that
// guard is doc.Content[0] — an index-out-of-range panic, not a wrong answer.
//
// yaml.v3 never hands one over: a bare `---` decodes to a *null scalar*
// document, which takes the kind-mismatch path instead ("expected a mapping,
// got scalar"), and cmd rejects a stream with no documents before any verb
// runs. A library caller building a tree by hand can, though, and this
// package's doc comments invite exactly that — "doc may be a DocumentNode or
// a bare value node". So the guards are a supported input's behavior rather
// than dead code, and this is what they do.
func TestEmptyDocumentNodeIsReportedNotPanicked(t *testing.T) {
	emptyDoc := func() *yaml.Node { return &yaml.Node{Kind: yaml.DocumentNode} }
	segs, err := path.Parse(".a")
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	value, err := ymledit.ParseValue("1", false)
	if err != nil {
		t.Fatalf("parse value: %v", err)
	}

	t.Run("delete", func(t *testing.T) {
		assertEmptyDocError(t, ymledit.Delete(emptyDoc(), segs, nil))
	})
	t.Run("append", func(t *testing.T) {
		assertEmptyDocError(t, ymledit.Append(emptyDoc(), segs, value, nil))
	})
	t.Run("rename", func(t *testing.T) {
		assertEmptyDocError(t, ymledit.Rename(emptyDoc(), segs, "b", nil))
	})

	// set creates what it can't find, and an empty document is the extreme
	// of that: it gets the mapping it needs before the walk continues.
	t.Run("set creates the mapping", func(t *testing.T) {
		doc := emptyDoc()
		if err := ymledit.Set(doc, segs, value, nil); err != nil {
			t.Fatalf("Set: %v", err)
		}
		out, merr := yaml.Marshal(doc)
		if merr != nil {
			t.Fatalf("marshal: %v", merr)
		}
		if got := string(out); got != "a: 1\n" {
			t.Errorf("Set left %q, want \"a: 1\\n\"", got)
		}
	})

	// --must-exist's whole promise is that a refused edit changes nothing,
	// which FuzzSetMustExistNeverCreates asserts over every path it can
	// reach — but never over this document, since its seed always decodes to
	// a non-empty one.
	t.Run("set --must-exist refuses without mutating", func(t *testing.T) {
		doc := emptyDoc()
		err := ymledit.SetMustExist(doc, segs, value, nil) //nolint:govet // deliberate shadow: the outer err is the setup's
		if err == nil {
			t.Fatal("SetMustExist on an empty document: want an error, got nil")
		}
		if !errors.Is(err, ymledit.ErrNotResolved) {
			t.Errorf("error %v does not wrap ErrNotResolved", err)
		}
		if len(doc.Content) != 0 {
			out, _ := yaml.Marshal(doc)
			t.Errorf("SetMustExist refused but left the document changed: %q", out)
		}
	})
}

func assertEmptyDocError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "the document is empty") {
		t.Errorf("error %q does not say the document is empty", err)
	}
	if !errors.Is(err, ymledit.ErrNotResolved) {
		t.Errorf("error %v does not wrap ErrNotResolved", err)
	}
}

// resolveDoc is the document TestResolutionFailsIdenticallyAcrossVerbs walks:
// one of each kind at the top level, so a path can fail at every seam.
const resolveDoc = "scalar: 1\nmap: {a: 1}\nlist: [1, 2]\n"

// TestResolutionFailsIdenticallyAcrossVerbs is #210's premise as a property.
// delete, append and rename each walked the path themselves, so the kind
// checks, the negative-index normalization and the bounds message existed in
// triplicate and could drift apart one verb at a time — a fix landing in one
// copy and missing the others. They resolve a path the same way, so they must
// fail it with the same words.
//
// Two differences are legitimate and asserted rather than elided: a wildcard
// refusal names the verb, and rename rejects an index as its final segment
// before resolving anything, since there is no key there to rename.
func TestResolutionFailsIdenticallyAcrossVerbs(t *testing.T) {
	fresh := func(t *testing.T) *yaml.Node {
		t.Helper()
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(resolveDoc), &doc); err != nil {
			t.Fatal(err)
		}
		return &doc
	}
	value, err := ymledit.ParseValue("9", false)
	if err != nil {
		t.Fatal(err)
	}
	text := func(err error) string {
		if err == nil {
			return "<nil: the path resolved>"
		}
		return err.Error()
	}

	// Paths whose final segment is a mapping key: all three verbs resolve
	// them the same way and must report the same failure.
	for _, expr := range []string{
		".nope", ".map.nope", ".nope.deeper", ".scalar.child", ".list.x",
		".map.a.deeper", ".list[0].x", ".map[0].x",
	} {
		t.Run(expr, func(t *testing.T) {
			segs, err := path.Parse(expr)
			if err != nil {
				t.Fatal(err)
			}
			del := text(ymledit.Delete(fresh(t), segs, nil))
			app := text(ymledit.Append(fresh(t), segs, value, nil))
			ren := text(ymledit.Rename(fresh(t), segs, "z", nil))
			if del != app || del != ren {
				t.Errorf("the three verbs disagree about %s:\n delete: %s\n append: %s\n rename: %s", expr, del, app, ren)
			}
			if del == "<nil: the path resolved>" {
				t.Errorf("%s resolved; this table needs paths that fail", expr)
			}
		})
	}

	// Paths ending in an index: delete and append still agree, and rename's
	// own refusal is the documented difference.
	for _, expr := range []string{".map[0]", ".list[9]", ".list[-9]", ".scalar[0]"} {
		t.Run(expr, func(t *testing.T) {
			segs, err := path.Parse(expr)
			if err != nil {
				t.Fatal(err)
			}
			del := text(ymledit.Delete(fresh(t), segs, nil))
			app := text(ymledit.Append(fresh(t), segs, value, nil))
			if del != app {
				t.Errorf("delete and append disagree about %s:\n delete: %s\n append: %s", expr, del, app)
			}
			rerr := ymledit.Rename(fresh(t), segs, "z", nil)
			if !errors.Is(rerr, ymledit.ErrUnsupported) {
				t.Errorf("rename(%s) = %v, want ErrUnsupported: an index is not a key to rename", expr, rerr)
			}
		})
	}
}
