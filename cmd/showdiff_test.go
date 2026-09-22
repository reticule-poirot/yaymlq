package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp returns a temp file holding content, for the in-place tests below.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestShowDiffWritesTheFileAndPrintsTheDiff is the whole point of the flag:
// -i alone is silent and -i --diff deliberately doesn't write, so a caller
// wanting both had to spend two invocations — or write, then re-read the file
// to find out what changed.
func TestShowDiffWritesTheFileAndPrintsTheDiff(t *testing.T) {
	f := writeTemp(t, "a: 1\nb: 2\n")

	got, err := execute(t, "", "set", "-i", "--show-diff", ".a", "9", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	after, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "a: 9\nb: 2\n" {
		t.Fatalf("--show-diff must still write the file, got %q", after)
	}
	if !strings.Contains(got, "-a: 1") || !strings.Contains(got, "+a: 9") {
		t.Fatalf("want a unified diff of the change on stdout, got %q", got)
	}
}

func TestShowDiffRequiresInPlace(t *testing.T) {
	// Without -i the edited document goes to stdout; a diff there would be
	// interleaved with the output it describes.
	_, err := execute(t, "a: 1\n", "set", "--show-diff", ".a", "9")
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("want exit 3 for --show-diff without -i, got %d (%v)", got, err)
	}
}

func TestShowDiffConflictsWithDiff(t *testing.T) {
	// --diff means "preview, don't write"; --show-diff means "write and tell
	// me". Asking for both is a contradiction, not a preference.
	f := writeTemp(t, "a: 1\n")
	_, err := execute(t, "", "set", "-i", "--diff", "--show-diff", ".a", "9", f)
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("want exit 3 for --diff with --show-diff, got %d (%v)", got, err)
	}
}

func TestShowDiffRejectedInvocationDoesNotWrite(t *testing.T) {
	const original = "a: 1\n"
	f := writeTemp(t, original)
	if _, err := execute(t, "", "set", "-i", "--diff", "--show-diff", ".a", "9", f); err == nil {
		t.Fatal("want an error, got nil")
	}
	after, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("a rejected invocation must not write the file, got %q", after)
	}
}

func TestShowDiffHonoursDiffFormatJSON(t *testing.T) {
	f := writeTemp(t, "a: 1\nb: 2\n")

	got, err := execute(t, "", "set", "-i", "--show-diff", "--diff-format", "json", ".a", "9", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed || len(d.Hunks) != 1 {
		t.Fatalf("got %+v", d)
	}

	after, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "a: 9\nb: 2\n" {
		t.Fatalf("--show-diff --diff-format json must still write, got %q", after)
	}
}

// TestShowDiffNoChangeIsEmpty keeps --show-diff consistent with --diff's
// existing convention rather than inventing a second one.
func TestShowDiffNoChangeIsEmpty(t *testing.T) {
	f := writeTemp(t, "a: 1\n")
	got, err := execute(t, "", "set", "-i", "--show-diff", ".a", "1", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "" {
		t.Fatalf("want no diff output for a no-op edit, got %q", got)
	}
}

func TestShowDiffWorksOnEveryEditingCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"set", []string{"set", ".a", "9"}},
		{"append", []string{"append", ".list", "3"}},
		{"delete", []string{"delete", ".b"}},
		{"rename", []string{"rename", ".b", "z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := writeTemp(t, "a: 1\nb: 2\nlist:\n  - 1\n")
			args := append([]string{tc.args[0], "-i", "--show-diff"}, tc.args[1:]...)
			got, err := execute(t, "", append(args, f)...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(got, "@@") {
				t.Fatalf("want a diff hunk header, got %q", got)
			}
		})
	}
}
