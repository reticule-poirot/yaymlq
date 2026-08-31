package ymledit_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
	if err := ymledit.Set(&doc, segs, vn); err != nil {
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
		if err := ymledit.Set(&doc, segs, vn); err == nil {
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
		err := ymledit.Set(&doc, segs, vn)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Set(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
		}
	}
}

func TestSetWildcardIsUnsupported(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("a: {b: 1}\n"), &doc)
	segs, _ := path.Parse(".a.*")
	vn, _ := ymledit.ParseValue("x", false)
	err := ymledit.Set(&doc, segs, vn)
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
	if err := ymledit.Delete(&doc, segs); err != nil {
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
			err := ymledit.Delete(&doc, segs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Delete(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
			}
		})
	}
}

func TestDeleteWildcardIsUnsupported(t *testing.T) {
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("a: {b: 1}\n"), &doc)
	segs, _ := path.Parse(".a.*")
	if err := ymledit.Delete(&doc, segs); !errors.Is(err, ymledit.ErrUnsupported) {
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
	if err := ymledit.Append(&doc, segs, vn); err != nil {
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
			err := ymledit.Append(&doc, segs, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Append(%q) err = %v, want to contain %q", tc.expr, err, tc.want)
			}
		})
	}
}
