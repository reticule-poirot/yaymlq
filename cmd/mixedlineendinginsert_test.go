package cmd

import "testing"

// TestMixedLineEndingInsertKeepsUntouchedTerminators is #327.
// insertEntries (the splice for a brand new key or appended sequence item)
// inserted new text right before the existing last entry's own line
// terminator, rather than after it — invisible on a uniform-ending source,
// since the reassigned terminator and the new one are the same bytes, but
// on a source that mixes line endings it silently rewrote the untouched
// entry's `\r\n` to a plain `\n`, moving the original `\r\n` to trail the
// new entry instead.
func TestMixedLineEndingInsertKeepsUntouchedTerminators(t *testing.T) {
	cases := []struct {
		name string
		in   string
		args []string
		want string
	}{
		{
			"new mapping key after a CRLF line in a mixed stream",
			"a: 1\r\nb: 2\nc: 3\r\n",
			[]string{"set", ".d", "4"},
			"a: 1\r\nb: 2\nc: 3\r\nd: 4\n",
		},
		{
			"appended sequence item after a CRLF line in a mixed stream",
			"a: 1\nitems:\n  - x\r\n",
			[]string{"append", ".items", "y"},
			"a: 1\nitems:\n  - x\r\n  - y\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLineEndingInsertRegressions pins the cases the fix must not disturb:
// a uniform-ending source (where the reassignment above is invisible) and a
// source with no trailing newline at all (where the new entry must not
// gain one either).
func TestLineEndingInsertRegressions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"uniform LF", "a: 1\nb: 2\n", "a: 1\nb: 2\nc: 3\n"},
		{"uniform CRLF", "a: 1\r\nb: 2\r\n", "a: 1\r\nb: 2\r\nc: 3\r\n"},
		{"no trailing newline", "a: 1\nb: 2", "a: 1\nb: 2\nc: 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", ".c", "3")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
