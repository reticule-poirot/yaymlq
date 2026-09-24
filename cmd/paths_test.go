package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// workflow mirrors the shape #133 was found on: the same key buried under
// several jobs, each at a different list index.
const workflow = `
jobs:
  lint:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
  test:
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
"odd.key":
  go-version: "1.26"
`

// TestGetPathsListsResolvedPaths is the flag's reason to exist: the values
// alone said four things matched but never where, so there was no way to
// get from "find" to "edit".
func TestGetPathsListsResolvedPaths(t *testing.T) {
	got, err := execute(t, workflow, "--paths", ".jobs.*.steps[*].with.go-version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "jobs.lint.steps[1].with.go-version\njobs.test.steps[0].with.go-version\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestGetPathsRoundTripsIntoApply closes the loop the issue said was open:
// the path list is fed straight back in as an apply script.
func TestGetPathsRoundTripsIntoApply(t *testing.T) {
	listed, err := execute(t, workflow, "--paths", ".jobs.*.steps[*].with.go-version")
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}

	var script strings.Builder
	for _, p := range strings.Fields(listed) {
		fmt.Fprintf(&script, "set %s = \"1.27\"\n", p)
	}
	f := writeScript(t, t.TempDir(), script.String())

	got, err := execute(t, workflow, "apply", "-f", f)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if strings.Contains(got, `"1.26"`) && strings.Count(got, `"1.26"`) != 1 {
		t.Fatalf("expected only the untouched odd.key copy to remain at 1.26:\n%s", got)
	}
	if n := strings.Count(got, `"1.27"`); n != 2 {
		t.Fatalf("expected 2 bumped versions, got %d:\n%s", n, got)
	}
}

// TestGetPathsQuotesAmbiguousKeys: a listed path has to parse back to the
// same key, so one containing a dot comes out quoted.
func TestGetPathsQuotesAmbiguousKeys(t *testing.T) {
	got, err := execute(t, workflow, "--paths", ".*.go-version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `"odd.key".go-version` {
		t.Fatalf("got %q, want %q", got, `"odd.key".go-version`)
	}
}

// TestGetPathsOnNonWildcardEchoesTheNormalizedPath: no wildcard means one
// match, whose path is the query itself in canonical form.
func TestGetPathsOnNonWildcardEchoesTheNormalizedPath(t *testing.T) {
	got, err := execute(t, doc, "--paths", ".meta.tags.1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "meta.tags[1]" {
		t.Fatalf("got %q, want %q", got, "meta.tags[1]")
	}
}

// TestGetPathsRootIsDot: the whole document has a path too.
func TestGetPathsRootIsDot(t *testing.T) {
	got, err := execute(t, doc, "--paths", ".")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "." {
		t.Fatalf("got %q, want %q", got, ".")
	}
}

// TestGetPathsWithPrint0 is the xargs -0 pairing.
func TestGetPathsWithPrint0(t *testing.T) {
	got, err := execute(t, workflow, "--paths", "-0", ".jobs.*.steps[*].with.go-version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "jobs.lint.steps[1].with.go-version\x00jobs.test.steps[0].with.go-version"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestGetPathsConflictsWithStructuredOutput: a path is text, not a value, so
// --paths forces raw the same way --print0 does and rejects an -o that
// contradicts it.
func TestGetPathsConflictsWithStructuredOutput(t *testing.T) {
	wantExit(t, doc, 3, "--paths", "-o", "json", ".meta.name")
	wantExit(t, doc, 3, "-o", "yaml", "--paths", ".meta.name")
	if _, err := execute(t, doc, "--paths", "-o", "raw", ".meta.name"); err != nil {
		t.Errorf("--paths -o raw: %v", err)
	}
}

// TestGetPathsWithDefaultIsUsageError: --default supplies a value to print
// when nothing matched, and a value that isn't in the document has no path.
func TestGetPathsWithDefaultIsUsageError(t *testing.T) {
	wantExit(t, doc, 3, "--paths", "--default", "x", ".missing")
}

// TestGetPathsNoMatchStillReportsNoMatch: --paths changes what is printed,
// not when a query has failed.
func TestGetPathsNoMatchStillReportsNoMatch(t *testing.T) {
	wantExit(t, doc, 1, "--paths", ".missing")
	wantExit(t, doc, 1, "--paths", "-e", ".missing")
	out, _ := execute(t, doc, "--paths", "-e", ".missing")
	if out != "" {
		t.Errorf("-e miss printed %q, want nothing", out)
	}
}

// TestGetPathsAcrossAllDocs keeps --all-docs working; the paths are
// per-document, so a caller pairing them with --doc has to track which is
// which itself.
func TestGetPathsAcrossAllDocs(t *testing.T) {
	got, err := execute(t, doc, "--paths", "--all-docs", ".meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "meta.name\nmeta.name\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestPathsRoundTripSurvivesASeparatorInAKey is #158 end to end: the apply
// script format splits an op on " = ", so a key containing one used to be
// cut in half — and the truncated path still parsed, so the edit landed on a
// newly created key while the real one kept its old value, at exit 0.
func TestPathsRoundTripSurvivesASeparatorInAKey(t *testing.T) {
	const src = "a = b: old\nz: 1\n"

	listed, err := execute(t, src, "--paths", ".*")
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	if !strings.Contains(listed, `"a = b"`) {
		t.Fatalf("path holding the separator was not quoted: %q", listed)
	}

	var script strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(listed), "\n") {
		fmt.Fprintf(&script, "set %s = new\n", line)
	}
	f := writeScript(t, t.TempDir(), script.String())

	got, err := execute(t, src, "apply", "-f", f)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(got, "a = b: new") {
		t.Errorf("the real key was not edited:\n%s", got)
	}
	if strings.Contains(got, "\na: ") {
		t.Errorf("a bogus key was created instead:\n%s", got)
	}
}

// TestPathsRoundTripSurvivesAwkwardKeys walks every key shape that needs
// quoting through the whole advertised pipeline — list the paths, build an
// apply script from them, run it — and asserts each key kept its identity.
// Each of these is a way the rendered path can be misread by either grammar
// it passes through: the path parser's (a dot, a bracket, a quote, a bare
// number or star) or the script's (whitespace, which splits an op).
func TestPathsRoundTripSurvivesAwkwardKeys(t *testing.T) {
	src := strings.Join([]string{
		`"a = b": old`,
		`"a=b": old`,
		`"two words": old`,
		`"odd.key": old`,
		`"7": old`,
		`"*": old`,
		`"say \"hi\"": old`,
		`"it's": old`,
		`"": old`,
		`plain: old`,
	}, "\n") + "\n"

	listed, err := execute(t, src, "--paths", ".*")
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	paths := strings.Split(strings.TrimSpace(listed), "\n")
	if len(paths) != 10 {
		t.Fatalf("listed %d paths, want 10: %q", len(paths), listed)
	}

	var script strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&script, "set %s = new\n", p)
	}
	f := writeScript(t, t.TempDir(), script.String())

	got, err := execute(t, src, "apply", "-f", f)
	if err != nil {
		t.Fatalf("apply:\n%s\n%v", script.String(), err)
	}
	if strings.Contains(got, "old") {
		t.Errorf("a key kept its old value, so its path missed:\n%s\nscript:\n%s", got, script.String())
	}
	if n := strings.Count(got, "new"); n != 10 {
		t.Errorf("want 10 edited keys, got %d — a path landed somewhere new:\n%s", n, got)
	}
}
