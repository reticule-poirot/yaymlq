package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOK(t *testing.T) {
	out, err := execute(t, "a: 1\nb: [1, 2]\n", "validate")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "" {
		t.Fatalf("want no output on success, got %q", out)
	}
}

func TestValidateEmptyStreamErrors(t *testing.T) {
	// Every other command (get, keys/len/type, set/append/delete/rename/apply)
	// already treats an empty input stream as a parse failure rather than
	// trivially "valid" — validate previously had no such guard.
	out, err := execute(t, "", "validate")
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, "no YAML documents") {
		t.Fatalf("want an error naming the empty stream, got %q", out)
	}
}

func TestValidateMultiDocOK(t *testing.T) {
	if _, err := execute(t, "a: 1\n---\nb: 2\n", "validate"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateBadYAMLFromStdin(t *testing.T) {
	out, err := execute(t, "a: [1, 2\n", "validate")
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, "stdin:") || !strings.Contains(out, "line") {
		t.Fatalf("want a labeled line/col error, got %q", out)
	}
}

func TestValidateFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "", "validate", f); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateMultipleFilesOneBadKeepsCheckingAll(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(good, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("a: [1, 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "", "validate", good, bad)
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, filepath.Base(bad)) {
		t.Fatalf("want the bad file named in output, got %q", out)
	}
	if strings.Contains(out, filepath.Base(good)) {
		t.Fatalf("good file shouldn't be mentioned, got %q", out)
	}
}

func TestValidateMissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")

	out, err := execute(t, "", "validate", missing)
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, filepath.Base(missing)) {
		t.Fatalf("want the missing file named in output, got %q", out)
	}
}

func TestValidateMaxBytes(t *testing.T) {
	big := "data: " + strings.Repeat("x", 4096) + "\n"
	if _, err := execute(t, big, "validate", "--max-bytes", "128"); err == nil {
		t.Fatal("want error when input exceeds --max-bytes")
	}
}

func TestValidateRequirePresent(t *testing.T) {
	in := "image:\n  tag: v1\nreplicas: 3\n"
	if _, err := execute(t, in, "validate", "--require", ".image.tag", "--require", ".replicas"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateRequireMissing(t *testing.T) {
	in := "image:\n  tag: v1\n"
	out, err := execute(t, in, "validate", "--require", ".image.tag", "--require", ".nope")
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, "stdin:") || !strings.Contains(out, ".nope") {
		t.Fatalf("want the missing path named, got %q", out)
	}
	if strings.Contains(out, ".image.tag") {
		t.Fatalf("present path shouldn't be named as missing, got %q", out)
	}
}

func TestValidateRequireSatisfiedByEitherDoc(t *testing.T) {
	in := "a: 1\n---\nb: 2\n"
	if _, err := execute(t, in, "validate", "--require", ".b"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateRequireDoesNotMaskParseError(t *testing.T) {
	out, err := execute(t, "a: [1, 2\n", "validate", "--require", ".a")
	if err == nil {
		t.Fatal("want an error for malformed input")
	}
	if !strings.Contains(out, "parsing YAML") {
		t.Fatalf("want the parse error, not a require error, got %q", out)
	}
}

// TestValidateRequireSyntaxErrorIsUsage is the #124 regression. checkRequired
// treated every query.Run error as "not found", which swallowed
// path.SyntaxError too — so a malformed expression was reported as a path
// missing from the document (exit 1) rather than as the bad argument it is
// (exit 3, the code every other command returns for the same input).
func TestValidateRequireSyntaxErrorIsUsage(t *testing.T) {
	_, err := execute(t, "a: 1\n", "validate", "--require", "a[")
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("malformed --require path -> exit %d (%v), want 3", got, err)
	}
}

// TestValidateRequireSyntaxErrorMentionsTheSyntax: the message must point at
// the expression, not at the document. "missing required path(s)" sends the
// reader to look in their YAML for something that could never parse.
func TestValidateRequireSyntaxErrorMentionsTheSyntax(t *testing.T) {
	out, err := execute(t, "a: 1\n", "validate", "--require", "a[")
	msg := out
	if err != nil {
		msg += err.Error()
	}
	if strings.Contains(msg, "missing required path") {
		t.Fatalf("a syntax error must not be reported as a missing path, got %q", msg)
	}
	if !strings.Contains(msg, "unterminated") {
		t.Fatalf("want the underlying syntax error surfaced, got %q", msg)
	}
}

// TestValidateRequireSyntaxErrorCheckedBeforeInput: an unsatisfiable
// invocation should fail on its own terms, not depend on the input parsing.
func TestValidateRequireSyntaxErrorCheckedBeforeInput(t *testing.T) {
	_, err := execute(t, "a: [1, 2\n", "validate", "--require", "a[")
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("bad --require with malformed YAML -> exit %d, want 3 (the argument is wrong regardless)", got)
	}
}

// TestValidateRequireGenuineMissStaysExitOne pins the behavior that must NOT
// change: a well-formed path that simply isn't there is a validation
// failure, and validate deliberately keeps its flat exit 1 for those.
func TestValidateRequireGenuineMissStaysExitOne(t *testing.T) {
	_, err := execute(t, "a: 1\n", "validate", "--require", ".nope")
	if got := exitCode(err, io.Discard); got != 1 {
		t.Fatalf("genuine miss -> exit %d, want 1", got)
	}
}

// TestValidateRequireUsageMatchesBehavior pins --require's flag description
// to what TestValidateRequireSatisfiedByEitherDoc demonstrates: the path has
// to resolve in one document, not in all of them. The description is the only
// account of the flag a `--help` reader reaches first, and the only one
// `yaymlq schema` exports — so it drifting from the long help above it is a
// contract error, not a typo.
func TestValidateRequireUsageMatchesBehavior(t *testing.T) {
	usage := newValidateCommand().Flags().Lookup("require").Usage
	if strings.Contains(usage, "every input") {
		t.Errorf("--require usage promises %q, but a path resolving in a single document satisfies it: %q", "every input", usage)
	}
	if !strings.Contains(usage, "at least one") {
		t.Errorf("--require usage should say the path must resolve in at least one document, got %q", usage)
	}
}
