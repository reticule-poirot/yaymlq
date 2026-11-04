package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestEditFlagsRegisteredOnce is #311. bindEditFlags claims to be the single
// registration point for the flags every editing verb shares, but
// -i/--in-place, --doc, --max-bytes, and --indent were hand-registered
// separately in set.go (via bindValueEditFlags), delete.go, rename.go, and
// apply.go instead of going through it — four copies of the same four
// flags, free to drift the way --print0 already had on the read side
// (#309). This locks them to one usage string and default each, the same
// guard TestReadFlagsRegisteredOnce gives the read verbs.
func TestEditFlagsRegisteredOnce(t *testing.T) {
	root := NewRootCommand()
	verbs := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		switch c.Name() {
		case "set", "append", "delete", "rename", "apply":
			verbs[c.Name()] = c
		}
	}
	if len(verbs) != 5 {
		t.Fatalf("want set, append, delete, rename, apply covered, got %d: %v", len(verbs), verbs)
	}

	order := []string{"set", "append", "delete", "rename", "apply"}
	want := map[string]string{}
	for _, name := range order {
		c := verbs[name]
		for _, flagName := range []string{"in-place", "doc", "max-bytes", "indent"} {
			f := c.Flags().Lookup(flagName)
			if f == nil {
				t.Errorf("%s: missing shared edit flag --%s", name, flagName)
				continue
			}
			key := flagName
			got := f.Usage + "|" + f.DefValue
			if w, ok := want[key]; !ok {
				want[key] = got
			} else if got != w {
				t.Errorf("%s: --%s is %q, want %q (same as the other editing verbs)", name, flagName, got, w)
			}
		}
	}
}
