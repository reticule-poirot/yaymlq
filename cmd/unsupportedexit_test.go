package cmd

import "testing"

// TestUnsupportedEditIsUsageError is #313. ymledit.ErrUnsupported means the
// path expression itself can't be used with the verb — a wildcard, or the
// whole document — which is exactly what schema's exit-code table calls a
// usage error (exit 3): "a bad flag, argument count, path expression, or
// value". It fell through unclassified to exit 1, the "no match" family,
// so a script doing `yaymlq set ... || create_it` on a malformed path took
// the miss branch instead of failing loudly.
func TestUnsupportedEditIsUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"wildcard set", []string{"set", ".services.*.image", "x"}},
		{"empty path delete", []string{"delete", ""}},
		{"whole document append", []string{"append", ".", "x"}},
		{"whole document rename", []string{"rename", ".", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, "services:\n  web:\n    image: a\n", tc.args...)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if code := codeFor(err); code != 3 {
				t.Errorf("%v: codeFor = %d, want 3 (usage); err: %v", tc.args, code, err)
			}
		})
	}
}
