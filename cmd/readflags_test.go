package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestReadFlagsRegisteredOnce is #309. bindReadFlags is meant to be the
// single registration point for the flags get, keys, len, and type share —
// before it existed, root.go and inspect.go hand-registered the same flags
// separately, and the wording had already drifted: --print0's help said
// "implies -o raw" on get and "implies --output raw" on keys/len/type.
func TestReadFlagsRegisteredOnce(t *testing.T) {
	root := NewRootCommand()
	verbs := map[string]*cobra.Command{"get": root}
	for _, c := range root.Commands() {
		switch c.Name() {
		case "keys", "len", "type":
			verbs[c.Name()] = c
		}
	}
	if len(verbs) != 4 {
		t.Fatalf("want get, keys, len, type covered, got %d: %v", len(verbs), verbs)
	}

	shared := []string{
		"output", "print0", "doc", "all-docs", "max-bytes",
		"max-suggestions", "exit-status", "quiet", "string", "default",
	}
	order := []string{"get", "keys", "len", "type"}

	wantPrint0 := ""
	for _, name := range order {
		c := verbs[name]
		for _, n := range shared {
			if c.Flags().Lookup(n) == nil {
				t.Errorf("%s: missing shared read flag --%s", name, n)
			}
		}
		f := c.Flags().Lookup("print0")
		if f == nil {
			continue
		}
		if wantPrint0 == "" {
			wantPrint0 = f.Usage
		} else if f.Usage != wantPrint0 {
			t.Errorf("%s: --print0 usage %q, want %q (same as get's)", name, f.Usage, wantPrint0)
		}
	}
}
