package cmd

import (
	"strings"
	"testing"
)

// TestYAMLVersionDirectiveNamesTheCause: a version with no 1.x reading fails in
// yaml.v4 with "found incompatible YAML document", which doesn't say why to
// someone who didn't write that string themselves. A10.
func TestYAMLVersionDirectiveNamesTheCause(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"1.0", "%YAML 1.0\n---\na: 1\n"},
		{"2.0", "%YAML 2.0\n---\na: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, tc.src, ".a")
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, "%YAML") {
				t.Errorf("error should name the directive as the cause, got %q", msg)
			}
			if !strings.Contains(msg, "1.2") {
				t.Errorf("error should say which version is supported, got %q", msg)
			}
			if codeFor(err) != 2 {
				t.Errorf("codeFor = %d, want 2 (parse)", codeFor(err))
			}
		})
	}
}

// TestYAML11DirectiveDoesNotMaskAnUnrelatedParseFailure: a file that
// declares the one version yaml.v4 actually reads, and *also* fails to
// parse for an unrelated reason (a template directive), must get the
// template hint, not a claim about its (correct) version. A source with no
// parse failure at all never reaches explainParseError, so it can't tell
// the version check from a no-op — this is the case that actually
// exercises the 1.1 directive being found and then correctly not acted on.
func TestYAML11DirectiveDoesNotMaskAnUnrelatedParseFailure(t *testing.T) {
	_, err := execute(t, "%YAML 1.1\n---\n"+helmChart, ".metadata.name")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "template") {
		t.Errorf("want the template directive named as the cause, got %q", msg)
	}
	if strings.Contains(msg, "%YAML") {
		t.Errorf("want no claim about the (correct) version directive, got %q", msg)
	}
}
