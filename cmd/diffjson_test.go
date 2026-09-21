package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffFormatInvalidValueIsUsageError(t *testing.T) {
	_, err := execute(t, "a: 1\n", "set", "--diff", "--diff-format", "xml", ".a", "9")
	if exitCode(err, io.Discard) != 3 {
		t.Fatalf("want exit 3 for an unknown --diff-format value, got %v", err)
	}
}

func TestSetDiffFormatJSONPrintsStructuredDiff(t *testing.T) {
	got, err := execute(t, "a: 1\nb: 2\n", "set", "--diff", "--diff-format", "json", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if d.File != "stdin" || !d.Changed || len(d.Hunks) != 1 {
		t.Fatalf("got %+v", d)
	}
}

func TestSetDiffFormatJSONNoChangeIsValidJSON(t *testing.T) {
	got, err := execute(t, "a: 1\n", "set", "--diff", "--diff-format", "json", ".a", "1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, `"hunks":[]`) {
		t.Fatalf("want an empty (not null) hunks array for a no-op edit, got %q", got)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if d.Changed {
		t.Fatalf("want changed=false for a no-op edit, got %+v", d)
	}
}

func TestDiffFormatJSONAlwaysExitsZero(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "set", "--diff", "--diff-format", "json", ".a", "1"); err != nil {
		t.Fatalf("no-op diff: want nil error, got %v", err)
	}
	if _, err := execute(t, "a: 1\n", "set", "--diff", "--diff-format", "json", ".a", "9"); err != nil {
		t.Fatalf("changed diff: want nil error, got %v", err)
	}
}

func TestDiffFormatDefaultIsTextByteForByte(t *testing.T) {
	in := "a: 1\nb: 2\n"
	withoutFlag, err := execute(t, in, "set", "--diff", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	withText, err := execute(t, in, "set", "--diff", "--diff-format", "text", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if withoutFlag != withText {
		t.Fatalf("--diff-format text must match the no-flag default byte-for-byte:\nno-flag: %q\ntext:    %q", withoutFlag, withText)
	}
}

func TestAppendDiffFormatJSON(t *testing.T) {
	got, err := execute(t, "a:\n  - 1\n  - 2\n", "append", "--diff", "--diff-format", "json", ".a", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed {
		t.Fatalf("got %+v", d)
	}
}

func TestDeleteDiffFormatJSON(t *testing.T) {
	got, err := execute(t, "a: 1\nb: 2\n", "delete", "--diff", "--diff-format", "json", ".b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed {
		t.Fatalf("got %+v", d)
	}
}

func TestRenameDiffFormatJSON(t *testing.T) {
	got, err := execute(t, "a: 1\n", "rename", "--diff", "--diff-format", "json", ".a", "z")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed {
		t.Fatalf("got %+v", d)
	}
}

func TestApplyDiffFormatJSON(t *testing.T) {
	editsFile := filepath.Join(t.TempDir(), "edits.txt")
	if err := os.WriteFile(editsFile, []byte("set .a = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := execute(t, "a: 1\n", "apply", "--diff", "--diff-format", "json", "-f", editsFile)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed {
		t.Fatalf("got %+v", d)
	}
}

func TestDiffFormatJSONDoesNotWriteInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := execute(t, "", "set", "-i", "--diff", "--diff-format", "json", ".a", "9", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var d diffJSON
	if err := json.Unmarshal([]byte(got), &d); err != nil {
		t.Fatalf("unmarshal %q: %v", got, err)
	}
	if !d.Changed {
		t.Fatalf("got %+v", d)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a: 1\n" {
		t.Fatalf("-i --diff must not write the file; got %q", data)
	}
}

func TestDiffFormatWithoutDiffIsUsageError(t *testing.T) {
	// Passing --diff-format without --diff used to be silently ignored, so
	// `set -i --diff-format json` wrote the file instead of previewing it.
	_, err := execute(t, "a: 1\n", "set", "--diff-format", "json", ".a", "9")
	if exitCode(err, io.Discard) != 3 {
		t.Fatalf("want exit 3 for --diff-format without --diff, got %v", err)
	}
}

func TestDiffFormatWithoutDiffDoesNotWriteInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "", "set", "-i", "--diff-format", "json", ".a", "9", f); err == nil {
		t.Fatal("want an error, got nil")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a: 1\n" {
		t.Fatalf("a rejected invocation must not write the file; got %q", data)
	}
}

func TestDiffFormatTextDefaultWithoutDiffStillFine(t *testing.T) {
	// Not passing the flag at all is unaffected — a plain edit still works.
	if _, err := execute(t, "a: 1\n", "set", ".a", "9"); err != nil {
		t.Fatalf("plain edit without --diff: want nil error, got %v", err)
	}
}
