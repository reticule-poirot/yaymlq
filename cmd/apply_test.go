package cmd

import (
	"fmt"
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

func TestApplyValueStartingWithHashIsRejected(t *testing.T) {
	// In YAML a leading '#' always opens a comment — before the fix this
	// silently parsed as null instead of erroring, destroying the intended
	// value with no diagnostic.
	f := writeScript(t, t.TempDir(), "set .color = #ffffff\n")
	_, err := execute(t, "color: red\n", "apply", "-f", f)
	if err == nil {
		t.Fatal("want an error: unquoted value starting with '#' looks like a comment")
	}
	if got := exitCode(err, os.Stderr); got != 3 {
		t.Fatalf("want exit 3 (usage), got %d (%v)", got, err)
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

// TestApplyManySetsOnSameMapping guards #72: findValueIndex's full mapping
// scan on every call used to make a batch script that sets many sibling
// keys on the same mapping O(ops²). This doesn't assert on timing (that's
// internal/ymledit's job — TestSetLongPathStaysLinear and friends) — it's a
// correctness check that a real batch through the CLI still applies every
// op right when the shared EditIndex is involved.
func TestApplyManySetsOnSameMapping(t *testing.T) {
	var script strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&script, "set .k%d = %d\n", i, i+1)
	}
	f := writeScript(t, t.TempDir(), script.String())

	got, err := execute(t, "k0: 0\n", "apply", "-f", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for i := 0; i < 500; i++ {
		want := fmt.Sprintf("k%d: %d", i, i+1)
		if !strings.Contains(got, want) {
			t.Fatalf("missing or wrong %q in output", want)
		}
	}
}

// TestApplyDeleteAliasThenAnchorInSameBatch guards #73's trickiest
// correctness case: the shared EditIndex's anchor-reference tracking must
// update incrementally as ops run, not just once up front, or deleting an
// alias and then its anchor's origin in the same batch would be wrongly
// refused (the origin would look "still referenced" using stale state).
func TestApplyDeleteAliasThenAnchorInSameBatch(t *testing.T) {
	in := "a: &x 1\nb: *x\nc: 2\n"
	f := writeScript(t, t.TempDir(), "delete .b\ndelete .a\n")

	got, err := execute(t, in, "apply", "-f", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "c: 2" {
		t.Fatalf("got %q", got)
	}
}

// TestApplyDeleteAnchorBeforeAliasInSameBatchStillRefused is the opposite
// order: the anchor's origin is still referenced when its delete is
// attempted, so it must still be refused, same as a single `delete` would.
func TestApplyDeleteAnchorBeforeAliasInSameBatchStillRefused(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "cfg.yaml")
	original := "a: &x 1\nb: *x\nc: 2\n"
	if err := os.WriteFile(doc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	f := writeScript(t, dir, "delete .a\ndelete .b\n")

	_, err := execute(t, "", "apply", "-f", f, "-i", doc)
	if err == nil {
		t.Fatal("want an error: .a's anchor is still referenced by .b when its delete runs")
	}
	out, rerr := os.ReadFile(doc)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(out) != original {
		t.Fatalf("file must be untouched after a refused op, got %q", out)
	}
}
