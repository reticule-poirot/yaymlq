package cmd

import (
	"strings"
	"testing"
)

// TestUnknownTagWarnsOnRead is A5: an unknown tag used to vanish from a read
// with nothing to say so. Now get/keys/len/type note it on stderr, naming
// the tag and the line it was first seen on, without touching stdout or the
// exit code.
func TestUnknownTagWarnsOnRead(t *testing.T) {
	const src = "a: !Ref foo\nb: 2\n"

	out, errOut, err := executeSplit(t, src, ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "foo" {
		t.Errorf("stdout changed: %q", out)
	}
	if !strings.Contains(errOut, "!Ref") || !strings.Contains(errOut, "line 1") {
		t.Errorf("stderr does not name the dropped tag: %q", errOut)
	}
}

// TestUnknownTagWarningOnlyFiresForAPrintedValue: a tagged node elsewhere in
// the document, outside what this query actually printed, is not this
// query's business to report — that would fire on every query of a
// CloudFormation-shaped file regardless of what it asked for.
func TestUnknownTagWarningOnlyFiresForAPrintedValue(t *testing.T) {
	const src = "a: !Ref foo\nb: 2\n"

	_, errOut, err := executeSplit(t, src, ".b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut != "" {
		t.Errorf("warned about a tag outside the printed value: %q", errOut)
	}
}

// TestUnknownTagWarningRespectsQuiet: -q suppresses the note along with
// every other output, the same as the --paths/--all-docs note does.
func TestUnknownTagWarningRespectsQuiet(t *testing.T) {
	const src = "a: !Ref foo\n"

	_, errOut, err := executeSplit(t, src, "-q", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut != "" {
		t.Errorf("-q did not suppress the note: %q", errOut)
	}
}

// TestUnknownTagWarningNamesEachDistinctTagOnce: several nodes under the
// same tag note that tag once, at its first line; a different tag gets its
// own entry.
func TestUnknownTagWarningNamesEachDistinctTagOnce(t *testing.T) {
	const src = "a:\n  x: !Ref one\n  y: !Ref two\n  z: !GetAtt three\n"

	_, errOut, err := executeSplit(t, src, ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if n := strings.Count(errOut, "!Ref"); n != 1 {
		t.Errorf("want !Ref named once, got %d times: %q", n, errOut)
	}
	if !strings.Contains(errOut, "!GetAtt") {
		t.Errorf("want !GetAtt named too: %q", errOut)
	}
}

// TestKnownTagsDoNotWarn: the core YAML tags, which yaml.v3 resolves to a
// concrete Go type without losing anything, are not "unknown" — only a tag
// outside that set (merge keys and the non-specific "!" included) is.
func TestKnownTagsDoNotWarn(t *testing.T) {
	const src = "base: &base {x: 1}\na: !!str 5\nb: hi\nm:\n  <<: *base\n"

	_, errOut, err := executeSplit(t, src, ".")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut != "" {
		t.Errorf("warned about a core tag or a merge key: %q", errOut)
	}
}

// TestBinaryTagWarns: !!binary decodes to bytes rather than round-tripping
// its source spelling, so it gets the same note any other unknown tag does.
func TestBinaryTagWarns(t *testing.T) {
	const src = "a: !!binary aGVsbG8=\n"

	_, errOut, err := executeSplit(t, src, ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(errOut, "!!binary") {
		t.Errorf("want !!binary named: %q", errOut)
	}
}
