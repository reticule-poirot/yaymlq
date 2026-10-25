package cmd

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// readVerbs returns the commands that read without editing: get and the three
// inspect verbs. --max-suggestions marks a verb that resolves a path,
// --in-place marks an editing one.
func readVerbs() []*cobra.Command {
	var out []*cobra.Command
	for _, c := range conflictCommands() {
		if c.Flags().Lookup("max-suggestions") == nil || c.Flags().Lookup("in-place") != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

// TestReadVerbsOfferTheSoftMissFlags is #213. -e, -q and --default are the
// three ways to say a miss is expected, and the root verb offered all three
// while keys/len/type offered none — so a script that doesn't know a
// document's shape, which is exactly when it reaches for these verbs, had to
// discard stderr or parse the document twice.
//
// Driven off the live tree, so a fifth read verb inherits the requirement.
func TestReadVerbsOfferTheSoftMissFlags(t *testing.T) {
	seen := 0
	for _, c := range readVerbs() {
		name := commandName(c)
		for _, f := range []string{"exit-status", "quiet", "default"} {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s does not offer --%s", name, f)
			}
		}
		seen++
	}
	if seen != 4 {
		t.Errorf("want get, keys, len and type, got %d read verbs", seen)
	}
}

// inspectVerbs is the three verbs built by newInspectCommand, with a path
// that resolves in doc and one that doesn't.
var inspectVerbs = []struct {
	verb string
	hit  string // the output for .meta.tags
}{
	{"keys", "0\n1\n2\n"},
	{"len", "3\n"},
	{"type", "array\n"},
}

func TestInspectQuietHitIsSilentAndSucceeds(t *testing.T) {
	for _, tc := range inspectVerbs {
		t.Run(tc.verb, func(t *testing.T) {
			out, err := execute(t, doc, tc.verb, "-q", ".meta.tags")
			if err != nil {
				t.Fatalf("want nil error on a match, got %v", err)
			}
			if out != "" {
				t.Fatalf("want no output under -q, got %q", out)
			}
		})
	}
}

func TestInspectQuietMissExitsOneSilently(t *testing.T) {
	for _, tc := range inspectVerbs {
		t.Run(tc.verb, func(t *testing.T) {
			out, err := execute(t, doc, tc.verb, "-q", ".meta.missing")
			if out != "" {
				t.Fatalf("want no output under -q, got %q", out)
			}
			var se silentExit
			if !errors.As(err, &se) || se.code != 1 {
				t.Fatalf("want silentExit{1}, got %v", err)
			}
		})
	}
}

func TestInspectExitStatusMissIsSilentExitOne(t *testing.T) {
	for _, tc := range inspectVerbs {
		t.Run(tc.verb, func(t *testing.T) {
			out, err := execute(t, doc, tc.verb, "-e", ".meta.missing")
			if out != "" {
				t.Fatalf("want no output under -e, got %q", out)
			}
			if code := exitCode(err, io.Discard); code != 1 {
				t.Fatalf("want exit 1, got %d (%v)", code, err)
			}
		})
	}
}

func TestInspectExitStatusHitStillPrints(t *testing.T) {
	for _, tc := range inspectVerbs {
		t.Run(tc.verb, func(t *testing.T) {
			out, err := execute(t, doc, tc.verb, "-e", ".meta.tags")
			if err != nil {
				t.Fatalf("want nil error on a match, got %v", err)
			}
			if out != tc.hit {
				t.Fatalf("want %q, got %q", tc.hit, out)
			}
		})
	}
}

// TestInspectDefaultIsPrintedAsGivenNotInspected is the decision in #213 that
// needed stating rather than assuming: the default stands in for the answer,
// not for the value the verb would have been handed. `len --default 0`
// answers 0 for a path that isn't there; it does not measure the 0.
func TestInspectDefaultIsPrintedAsGivenNotInspected(t *testing.T) {
	for name, want := range map[string]string{
		"keys": "none\n",
		"len":  "none\n",
		"type": "none\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := execute(t, doc, name, "--default", "none", ".meta.missing")
			if err != nil {
				t.Fatalf("want a successful run, got %v", err)
			}
			if out != want {
				t.Fatalf("want the default as given (%q), got %q", want, out)
			}
		})
	}
}

// TestInspectDefaultCountsAsAMatch keeps #208's rule for these verbs too:
// asking for a default says a miss is expected and names the answer, so the
// run succeeded and there is nothing left for -e to report.
func TestInspectDefaultCountsAsAMatch(t *testing.T) {
	out, err := execute(t, doc, "len", "-e", "--default", "0", ".meta.missing")
	if err != nil {
		t.Fatalf("want exit 0 with -e --default, got %v", err)
	}
	if out != "0\n" {
		t.Fatalf("want the default on stdout, got %q", out)
	}
}

// TestInspectDefaultFillsPerDocumentUnderAllDocs mirrors get's own case: the
// second document in doc has no meta.tags.
func TestInspectDefaultFillsPerDocumentUnderAllDocs(t *testing.T) {
	out, err := execute(t, doc, "len", "--all-docs", "--default", "0", ".meta.tags")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "3\n0\n" {
		t.Fatalf("want the length then the default, got %q", out)
	}
}

// TestInspectQuietStillFailsOnATypeError holds the line runInspect already
// drew: a path that resolved and then turned out to be the wrong kind of
// thing is a question the verb can't answer, not a missing path, so -q
// doesn't turn it into a quiet exit 1-as-no-match or hide it.
func TestInspectQuietStillFailsOnATypeError(t *testing.T) {
	out, err := execute(t, "flag: true\n", "len", "-q", ".flag")
	if err == nil {
		t.Fatalf("want a failure for len of a boolean, got output %q", out)
	}
	var se silentExit
	if errors.As(err, &se) {
		t.Fatalf("want the type error reported, got a silent exit: %v", err)
	}
	if !strings.Contains(err.Error(), "no length") {
		t.Errorf("want the type error's message, got %q", err)
	}
}

// TestInspectQuietCountsAnEmptyMappingAsAMatch separates "the path resolved
// and there was nothing in it" from "the path didn't resolve". keys of an
// empty mapping emits nothing, which must not read as a miss.
func TestInspectQuietCountsAnEmptyMappingAsAMatch(t *testing.T) {
	out, err := execute(t, "empty: {}\n", "keys", "-q", ".empty")
	if err != nil {
		t.Fatalf("want exit 0 for a path that resolved, got %v", err)
	}
	if out != "" {
		t.Fatalf("want no output under -q, got %q", out)
	}
}

func TestInspectBadDefaultValueIsAUsageError(t *testing.T) {
	_, err := execute(t, doc, "len", "--default", "{", ".meta.missing")
	if code := exitCode(err, io.Discard); code != 3 {
		t.Fatalf("want exit 3 for an unparseable --default, got %d (%v)", code, err)
	}
}
