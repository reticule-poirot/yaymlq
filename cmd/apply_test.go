package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript writes script to a temp file in dir and returns its path.
func writeScript(t *testing.T, dir, script string) string {
	t.Helper()
	f := filepath.Join(dir, "edits.txt")
	if err := os.WriteFile(f, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestApplyRunsEveryOp(t *testing.T) {
	in := "a: 1\nb: [1, 2]\nc: 2\nd: 3\n"
	script := "set .a = 9\n" +
		"append .b = 3\n" +
		"delete .c\n" +
		"rename .d = e\n"
	f := writeScript(t, t.TempDir(), script)

	got, err := execute(t, in, "apply", "-f", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "c:") {
		t.Fatalf("deleted key still present:\n%s", got)
	}
	if !strings.Contains(got, "a: 9") {
		t.Fatalf("set didn't apply:\n%s", got)
	}
	if !strings.Contains(got, "b: [1, 2, 3]") {
		t.Fatalf("append didn't apply:\n%s", got)
	}
	if !strings.Contains(got, "e: 3") || strings.Contains(got, "d:") {
		t.Fatalf("rename didn't apply:\n%s", got)
	}
}

func TestApplyCommentsAndBlankLinesIgnored(t *testing.T) {
	in := "a: 1\n"
	script := "# leading comment\n\nset .a = 2\n\n# trailing comment\n"
	f := writeScript(t, t.TempDir(), script)

	got, err := execute(t, in, "apply", "-f", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "a: 2" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyInPlace(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(doc, []byte("a: 1\nb: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := writeScript(t, dir, "set .a = 9\ndelete .b\n")

	if _, err := execute(t, "", "apply", "-f", f, "-i", doc); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "a: 9" {
		t.Fatalf("file after apply:\n%s", out)
	}
}

func TestApplyDiff(t *testing.T) {
	in := "a: 1\n"
	f := writeScript(t, t.TempDir(), "set .a = 9\n")

	got, err := execute(t, in, "apply", "-f", f, "--diff")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "-a: 1") || !strings.Contains(got, "+a: 9") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestApplyScriptFromStdin(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(doc, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := execute(t, "set .a = 9\n", "apply", "-f", "-", doc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "a: 9" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyStdinScriptNeedsFileArg(t *testing.T) {
	_, err := execute(t, "set .a = 9\n", "apply", "-f", "-")
	if err == nil {
		t.Fatal("want an error: script from stdin with no document file argument")
	}
}

func TestApplyMissingEditsFlag(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "apply"); err == nil {
		t.Fatal("want an error: apply requires -f/--edits")
	}
}

func TestApplyEmptyScriptErrors(t *testing.T) {
	f := writeScript(t, t.TempDir(), "# just a comment\n\n")
	if _, err := execute(t, "a: 1\n", "apply", "-f", f); err == nil {
		t.Fatal("want an error for an empty edit script")
	}
}

func TestApplyBadScriptSyntaxErrors(t *testing.T) {
	f := writeScript(t, t.TempDir(), "frobnicate .a = 1\n")
	if _, err := execute(t, "a: 1\n", "apply", "-f", f); err == nil {
		t.Fatal("want an error for an unknown verb")
	}
}

func TestApplyMissingScriptFileErrors(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "apply", "-f", "/no/such/edits.txt"); err == nil {
		t.Fatal("want an error for a missing edit script file")
	}
}

func TestApplyFailingOpAbortsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "cfg.yaml")
	original := "a: 1\n"
	if err := os.WriteFile(doc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// The first op would succeed; the second fails (no such path) — nothing
	// should be written for either.
	f := writeScript(t, dir, "set .a = 9\ndelete .nonexistent.path\n")

	_, err := execute(t, "", "apply", "-f", f, "-i", doc)
	if err == nil {
		t.Fatal("want an error from the failing second op")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error should name line 2, got %v", err)
	}
	out, rerr := os.ReadFile(doc)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(out) != original {
		t.Fatalf("file must be untouched after a failing op, got %q", out)
	}
}

func TestApplyExitCodeForBadScriptIsUsage(t *testing.T) {
	f := writeScript(t, t.TempDir(), "frobnicate .a = 1\n")
	_, err := execute(t, "a: 1\n", "apply", "-f", f)
	if got := exitCode(err, os.Stderr); got != 3 {
		t.Fatalf("want exit 3 (usage) for a bad script, got %d (%v)", got, err)
	}
}

func TestApplyPathParseErrorInScriptIsUsage(t *testing.T) {
	f := writeScript(t, t.TempDir(), "set a[ = 1\n")
	_, err := execute(t, "a: 1\n", "apply", "-f", f)
	if got := exitCode(err, os.Stderr); got != 3 {
		t.Fatalf("want exit 3 (usage) for a bad path expression, got %d (%v)", got, err)
	}
}

func TestApplyMaxBytesBoundsTheEditScriptToo(t *testing.T) {
	// --max-bytes previously only bounded the document argument; the script
	// read via -f/--edits was unbounded.
	f := writeScript(t, t.TempDir(), "set .a = 1\nset .b = 2\nset .c = 3\n")
	_, err := execute(t, "a: 1\n", "apply", "-f", f, "--max-bytes", "5")
	if err == nil {
		t.Fatal("want an error: edit script exceeds --max-bytes")
	}
	if got := exitCode(err, os.Stderr); got != 4 {
		t.Fatalf("want exit 4 (io) for an oversized edit script, got %d (%v)", got, err)
	}
}

func TestApplyExitCodeForOversizedLineIsIO(t *testing.T) {
	// A single line over the scanner's buffer cap is a read-level failure
	// (editscript.ErrRead), not a syntax mistake — it should classify the
	// same as any other failed read (exit 4), not as usage (exit 3).
	f := writeScript(t, t.TempDir(), "set .a = "+strings.Repeat("x", 2<<20)+"\n")
	_, err := execute(t, "a: 1\n", "apply", "-f", f, "--max-bytes", "0")
	if got := exitCode(err, os.Stderr); got != 4 {
		t.Fatalf("want exit 4 (io) for an oversized script line, got %d (%v)", got, err)
	}
}
