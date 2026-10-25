package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Three documents, the *first* of which has no .meta at all, and a list
// where the other two do so append has something to append to. One stream
// every verb offering --all-docs can be aimed at.
//
// The order is what makes the "first miss" half of the rule testable: on a
// path nothing resolves, document 0's error names a different set of
// available keys (kind) from the other two (tags), so reporting the last miss
// instead of the first is visible rather than indistinguishable.
const leniencyStream = "kind: B\n" +
	"---\n" +
	"kind: A\nmeta:\n  tags: [a]\n" +
	"---\n" +
	"kind: C\nmeta:\n  tags: [c]\n"

// leniencyArgs aims a command at path in its own argument shape. A creating
// verb gets --must-exist, which is its selector form: without it there is
// nothing to skip (#164), and skipping is the rule under test.
//
// Hand-maintained like minimalArgs, and TestLeniencyArgsCoverEveryAllDocsVerb
// is the tripwire for a verb added later with no entry.
func leniencyArgs(t *testing.T, name, path string) ([]string, bool) {
	t.Helper()
	switch name {
	case "get", "keys", "len", "type", "delete":
		return []string{path}, true
	case "set":
		return []string{"--must-exist", path, "9"}, true
	case "append":
		return []string{path, "9"}, true
	case "rename":
		return []string{path, "labels"}, true
	case "apply":
		// delete, not set: a creating op would land the path in the
		// document that hasn't got it instead of skipping it.
		script := filepath.Join(t.TempDir(), "edits.txt")
		if err := os.WriteFile(script, []byte("delete "+path+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"--must-exist", "-f", script}, true
	}
	return nil, false
}

// leniencyCommands returns every command whose --all-docs is the selector the
// cross-file contract describes. validate is the one documented exception:
// --require --all-docs is the strict "every document has it" reading, and
// CLAUDE.md names it as the only place that reading exists.
func leniencyCommands() []*cobra.Command {
	var out []*cobra.Command
	for _, c := range conflictCommands() {
		if c.Flags().Lookup("all-docs") == nil || commandName(c) == "validate" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// TestAllDocsSkipsADocumentThePathIsntIn is the first half of the rule, for
// every verb at once: --all-docs selects documents, so a document without the
// path is skipped rather than failing the run (#170/#175).
func TestAllDocsSkipsADocumentThePathIsntIn(t *testing.T) {
	for _, c := range leniencyCommands() {
		name := commandName(c)
		t.Run(name, func(t *testing.T) {
			base, ok := leniencyArgs(t, name, ".meta.tags")
			if !ok {
				t.Skip("no leniencyArgs entry; TestLeniencyArgsCoverEveryAllDocsVerb reports this")
			}
			got, err := execute(t, leniencyStream, invocation(name, []string{"--all-docs"}, base)...)
			if err != nil {
				t.Fatalf("want the two matching documents to carry the run, got %v", err)
			}
			if got == "" {
				t.Fatal("want output from the documents that did match")
			}
			// An editing verb writes the whole stream back, so the skipped
			// document has to come through untouched rather than dropped.
			if c.Flags().Lookup("in-place") != nil && !strings.Contains(got, "kind: B") {
				t.Errorf("the skipped document is missing from the output:\n%s", got)
			}
		})
	}
}

// TestAllDocsFailsWhenNoDocumentMatches is the other half, and the one that
// is written down three times: lenient about which documents match, not about
// matching nothing — and the failure that surfaces is the *first* miss, so
// #137's recovery facts survive instead of the last document's.
func TestAllDocsFailsWhenNoDocumentMatches(t *testing.T) {
	for _, c := range leniencyCommands() {
		name := commandName(c)
		t.Run(name, func(t *testing.T) {
			base, ok := leniencyArgs(t, name, ".meta.nope")
			if !ok {
				t.Skip("no leniencyArgs entry; TestLeniencyArgsCoverEveryAllDocsVerb reports this")
			}
			args := invocation(name, []string{"--all-docs"}, base)
			out, err := execute(t, leniencyStream, args...)
			if err == nil {
				t.Fatalf("want a failure when no document matches, got output %q", out)
			}
			if code := exitCode(err, io.Discard); code != 1 {
				t.Errorf("want exit 1 for a stream-wide miss, got %d", code)
			}
			if msg := annotateNotFound(err, defaultMaxSuggestions).Error(); !strings.Contains(msg, "available keys: kind") {
				t.Errorf("want the *first* missing document's available keys, got %q", msg)
			}
		})
	}
}

// TestLeniencyArgsCoverEveryAllDocsVerb catches a verb added later with no
// leniencyArgs entry, which would otherwise skip both tests above for it
// instead of failing.
func TestLeniencyArgsCoverEveryAllDocsVerb(t *testing.T) {
	seen := 0
	for _, c := range leniencyCommands() {
		name := commandName(c)
		if _, ok := leniencyArgs(t, name, ".a"); !ok {
			t.Errorf("leniencyArgs has no entry for command %q", name)
			continue
		}
		seen++
	}
	// get, keys, len, type and the five editing verbs.
	if seen != 9 {
		t.Errorf("want the nine verbs offering a selector --all-docs, got %d", seen)
	}
}
