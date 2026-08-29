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
