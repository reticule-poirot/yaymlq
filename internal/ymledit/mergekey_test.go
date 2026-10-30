package ymledit

import (
	"errors"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"gopkg.in/yaml.v3"
)

func mustNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}

// TestMergedKeyErrorIsNotAMissedPath is the sentinel contract stated where it
// lives, rather than only through the CLI. ErrMergedKey must behave like
// ErrUnsupported and ErrAnchored — wrong about the edit, not about the
// document — so a caller walking a stream reports it instead of skipping the
// document and going on to claim nothing matched.
func TestMergedKeyErrorIsNotAMissedPath(t *testing.T) {
	doc := mustNode(t, "base: &base\n  x: 1\none:\n  <<: *base\n")
	segs, err := path.Parse(".one.x")
	if err != nil {
		t.Fatal(err)
	}

	err = Delete(doc, segs, nil)
	if err == nil {
		t.Fatal("want a refusal for a merged key")
	}
	if !errors.Is(err, ErrMergedKey) {
		t.Errorf("want ErrMergedKey, got %v", err)
	}
	if errors.Is(err, ErrNotResolved) {
		t.Error("ErrMergedKey must not carry ErrNotResolved: the document has the path, only the edit is impossible")
	}
	var ke *KeyError
	if errors.As(err, &ke) {
		t.Errorf("want a refusal of its own kind, got a KeyError: %v", err)
	}
	if !strings.Contains(err.Error(), `"base"`) {
		t.Errorf("want the source anchor named, got %q", err)
	}
}

// TestMergedFromFollowsAChain covers the shapes a merge can take, including
// the one a document is least likely to have and a hand-built tree most
// easily produces: a merged mapping that merges in turn.
func TestMergedFromFollowsAChain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    string
		key    string
		anchor string
		want   bool
	}{
		{"single alias", "a: &a\n  x: 1\nm:\n  <<: *a\n", "x", "a", true},
		{"sequence", "a: &a\n  x: 1\nb: &b\n  z: 2\nm:\n  <<: [*a, *b]\n", "z", "b", true},
		{"chained", "a: &a\n  x: 1\nb: &b\n  <<: *a\nm:\n  <<: *b\n", "x", "b", true},
		{"absent", "a: &a\n  x: 1\nm:\n  <<: *a\n", "nope", "", false},
		{"no merge key", "m:\n  y: 1\n", "y", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustNode(t, tc.src)
			// doc -> document node -> mapping -> value of the last key.
			root := doc.Content[0]
			m := root.Content[len(root.Content)-1]
			anchor, ok := mergedFrom(m, tc.key, 0)
			if ok != tc.want {
				t.Fatalf("mergedFrom(%q) = %v, want %v", tc.key, ok, tc.want)
			}
			if ok && anchor != tc.anchor {
				t.Errorf("want the anchor %q, got %q", tc.anchor, anchor)
			}
		})
	}
}

// TestMergedFromStopsOnADepthLimit: a parsed document cannot form a merge
// cycle, since an alias can only name an anchor defined above it. The guard
// is for a hand-built tree, which this package's callers and its fuzzers can
// produce, so it is checked directly rather than assumed.
func TestMergedFromStopsOnADepthLimit(t *testing.T) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: "<<"}
	alias := &yaml.Node{Kind: yaml.AliasNode, Value: "self"}
	alias.Alias = m
	m.Content = []*yaml.Node{key, alias}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := mergedFrom(m, "anything", 0); ok {
			t.Error("a self-referential merge should find nothing")
		}
	}()
	<-done
}
