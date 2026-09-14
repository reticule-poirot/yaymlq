package query_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
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

func TestRunSingle(t *testing.T) {
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
			if len(got) != 1 {
				t.Fatalf("Run(%q) returned %d values, want 1", tc.path, len(got))
			}
			if !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("Run(%q) = %#v, want %#v", tc.path, got[0], tc.want)
			}
		})
	}
}

func TestRunWildcard(t *testing.T) {
	doc := mustDoc(t)

	tests := []struct {
		name string
		path string
		want []any
	}{
		{"list star", "services.*.name", []any{"web", "db"}},
		{"list brackets", "services[].name", []any{"web", "db"}},
		{"list star brackets", "services[*].name", []any{"web", "db"}},
		{"nested wildcard into list", "services.*.ports.0", []any{80, 5432}},
		{"map values sorted by key", "nested.a.*", []any{"hello"}},
		{"double wildcard", "services.*.ports.*", []any{80, 443, 5432}},
		{"missing key on some branches is skipped", "services.*.image", nil},
		{"scalar branch under a wildcard is skipped", "services.*.ports.0.*", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := query.Run(doc, tc.path)
			if err != nil {
				t.Fatalf("Run(%q) error: %v", tc.path, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
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
		{"leading wildcard over scalar", "version.*"},
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

// mixedKeyDoc has a mapping that mixes a non-string key (80) with ordinary
// string keys, so yaml.v3 decodes it as map[any]any instead of
// map[string]any — the whole mapping, including its string keys, needs to
// stay reachable.
const mixedKeyDoc = `
80: http
443: https
name: web
svc:
  a: {name: alpha}
  b: {80: http, name: beta}
  c: {name: gamma}
`

func TestRunNonStringKeyedMapping(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte(mixedKeyDoc), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	t.Run("string key next to a non-string one resolves", func(t *testing.T) {
		got, err := query.Run(doc, "name")
		if err != nil {
			t.Fatalf("Run(%q) error: %v", "name", err)
		}
		if len(got) != 1 || got[0] != "web" {
			t.Fatalf("Run(%q) = %#v, want [web]", "name", got)
		}
	})

	t.Run("quoted numeric key resolves by string form", func(t *testing.T) {
		got, err := query.Run(doc, `"80"`)
		if err != nil {
			t.Fatalf(`Run(%q) error: %v`, `"80"`, err)
		}
		if len(got) != 1 || got[0] != "http" {
			t.Fatalf(`Run(%q) = %#v, want [http]`, `"80"`, got)
		}
	})

	t.Run("wildcard does not skip a non-string-keyed branch", func(t *testing.T) {
		got, err := query.Run(doc, "svc.*.name")
		if err != nil {
			t.Fatalf("Run(%q) error: %v", "svc.*.name", err)
		}
		want := []any{"alpha", "beta", "gamma"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Run(%q) = %#v, want %#v", "svc.*.name", got, want)
		}
	})
}

func TestRunNotFoundErrorCarriesPath(t *testing.T) {
	doc := mustDoc(t)

	tests := []struct {
		name string
		path string
		want string // path.Format(NotFoundError.Path)
	}{
		{"missing top-level key", "missing", "missing"},
		{"missing nested key", "nested.a.z", "nested.a.z"},
		{"index into map", "nested[0]", "nested[0]"},
		{"key into list", "services.name", "services.name"},
		{"index out of range", "services[9]", "services[9]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := query.Run(doc, tc.path)
			var nfe *query.NotFoundError
			if !errors.As(err, &nfe) {
				t.Fatalf("Run(%q): error %v is not a *query.NotFoundError", tc.path, err)
			}
			if got := path.Format(nfe.Path); got != tc.want {
				t.Fatalf("Run(%q): NotFoundError.Path formats to %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
