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
