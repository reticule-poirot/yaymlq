package query_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

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

// TestRunWildcardManySiblingsStayCorrect guards #68's extend() optimization
// (reusing trail's spare capacity via append instead of always copying to a
// fresh array): out only ever collects values, never trail data, but this
// confirms a large sibling fan-out doesn't somehow cross-contaminate
// results if that ever changed.
func TestRunWildcardManySiblingsStayCorrect(t *testing.T) {
	m := make(map[string]any, 500)
	want := make([]any, 500)
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("k%04d", i) // zero-padded so string sort == numeric order
		m[key] = i
		want[i] = i
	}
	got, err := query.Run(m, "*")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %d values, want %d (or a value out of order/wrong)", len(got), len(want))
	}
}

// TestRunDeepWildcardFanOutStaysLinear guards the actual fix for #68:
// extend() used to copy the whole trail on every step, even on the
// non-error path where it's only used to build an eventual error message —
// amplified by wildcard fan-out, since every dead sibling branch paid a
// full trail copy before returning. A document combining real depth and
// fan-out (deep nested chain, each level also carrying several sibling
// scalars) exercises both dimensions at once.
func TestRunDeepWildcardFanOutStaysLinear(t *testing.T) {
	const depth, width = 500, 20
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString("{c: ")
	}
	b.WriteString("leaf")
	for i := 0; i < depth; i++ {
		b.WriteString(", ")
		for j := 0; j < width; j++ {
			fmt.Fprintf(&b, "s%d: 0, ", j)
		}
		b.WriteString("}")
	}
	var doc any
	if err := yaml.Unmarshal([]byte(b.String()), &doc); err != nil {
		t.Fatalf("unmarshal generated doc: %v", err)
	}
	expr := strings.Repeat(".*", depth)

	start := time.Now()
	_, err := query.Run(doc, expr)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Generous bound: the fixed cost is well under a second; a regression
	// to O(depth²) trail-copying (amplified by the width-20 fan-out at
	// every level) would blow well past this even at this modest size.
	if elapsed > 5*time.Second {
		t.Fatalf("Run over a %d-deep/%d-wide document took %v, want well under 5s — looks like the quadratic bug is back", depth, width, elapsed)
	}
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

// TestRunMatchesCarriesPaths is the core of --paths: every match reports the
// concrete path that reached it, with wildcards resolved.
func TestRunMatchesCarriesPaths(t *testing.T) {
	doc := mustDoc(t)
	got, err := query.RunMatches(doc, ".services[*].name")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	want := []struct {
		path  string
		value any
	}{
		{"services[0].name", "web"},
		{"services[1].name", "db"},
	}
	if len(got) != len(want) {
		t.Fatalf("RunMatches returned %d matches, want %d: %#v", len(got), len(want), got)
	}
	for i, w := range want {
		if p := path.Format(got[i].Path); p != w.path {
			t.Errorf("match %d path = %q, want %q", i, p, w.path)
		}
		if got[i].Value != w.value {
			t.Errorf("match %d value = %#v, want %#v", i, got[i].Value, w.value)
		}
	}
}

// TestRunMatchesPathsAreNotAliased guards the one way this can silently go
// wrong: walk's extend() appends into the trail's spare capacity, so a
// collected path that isn't copied out is overwritten by the next sibling
// and every match reports the last one's key.
//
// The shape is deliberate: three literal segments, then a trailing
// wildcard. A trail only shares an array once append has over-allocated it
// (nil grows to cap 1, then 2, then 4), and the shared write is only still
// visible in the result if the match happens on that same array — so the
// wildcard has to be the last segment. Anything shallower, or with a
// segment after the wildcard, allocates a fresh array and hides the bug.
func TestRunMatchesPathsAreNotAliased(t *testing.T) {
	var doc any
	src := "a:\n  b:\n    c:\n      x: 1\n      y: 2\n      z: 3\n"
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := query.RunMatches(doc, ".a.b.c.*")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	paths := make([]string, 0, len(got))
	for _, m := range got {
		paths = append(paths, path.Format(m.Path))
	}
	want := []string{"a.b.c.x", "a.b.c.y", "a.b.c.z"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
}

// TestRunMatchesQuotesAmbiguousKeys: a discovered path has to be usable as
// input, so a key needing quotes gets them.
func TestRunMatchesQuotesAmbiguousKeys(t *testing.T) {
	got, err := query.RunMatches(mustDoc(t), ".*")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	var found bool
	for _, m := range got {
		if path.Format(m.Path) == `"weird.key"` {
			found = true
		}
	}
	if !found {
		paths := make([]string, 0, len(got))
		for _, m := range got {
			paths = append(paths, path.Format(m.Path))
		}
		t.Errorf(`no match formatted as "weird.key"; got %q`, paths)
	}
}

// TestRunMatchesWholeDocument: an empty path matches the document itself,
// whose path is the root.
func TestRunMatchesWholeDocument(t *testing.T) {
	got, err := query.RunMatches(mustDoc(t), ".")
	if err != nil {
		t.Fatalf("RunMatches: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("RunMatches(.) returned %d matches, want 1", len(got))
	}
	if p := path.Format(got[0].Path); p != "." {
		t.Errorf("root path = %q, want %q", p, ".")
	}
}

// TestRunMatchesReportsNotFound: the error contract is Run's, unchanged.
func TestRunMatchesReportsNotFound(t *testing.T) {
	_, err := query.RunMatches(mustDoc(t), ".nope.deeper")
	if !errors.Is(err, query.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestNotFoundErrorCarriesAvailableKeys: the mapping that failed the lookup
// is in hand when the error is built, so listing its keys costs nothing —
// and saves the caller a second parse of the same document just to find out
// what was there.
func TestNotFoundErrorCarriesAvailableKeys(t *testing.T) {
	tests := []struct {
		name, path string
		want       []string
	}{
		{"missing top-level key", "missing", []string{"name", "nested", "services", "version", "weird.key"}},
		{"missing nested key", "nested.a.z", []string{"b"}},
		{"missing key under a wildcard is not an error", "services[*].name", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := query.Run(mustDoc(t), tc.path)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Run(%q): unexpected error %v", tc.path, err)
				}
				return
			}
			var nfe *query.NotFoundError
			if !errors.As(err, &nfe) {
				t.Fatalf("Run(%q): error %v is not a *query.NotFoundError", tc.path, err)
			}
			if !reflect.DeepEqual(nfe.Available, tc.want) {
				t.Errorf("Run(%q): Available = %q, want %q", tc.path, nfe.Available, tc.want)
			}
		})
	}
}

// TestNotFoundErrorAvailableIsEmptyWhenThereAreNoKeysToOffer: a list index
// out of range, a key into a list, or a scalar in the way all already say
// what went wrong in the message; a key list would be noise or a lie.
func TestNotFoundErrorAvailableIsEmptyWhenThereAreNoKeysToOffer(t *testing.T) {
	for _, expr := range []string{"services[9]", "services.name", "name.deeper", "nested[0]"} {
		_, err := query.Run(mustDoc(t), expr)
		var nfe *query.NotFoundError
		if !errors.As(err, &nfe) {
			t.Fatalf("Run(%q): error %v is not a *query.NotFoundError", expr, err)
		}
		if len(nfe.Available) != 0 {
			t.Errorf("Run(%q): Available = %q, want none", expr, nfe.Available)
		}
	}
}

// TestNotFoundErrorAvailableIsSortedAndUncapped: the engine reports every
// key in a deterministic order and leaves any truncation to the caller,
// which is the layer that knows what its output is for.
func TestNotFoundErrorAvailableIsSortedAndUncapped(t *testing.T) {
	var doc any
	src := "m:\n"
	for i := 30; i > 0; i-- {
		src += fmt.Sprintf("  k%02d: %d\n", i, i)
	}
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	_, err := query.Run(doc, ".m.nope")
	var nfe *query.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("error %v is not a *query.NotFoundError", err)
	}
	if len(nfe.Available) != 30 {
		t.Fatalf("Available has %d keys, want all 30", len(nfe.Available))
	}
	if !sort.StringsAreSorted(nfe.Available) {
		t.Errorf("Available is not sorted: %q", nfe.Available)
	}
}

// TestNotFoundErrorAvailableOnNonStringKeys: a mapping with a non-string key
// decodes to map[any]any, and its keys are offered in the same string form a
// path expression would use to address them.
func TestNotFoundErrorAvailableOnNonStringKeys(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("m:\n  1: a\n  true: b\n  x: c\n"), &doc); err != nil {
		t.Fatal(err)
	}
	_, err := query.Run(doc, ".m.nope")
	var nfe *query.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("error %v is not a *query.NotFoundError", err)
	}
	want := []string{"1", "true", "x"}
	if !reflect.DeepEqual(nfe.Available, want) {
		t.Errorf("Available = %q, want %q", nfe.Available, want)
	}
}

// TestNotFoundErrorAvailableIsNonNilForAnEmptyMapping pins the distinction
// cmd relies on to tell "this mapping has no keys" from "this miss has no
// keys to offer at all": Available is non-nil exactly when the miss was a
// key lookup against a mapping, empty mapping included.
func TestNotFoundErrorAvailableIsNonNilForAnEmptyMapping(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal([]byte("m: {}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	_, err := query.Run(doc, ".m.nope")
	var nfe *query.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("error %v is not a *query.NotFoundError", err)
	}
	if nfe.Available == nil {
		t.Error("Available is nil for an empty mapping; it must be non-nil and empty")
	}
	if len(nfe.Available) != 0 {
		t.Errorf("Available = %q, want empty", nfe.Available)
	}

	// The contrast: a miss that isn't a key lookup leaves it nil.
	_, err = query.Run(doc, ".m[0]")
	if !errors.As(err, &nfe) {
		t.Fatalf("error %v is not a *query.NotFoundError", err)
	}
	if nfe.Available != nil {
		t.Errorf("Available = %q for an index miss, want nil", nfe.Available)
	}
}
