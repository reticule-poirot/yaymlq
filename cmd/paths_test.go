package cmd

import (
	"bytes"
	"encoding/json"
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

// TestGetPathsRejectsARenderedOutputFormat: a path is text, not a value, so
// --paths forces raw the same way --print0 does and rejects an -o that would
// render it as a value. -o json is the one exception, since it carries the
// document index rather than rendering the path as a JSON value (#159).
func TestGetPathsRejectsARenderedOutputFormat(t *testing.T) {
	wantExit(t, doc, 3, "-o", "yaml", "--paths", ".meta.name")
	for _, args := range [][]string{
		{"--paths", "-o", "raw", ".meta.name"},
		{"--paths", "-o", "json", ".meta.name"},
	} {
		if _, err := execute(t, doc, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
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
	out, _, err := executeSplit(t, doc, "--paths", "--all-docs", ".meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "meta.name\nmeta.name\n"; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// executeSplit is execute with stdout and stderr kept apart, for the cases
// that assert on a note written to stderr without it landing in the output
// being checked.
func executeSplit(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := NewRootCommand()
	var out, errBuf bytes.Buffer
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&errBuf)
	c.SetArgs(args)
	err = c.Execute()
	return out.String(), errBuf.String(), err
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

// TestPathsRoundTripSurvivesALineBreakInAKey is #157 end to end. A key
// holding a newline had no single-line rendering, so --paths emitted it as
// two lines and an apply script built from that output created two keys that
// didn't exist while leaving the real one untouched, at exit 0.
func TestPathsRoundTripSurvivesALineBreakInAKey(t *testing.T) {
	// ? "a\nb" : old — a real newline inside the key, not the two
	// characters backslash-n.
	const src = "? \"a\\nb\"\n: old\nz: 1\n"

	listed, err := execute(t, src, "--paths", ".*")
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) != 2 {
		t.Fatalf("a two-key document listed %d lines: %q", len(lines), listed)
	}
	if !strings.Contains(listed, `"a\nb"`) {
		t.Fatalf("the newline was not escaped: %q", listed)
	}

	var script strings.Builder
	for _, p := range lines {
		fmt.Fprintf(&script, "set %s = new\n", p)
	}
	f := writeScript(t, t.TempDir(), script.String())

	got, err := execute(t, src, "apply", "-f", f)
	if err != nil {
		t.Fatalf("apply:\n%s\n%v", script.String(), err)
	}
	if strings.Contains(got, "old") {
		t.Errorf("the real key kept its value, so the path missed:\n%s", got)
	}
	if n := strings.Count(got, "new"); n != 2 {
		t.Errorf("want 2 edited keys, got %d — a path landed somewhere new:\n%s", n, got)
	}
}

// TestPathsRoundTripSurvivesBothQuoteCharacters: the other key #157 couldn't
// express at all.
func TestPathsRoundTripSurvivesBothQuoteCharacters(t *testing.T) {
	const src = "\"it's \\\"x\\\"\": old\n"

	listed, err := execute(t, src, "--paths", ".*")
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	f := writeScript(t, t.TempDir(), "set "+strings.TrimSpace(listed)+" = new\n")

	got, err := execute(t, src, "apply", "-f", f)
	if err != nil {
		t.Fatalf("apply (path %q): %v", strings.TrimSpace(listed), err)
	}
	if strings.Contains(got, "old") || !strings.Contains(got, "new") {
		t.Errorf("edit did not land on the original key:\n%s", got)
	}
}

// TestPathsJSONCarriesTheDocumentIndex is #159's read half: a path alone
// can't say which document it came from, so across a stream the listing was
// ambiguous and a script built from it edited document 0 over and over.
func TestPathsJSONCarriesTheDocumentIndex(t *testing.T) {
	const stream = "kind: A\nimage: nginx:1.0\n---\nkind: B\nimage: nginx:2.0\n"

	got, err := execute(t, stream, "--paths", "--all-docs", "-o", "json", ".image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one object per match, got %q", got)
	}
	for i, line := range lines {
		var p struct {
			Doc  int    `json:"doc"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("line %d is not one JSON object: %v (%q)", i, err, line)
		}
		if p.Doc != i {
			t.Errorf("line %d: doc = %d, want %d", i, p.Doc, i)
		}
		if p.Path != "image" {
			t.Errorf("line %d: path = %q, want %q", i, p.Path, "image")
		}
	}
}

// TestPathsJSONIncludesDocWithoutAllDocs: the field is always there, so a
// consumer parses one shape — and its value is exactly what to pass to --doc.
func TestPathsJSONIncludesDocWithoutAllDocs(t *testing.T) {
	got, err := execute(t, doc, "--paths", "-o", "json", "--doc", "1", ".meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `{"doc":1,"path":"meta.name"}` {
		t.Fatalf("got %q", strings.TrimSpace(got))
	}
}

// TestPathsJSONIsOneCompactObjectPerLine: line-oriented like the text form,
// not a pretty-printed blob, so it can be read a line at a time.
func TestPathsJSONIsOneCompactObjectPerLine(t *testing.T) {
	got, err := execute(t, workflow, "--paths", "-o", "json", ".jobs.*.steps[*].with.go-version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if !strings.HasPrefix(line, `{"doc":`) || !strings.HasSuffix(line, "}") {
			t.Errorf("line is not one compact object: %q", line)
		}
	}
	if n := strings.Count(got, "\n"); n != 2 {
		t.Errorf("want 2 lines for 2 matches, got %d:\n%s", n, got)
	}
}

// TestPathsJSONPathFieldIsAUsablePath: the path travels as a JSON string, so
// a key needing quotes or escapes survives two layers of quoting intact.
func TestPathsJSONPathFieldIsAUsablePath(t *testing.T) {
	got, err := execute(t, "? \"a\\nb\"\n: v\n", "--paths", "-o", "json", ".*")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &p); err != nil {
		t.Fatalf("not one JSON object: %v (%q)", err, got)
	}
	if p.Path != `"a\nb"` {
		t.Fatalf("path = %q, want %q", p.Path, `"a\nb"`)
	}
	// And it resolves when handed straight back.
	back, err := execute(t, "? \"a\\nb\"\n: v\n", "-o", "raw", p.Path)
	if err != nil {
		t.Fatalf("the listed path did not resolve: %v", err)
	}
	if strings.TrimSpace(back) != "v" {
		t.Errorf("resolved to %q, want %q", back, "v")
	}
}

// TestPathsTextUnderAllDocsWarns: text mode still can't say which document a
// path came from, and the whole point of #159 is that the ambiguity was
// silent. The warning goes to stderr so stdout stays pipeable and the exit
// code is unchanged.
func TestPathsTextUnderAllDocsWarns(t *testing.T) {
	const stream = "image: a\n---\nimage: b\n"

	out, errOut, err := executeSplit(t, stream, "--paths", "--all-docs", ".image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "image\nimage\n" {
		t.Errorf("stdout changed: %q", out)
	}
	if !strings.Contains(errOut, "--all-docs") || !strings.Contains(errOut, "-o json") {
		t.Errorf("stderr does not point at the unambiguous form: %q", errOut)
	}

	// One document is unambiguous, so there is nothing to note.
	if _, errOut, err = executeSplit(t, stream, "--paths", ".image"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut != "" {
		t.Errorf("noted an ambiguity for a single-document listing: %q", errOut)
	}

	// Nor is there under -o json, which carries the index.
	if _, errOut, err = executeSplit(t, stream, "--paths", "--all-docs", "-o", "json", ".image"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut != "" {
		t.Errorf("noted an ambiguity that -o json resolves: %q", errOut)
	}
}

// TestPathsJSONStillRejectsYAMLAndPrint0: json is the one -o that --paths
// accepts, since it is the only one that can carry the document index.
func TestPathsJSONStillRejectsYAMLAndPrint0(t *testing.T) {
	wantExit(t, doc, 3, "--paths", "-o", "yaml", ".meta.name")
	wantExit(t, doc, 3, "--paths", "-0", "-o", "json", ".meta.name")
}

// TestPathsJSONUnderQuietPrintsNothing: -q is a presence check either way.
func TestPathsJSONUnderQuietPrintsNothing(t *testing.T) {
	got, err := execute(t, doc, "--paths", "-o", "json", "-q", ".meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "" {
		t.Errorf("printed %q, want nothing", got)
	}
}
