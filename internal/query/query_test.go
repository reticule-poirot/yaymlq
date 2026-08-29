package query_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

const sample = `
name: yaymlq
version: 1
nested:
  a:
    b: hello
services:
  - name: web
    ports: [80, 443]
  - name: db
    ports: [5432]
"weird.key": value
`

func mustDoc(t *testing.T) any {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal([]byte(sample), &doc); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	return doc
}

func TestRun(t *testing.T) {
	doc := mustDoc(t)

	tests := []struct {
		name string
		path string
		want any
	}{
		{"whole doc", "", doc},
		{"dot only", ".", doc},
		{"top scalar", "name", "yaymlq"},
		{"leading dot", ".version", 1},
		{"deep nested", "nested.a.b", "hello"},
		{"bracket index", "services[0].name", "web"},
		{"bare numeric index", "services.1.name", "db"},
		{"nested list value", "services[0].ports[1]", 443},
		{"negative index", "services[-1].name", "db"},
		{"quoted key with dot", `"weird.key"`, "value"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := query.Run(doc, tc.path)
			if err != nil {
				t.Fatalf("Run(%q) error: %v", tc.path, err)
			}
			if !equal(got, tc.want) {
				t.Fatalf("Run(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	doc := mustDoc(t)

	tests := []struct {
		name string
		path string
	}{
		{"missing key", "nope"},
		{"missing nested key", "nested.a.z"},
		{"index into map", "nested[0]"},
		{"key into list", "services.name"},
		{"index out of range", "services[9]"},
		{"unterminated bracket", "services[0"},
		{"bad index", "services[x]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := query.Run(doc, tc.path); err == nil {
				t.Fatalf("Run(%q) expected error, got nil", tc.path)
			}
		})
	}
}

func TestRunNotFoundIs(t *testing.T) {
	doc := mustDoc(t)
	_, err := query.Run(doc, "missing")
	if !errors.Is(err, query.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func equal(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
