package cmd

import (
	"strings"
	"testing"
)

// Two services, both with an image: a wildcard over them matches, a wildcard
// over a key they don't have matches nothing without that being an error.
const wcDoc = "services:\n" +
	"  web:\n    image: nginx:1.27\n" +
	"  db:\n    image: postgres:16\n"

// TestWildcardMatchingNothingSaysSo is #173: a non-wildcard miss is an error
// carrying the keys that were there, but the same path with a wildcard in it
// printed nothing and exited 0, so a typo or a wildcard at the wrong depth
// was indistinguishable from "this document genuinely has none of those".
func TestWildcardMatchingNothingSaysSo(t *testing.T) {
	for name, expr := range map[string]string{
		"typo'd leaf under a wildcard": ".services.*.nope",
		"wildcard at the wrong depth":  ".*.image",
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := executeSplit(t, wcDoc, expr)
			if err != nil {
				t.Fatalf("a wildcard matching nothing must stay exit 0: %v", err)
			}
			if stdout != "" {
				t.Errorf("stdout must stay empty, got %q", stdout)
			}
			if !strings.Contains(stderr, "matched nothing") {
				t.Errorf("want a note on stderr saying nothing matched, got %q", stderr)
			}
			// The note has to name the way to turn this into exit 1, since
			// that is the whole reason a caller couldn't detect it before.
			if !strings.Contains(stderr, "-e") {
				t.Errorf("want the note to point at -e, got %q", stderr)
			}
		})
	}
}

// TestWildcardThatMatchesStaysSilent: the note fires on zero *total* matches,
// never as commentary on a successful query.
func TestWildcardThatMatchesStaysSilent(t *testing.T) {
	stdout, stderr, err := executeSplit(t, wcDoc, ".services.*.image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if n := strings.Count(stdout, "\n"); n != 2 {
		t.Errorf("want both images, got %q", stdout)
	}
	if stderr != "" {
		t.Errorf("no note is due when the query matched: %q", stderr)
	}
}

// TestPartialWildcardMatchStaysSilent guards the behavior #173 says must not
// change: once a wildcard has matched, a missing key on an individual branch
// is skipped rather than reported, which is what makes `services.*.image`
// usefully return only the services that set one.
func TestPartialWildcardMatchStaysSilent(t *testing.T) {
	in := "services:\n  web:\n    image: nginx:1.27\n  db:\n    ports: []\n"
	stdout, stderr, err := executeSplit(t, in, ".services.*.image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "nginx:1.27" {
		t.Errorf("want the one service that has an image, got %q", stdout)
	}
	if stderr != "" {
		t.Errorf("a partial match is not a miss: %q", stderr)
	}
}

// TestNonWildcardMissIsStillAnError: the note is for the gap, not a
// replacement for the error that already works.
func TestNonWildcardMissIsStillAnError(t *testing.T) {
	_, _, err := executeSplit(t, wcDoc, ".services.web.nope")
	if err == nil {
		t.Fatal("a concrete path that misses is still an error")
	}
	if code := codeFor(err); code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
	if msg := annotateNotFound(err, defaultMaxSuggestions).Error(); !strings.Contains(msg, "image") {
		t.Errorf("want the available keys still offered, got %q", msg)
	}
}

// TestNoteSuppressedWhenTheCallerOptedIntoAMiss: -e and -q already report a
// miss through the exit code, and --default says a miss is expected. Printing
// a note in those cases is unsolicited commentary, and under -q it would
// break the silence contract outright.
func TestNoteSuppressedWhenTheCallerOptedIntoAMiss(t *testing.T) {
	for name, args := range map[string][]string{
		"-e":        {"-e", ".services.*.nope"},
		"-q":        {"-q", ".services.*.nope"},
		"--default": {"--default", "none", ".services.*.nope"},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, _ := executeSplit(t, wcDoc, args...)
			if stderr != "" {
				t.Errorf("want no note when the caller opted in, got %q", stderr)
			}
		})
	}
}

// TestInspectVerbsSayItToo: #173 names keys/len/type as behaving the same way,
// so the fix has to reach them and not just root/get.
func TestInspectVerbsSayItToo(t *testing.T) {
	for _, verb := range []string{"keys", "len", "type"} {
		t.Run(verb, func(t *testing.T) {
			stdout, stderr, err := executeSplit(t, wcDoc, verb, ".services.*.nope")
			if err != nil {
				t.Fatalf("want exit 0, got %v", err)
			}
			if stdout != "" {
				t.Errorf("stdout must stay empty, got %q", stdout)
			}
			if !strings.Contains(stderr, "matched nothing") {
				t.Errorf("want the note, got %q", stderr)
			}
		})
	}
}

// TestWildcardNoteFiresOnceAcrossAStream: the note describes the run, not each
// document, so --all-docs over three documents still prints it once.
func TestWildcardNoteFiresOnceAcrossAStream(t *testing.T) {
	in := "a: 1\n---\nb: 2\n---\nc: 3\n"
	_, stderr, err := executeSplit(t, in, "--all-docs", ".*.nope")
	if err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	if n := strings.Count(stderr, "matched nothing"); n != 1 {
		t.Errorf("want exactly one note, got %d:\n%s", n, stderr)
	}
}

// TestWildcardNoteUnderPaths: --paths is the case the issue calls out as
// interacting badly — a listing that matches nothing feeds apply an empty
// script and the whole pipeline is a successful no-op.
func TestWildcardNoteUnderPaths(t *testing.T) {
	stdout, stderr, err := executeSplit(t, wcDoc, "--paths", ".services.*.nope")
	if err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout must stay empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "matched nothing") {
		t.Errorf("want the note, got %q", stderr)
	}
}
