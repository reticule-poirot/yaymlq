package cmd

import (
	"strings"
	"testing"
)

// TestCollectionMappingKeyErrorIsConsistent is #355: a mapping or sequence
// used as a mapping key (`? [a, b]\n: 1\n`) is refused by every editing
// verb, through rejectMappingKeys/describeMappingKey, with a message that
// doesn't mention Go at all — "a mapping or sequence used as a mapping key
// is not supported". get/keys/len/type decoded the same input their own
// way, straight into `any` without that check, and surfaced yaml.v3's raw
// panic-recovery text instead ("invalid map key: []interface {}{...}").
// Both halves of the CLI now refuse the same input with the same message.
func TestCollectionMappingKeyErrorIsConsistent(t *testing.T) {
	const src = "? [a, b]\n: 1\n"
	const want = "a mapping or sequence used as a mapping key is not supported"

	cases := []struct {
		name string
		args []string
	}{
		{"get", []string{"."}},
		{"keys", []string{"keys", "."}},
		{"len", []string{"len", "."}},
		{"type", []string{"type", "."}},
		{"set", []string{"set", ".x", "1"}},
		{"delete", []string{"delete", ".x"}},
		{"rename", []string{"rename", ".x", "y"}},
		{"append", []string{"append", ".x", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, src, tc.args...)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, want) {
				t.Errorf("%v: got %q, want it to contain %q", tc.args, msg, want)
			}
			if strings.Contains(msg, "interface") || strings.Contains(msg, "[]interface") {
				t.Errorf("%v: error leaks Go syntax: %q", tc.args, msg)
			}
			if codeFor(err) != 2 {
				t.Errorf("%v: codeFor = %d, want 2 (parse)", tc.args, codeFor(err))
			}
		})
	}
}
