package cmd

import (
	"io"
	"strings"
	"testing"
)

// TestInPlaceNeedsAFileOnEveryEditingVerb pins the rule every editing verb
// shares and none of them may skip: --in-place rewrites the file it was
// given, so there is nothing to rewrite when the document arrived on stdin.
//
// Each verb used to spell the check out itself, four copies of the same two
// lines (#211), and nothing tested any of them. Driven off the live cobra
// tree like flagConflicts, so a sixth editing verb is covered without anyone
// remembering this file exists.
func TestInPlaceNeedsAFileOnEveryEditingVerb(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range conflictCommands() {
		if c.Flags().Lookup("in-place") == nil {
			continue
		}
		name := commandName(c)
		t.Run(name, func(t *testing.T) {
			base, ok := minimalArgs(t, name)
			if !ok {
				t.Skip("no minimalArgs entry; TestMinimalArgsCoversEveryCommand reports this")
			}
			args := invocation(name, []string{"-i"}, base)
			_, err := execute(t, "a: 1\n", args...)
			if err == nil {
				t.Fatalf("%v: want a usage error, got nil", args)
			}
			if got := exitCode(err, io.Discard); got != 3 {
				t.Errorf("%v -> exit %d, want 3 (usage)", args, got)
			}
			if msg := err.Error(); !strings.Contains(msg, "--in-place needs a file argument") {
				t.Errorf("%v -> %q, want the shared --in-place message", args, msg)
			}
		})
		seen[name] = true
	}
	if len(seen) != 5 {
		t.Fatalf("want all five editing verbs covered, got %d: %v", len(seen), seen)
	}
}
