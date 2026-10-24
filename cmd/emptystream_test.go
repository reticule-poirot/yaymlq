package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An empty stream and a comment-only one are the same thing to yaml.v3: both
// decode to zero documents. Neither is a syntax error — there is nothing
// malformed about a file that holds only a comment header.
var emptyStreams = map[string]string{
	"empty":        "",
	"comment only": "# just a comment\n",
	"blank lines":  "\n\n",
}

// TestEmptyStreamIsNotAParseError is #209. Zero documents used to be
// parseErr — exit 2, the class that means the input could not be parsed —
// everywhere it was reported, and validate failed outright on a stream it
// had nothing to complain about, contradicting its own "checks syntax only"
// contract.
//
// A stream with no documents is well-formed YAML with nothing in it. validate
// accepts it; every verb that needs a document to work on reports that it has
// none, in the exit-1 family that already covers "the path didn't resolve".
func TestEmptyStreamIsNotAParseError(t *testing.T) {
	for name, in := range emptyStreams {
		t.Run(name, func(t *testing.T) {
			for _, c := range conflictCommands() {
				verb := commandName(c)
				if verb == "validate" || verb == "schema" {
					continue // covered below, and schema reads no input
				}
				base, ok := minimalArgs(t, verb)
				if !ok {
					t.Skip("no minimalArgs entry; TestMinimalArgsCoversEveryCommand reports this")
				}
				args := invocation(verb, base)
				_, err := execute(t, in, args...)
				if got := exitCode(err, io.Discard); got != 1 {
					t.Errorf("%v on an %s stream -> exit %d (%v), want 1", args, name, got, err)
				}
			}
		})
	}
}

// TestValidateAcceptsAnEmptyStream is the other half: syntax only means
// syntax only.
func TestValidateAcceptsAnEmptyStream(t *testing.T) {
	for name, in := range emptyStreams {
		t.Run(name, func(t *testing.T) {
			out, err := execute(t, in, "validate")
			if err != nil {
				t.Fatalf("validate on an %s stream: %v", name, err)
			}
			if out != "" {
				t.Errorf("want no output, got %q", out)
			}
		})
	}
}

// TestValidateRequireFailsOnAnEmptyStream: accepting the syntax does not mean
// satisfying a requirement. A stream with no documents has no document to
// hold the path — including under --all-docs, where "every document has it"
// would otherwise be vacuously true over zero documents and pass where the
// laxer default fails.
func TestValidateRequireFailsOnAnEmptyStream(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"default scope", []string{"validate", "--require", ".a"}},
		{"--all-docs", []string{"validate", "--all-docs", "--require", ".a"}},
		{"--doc 0", []string{"validate", "--doc", "0", "--require", ".a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, "", tc.args...)
			if err == nil {
				t.Fatal("want a failure: an empty stream satisfies no requirement")
			}
			if got := exitCode(err, io.Discard); got != 1 {
				t.Errorf("exit %d (%v), want 1", got, err)
			}
		})
	}
}

// TestValidateEmptyFileAmongOthers: an empty file is not a failure the way a
// malformed one is, and validate still reports every source it was given.
func TestValidateEmptyFileAmongOthers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	empty := write("empty.yaml", "# header only\n")
	good := write("good.yaml", "a: 1\n")
	bad := write("bad.yaml", "a: [\n")

	if _, err := execute(t, "", "validate", empty, good); err != nil {
		t.Errorf("an empty file alongside a good one should pass: %v", err)
	}

	out, err := execute(t, "", "validate", empty, bad)
	if err == nil {
		t.Fatal("want a failure from the malformed file")
	}
	if strings.Contains(out+err.Error(), "empty.yaml") {
		t.Errorf("the empty file should not be reported as a failure:\n%s\n%v", out, err)
	}
}
