package cmd

import (
	"strings"
	"testing"
)

// TestDefaultCountsAsAMatch is #208. --default and -e/-q used to disagree
// about the same run: the default went to stdout and the exit status said the
// path had not resolved, so
//
//	v=$(yaymlq -e --default fallback .maybe cfg.yml) || die
//
// died holding "fallback" — the one outcome --default exists to prevent.
// Asking for a default says a miss is expected and names the answer, so the
// run succeeded: the caller got a value, which is what -e and -q report.
func TestDefaultCountsAsAMatch(t *testing.T) {
	t.Run("-e prints the default and succeeds", func(t *testing.T) {
		out, err := execute(t, doc, "-e", "-o", "raw", "--default", "N/A", "meta.missing")
		if err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if strings.TrimSpace(out) != "N/A" {
			t.Errorf("out = %q, want N/A", out)
		}
	})

	// -q prints nothing either way, so with --default its exit code is the
	// only thing it still says — and it says the caller has a value.
	t.Run("-q succeeds silently", func(t *testing.T) {
		out, err := execute(t, doc, "-q", "--default", "N/A", "meta.missing")
		if err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if out != "" {
			t.Errorf("want no output, got %q", out)
		}
	})

	// The default is a fallback, not an override: a path that resolves is
	// still the answer, and -e still reports a real match as one.
	t.Run("a real match wins over the default", func(t *testing.T) {
		out, err := execute(t, doc, "-e", "-o", "raw", "--default", "N/A", "meta.name")
		if err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if strings.TrimSpace(out) == "N/A" {
			t.Errorf("the default replaced a resolved value: %q", out)
		}
	})

	// A wildcard that matches no branch is the other way in: it is not an
	// error, so --default fills it and -e has a value to report too.
	t.Run("-e with a wildcard that matches nothing", func(t *testing.T) {
		out, err := execute(t, doc, "-e", "-o", "raw", "--default", "none", "items[*].nope")
		if err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if strings.TrimSpace(out) != "none" {
			t.Errorf("out = %q, want none", out)
		}
	})
}
