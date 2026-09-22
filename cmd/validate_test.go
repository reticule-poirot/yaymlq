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

// twoDocs: .a is in both, .only1 is in the second alone.
const twoDocs = "a: 1\n---\na: 2\nonly1: yes\n"

func TestValidateRequireAllDocsRejectsAPartialMatch(t *testing.T) {
	// The default is satisfied by one document; --all-docs is the strict
	// reading people actually want on a multi-document manifest.
	if _, err := execute(t, twoDocs, "validate", "--require", ".only1"); err != nil {
		t.Fatalf("default should pass on a single-document match: %v", err)
	}
	_, err := execute(t, twoDocs, "validate", "--all-docs", "--require", ".only1")
	if got := exitCode(err, io.Discard); got != 1 {
		t.Fatalf("--all-docs should fail when a document lacks the path: exit %d (%v)", got, err)
	}
}

func TestValidateRequireAllDocsPassesWhenEveryDocHasIt(t *testing.T) {
	if _, err := execute(t, twoDocs, "validate", "--all-docs", "--require", ".a"); err != nil {
		t.Fatalf("every document has .a: %v", err)
	}
}

func TestValidateRequireDocSelectsOneDocument(t *testing.T) {
	if _, err := execute(t, twoDocs, "validate", "--doc", "1", "--require", ".only1"); err != nil {
		t.Fatalf("document 1 has .only1: %v", err)
	}
	_, err := execute(t, twoDocs, "validate", "--doc", "0", "--require", ".only1")
	if got := exitCode(err, io.Discard); got != 1 {
		t.Fatalf("document 0 lacks .only1: want exit 1, got %d (%v)", got, err)
	}
}

// TestValidateDocOutOfRangeIsASourceFailure: validate reports per source and
// keeps checking the rest, so a file with fewer documents than --doc asks for
// is that file failing, not the command line being wrong.
func TestValidateDocOutOfRangeIsASourceFailure(t *testing.T) {
	out, err := execute(t, twoDocs, "validate", "--doc", "5", "--require", ".a")
	if got := exitCode(err, io.Discard); got != 1 {
		t.Fatalf("want exit 1, got %d (%v)", got, err)
	}
	if !strings.Contains(out+errText(err), "document 5") {
		t.Fatalf("error should name the out-of-range index, got %q / %v", out, err)
	}
}

// TestValidateDocScopeNeedsRequire: --doc and --all-docs only scope the
// --require check; validate always parses the whole stream. Accepting them
// alone would be a flag that silently does nothing, the shape of #118.
func TestValidateDocScopeNeedsRequire(t *testing.T) {
	for _, args := range [][]string{
		{"validate", "--doc", "1"},
		{"validate", "--all-docs"},
	} {
		_, err := execute(t, twoDocs, args...)
		if got := exitCode(err, io.Discard); got != 3 {
			t.Fatalf("%v: want exit 3, got %d (%v)", args, got, err)
		}
	}
}

func TestValidateDocAndAllDocsConflict(t *testing.T) {
	_, err := execute(t, twoDocs, "validate", "--doc", "1", "--all-docs", "--require", ".a")
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("want exit 3 for --doc with --all-docs, got %d (%v)", got, err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
