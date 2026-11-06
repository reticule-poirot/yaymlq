package ymledit_test

import (
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"go.yaml.in/yaml/v3"
)

// TestEditsKeepUntouchedNodeIdentity: cmd's splice finds what an edit changed
// by comparing node pointers against a snapshot, so every edit has to leave
// the nodes it did not touch at the same address, in a parent that still
// holds them.
func TestEditsKeepUntouchedNodeIdentity(t *testing.T) {
	const src = "a: 1\nb:\n  c: 2\n  d: [x, y]\ne: 3\n"
	edits := []struct {
		name string
		run  func(doc *yaml.Node, segs []path.Segment) error
		expr string
	}{
		{"set", func(d *yaml.Node, s []path.Segment) error {
			return ymledit.Set(d, s, &yaml.Node{Kind: yaml.ScalarNode, Value: "9"}, nil)
		}, ".b.c"},
		{"set new key", func(d *yaml.Node, s []path.Segment) error {
			return ymledit.Set(d, s, &yaml.Node{Kind: yaml.ScalarNode, Value: "9"}, nil)
		}, ".b.z"},
		{"delete", func(d *yaml.Node, s []path.Segment) error { return ymledit.Delete(d, s, nil) }, ".b.c"},
		{"rename", func(d *yaml.Node, s []path.Segment) error { return ymledit.Rename(d, s, "r", nil) }, ".b.c"},
		{"append", func(d *yaml.Node, s []path.Segment) error {
			return ymledit.Append(d, s, &yaml.Node{Kind: yaml.ScalarNode, Value: "z"}, nil)
		}, ".b.d"},
	}
	for _, tc := range edits {
		t.Run(tc.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
				t.Fatal(err)
			}
			before := map[*yaml.Node]bool{}
			var collect func(*yaml.Node)
			collect = func(n *yaml.Node) {
				before[n] = true
				for _, c := range n.Content {
					collect(c)
				}
			}
			collect(&doc)
			segs, err := path.Parse(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.run(&doc, segs); err != nil {
				t.Fatal(err)
			}
			// Every untouched sibling of the edit's parent chain is still
			// reachable at its old address.
			for _, keep := range []string{".a", ".e", ".b"} {
				ks, _ := path.Parse(keep)
				n := lookup(doc.Content[0], ks[0].Key)
				if n == nil || !before[n] {
					t.Errorf("%s: %s no longer the node it was", tc.name, keep)
				}
			}
		})
	}
}

func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
