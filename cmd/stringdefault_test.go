package cmd

import (
	"io"
	"strings"
	"testing"
)

// literalDefaults are the --default values that mean one thing to a caller and
// another to a YAML parser. Every one of these was observed on 0.21.0 while
// using the tool on real manifests, which is how #232 was found: a placeholder
// is the usual thing to hand --default, and `-` — the most likely placeholder
// of all — parses as a sequence containing null, so the run printed "- null"
// at exit 0 with nothing on stderr.
//
// The two that used to be usage errors (`*`, `:`) are in here too: under -s
// nothing is parsed, so there is no longer anything to fail on.
var literalDefaults = []struct {
	name string
	give string
	why  string
}{
	{"dash", "-", "a sequence containing null"},
	{"comment", "#none", "a comment, so the value vanished"},
	{"tilde", "~", "YAML null"},
	{"null", "null", "YAML null"},
	{"padded", "08", "a number, so the leading zero was stripped"},
	{"version", "1.0", "a float, so the trailing zero was dropped"},
	{"date", "2026-01-01", "a timestamp, normalized to RFC3339"},
	{"star", "*", "an alias, which failed to parse at all"},
	{"colon", ":", "a mapping key, which failed to parse at all"},
}

// TestStringDefaultIsTakenLiterally is #232. -s/--string says the --default is
// text, reusing the flag set/append already carry for the same question about
// their <value>. Driven off the live tree so a fifth read verb inherits it.
//
// The inspect verbs print a default as given rather than inspected (#213), so
// the expected output is the same for all four: whatever was passed.
func TestStringDefaultIsTakenLiterally(t *testing.T) {
	for _, c := range readVerbs() {
		name := commandName(c)
		for _, tc := range literalDefaults {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				args := invocation(name, []string{"-o", "raw", "-s", "--default", tc.give}, []string{".nope"})
				out, err := execute(t, doc, args...)
				if err != nil {
					t.Fatalf("%v: want a successful run, got %v", args, err)
				}
				if got := strings.TrimSuffix(out, "\n"); got != tc.give {
					t.Errorf("%v: want the default verbatim (%q), got %q — without -s it is %s",
						args, tc.give, got, tc.why)
				}
			})
		}
	}
}

// TestDefaultWithoutStringStillParsesAsYAML keeps the existing reading intact:
// -s is opt-in, so a default that was a list or a mapping still is one. This
// is the half that makes #232 additive rather than breaking.
func TestDefaultWithoutStringStillParsesAsYAML(t *testing.T) {
	out, err := execute(t, doc, "-o", "json", "--default", "[1, 2]", ".nope")
	if err != nil {
		t.Fatalf("want a successful run, got %v", err)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Fatalf("want the default parsed as a list, got %q", out)
	}
	if strings.Contains(out, `"[1, 2]"`) {
		t.Fatalf("--default was taken literally without -s: %q", out)
	}
}

// TestStringDefaultEmitsAJSONString pins what -s means for a rendered format:
// the default is text, so -o json quotes it rather than emitting the value the
// text would have parsed to.
func TestStringDefaultEmitsAJSONString(t *testing.T) {
	out, err := execute(t, doc, "-o", "json", "-s", "--default", "-", ".nope")
	if err != nil {
		t.Fatalf("want a successful run, got %v", err)
	}
	if !strings.Contains(out, `"-"`) {
		t.Fatalf("want a JSON string, got %q", out)
	}
}

// TestStringWithoutDefaultIsAUsageError: -s on a read verb has nothing to
// apply to unless --default was given — the same shape as --diff-format
// without --diff (#118), which is why it gets a flagConflicts row too.
func TestStringWithoutDefaultIsAUsageError(t *testing.T) {
	for _, c := range readVerbs() {
		name := commandName(c)
		t.Run(name, func(t *testing.T) {
			args := invocation(name, []string{"-s"}, []string{".meta.tags"})
			_, err := execute(t, doc, args...)
			if code := exitCode(err, io.Discard); code != 3 {
				t.Fatalf("%v: want exit 3 (usage), got %d (%v)", args, code, err)
			}
			if msg := err.Error(); !strings.Contains(msg, "--default") {
				t.Errorf("want a message naming --default, got %q", msg)
			}
		})
	}
}

// TestEditVerbsKeepTheirOwnString guards the half that is easy to break by
// sharing a name: -s on set/append is about <value> and is valid on its own,
// so the read verbs' new check must not leak into them.
func TestEditVerbsKeepTheirOwnString(t *testing.T) {
	out, err := execute(t, doc, "set", "-s", ".meta.name", "123")
	if err != nil {
		t.Fatalf("set -s without --default must stay valid, got %v", err)
	}
	if !strings.Contains(out, `"123"`) {
		t.Fatalf("want the value quoted as a string, got %q", out)
	}
}
