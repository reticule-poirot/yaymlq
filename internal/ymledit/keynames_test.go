package ymledit_test

import (
	"errors"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"go.yaml.in/yaml/v4"
)

// TestKeyLookupAgreesWithAndWithoutTheCache: the cached index and a fresh
// lookup have to give the same answers, including after a rename changes what
// a mapping's keys answer to.
func TestKeyLookupAgreesWithAndWithoutTheCache(t *testing.T) {
	seg := func(k string) []path.Segment { return []path.Segment{{Key: k}} }
	val := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
	type op func(*yaml.Node, *ymledit.EditIndex) error
	set := func(k string) op {
		return func(d *yaml.Node, ei *ymledit.EditIndex) error { return ymledit.SetMustExist(d, seg(k), val("z"), ei) }
	}
	rename := func(k, to string) op {
		return func(d *yaml.Node, ei *ymledit.EditIndex) error { return ymledit.Rename(d, seg(k), to, ei) }
	}
	for _, tc := range []struct {
		name, in string
		ops      []op
		want     []error
	}{
		{"either spelling", "1.0: a\n", []op{set("1"), set("1.0")}, []error{nil, nil}},
		{"a miss", "1.0: a\n", []op{set("2")}, []error{ymledit.ErrNotResolved}},
		{"a shared spelling", "1: a\n1.0: b\n", []op{set("1"), set("1.0")}, []error{ymledit.ErrAmbiguousKey, nil}},
		{"a rename frees both spellings", "1.0: a\n", []op{rename("1", "q"), set("1"), set("1.0"), set("q")},
			[]error{nil, ymledit.ErrNotResolved, ymledit.ErrNotResolved, nil}},
		{"a rename ends a shared spelling", "1: a\n1.0: b\n", []op{set("1"), rename("1.0", "q"), set("1")},
			[]error{ymledit.ErrAmbiguousKey, nil, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, shared := range []bool{false, true} {
				var doc yaml.Node
				if err := yaml.Unmarshal([]byte(tc.in), &doc); err != nil {
					t.Fatal(err)
				}
				var ei *ymledit.EditIndex
				if shared {
					ei = &ymledit.EditIndex{}
				}
				for i, o := range tc.ops {
					if err := o(&doc, ei); !errors.Is(err, tc.want[i]) {
						t.Errorf("shared=%v op %d: got %v, want %v", shared, i, err, tc.want[i])
					}
				}
			}
		})
	}
}
