package ymledit

import (
	"errors"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"go.yaml.in/yaml/v3"
)

// TestAliasDescentNamesTheAnchor: descending through a plain alias (not a
// merge key) must read like ErrMergedKey's remedy — name the anchor to edit
// instead — not the generic "expected a mapping, got alias" a caller would
// otherwise get for any non-mapping value.
func TestAliasDescentNamesTheAnchor(t *testing.T) {
	doc := mustNode(t, "defaults: &defaults\n  x: 1\nweb: *defaults\n")
	segs, err := path.Parse(".web.x")
	if err != nil {
		t.Fatal(err)
	}

	err = Set(doc, segs, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "2"}, nil)
	if err == nil {
		t.Fatal("want a refusal for descending through an alias")
	}
	if !errors.Is(err, ErrAliased) {
		t.Errorf("want ErrAliased, got %v", err)
	}
	if errors.Is(err, ErrNotResolved) {
		t.Error("ErrAliased must not carry ErrNotResolved: the document has the path, only the edit is impossible")
	}
	if !strings.Contains(err.Error(), `"defaults"`) {
		t.Errorf("want the anchor named, got %q", err)
	}
	if strings.Contains(err.Error(), "expected a mapping") {
		t.Errorf("want the alias remedy, not the generic kind-mismatch message: %q", err)
	}
}

// TestAliasDescentOnDeleteNamesTheAnchor covers the resolve walk Delete,
// Rename and Append share, not just Set's own.
func TestAliasDescentOnDeleteNamesTheAnchor(t *testing.T) {
	doc := mustNode(t, "defaults: &defaults\n  x: 1\nweb: *defaults\n")
	segs, err := path.Parse(".web.x")
	if err != nil {
		t.Fatal(err)
	}

	err = Delete(doc, segs, nil)
	if err == nil {
		t.Fatal("want a refusal for descending through an alias")
	}
	if !errors.Is(err, ErrAliased) {
		t.Errorf("want ErrAliased, got %v", err)
	}
	if !strings.Contains(err.Error(), `"defaults"`) {
		t.Errorf("want the anchor named, got %q", err)
	}
}

// TestAliasDescentThroughSequenceAlias covers the IsIndex branch: an alias
// to a sequence, indexed past it.
func TestAliasDescentThroughSequenceAlias(t *testing.T) {
	doc := mustNode(t, "defaults: &defaults\n  - 1\n  - 2\nweb: *defaults\n")
	segs, err := path.Parse(".web[0]")
	if err != nil {
		t.Fatal(err)
	}

	err = Delete(doc, segs, nil)
	if err == nil {
		t.Fatal("want a refusal for descending through an alias")
	}
	if !errors.Is(err, ErrAliased) {
		t.Errorf("want ErrAliased, got %v", err)
	}
	if !strings.Contains(err.Error(), `"defaults"`) {
		t.Errorf("want the anchor named, got %q", err)
	}
}
