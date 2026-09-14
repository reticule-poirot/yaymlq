package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDiffPrintsUnifiedDiff(t *testing.T) {
	in := "a: 1\nb: 2\n"
	got, err := execute(t, in, "set", "--diff", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "--- a/stdin\n+++ b/stdin\n@@ -1,2 +1,2 @@\n-a: 1\n+a: 9\n b: 2\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSetDryRunIsAnAliasForDiff(t *testing.T) {
	in := "a: 1\n"
	got, err := execute(t, in, "set", "--dry-run", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "-a: 1") || !strings.Contains(got, "+a: 9") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetDiffNoChangeIsEmpty(t *testing.T) {
	in := "a: 1\n"
	got, err := execute(t, in, "set", "--diff", ".a", "1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "" {
		t.Fatalf("want no output for a no-op edit, got %q", got)
	}
}

func TestAppendDiff(t *testing.T) {
	in := "a:\n  - 1\n  - 2\n"
	got, err := execute(t, in, "append", "--diff", ".a", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "+  - 3") {
		t.Fatalf("want an added \"- 3\" line, got:\n%s", got)
	}
}

func TestDeleteDiff(t *testing.T) {
	in := "a: 1\nb: 2\n"
	got, err := execute(t, in, "delete", "--diff", ".b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "-b: 2") {
		t.Fatalf("want a removed \"b: 2\" line, got:\n%s", got)
	}
}

func TestRenameDiff(t *testing.T) {
	in := "a: 1\n"
	got, err := execute(t, in, "rename", "--diff", ".a", "z")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "-a: 1") || !strings.Contains(got, "+z: 1") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDiffDoesNotWriteInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := execute(t, "", "set", "-i", "--diff", ".a", "9", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "-a: 1") || !strings.Contains(got, "+a: 9") {
		t.Fatalf("want a diff on stdout, got:\n%s", got)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a: 1\n" {
		t.Fatalf("-i --diff must not write the file; got %q", data)
	}
}

func TestDiffAlwaysExitsZero(t *testing.T) {
	// A dry-run is a preview, not an assertion — it succeeds whether or not
	// there were changes.
	if _, err := execute(t, "a: 1\n", "set", "--diff", ".a", "1"); err != nil {
		t.Fatalf("no-op diff: want nil error, got %v", err)
	}
	if _, err := execute(t, "a: 1\n", "set", "--diff", ".a", "9"); err != nil {
		t.Fatalf("changed diff: want nil error, got %v", err)
	}
}
