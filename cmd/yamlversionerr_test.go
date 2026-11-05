package cmd

import (
	"strings"
	"testing"
)

// TestYAMLVersionDirectiveNamesTheCause: yaml.v3 only reads the 1.1
// directive; any other version fails with "found incompatible YAML
// document" and no line number, which doesn't say why to someone who
// didn't write that string themselves. A10.
func TestYAMLVersionDirectiveNamesTheCause(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"1.2", "%YAML 1.2\n---\na: 1\n"},
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
			if !strings.Contains(msg, "1.1") {
				t.Errorf("error should say which version is supported, got %q", msg)
			}
			if codeFor(err) != 2 {
				t.Errorf("codeFor = %d, want 2 (parse)", codeFor(err))
			}
		})
	}
}

// TestYAML11DirectiveIsFine: the one version yaml.v3 actually reads is not
// an error at all, and the hint must not fire for it.
func TestYAML11DirectiveIsFine(t *testing.T) {
	got, err := execute(t, "%YAML 1.1\n---\na: 1\n", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "1" {
		t.Errorf("got %q, want %q", got, "1")
	}
}

// TestYAMLVersionHintDoesNotMaskAGenuineIncompatibility: the hint is only
// for the directive-version case; an unrelated "incompatible document"
// error (not reachable without a directive today, but the check is gated
// on actually finding one) must not claim a %YAML cause it didn't find.
func TestYAMLVersionHintDoesNotMaskAGenuineIncompatibility(t *testing.T) {
	if line, _, ok := yamlVersionDirective([]byte("a: 1\n")); ok {
		t.Fatalf("found a directive in a file without one: line %d", line)
	}
}
