package cmd

import (
	"strings"
	"testing"
)

// TestAllDocsUsageSaysWhetherTheVerbCreates is #202. bindEditFlags is the
// single registration point for the flags every editing verb shares, which
// gave all five the same --all-docs help: "skipping those the path isn't
// in". For set — and for apply's set ops — nothing is skipped. set creates
// what it can't find, so --all-docs lands the key in every document, and
// --must-exist is what makes it skip instead (#164).
//
// The verbs that create are exactly the verbs offering --must-exist, so the
// live tree decides which promise each one makes rather than a list here. A
// sixth editing verb inherits whichever wording matches its own behavior, or
// fails this test.
func TestAllDocsUsageSaysWhetherTheVerbCreates(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range conflictCommands() {
		// --in-place marks the editing verbs. The read verbs carry an
		// --all-docs of their own, with its own wording, and nothing to
		// create.
		if c.Flags().Lookup("in-place") == nil {
			continue
		}
		name := commandName(c)
		f := c.Flags().Lookup("all-docs")
		if f == nil {
			t.Fatalf("%s: an editing verb that doesn't offer --all-docs", name)
		}
		seen[name] = true

		creates := c.Flags().Lookup("must-exist") != nil
		switch {
		case creates && strings.Contains(f.Usage, "skipping those the path"):
			t.Errorf("%s creates a path it can't find, but its --all-docs help promises to skip: %q", name, f.Usage)
		case creates && !strings.Contains(f.Usage, "creat"):
			t.Errorf("%s creates a path it can't find, and its --all-docs help doesn't say so: %q", name, f.Usage)
		case !creates && !strings.Contains(f.Usage, "skip"):
			t.Errorf("%s skips a document the path isn't in, and its --all-docs help doesn't say so: %q", name, f.Usage)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("want all five editing verbs covered, got %d: %v", len(seen), seen)
	}
}
